package legacyimport

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"

	"github.com/mann1x/automaticum/internal/store/blob"
)

// Options configures an import.
type Options struct {
	// LegacyRoot is the old harness's directory. It is only read.
	LegacyRoot string
	// StateArchive is the external directory of archived session state, one
	// compressed tar per run. Optional. The state-archive directory inside
	// the legacy root is always consulted as well.
	StateArchive string

	// Sink receives the import. It may be nil in a dry run.
	Sink Sink
	// DryRun reads and maps everything and writes nothing.
	DryRun bool
	// MetadataOnly, valid only with DryRun, skips the event streams and the
	// state archives: verdicts and build fields are mapped, tool calls and
	// stream-derived fields are not.
	MetadataOnly bool

	// Sets limits the import to some run sets (the Set constants). Empty
	// means all.
	Sets []string
	// RunIDs limits the import to some runs. Empty means all.
	RunIDs []string

	// Workers is the number of runs processed at once. Zero means one.
	Workers int
	// MaxLine bounds one line of an event stream. Zero means DefaultMaxLine.
	MaxLine int

	// TaskV2From and TaskV3From are the first runs of task versions 2 and 3.
	// Empty means the defaults of spec 100 section 3.
	TaskV2From string
	TaskV3From string

	// Progress, when set, is called after each run, from one goroutine at a
	// time.
	Progress func(RunSummary)
}

// Run statuses in a report.
const (
	StatusImported = "imported"
	StatusDryRun   = "dry-run"
	StatusSkipped  = "skipped"
	StatusFailed   = "failed"
)

// RunSummary is one run in a report.
type RunSummary struct {
	ID              string
	Loop            string
	Set             string
	RelDir          string
	Status          string
	Finished        bool // the old harness wrote an exit.txt
	OriginalVerdict string
	Verdict         Verdict
	PriorVerdict    Verdict
	TaskVersion     int
	Missing         []string
	ToolCalls       int
	Blobs           int
	StateOnDisk     bool // a session state directory is still in the run directory
	StateArchived   bool // an archived session state tar exists for the run
}

// Counts are the per-loop and per-set counts of a report.
type Counts struct {
	Dirs     int // run directories found
	Finished int // of which the old harness wrote an exit.txt
	Imported int // imported, or would be in a dry run
	Skipped  int
	Failed   int
}

// Skip records a run that was not imported, and why.
type Skip struct {
	ID     string
	RelDir string
	Reason string
}

// Report is the outcome of an import.
type Report struct {
	ImporterVersion string
	DryRun          bool
	MetadataOnly    bool

	Loops map[string]*Counts // by loop: native, atomic, teams
	Sets  map[string]*Counts // by run set: the loops plus discarded and abandoned

	// OriginalVerdicts counts the old harness's verdict words per loop;
	// the key "" counts runs that have none. Verdicts counts the mapped
	// verdicts per loop. Failed runs are in neither.
	OriginalVerdicts map[string]map[string]int
	Verdicts         map[string]map[string]int

	Runs    []RunSummary
	Skipped []Skip // already imported with the same fingerprint, or filtered by collision
	Failed  []Skip // bad runs: the import went on without them

	// Warnings are problems that did not stop a run: an unreadable
	// artefact, a compressed file that would not decompress.
	Warnings []string
	// Inconsistencies are places where the corpus disagrees with itself or
	// with the boundaries the import was given.
	Inconsistencies []string

	BytesRead   int64 // bytes read from the legacy paths
	StreamBytes int64 // uncompressed bytes of event streams scanned
	Files       int64 // artefact files seen
	Blobs       int64 // blobs written
	ToolCalls   int64

	// UnknownFields counts, per field, the runs where it could not be
	// recovered.
	UnknownFields map[string]int
	// EventTypes counts stream lines by "<line type>/<inner type or hook>".
	EventTypes map[string]int64

	Boundaries   []Boundary
	InvalidNotes []InvalidNote

	StateOnDisk        int // runs with a state directory still on disk
	StateArchived      int // runs with an archived state tar
	StateArchiveFiles  int // archives indexed
	OrphanStateArchive []string
	Ignored            []string // entries of a run set that are not runs
	IDCollisions       []string
}

