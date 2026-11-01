package ingest

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/mann1x/automaticum/internal/store"
)

// The detectors of what the oracles and the agent's own end said (020 §3,
// 060 §2).
const (
	// SecondaryOracle is a secondary oracle that did not pass, or could not
	// be run: a check recorded beside the verdict, never the verdict.
	SecondaryOracle = "secondary_oracle"
	// ClaimedNotPassed is an agent that said it succeeded on a run the
	// oracle did not pass.
	ClaimedNotPassed = "claimed_success_oracle_no"
)

// PartialScore reads the tests passed and the tests run from an oracle's
// output, where the output says them (060 §2: recorded, never the
// verdict). It knows two forms: the JSON line the harness's own checkers
// print last (`"passed":N,"total":N`, at its top or under `game_logic`),
// and pytest's closing summary (`=== 3 passed, 1 failed in 0.52s ===`),
// whose total is the tests passed, failed and in error (skipped and
// expected failures are not tests run).
func PartialScore(output string) (passed, total int, ok bool) {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "{") {
			var v struct {
				Passed *int `json:"passed"`
				Total  *int `json:"total"`
				Game   *struct {
					Passed *int `json:"passed"`
					Total  *int `json:"total"`
				} `json:"game_logic"`
			}
			if json.Unmarshal([]byte(l), &v) != nil {
				continue
			}
			if v.Passed != nil && v.Total != nil && *v.Total > 0 {
				return *v.Passed, *v.Total, true
			}
			if g := v.Game; g != nil && g.Passed != nil && g.Total != nil && *g.Total > 0 {
				return *g.Passed, *g.Total, true
			}
			continue
		}
		if m := pytestSummary.FindStringSubmatch(l); m != nil {
			counts := map[string]int{}
			for _, c := range pytestCount.FindAllStringSubmatch(m[1], -1) {
				n, _ := strconv.Atoi(c[1])
				counts[strings.TrimSuffix(c[2], "s")] += n
			}
			total := counts["passed"] + counts["failed"] + counts["error"]
			if total > 0 {
				return counts["passed"], total, true
			}
		}
	}
	return 0, 0, false
}

var (
	pytestSummary = regexp.MustCompile(`^=+ (.*\d+ (?:passed|failed|errors?)\b.*) in [\d.]+s(?: \([^)]*\))? =+$`)
	pytestCount   = regexp.MustCompile(`(\d+) (passed|failed|errors?|skipped|xfailed|xpassed|deselected|warnings?)\b`)
)

// oracleBrief is what a run's record keeps of an oracle (task.OracleResult
// without its output, which is a blob).
type oracleBrief struct {
	Oracle   string
	ExitCode int
	TimedOut bool
	Passed   bool
	Err      string
}

// oracleFindings gives the findings of the run's secondary oracles that
// did not pass and of an agent that claimed success on a run the oracle did
// not pass, and the primary oracle's partial score as metrics when its
// output says one. outputs maps each oracle's name to its stored output.
func oracleFindings(r *store.Run, outputs map[string]string) ([]store.Finding, []store.Metric) {
	var p struct {
		Oracle  *oracleBrief  `json:"oracle"`
		Oracles []oracleBrief `json:"oracles"`
	}
	json.Unmarshal(r.Provenance, &p)
	var findings []store.Finding
	var metrics []store.Metric
	for _, o := range p.Oracles {
		if o.Passed && o.Err == "" {
			continue
		}
		ev, _ := json.Marshal(map[string]any{"oracle": o.Oracle, "exit_code": o.ExitCode, "timed_out": o.TimedOut, "error": o.Err,
			"output_tail": tail(outputs[o.Oracle], 2000)})
		findings = append(findings, store.Finding{RunID: r.ID, Detector: SecondaryOracle, DetectorVersion: "1", Severity: "warning", Count: 1, Evidence: ev})
	}
	if p.Oracle != nil {
		if passed, total, ok := PartialScore(outputs[p.Oracle.Oracle]); ok {
			metrics = append(metrics,
				store.Metric{RunID: r.ID, Name: "oracle.tests_passed", Value: float64(passed)},
				store.Metric{RunID: r.ID, Name: "oracle.tests_total", Value: float64(total)},
				store.Metric{RunID: r.ID, Name: "oracle.partial_share", Value: float64(passed) / float64(total)})
		}
	}
	if r.ClaimedSuccess != nil && *r.ClaimedSuccess && (r.Verdict == store.Fail || r.Verdict == store.Timeout) {
		ev, _ := json.Marshal(map[string]any{"verdict": r.Verdict, "reason": r.VerdictReason, "agent_end": r.AgentEnd})
		findings = append(findings, store.Finding{RunID: r.ID, Detector: ClaimedNotPassed, DetectorVersion: "1", Severity: "note", Count: 1, Evidence: ev})
	}
	return findings, metrics
}

// tail is the last n bytes of s, cut at a line.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 && i < len(s)-1 {
		s = s[i+1:]
	}
	return s
}
