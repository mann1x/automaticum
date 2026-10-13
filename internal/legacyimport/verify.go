package legacyimport

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileCheck compares one verdict file of the old harness with a report.
type FileCheck struct {
	File      string // for a sibling lane, "<lane>:<file>"
	Lane      string
	Loop      string
	Lines     int            // non-empty lines in the file
	Malformed int            // of which are not run lines
	Expected  map[string]int // run lines per verdict word, from the file
	Imported  map[string]int // the same runs as imported, by their original word
	// MissingIDs are runs of the file that the report does not have under
	// their original id.
	MissingIDs []string
	Mismatches []string
}

// Verification is the result of VerifyAgainstLegacy.
type Verification struct {
	Files []FileCheck
	// NoVerdictFile lists the sibling lanes that have no verdict file. There
	// is nothing to compare them with, which is not a difference.
	NoVerdictFile []string
	// Differences is empty when every verdict file is reproduced exactly:
	// every run line has an imported run with the same id and the same
	// original word, the word maps to the verdict the run carries, and the
	// counts per word are equal.
	Differences []string
}

// OK reports whether the old verdict counts were reproduced exactly and the
// original run ids kept (threshold T3 of the store spike).
func (v *Verification) OK() bool { return len(v.Differences) == 0 }

// VerifyAgainstLegacy recomputes the verdict counts from the old harness's
// verdict files under legacyRoot, and under the root of every lane given,
// and compares them with the runs in report. It reads the verdict files
// only. A sibling lane without a verdict file is named in NoVerdictFile; the
// legacy root must have one.
func VerifyAgainstLegacy(report *Report, legacyRoot string, lanes ...Lane) (*Verification, error) {
	if report == nil {
		return nil, errors.New("legacyimport: no report")
	}
	if err := checkLanes(lanes); err != nil {
		return nil, fmt.Errorf("legacyimport: %w", err)
	}
	byID := map[string]*RunSummary{}
	for i := range report.Runs {
		byID[report.Runs[i].ID] = &report.Runs[i]
	}
	v := &Verification{}
	loops := make([]string, 0, len(verdictFiles))
	for loop := range verdictFiles {
		loops = append(loops, loop)
	}
	sort.Strings(loops)
	for _, lane := range append([]Lane{{Name: MainLane, Root: legacyRoot}}, lanes...) {
		found := 0
		for _, loop := range loops {
			fc, err := verifyFile(byID, lane, loop)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			found++
			v.Files = append(v.Files, *fc)
			v.Differences = append(v.Differences, fc.differences()...)
		}
		switch {
		case found > 0:
		case lane.Name == MainLane:
			return nil, errors.New("legacyimport: no verdict file under the legacy root")
		default:
			v.NoVerdictFile = append(v.NoVerdictFile, lane.Name)
		}
	}
	return v, nil
}

// verifyFile compares one loop's verdict file of one lane with the imported
// runs. The error is fs.ErrNotExist when the lane has no such file.
func verifyFile(byID map[string]*RunSummary, lane Lane, loop string) (*FileCheck, error) {
	name := verdictFiles[loop]
	f, err := os.Open(filepath.Join(lane.Root, name))
	if err != nil {
		return nil, err
	}
	vf, err := ParseVerdictFile(f)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", laneLabel(lane.Name, name), err)
	}
	fc := &FileCheck{
		File: laneLabel(lane.Name, name), Lane: lane.Name, Loop: loop, Lines: vf.Lines, Malformed: len(vf.Malformed),
		Expected: vf.Counts(), Imported: map[string]int{},
	}
	for _, l := range vf.Runs {
		r, ok := byID[LaneRunID(lane.Name, l.RunID)]
		switch {
		case !ok:
			fc.MissingIDs = append(fc.MissingIDs, l.RunID)
		case r.Status == StatusFailed:
			fc.Mismatches = append(fc.Mismatches, fmt.Sprintf("%s: the run failed to import", l.RunID))
		default:
			fc.Imported[r.OriginalVerdict]++
			if r.OriginalVerdict != l.Verdict {
				fc.Mismatches = append(fc.Mismatches,
					fmt.Sprintf("%s: file says %s, imported as %q", l.RunID, l.Verdict, r.OriginalVerdict))
				continue
			}
			// A run marked INVALID or re-mapped carries the plain mapping
			// of its word beside its verdict.
			want, known := MapVerdict(l.Verdict)
			got := r.Verdict
			if got == Invalid || r.Remap != nil {
				got = r.PriorVerdict
			}
			if !known || got != want {
				fc.Mismatches = append(fc.Mismatches,
					fmt.Sprintf("%s: %s should map to %q, run carries %q", l.RunID, l.Verdict, want, got))
			}
		}
	}
	return fc, nil
}

// differences lists where the file and the import disagree.
func (fc *FileCheck) differences() []string {
	var out []string
	for _, id := range fc.MissingIDs {
		out = append(out, fmt.Sprintf("%s: run %s is not in the import", fc.File, id))
	}
	for _, m := range fc.Mismatches {
		out = append(out, fc.File+": "+m)
	}
	words := map[string]bool{}
	for w := range fc.Expected {
		words[w] = true
	}
	for w := range fc.Imported {
		words[w] = true
	}
	sorted := make([]string, 0, len(words))
	for w := range words {
		sorted = append(sorted, w)
	}
	sort.Strings(sorted)
	for _, w := range sorted {
		if fc.Expected[w] != fc.Imported[w] {
			out = append(out, fmt.Sprintf("%s: %s: %d in the file, %d imported", fc.File, w, fc.Expected[w], fc.Imported[w]))
		}
	}
	return out
}