type importer struct {
	o       Options
	archive *StateArchive
	invalid map[string]InvalidNote
	lines   map[string]map[string]*VerdictLine // loop -> run id -> verdict line
	changes []Boundary

	mu      sync.Mutex // guards rep, results and bytesIn
	rep     *Report
	results map[string]RunSummary
	bytesIn int64
	// sinkMu makes the sink's metadata calls one at a time, and the four
	// calls of one run contiguous.
	sinkMu sync.Mutex
}

// verdictFiles names the verdict file of each loop, relative to the root.
var verdictFiles = map[string]string{
	SetNative: "verdicts-native.txt",
	SetAtomic: "verdicts-atomic.txt",
	SetTeams:  "verdicts-teams.txt",
}

// Import imports the old harness's corpus into o.Sink.
//
// It goes on past a run it cannot import and records it in Report.Failed.
// It returns an error only when it cannot start (no legacy root, no sink)
// or when ctx is cancelled; the report is valid in both cases as far as the
// import got.
func Import(ctx context.Context, o Options) (*Report, error) {
	if o.LegacyRoot == "" {
		return nil, errors.New("legacyimport: no legacy root")
	}
	if !o.DryRun && o.Sink == nil {
		return nil, errors.New("legacyimport: no sink and not a dry run")
	}
	if o.MetadataOnly && !o.DryRun {
		return nil, errors.New("legacyimport: MetadataOnly is only valid in a dry run")
	}
	if o.TaskV2From == "" {
		o.TaskV2From = DefaultTaskV2From
	}
	if o.TaskV3From == "" {
		o.TaskV3From = DefaultTaskV3From
	}
	if o.MaxLine <= 0 {
		o.MaxLine = DefaultMaxLine
	}
	if o.Workers <= 0 {
		o.Workers = 1
	}

	disc, err := Discover(o.LegacyRoot)
	if err != nil {
		return nil, fmt.Errorf("legacyimport: %w", err)
	}
	im := &importer{
		o:       o,
		invalid: map[string]InvalidNote{},
		lines:   map[string]map[string]*VerdictLine{},
		results: map[string]RunSummary{},
		rep: &Report{
			ImporterVersion:  ImporterVersion,
			DryRun:           o.DryRun,
			MetadataOnly:     o.MetadataOnly,
			Loops:            map[string]*Counts{},
			Sets:             map[string]*Counts{},
			OriginalVerdicts: map[string]map[string]int{},
			Verdicts:         map[string]map[string]int{},
			UnknownFields:    map[string]int{},
			EventTypes:       map[string]int64{},
			Ignored:          disc.Ignored,
			IDCollisions:     disc.Collisions,
		},
	}
	if err := im.loadContext(); err != nil {
		return nil, fmt.Errorf("legacyimport: %w", err)
	}
	im.archive, err = OpenStateArchive(o.StateArchive, filepath.Join(o.LegacyRoot, "state-archive"))
	if err != nil {
		return nil, fmt.Errorf("legacyimport: state archive: %w", err)
	}
	im.rep.StateArchiveFiles = im.archive.Len()
	im.rep.OrphanStateArchive = im.archive.orphans(disc.Runs)

	runs := filterRuns(disc.Runs, o)
	jobs := make(chan RunSource)
	var wg sync.WaitGroup
	for w := 0; w < o.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for src := range jobs {
				im.one(ctx, src)
			}
		}()
	}
	var ctxErr error
feed:
	for _, src := range runs {
		select {
		case <-ctx.Done():
			ctxErr = ctx.Err()
			break feed
		case jobs <- src:
		}
	}
	close(jobs)
	wg.Wait()

	// Report runs in discovery order, whatever order the workers finished in.
	for _, src := range runs {
		if s, ok := im.results[src.RelDir]; ok {
			im.rep.Runs = append(im.rep.Runs, s)
		}
	}
	sort.Strings(im.rep.Warnings)
	sort.Strings(im.rep.Inconsistencies)
	sort.Slice(im.rep.Failed, func(i, j int) bool { return im.rep.Failed[i].RelDir < im.rep.Failed[j].RelDir })
	sort.Slice(im.rep.Skipped, func(i, j int) bool { return im.rep.Skipped[i].RelDir < im.rep.Skipped[j].RelDir })
	im.rep.BytesRead = im.bytesIn
	return im.rep, ctxErr
}

