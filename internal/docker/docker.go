// Package docker is a small client for the Docker Engine API over the local
// socket. It has what a lane needs and no more: pull, build and inspect an
// image; create, start, follow, wait for, kill and remove a container, and
// copy paths out of one that has stopped.
//
// automaticum never runs the docker command and never stops a container by
// name: every container is addressed by the id the engine returned when it
// was created.
package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultSocket is where the engine listens on Linux.
const DefaultSocket = "/var/run/docker.sock"

// apiVersion is the oldest API version this client needs. The engine serves
// any version from its minimum up to its own; an engine whose minimum is
// newer (Docker 29.1 and later refuse anything below 1.44) is spoken to at
// its minimum instead, read once from its unversioned /version.
const apiVersion = "v1.41"

// Client talks to one engine.
type Client struct {
	http   *http.Client
	stream *http.Client // no overall timeout: logs, waits, builds

	mu  sync.Mutex
	ver string // the API version in use, once the engine has said its minimum
}

// New returns a client for the engine at socket; "" means DefaultSocket.
func New(socket string) *Client {
	if socket == "" {
		socket = DefaultSocket
	}
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}
	return &Client{
		http:   &http.Client{Transport: &http.Transport{DialContext: dial}, Timeout: 60 * time.Second},
		stream: &http.Client{Transport: &http.Transport{DialContext: dial}},
	}
}

// Error is an error response from the engine.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("docker: %d: %s", e.Status, e.Message) }

// IsNotFound reports whether err says the image or container does not exist.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

func (c *Client) do(ctx context.Context, hc *http.Client, method, path string, query url.Values, body io.Reader, contentType string) (*http.Response, error) {
	u := "http://docker/" + c.apiVersion(ctx) + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker: %s %s: %w", method, path, err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		var m struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(b, &m) != nil || m.Message == "" {
			m.Message = strings.TrimSpace(string(b))
		}
		return nil, &Error{Status: resp.StatusCode, Message: m.Message}
	}
	return resp, nil
}

func (c *Client) json(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var body io.Reader
	ct := ""
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body, ct = bytes.NewReader(b), "application/json"
	}
	resp, err := c.do(ctx, c.http, method, path, query, body, ct)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// apiVersion is apiVersion, or the engine's minimum when that is newer. A
// failed question is not remembered: the request that follows reports the
// engine's own error.
func (c *Client) apiVersion(ctx context.Context) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ver != "" {
		return c.ver
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://docker/version", nil)
	if err != nil {
		return apiVersion
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return apiVersion
	}
	defer resp.Body.Close()
	var v Version
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&v) != nil {
		return apiVersion
	}
	c.ver = pickVersion(v.MinAPIVersion)
	return c.ver
}

// pickVersion is apiVersion unless the engine's minimum is newer.
func pickVersion(min string) string {
	if min != "" && newer(min, strings.TrimPrefix(apiVersion, "v")) {
		return "v" + min
	}
	return apiVersion
}

