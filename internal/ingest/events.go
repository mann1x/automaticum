// Package ingest turns a run's stored raw material into what is queried:
// metrics, tool calls and findings (spec 070). It reads the event stream and
// the wire capture from blobs, so the same code serves a run that has just
// ended and a run analysed again years later.
package ingest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/mann1x/automaticum/internal/detect"
	"github.com/mann1x/automaticum/internal/store"
)

// Block is one finished piece of what the model wrote: its reasoning or its
// visible text. Detectors work on blocks.
type Block struct {
	Turn int
	Kind string // "reasoning" or "text"
	Text string
	// GivenTo is how much of Events.Given came before the block.
	GivenTo int
	// Unfinished is a block whose stream stopped before its "content_end":
	// the output limit or the wall limit cut the turn. It is what its
	// deltas added up to (bug-076).
	Unfinished bool
	// Start and End are when its first delta and its end were printed.
	Start, End time.Time
	// Request is the wire's request, for a block read from the wire
	// because the event stream never had it.
	Request int
}

// Events is what one pass over a run's event stream gives.
type Events struct {
	Lines, BadLines int
	First, Last     time.Time
	// TurnStarts is when each turn started; MaxGapMS the longest time
	// between two lines with a time (a hold of a paused run is inside it).
	TurnStarts []time.Time
	MaxGapMS   float64
	Iterations int
	AgentEnd   string
	ToolCalls  []store.ToolCall
	// Calls holds the same calls with their arguments, for the detectors.
	Calls  []detect.Call
	Blocks []Block
	// Notices counts the SDK's notices by key (NoticeKey): what kind of
	// notice it is, never its sentence, which differs from run to run.
	Notices map[string]int
	// Compactions counts the compactions the SDK completed.
	Compactions int
	// KeepNotices asks Read to keep every notice with its sentence in
	// NoticeLog, for `automaticum notices`; the analysis does not.
	KeepNotices bool
	NoticeLog   []Notice
	// KeepTimeline asks Read to keep the run's moments in order, with
	// their times, in Timeline, for `automaticum run timeline` (070 §1.1);
	// the analysis does not.
	KeepTimeline bool
	Timeline     []Moment
	// GuardTrips counts the guards' trips the runner noted, by guard and
	// phase ("reasoning_loop.cut"); GuardStop is the trip that ended the
	// run, for an arm that stops at the first one (D99).
	GuardTrips map[string]int
	GuardStop  string
	// GuardCauses counts the trips again by what was repeating, for the
	// guards that say ("reasoning_loop.cut.answer_loop"); GuardStopCause is
	// the same key of the trip that ended the run, or empty.
	GuardCauses    map[string]int
	GuardStopCause string
	// FirstTokenMS is the time to the first token of the first request.
	FirstTokenMS float64
	// Tokens by kind, as the SDK split them, and how it split.
	ReasoningTokens, TextTokens, ToolInputTokens int
	SplitMethod                                  string
	// Derived holds the values the SDK worked out from the arm, as the
	// runner noted them.
	Derived map[string]float64
	// Questions is how many questions the scripted user answered.
	Questions int
	// UserActions is how many times a rule of the scripted user acted;
	// UserDenials how many tool calls it denied, by a rule or by default.
	UserActions int
	UserDenials int
	// UserFailed is how many of the scripted user's actions the SDK
	// refused: the run did not play the script it was meant to.
	UserFailed int
	// LSP says the run had the editor's language servers (D149);
	// LSPEdits counts the edits the diagnostics after edits looked at,
	// LSPReports those that got a block, LSPRegressed those marked
	// regressed, LSPErrors what the servers failed at.
	LSP                                           bool
	LSPEdits, LSPReports, LSPRegressed, LSPErrors int
	// Forks are the session forks a restore of messages started (D86), in
	// order; ModeSwitches how often the scripted user switched the mode,
	// AgentModeSwitches how often the agent did (switch_to_act_mode).
	Forks             []Fork
	ModeSwitches      int
	AgentModeSwitches int
	// TurnTokens are the SDK's split of each usage report, in order: one
	// per request the agent's loop made (070 §2).
	TurnTokens []TurnTokens
	// Turns says, by turn, what the model gave in it.
	Turns map[int]*TurnActs
	// CompactionTimes are when the compactions completed.
	CompactionTimes []time.Time
	// Trips are the guards' trips in order, each with what the model did
	// next (070 §4).
	Trips []Trip
	// CompactionLog holds each completed compaction's numbers.
	CompactionLog []Compaction
	// Escalations are the escalations the SDK ended, with their spend.
	Escalations []Escalation
	// ForkCalls is, for each fork, how many calls came before it.
	ForkCalls []int
	// Given is what the tools gave the model, in order: the strings of each
	// result, as the SDK's loop guard collects them from the request's
	// messages (D75). A block's GivenTo says how much came before it.
	Given strings.Builder
	// wired is set once WireAnswers added the wire's answers.
	wired bool
	// turnOf is the turn each of TurnStarts started.
	turnOf []int
	// CutBlocks are what the reasoning-loop guard cut off: the stream up
	// to its trip, which the SDK threw away and asked for again. They are
	// not part of Blocks; the guard's replay reads them (070 §4).
	CutBlocks []Block
	// Struggle is what the SDK's struggle detector observes, in order, for
	// its replay (D126).
	Struggle []detect.StruggleEvent
	// NoticeTurns is the turn of each notice key's first notice.
	NoticeTurns map[string]int
	// Transactions are the change protocol's transactions as they settled,
	// EmptyAttempts the attempts submitted with nothing changed and Plans
	// the plans it recorded (070 §2).
	Transactions  []Transaction
	EmptyAttempts int
	Plans         int
}

