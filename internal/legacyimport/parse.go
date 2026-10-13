package legacyimport

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// maxTextFile bounds the small per-run text files. They are a few hundred
// bytes in practice; anything larger is read up to this limit.
const maxTextFile = 4 << 20

// KV is one key=value field, in the order it appeared.
type KV struct {
	Key   string
	Value string
}

// Exit is the parsed exit.txt of a run: one line of key=value fields that is
// positional at the front (verdict, transaction counts, wall, exit, arm) and
// append-only after that.
type Exit struct {
	Raw     string
	Verdict string // the old harness's word, verbatim
	Fields  []KV   // every field in order, verdict included
	Extra   []string
	Reason  string // reason="..." when present

	WallSeconds int64
	HasWall     bool
	ExitCode    int
	HasExitCode bool
}

// Get returns the value of the first field named key.
func (e *Exit) Get(key string) (string, bool) {
	for _, f := range e.Fields {
		if f.Key == key {
			return f.Value, true
		}
	}
	return "", false
}

// Map returns the fields as a map; the first occurrence of a key wins.
func (e *Exit) Map() map[string]string {
	m := make(map[string]string, len(e.Fields))
	for _, f := range e.Fields {
		if _, ok := m[f.Key]; !ok {
			m[f.Key] = f.Value
		}
	}
	return m
}

// ParseExit parses the content of an exit.txt. Only the first non-empty line
// is the verdict line.
func ParseExit(content string) (*Exit, error) {
	line := firstLine(content)
	if line == "" {
		return nil, errors.New("exit: empty")
	}
	e := &Exit{Raw: line}
	for _, tok := range splitQuoted(line) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok || k == "" {
			e.Extra = append(e.Extra, tok)
			continue
		}
		v = strings.Trim(v, `"`)
		e.Fields = append(e.Fields, KV{k, v})
	}
	if len(e.Fields) == 0 || e.Fields[0].Key != "verdict" {
		return nil, fmt.Errorf("exit: line does not start with verdict=: %q", truncate(line, 80))
	}
	e.Verdict = e.Fields[0].Value
	if e.Verdict == "" {
		return nil, errors.New("exit: empty verdict")
	}
	e.Reason, _ = e.Get("reason")
	if w, ok := e.Get("wall"); ok {
		if n, err := strconv.ParseInt(strings.TrimSuffix(w, "s"), 10, 64); err == nil {
			e.WallSeconds, e.HasWall = n, true
		}
	}
	if x, ok := e.Get("exit"); ok {
		if n, err := strconv.Atoi(x); err == nil {
			e.ExitCode, e.HasExitCode = n, true
		}
	}
	return e, nil
}

// splitQuoted splits on spaces, keeping a double-quoted value that follows
// an equals sign (reason="clock: 7203s vs 7200s cap") in one token.
func splitQuoted(line string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"' && (inQuote || (i > 0 && line[i-1] == '=')):
			inQuote = !inQuote
			cur.WriteByte(c)
		case (c == ' ' || c == '\t') && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// Build is the parsed build.txt of a run.
type Build struct {
	// Fields holds every key. A line may carry several fields; a value runs
	// to the next " key=" on its line, so it may contain spaces. A key that
	// repeats (the source file's sha after the build's sha) is stored as
	// <first key of its line>_<key>.
	Fields map[string]string
	Order  []string
}

// Get returns a field and whether it was present.
func (b *Build) Get(key string) (string, bool) {
	v, ok := b.Fields[key]
	return v, ok
}

var buildKey = regexp.MustCompile(`(^| )([a-z][a-z0-9_]*)=`)

// ParseBuild parses a build.txt.
func ParseBuild(content string) (*Build, error) {
	b := &Build{Fields: map[string]string{}}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		locs := buildKey.FindAllStringSubmatchIndex(line, -1)
		if len(locs) == 0 || locs[0][0] != 0 {
			continue // not a key=value line
		}
		first := line[locs[0][4]:locs[0][5]]
		for i, loc := range locs {
			key := line[loc[4]:loc[5]]
			end := len(line)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			val := strings.TrimSpace(line[loc[1]:end])
			if _, dup := b.Fields[key]; dup {
				key = first + "_" + key
			}
			if _, dup := b.Fields[key]; dup {
				continue
			}
			b.Fields[key] = val
			b.Order = append(b.Order, key)
		}
	}
	if len(b.Fields) == 0 {
		return nil, errors.New("build: no key=value lines")
	}
	return b, nil
}

