package legacyimport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Identity given to every imported run. The old harness had one task.
const (
	LegacySuite    = "legacy-manic-miner"
	LegacyTaskID   = "manic-miner"
	CampaignPrefix = "legacy-"
)

// Default comparability boundaries of the native loop (spec 100 section 3).
// A run at or after a boundary belongs to the later task version. Run ids
// start with their timestamp, so they compare as text; the sequence number
// alone would not do, because the old harness reused some numbers.
const (
	DefaultTaskV2From = "20260911-155111-0213" // 400 frames, must reach play
	DefaultTaskV3From = "20260930-131610-0411" // compiled oracle with traps
)

// MapVerdict maps a verdict word of the old harness to a verdict. ok is
// false for a word the old harness never wrote.
func MapVerdict(word string) (v Verdict, ok bool) {
	switch word {
	case "FIXED":
		return Pass, true
	case "broken":
		return Fail, true
	case "TIMEOUT":
		return Timeout, true
	case "ABORTED":
		return Aborted, true
	case "ROLLBACK-FAILED":
		return Aborted, true
	}
	return "", false
}

// TaskVersionFor returns the task version of a run from its id.
func TaskVersionFor(runID, v2From, v3From string) int {
	id := RunIDPattern.FindString(runID)
	if id == "" {
		id = runID
	}
	switch {
	case id >= v3From:
		return 3
	case id >= v2From:
		return 2
	}
	return 1
}

// Boundary is a comparability boundary taken from the old harness.
type Boundary struct {
	Kind        string // "task_version" or "harness_change"
	From        string // the first run id on the later side, or a day (YYYYMMDD) when no run is named
	TaskVersion int    // for task_version
	Source      string // where it was recorded, relative to the legacy root
}

// runMeta is everything read from a run's small text files.
type runMeta struct {
	build     *Build
	exit      *Exit
	hasExit   bool
	oracle    *Oracle
	hasOracle bool
	logic     string
	tx        *Transactions
	checks    map[string]string
	esc       map[string]string
	deleg     map[string]string
	delegBy   []string
	abortNote string
	notes     []string // operator notes found in the directory
	txs       map[string]*txMeta
	settings  *ProviderSettings // from a state directory on disk
	stateDirs int               // state directories on disk (one, or one per transaction)

	verdictLine   *VerdictLine // this run's line in its loop's verdict file
	invalid       *InvalidNote
	fingerprint   string
	files         int
	bytesOnDisk   int64
	archiveRel    string
	hasArchive    bool
	warnings      []string
	verdictSource string
}

type txMeta struct {
	Oracle    *Oracle `json:"oracle,omitempty"`
	Diffstat  string  `json:"diffstat,omitempty"`
	HasStream bool    `json:"has_stream"`
	HasState  bool    `json:"has_state"`
	HasPrompt bool    `json:"has_prompt"`
}

// verdictOf decides the verdict fields of a run from what the old harness
// recorded.
func verdictOf(m *runMeta) (v Verdict, original, reason string, prior Verdict, missing bool, err error) {
	switch {
	case m.hasExit:
		original = m.exit.Verdict
		m.verdictSource = "exit.txt"
		reason = m.exit.Reason
		if reason == "" {
			reason = m.abortNote
		}
	case m.verdictLine != nil:
		original = m.verdictLine.Verdict
		m.verdictSource = "verdict file"
	}
	if original != "" {
		var ok bool
		if v, ok = MapVerdict(original); !ok {
			return "", original, "", "", false, fmt.Errorf("verdict word %q is not one the old harness is known to write", original)
		}
		if original == "ROLLBACK-FAILED" && reason == "" {
			reason = "rollback failed: the work directory did not match the snapshot"
		}
	}
	if m.invalid != nil {
		prior = v
		return Invalid, original, m.invalid.Reason, prior, false, nil
	}
	if original == "" {
		// Not SKIPPED (it started) and not a default: no verdict could be
		// produced, which is what ABORTED means, and the absence is named.
		return Aborted, "", "the old harness recorded no verdict (no exit.txt): the run did not finish", "", true, nil
	}
	return v, original, reason, "", false, nil
}

type stateFacts struct {
	onDisk  bool
	archive *StateTar
	// settings from the state directory or, failing that, from the archive
	settings *ProviderSettings
}