// Transaction is one transaction of the SDK's change protocol as it
// settled: kept or put back, carried on, whether its check gave output,
// how long it took and how many files were put back.
type Transaction struct {
	Turn         int
	ID           int
	Kept         bool
	Carried      bool
	Source       string
	Checked      bool
	ElapsedMS    float64
	FilesPutBack int
}

// Compaction is one compaction the SDK completed: what started it and the
// prompt it left.
type Compaction struct {
	Turn         int     `json:"turn"`
	Kind         string  `json:"kind"`
	Cause        string  `json:"cause,omitempty"`
	TokensBefore float64 `json:"tokens_before"`
	TokensAfter  float64 `json:"tokens_after"`
	DurationMS   float64 `json:"duration_ms"`
}

// EscalationUsage is an escalation's spend, as the SDK states it.
type EscalationUsage struct {
	InputTokens    float64 `json:"inputTokens"`
	OutputTokens   float64 `json:"outputTokens"`
	GenerateTokens float64 `json:"generateTokens"`
	WallMS         float64 `json:"wallMs"`
	Requests       float64 `json:"requests"`
}

// Escalation is one escalation that ended: failed or not, whether the
// expert changed anything, and its spend.
type Escalation struct {
	Turn    int
	Failed  bool
	Changed bool
	Usage   EscalationUsage
}

// TurnTokens is one usage report's split of the completion.
type TurnTokens struct {
	Turn                       int
	Reasoning, Text, ToolInput int
}

// TurnActs is what one turn held: reasoning, visible text (its characters
// without the space around it) and tool calls.
type TurnActs struct {
	Reasoning bool
	TextChars int
	Calls     int
}

// Trip is one guard's trip and the first thing the model did after it: a
// call ("tool"), a reply ("text"), its reasoning ("reasoning") or the end
// of the run ("end"); empty when nothing came.
type Trip struct {
	T     time.Time `json:"t"`
	Turn  int       `json:"turn"`
	Guard string    `json:"guard"`
	Phase string    `json:"phase,omitempty"`
	Cause string    `json:"cause,omitempty"`
	Next  string    `json:"next,omitempty"`
	Tool  string    `json:"tool,omitempty"`
}

