package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/config"
	"github.com/mann1x/automaticum/internal/daemon"
	"github.com/mann1x/automaticum/internal/docker"
	"github.com/mann1x/automaticum/internal/run"
)

// `automaticum run server` (030 §4.3, 050 §4.1) runs tasks against a model
// server that is already running, in one step: it asks the server what it
// serves, writes or reuses the endpoint and the arm, lets a lane use the
// endpoint, has the daemon forward a loopback-only server to the lanes'
// containers, proposes an idempotency key, and queues the runs once the
// daemon's preflight has asked the server what each run's gates will ask.

// serverFacts is what a running server says of itself.
type serverFacts struct {
	// Base is the server's address as given, without /v1.
	Base   string
	Models run.ModelList
	// Props is llama-server's /props; nil for a server without it.
	Props *run.Props
}

// names lists every name the server answers to, from the model list and
// /props, in the order they are first given.
func (f *serverFacts) names() []string {
	var out []string
	add := func(n string) {
		if n = strings.TrimSpace(n); n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	for _, d := range f.Models.Data {
		add(d.ID)
		for _, a := range d.Aliases {
			add(a)
		}
	}
	for _, a := range f.Props.Aliases() {
		add(a)
	}
	return out
}

// kind is the endpoint kind the server is: a llama-server answers /props.
func (f *serverFacts) kind() string {
	if f.Props != nil {
		return "llamacpp"
	}
	return "openai-compatible"
}

// pathLike says a name is a weights file's path, which a llama-server lists
// among its aliases when it was started without --alias: not a tag.
func pathLike(n string) bool {
	return strings.Contains(n, "/") || strings.Contains(n, `\`) || strings.HasSuffix(strings.ToLower(n), ".gguf")
}

// pickTag is the model tag to ask for: the one given, which the server must
// answer to, or the one name it answers to that is not a path.
func pickTag(f *serverFacts, given string) (string, error) {
	names := f.names()
	if given != "" {
		if !slices.Contains(names, given) {
			return "", fmt.Errorf("the server does not answer to %s; it answers to %s", given, listOr(names, "nothing"))
		}
		return given, nil
	}
	var tags []string
	for _, n := range names {
		if !pathLike(n) {
			tags = append(tags, n)
		}
	}
	switch len(tags) {
	case 1:
		return tags[0], nil
	case 0:
		return "", fmt.Errorf("the server answers to %s, none of which is a model tag; start it with --alias <tag>, or give --model", listOr(names, "nothing"))
	}
	return "", fmt.Errorf("the server answers to %s; say which with --model", strings.Join(tags, ", "))
}

func listOr(names []string, none string) string {
	if len(names) == 0 {
		return none
	}
	return strings.Join(names, ", ")
}

// readServer asks a server at base (with or without /v1) for its model list
// and, when it has one, llama-server's /props.
func readServer(ctx context.Context, hc *http.Client, base, apiKey string) (*serverFacts, error) {
	base = strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1")
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("--url %q is not an http or https address", base)
	}
	f := &serverFacts{Base: base}
	get := func(path string, out any) (int, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", base+path, nil)
		if err != nil {
			return 0, err
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := hc.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return resp.StatusCode, nil
		}
		return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
	}
	code, err := get("/v1/models", &f.Models)
	switch {
	case err != nil:
		return nil, fmt.Errorf("the server at %s does not answer /v1/models: %v", base, err)
	case code != http.StatusOK:
		return nil, fmt.Errorf("the server at %s: /v1/models gave status %d", base, code)
	}
	e := &config.Endpoint{Name: "the server", Kind: "llamacpp", BaseURL: base + "/v1"}
	if p, err := run.ReadProps(ctx, hc, e, apiKey); err == nil {
		f.Props = p
	}
	return f, nil
}

// serverOpts is what the command was asked.
type serverOpts struct {
	URL, Model, Extends, Name, Endpoint, Lane, Sampler, APIKeyFile string
	Tasks                                                          []string
	Count, Capacity                                                int
	Limits                                                         *run.Override
	Today                                                          time.Time
}

// serverPlan is what the command will write and start.
type serverPlan struct {
	Tag, Kind, BaseURL, ForwardTo string

	Endpoint, EndpointText string
	EndpointNew            bool

	Arm, ArmText string
	ArmNew       bool
	// ArmNotes say what the arm changes of its parent's values, and why.
	ArmNotes []string

	Lane, LaneText, LaneOld string
	LaneAttach              bool

	Key string
	// Catalogued says the tag has an entry in models/: the owner's words
	// for what it is (D70). Without one the runs say it is missing (D71).
	Catalogued bool
}

var slugRE = regexp.MustCompile(`[^a-z0-9._-]+`)

// slug makes a name a file and a key may carry.
func slug(s string) string {
	s = strings.Trim(slugRE.ReplaceAllString(strings.ToLower(s), "-"), "-._")
	if s == "" {
		return "x"
	}
	return s
}

// isLoopback says a URL's host is this host's loopback.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// containerAddress is where a lane's container reaches the server, and
// what the daemon must forward for it: a loopback address is reached on
// the bridge's gateway, directly when the server listens there too, else
// through the daemon's forward (forward_to).
func containerAddress(serverURL, gateway string, answersOnGateway bool) (base, forward string, err error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", "", err
	}
	if !isLoopback(u.Hostname()) {
		return strings.TrimSuffix(strings.TrimRight(serverURL, "/"), "/v1") + "/v1", "", nil
	}
	port := u.Port()
	if port == "" {
		return "", "", fmt.Errorf("--url %s states no port", serverURL)
	}
	base = u.Scheme + "://" + net.JoinHostPort(gateway, port) + "/v1"
	if answersOnGateway {
		return base, "", nil
	}
	host := u.Hostname()
	if host == "localhost" {
		host = "127.0.0.1"
	}
	return base, net.JoinHostPort(host, port), nil
}

// pickLane is the lane named, or one that may use the endpoint and is
// idle, or the first idle lane; an idle lane runs nothing, has nothing
// queued and is not drained, quarantined or reserved.
func pickLane(l *config.Local, st *daemon.Status, given, endpoint string) (string, error) {
	if given != "" {
		if l.Lane(given) == nil {
			return "", fmt.Errorf("there is no lane %s; the lanes are %s", given, strings.Join(l.LaneNames(), ", "))
		}
		return given, nil
	}
	queued := map[string]int{}
	for _, q := range st.Queue {
		queued[q.Lane]++
	}
	idle := func(s daemon.LaneStatus) bool {
		return s.State == "idle" && queued[s.Lane] == 0 && !s.Drained && s.Quarantined == "" && s.Reserved == nil && !s.Removed
	}
	var free []string
	var busy []string
	for _, s := range st.Lanes {
		if l.Lane(s.Lane) == nil {
			continue
		}
		if !idle(s) {
			what := s.State
			if n := queued[s.Lane]; n > 0 {
				what += fmt.Sprintf(", %d queued", n)
			}
			busy = append(busy, s.Lane+" ("+what+")")
			continue
		}
		if endpoint != "" && l.MayUse(s.Lane, endpoint) {
			return s.Lane, nil
		}
		free = append(free, s.Lane)
	}
	if len(free) > 0 {
		slices.Sort(free)
		return free[0], nil
	}
	return "", fmt.Errorf("no lane is idle (%s); name one with --lane and the runs queue behind its work", strings.Join(busy, ", "))
}

var endpointsLineRE = regexp.MustCompile(`(?m)^endpoints[ \t]*=[ \t]*\[([^\]\n]*)\][ \t]*(#.*)?$`)

// attachEndpoint adds an endpoint to a lane file's endpoints list, keeping
// every other line. A list written over several lines is not edited.
func attachEndpoint(text, endpoint string) (string, error) {
	m := endpointsLineRE.FindStringSubmatchIndex(text)
	if m == nil {
		return "", errors.New("its endpoints list is not on one line; add the endpoint to it by hand")
	}
	inner := strings.TrimSpace(text[m[2]:m[3]])
	inner = strings.TrimSuffix(inner, ",")
	add := strconv.Quote(endpoint)
	if inner != "" {
		add = inner + ", " + add
	}
	return text[:m[2]] + add + text[m[3]:], nil
}

// q is a TOML basic string.
func q(s string) string { return strconv.Quote(s) }

// planServer decides what to write: the endpoint (one already there for
// the same address is reused), the arm and the lane's change. It writes
// nothing.
func planServer(l *config.Local, f *serverFacts, o serverOpts, st *daemon.Status, gateway string, answersOnGateway bool) (*serverPlan, error) {
	p := &serverPlan{Kind: f.kind()}
	var err error
	if p.Tag, err = pickTag(f, o.Model); err != nil {
		return nil, err
	}
	if p.BaseURL, p.ForwardTo, err = containerAddress(f.Base, gateway, answersOnGateway); err != nil {
		return nil, err
	}
	same := func(e *config.Endpoint) bool {
		return strings.TrimRight(e.BaseURL, "/") == p.BaseURL && e.ForwardTo == p.ForwardTo && e.Kind == p.Kind && e.APIKeyFile == o.APIKeyFile
	}
	if o.Endpoint != "" {
		if e := l.Endpoint(o.Endpoint); e != nil {
			if !same(e) {
				return nil, fmt.Errorf("endpoint %s is already another server's (%s); give another --endpoint", o.Endpoint, e.BaseURL)
			}
			p.Endpoint = o.Endpoint
		}
	} else {
		for _, n := range l.EndpointNames() {
			if e := l.Endpoint(n); e != nil && !e.Cell && same(e) {
				p.Endpoint = n
				break
			}
		}
	}
	if p.Lane, err = pickLane(l, st, o.Lane, p.Endpoint); err != nil {
		return nil, err
	}
	if p.Endpoint == "" {
		p.EndpointNew = true
		p.Endpoint = o.Endpoint
		if p.Endpoint == "" {
			u, _ := url.Parse(p.BaseURL)
			base := "server-" + u.Port()
			if u.Port() == "" {
				base = "server-" + slug(u.Hostname())
			}
			p.Endpoint = base
			for i := 2; l.Endpoint(p.Endpoint) != nil || fileExists(filepath.Join(l.Dir, "endpoints", p.Endpoint+".toml")); i++ {
				p.Endpoint = fmt.Sprintf("%s-%d", base, i)
			}
		}
		capacity := o.Capacity
		if capacity < 1 {
			capacity = 1
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# Written by `automaticum run server` on %s for the server at %s,\n", o.Today.Format("2006-01-02"), f.Base)
		fmt.Fprintf(&b, "# which answered to %s", strings.Join(f.names(), ", "))
		if f.Props != nil && f.Props.BuildInfo != "" {
			fmt.Fprintf(&b, " (llama-server build %s)", f.Props.BuildInfo)
		}
		b.WriteString(".\n")
		if p.ForwardTo != "" {
			b.WriteString("# It listens only on loopback: the daemon listens on base_url's address and forwards to it.\n")
		}
		fmt.Fprintf(&b, "kind = %s\nbase_url = %s\n", q(p.Kind), q(p.BaseURL))
		if p.ForwardTo != "" {
			fmt.Fprintf(&b, "forward_to = %s\n", q(p.ForwardTo))
		}
		fmt.Fprintf(&b, "host = %s\ncapacity = %d\n", q(l.Lane(p.Lane).Host), capacity)
		if o.APIKeyFile != "" {
			fmt.Fprintf(&b, "api_key_file = %s\n", q(o.APIKeyFile))
		}
		p.EndpointText = b.String()
	}
	if !l.MayUse(p.Lane, p.Endpoint) {
		path := filepath.Join(l.Dir, "lanes", p.Lane+".toml")
		old, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		text, err := attachEndpoint(string(old), p.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("lane %s: %w", p.Lane, err)
		}
		p.LaneAttach, p.LaneOld, p.LaneText = true, string(old), text
	}

	parent, err := config.LoadArm(l.ArmsDir(), o.Extends)
	if err != nil {
		return nil, fmt.Errorf("--extends: %w", err)
	}
	p.Arm = o.Name
	if p.Arm == "" {
		p.Arm = slug(o.Extends + "-" + p.Endpoint + "-" + p.Tag)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Written by `automaticum run server` on %s: arm %s on the server at %s (endpoint %s), model %s.\n",
		o.Today.Format("2006-01-02"), o.Extends, f.Base, p.Endpoint, p.Tag)
	if o.Sampler == "tag" {
		b.WriteString("# The sampler is the server's own: none is sent, and each run records the server's defaults from /props.\n")
	} else {
		fmt.Fprintf(&b, "# The sampler is %s's.\n", o.Extends)
	}
	fmt.Fprintf(&b, "extends = %s\nendpoint = %s\n\n[model]\nmodel = %s\n", q(o.Extends), q(p.Endpoint), q(p.Tag))
	if pr := parent.Model.Provider; (pr == "ollama" || pr == "xollama") && p.Kind != pr {
		fmt.Fprintf(&b, "provider = %s\n", q(config.ProviderFor(p.Kind)))
		p.ArmNotes = append(p.ArmNotes, fmt.Sprintf("provider %s, not %s's %s: the server is not an Ollama server", config.ProviderFor(p.Kind), o.Extends, pr))
	}
	if parent.Model.Digest != "" {
		b.WriteString("digest = \"\"\n")
		p.ArmNotes = append(p.ArmNotes, fmt.Sprintf("no digest pin: %s's pin is its own model's", o.Extends))
	}
	if cw := parent.Model.ContextWindow; cw != nil && f.Props != nil && f.Props.NCtx > 0 && *cw > f.Props.NCtx {
		fmt.Fprintf(&b, "context_window = %d\n", f.Props.NCtx)
		p.ArmNotes = append(p.ArmNotes, fmt.Sprintf("context window %d, not %s's %d: a slot of the server holds %d tokens", f.Props.NCtx, o.Extends, *cw, f.Props.NCtx))
	}
	if o.Sampler == "tag" {
		b.WriteString("\n[sampler]\nowner = \"tag\"\n")
	}
	p.ArmText = b.String()
	path := filepath.Join(l.ArmsDir(), p.Arm+".toml")
	if old, err := os.ReadFile(path); err == nil {
		if body(string(old)) != body(p.ArmText) {
			return nil, fmt.Errorf("arm %s already exists with other values; give another --name, or run it with run start", p.Arm)
		}
	} else {
		p.ArmNew = true
	}
	p.Key = defaultKey(p.Arm, o)
	if models, err := config.LoadModels(l.ModelsDir()); err == nil {
		p.Catalogued = models[p.Tag] != nil
	}
	return p, nil
}

// body is a TOML file's text without its comments and blank lines, to
// compare two files' values.
func body(text string) string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// defaultKey is the idempotency key proposed: the arm, the tasks, the
// count, the wall limit and the day, so the same command on the same day
// finds the batch it made instead of making another.
func defaultKey(arm string, o serverOpts) string {
	var tasks []string
	for _, t := range o.Tasks {
		tasks = append(tasks, slug(t[strings.LastIndex(t, "/")+1:]))
	}
	k := fmt.Sprintf("%s-%s-n%d", arm, strings.Join(tasks, "+"), o.Count)
	if o.Limits != nil && o.Limits.WallS != nil {
		k += fmt.Sprintf("-w%d", *o.Limits.WallS)
	}
	k = slug(k) + "-" + o.Today.Format("20060102")
	if len(k) > 120 {
		sum := sha256.Sum256([]byte(k))
		k = k[:100] + "-" + hex.EncodeToString(sum[:4]) + "-" + o.Today.Format("20060102")
	}
	return k
}

// writeAtomic writes a file through a temporary file in its directory, so
// a reader never sees half of it.
func writeAtomic(path, text string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// apply writes the plan: the endpoint first, then the arm, then the lane,
// so each step leaves a configuration that loads. The undo puts back what
// was there.
func (p *serverPlan) apply(l *config.Local) (undo func(), err error) {
	var undos []func()
	undo = func() {
		for i := len(undos) - 1; i >= 0; i-- {
			undos[i]()
		}
	}
	defer func() {
		if err != nil {
			undo()
		}
	}()
	if p.EndpointNew {
		path := filepath.Join(l.Dir, "endpoints", p.Endpoint+".toml")
		if err := writeAtomic(path, p.EndpointText); err != nil {
			return undo, err
		}
		undos = append(undos, func() { os.Remove(path) })
	}
	if p.ArmNew {
		path := filepath.Join(l.ArmsDir(), p.Arm+".toml")
		if err := writeAtomic(path, p.ArmText); err != nil {
			return undo, err
		}
		undos = append(undos, func() { os.Remove(path) })
		a, err := config.LoadArm(l.ArmsDir(), p.Arm)
		if err != nil {
			return undo, err
		}
		if probs := a.Validate(); len(probs) > 0 {
			return undo, fmt.Errorf("arm %s does not validate:\n  - %s", p.Arm, strings.Join(probs, "\n  - "))
		}
	}
	if p.LaneAttach {
		path := filepath.Join(l.Dir, "lanes", p.Lane+".toml")
		if err := writeAtomic(path, p.LaneText); err != nil {
			return undo, err
		}
		old := p.LaneOld
		undos = append(undos, func() { writeAtomic(path, old) })
	}
	return undo, nil
}

// print says what the plan does.
func (p *serverPlan) print(w io.Writer, dry bool) {
	will := map[bool]string{true: "would write", false: "writes"}[dry]
	fmt.Fprintf(w, "model       %s (%s)\n", p.Tag, p.Kind)
	switch {
	case p.EndpointNew:
		fmt.Fprintf(w, "endpoint    %s: new, %s endpoints/%s.toml\n", p.Endpoint, will, p.Endpoint)
	default:
		fmt.Fprintf(w, "endpoint    %s: already there, reused\n", p.Endpoint)
	}
	fmt.Fprintf(w, "            containers reach it at %s", p.BaseURL)
	if p.ForwardTo != "" {
		fmt.Fprintf(w, ", which the daemon forwards to %s (the server listens only on loopback)", p.ForwardTo)
	}
	fmt.Fprintln(w)
	if p.ArmNew {
		fmt.Fprintf(w, "arm         %s: new, %s arms/%s.toml\n", p.Arm, will, p.Arm)
	} else {
		fmt.Fprintf(w, "arm         %s: already there with these values, reused\n", p.Arm)
	}
	for _, n := range p.ArmNotes {
		fmt.Fprintf(w, "            %s\n", n)
	}
	if !p.Catalogued {
		fmt.Fprintf(w, "            the tag has no entry in models/: its runs record what the server says and that family, size and variant are not stated (D71); add one with them, in your words\n")
	}
	if p.LaneAttach {
		fmt.Fprintf(w, "lane        %s: may now use %s (%s lanes/%s.toml)\n", p.Lane, p.Endpoint, map[bool]string{true: "would edit", false: "edits"}[dry], p.Lane)
	} else {
		fmt.Fprintf(w, "lane        %s\n", p.Lane)
	}
	if dry {
		for _, f := range []struct {
			what, text string
			show       bool
		}{{"endpoints/" + p.Endpoint + ".toml", p.EndpointText, p.EndpointNew}, {"arms/" + p.Arm + ".toml", p.ArmText, p.ArmNew}} {
			if f.show {
				fmt.Fprintf(w, "\n--- %s\n%s", f.what, f.text)
			}
		}
		fmt.Fprintln(w)
	}
}

// answersAt says whether the server at base (a /v1 address) answers with
// the same names as the one asked first.
func answersAt(ctx context.Context, base, apiKey string, want []string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	f, err := readServer(ctx, &http.Client{Timeout: 3 * time.Second}, base, apiKey)
	return err == nil && slices.Equal(f.names(), want)
}

// askKey asks for the idempotency key on a terminal, offering the default;
// elsewhere the default is taken.
func askKey(def string, in *os.File, out io.Writer) string {
	if st, err := in.Stat(); err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return def
	}
	fmt.Fprintf(out, "idempotency key [%s]: ", def)
	line, _ := bufio.NewReader(in).ReadString('\n')
	if line = strings.TrimSpace(line); line != "" {
		return line
	}
	return def
}

// bridgeGateway is the host's address on the default bridge, where the
// lanes' containers reach it; a test replaces it.
var bridgeGateway = func(ctx context.Context, socket string) (string, error) {
	return docker.New(socket).Gateway(ctx, "bridge")
}

func runServer(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run server", flag.ExitOnError)
	p, dial := addClient(fs)
	var o serverOpts
	var tasks list
	fs.StringVar(&o.URL, "url", "", "the running server's address as this host reaches it, e.g. http://127.0.0.1:8244")
	fs.Var(&tasks, "task", "the task; may be given several times")
	fs.IntVar(&o.Count, "n", 1, "runs of each task")
	fs.StringVar(&o.Extends, "extends", "", "the arm whose harness, agent and guards the new arm takes; its endpoint and model are replaced")
	fs.StringVar(&o.Model, "model", "", "the model tag to ask for; by default the one name the server answers to that is not a path")
	fs.StringVar(&o.Sampler, "sampler", "tag", "tag: send no sampler, the server's defaults apply and each run records them from /props; arm: the --extends arm's sampler")
	fs.StringVar(&o.Name, "name", "", "the new arm's name (default <extends>-<endpoint>-<model>)")
	fs.StringVar(&o.Endpoint, "endpoint", "", "the endpoint's name (default: an endpoint already there for the same address, else server-<port>)")
	fs.StringVar(&o.Lane, "lane", "", "the lane (default: an idle lane, one that may already use the endpoint first)")
	fs.IntVar(&o.Capacity, "capacity", 1, "a new endpoint's capacity: how many runs it serves at once")
	fs.StringVar(&o.APIKeyFile, "api-key-file", "", "a credential file for the server, relative to the configuration directory")
	key := fs.String("idempotency-key", "", "the batch's key (default: proposed from the arm, tasks, count, wall limit and day, and asked for on a terminal)")
	yes := fs.Bool("yes", false, "take the proposed idempotency key without asking")
	dry := fs.Bool("dry-run", false, "say what would be written and queued; write and queue nothing")
	priority := fs.Int("priority", 0, "the batch's priority (D125)")
	heldout := fs.Bool("heldout", false, "the batch is held out of training exports (D115)")
	limits := addLimits(fs)
	if rest := parse(fs, args); len(rest) > 0 || o.URL == "" || len(tasks) == 0 || o.Extends == "" {
		return errors.New("usage: automaticum run server --url <server> --task <task> --extends <arm> [-n 10] [--wall-s S] [--model TAG] [--sampler tag|arm] [--name ARM] [--lane L] [--idempotency-key K] [--yes] [--dry-run]")
	}
	if o.Sampler != "tag" && o.Sampler != "arm" {
		return fmt.Errorf("--sampler is tag or arm, not %q", o.Sampler)
	}
	o.Tasks, o.Limits, o.Today = tasks, limits(), time.Now()
	if err := o.Limits.Check(); err != nil {
		return err
	}
	l, err := p.local()
	if err != nil {
		return err
	}
	apiKey := ""
	if o.APIKeyFile != "" {
		if apiKey, err = l.APIKey(&config.Endpoint{Name: "the server", APIKeyFile: o.APIKeyFile}); err != nil {
			return err
		}
	}
	f, err := readServer(ctx, &http.Client{Timeout: 15 * time.Second}, o.URL, apiKey)
	if err != nil {
		return err
	}
	c, err := dial()
	if err != nil {
		return err
	}
	var st daemon.Status
	if err := c.do(ctx, "GET", "/v1/status", nil, &st); err != nil {
		return err
	}
	gateway, answers := "", false
	if u, _ := url.Parse(f.Base); isLoopback(u.Hostname()) {
		if gateway, err = bridgeGateway(ctx, l.Settings().DockerSocket); err != nil {
			return fmt.Errorf("where the lanes' containers reach this host: %w", err)
		}
		answers = answersAt(ctx, u.Scheme+"://"+net.JoinHostPort(gateway, u.Port()), apiKey, f.names())
	}
	plan, err := planServer(l, f, o, &st, gateway, answers)
	if err != nil {
		return err
	}
	// With --json only the start's answer goes to standard output.
	out := io.Writer(os.Stdout)
	if c.json {
		out = os.Stderr
	}
	plan.print(out, *dry)
	if *key != "" {
		plan.Key = *key
	} else if !*yes && !*dry {
		plan.Key = askKey(plan.Key, os.Stdin, out)
	}
	fmt.Fprintf(out, "key         %s (the same command with it again starts nothing new)\n", plan.Key)
	if *dry {
		fmt.Fprintln(out, "dry run: nothing was written or queued; the daemon asks the server's gates before queueing when it is run")
		return nil
	}
	undo, err := plan.apply(l)
	if err != nil {
		return err
	}
	var res daemon.ReloadResult
	if err := c.do(ctx, "POST", "/v1/config/reload", nil, &res); err != nil || res.Error != "" {
		undo()
		c.do(ctx, "POST", "/v1/config/reload", nil, nil)
		if err == nil {
			err = errors.New(res.Error)
		}
		return fmt.Errorf("the daemon did not take the new configuration, so it was put back: %w", err)
	}
	if len(res.Unforwarded) > 0 {
		return fmt.Errorf("written and in force, but %s; nothing was queued", strings.Join(res.Unforwarded, "; "))
	}
	// The containers' address answers, directly or through the forward.
	ok := false
	for deadline := time.Now().Add(5 * time.Second); !ok && time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		ok = answersAt(ctx, plan.BaseURL, apiKey, f.names())
	}
	if !ok {
		return fmt.Errorf("written and in force, but the server does not answer at %s, where the containers reach it; nothing was queued", plan.BaseURL)
	}
	req := daemon.StartRequest{Arm: plan.Arm, Tasks: o.Tasks, Lane: plan.Lane, Count: o.Count, Key: plan.Key,
		Limits: o.Limits, Priority: *priority, Heldout: *heldout}
	var resp daemon.StartResponse
	if err := c.do(ctx, "POST", "/v1/runs", req, &resp); err != nil {
		return fmt.Errorf("the endpoint and arm are written and in force; the start was refused: %w", err)
	}
	if c.json {
		return printJSON(resp)
	}
	printStart(resp, []string{plan.Arm})
	return nil
}
