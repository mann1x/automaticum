package ingest

import (
	"time"
)

// WireAnswers adds to the run's blocks what the model answered on the
// wire and the event stream never had: an answer the SDK did not stream
// as events, such as its retry after the output limit
// (max_tokens_turn_recovery). A client's retry wrote 94,187 characters of
// reasoning and 106,517 of text, the same loop as the turn it retried, in
// ten minutes of an event stream with no line (bug-077).
//
// An agent request's answer is the event stream's when a block of the
// same kind was printed while the request ran; by time, not by text, as
// a retry may begin as the turn it retries did. An answer of neither is
// a block read from the wire, in the turn that had started when it was
// asked. It is added once: a second call adds nothing. A stream without
// times adds nothing, as every answer would look unprinted.
func (ev *Events) WireAnswers(reqs []Request) int {
	if ev.wired {
		return 0
	}
	ev.wired = true
	// A stream without times cannot say what it printed when.
	if ev.First.IsZero() {
		return 0
	}
	added := 0
	for _, q := range reqs {
		if !q.Agent || q.Failed || q.Start.IsZero() {
			continue
		}
		end := q.Start.Add(time.Duration(q.MS * float64(time.Millisecond)))
		for _, part := range []struct{ kind, text string }{{"reasoning", q.Reasoning}, {"text", q.Visible}} {
			if part.text == "" || ev.printed(part.kind, q.Start, end) {
				continue
			}
			ev.Blocks = append(ev.Blocks, Block{Turn: ev.turnAt(q.Start), Kind: part.kind, Text: part.text,
				GivenTo: ev.givenAt(q.Start), Start: q.Start, End: end, Request: q.Seq})
			added++
		}
	}
	return added
}

// printed says whether a block of a kind was printed between two times.
func (ev *Events) printed(kind string, from, to time.Time) bool {
	for _, b := range ev.Blocks {
		if b.Kind == kind && b.Request == 0 && !b.Start.IsZero() && !b.Start.After(to) && !b.End.Before(from) {
			return true
		}
	}
	return false
}

// turnAt is the turn that had started at a time.
func (ev *Events) turnAt(t time.Time) int {
	turn := 0
	for i, s := range ev.TurnStarts {
		if !s.After(t) && i < len(ev.turnOf) {
			turn = ev.turnOf[i]
		}
	}
	return turn
}

// givenAt is how much of Given came before the last block ended by a time.
func (ev *Events) givenAt(t time.Time) int {
	given := 0
	for _, b := range ev.Blocks {
		if b.Request == 0 && !b.End.IsZero() && !b.End.After(t) {
			given = b.GivenTo
		}
	}
	return given
}