func filterRuns(all []RunSource, o Options) []RunSource {
	if len(o.Sets) == 0 && len(o.RunIDs) == 0 {
		return all
	}
	sets, ids := map[string]bool{}, map[string]bool{}
	for _, s := range o.Sets {
		sets[s] = true
	}
	for _, id := range o.RunIDs {
		ids[id] = true
	}
	var out []RunSource
	for _, r := range all {
		if (len(sets) == 0 || sets[r.Set]) && (len(ids) == 0 || ids[r.ID]) {
			out = append(out, r)
		}
	}
	return out
}

// loadContext reads what applies to the whole corpus: the verdict files,
// the notes that void runs, and the change logs.
func (im *importer) loadContext() error {
	root := im.o.LegacyRoot
	for loop, name := range verdictFiles {
		f, err := os.Open(filepath.Join(root, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		vf, err := ParseVerdictFile(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		byID := map[string]*VerdictLine{}
		for i := range vf.Runs {
			l := &vf.Runs[i]
			if _, dup := byID[l.RunID]; dup {
				im.rep.Inconsistencies = append(im.rep.Inconsistencies,
					fmt.Sprintf("%s: run %s has more than one line; the last one is used", name, l.RunID))
			}
			byID[l.RunID] = l
		}
		im.lines[loop] = byID
		for _, n := range vf.Notes {
			if strings.Contains(strings.ToUpper(n.Text), "VOID") {
				im.addInvalid(InvalidNote{RunID: n.RunID, Reason: n.Text, Source: name})
			}
		}
	}
	if b, err := readSmall(filepath.Join(root, "INVALID-RUNS.md")); err == nil {
		for _, n := range ParseInvalidRuns(string(b)) {
			n.Source = "INVALID-RUNS.md"
			im.addInvalid(n)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	im.rep.Boundaries = append(im.rep.Boundaries,
		Boundary{Kind: "task_version", From: im.o.TaskV2From, TaskVersion: 2, Source: "spec 100 section 3"},
		Boundary{Kind: "task_version", From: im.o.TaskV3From, TaskVersion: 3, Source: "spec 100 section 3"},
	)
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "changes-") {
			continue
		}
		rel := e.Name() + "/CHANGES.md"
		b, err := readSmall(filepath.Join(root, e.Name(), "CHANGES.md"))
		if err != nil {
			continue
		}
		from := ParseChangeBoundary(string(b))
		if from == "" {
			// The log names no boundary run: the day is the boundary. As
			// text it sorts before every run id of that day.
			from = strings.TrimPrefix(e.Name(), "changes-")
		}
		bd := Boundary{Kind: "harness_change", From: from, Source: rel}
		im.rep.Boundaries = append(im.rep.Boundaries, bd)
		im.changes = append(im.changes, bd)
	}
	return nil
}

func (im *importer) addInvalid(n InvalidNote) {
	if old, ok := im.invalid[n.RunID]; ok {
		n.Reason = old.Reason + "; " + n.Reason
		n.Source = old.Source + ", " + n.Source
	}
	im.invalid[n.RunID] = n
	im.rep.InvalidNotes = append(im.rep.InvalidNotes, n)
}

// one imports one run and records the outcome. It never returns an error:
// a bad run is recorded and the import goes on.
func (im *importer) one(ctx context.Context, src RunSource) {
	sum, bytesIn, streamBytes, files, blobs, stats, warns, incons, skip, err := im.process(ctx, src)

	im.mu.Lock()
	defer im.mu.Unlock()
	rep := im.rep
	im.bytesIn += bytesIn
	rep.StreamBytes += streamBytes
	rep.Files += files
	rep.Blobs += blobs
	rep.ToolCalls += int64(sum.ToolCalls)
	rep.Warnings = append(rep.Warnings, warns...)
	rep.Inconsistencies = append(rep.Inconsistencies, incons...)
	if stats != nil {
		for k, n := range stats.TopLevel {
			switch k {
			case "agent_event", "hook_event":
			default:
				rep.EventTypes[k] += n
			}
		}
		for k, n := range stats.Inner {
			rep.EventTypes["agent_event/"+k] += n
		}
		for k, n := range stats.Hooks {
			rep.EventTypes["hook_event/"+k] += n
		}
	}

	loop, set := counts(rep.Loops, sum.Loop), counts(rep.Sets, sum.Set)
	loop.Dirs++
	set.Dirs++
	if sum.Finished {
		loop.Finished++
		set.Finished++
	}
	switch {
	case err != nil:
		sum.Status = StatusFailed
		loop.Failed++
		set.Failed++
		rep.Failed = append(rep.Failed, Skip{ID: src.ID, RelDir: src.RelDir, Reason: err.Error()})
	case skip != "":
		sum.Status = StatusSkipped
		loop.Skipped++
		set.Skipped++
		rep.Skipped = append(rep.Skipped, Skip{ID: src.ID, RelDir: src.RelDir, Reason: skip})
	default:
		sum.Status = StatusImported
		if im.o.DryRun {
			sum.Status = StatusDryRun
		}
		loop.Imported++
		set.Imported++
	}
	if err == nil {
		if sum.StateOnDisk {
			rep.StateOnDisk++
		}
		if sum.StateArchived {
			rep.StateArchived++
		}
		bump(rep.OriginalVerdicts, sum.Loop, sum.OriginalVerdict)
		bump(rep.Verdicts, sum.Loop, string(sum.Verdict))
		for _, f := range sum.Missing {
			rep.UnknownFields[f]++
		}
	}
	im.results[src.RelDir] = sum
	if im.o.Progress != nil {
		im.o.Progress(sum)
	}
}

func counts(m map[string]*Counts, key string) *Counts {
	c, ok := m[key]
	if !ok {
		c = &Counts{}
		m[key] = c
	}
	return c
}

func bump(m map[string]map[string]int, a, b string) {
	if m[a] == nil {
		m[a] = map[string]int{}
	}
	m[a][b]++
}

// countingReader counts what is read from a legacy file and remembers a
// read error, so a failure of the source can be told from one of the sink.
type countingReader struct {
	r   io.Reader
	n   *int64
	err error
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	*c.n += int64(n)
	if err != nil && err != io.EOF {
		c.err = err
	}
	return n, err
}

// source is an opened legacy file, decompressed when asked.
type source struct {
	f    *os.File
	raw  *countingReader
	r    io.Reader
	zr   *zstd.Decoder
	derr error // a decompression error seen while reading
}

func (s *source) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && err != io.EOF {
		s.derr = err
	}
	return n, err
}

func (s *source) Close() {
	if s.zr != nil {
		s.zr.Close()
	}
	s.f.Close()
}

// failed reports an error of the source itself (unreadable or corrupt).
func (s *source) failed() error {
	if s.raw.err != nil {
		return s.raw.err
	}
	return s.derr
}

// openSource opens a legacy file read-only.
func openSource(path, compression string, counter *int64) (*source, error) {
	f, err := os.Open(path) // O_RDONLY: nothing under the legacy paths is ever written
	if err != nil {
		return nil, err
	}
	s := &source{f: f, raw: &countingReader{r: f, n: counter}}
	switch compression {
	case "gzip":
		zr, err := gzip.NewReader(s.raw)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("gzip: %w", err)
		}
		s.r = zr
	case "zstd":
		zr, err := zstd.NewReader(s.raw, zstd.WithDecoderConcurrency(1))
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("zstd: %w", err)
		}
		s.zr, s.r = zr, zr
	default:
		s.r = s.raw
	}
	return s, nil
}

