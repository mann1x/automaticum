package legacyimport

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The second pass over the corpus (spec 100 section 8). Everything the owner
// decided by run id or by rule is in this file: the runs the old harness's
// records call invalid, the verdicts contradicted by how the run ended, the
// sibling lanes and their stale lane script.

// MainLane is the lane name of Options.LegacyRoot. Its run ids carry no
// prefix.
const MainLane = "main"

// Lane is a sibling directory of the old harness with the layout of the
// legacy root. It is only read.
type Lane struct {
	// Name prefixes the lane's run ids: "<name>-<original id>".
	Name string
	Root string
	// StateArchive is an external directory of archived session state.
	// Optional; the state-archive directory inside Root is always consulted.
	StateArchive string
}

var laneName = regexp.MustCompile(`^[a-z0-9][a-z0-9_]*$`)

func checkLanes(lanes []Lane) error {
	seen := map[string]bool{MainLane: true}
	for _, l := range lanes {
		switch {
		case !laneName.MatchString(l.Name):
			return fmt.Errorf("lane name %q: use lower-case letters, digits and underscores", l.Name)
		case seen[l.Name]:
			return fmt.Errorf("lane name %q is taken", l.Name)
		case l.Root == "":
			return fmt.Errorf("lane %s has no root", l.Name)
		}
		seen[l.Name] = true
	}
	return nil
}

// LaneRunID returns the id an imported run carries: the original id for the
// main lane, "<lane>-<original id>" for a sibling lane, whose run numbers
// restart at 0001. A hyphen and not a slash, so the id stays one path
// segment, one shell word and one URL segment wherever it is used later.
func LaneRunID(lane, originalID string) string {
	if lane == "" || lane == MainLane {
		return originalID
	}
	return lane + "-" + originalID
}

// laneKey is the key of a report's per-loop and per-set maps: the bare name
// for the main lane, "<lane>/<name>" for a sibling lane.
func laneKey(lane, name string) string {
	if lane == "" || lane == MainLane {
		return name
	}
	return lane + "/" + name
}

// laneLabel names a path of a lane in a message.
func laneLabel(lane, rel string) string {
	if lane == "" || lane == MainLane {
		return rel
	}
	return lane + ":" + rel
}

// --- A. Runs the old harness's own records call invalid ---------------------

const (
	reasonPartial  = "partial states and operator kills; not to be read as an arm (old change log of 2026-09-11)"
	reasonInFlight = "killed in flight; no verdict to read (old change log of 2026-09-13)"
	reasonUnloaded = "another model was loaded on the endpoint and unloaded the arm's model mid-run (old change log of 2026-10-11)"
)

// invalidByRecord lists runs of the main lane that are INVALID although
// neither INVALID-RUNS.md nor a VOID marker names them. The original verdict
// stays beside the new one, as for every INVALID run.
var invalidByRecord = []InvalidNote{
	// run id, reason, where the old harness recorded it
	{"20260911-141110-0207", reasonPartial, "changes-20260911/CHANGES.md"},
	{"20260911-142601-0208", reasonPartial, "changes-20260911/CHANGES.md"},
	{"20260911-151011-0209", reasonPartial, "changes-20260911/CHANGES.md"},
	{"20260911-152434-0210", reasonPartial, "changes-20260911/CHANGES.md"},
	{"20260911-153450-0211", reasonPartial, "changes-20260911/CHANGES.md"},
	{"20260911-154542-0212", reasonPartial, "changes-20260911/CHANGES.md"},
	{"20260913-214530-0296", reasonInFlight, "changes-20260913/CHANGES.md"},
	{"20261011-132114-0431", reasonUnloaded, "changes-20261011/CHANGES.md"},
}

// --- B. Verdicts contradicted by how the run ended --------------------------

// Remap bases: the cause is written in the old harness's notes; or it was
// stated by the session that ran the old harness and is not in the corpus;
// or it is read off the exit code and the wall time.
const (
	BasisDocumented = "documented"
	BasisReported   = "reported"
	BasisInferred   = "inferred"
)

// RemapDetector is the detector name of the finding a re-mapped run carries.
const RemapDetector = "legacy_verdict_remap"

// FailedWord is the old harness's word for a failed run.
const FailedWord = "broken"

// Remap says a run's mapped verdict was changed because of how the run
// ended. The original word is untouched; From is what it maps to.
type Remap struct {
	From  Verdict `json:"from"`
	To    Verdict `json:"to"`
	Cause string  `json:"cause"`
	Basis string  `json:"basis"`
	// What the rule read from the run's exit file.
	ExitCode    int   `json:"exit_code"`
	WallSeconds int64 `json:"wall_seconds,omitempty"`
	CapSeconds  int64 `json:"cap_seconds,omitempty"` // TIMEOUT only: the limit the wall time sits at
}

const (
	causeKillHost   = "ended by an operator's kill to free the host (old change log of 2026-10-11)"
	causeKill1h56   = "operator kill at 1h56m"
	causeExit143    = "exit code 143 on a run recorded as failed, before the ABORTED verdict existed"
	causeClockExit1 = "exit code 1 at the CLI's own time limit, before the TIMEOUT verdict existed"
)