// String renders the verification as text.
func (v *Verification) String() string {
	var b strings.Builder
	for _, f := range v.Files {
		fmt.Fprintf(&b, "%s: %d lines, %d not run lines; file %s; imported %s\n",
			f.File, f.Lines, f.Malformed, countsText(f.Expected), countsText(f.Imported))
	}
	for _, lane := range v.NoVerdictFile {
		fmt.Fprintf(&b, "lane %s: no verdict file\n", lane)
	}
	if v.OK() {
		b.WriteString("verdict counts reproduced exactly, original run ids kept\n")
	}
	for _, d := range v.Differences {
		b.WriteString("DIFFERENCE: " + d + "\n")
	}
	return b.String()
}

func countsText(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		name := k
		if name == "" {
			name = "(none)"
		}
		parts = append(parts, fmt.Sprintf("%s=%d", name, m[k]))
	}
	return strings.Join(parts, " ")
}

// String renders the report as text.
func (r *Report) String() string {
	var b strings.Builder
	mode := "import"
	if r.DryRun {
		mode = "dry run"
		if r.MetadataOnly {
			mode = "dry run, metadata only (stream-derived fields not evaluated)"
		}
	}
	fmt.Fprintf(&b, "%s, %s\n", r.ImporterVersion, mode)
	if len(r.Lanes) > 1 {
		fmt.Fprintf(&b, "lanes: %s\n", strings.Join(r.Lanes, " "))
	}
	section := func(title string, m map[string]*Counts) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			c := m[k]
			fmt.Fprintf(&b, "%s %-9s dirs=%d finished=%d imported=%d skipped=%d failed=%d\n",
				title, k, c.Dirs, c.Finished, c.Imported, c.Skipped, c.Failed)
		}
	}
	section("loop", r.Loops)
	section("set ", r.Sets)
	loops := make([]string, 0, len(r.Verdicts))
	for k := range r.Verdicts {
		loops = append(loops, k)
	}
	sort.Strings(loops)
	for _, k := range loops {
		fmt.Fprintf(&b, "verdicts %-7s original: %s | mapped: %s\n", k, countsText(r.OriginalVerdicts[k]), countsText(r.Verdicts[k]))
	}
	versions := map[string]int{}
	for _, run := range r.Runs {
		if run.Status != StatusFailed {
			versions[fmt.Sprintf("%s/v%d", laneKey(run.Lane, run.Loop), run.TaskVersion)]++
		}
	}
	fmt.Fprintf(&b, "task versions: %s\n", countsText(versions))
	fmt.Fprintf(&b, "files=%d blobs=%d tool_calls=%d bytes_read=%d stream_bytes=%d\n",
		r.Files, r.Blobs, r.ToolCalls, r.BytesRead, r.StreamBytes)
	fmt.Fprintf(&b, "session state: on disk for %d runs, archived for %d runs, %d archives indexed, %d matched no run\n",
		r.StateOnDisk, r.StateArchived, r.StateArchiveFiles, len(r.OrphanStateArchive))
	fmt.Fprintf(&b, "unknown fields (runs): %s\n", countsText(r.UnknownFields))
	ev := make(map[string]int, len(r.EventTypes))
	for k, n := range r.EventTypes {
		ev[k] = int(n)
	}
	fmt.Fprintf(&b, "event types: %s\n", countsText(ev))
	for _, bd := range r.Boundaries {
		fmt.Fprintf(&b, "boundary %s from %s (%s)\n", bd.Kind, bd.From, bd.Source)
	}
	fmt.Fprintf(&b, "invalid notes: %d; ignored entries: %d; id collisions: %d\n", len(r.InvalidNotes), len(r.Ignored), len(r.IDCollisions))
	fmt.Fprintf(&b, "second pass: invalid by table=%d, invalid by stale lane script=%d, re-mapped to ABORTED=%d, re-mapped to TIMEOUT=%d\n",
		r.InvalidByTable, r.InvalidStale, r.RemappedAborted, r.RemappedTimeout)
	for _, run := range r.Runs {
		if rm := run.Remap; rm != nil {
			fmt.Fprintf(&b, "re-mapped %s: %s -> %s (%s) exit=%d wall=%ds", run.ID, rm.From, rm.To, rm.Basis, rm.ExitCode, rm.WallSeconds)
			if rm.CapSeconds > 0 {
				fmt.Fprintf(&b, " cap=%ds", rm.CapSeconds)
			}
			fmt.Fprintf(&b, ": %s\n", rm.Cause)
		}
	}
	for _, s := range r.Skipped {
		fmt.Fprintf(&b, "skipped %s: %s\n", s.RelDir, s.Reason)
	}
	for _, s := range r.Failed {
		fmt.Fprintf(&b, "FAILED %s: %s\n", s.RelDir, s.Reason)
	}
	for _, s := range r.Inconsistencies {
		fmt.Fprintf(&b, "inconsistent: %s\n", s)
	}
	for _, s := range r.Warnings {
		fmt.Fprintf(&b, "warning: %s\n", s)
	}
	return b.String()
}