// RunIDPattern matches the old harness's run id: a local timestamp and a
// sequence number.
var RunIDPattern = regexp.MustCompile(`\d{8}-\d{6}-\d{4}`)

var runIDExact = regexp.MustCompile(`^\d{8}-\d{6}-\d{4}$`)

// VerdictLine is one run line of a verdicts-*.txt file.
type VerdictLine struct {
	LineNo     int
	RunID      string
	Verdict    string
	Seconds    int64
	HasSeconds bool
	Fields     []KV
	Raw        string
}

// VerdictNote is a comment in a verdict file that names a run, such as an
// operator voiding the line above it.
type VerdictNote struct {
	LineNo int
	RunID  string
	Text   string
}

// VerdictFile is a parsed verdicts-*.txt.
type VerdictFile struct {
	Lines     int // non-empty lines
	Runs      []VerdictLine
	Malformed []string // non-empty lines that are not run lines, comments included
	Notes     []VerdictNote
}

// Counts returns the number of run lines per verdict word.
func (v *VerdictFile) Counts() map[string]int {
	m := map[string]int{}
	for _, l := range v.Runs {
		m[l.Verdict]++
	}
	return m
}

var secondsToken = regexp.MustCompile(`^\d+s$`)

// ParseVerdictFile parses a verdicts-*.txt stream.
func ParseVerdictFile(r io.Reader) (*VerdictFile, error) {
	v := &VerdictFile{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		v.Lines++
		f := strings.Fields(line)
		if len(f) < 2 || !runIDExact.MatchString(f[0]) || strings.Contains(f[1], "=") {
			v.Malformed = append(v.Malformed, line)
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				if id := RunIDPattern.FindString(line); id != "" {
					text := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
					v.Notes = append(v.Notes, VerdictNote{LineNo: n, RunID: id, Text: text})
				}
			}
			continue
		}
		vl := VerdictLine{LineNo: n, RunID: f[0], Verdict: f[1], Raw: line}
		for _, tok := range f[2:] {
			if secondsToken.MatchString(tok) {
				vl.Seconds, _ = strconv.ParseInt(strings.TrimSuffix(tok, "s"), 10, 64)
				vl.HasSeconds = true
				continue
			}
			if k, val, ok := strings.Cut(tok, "="); ok {
				vl.Fields = append(vl.Fields, KV{k, val})
			}
		}
		v.Runs = append(v.Runs, vl)
	}
	return v, sc.Err()
}

// TxEntry is one transaction line of a transactions.txt.
type TxEntry struct {
	TX      string // "TX-01"
	Outcome string // kept, discarded, empty, or "" when not recognised
	Text    string
}

// Transactions is the parsed transactions.txt of a run.
type Transactions struct {
	Entries []TxEntry
	Counts  map[string]string // the summary line: kept, discarded, empty
}

var txLine = regexp.MustCompile(`^(TX-\d+)\s+(.*)$`)

// ParseTransactions parses a transactions.txt.
func ParseTransactions(content string) *Transactions {
	t := &Transactions{Counts: map[string]string{}}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := txLine.FindStringSubmatch(line); m != nil {
			e := TxEntry{TX: m[1], Text: m[2]}
			if w := strings.Fields(m[2]); len(w) > 0 {
				switch w[0] {
				case "kept", "discarded", "empty":
					e.Outcome = w[0]
				}
			}
			t.Entries = append(t.Entries, e)
			continue
		}
		if strings.Contains(line, "=") {
			for k, v := range ParseKVLine(line) {
				t.Counts[k] = v
			}
		}
	}
	return t
}

// ParseKVLine parses a line of space-separated key=value fields, as in the
// last line of checks.txt and escalations.txt. Tokens without "=" are
// ignored.
func ParseKVLine(line string) map[string]string {
	m := map[string]string{}
	for _, tok := range splitQuoted(strings.TrimSpace(line)) {
		if k, v, ok := strings.Cut(tok, "="); ok && k != "" {
			m[k] = strings.Trim(v, `"`)
		}
	}
	return m
}

