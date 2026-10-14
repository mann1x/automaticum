package ingest

// CapHit is the finding that a request of the agent's loop was ended by its
// output limit (070 §5): in an agent loop the mid-loop cap is the failure,
// so every turn counts, not only the last.
const CapHit = "cap_hit"

// CapTolerance is how many tokens short of a declared limit a completion
// still counts as capped: a server may stop a few tokens early. The number
// is omnimergekit's (070 §5).
const CapTolerance = 16

// Capped is one request ended by its limit.
type Capped struct {
	Seq    int `json:"seq"`
	Tokens int `json:"tokens"`
	// Limit is the output limit the request sent; 0 when it sent none.
	Limit int `json:"limit,omitempty"`
	// By says how it is known: the server said "length", or the
	// completion reached the declared limit.
	By string `json:"by"`
}

// CapHits lists the requests of the agent's loop that ended at their
// output limit: those the server says it ended by length, and those whose
// completion reached the limit the request declared (num_predict) less the
// tolerance, for a server that does not say so.
func CapHits(agent []Request) []Capped {
	var out []Capped
	for _, q := range agent {
		if !q.Agent || q.Failed {
			continue
		}
		limit := 0
		if v, ok := q.Options["num_predict"].(float64); ok && v > 0 {
			limit = int(v)
		}
		switch {
		case q.DoneReason == "length":
			out = append(out, Capped{Seq: q.Seq, Tokens: q.EvalTokens, Limit: limit, By: "length"})
		case limit > 0 && q.EvalTokens >= limit-CapTolerance:
			out = append(out, Capped{Seq: q.Seq, Tokens: q.EvalTokens, Limit: limit, By: "declared limit"})
		}
	}
	return out
}
