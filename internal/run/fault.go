package run

import (
	"regexp"
	"strings"

	"github.com/mann1x/automaticum/internal/store"
)

// Fault says whether an error the agent ended with was the server's, and of
// which kind, as the SDK itself tells them apart when it decides to wait
// and retry a turn: "refusal" (the server refused or queued the request:
// admission, rate limit, an evicted cache) or "transport" (the connection
// or the server went away). Anything else, a context overflow or a tool
// call the parser rejected among them, is "" and stays the model's.
//
// The patterns are ported verbatim from @cerebriline/llms 4.100.269 (the
// `Mk` and `_k` lists of its `Kr`); the SDK retries such errors on its own
// first (three attempts in the stream wrapper, then the agent's provider
// retries with backoff), so an agent that ends with one has exhausted them.
func Fault(message string) string {
	m := strings.TrimSpace(message)
	if m == "" {
		return ""
	}
	for _, re := range refusalFaults {
		if re.MatchString(m) {
			return "refusal"
		}
	}
	for _, re := range transportFaults {
		if re.MatchString(m) {
			return "transport"
		}
	}
	return ""
}

// refusalFaults is the SDK's `Mk`, with its `Xm` (an evicted request).
var refusalFaults = compileAll(
	`(?i)projected mean tps below floor`,
	`(?i)context allocation exhausted`,
	`(?i)session allocation full`,
	`(?i)admission (?:rejected|refused|queued)`,
	`(?i)\btoo many requests\b`,
	`(?i)^(?:error:\s*)?evicted to keep other in-flight requests alive\b`,
	`\[opencoti_window_unavailable\b`,
)

// transportFaults is the SDK's `_k`.
var transportFaults = compileAll(
	`(?i)\bserver is shutting down\b`,
	`(?i)^(?:error:\s*)?(?:503\s*)?loading model\b`,
	`\b(?:ECONNREFUSED|ECONNRESET|ECONNABORTED|EPIPE|EHOSTUNREACH|EHOSTDOWN|ENETUNREACH|ENETDOWN|ETIMEDOUT|UND_ERR_SOCKET|UND_ERR_CONNECT_TIMEOUT|UND_ERR_CLOSED)\b`,
	`(?i)\bsocket hang up\b`,
	`(?i)\bother side closed\b`,
	`(?i)\bsocket (?:was )?closed\b`,
	`(?i)\bpremature close\b`,
	`(?i)\bcannot connect to api\b`,
	`(?i)^fetch failed$`,
	`(?i)^terminated$`,
	`(?i)^(?:error:\s*)?terminated:`,
	`\bHTTPParserError\b`,
	`\bHPE_[A-Z_]+\b`,
	`(?i)^(?:network|connection) (?:error|failure|lost|reset)$`,
	`(?i)^(?:(?:error:\s*)?(?:502|503|504)\s*)?(?:bad gateway|service unavailable|gateway time-?out)$`,
	`(?i)^no healthy upstream$`,
	`(?i)^(?:error:\s*)?(?:500\s*)?invalid input batch\.?$`,
	`(?i)^(?:error:\s*)?got exception: speculative batch index \d+ is not inside the current sub-batch\b`,
	`(?i)^(?:error:\s*)?(?:AI_TypeValidationError:\s*)?type validation failed\b[^:\n]*: value:`,
	`(?i)^(?:error:\s*)?(?:AI_JSONParseError:\s*)?json parsing failed: text:`,
)

func compileAll(exprs ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(exprs))
	for i, e := range exprs {
		out[i] = regexp.MustCompile(e)
	}
	return out
}

// infrastructureEnds are the reasons of an ABORTED run that are the
// harness's or the server's, never the model's or the operator's (D124):
// the container engine, the runner, the stream, the oracle's container,
// and a server fault after the SDK's retries. An operator's abort, a
// daemon's stop, a container out of memory, a tampered oracle and an
// oracle that timed out on the agent's work are not among them.
var infrastructureEnds = []string{
	"the container engine failed",
	"what the task collects of the agent's container could not be copied",
	"the runner ended without its last line",
	"the runner failed",
	"the runner reported an outcome this harness does not know",
	"the agent ended in error before the server answered any of its requests",
	"the event stream is empty",
	"the oracle could not be run",
	"the server failed after the SDK's retries",
}

// Infrastructure says whether a run ended ABORTED for the harness's or the
// server's reason.
func Infrastructure(v store.Verdict, reason string) bool {
	if v != store.Aborted {
		return false
	}
	for _, p := range infrastructureEnds {
		if strings.HasPrefix(reason, p) {
			return true
		}
	}
	return false
}
