// Package human writes values the way a person reads them: a duration in
// hours, minutes and seconds, a count with its thousands apart, a size in
// binary units, a share as a percentage. Every interface a person looks at
// (the command line, the terminal interface, the web interface) writes
// through it, so the same value reads the same everywhere. Machine output
// (JSON) is never formatted (090 §1.1).
package human

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Duration writes a length of time in hours, minutes and seconds:
// "1h 02m 05s", "8m 13s", "42s". Under ten seconds it keeps one decimal
// ("2.3s"), under one second two ("0.45s"). It never writes bare seconds for
// a minute or more.
func Duration(d time.Duration) string {
	sign := ""
	if d < 0 {
		sign, d = "-", -d
	}
	switch {
	case d < time.Second:
		return sign + strconv.FormatFloat(math.Round(d.Seconds()*100)/100, 'f', -1, 64) + "s"
	case d < 10*time.Second:
		return sign + strconv.FormatFloat(math.Round(d.Seconds()*10)/10, 'f', 1, 64) + "s"
	}
	total := int64(math.Round(d.Seconds()))
	h, m, s := total/3600, total%3600/60, total%60
	switch {
	case h >= 24:
		return fmt.Sprintf("%s%dd %02dh %02dm %02ds", sign, h/24, h%24, m, s)
	case h > 0:
		return fmt.Sprintf("%s%dh %02dm %02ds", sign, h, m, s)
	case m > 0:
		return fmt.Sprintf("%s%dm %02ds", sign, m, s)
	}
	return fmt.Sprintf("%s%ds", sign, s)
}

// Millis writes a number of milliseconds as a duration.
func Millis(ms float64) string { return Duration(time.Duration(ms * float64(time.Millisecond))) }

// Seconds writes a number of seconds as a duration.
func Seconds(s float64) string { return Duration(time.Duration(s * float64(time.Second))) }

// Since writes how long ago a moment was; "" for no moment.
func Since(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return Duration(time.Since(t).Round(time.Second))
}

// Time writes a moment in the reader's own zone, with the zone's offset.
func Time(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04:05 -07:00")
}

// Count writes a whole number with its thousands apart: 1,234,567.
func Count(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// Number writes a value that may have a fraction: whole numbers as counts,
// others with up to three decimals.
func Number(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return Count(int64(v))
	}
	whole := math.Trunc(v)
	frac := strconv.FormatFloat(math.Abs(v-whole), 'f', 3, 64) // "0.123"
	frac = strings.TrimRight(strings.TrimPrefix(frac, "0"), "0")
	if frac == "." || strings.HasPrefix(frac, "1") { // rounded up to a whole
		return Count(int64(math.Round(v)))
	}
	out := Count(int64(whole)) + frac
	if v < 0 && whole == 0 {
		out = "-" + out
	}
	return out
}

// Bytes writes a size in binary units: "512 B", "8.1 MiB", "17.2 GiB".
func Bytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	v := float64(n) / 1024
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// Percent writes a share between 0 and 1 as a percentage: "33.2%".
func Percent(share float64) string {
	p := share * 100
	if p == math.Trunc(p) {
		return fmt.Sprintf("%.0f%%", p)
	}
	return fmt.Sprintf("%.1f%%", p)
}

// Rate writes tokens per second: "57.5 tok/s".
func Rate(v float64) string { return fmt.Sprintf("%.1f tok/s", v) }

// Metric writes one of a run's metrics by what its name says it is (070
// §2): a name that ends in "_ms" or "_s" is a time, one that holds "share"
// is a share, and so on. A name it does not know is written as a number.
func Metric(name string, v float64) string {
	last := name[strings.LastIndexByte(name, '.')+1:]
	switch {
	case strings.HasSuffix(last, "_ms"):
		return Millis(v)
	case strings.HasSuffix(last, "_s") && !strings.HasSuffix(last, "tok_s"):
		return Seconds(v)
	case strings.HasSuffix(last, "tok_s"):
		return Rate(v)
	case strings.Contains(last, "share"):
		return Percent(v)
	case last == "available":
		if v != 0 {
			return "yes"
		}
		return "no"
	case strings.Contains(name, ".chars."):
		if last == "n" {
			return Number(v) + " blocks"
		}
		return Number(v) + " characters"
	case strings.HasPrefix(name, "tokens.") || strings.HasPrefix(name, "derived.") ||
		name == "context.window" || name == "context.peak_tokens" || strings.HasPrefix(name, "context.prompt_") ||
		name == "thinking.budget_tokens.served":
		if last == "n" {
			return Number(v) + " requests"
		}
		return Number(v) + " tokens"
	}
	return Number(v)
}