// Fork is one session fork of a run (D86): when it started, the
// checkpoint it went back to, and what the SDK started again from nothing
// in the new session, as the runner noted it.
type Fork struct {
	T          time.Time `json:"t"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Checkpoint int       `json:"checkpoint"`
	Workspace  bool      `json:"workspace"`
	Restarted  []string  `json:"restarted"`
}

// eventLine is one line of a stream. The runner writes
//
//	{"t":..., "k":"event", "event":{"type":"agent_event", "payload":{"event":{...}}}}
//
// and the older harness, whose runs are imported, wrote
//
//	{"ts":..., "type":"agent_event", "event":{...}}
//
// The innermost object is the SDK's event in both.
type eventLine struct {
	T       time.Time          `json:"t"`
	K       string             `json:"k"`
	Type    string             `json:"type"`
	Note    string             `json:"note"`
	Event   json.RawMessage    `json:"event"`
	Derived map[string]float64 `json:"derived"`
	// A runner's guard_trip or guard_stop note (protocol 4).
	Guard string `json:"guard"`
	Phase string `json:"phase"`
	Cause string `json:"cause"`
	// A scripted user's line (040 §6.2): the event it answered, the rule
	// (0 for the default) and what it did.
	On       string `json:"on"`
	Rule     int    `json:"rule"`
	Approved *bool  `json:"approved"`
	// A runner's fork note (D86).
	From       string   `json:"from"`
	To         string   `json:"to"`
	Checkpoint int      `json:"checkpoint"`
	Workspace  bool     `json:"workspace"`
	Restarted  []string `json:"restarted"`
	// Who made a mode switch: "user" or "agent"; absent before 2026-10-16,
	// when only the user could.
	Source string `json:"source"`
	// The runner's prepared note: its language servers, null when the arm
	// has none (protocol 11, D149); an editor_diagnostics note's block and
	// whether the edit was marked regressed.
	LSP        json.RawMessage `json:"lsp"`
	BlockChars int             `json:"block_chars"`
	Regressed  bool            `json:"regressed"`
}

type agentEvent struct {
	Type        string          `json:"type"`
	ContentType string          `json:"contentType"`
	Reasoning   string          `json:"reasoning"`
	Text        string          `json:"text"`
	ToolName    string          `json:"toolName"`
	ToolCallID  string          `json:"toolCallId"`
	Iteration   int             `json:"iteration"`
	Input       json.RawMessage `json:"input"`
	Error       json.RawMessage `json:"error"`
	Output      json.RawMessage `json:"output"`
	DurationMS  *float64        `json:"durationMs"`
	NoticeType  string          `json:"noticeType"`
	Reason      string          `json:"reason"`
	Message     string          `json:"message"`
	Metadata    struct {
		Kind  string `json:"kind"`
		Phase string `json:"phase"`
		// A compaction's numbers (070 §2), as the SDK states them at
		// "completed", and what started it.
		Cause        string   `json:"cause"`
		TokensBefore *float64 `json:"tokensBefore"`
		TokensAfter  *float64 `json:"tokensAfter"`
		DurationMS   *float64 `json:"durationMs"`
		// An escalation's spend and whether the expert changed anything.
		Usage   *EscalationUsage `json:"usage"`
		Changed *bool            `json:"changed"`
		// A transaction of the change protocol (atomic_*).
		Transaction  *int            `json:"transaction"`
		Kept         *bool           `json:"kept"`
		Carried      bool            `json:"carried"`
		Source       string          `json:"source"`
		ElapsedMS    *float64        `json:"elapsedMs"`
		Output       json.RawMessage `json:"output"`
		FilesPutBack int             `json:"filesPutBack"`
	} `json:"metadata"`
	Timings struct {
		FirstTokenMS float64 `json:"firstTokenMs"`
	} `json:"timings"`
	OutputSplit *struct {
		ReasoningTokens int    `json:"reasoningTokens"`
		TextTokens      int    `json:"textTokens"`
		ToolInputTokens int    `json:"toolInputTokens"`
		Method          string `json:"method"`
	} `json:"outputSplit"`
}

// Moment is one thing that happened in a run, for its timeline: a turn
// starting ("turn"), a tool call ending ("tool", with Failed and the error
// class in What), a guard's trip ("trip"), a notice of the SDK ("notice",
// its key), a session fork ("fork"), a mode switch ("mode") or the
// agent's end ("end").
type Moment struct {
	T      time.Time `json:"t"`
	Turn   int       `json:"turn"`
	Kind   string    `json:"kind"`
	What   string    `json:"what,omitempty"`
	Failed bool      `json:"failed,omitempty"`
}

// moment keeps one moment when the timeline is asked for.
func (ev *Events) moment(t time.Time, turn int, kind, what string, failed bool) {
	if ev.KeepTimeline {
		ev.Timeline = append(ev.Timeline, Moment{T: t, Turn: turn, Kind: kind, What: what, Failed: failed})
	}
}

// Notice is one notice of the SDK as it was shown, with its key.
type Notice struct {
	T       time.Time `json:"t"`
	Turn    int       `json:"turn"`
	Key     string    `json:"key"`
	Message string    `json:"message"`
}

// NoticeKey names a notice by what kind it is, so that the same notice in
// two runs counts under one metric (070 §2): its type and the kind and
// phase the SDK states ("status.reasoning_loop.cut"), else the reason it
// gives, else its type alone. Its sentence is never part of the key: it
// carries counts and quoted text, and `automaticum notices` shows it.
func NoticeKey(noticeType, kind, phase, reason string) string {
	switch {
	case kind != "" && phase != "":
		return noticeType + "." + kind + "." + phase
	case kind != "":
		return noticeType + "." + kind
	case reason != "":
		return reason
	}
	return noticeType
}

// chunkMark is how a "chunk" event starts. Chunks repeat the agent's events
// as text and make up half the stream; they are passed over unparsed.
var chunkMark = []byte(`"event":{"type":"chunk"`)

// scan is what is carried from line to line while a stream is read.
type scan struct {
	turn    int
	started map[string]time.Time // tool calls, by id
	inputs  map[string]string
	// What a block's deltas add up to, by kind, for a stream whose
	// "content_end" does not repeat the whole block.
	partial map[string]*bytes.Buffer
	// partialTurn is the turn each partial block started in, and
	// partialStart when.
	partialTurn  map[string]int
	partialStart map[string]time.Time
	// partialLast is when the last delta of each partial block came.
	partialLast map[string]time.Time
}

// flush keeps what a block's deltas added up to when its "content_end"
// never came, as an unfinished block: a turn the output limit cut, which
// the SDK retries as a new turn, or the last turn of a run the wall limit
// stopped. Before, the next block's end threw it away, and the detectors
// never read a 360,271-character text loop (bug-076).
func (sc *scan) flush(ev *Events) {
	for _, kind := range []string{"reasoning", "text"} {
		if buf := sc.partial[kind]; buf != nil && buf.Len() > 0 {
			ev.Blocks = append(ev.Blocks, Block{Turn: sc.partialTurn[kind], Kind: kind, Text: buf.String(), GivenTo: ev.Given.Len(), Unfinished: true,
				Start: sc.partialStart[kind], End: sc.partialLast[kind]})
			buf.Reset()
		}
	}
}

// ReadEvents reads a run's event stream: the JSON lines the runner printed.
func ReadEvents(r io.Reader) (*Events, error) {
	ev := &Events{Notices: map[string]int{}, GuardTrips: map[string]int{}, GuardCauses: map[string]int{}, NoticeTurns: map[string]int{}}
	return ev, ev.Read(r)
}

// Read adds one more stream to what was read: an imported run may have
// several.
func (ev *Events) Read(r io.Reader) error {
	// A caller may start from a bare Events; the counts need their maps.
	if ev.Notices == nil {
		ev.Notices = map[string]int{}
	}
	if ev.GuardTrips == nil {
		ev.GuardTrips = map[string]int{}
	}
	if ev.GuardCauses == nil {
		ev.GuardCauses = map[string]int{}
	}
	if ev.NoticeTurns == nil {
		ev.NoticeTurns = map[string]int{}
	}
	rd := bufio.NewReaderSize(r, 1<<20)
	sc := &scan{started: map[string]time.Time{}, inputs: map[string]string{}, partial: map[string]*bytes.Buffer{}, partialTurn: map[string]int{}, partialStart: map[string]time.Time{}, partialLast: map[string]time.Time{}}
	for {
		line, err := rd.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			ev.Lines++
			if !bytes.Contains(line[:min(len(line), 200)], chunkMark) {
				ev.line(line, sc)
			}
		}
		if err == io.EOF {
			sc.flush(ev)
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (ev *Events) line(line []byte, sc *scan) {
	var l eventLine
	if err := json.Unmarshal(line, &l); err != nil {
		ev.BadLines++
		return
	}
	if !l.T.IsZero() {
		if ev.First.IsZero() {
			ev.First = l.T
		} else if gap := float64(l.T.Sub(ev.Last).Milliseconds()); gap > ev.MaxGapMS {
			ev.MaxGapMS = gap
		}
		ev.Last = l.T
	}
	var a agentEvent
	switch {
	case l.K == "runner":
		switch l.Note {
		case "config_resolved":
			ev.Derived = l.Derived
		case "ask_question":
			// Protocol 1.
			ev.Questions++
		case "user_action_failed":
			ev.UserFailed++
		case "prepared":
			ev.LSP = len(l.LSP) > 0 && string(l.LSP) != "null"
		case "editor_diagnostics":
			ev.LSPEdits++
			if l.BlockChars > 0 {
				ev.LSPReports++
			}
			if l.Regressed {
				ev.LSPRegressed++
			}
		case "lsp_error", "editor_diagnostics_failed":
			ev.LSPErrors++
		case "fork":
			ev.Forks = append(ev.Forks, Fork{T: l.T, From: l.From, To: l.To, Checkpoint: l.Checkpoint, Workspace: l.Workspace, Restarted: l.Restarted})
			ev.ForkCalls = append(ev.ForkCalls, len(ev.Calls))
			ev.moment(l.T, sc.turn, "fork", fmt.Sprintf("back to checkpoint %d", l.Checkpoint), false)
		case "guard_trip":
			key := l.Guard
			if l.Phase != "" {
				key += "." + l.Phase
			}
			ev.GuardTrips[key]++
			if l.Cause != "" {
				ev.GuardCauses[key+"."+l.Cause]++
			}
			ev.Trips = append(ev.Trips, Trip{T: l.T, Turn: sc.turn, Guard: l.Guard, Phase: l.Phase, Cause: l.Cause})
			ev.moment(l.T, sc.turn, "trip", strings.TrimSuffix(strings.Join([]string{l.Guard, l.Phase, l.Cause}, " "), " "), false)
			if l.Guard == "reasoning_loop" && (l.Phase == "cut" || l.Phase == "aborted") {
				// The stream so far was thrown away: keep it apart, and
				// start the block again with what is asked for next.
				kind := "reasoning"
				if l.Cause == "answer_loop" {
					kind = "text"
				}
				if buf := sc.partial[kind]; buf != nil && buf.Len() > 0 {
					ev.CutBlocks = append(ev.CutBlocks, Block{Turn: sc.turn, Kind: kind, Text: buf.String(), GivenTo: ev.Given.Len()})
					buf.Reset()
				}
			}
		case "guard_stop":
			ev.GuardStop = l.Guard
			if l.Phase != "" {
				ev.GuardStop += "." + l.Phase
			}
			if l.Cause != "" {
				ev.GuardStopCause = ev.GuardStop + "." + l.Cause
			}
		case "mode_switched":
			ev.moment(l.T, sc.turn, "mode", "switched by the "+map[bool]string{true: "agent", false: "user"}[l.Source == "agent"], false)
			if l.Source == "agent" {
				ev.AgentModeSwitches++
			} else {
				ev.ModeSwitches++
			}
		}
		return
	case l.K == "user":
		if l.On == "question" {
			ev.Questions++
		}
		if l.Rule > 0 && l.On != "resume" {
			ev.UserActions++
		}
		if l.On == "approval" && l.Approved != nil && !*l.Approved {
			ev.UserDenials++
		}
		return
	case l.K == "event":
		var e struct {
			Type    string `json:"type"`
			Payload struct {
				Event agentEvent `json:"event"`
			} `json:"payload"`
		}
		if len(l.Event) == 0 {
			return
		}
		if json.Unmarshal(l.Event, &e) != nil {
			ev.BadLines++
			return
		}
		if e.Type != "agent_event" {
			return
		}
		a = e.Payload.Event
	case l.K == "" && l.Type == "agent_event" && len(l.Event) > 0:
		if json.Unmarshal(l.Event, &a) != nil {
			ev.BadLines++
			return
		}
	default:
		return
	}
	switch a.Type {
	case "iteration_start":
		sc.flush(ev)
		sc.turn = a.Iteration
		if !l.T.IsZero() {
			ev.TurnStarts = append(ev.TurnStarts, l.T)
			ev.turnOf = append(ev.turnOf, a.Iteration)
		}
		if a.Iteration > ev.Iterations {
			ev.Iterations = a.Iteration
		}
		ev.Struggle = append(ev.Struggle, detect.StruggleEvent{Kind: "turn", Turn: a.Iteration})
		ev.moment(l.T, a.Iteration, "turn", "", false)
	case "iteration_end":
		ev.Struggle = append(ev.Struggle, detect.StruggleEvent{Kind: "turn_end", Turn: a.Iteration})
	case "content_start":
		switch a.ContentType {
		case "tool":
			if a.ToolCallID != "" {
				sc.started[a.ToolCallID] = l.T
				sc.inputs[a.ToolCallID] = compact(a.Input)
			}
			ev.Struggle = append(ev.Struggle, detect.StruggleEvent{Kind: "call", Turn: sc.turn, Tool: a.ToolName, Path: editorPath(a.Input)})
		case "reasoning", "text":
			buf := sc.partial[a.ContentType]
			if buf == nil {
				buf = &bytes.Buffer{}
				sc.partial[a.ContentType] = buf
			}
			if buf.Len() == 0 {
				sc.partialTurn[a.ContentType], sc.partialStart[a.ContentType] = sc.turn, l.T
			}
			sc.partialLast[a.ContentType] = l.T
			buf.WriteString(a.Reasoning + a.Text)
		}
	case "content_end":
		switch a.ContentType {
		case "reasoning", "text":
			text := a.Reasoning + a.Text
			start := l.T
			if buf := sc.partial[a.ContentType]; buf != nil {
				if text == "" {
					text = buf.String()
				}
				if buf.Len() > 0 {
					start = sc.partialStart[a.ContentType]
				}
				buf.Reset()
			}
			ev.Blocks = append(ev.Blocks, Block{Turn: sc.turn, Kind: a.ContentType, Text: text, GivenTo: ev.Given.Len(), Start: start, End: l.T})
			ev.Struggle = append(ev.Struggle, detect.StruggleEvent{Kind: a.ContentType, Turn: sc.turn, Text: text})
			t := ev.turn(sc.turn)
			if a.ContentType == "reasoning" {
				t.Reasoning = true
			} else {
				t.TextChars += len([]rune(strings.TrimSpace(text)))
			}
			if strings.TrimSpace(text) != "" {
				ev.next(a.ContentType, "")
			}
		case "tool":
			call := store.ToolCall{Turn: sc.turn, Seq: len(ev.ToolCalls), Tool: a.ToolName, OK: true}
			if failed(&a) {
				call.OK, call.ErrorClass = false, ErrorClass(&a)
			}
			ev.turn(sc.turn).Calls++
			ev.next("tool", a.ToolName)
			switch t0, ok := sc.started[a.ToolCallID]; {
			case a.DurationMS != nil:
				call.DurationMS = int64(*a.DurationMS)
			case ok && !l.T.IsZero():
				call.DurationMS = l.T.Sub(t0).Milliseconds()
			}
			input := compact(a.Input)
			if input == "" {
				input = sc.inputs[a.ToolCallID]
			}
			delete(sc.started, a.ToolCallID)
			delete(sc.inputs, a.ToolCallID)
			ev.ToolCalls = append(ev.ToolCalls, call)
			ev.moment(l.T, sc.turn, "tool", a.ToolName+map[bool]string{true: ": " + call.ErrorClass, false: ""}[!call.OK], !call.OK)
			givenStrings(&ev.Given, a.Output)
			loopFailed := guardFailed(&a)
			ev.Calls = append(ev.Calls, detect.Call{Turn: sc.turn, Tool: a.ToolName, Input: input, Result: compact(a.Output),
				Failed: loopFailed, Refused: loopFailed && detect.Refusal(resultErrors(a.Output)), Paths: callPaths(input)})
			// The struggle detector reads a failure as the event's error,
			// else as what the result states.
			_, struggleFailed := detect.StruggleFailure(a.Output)
			if e := bytes.TrimSpace(a.Error); len(e) > 0 && !bytes.Equal(e, []byte("null")) {
				struggleFailed = true
			}
			ev.Struggle = append(ev.Struggle, detect.StruggleEvent{Kind: "tool", Turn: sc.turn, Tool: a.ToolName, Failed: struggleFailed})
		}
	case "usage":
		if ev.FirstTokenMS == 0 {
			ev.FirstTokenMS = a.Timings.FirstTokenMS
		}
		if s := a.OutputSplit; s != nil {
			ev.ReasoningTokens += s.ReasoningTokens
			ev.TextTokens += s.TextTokens
			ev.ToolInputTokens += s.ToolInputTokens
			ev.SplitMethod = s.Method
			ev.TurnTokens = append(ev.TurnTokens, TurnTokens{Turn: sc.turn, Reasoning: s.ReasoningTokens, Text: s.TextTokens, ToolInput: s.ToolInputTokens})
		}
	case "notice":
		key := NoticeKey(a.NoticeType, a.Metadata.Kind, a.Metadata.Phase, a.Reason)
		ev.Notices[key]++
		if _, ok := ev.NoticeTurns[key]; !ok {
			ev.NoticeTurns[key] = sc.turn
		}
		// A compaction is announced when it starts and when it ends; the
		// SDK counts it once, at "completed", by these kinds. A notice
		// without a kind, from an older SDK, is counted by its reason.
		switch k := a.Metadata.Kind; {
		case k == "auto_compaction" || k == "manual_compaction" || k == "overflow_recovery_compaction":
			if a.Metadata.Phase == "completed" {
				ev.Compactions++
				ev.CompactionTimes = append(ev.CompactionTimes, l.T)
				md := a.Metadata
				c := Compaction{Turn: sc.turn, Kind: k, Cause: md.Cause}
				for _, f := range []struct {
					to   *float64
					from *float64
				}{{&c.TokensBefore, md.TokensBefore}, {&c.TokensAfter, md.TokensAfter}, {&c.DurationMS, md.DurationMS}} {
					if f.from != nil {
						*f.to = *f.from
					}
				}
				ev.CompactionLog = append(ev.CompactionLog, c)
				ev.Struggle = append(ev.Struggle, detect.StruggleEvent{Kind: "compaction", Turn: sc.turn})
			}
		case k == "" && strings.Contains(a.Reason, "compact"):
			ev.Compactions++
			ev.CompactionTimes = append(ev.CompactionTimes, l.T)
			ev.Struggle = append(ev.Struggle, detect.StruggleEvent{Kind: "compaction", Turn: sc.turn})
		}
		ev.transaction(&a, sc.turn)
		// An escalation's end: an expert's reply, or a failure (the
		// SDK's escalation_failed); escalation_ended closes either.
		switch md := a.Metadata; md.Kind {
		case "expert_reply", "escalation_failed":
			e := Escalation{Turn: sc.turn, Failed: md.Kind == "escalation_failed"}
			if md.Changed != nil {
				e.Changed = *md.Changed
			}
			if md.Usage != nil {
				e.Usage = *md.Usage
			}
			ev.Escalations = append(ev.Escalations, e)
		}
		if ev.KeepNotices {
			ev.NoticeLog = append(ev.NoticeLog, Notice{T: l.T, Turn: sc.turn, Key: key, Message: a.Message})
		}
		// Every notice but the context report each turn carries.
		if a.Metadata.Kind != "context_breakdown" {
			ev.moment(l.T, sc.turn, "notice", key, false)
		}
	case "done":
		ev.AgentEnd = a.Reason
		ev.next("end", "")
		ev.moment(l.T, sc.turn, "end", a.Reason, false)
	}
}

// transaction reads the change protocol's notices. The stream does not say
// when a transaction opens: the first notice that names it stands for that.
func (ev *Events) transaction(a *agentEvent, turn int) {
	md := a.Metadata
	if md.Transaction == nil || !strings.HasPrefix(md.Kind, "atomic_") {
		return
	}
	id := *md.Transaction
	ev.Struggle = append(ev.Struggle, detect.StruggleEvent{Kind: "transaction", Turn: turn, Transaction: id})
	switch md.Kind {
	case "atomic_plan":
		ev.Plans++
	case "atomic_empty_attempt":
		ev.EmptyAttempts++
		ev.Struggle = append(ev.Struggle, detect.StruggleEvent{Kind: "empty", Turn: turn, Transaction: id})
	case "atomic_transaction":
		out := bytes.TrimSpace(md.Output)
		t := Transaction{Turn: turn, ID: id, Kept: md.Kept != nil && *md.Kept, Carried: md.Carried, Source: md.Source,
			Checked: len(out) > 0 && !bytes.Equal(out, []byte("null")), FilesPutBack: md.FilesPutBack}
		if md.ElapsedMS != nil {
			t.ElapsedMS = *md.ElapsedMS
		}
		ev.Transactions = append(ev.Transactions, t)
		if !t.Kept && !t.Carried {
			ev.Struggle = append(ev.Struggle, detect.StruggleEvent{Kind: "discarded", Turn: turn, Transaction: id})
		}
	}
}

// editorPath is the file a file-changing call names, as the SDK reads it
// (its `editorFilePath`): file_path, else path.
func editorPath(raw json.RawMessage) string {
	var in struct {
		FilePath any `json:"file_path"`
		Path     any `json:"path"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	if s, ok := in.FilePath.(string); ok && s != "" {
		return s
	}
	if s, ok := in.Path.(string); ok {
		return s
	}
	return ""
}

// turn is what a turn held so far.
func (ev *Events) turn(n int) *TurnActs {
	if ev.Turns == nil {
		ev.Turns = map[int]*TurnActs{}
	}
	t := ev.Turns[n]
	if t == nil {
		t = &TurnActs{}
		ev.Turns[n] = t
	}
	return t
}

// next gives every trip that has no next act yet this one.
func (ev *Events) next(kind, tool string) {
	for i := len(ev.Trips) - 1; i >= 0 && ev.Trips[i].Next == ""; i-- {
		ev.Trips[i].Next, ev.Trips[i].Tool = kind, tool
	}
}

// ErrorClass is the class of a failed tool call: the head of the tool's
// own error sentence (the SDK's tools say what went wrong in fixed words,
// "No replacement performed", "Read before editing"), with the tool's
// "<name> operation failed:" prefix taken off, cut at the first ": ",
// ". " or line end, digits written as N and at most 60 characters.
// A failure that says nothing is "tool_error" (070 §2).
func ErrorClass(a *agentEvent) string {
	msg := errorText(a.Error)
	if msg == "" {
		msg = resultError(a.Output)
	}
	msg = strings.TrimSpace(msg)
	// An error that is itself a JSON document with an error in it.
	if strings.HasPrefix(msg, "{") {
		if inner := errorText(json.RawMessage(msg)); inner != "" {
			msg = strings.TrimSpace(inner)
		}
	}
	if i := strings.Index(msg, " operation failed: "); i >= 0 && i < 40 {
		msg = msg[i+len(" operation failed: "):]
	}
	for _, sep := range []string{": ", ". ", "\n"} {
		if i := strings.Index(msg, sep); i >= 0 {
			msg = msg[:i]
		}
	}
	msg = strings.TrimSuffix(strings.TrimSuffix(msg, "."), ":")
	var b strings.Builder
	for _, r := range msg {
		switch {
		case r >= '0' && r <= '9':
			if !strings.HasSuffix(b.String(), "N") {
				b.WriteRune('N')
			}
		case r == '\n' || r == '\t':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if r := []rune(out); len(r) > 60 {
		out = strings.TrimSpace(string(r[:60]))
	}
	if out == "" {
		return "tool_error"
	}
	return out
}

// errorText reads an error given as a string or as an object with a
// message.
func errorText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(raw, &o) == nil {
		if o.Message != "" {
			return o.Message
		}
		return o.Error
	}
	return ""
}

// resultError is the error a tool's result states, in its one result, the
// first failed one of a list, or a JSON document inside a string.
func resultError(raw json.RawMessage) string {
	type result struct {
		Error json.RawMessage `json:"error"`
	}
	raw = bytes.TrimSpace(raw)
	switch {
	case len(raw) == 0:
		return ""
	case raw[0] == '[':
		var list []result
		json.Unmarshal(raw, &list)
		for _, r := range list {
			if e := errorText(r.Error); e != "" {
				return e
			}
		}
	case raw[0] == '{':
		var one result
		json.Unmarshal(raw, &one)
		return errorText(one.Error)
	case raw[0] == '"':
		var s string
		if json.Unmarshal(raw, &s) == nil && strings.HasPrefix(s, "{") {
			return resultError(json.RawMessage(s))
		}
	}
	return ""
}

// givenStrings adds a tool result's strings to what the model was given,
// as the SDK's guard reads a tool result: every non-empty string in it,
// six levels deep, each on its own line; a result that is not JSON is its
// own text.
func givenStrings(b *strings.Builder, raw json.RawMessage) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		v = string(raw)
	}
	var walk func(x any, depth int)
	walk = func(x any, depth int) {
		switch t := x.(type) {
		case string:
			if t != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(t)
			}
		case []any:
			if depth < 6 {
				for _, e := range t {
					walk(e, depth+1)
				}
			}
		case map[string]any:
			if depth < 6 {
				keys := make([]string, 0, len(t))
				for k := range t {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					walk(t[k], depth+1)
				}
			}
		}
	}
	walk(v, 0)
}

