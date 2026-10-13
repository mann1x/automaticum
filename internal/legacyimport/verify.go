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
	File      string
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
// verdict files under legacyRoot and compares them with the runs in report.
// It reads the verdict files only.
func VerifyAgainstLegacy(report *Report, legacyRoot string) (*Verification, error) {
	if report == nil {
		return nil, errors.New("legacyimport: no report")
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
	found := 0
	for _, loop := range loops {
		name := verdictFiles[loop]
		f, err := os.Open(filepath.Join(legacyRoot, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		vf, err := ParseVerdictFile(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		found++
		fc := FileCheck{
			File: name, Loop: loop, Lines: vf.Lines, Malformed: len(vf.Malformed),
			Expected: vf.Counts(), Imported: map[string]int{},
		}
		for _, l := range vf.Runs {
			r, ok := byID[l.RunID]
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
				want, known := MapVerdict(l.Verdict)
				got := r.Verdict
				if got == Invalid {
					got = r.PriorVerdict
				}
				if !known || got != want {
					fc.Mismatches = append(fc.Mismatches,
						fmt.Sprintf("%s: %s should map to %q, run carries %q", l.RunID, l.Verdict, want, got))
				}
			}
		}
		for _, id := range fc.MissingIDs {
			v.Differences = append(v.Differences, fmt.Sprintf("%s: run %s is not in the import", name, id))
		}
		for _, m := range fc.Mismatches {
			v.Differences = append(v.Differences, name+": "+m)
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
				v.Differences = append(v.Differences,
					fmt.Sprintf("%s: %s: %d in the file, %d imported", name, w, fc.Expected[w], fc.Imported[w]))
			}
		}
		v.Files = append(v.Files, fc)
	}
	if found == 0 {
		return nil, errors.New("legacyimport: no verdict file under the legacy root")
	}
	return v, nil
}

// String renders the verification as text.
func (v *Verification) String() string {
	var b strings.Builder
	for _, f := range v.Files {
		fmt.Fprintf(&b, "%s: %d lines, %d not run lines; file %s; imported %s\n",
			f.File, f.Lines, f.Malformed, countsText(f.Expected), countsText(f.Imported))
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
			versions[fmt.Sprintf("%s/v%d", run.Loop, run.TaskVersion)]++
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
