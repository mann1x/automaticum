package human

import (
	"testing"
	"time"
)

func TestDuration(t *testing.T) {
	for in, want := range map[time.Duration]string{
		0:                            "0s",
		450 * time.Millisecond:       "0.45s",
		2340 * time.Millisecond:      "2.3s",
		42 * time.Second:             "42s",
		59600 * time.Millisecond:     "1m 00s",
		493 * time.Second:            "8m 13s",
		1121 * time.Second:           "18m 41s",
		3725 * time.Second:           "1h 02m 05s",
		7200 * time.Second:           "2h 00m 00s",
		26*time.Hour + 3*time.Second: "1d 02h 00m 03s",
		-90 * time.Second:            "-1m 30s",
		733347 * time.Millisecond:    "12m 13s",
		9999 * time.Millisecond:      "10.0s",
		time.Duration(1582.4 * float64(time.Second)): "26m 22s",
	} {
		if got := Duration(in); got != want {
			t.Errorf("Duration(%v) = %q, want %q", in, got, want)
		}
	}
	if Millis(733347) != "12m 13s" || Seconds(8.171) != "8.2s" || Since(time.Time{}) != "" {
		t.Error("Millis, Seconds or Since")
	}
}

func TestNumbers(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 625339: "625,339", 1234567: "1,234,567", -1234: "-1,234"} {
		if got := Count(in); got != want {
			t.Errorf("Count(%d) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{18: "18", 34136: "34,136", 0.5: "0.5", 57.4769: "57.477", 1234.5: "1,234.5", 2.9996: "3", -0.25: "-0.25"} {
		if got := Number(in); got != want {
			t.Errorf("Number(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[int64]string{0: "0 B", 193: "193 B", 2048: "2.0 KiB", 8502350: "8.1 MiB", 150422016: "143 MiB", 17123036364: "15.9 GiB"} {
		if got := Bytes(in); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{1: "100%", 0.3316: "33.2%", 0: "0%", 0.6: "60%"} {
		if got := Percent(in); got != want {
			t.Errorf("Percent(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestMetric(t *testing.T) {
	for _, c := range []struct {
		name string
		v    float64
		want string
	}{
		{"time.wall_ms", 733347, "12m 13s"},
		{"time.first_token_ms", 12690, "13s"},
		{"endpoint.first_load_s", 8.171, "8.2s"},
		{"rate.generation_tok_s", 57.4769, "57.5 tok/s"},
		{"context.peak_share", 0.3316, "33.2%"},
		{"endpoint.gpu_share", 1, "100%"},
		{"tokens.prompt", 625339, "625,339 tokens"},
		{"tokens.completion.per_request.n", 18, "18 requests"},
		{"tokens.completion.per_request.p90", 6513, "6,513 tokens"},
		{"thinking.chars.max", 29345, "29,345 characters"},
		{"thinking.chars.n", 17, "17 blocks"},
		{"detector.runaway.available", 1, "yes"},
		{"detector.char_run.available", 0, "no"},
		{"tool_calls.by_tool.editor", 7, "7"},
		{"turns", 18, "18"},
		{"context.window", 131072, "131,072 tokens"},
	} {
		if got := Metric(c.name, c.v); got != c.want {
			t.Errorf("Metric(%s, %v) = %q, want %q", c.name, c.v, got, c.want)
		}
	}
}