func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxTextFile))
}

// process does the work for one run.
func (im *importer) process(ctx context.Context, src RunSource) (
	sum RunSummary, bytesIn, streamBytes, files, blobs int64,
	stats *StreamStats, warns, incons []string, skip string, err error,
) {
	sum = RunSummary{ID: src.ID, Loop: src.Loop, Set: src.Set, RelDir: src.RelDir}
	warn := func(format string, a ...any) {
		warns = append(warns, src.RelDir+": "+fmt.Sprintf(format, a...))
	}

	arts, listing, skippedLinks, lerr := ListArtefacts(src.Dir)
	if lerr != nil {
		return sum, 0, 0, 0, 0, nil, warns, nil, "", fmt.Errorf("list: %w", lerr)
	}
	for _, l := range skippedLinks {
		warn("%s is not a regular file and was not imported", l)
	}
	files = int64(len(arts))

	// Phase A: the small text files. Cheap, and enough for the verdict.
	m := &runMeta{files: len(arts), txs: map[string]*txMeta{}}
	text := func(a Artefact) (string, bool) {
		b, rerr := readSmall(a.Path)
		if rerr != nil {
			warn("%s: %v", a.Rel, rerr)
			return "", false
		}
		bytesIn += int64(len(b))
		return string(b), true
	}
	tx := func(name string) *txMeta {
		t, ok := m.txs[name]
		if !ok {
			t = &txMeta{}
			m.txs[name] = t
		}
		return t
	}
	var exitErr error
	stateDirs := map[string]bool{}
	for _, a := range arts {
		m.bytesOnDisk += a.Size
		if a.Kind == "session" {
			stateDirs[strings.SplitN(a.Rel, "/", 2)[0]] = true
			if a.TX != "" {
				tx(a.TX).HasState = true
			}
			if strings.HasSuffix(a.Rel, "settings/providers.json") && m.settings == nil {
				if s, ok := text(a); ok {
					if p, perr := ParseProviderSettings([]byte(s)); perr == nil {
						m.settings = p
					} else {
						warn("%s: %v", a.Rel, perr)
					}
				}
			}
			continue
		}
		if a.TX != "" {
			t := tx(a.TX)
			switch {
			case a.Stream:
				t.HasStream = true
			case a.Kind == "prompt":
				t.HasPrompt = true
			case a.Kind == "oracle":
				if s, ok := text(a); ok {
					t.Oracle = ParseOracle(s)
				}
			case strings.HasSuffix(a.Name, ".diffstat.txt"):
				if s, ok := text(a); ok {
					t.Diffstat = truncate(strings.TrimSpace(s), 400)
				}
			}
			continue
		}
		switch a.Rel {
		case "build.txt":
			if s, ok := text(a); ok {
				b, berr := ParseBuild(s)
				if berr != nil {
					warn("build.txt: %v", berr)
				}
				m.build = b
			}
		case "exit.txt":
			if s, ok := text(a); ok {
				m.hasExit = true
				m.exit, exitErr = ParseExit(s)
			}
		case "oracle.txt":
			if s, ok := text(a); ok {
				m.oracle, m.hasOracle = ParseOracle(s), true
			}
		case "logic.txt":
			if s, ok := text(a); ok {
				m.logic = strings.TrimSpace(s)
			}
		case "transactions.txt":
			if s, ok := text(a); ok {
				m.tx = ParseTransactions(s)
			}
		case "checks.txt":
			if s, ok := text(a); ok {
				m.checks, _ = ParseSummaryFile(s)
			}
		case "escalations.txt":
			if s, ok := text(a); ok {
				m.esc, _ = ParseSummaryFile(s)
			}
		case "delegation.txt":
			if s, ok := text(a); ok {
				m.deleg, m.delegBy = ParseSummaryFile(s)
			}
		case "abort-reason.txt":
			if s, ok := text(a); ok {
				m.abortNote = strings.TrimSpace(s)
			}
		default:
			if a.Kind == "note" {
				if s, ok := text(a); ok {
					m.notes = append(m.notes, a.Rel+": "+truncate(strings.TrimSpace(s), 2000))
				}
			}
		}
	}
	m.stateDirs = len(stateDirs)
	sum.Finished = m.hasExit
	if m.build != nil {
		switch mode := m.build.Fields["mode"]; mode {
		case SetNative, SetAtomic, SetTeams:
			src.Loop = mode
			sum.Loop = mode
		}
	}
	if exitErr != nil {
		return sum, bytesIn, 0, files, 0, nil, warns, nil, "", exitErr
	}

	if byID := im.lines[src.Loop]; byID != nil {
		m.verdictLine = byID[src.ID]
	}
	if n, ok := im.invalid[src.ID]; ok {
		n := n
		m.invalid = &n
	}
	if strings.Contains(strings.ToUpper(src.Annotation), "VOID") {
		reason := "directory marked " + src.Annotation + " by the operator"
		if len(m.notes) > 0 {
			reason += ": " + strings.Join(m.notes, "; ")
		}
		if m.invalid != nil {
			reason = m.invalid.Reason + "; " + reason
		}
		m.invalid = &InvalidNote{RunID: src.ID, Reason: reason, Source: src.RelDir}
	}

	arch, hasArch := im.archive.find(src)
	m.hasArchive, m.archiveRel = hasArch, arch.rel
	sum.StateOnDisk, sum.StateArchived = m.stateDirs > 0, hasArch

	// The fingerprint covers everything the mapping reads: the listing of
	// the run directory, the archive, the notes and the verdict line.
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s\n%s\n%d\n", ImporterVersion, src.RelDir, listing, arch.rel, arch.size)
	if m.verdictLine != nil {
		fmt.Fprintln(h, m.verdictLine.Raw)
	}
	if m.invalid != nil {
		fmt.Fprintln(h, m.invalid.Reason)
	}
	fmt.Fprintln(h, im.o.TaskV2From, im.o.TaskV3From)
	m.fingerprint = hex.EncodeToString(h.Sum(nil))

	// The verdict is known from here on, also for a run that will be skipped.
	v, original, _, prior, _, verr := verdictOf(m)
	if verr != nil {
		return sum, bytesIn, 0, files, 0, nil, warns, nil, "", verr
	}
	sum.Verdict, sum.OriginalVerdict, sum.PriorVerdict = v, original, prior
	sum.TaskVersion = TaskVersionFor(src.ID, im.o.TaskV2From, im.o.TaskV3From)
	incons = im.crossCheck(src, m, sum.TaskVersion)

	if !im.o.DryRun {
		if res, ok := im.o.Sink.(Resumer); ok {
			fp, found, rerr := res.ImportedFingerprint(ctx, src.ID)
			if rerr != nil {
				return sum, bytesIn, 0, files, 0, nil, warns, incons, "", fmt.Errorf("sink: %w", rerr)
			}
			if found && fp == m.fingerprint {
				return sum, bytesIn, 0, files, 0, nil, warns, incons, "already imported with the same source fingerprint", nil
			}
		}
	}

	// Phase B: the streams, the state and the blobs.
	st := stateFacts{onDisk: m.stateDirs > 0, settings: m.settings}
	var refs []BlobRef
	var calls []ToolCall
	if !im.o.MetadataOnly {
		sc := NewStreamScanner()
		sc.MaxLine = im.o.MaxLine
		for _, a := range arts {
			if cerr := ctx.Err(); cerr != nil {
				return sum, bytesIn, streamBytes, files, blobs, nil, warns, incons, "", cerr
			}
			ref, werr, serr := im.artefact(ctx, a, sc, &bytesIn)
			if serr != nil {
				return sum, bytesIn, streamBytes, files, blobs, nil, warns, incons, "", fmt.Errorf("sink: %s: %w", a.Rel, serr)
			}
			if werr != nil {
				warn("%s: %v", a.Rel, werr)
				m.warnings = append(m.warnings, a.Rel+": "+werr.Error())
			}
			if ref != nil {
				ref.RunID = src.ID
				refs = append(refs, *ref)
			}
		}
		if hasArch {
			info, ref, werr, serr := im.stateArchive(ctx, arch, &bytesIn)
			if serr != nil {
				return sum, bytesIn, streamBytes, files, blobs, nil, warns, incons, "", fmt.Errorf("sink: %s: %w", arch.rel, serr)
			}
			if werr != nil {
				warn("state archive %s: %v", arch.rel, werr)
				m.warnings = append(m.warnings, "state archive: "+werr.Error())
			}
			st.archive = info
			if st.settings == nil && info != nil {
				st.settings = info.Settings
			}
			if ref != nil {
				ref.RunID = src.ID
				refs = append(refs, *ref)
			}
		}
		s := sc.Stats()
		stats = &s
		streamBytes = s.Bytes
		calls = sc.ToolCalls()
		for i := range calls {
			calls[i].RunID = src.ID
		}
		if s.BadLines > 0 || s.OversizeLines > 0 {
			warn("event stream: %d lines were not JSON, %d were over the line limit", s.BadLines, s.OversizeLines)
		}
	}
	blobs = int64(len(refs))
	sum.ToolCalls, sum.Blobs = len(calls), len(refs)

	run, berr := buildRun(src, m, stats, st, len(calls), &im.o, im.changes)
	if berr != nil {
		return sum, bytesIn, streamBytes, files, blobs, stats, warns, incons, "", berr
	}
	sum.Missing = run.Missing

	if im.o.DryRun {
		return sum, bytesIn, streamBytes, files, 0, stats, warns, incons, "", nil
	}

	// Phase C: the rows. The run goes in first without the completion mark,
	// then what hangs off it, then the mark: a pass that died in between
	// is redone, not skipped.
	im.sinkMu.Lock()
	defer im.sinkMu.Unlock()
	if err := im.o.Sink.UpsertRun(ctx, run); err != nil {
		return sum, bytesIn, streamBytes, files, blobs, stats, warns, incons, "", fmt.Errorf("sink: upsert run: %w", err)
	}
	if err := im.o.Sink.AddBlobRefs(ctx, refs); err != nil {
		return sum, bytesIn, streamBytes, files, blobs, stats, warns, incons, "", fmt.Errorf("sink: blob refs: %w", err)
	}
	if err := im.o.Sink.ReplaceToolCalls(ctx, src.ID, calls); err != nil {
		return sum, bytesIn, streamBytes, files, blobs, stats, warns, incons, "", fmt.Errorf("sink: tool calls: %w", err)
	}
	if err := markComplete(run); err != nil {
		return sum, bytesIn, streamBytes, files, blobs, stats, warns, incons, "", err
	}
	if err := im.o.Sink.UpsertRun(ctx, run); err != nil {
		return sum, bytesIn, streamBytes, files, blobs, stats, warns, incons, "", fmt.Errorf("sink: upsert run: %w", err)
	}
	return sum, bytesIn, streamBytes, files, blobs, stats, warns, incons, "", nil
}

