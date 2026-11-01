package run

import (
	"testing"

	"github.com/mann1x/automaticum/internal/store"
)

func TestFault(t *testing.T) {
	for msg, want := range map[string]string{
		"fetch failed":                                   "transport",
		"connect ECONNREFUSED 127.0.0.1:11434":           "transport",
		"Error: 503 loading model":                       "transport",
		"Service Unavailable":                            "transport",
		"502 Bad Gateway":                                "transport",
		"other side closed":                              "transport",
		"terminated: other side closed":                  "transport",
		"the server is shutting down":                    "transport",
		"429 Too Many Requests":                          "refusal",
		"admission rejected: queue full":                 "refusal",
		"evicted to keep other in-flight requests alive": "refusal",
		"context_length_exceeded":                        "",
		"tool_call_rejected: arguments not JSON":         "",
		"the model returned nothing":                     "",
		"":                                               "",
		"bad gateway, then something else":               "",
	} {
		if got := Fault(msg); got != want {
			t.Errorf("%q: %q, want %q", msg, got, want)
		}
	}
}

func TestInfrastructure(t *testing.T) {
	for reason, want := range map[string]bool{
		"the container engine failed: EOF": true,
		"the server failed after the SDK's retries (transport: fetch failed); not the model's failure": true,
		"the runner ended without its last line (exit code 137)":                                       true,
		"the event stream is empty":                             true,
		"operator abort":                                        false,
		"daemon stop":                                           false,
		"the lane's container ran out of memory":                false,
		"the oracle gave no answer: the oracle timed out":       false,
		"the oracle's copy in the workspace was changed: check": false,
	} {
		if got := Infrastructure(store.Aborted, reason); got != want {
			t.Errorf("%q: %v", reason, got)
		}
	}
	if Infrastructure(store.Fail, "the container engine failed") {
		t.Error("only an ABORTED run")
	}
}
