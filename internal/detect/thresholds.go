package detect

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Every number a detector or a measure decides with is a Threshold (D148,
// the owner, 2026-11-12: "the thresholds comes from the SDK but as
// defaults. automaticum must be flexible so it needs to have the
// thresholds customizable"). Its default is the SDK's own value where the
// SDK has the rule, else the owner's, and says which; an installation may
// override it in automaticum.toml ([thresholds]) and one `detect` command
// with --threshold. Whatever reads with thresholds records the effective
// values and where each came from, so results read at different values
// are never merged unseen.

// Threshold is one number, its default and where the default comes from.
type Threshold struct {
	Name    string  `json:"name"`
	Default float64 `json:"default"`
	// Integer says the value is a count: an override must be a whole
	// number.
	Integer bool `json:"integer,omitempty"`
	// Origin is "sdk" (the SDK's own value for the same rule) or "owner"
	// (the SDK has no such rule; the owner set it).
	Origin string `json:"origin"`
	// Source says exactly where the default comes from.
	Source string `json:"source"`
	Doc    string `json:"doc"`
}

// The reasoning-block repeat measure (D148): the most repeats of one unit
// in a block. The SDK has no rule that counts a block's repeats this way
// (its reasoning-loop guard counts cycles of consecutive lines, its
// argument guard third copies), so every number of it is the owner's, as
// the client's loopscan used them on 2026-11-11.
const (
	BlockMaxRepeat   = "reasoning_block.max_repeat"
	BlockUnitChars   = "reasoning_block.unit_min_chars"
	BlockKeyChars    = "reasoning_block.key_chars"
	BlockKeyMinChars = "reasoning_block.key_min_chars"
)

const ownerLoopscan = "owner 2026-11-12 (D148): the client's loopscan (bm-889, 2026-11-11); the SDK has no rule that counts one unit's repeats in a block"

var thresholds = []Threshold{
	{Name: BlockMaxRepeat, Default: 20, Integer: true, Origin: "owner", Source: ownerLoopscan,
		Doc: "a reasoning block whose most repeated unit is written this many times or more is a loop block"},
	{Name: BlockUnitChars, Default: 40, Integer: true, Origin: "owner", Source: ownerLoopscan,
		Doc: "a unit (a line, or a sentence ended by . ! or ?) counts from this many characters, spaces at its ends left out"},
	{Name: BlockKeyChars, Default: 80, Integer: true, Origin: "owner", Source: ownerLoopscan,
		Doc: "a unit is compared by its first this many letters and digits, lower case, so a line re-quoted with small edits stays one unit"},
	{Name: BlockKeyMinChars, Default: 20, Integer: true, Origin: "owner", Source: ownerLoopscan,
		Doc: "a unit with fewer letters and digits than this is not counted"},
}

// Thresholds lists every threshold, by name.
func Thresholds() []Threshold {
	out := slices.Clone(thresholds)
	slices.SortFunc(out, func(a, b Threshold) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func lookup(name string) (Threshold, bool) {
	for _, t := range thresholds {
		if t.Name == name {
			return t, true
		}
	}
	return Threshold{}, false
}

// Values are overrides of thresholds, by name; a name not in it has its
// default. The zero value is every default.
type Values map[string]float64

// Get is a threshold's value. An unknown name is a programming error.
func (v Values) Get(name string) float64 {
	if x, ok := v[name]; ok {
		return x
	}
	t, ok := lookup(name)
	if !ok {
		panic("detect: no threshold " + name)
	}
	return t.Default
}

// Int is a count threshold's value.
func (v Values) Int(name string) int { return int(v.Get(name)) }

// Check says what is wrong with overrides: a name no threshold has, a
// count that is not whole, a value below zero.
func (v Values) Check() error {
	var bad []string
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for k := range v {
			if !yield(k) {
				return
			}
		}
	}) {
		t, ok := lookup(name)
		x := v[name]
		switch {
		case !ok:
			bad = append(bad, fmt.Sprintf("there is no threshold %q", name))
		case x < 0:
			bad = append(bad, fmt.Sprintf("threshold %s must not be below zero, not %v", name, x))
		case t.Integer && x != float64(int(x)):
			bad = append(bad, fmt.Sprintf("threshold %s is a count, not %v", name, x))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("%s (the thresholds are %s)", strings.Join(bad, "; "), strings.Join(Names(), ", "))
	}
	return nil
}

// Names lists the thresholds' names.
func Names() []string {
	var out []string
	for _, t := range Thresholds() {
		out = append(out, t.Name)
	}
	return out
}

// ParseValues reads overrides written name=value.
func ParseValues(items []string) (Values, error) {
	v := Values{}
	for _, it := range items {
		name, val, ok := strings.Cut(it, "=")
		name = strings.TrimSpace(name)
		x, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if !ok || name == "" || err != nil {
			return nil, fmt.Errorf("threshold %q: write it name=number", it)
		}
		v[name] = x
	}
	return v, v.Check()
}

// Effective is a threshold's value as read, and where the value came
// from: "sdk default", "owner default", "config" (the installation's
// automaticum.toml) or "cli" (the command's --threshold).
type Effective struct {
	Name    string  `json:"name"`
	Value   float64 `json:"value"`
	From    string  `json:"from"`
	Default float64 `json:"default"`
	Source  string  `json:"default_source"`
}

// Resolve lays the command's overrides over the installation's and says
// for every threshold what it is and why. Both must have passed Check.
func Resolve(config, cli Values) (Values, []Effective) {
	merged := Values{}
	var out []Effective
	for _, t := range Thresholds() {
		e := Effective{Name: t.Name, Value: t.Default, From: t.Origin + " default", Default: t.Default, Source: t.Source}
		if x, ok := config[t.Name]; ok {
			e.Value, e.From = x, "config"
			merged[t.Name] = x
		}
		if x, ok := cli[t.Name]; ok {
			e.Value, e.From = x, "cli"
			merged[t.Name] = x
		}
		out = append(out, e)
	}
	return merged, out
}

// BlockRepeat is a reasoning block read for its most repeated unit.
type BlockRepeat struct {
	Units int `json:"units"`
	// Max is how often the most repeated unit was written; Top its key.
	Max int    `json:"max_repeat"`
	Top string `json:"top,omitempty"`
}

// BlockRepeats counts a block's units, as the client's loopscan did: the
// text is cut into lines, and each line after a ".", "!" or "?" followed by
// a space; a unit of BlockUnitChars characters or more is kept, compared
// by its first BlockKeyChars letters and digits in lower case, and left out
// when that key has fewer than BlockKeyMinChars.
func BlockRepeats(text string, v Values) BlockRepeat {
	unitMin, keyLen, keyMin := v.Int(BlockUnitChars), v.Int(BlockKeyChars), v.Int(BlockKeyMinChars)
	counts := map[string]int{}
	var order []string
	n := 0
	add := func(u string) {
		u = strings.TrimSpace(u)
		if len([]rune(u)) < unitMin {
			return
		}
		var k []rune
		for _, c := range strings.ToLower(u) {
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
				k = append(k, c)
				if len(k) == keyLen {
					break
				}
			}
		}
		if len(k) < keyMin {
			return
		}
		key := string(k)
		if counts[key] == 0 {
			order = append(order, key)
		}
		counts[key]++
		n++
	}
	for _, line := range strings.Split(text, "\n") {
		r := []rune(line)
		start := 0
		for i := 0; i < len(r); i++ {
			if (r[i] == '.' || r[i] == '!' || r[i] == '?') && i+1 < len(r) && unicode.IsSpace(r[i+1]) {
				add(string(r[start : i+1]))
				start = i + 1
			}
		}
		add(string(r[start:]))
	}
	out := BlockRepeat{Units: n}
	// The first key to reach the most, as Counter.most_common gives it.
	for _, k := range order {
		if counts[k] > out.Max {
			out.Max, out.Top = counts[k], k
		}
	}
	return out
}