// artefact reads one artefact. An event stream is scanned on the way. In a
// dry run only event streams are read. warnErr is a problem with the
// source; sinkErr is a failure of the sink, which fails the run.
func (im *importer) artefact(ctx context.Context, a Artefact, sc *StreamScanner, bytesIn *int64) (ref *BlobRef, warnErr, sinkErr error) {
	if im.o.DryRun && !a.Stream {
		return nil, nil, nil
	}
	s, err := openSource(a.Path, a.Compression, bytesIn)
	if err != nil {
		if a.Compression == "" {
			return nil, err, nil
		}
		// Not a valid compressed file: keep the bytes as they are.
		ref, w, se := im.rawBlob(ctx, a, bytesIn)
		if w == nil {
			w = fmt.Errorf("%v; stored as written", err)
		}
		return ref, w, se
	}
	defer s.Close()

	var r io.Reader = s
	if a.Stream {
		defer sc.EndStream()
		if im.o.DryRun {
			_, cerr := io.Copy(sc, r)
			return nil, cerr, nil
		}
		r = io.TeeReader(r, sc)
	}
	hash, size, perr := im.o.Sink.PutBlob(ctx, r)
	if perr != nil {
		if ferr := s.failed(); ferr != nil {
			if a.Compression != "" {
				ref, w, se := im.rawBlob(ctx, a, bytesIn)
				if w == nil {
					w = fmt.Errorf("%v; stored as written", ferr)
				}
				return ref, w, se
			}
			return nil, ferr, nil
		}
		return nil, nil, perr
	}
	return &BlobRef{Kind: a.Kind, Name: a.Name, Hash: blob.Hash(hash), Size: size}, nil, nil
}

