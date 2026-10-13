package legacyimport

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The run sets of the old harness, by directory under the legacy root.
const (
	SetNative    = "native"
	SetAtomic    = "atomic"
	SetTeams     = "teams"
	SetDiscarded = "discarded"
	SetAbandoned = "abandoned"
)

// RunSource is one run directory found under the legacy root.
type RunSource struct {
	// ID is the run's original id. A directory the operator renamed
	// (a "-VOID-..." suffix, an "aborted-" prefix) keeps the id inside its
	// name; the decoration is in Annotation.
	ID         string
	DirName    string
	RelDir     string // slash-separated, relative to the legacy root
	Dir        string
	Set        string // one of the Set constants
	Loop       string // native, atomic or teams; refined from build.txt later
	Annotation string
}

// Discovery is the result of walking the run sets.
type Discovery struct {
	Runs    []RunSource
	Ignored []string // entries in a run set that are not run directories
	// Collisions lists directories whose id was already taken by an earlier
	// directory; they keep their full directory name as id.
	Collisions []string
}

// Discover lists the runs of the three loops and of the abandoned and
// discarded sets, in a stable order. It reads directory names only.
func Discover(legacyRoot string) (*Discovery, error) {
	if fi, err := os.Stat(legacyRoot); err != nil {
		return nil, err
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("legacy root is not a directory")
	}
	type set struct{ dir, kind, loop string }
	sets := []set{
		{"runs-native", SetNative, SetNative},
		{"runs-atomic", SetAtomic, SetAtomic},
		{"runs-teams", SetTeams, SetTeams},
		{"runs-discarded", SetDiscarded, SetNative},
	}
	top, err := os.ReadDir(legacyRoot)
	if err != nil {
		return nil, err
	}
	for _, e := range top {
		if e.IsDir() && strings.HasPrefix(e.Name(), "runs-abandoned") {
			sets = append(sets, set{e.Name(), SetAbandoned, SetNative})
		}
	}
	d := &Discovery{}
	seen := map[string]bool{}
	for _, s := range sets {
		entries, err := os.ReadDir(filepath.Join(legacyRoot, s.dir))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries { // ReadDir sorts by name
			rel := s.dir + "/" + e.Name()
			if !e.IsDir() {
				d.Ignored = append(d.Ignored, rel)
				continue
			}
			id := RunIDPattern.FindString(e.Name())
			if id == "" {
				d.Ignored = append(d.Ignored, rel)
				continue
			}
			ann := strings.Trim(strings.Replace(e.Name(), id, "", 1), "-")
			if seen[id] {
				d.Collisions = append(d.Collisions, rel)
				id = e.Name()
			}
			seen[id] = true
			d.Runs = append(d.Runs, RunSource{
				ID: id, DirName: e.Name(), RelDir: rel,
				Dir: filepath.Join(legacyRoot, s.dir, e.Name()),
				Set: s.kind, Loop: s.loop, Annotation: ann,
			})
		}
	}
	return d, nil
}

// Artefact is one file of a run directory.
type Artefact struct {
	Rel  string // slash-separated, relative to the run directory
	Path string
	Size int64  // bytes on disk
	Kind string // BlobRef kind
	Name string // BlobRef name: Rel without a compression suffix
	// Compression is "gzip", "zstd" or "". A compressed artefact is stored
	// uncompressed, so its blob does not depend on how the old harness aged
	// it out.
	Compression string
	Stream      bool   // an agent event stream
	TX          string // "TX-01" for a per-transaction file of the atomic loop
}

var (
	txFile   = regexp.MustCompile(`^(TX-\d+)\.(.+)$`)
	txState  = regexp.MustCompile(`^state-(TX-\d+)$`)
	exitFile = regexp.MustCompile(`^exit\.txt(\..+)?$`)
)

