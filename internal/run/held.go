package run

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/mann1x/automaticum/internal/human"
)

// A held stream is a request the SDK gave up on, presumed dead, while the
// server was still decoding for it: the server's parser held back what the
// model wrote (a tool call its grammar rejects, a channel tag it does not
// know), so nothing reached the wire, keepalives included, until the SDK's
// silence ceiling cancelled the request. The server was healthy and the
// stall was the model's own output, so such a run is not ABORTED: the
// oracle decides it, with the held stream in the reason (D136, amending
// 050 §4). It needs the proof that the server was decoding, the readings
// of /slots; the SDK's message alone stays a server fault.

var (
	// silentRE is the SDK's message when it gives the server up
	// (OpencotiServerSilentError, @cerebriline/llms 4.100.266 on).
	silentRE  = regexp.MustCompile(`OpencotiServerSilentError|sent nothing for \d+s \(\d+ keepalive periods\); presumed dead`)
	silentSRE = regexp.MustCompile(`sent nothing for (\d+)s`)
)

// heldErrorChars is how much of the error a Held keeps.
const heldErrorChars = 300

// Held is what is known of a held stream: the silence the SDK reported
// and, from the readings of /slots, the server's task, how many tokens it
// had decoded by the last reading and whether the count still grew during
// the silence.
type Held struct {
	SilentS float64 `json:"silent_s,omitempty"`
	// By is how it is known: "slots", the readings of /slots; "server
	// log", the server's log an operator read (Source says which); or
	// "error text", the SDK's message alone.
	By      string `json:"by"`
	Source  string `json:"source,omitempty"`
	Slot    *int   `json:"slot,omitempty"`
	Task    int64  `json:"task,omitempty"`
	Decoded int    `json:"n_decoded,omitempty"`
	// DecodedSilent is how many of them were decoded during the silence.
	DecodedSilent int    `json:"n_decoded_in_silence,omitempty"`
	Grew          bool   `json:"grew,omitempty"`
	Readings      int    `json:"readings,omitempty"`
	Error         string `json:"error"`
}

// HeldStream says whether an agent's error is the SDK's silence ceiling,
// and what the readings of /slots say of it; nil if it is not.
func HeldStream(agentError string, slots []SlotSample) *Held {
	if !silentRE.MatchString(agentError) {
		return nil
	}
	text := agentError
	if e := []rune(text); len(e) > heldErrorChars {
		text = string(e[:heldErrorChars]) + "…"
	}
	h := &Held{By: "error text", Error: text}
	if m := silentSRE.FindStringSubmatch(agentError); m != nil {
		h.SilentS, _ = strconv.ParseFloat(m[1], 64)
	}
	// The held task is the last the server was processing; its readings
	// in the silence before its last show whether it still decoded.
	last := -1
	for i, s := range slots {
		if s.Processing {
			last = i
		}
	}
	if last < 0 {
		return h
	}
	end := slots[last]
	from := end.At.Add(-time.Duration(h.SilentS * float64(time.Second)))
	first := -1
	for i, s := range slots[:last+1] {
		if s.Slot == end.Slot && s.Task == end.Task && s.Processing {
			if first < 0 && !s.At.Before(from) {
				first = i
			}
			h.Readings++
		}
	}
	slot := end.Slot
	h.By, h.Slot, h.Task, h.Decoded = "slots", &slot, end.Task, end.Decoded
	if first >= 0 && h.SilentS > 0 {
		h.DecodedSilent = end.Decoded - slots[first].Decoded
		h.Grew = h.DecodedSilent > 0
	}
	return h
}

// Proven says the server was decoding through the silence, which is what
// makes it a held stream for the verdict: the readings of /slots, or the
// server's log as an operator recorded it (`run held`).
func (h *Held) Proven() bool {
	return h != nil && (h.By == "slots" && h.Grew || h.By == "server log" && h.Decoded > 0)
}

// Reason is how the verdict's reason names it.
func (h *Held) Reason() string {
	return fmt.Sprintf("held stream: model output unparseable for %s (n_decoded %s)",
		human.Seconds(h.SilentS), human.Count(int64(h.Decoded)))
}
