package ingest

import (
	"fmt"
	"strings"
)

// CanaryRequests is how many finished requests of a batch the
// first-generations canary judges (050 §3.3). Decided by the owner, D84.
const CanaryRequests = 5

// CanaryVerdict is what the canary says of the first finished requests of
// a batch.
type CanaryVerdict struct {
	// Requests are the requests judged, as "run#seq".
	Requests []string `json:"requests"`
	// Problems lists each request that is empty, was ended by length, or
	// shows one of the model's control tokens in its visible text. Any
	// problem stops the batch.
	Problems []string `json:"problems,omitempty"`
	// Unchecked says what the canary could not look at, and why.
	Unchecked []string `json:"unchecked,omitempty"`
}

// CanaryRequest is a finished agent request with the run it belongs to.
type CanaryRequest struct {
	Run string
	Request
}

// Finished keeps the requests of the agent's loop that got an answer: the
// SDK's own probes and requests that failed on the way are not the model's
// generations.
func Finished(run string, reqs []Request) []CanaryRequest {
	var out []CanaryRequest
	for _, q := range reqs {
		if q.Agent && !q.Failed {
			out = append(out, CanaryRequest{Run: run, Request: q})
		}
	}
	return out
}

// Canary judges the first CanaryRequests of reqs, or returns nil while
// there are fewer. controls are the model's own control tokens, as its
// tokenizer marks them; nil means they could not be read, and that check is
// unchecked rather than passed. A length band is checked only where a task
// declares one; no task does yet (D84).
func Canary(reqs []CanaryRequest, controls []string) *CanaryVerdict {
	if len(reqs) < CanaryRequests {
		return nil
	}
	v := &CanaryVerdict{}
	if controls == nil {
		v.Unchecked = append(v.Unchecked, "control tokens: the model's tokenizer could not be read")
	}
	v.Unchecked = append(v.Unchecked, "answer length: the task declares no band")
	for _, q := range reqs[:CanaryRequests] {
		name := fmt.Sprintf("%s#%d", q.Run, q.Seq)
		v.Requests = append(v.Requests, name)
		if q.Empty {
			v.Problems = append(v.Problems, name+": empty answer")
		}
		if q.DoneReason == "length" {
			v.Problems = append(v.Problems, name+": ended by length")
		}
		for _, t := range controls {
			if t != "" && strings.Contains(q.Visible, t) {
				v.Problems = append(v.Problems, fmt.Sprintf("%s: control token %s in the visible text", name, t))
			}
		}
	}
	return v
}