// guardFailed is a call's outcome as the SDK's loop guard reads it: failed
// when the result is an error, when every item of it says success false,
// or when one says it regressed.
func guardFailed(a *agentEvent) bool {
	if errorText(a.Error) != "" {
		return true
	}
	type item struct {
		Success   *bool `json:"success"`
		Regressed bool  `json:"regressed"`
	}
	var items []item
	out := bytes.TrimSpace(a.Output)
	if len(out) > 0 && out[0] == '"' {
		var s string
		if json.Unmarshal(out, &s) == nil {
			out = []byte(strings.TrimSpace(s))
		}
	}
	switch {
	case len(out) == 0:
		return false
	case out[0] == '[':
		json.Unmarshal(out, &items)
	case out[0] == '{':
		var one item
		if json.Unmarshal(out, &one) == nil {
			items = []item{one}
		}
	}
	if len(items) == 0 {
		return false
	}
	all := true
	for _, it := range items {
		if it.Regressed {
			return true
		}
		all = all && it.Success != nil && !*it.Success
	}
	return all
}

// resultErrors is every error a tool's result states, joined.
func resultErrors(raw json.RawMessage) string {
	type result struct {
		Error json.RawMessage `json:"error"`
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			raw = []byte(strings.TrimSpace(s))
		}
	}
	var list []result
	switch {
	case len(raw) == 0:
	case raw[0] == '[':
		json.Unmarshal(raw, &list)
	case raw[0] == '{':
		var one result
		json.Unmarshal(raw, &one)
		list = []result{one}
	}
	var b strings.Builder
	for _, r := range list {
		b.WriteString(errorText(r.Error))
		b.WriteByte('\n')
	}
	return b.String()
}