// rawBlob stores a compressed artefact without decompressing it, under its
// name on disk.
func (im *importer) rawBlob(ctx context.Context, a Artefact, bytesIn *int64) (*BlobRef, error, error) {
	if im.o.DryRun {
		return nil, nil, nil
	}
	s, err := openSource(a.Path, "", bytesIn)
	if err != nil {
		return nil, err, nil
	}
	defer s.Close()
	hash, size, perr := im.o.Sink.PutBlob(ctx, s)
	if perr != nil {
		if ferr := s.failed(); ferr != nil {
			return nil, ferr, nil
		}
		return nil, nil, perr
	}
	return &BlobRef{Kind: a.Kind, Name: a.Rel, Hash: blob.Hash(hash), Size: size}, nil, nil
}

// stateArchive scans an archived session state tar and, outside a dry run,
// stores it uncompressed.
func (im *importer) stateArchive(ctx context.Context, f archiveFile, bytesIn *int64) (info *StateTar, ref *BlobRef, warnErr, sinkErr error) {
	s, err := openSource(f.path, f.compression, bytesIn)
	if err != nil {
		return nil, nil, err, nil
	}
	defer s.Close()
	if im.o.DryRun {
		info, err = ScanStateTar(s)
		return info, nil, err, nil
	}
	pr, pw := io.Pipe()
	done := make(chan struct{})
	var scanErr error
	go func() {
		defer close(done)
		info, scanErr = ScanStateTar(pr)
		// The tar reader stops at the end-of-archive marker; whatever
		// follows must still be drained or the writer would block.
		_, _ = io.Copy(io.Discard, pr)
	}()
	hash, size, perr := im.o.Sink.PutBlob(ctx, io.TeeReader(s, pw))
	pw.CloseWithError(perr)
	<-done
	if perr != nil {
		if ferr := s.failed(); ferr != nil {
			return info, nil, ferr, nil
		}
		return info, nil, nil, perr
	}
	return info, &BlobRef{Kind: "session_archive", Name: "state.tar", Hash: blob.Hash(hash), Size: size}, scanErr, nil
}