// ClassifyArtefact maps a file of a run directory, by its relative path, to
// a blob kind and name.
func ClassifyArtefact(rel string) Artefact {
	a := Artefact{Rel: rel, Name: rel}
	switch {
	case strings.HasSuffix(rel, ".gz"):
		a.Compression, a.Name = "gzip", strings.TrimSuffix(rel, ".gz")
	case strings.HasSuffix(rel, ".zst"):
		a.Compression, a.Name = "zstd", strings.TrimSuffix(rel, ".zst")
	}
	parts := strings.Split(rel, "/")
	top := parts[0]
	base := a.Name
	if len(parts) > 1 {
		switch {
		case top == "state":
			a.Kind = "session"
		case txState.MatchString(top):
			a.Kind = "session"
			a.TX = txState.FindStringSubmatch(top)[1]
		case top == "compactions":
			a.Kind = "compaction"
		case top == "tmp":
			a.Kind = "scratch"
		case top == ".tx-base":
			a.Kind = "workspace_before"
		default:
			a.Kind = "other"
		}
		return a
	}
	if m := txFile.FindStringSubmatch(base); m != nil {
		a.TX = m[1]
		switch m[2] {
		case "jsonl":
			a.Kind, a.Stream = "events", true
		case "prompt.txt":
			a.Kind = "prompt"
		case "oracle.txt":
			a.Kind = "oracle"
		case "stderr":
			a.Kind = "runner_stderr"
		case "diff.txt", "diffstat.txt":
			a.Kind = "workspace_diff"
		default:
			a.Kind = "other"
		}
		return a
	}
	switch {
	case base == "run.jsonl":
		a.Kind, a.Stream = "events", true
	case base == "run.stderr":
		a.Kind = "runner_stderr"
	case base == "build.txt":
		a.Kind = "build"
	case base == "prompt.txt":
		a.Kind = "prompt"
	case base == "oracle.txt", base == "logic.txt":
		a.Kind = "oracle"
	case exitFile.MatchString(base):
		a.Kind = "exit"
	case base == "abort-reason.txt":
		a.Kind = "abort_reason"
	case base == "transactions.txt":
		a.Kind = "transactions"
	case base == "checks.txt":
		a.Kind = "checks"
	case base == "escalations.txt":
		a.Kind = "escalations"
	case base == "delegation.txt":
		a.Kind = "delegation"
	case base == "ollama.log", base == "server.log", base == "server-log.offset":
		a.Kind = "server_log"
	case base == "auth.log":
		a.Kind = "runtime_log"
	case strings.HasSuffix(base, ".before.html"), base == ".tx-base.manifest":
		a.Kind = "workspace_before"
	case strings.HasSuffix(base, ".after.html"):
		a.Kind = "workspace_after"
	case base == "tmp-scratch.tar":
		a.Kind = "scratch"
	case base == "NOTES.txt", strings.HasPrefix(base, "VOID"):
		a.Kind = "note"
	default:
		a.Kind = "other"
	}
	return a
}