// ParseSummaryFile returns the fields of the last non-empty line of a
// checks.txt, escalations.txt or delegation.txt, plus the lines before it.
func ParseSummaryFile(content string) (fields map[string]string, detail []string) {
	var lines []string
	for _, l := range strings.Split(content, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return map[string]string{}, nil
	}
	return ParseKVLine(lines[len(lines)-1]), lines[:len(lines)-1]
}

// Oracle is the parsed output of the harness's own oracle run.
type Oracle struct {
	Parsed         bool // a JSON report line was found
	OK             bool
	Error          string
	FramesRun      int64
	ReachedPlaying *bool // nil when the oracle did not report it (task version 1)
	HasGameLogic   bool  // the field is present, even when null (task version 3)
	LogicPassed    int
	LogicTotal     int
	LogicFailed    []string
}

// ParseOracle parses an oracle.txt: the last line that is a JSON object is
// the oracle's report.
func ParseOracle(content string) *Oracle {
	o := &Oracle{}
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var raw struct {
			OK             *bool           `json:"ok"`
			Error          json.RawMessage `json:"error"`
			FramesRun      float64         `json:"frames_run"`
			ReachedPlaying *bool           `json:"reached_playing"`
			GameLogic      json.RawMessage `json:"game_logic"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil || raw.OK == nil {
			continue
		}
		o.Parsed = true
		o.OK = *raw.OK
		o.FramesRun = int64(raw.FramesRun)
		o.ReachedPlaying = raw.ReachedPlaying
		if len(raw.Error) > 0 && string(raw.Error) != "null" {
			var s string
			if json.Unmarshal(raw.Error, &s) == nil {
				o.Error = s
			} else {
				o.Error = string(raw.Error)
			}
		}
		if raw.GameLogic != nil {
			o.HasGameLogic = true
			var g struct {
				Passed int `json:"passed"`
				Total  int `json:"total"`
				Failed []struct {
					Trap string `json:"trap"`
				} `json:"failed"`
			}
			if json.Unmarshal(raw.GameLogic, &g) == nil {
				o.LogicPassed, o.LogicTotal = g.Passed, g.Total
				for _, f := range g.Failed {
					o.LogicFailed = append(o.LogicFailed, f.Trap)
				}
			}
		}
		return o
	}
	return o
}

// InvalidNote says the old harness's own notes mark a run as not countable.
type InvalidNote struct {
	RunID  string
	Reason string
	Source string // file the note came from, relative to the legacy root
}

// ParseInvalidRuns parses the harness's INVALID-RUNS.md: each "## " section
// lists run ids in a fenced block and explains itself in a paragraph that
// starts with "**Why invalid.**".
func ParseInvalidRuns(content string) []InvalidNote {
	var notes []InvalidNote
	var heading, why string
	var ids []string
	flush := func() {
		reason := heading
		if why != "" {
			reason += ": " + why
		}
		for _, id := range ids {
			notes = append(notes, InvalidNote{RunID: id, Reason: reason})
		}
		heading, why, ids = "", "", nil
	}
	inFence, inWhy := false, false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			flush()
			heading = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			inWhy = false
			continue
		}
		if heading == "" {
			continue
		}
		if inFence {
			if f := strings.Fields(trim); len(f) > 0 && runIDExact.MatchString(f[0]) {
				ids = append(ids, f[0])
			}
			continue
		}
		if strings.HasPrefix(trim, "**Why invalid.**") {
			inWhy = true
			why = strings.TrimSpace(strings.TrimPrefix(trim, "**Why invalid.**"))
			continue
		}
		if inWhy {
			if trim == "" {
				inWhy = false
				continue
			}
			why += " " + trim
		}
	}
	flush()
	return notes
}

var boundaryRun = regexp.MustCompile("(?i)boundary run is\\s+`?(\\d{8}-\\d{6}-\\d{4})`?")

// ParseChangeBoundary returns the boundary run named by a change log
// ("boundary run is `<id>`"), or "" when the log names none.
func ParseChangeBoundary(content string) string {
	// The sentence may be wrapped across lines.
	flat := strings.Join(strings.Fields(content), " ")
	if m := boundaryRun.FindStringSubmatch(flat); m != nil {
		return m[1]
	}
	return ""
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Do not cut inside a UTF-8 sequence.
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "..."
}