// newer reports whether API version a ("1.44") is after b.
func newer(a, b string) bool {
	pa, pb := strings.SplitN(a, ".", 2), strings.SplitN(b, ".", 2)
	if len(pa) != 2 || len(pb) != 2 {
		return false
	}
	for i := range 2 {
		x, err1 := strconv.Atoi(pa[i])
		y, err2 := strconv.Atoi(pb[i])
		if err1 != nil || err2 != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// Version is what the engine says about itself.
type Version struct {
	Version       string
	APIVersion    string `json:"ApiVersion"`
	MinAPIVersion string
	Os            string
	Arch          string
	KernelVersion string
}

// Version asks the engine for its version. It is also the reachability check.
func (c *Client) Version(ctx context.Context) (Version, error) {
	var v Version
	err := c.json(ctx, "GET", "/version", nil, nil, &v)
	return v, err
}

// Image is what a lane records about an image.
type Image struct {
	ID          string `json:"Id"` // sha256:... of the image configuration
	RepoDigests []string
	RepoTags    []string
	Size        int64
	Created     string
	Config      struct {
		Labels map[string]string
	}
}

// ImageInspect returns the image named by ref (a tag, a digest or an id).
func (c *Client) ImageInspect(ctx context.Context, ref string) (Image, error) {
	var im Image
	err := c.json(ctx, "GET", "/images/"+url.PathEscape(ref)+"/json", nil, nil, &im)
	return im, err
}

// progress drains a stream of JSON progress messages (pull, build) and
// returns the first error message in it. Each line's text goes to w.
func progress(r io.Reader, w io.Writer) error {
	dec := json.NewDecoder(r)
	for {
		var m struct {
			Stream string `json:"stream"`
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if err := dec.Decode(&m); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if m.Error != "" {
			return errors.New("docker: " + strings.TrimSpace(m.Error))
		}
		if w != nil {
			if m.Stream != "" {
				io.WriteString(w, m.Stream)
			} else if m.Status != "" {
				io.WriteString(w, m.Status+"\n")
			}
		}
	}
}

// ImagePull pulls ref. Progress text goes to w, which may be nil.
func (c *Client) ImagePull(ctx context.Context, ref string, w io.Writer) error {
	resp, err := c.do(ctx, c.stream, "POST", "/images/create", url.Values{"fromImage": {ref}}, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return progress(resp.Body, w)
}

// BuildOptions are the inputs of an image build.
type BuildOptions struct {
	Tag        string
	Dockerfile string            // path inside the context; "" means Dockerfile
	Args       map[string]string // build arguments
	Pull       bool              // refresh the base image
	Network    string            // "" for the default, "none" to build offline
}

// ImageBuild builds an image from a tar archive of the build context. Build
// output goes to w, which may be nil.
func (c *Client) ImageBuild(ctx context.Context, tarContext io.Reader, o BuildOptions, w io.Writer) error {
	q := url.Values{"t": {o.Tag}, "rm": {"1"}, "forcerm": {"1"}}
	if o.Dockerfile != "" {
		q.Set("dockerfile", o.Dockerfile)
	}
	if o.Pull {
		q.Set("pull", "1")
	}
	if o.Network != "" {
		q.Set("networkmode", o.Network)
	}
	if len(o.Args) > 0 {
		b, _ := json.Marshal(o.Args)
		q.Set("buildargs", string(b))
	}
	resp, err := c.do(ctx, c.stream, "POST", "/build", q, tarContext, "application/x-tar")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return progress(resp.Body, w)
}

// Mount binds a host path into a container.
type Mount struct {
	Source   string // absolute host path
	Target   string // absolute path in the container
	ReadOnly bool
}

// Spec describes a container.
type Spec struct {
	// Extra, when set, is asked when the timeout is reached: the time it
	// returns is added to the timeout, as often as it is more than was
	// already added. A run that was held for a while gets that long more.
	Extra   func() time.Duration
	Name    string // optional; the id is what everything else uses
	Image   string
	Cmd     []string
	Env     []string // KEY=value
	WorkDir string
	User    string // "uid:gid"; "" is the image's own
	Mounts  []Mount
	// Tmpfs maps a container path to mount options ("" for the defaults).
	Tmpfs map[string]string
	// Network is "" for the default bridge, or "none".
	Network string
	// Limits; zero means none.
	MemoryBytes int64
	NanoCPUs    int64
	PidsLimit   int64
	// ReadOnlyRoot mounts the container's root filesystem read-only.
	ReadOnlyRoot bool
	Labels       map[string]string
	// CapAdd gives the container these capabilities; it has none
	// otherwise. DefaultCapabilities are the engine's own defaults, what a
	// task that runs as root is given (D110).
	CapAdd []string
	// Collect maps container paths to host directories: once the
	// container has stopped, and before it is removed, each path is
	// copied into its directory (CopyOut). Result.CollectErr says what
	// could not be copied.
	Collect map[string]string
}

// DefaultCapabilities are the capabilities the engine gives a container
// unless told otherwise.
var DefaultCapabilities = []string{
	"AUDIT_WRITE", "CHOWN", "DAC_OVERRIDE", "FOWNER", "FSETID", "KILL", "MKNOD",
	"NET_BIND_SERVICE", "NET_RAW", "SETFCAP", "SETGID", "SETPCAP", "SETUID", "SYS_CHROOT",
}

// Create makes a container and returns its id. It is not started.
func (c *Client) Create(ctx context.Context, s Spec) (string, error) {
	type hostConfig struct {
		Binds          []string          `json:",omitempty"`
		Tmpfs          map[string]string `json:",omitempty"`
		NetworkMode    string            `json:",omitempty"`
		Memory         int64             `json:",omitempty"`
		MemorySwap     int64             `json:",omitempty"`
		NanoCpus       int64             `json:",omitempty"`
		PidsLimit      int64             `json:",omitempty"`
		ReadonlyRootfs bool              `json:",omitempty"`
		Init           bool
		CapDrop        []string
		CapAdd         []string `json:",omitempty"`
		SecurityOpt    []string
	}
	hc := hostConfig{
		Tmpfs: s.Tmpfs, NetworkMode: s.Network, Memory: s.MemoryBytes, MemorySwap: s.MemoryBytes,
		NanoCpus: s.NanoCPUs, PidsLimit: s.PidsLimit, ReadonlyRootfs: s.ReadOnlyRoot,
		// An init process reaps what the agent's commands leave behind, and
		// a container has no use for any capability it is not given, or
		// for gaining privileges.
		Init: true, CapDrop: []string{"ALL"}, CapAdd: s.CapAdd, SecurityOpt: []string{"no-new-privileges"},
	}
	for _, m := range s.Mounts {
		b := m.Source + ":" + m.Target
		if m.ReadOnly {
			b += ":ro"
		}
		hc.Binds = append(hc.Binds, b)
	}
	body := map[string]any{
		"Image": s.Image, "Cmd": s.Cmd, "Env": s.Env, "WorkingDir": s.WorkDir, "User": s.User,
		"Labels": s.Labels, "HostConfig": hc, "AttachStdout": true, "AttachStderr": true, "Tty": false,
	}
	q := url.Values{}
	if s.Name != "" {
		q.Set("name", s.Name)
	}
	var out struct {
		ID string `json:"Id"`
	}
	if err := c.json(ctx, "POST", "/containers/create", q, body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// Start starts a created container.
func (c *Client) Start(ctx context.Context, id string) error {
	return c.json(ctx, "POST", "/containers/"+id+"/start", nil, nil, nil)
}

// Kill sends a signal ("" means KILL) to a container. A container that has
// already stopped is not an error.
func (c *Client) Kill(ctx context.Context, id, signal string) error {
	q := url.Values{}
	if signal != "" {
		q.Set("signal", signal)
	}
	err := c.json(ctx, "POST", "/containers/"+id+"/kill", q, nil, nil)
	var e *Error
	if errors.As(err, &e) && e.Status == http.StatusConflict {
		return nil
	}
	return err
}

// Remove removes a container and its anonymous volumes, killing it first if
// it runs. A container that is already gone is not an error.
func (c *Client) Remove(ctx context.Context, id string) error {
	err := c.json(ctx, "DELETE", "/containers/"+id, url.Values{"force": {"1"}, "v": {"1"}}, nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// CopyOut copies the directory p of a container into the existing host
// directory dst: what is under p is under dst. Files, directories and links
// are copied, links as links; nothing is written outside dst, whatever the
// links say. A p the container does not have leaves dst empty.
func (c *Client) CopyOut(ctx context.Context, id, p, dst string) error {
	resp, err := c.do(ctx, c.stream, "GET", "/containers/"+id+"/archive", url.Values{"path": {p}}, nil, "")
	if IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return untar(resp.Body, path.Base(p), dst)
}

// untar writes the archive of the directory named top into dst.
func untar(r io.Reader, top, dst string) error {
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(h.Name)
		if name == top {
			if h.Typeflag != tar.TypeDir {
				return fmt.Errorf("docker: %s is not a directory", top)
			}
			continue
		}
		rel, ok := strings.CutPrefix(name, top+"/")
		if !ok || rel == "" || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
			return fmt.Errorf("docker: the archive of %s holds %q", top, h.Name)
		}
		mode := os.FileMode(h.Mode).Perm()
		switch h.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(rel, 0o755); err != nil {
				return err
			}
			if err := root.Chmod(rel, mode|0o700); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := root.MkdirAll(path.Dir(rel), 0o755); err != nil {
				return err
			}
			if err := root.Symlink(h.Linkname, rel); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := root.MkdirAll(path.Dir(rel), 0o755); err != nil {
				return err
			}
			f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
			if err := root.Chmod(rel, mode); err != nil {
				return err
			}
		case tar.TypeLink:
			old, ok := strings.CutPrefix(path.Clean(h.Linkname), top+"/")
			if !ok || strings.HasPrefix(old, "../") {
				return fmt.Errorf("docker: the archive of %s links %q to %q", top, h.Name, h.Linkname)
			}
			if err := root.MkdirAll(path.Dir(rel), 0o755); err != nil {
				return err
			}
			if err := root.Link(old, rel); err != nil {
				return err
			}
		default:
			// Devices, fifos: not files a test reads.
		}
	}
}

// State is a container's state as the engine reports it.
type State struct {
	Status     string // created, running, exited, ...
	Running    bool
	ExitCode   int
	OOMKilled  bool
	Error      string
	StartedAt  string
	FinishedAt string
}

// Inspect returns a container's state and the id of the image it runs.
func (c *Client) Inspect(ctx context.Context, id string) (State, string, error) {
	var out struct {
		State State
		Image string
	}
	err := c.json(ctx, "GET", "/containers/"+id+"/json", nil, nil, &out)
	return out.State, out.Image, err
}

// Wait blocks until the container stops and returns its exit code.
func (c *Client) Wait(ctx context.Context, id string) (int, error) {
	resp, err := c.do(ctx, c.stream, "POST", "/containers/"+id+"/wait", url.Values{"condition": {"not-running"}}, nil, "")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var out struct {
		StatusCode int
		Error      *struct{ Message string }
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("docker: wait %s: %w", id, err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return out.StatusCode, errors.New("docker: wait: " + out.Error.Message)
	}
	return out.StatusCode, nil
}

// Logs copies a container's output to stdout and stderr until the container
// stops or ctx ends. With follow false it returns what exists so far. Either
// writer may be nil.
func (c *Client) Logs(ctx context.Context, id string, follow bool, stdout, stderr io.Writer) error {
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "follow": {strconv.FormatBool(follow)}}
	resp, err := c.do(ctx, c.stream, "GET", "/containers/"+id+"/logs", q, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return demux(resp.Body, stdout, stderr)
}

// demux splits the engine's multiplexed stream: each frame has an 8-byte
// header, the stream number in the first byte and the length in the last
// four.
func demux(r io.Reader, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	var head [8]byte
	for {
		if _, err := io.ReadFull(r, head[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		w := stdout
		if head[0] == 2 {
			w = stderr
		}
		if _, err := io.CopyN(w, r, int64(binary.BigEndian.Uint32(head[4:]))); err != nil {
			return err
		}
	}
}

// Result is how a container run ended.
type Result struct {
	ID        string
	ExitCode  int
	TimedOut  bool // killed because the timeout passed
	OOMKilled bool
	ImageID   string
	Started   time.Time
	Ended     time.Time
	// CollectErr says which of Spec.Collect could not be copied out.
	CollectErr error
}

// Run creates a container, starts it, copies its output to stdout and stderr
// as it is produced, waits for it and removes it. If timeout is positive and
// passes first, the container is killed and Result.TimedOut is set. If ctx
// ends first the container is killed and removed and ctx's error returned.
// onStart, if not nil, is called with the container's id once it runs.
func (c *Client) Run(ctx context.Context, s Spec, timeout time.Duration, stdout, stderr io.Writer, onStart func(id string)) (Result, error) {
	var res Result
	id, err := c.Create(ctx, s)
	if err != nil {
		return res, err
	}
	res.ID = id
	// Removal must happen even when ctx is already over.
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c.Remove(rctx, id)
	}()
	if err := c.Start(ctx, id); err != nil {
		return res, err
	}
	res.Started = time.Now()
	if onStart != nil {
		onStart(id)
	}
	logsDone := make(chan error, 1)
	go func() { logsDone <- c.Logs(context.Background(), id, true, stdout, stderr) }()

	type waited struct {
		code int
		err  error
	}
	waitDone := make(chan waited, 1)
	go func() {
		code, err := c.Wait(context.Background(), id)
		waitDone <- waited{code, err}
	}()

	var timer <-chan time.Time
	var t *time.Timer
	if timeout > 0 {
		t = time.NewTimer(timeout)
		defer t.Stop()
		timer = t.C
	}
	var added time.Duration
	kill := func() {
		kctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c.Kill(kctx, id, "")
	}
	var w waited
	var ctxErr error
wait:
	for {
		select {
		case w = <-waitDone:
			break wait
		case <-timer:
			if s.Extra != nil {
				if more := s.Extra() - added; more > 0 {
					added += more
					t.Reset(more)
					continue
				}
			}
			res.TimedOut = true
			kill()
			w = <-waitDone
			break wait
		case <-ctx.Done():
			ctxErr = ctx.Err()
			kill()
			w = <-waitDone
			break wait
		}
	}
	res.Ended = time.Now()
	// The log stream ends when the container does; give it a moment to drain.
	select {
	case <-logsDone:
	case <-time.After(10 * time.Second):
	}
	ictx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if st, image, err := c.Inspect(ictx, id); err == nil {
		res.OOMKilled, res.ImageID = st.OOMKilled, image
	}
	for p, dst := range s.Collect {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		if err := c.CopyOut(cctx, id, p, dst); err != nil {
			res.CollectErr = errors.Join(res.CollectErr, fmt.Errorf("%s: %w", p, err))
		}
		cancel()
	}
	res.ExitCode = w.code
	if ctxErr != nil {
		return res, ctxErr
	}
	return res, w.err
}