// ListArtefacts walks a run directory and returns its regular files, sorted
// by relative path, with a fingerprint of the listing (paths, sizes and
// modification times). Symbolic links are not followed; they are returned in
// skipped.
func ListArtefacts(runDir string) (arts []Artefact, fingerprint string, skipped []string, err error) {
	h := sha256.New()
	err = filepath.WalkDir(runDir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(runDir, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if !d.Type().IsRegular() {
			skipped = append(skipped, rel)
			return nil
		}
		fi, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		a := ClassifyArtefact(rel)
		a.Path, a.Size = p, fi.Size()
		arts = append(arts, a)
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", rel, fi.Size(), fi.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return nil, "", nil, err
	}
	sort.Slice(arts, func(i, j int) bool { return arts[i].Rel < arts[j].Rel })
	return arts, hex.EncodeToString(h.Sum(nil)), skipped, nil
}

// ProviderSettings is what the CLI's settings file in a session state
// directory says about the provider entry the run used.
type ProviderSettings struct {
	Provider           string          `json:"provider,omitempty"`
	Model              string          `json:"model,omitempty"`
	BaseURL            string          `json:"base_url,omitempty"`
	ContextWindow      int64           `json:"context_window,omitempty"`
	MaxToolResultChars int64           `json:"max_tool_result_chars,omitempty"`
	Sampling           json.RawMessage `json:"sampling,omitempty"` // nil when the file sets none
	OutputBudget       json.RawMessage `json:"output_budget,omitempty"`
}

// ParseProviderSettings parses a settings/providers.json. The API key in the
// file is never read into the result.
func ParseProviderSettings(content []byte) (*ProviderSettings, error) {
	var raw struct {
		LastUsedProvider string `json:"lastUsedProvider"`
		Providers        map[string]struct {
			Settings struct {
				Provider           string          `json:"provider"`
				Model              string          `json:"model"`
				BaseURL            string          `json:"baseUrl"`
				ContextWindow      int64           `json:"contextWindow"`
				MaxToolResultChars int64           `json:"maxToolResultChars"`
				Sampling           json.RawMessage `json:"sampling"`
				OutputBudget       json.RawMessage `json:"outputBudget"`
			} `json:"settings"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("provider settings: %w", err)
	}
	name := raw.LastUsedProvider
	if _, ok := raw.Providers[name]; !ok {
		if len(raw.Providers) != 1 {
			return nil, errors.New("provider settings: cannot tell which provider entry was used")
		}
		for k := range raw.Providers {
			name = k
		}
	}
	s := raw.Providers[name].Settings
	p := &ProviderSettings{
		Provider: s.Provider, Model: s.Model, BaseURL: s.BaseURL,
		ContextWindow: s.ContextWindow, MaxToolResultChars: s.MaxToolResultChars,
		Sampling: s.Sampling, OutputBudget: s.OutputBudget,
	}
	if p.Provider == "" {
		p.Provider = name
	}
	return p, nil
}

// StateTar is what a session state archive contains.
type StateTar struct {
	Entries  int   `json:"entries"` // regular files
	Bytes    int64 `json:"bytes"`   // their total size
	Sessions int   `json:"sessions"`
	HasLog   bool  `json:"has_log"`
	Settings *ProviderSettings
}

// ScanStateTar reads an uncompressed tar of a session state directory to its
// end. It keeps nothing but the provider settings file.
func ScanStateTar(r io.Reader) (*StateTar, error) {
	st := &StateTar{}
	sessions := map[string]bool{}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return st, fmt.Errorf("state tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(hdr.Name)
		st.Entries++
		st.Bytes += hdr.Size
		if i := strings.Index(name, "sessions/"); i >= 0 {
			if rest := strings.Split(name[i+len("sessions/"):], "/"); len(rest) > 1 {
				sessions[rest[0]] = true
			}
		}
		if strings.HasSuffix(name, "logs/cline.log") {
			st.HasLog = true
		}
		if strings.HasSuffix(name, "settings/providers.json") && hdr.Size <= maxTextFile {
			b, err := io.ReadAll(tr)
			if err != nil {
				return st, fmt.Errorf("state tar: %w", err)
			}
			if p, err := ParseProviderSettings(b); err == nil {
				st.Settings = p
			}
		}
	}
	st.Sessions = len(sessions)
	return st, nil
}

// StateArchive locates the archived session state of runs: one compressed
// tar per run, named after the run directory.
type StateArchive struct {
	byName map[string]archiveFile
}

type archiveFile struct {
	path, rel, compression string
	size                   int64
}

// OpenStateArchive indexes the archive directories. dirs may contain empty
// strings and directories that do not exist. A directory may hold the tars
// directly or in a "state" subdirectory.
func OpenStateArchive(dirs ...string) (*StateArchive, error) {
	sa := &StateArchive{byName: map[string]archiveFile{}}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		for _, sub := range []string{"", "state"} {
			entries, err := os.ReadDir(filepath.Join(dir, sub))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !e.Type().IsRegular() {
					continue
				}
				name := e.Name()
				var key, comp string
				switch {
				case strings.HasSuffix(name, ".tar.zst"):
					key, comp = strings.TrimSuffix(name, ".tar.zst"), "zstd"
				case strings.HasSuffix(name, "-state.tar.gz"):
					key, comp = strings.TrimSuffix(name, "-state.tar.gz"), "gzip"
				case strings.HasSuffix(name, ".tar.gz"):
					key, comp = strings.TrimSuffix(name, ".tar.gz"), "gzip"
				default:
					continue
				}
				if _, dup := sa.byName[key]; dup {
					continue // the first directory given wins
				}
				fi, err := e.Info()
				if err != nil {
					return nil, err
				}
				sa.byName[key] = archiveFile{
					path: filepath.Join(dir, sub, name), rel: path.Join(sub, name),
					compression: comp, size: fi.Size(),
				}
			}
		}
	}
	return sa, nil
}

// Len returns the number of archives indexed.
func (sa *StateArchive) Len() int { return len(sa.byName) }

// find returns the archive of a run: by directory name first, then by id.
func (sa *StateArchive) find(src RunSource) (archiveFile, bool) {
	if sa == nil {
		return archiveFile{}, false
	}
	for _, key := range []string{src.DirName, src.ID} {
		if f, ok := sa.byName[key]; ok {
			return f, true
		}
	}
	return archiveFile{}, false
}

// orphans returns the archives no discovered run matched.
func (sa *StateArchive) orphans(runs []RunSource) []string {
	matched := map[string]bool{}
	for _, r := range runs {
		for _, key := range []string{r.DirName, r.ID} {
			if _, ok := sa.byName[key]; ok {
				matched[key] = true
				break
			}
		}
	}
	var out []string
	for key, f := range sa.byName {
		if !matched[key] {
			out = append(out, f.rel)
		}
	}
	sort.Strings(out)
	return out
}
