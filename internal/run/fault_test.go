package run

import "testing"

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