// compact gives a call's arguments one spelling, so that the same arguments
// compare equal however they were spaced.
func compact(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v) // maps are written with sorted keys
	return string(b)
}

// failed reports whether a tool call's end says it failed: an error on the
// event itself, or a result that says so.
func failed(a *agentEvent) bool {
	if e := bytes.TrimSpace(a.Error); len(e) > 0 && !bytes.Equal(e, []byte("null")) && !bytes.Equal(e, []byte(`""`)) {
		return true
	}
	type result struct {
		Success *bool           `json:"success"`
		Error   json.RawMessage `json:"error"`
	}
	bad := func(r result) bool {
		if r.Success != nil && !*r.Success {
			return true
		}
		e := bytes.TrimSpace(r.Error)
		return len(e) > 0 && !bytes.Equal(e, []byte("null")) && !bytes.Equal(e, []byte(`""`))
	}
	out := bytes.TrimSpace(a.Output)
	switch {
	case len(out) == 0:
		return false
	case out[0] == '[':
		var list []result
		if json.Unmarshal(out, &list) == nil {
			for _, r := range list {
				if bad(r) {
					return true
				}
			}
		}
	case out[0] == '{':
		var one result
		return json.Unmarshal(out, &one) == nil && bad(one)
	case out[0] == '"':
		// Some tools answer with a JSON document inside a string.
		var s string
		if json.Unmarshal(out, &s) == nil && len(s) > 0 && s[0] == '{' {
			var one result
			return json.Unmarshal([]byte(s), &one) == nil && bad(one)
		}
	}
	return false
}
