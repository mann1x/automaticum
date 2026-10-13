package detect

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// The two detectors of this file are the reasoning guards of the agent's
// own SDK, read after the run instead of during it. Their definitions and
// every threshold are the SDK's (packages/agents/src/reasoning-repetition.ts
// and reasoning-loop-guard.ts), which set them on recorded sessions; none
// is chosen here (D75). The numbers are those of the SDK's release tags
// v4.100.250 and v4.100.251, which the SDK's own session confirmed to be
// equal. The source of truth is the tag of the SDK build under test: a
// build with other numbers needs its own set here, and a new Version.
//
// One difference is unavoidable. The SDK's loop guard looks each time a
// piece of the stream arrives; here the whole block is at hand, so it is
// looked at after every line and, inside a line, after every character.
// What fires in the SDK fires here.

// Reasoning repetition: one paragraph that carries a sentence, repeated
// word for word, in a block of some length. Both signals are required.
const (
	repetitionMinChars      = 4000 // the block
	repetitionParagraph     = 60   // a paragraph counts from this length
	repetitionStopwords     = 3    // and with this many common words: a sentence, not a bare quote
	repetitionMaxRepeat     = 4    // occurrences of the most repeated paragraph
	repetitionDuplicateFrac = 0.25 // duplicates among the counted paragraphs
)

// Reasoning loop: a reasoning channel that has collapsed. Length is never
// the signal; only periodicity is.
const (
	loopMinChars        = 4000 // nothing is judged before this much of the block
	loopWindow          = 60   // lines looked at
	loopMaxUnique       = 4    // distinct lines in a full window: "low-uniqueness"
	loopMaxPeriod       = 6    // lines in a cycle: "line-cycle"
	loopMinCycles       = 20
	loopMinLineChars    = 2000 // an unbroken line from this length: "phrase-cycle"
	loopPhraseMaxPeriod = 120
	loopPhraseMinCycles = 12
)

// The kinds of a reasoning loop, as the SDK names them.
const (
	LowUniqueness = "low-uniqueness"
	LineCycle     = "line-cycle"
	PhraseCycle   = "phrase-cycle"
)

var (
	stopwords  = regexp.MustCompile(`\b(the|is|are|to|and|of|it|that|this|be|for|but|so|if|will|need|should|let|now|then|because|actually|wait)\b`)
	paragraphs = regexp.MustCompile(`\n\s*\n`)
	fenced     = regexp.MustCompile("(?s)```.*?```")
)

// bearsProse: whether a paragraph carries a sentence rather than being a
// bare quote of code.
func bearsProse(p string) bool {
	if strings.HasPrefix(p, "```") {
		p = fenced.ReplaceAllString(p, "")
	}
	return len(stopwords.FindAllStringIndex(strings.ToLower(p), -1)) >= repetitionStopwords
}

func reasoningRepetition(r []rune, text string, _ Options) *Hit {
	if len(r) < repetitionMinChars {
		return nil
	}
	counts, order, total := map[string]int{}, []string{}, 0
	for _, p := range paragraphs.Split(text, -1) {
		p = strings.TrimSpace(p)
		if utf8.RuneCountInString(p) < repetitionParagraph || !bearsProse(p) {
			continue
		}
		if counts[p] == 0 {
			order = append(order, p)
		}
		counts[p]++
		total++
	}
	if total == 0 {
		return nil
	}
	duplicates, most, sample := 0, 0, ""
	for _, p := range order {
		duplicates += counts[p] - 1
		if counts[p] > most {
			most, sample = counts[p], p
		}
	}
	share := float64(duplicates) / float64(total)
	if most < repetitionMaxRepeat || share < repetitionDuplicateFrac {
		return nil
	}
	return &Hit{Detector: ReasoningRepetition, Count: most, Share: share, Unit: cut(sample), Offset: len([]rune(text[:strings.Index(text, sample)]))}
}

func reasoningLoop(r []rune, _ string, _ Options) *Hit {
	if len(r) < loopMinChars {
		return nil
	}
	var window []string
	for start := 0; start <= len(r); {
		end := start
		for end < len(r) && r[end] != '\n' {
			end++
		}
		seg := r[start:end]
		// Inside the line, before its end is seen: one phrase repeating.
		if h := phraseCycle(seg, start); h != nil {
			return h
		}
		if end == len(r) {
			break // a line without its end is not yet a line
		}
		if line := strings.TrimSpace(string(seg)); line != "" {
			window = append(window, line)
			if len(window) > loopWindow {
				window = window[1:]
			}
		}
		start = end + 1
		if start < loopMinChars {
			continue
		}
		if len(window) == loopWindow {
			distinct, list := map[string]bool{}, []string{}
			for _, l := range window {
				if !distinct[l] {
					distinct[l] = true
					list = append(list, l)
				}
			}
			if len(list) <= loopMaxUnique {
				return &Hit{Detector: ReasoningLoop, Kind: LowUniqueness, Count: len(list), Unit: cut(strings.Join(list, " / ")), Offset: start}
			}
		}
		if n := len(window); n >= loopMaxPeriod*2 {
			for p := 1; p <= loopMaxPeriod; p++ {
				cycles := 0
			back:
				for i := n - p; i-p >= 0; i -= p {
					for k := 0; k < p; k++ {
						if window[i+k] != window[i-p+k] {
							break back
						}
					}
					cycles++
				}
				if cycles >= loopMinCycles {
					return &Hit{Detector: ReasoningLoop, Kind: LineCycle, Period: p, Count: cycles + 1, Unit: cut(strings.Join(window[n-p:], " / ")), Offset: start}
				}
			}
		}
	}
	return nil
}

// phraseCycle looks for a unit of 4 to 120 characters that ends at some
// place of an unbroken line and stands there 13 or more times one after
// the other, once the line is 2,000 characters long and the block 4,000.
func phraseCycle(seg []rune, at int) *Hit {
	if len(seg) < loopMinLineChars {
		return nil
	}
	var same [loopPhraseMaxPeriod + 1]int // per period: characters equal to the one a period before, in a row
	for e := 0; e < len(seg); e++ {
		for p := 4; p <= loopPhraseMaxPeriod && p <= e; p++ {
			if seg[e] == seg[e-p] {
				same[p]++
			} else {
				same[p] = 0
			}
		}
		if e+1 < loopMinLineChars || at+e+1 < loopMinChars {
			continue
		}
		for p := 4; p <= loopPhraseMaxPeriod; p++ {
			if cycles := same[p] / p; cycles >= loopPhraseMinCycles {
				unit := strings.Join(strings.Fields(string(seg[e+1-p:e+1])), " ")
				return &Hit{Detector: ReasoningLoop, Kind: PhraseCycle, Period: p, Count: cycles + 1, Unit: cut(unit), Offset: at + e + 1 - (cycles+1)*p}
			}
		}
	}
	return nil
}
