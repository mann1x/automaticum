package detect

import (
	"encoding/json"
	"testing"
)

// turns builds the events of turns from..to, each with the reasoning and
// the tool calls given (true for a failed call).
func struggleTurns(from, to int, reasoning string, calls ...bool) []StruggleEvent {
	var out []StruggleEvent
	for t := from; t <= to; t++ {
		out = append(out, StruggleEvent{Kind: "turn", Turn: t}, StruggleEvent{Kind: "reasoning", Turn: t, Text: reasoning})
		for _, failed := range calls {
			out = append(out, StruggleEvent{Kind: "tool", Turn: t, Tool: "read_files", Failed: failed})
		}
		out = append(out, StruggleEvent{Kind: "turn_end", Turn: t})
	}
	return out
}

func TestStruggleFailures(t *testing.T) {
	// Twenty quiet turns, then a failure a turn while the reasoning says it
	// is stuck: one short of six failures it nudges, at six it suggests.
	ev := struggleTurns(1, 19, "reading the file to see what it does", false)
	ev = append(ev, struggleTurns(20, 30, "I'm stuck, this keeps failing", true)...)
	got := Struggle(ev, nil)
	if len(got) < 2 {
		t.Fatalf("firings %+v", got)
	}
	// The window at turn T is turns T-9..T and turns 11..19 hold no
	// failure, so the fifth failure comes with turn 24's call.
	if f := got[0]; f.Kind != StruggleNudge || f.Reason != "failures" || f.Turn != 24 || f.FailedCalls != 5 {
		t.Errorf("first %+v", f)
	}
	var suggest *StruggleFiring
	for i := range got {
		if got[i].Kind == StruggleSuggest {
			suggest = &got[i]
			break
		}
	}
	if suggest == nil || suggest.Turn != 25 || suggest.FailedCalls != 6 || suggest.Distress < StruggleDistressHits {
		t.Fatalf("suggest %+v in %+v", suggest, got)
	}
	// Once a transaction (none here: one for the whole run), it fires once.
	n := 0
	for _, f := range got {
		if f.Kind == StruggleSuggest {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d suggestions, want 1 without a new transaction", n)
	}
}

func TestStruggleBeforeMinIteration(t *testing.T) {
	// Failures and distress before turn 20 say nothing.
	if got := Struggle(struggleTurns(1, 19, "I'm stuck, I'm confused", true, true), nil); len(got) != 0 {
		t.Errorf("firings %+v", got)
	}
}

func TestStruggleTransactions(t *testing.T) {
	// A new transaction lets it suggest again, at most twice a task, and
	// only on new signals or files.
	ev := struggleTurns(1, 19, "fine", false)
	ev = append(ev, struggleTurns(20, 26, "I'm stuck, keeps failing", true)...)
	ev = append(ev, StruggleEvent{Kind: "call", Turn: 26, Tool: "editor", Path: "a.go"})
	ev = append(ev, StruggleEvent{Kind: "transaction", Turn: 27, Transaction: 2})
	ev = append(ev, struggleTurns(27, 28, "I'm stuck, keeps failing", true)...)
	ev = append(ev, StruggleEvent{Kind: "call", Turn: 28, Tool: "editor", Path: "b.go"})
	ev = append(ev, StruggleEvent{Kind: "transaction", Turn: 29, Transaction: 3})
	ev = append(ev, struggleTurns(29, 30, "I'm stuck, keeps failing", true)...)
	ev = append(ev, StruggleEvent{Kind: "call", Turn: 30, Tool: "editor", Path: "c.go"})
	ev = append(ev, StruggleEvent{Kind: "transaction", Turn: 31, Transaction: 4})
	ev = append(ev, struggleTurns(31, 32, "I'm stuck, keeps failing", true)...)
	n := 0
	for _, f := range Struggle(ev, nil) {
		if f.Kind == StruggleSuggest {
			n++
		}
	}
	if n != StruggleMaxPerTask {
		t.Errorf("%d suggestions, want %d", n, StruggleMaxPerTask)
	}
	// Two attempts thrown away nudge, before turn 20 too.
	got := Struggle([]StruggleEvent{{Kind: "turn", Turn: 3}, {Kind: "discarded", Turn: 3}, {Kind: "empty", Turn: 3}, {Kind: "turn_end", Turn: 3}}, nil)
	if len(got) != 1 || got[0].Reason != "transactions" || got[0].ThrownAway != 2 {
		t.Errorf("thrown away %+v", got)
	}
}

func TestStruggleEditStreak(t *testing.T) {
	edit := func(turn int, failed bool) []StruggleEvent {
		return []StruggleEvent{{Kind: "turn", Turn: turn}, {Kind: "tool", Turn: turn, Tool: "editor", Failed: failed}, {Kind: "turn_end", Turn: turn}}
	}
	var ev []StruggleEvent
	for t := 1; t <= 3; t++ {
		ev = append(ev, edit(t, true)...)
	}
	got := Struggle(ev, nil)
	if len(got) != 1 || got[0].Reason != "edit-streak" || got[0].EditStreak != 3 || got[0].Turn != 3 {
		t.Fatalf("streak %+v", got)
	}
	// A success ends the streak; a compaction clears it; a failed
	// write_to_file is not part of it.
	ev = append(ev, edit(4, false)...)
	ev = append(ev, edit(5, true)...)
	ev = append(ev, StruggleEvent{Kind: "compaction", Turn: 5})
	ev = append(ev, edit(6, true)...)
	ev = append(ev, StruggleEvent{Kind: "tool", Turn: 6, Tool: "write_to_file", Failed: true})
	ev = append(ev, edit(7, true)...)
	if got := Struggle(ev, nil); len(got) != 1 {
		t.Errorf("after reset %+v", got)
	}
}

func TestStruggleTextAfterReasoning(t *testing.T) {
	// The visible text counts only while the model has not reasoned.
	text := func(from, to int) []StruggleEvent {
		var out []StruggleEvent
		for t := from; t <= to; t++ {
			out = append(out, StruggleEvent{Kind: "turn", Turn: t}, StruggleEvent{Kind: "text", Turn: t, Text: "I'm stuck, keeps failing"},
				StruggleEvent{Kind: "tool", Turn: t, Tool: "read_files", Failed: true}, StruggleEvent{Kind: "turn_end", Turn: t})
		}
		return out
	}
	ev := append(struggleTurns(1, 19, "fine", false), text(20, 26)...)
	for _, f := range Struggle(ev, nil) {
		if f.Kind == StruggleSuggest {
			t.Errorf("text read after reasoning: %+v", f)
		}
	}
	quiet := []StruggleEvent{}
	for t := 1; t <= 19; t++ {
		quiet = append(quiet, StruggleEvent{Kind: "turn", Turn: t}, StruggleEvent{Kind: "text", Turn: t, Text: "fine"}, StruggleEvent{Kind: "turn_end", Turn: t})
	}
	found := false
	for _, f := range Struggle(append(quiet, text(20, 26)...), nil) {
		found = found || f.Kind == StruggleSuggest
	}
	if !found {
		t.Error("text without reasoning not read")
	}
}

func TestStruggleHedging(t *testing.T) {
	// Hedging at the opening rate or above, with six failures, suggests
	// without distress.
	ev := struggleTurns(1, 10, "hmm wait the loop starts here and runs to the end of the list", false)
	ev = append(ev, struggleTurns(11, 19, "reading", false)...)
	ev = append(ev, struggleTurns(20, 26, "hmm wait wait this does not look right at all", true)...)
	var s *StruggleFiring
	got := Struggle(ev, nil)
	for i := range got {
		if got[i].Kind == StruggleSuggest {
			s = &got[i]
			break
		}
	}
	if s == nil || s.HedgingRatio == nil || *s.HedgingRatio < 1 || s.Distress != 0 {
		t.Fatalf("hedging %+v", s)
	}
}

func TestStruggleFailure(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
		msg  string
	}{
		{`{"success":false,"error":"No replacement performed"}`, true, "No replacement performed"},
		{`{"success":false}`, true, "the call did not succeed"},
		{`{"error":"boom"}`, true, "boom"},
		{`{"success":true,"error":"ignored"}`, false, ""},
		{`{"result":{"success":false,"error":"inner"}}`, true, "inner"},
		{`{"success":null,"result":{"success":false}}`, false, ""},
		{`[{"success":false,"error":"a"},{"success":false,"error":"b"}]`, true, "a"},
		{`[{"success":false,"error":"a"},{"success":true}]`, false, ""},
		{`[]`, false, ""},
		{`"{\"success\":false,\"error\":\"in a string\"}"`, true, "in a string"},
		{`"plain text output"`, false, ""},
		{`{"error":"  "}`, false, ""},
	} {
		msg, ok := StruggleFailure(json.RawMessage(c.in))
		if ok != c.want || msg != c.msg {
			t.Errorf("%s: %q %v, want %q %v", c.in, msg, ok, c.msg, c.want)
		}
	}
}