// buildRun maps one run. stats is nil when the event stream was not read.
func buildRun(src RunSource, m *runMeta, stats *StreamStats, st stateFacts, calls int, o *Options, changes []Boundary) (*Run, error) {
	r := &Run{
		ID:              src.ID,
		Campaign:        CampaignPrefix + src.Loop,
		Suite:           LegacySuite,
		TaskID:          LegacyTaskID,
		TaskVersion:     TaskVersionFor(src.ID, o.TaskV2From, o.TaskV3From),
		Imported:        true,
		ImporterVersion: ImporterVersion,
	}
	missing := map[string]bool{
		// Never captured by the old harness (spec 100 section 4).
		"wire": true,
		// The stream does not say; see ClaimedSuccess in the run record.
		"claimed_success": true,
		// Not recorded per run: which machine ran it, which driver script
		// launched it, and therefore the arm file it would correspond to.
		"host":          true,
		"arm_id":        true,
		"driver_script": true,
		// --thinking was passed on the command line and written nowhere.
		"thinking": true,
	}
	r.Host, r.ArmID, r.Thinking = Unknown, Unknown, Unknown

	v, original, reason, prior, noVerdict, err := verdictOf(m)
	if err != nil {
		return nil, err
	}
	r.Verdict, r.OriginalVerdict, r.VerdictReason, r.PriorVerdict = v, original, reason, prior
	if noVerdict {
		missing["verdict"] = true
	}

	get := func(key string) string {
		if m.build == nil {
			return ""
		}
		return m.build.Fields[key]
	}
	if m.build == nil {
		missing["build"] = true
	}
	if r.Model = get("model"); r.Model == "" {
		r.Model = Unknown
		missing["model"] = true
	}
	endpointDefaulted := false
	if ep := get("endpoint"); ep != "" {
		if strings.HasSuffix(ep, " (default)") {
			ep, endpointDefaulted = strings.TrimSuffix(ep, " (default)"), true
		}
		r.Endpoint = ep
	} else {
		r.Endpoint = Unknown
		missing["endpoint"] = true
	}
	if ver := get("version"); ver != "" {
		r.Harness = ver
		if sha := get("sha"); sha != "" {
			r.Harness += "+" + sha
		}
		if d := get("dirty"); d != "" && d != "0" {
			r.Harness += ".dirty"
		}
	} else {
		r.Harness = Unknown
		missing["harness"] = true
	}

	// Provider, how the agent ended and the clock come from the stream.
	r.Provider, r.AgentEnd = Unknown, Unknown
	contextWindow := int64(0)
	if st.settings != nil {
		r.Provider = st.settings.Provider
		contextWindow = st.settings.ContextWindow
	}
	if stats != nil {
		if stats.Provider != "" {
			r.Provider = stats.Provider
		}
		if stats.ContextWindow > 0 && contextWindow == 0 {
			contextWindow = stats.ContextWindow
		}
		switch {
		case stats.FinishReason != "":
			r.AgentEnd = stats.FinishReason
		case stats.DoneReason != "":
			r.AgentEnd = stats.DoneReason
		}
		r.StartedAt, r.EndedAt = stats.FirstTS, stats.LastTS
		if stats.Streams == 0 {
			missing["events"] = true
		} else if stats.TextDeltas > 0 && stats.AccumulatedLines == 0 {
			missing["accumulated_text"] = true
		}
	}
	if m.hasExit && m.exit.HasWall {
		r.WallMS = m.exit.WallSeconds * 1000
	} else if stats != nil && stats.DurationMS > 0 {
		r.WallMS = stats.DurationMS
	} else if stats != nil && !stats.FirstTS.IsZero() {
		r.WallMS = stats.LastTS.Sub(stats.FirstTS).Milliseconds()
	}
	if !o.MetadataOnly {
		if r.Provider == Unknown {
			missing["provider"] = true
		}
		if r.AgentEnd == Unknown {
			missing["agent_end"] = true
		}
		if r.StartedAt.IsZero() {
			missing["started_at"] = true
			missing["ended_at"] = true
		}
		if contextWindow == 0 {
			missing["context_window"] = true
		}
		if r.WallMS == 0 && !(m.hasExit && m.exit.HasWall) {
			missing["wall_ms"] = true
		}
	}
	if !st.onDisk && !m.hasArchive {
		missing["session_state"] = true
	}
	if st.settings == nil || len(st.settings.Sampling) == 0 {
		// Without the settings file the sampler is whatever the model tag
		// declared; with it and no sampling block, the same. Either way the
		// values are not on record.
		missing["sampler"] = true
	}

	arm := map[string]any{
		"values_are":    "as configured",
		"provider":      r.Provider,
		"model":         r.Model,
		"endpoint":      r.Endpoint,
		"thinking":      Unknown,
		"harness":       r.Harness,
		"driver_script": Unknown,
		"sampling":      Unknown,
	}
	if endpointDefaulted {
		arm["endpoint_defaulted"] = true
	}
	if m.build != nil {
		arm["build"] = m.build.Fields
	}
	if a := get("arm"); a != "" {
		arm["protocol_arm"] = a
	} else {
		arm["protocol_arm"] = Unknown
		missing["protocol_arm"] = true
	}
	if contextWindow > 0 {
		arm["context_window"] = contextWindow
	} else {
		arm["context_window"] = Unknown
	}
	if st.settings != nil {
		if len(st.settings.Sampling) > 0 {
			arm["sampling"] = st.settings.Sampling
		}
		arm["provider_settings"] = st.settings
	}

	prov := map[string]any{
		"legacy": map[string]any{
			"loop":           src.Loop,
			"set":            src.Set,
			"dir":            src.RelDir,
			"annotation":     src.Annotation,
			"verdict_source": m.verdictSource,
		},
		"source_fingerprint": m.fingerprint,
		"import_complete":    false,
		"files":              m.files,
		"bytes_on_disk":      m.bytesOnDisk,
	}
	switch r.TaskVersion {
	case 1:
		prov["task_version_reason"] = "before " + o.TaskV2From + ": 30-frame oracle"
		prov["findings"] = []map[string]any{{
			"detector": "legacy_oracle",
			"severity": "warning",
			"detail":   "task version 1: the oracle pumped 30 frames, never left the countdown and scored broken code as passing; it could not fail on game logic",
		}}
	case 2:
		prov["task_version_reason"] = "from " + o.TaskV2From + ": 400 frames, must reach play"
	case 3:
		prov["task_version_reason"] = "from " + o.TaskV3From + ": compiled oracle with game-logic traps"
	}
	var crossed []string
	id := RunIDPattern.FindString(src.ID)
	for _, b := range changes {
		if b.Kind == "harness_change" && id != "" && id >= b.From {
			crossed = append(crossed, b.From)
		}
	}
	prov["harness_changes_before_run"] = crossed
	if m.hasExit {
		prov["exit"] = m.exit.Map()
		prov["exit_raw"] = m.exit.Raw
	}
	if m.verdictLine != nil {
		prov["verdict_file_line"] = m.verdictLine.Raw
	}
	if m.invalid != nil {
		prov["invalid"] = map[string]any{"reason": m.invalid.Reason, "source": m.invalid.Source}
	}
	if m.tx != nil {
		prov["transactions"] = m.tx
	}
	if m.checks != nil {
		prov["checks"] = m.checks
	}
	if m.esc != nil {
		prov["escalations"] = m.esc
	}
	if m.deleg != nil {
		prov["delegation"] = map[string]any{"summary": m.deleg, "by_agent": m.delegBy}
	}
	if m.hasOracle {
		prov["oracle"] = m.oracle
	}
	if m.logic != "" {
		prov["oracle_logic"] = m.logic
	}
	if m.abortNote != "" {
		prov["abort_reason"] = m.abortNote
	}
	if len(m.notes) > 0 {
		prov["operator_notes"] = m.notes
	}
	if len(m.txs) > 0 {
		prov["atomic_transactions"] = m.txs
	}
	if stats != nil {
		prov["events"] = stats
		prov["events_format"] = "CLI wrapped stream, stored as written; ConvertLine maps a line to the SDK event shape"
		prov["tool_calls"] = calls
	}
	state := map[string]any{"on_disk": st.onDisk, "state_dirs": m.stateDirs}
	if m.hasArchive {
		state["archive"] = m.archiveRel
	}
	if st.archive != nil {
		state["archive_scan"] = st.archive
	}
	prov["session_state"] = state
	if len(m.warnings) > 0 {
		prov["import_warnings"] = m.warnings
	}

	for k := range missing {
		r.Missing = append(r.Missing, k)
	}
	sort.Strings(r.Missing)
	if r.Arm, err = json.Marshal(arm); err != nil {
		return nil, err
	}
	if r.Provenance, err = json.Marshal(prov); err != nil {
		return nil, err
	}
	return r, nil
}

// markComplete rewrites the provenance to say the import of the run
// finished, which is what makes it skippable on the next pass.
// importFindings returns the findings the import makes about a run: a run
// scored under task version 1 had an oracle that could not fail (spec 100
// section 3).
func importFindings(r *Run) []Finding {
	if r.TaskVersion != 1 {
		return nil
	}
	ev, _ := json.Marshal(map[string]string{
		"detail": "task version 1: the oracle pumped 30 frames, never left the countdown and scored broken code as passing; it could not fail on game logic",
	})
	return []Finding{{RunID: r.ID, Detector: "legacy_oracle", DetectorVersion: ImporterVersion, Severity: "warning", Count: 1, Evidence: ev}}
}

func markComplete(r *Run) error {
	var prov map[string]json.RawMessage
	if err := json.Unmarshal(r.Provenance, &prov); err != nil {
		return err
	}
	prov["import_complete"] = json.RawMessage("true")
	prov["imported_at"], _ = json.Marshal(time.Now().UTC().Format(time.RFC3339))
	b, err := json.Marshal(prov)
	if err != nil {
		return err
	}
	r.Provenance = b
	return nil
}