// abortedByOperator lists runs of the main lane that an operator ended and
// the old harness recorded as failed. An entry is applied only when the
// run's exit file agrees with it: the failed word and this exit code.
var abortedByOperator = []struct {
	RunID    string
	ExitCode int
	Basis    string
	Cause    string
}{
	{"20261011-144820-0432", 0, BasisDocumented, causeKillHost},
	{"20260908-133809-0166", 143, BasisReported, causeKill1h56},
	{"20260908-090521-0164", 143, BasisInferred, causeExit143},
	{"20260908-153544-0167", 143, BasisInferred, causeExit143},
	{"20260909-121718-0178", 143, BasisInferred, causeExit143},
	{"20260910-152929-0190", 143, BasisInferred, causeExit143},
}

// The CLI's own clock: a failed run with exit code 1 whose wall time is
// within clockSlack of one of clockCaps ended at the CLI's time limit.
var clockCaps = []int64{21600, 5402}

const clockSlack = 60 // seconds, either side of a cap

// remapVerdict is the whole of rule B. It applies to a run of the main lane
// that is not INVALID, and changes the mapped verdict of a run recorded as
// failed in two cases:
//
//  1. the run is in abortedByOperator and its exit file agrees: ABORTED;
//  2. its exit code is 1 and its wall time is within clockSlack seconds of
//     a cap in clockCaps: TIMEOUT, always inferred. No run id is listed.
//
// A listed run whose files do not agree with the table is left as it is and
// reported in inconsistency. originalID is the run's id in the old harness;
// original is its verdict word; exit is nil when the run has no exit file.
func remapVerdict(originalID, original string, exit *Exit) (r *Remap, inconsistency string) {
	for _, e := range abortedByOperator {
		if e.RunID != originalID {
			continue
		}
		switch {
		case original != FailedWord:
			return nil, fmt.Sprintf("listed as an operator abort recorded as failed, but the old harness recorded %q; not re-mapped", original)
		case exit == nil || !exit.HasExitCode:
			return nil, "listed as an operator abort, but the run has no exit code on record; not re-mapped"
		case exit.ExitCode != e.ExitCode:
			return nil, fmt.Sprintf("listed as an operator abort with exit code %d, but the exit file says %d; not re-mapped", e.ExitCode, exit.ExitCode)
		}
		return &Remap{From: Fail, To: Aborted, Cause: e.Cause, Basis: e.Basis, ExitCode: exit.ExitCode, WallSeconds: exit.WallSeconds}, ""
	}
	if original != FailedWord || exit == nil || !exit.HasExitCode || exit.ExitCode != 1 || !exit.HasWall {
		return nil, ""
	}
	for _, limit := range clockCaps {
		if exit.WallSeconds >= limit-clockSlack && exit.WallSeconds <= limit+clockSlack {
			return &Remap{From: Fail, To: Timeout, Cause: causeClockExit1, Basis: BasisInferred, ExitCode: 1, WallSeconds: exit.WallSeconds, CapSeconds: limit}, ""
		}
	}
	return nil, ""
}

// --- C. Sibling lanes -------------------------------------------------------

// Until staleLaneScriptUntil a sibling lane ran under a lane script that was
// four days behind the main lane's: the old oracle, no TIMEOUT or ABORTED
// verdicts, other limits. Its runs before that moment are INVALID. The rule
// is applied to a lane that holds its own record of the replacement: a
// backups directory of that day with the lane script as it was before.
const (
	staleLaneScriptUntil  = "20260911-190500"
	staleLaneScriptReason = "stale lane script (old oracle and limits) until 2026-09-11 19:05"
)

// staleLaneScript returns the backup of the replaced lane script under a
// lane root, relative to it, or "" when the lane holds none.
func staleLaneScript(root string) (string, error) {
	day := staleLaneScriptUntil[:8]
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "backups-"+day) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, e.Name()))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		for _, f := range files {
			if strings.HasPrefix(f.Name(), "native.sh.pre-") {
				return e.Name() + "/" + f.Name(), nil
			}
		}
	}
	return "", nil
}

// laneTaskVersion derives the task version of a sibling lane's run from the
// run's own files. The main lane's boundaries are run ids of the main lane
// and say nothing about another lane, so they are not used. Version 0 means
// the oracle generation cannot be established; the caller records the task
// version as missing.
//
// The marks are those the main lane's runs carry on each side of its
// boundaries: a build file that names oracle version v3; an oracle pattern
// that demands reached_playing (the 400-frame oracle) or does not (the
// 30-frame one); and, when the build file gives no pattern, whether the
// oracle's own report has the reached_playing field.
func laneTaskVersion(m *runMeta) (version int, reason string) {
	if m.build != nil {
		if m.build.Fields["oracle_version"] == "v3" {
			return 3, "build.txt names oracle version v3"
		}
		if expect := m.build.Fields["expect"]; expect != "" {
			if strings.Contains(expect, "reached_playing") {
				return 2, "build.txt oracle pattern demands reached_playing: the 400-frame oracle"
			}
			return 1, "build.txt oracle pattern does not demand reached_playing: the 30-frame oracle"
		}
	}
	if m.hasOracle && m.oracle.Parsed {
		switch {
		case m.oracle.HasGameLogic:
			// The compiled oracle's report without its mark in the build file.
			return 0, "oracle.txt reports game logic but build.txt names no oracle version"
		case m.oracle.ReachedPlaying != nil:
			return 2, "oracle.txt reports reached_playing: the 400-frame oracle"
		}
		return 1, "oracle.txt does not report reached_playing: the 30-frame oracle"
	}
	return 0, "neither build.txt nor oracle.txt says which oracle scored the run"
}