// crossCheck compares what a run's own files say with the boundaries the
// import applies and with the verdict file.
func (im *importer) crossCheck(src RunSource, m *runMeta, version int) []string {
	var out []string
	say := func(format string, a ...any) {
		out = append(out, src.RelDir+": "+fmt.Sprintf(format, a...))
	}
	if m.hasExit && m.verdictLine != nil && m.exit.Verdict != m.verdictLine.Verdict {
		say("exit.txt says %s, the verdict file says %s", m.exit.Verdict, m.verdictLine.Verdict)
	}
	if src.Loop == SetNative && m.build != nil {
		v3 := m.build.Fields["oracle_version"] == "v3"
		if v3 != (version == 3) {
			say("build.txt oracle_version=%q but the run falls in task version %d", m.build.Fields["oracle_version"], version)
		}
		if expect, ok := m.build.Fields["expect"]; ok && expect != "" {
			playing := strings.Contains(expect, "reached_playing")
			if playing != (version >= 2) {
				say("build.txt oracle pattern (reached_playing=%v) does not fit task version %d", playing, version)
			}
		}
	}
	if m.hasOracle && m.oracle.Parsed && src.Loop == SetNative {
		if version == 1 && m.oracle.FramesRun > 30 {
			say("oracle ran %d frames in task version 1", m.oracle.FramesRun)
		}
		if version >= 2 && m.oracle.FramesRun == 30 {
			say("oracle ran 30 frames in task version %d", version)
		}
	}
	return out
}
