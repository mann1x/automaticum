// Package stats holds the statistics reports print about runs (spec 070
// §6). It decides nothing: every function returns numbers for a reader,
// and none of them draws a line between "different" and "the same" (D78).
package stats

import (
	"math"
	"sort"
)

// Z95 is the normal quantile of a two-sided 95% interval, the level every
// interval of this package is labelled with.
const Z95 = 1.959963984540054

// Wilson is the Wilson score interval of k successes in n trials.
func Wilson(k, n int, z float64) (lo, hi float64) {
	if n == 0 {
		return 0, 1
	}
	p, nn := float64(k)/float64(n), float64(n)
	den := 1 + z*z/nn
	mid := (p + z*z/(2*nn)) / den
	half := z * math.Sqrt(p*(1-p)/nn+z*z/(4*nn*nn)) / den
	return math.Max(0, mid-half), math.Min(1, mid+half)
}

// Summary describes a set of values: medians and ranges first.
type Summary struct {
	N      int     `json:"n"`
	Min    float64 `json:"min"`
	Q1     float64 `json:"q1"`
	Median float64 `json:"median"`
	Q3     float64 `json:"q3"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
	// SD is the sample standard deviation, and CV its share of the mean:
	// how much the same arm swings from run to run.
	SD float64 `json:"sd"`
	CV float64 `json:"cv"`
}

// Summarise sorts a copy of v and describes it.
func Summarise(v []float64) Summary {
	s := Summary{N: len(v)}
	if len(v) == 0 {
		return s
	}
	x := append([]float64(nil), v...)
	sort.Float64s(x)
	s.Min, s.Max = x[0], x[len(x)-1]
	s.Q1, s.Median, s.Q3 = quantile(x, 0.25), quantile(x, 0.5), quantile(x, 0.75)
	sum := 0.0
	for _, a := range x {
		sum += a
	}
	s.Mean = sum / float64(len(x))
	if len(x) > 1 {
		ss := 0.0
		for _, a := range x {
			ss += (a - s.Mean) * (a - s.Mean)
		}
		s.SD = math.Sqrt(ss / float64(len(x)-1))
		if s.Mean != 0 {
			s.CV = s.SD / math.Abs(s.Mean)
		}
	}
	return s
}

// quantile interpolates linearly between the two nearest ranks.
func quantile(sorted []float64, q float64) float64 {
	h := q * float64(len(sorted)-1)
	i := int(math.Floor(h))
	if i+1 >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	return sorted[i] + (h-float64(i))*(sorted[i+1]-sorted[i])
}

// Fisher is the two-sided exact test of a 2x2 table: a and b the successes
// and failures of one group, c and d those of the other. It returns the
// probability, under no difference, of a table at most as likely as this
// one.
func Fisher(a, b, c, d int) float64 {
	n, row, col := a+b+c+d, a+b, a+c
	if n == 0 {
		return 1
	}
	lp := func(x int) float64 { // log probability of x in the top-left cell
		return lchoose(row, x) + lchoose(n-row, col-x) - lchoose(n, col)
	}
	obs, p := lp(a), 0.0
	for x := max(0, col-(n-row)); x <= min(row, col); x++ {
		if l := lp(x); l <= obs+1e-9 {
			p += math.Exp(l)
		}
	}
	return math.Min(1, p)
}

func lchoose(n, k int) float64 {
	a, _ := math.Lgamma(float64(n + 1))
	b, _ := math.Lgamma(float64(k + 1))
	c, _ := math.Lgamma(float64(n - k + 1))
	return a - b - c
}

// Rank is the result of the Mann-Whitney test of two sets of values.
type Rank struct {
	// U is the count of pairs in which the first set's value is the
	// larger, ties counting half; of N1*N2 pairs.
	U      float64 `json:"u"`
	N1, N2 int
	// P is two-sided. Exact says how it was computed: from the exact
	// distribution (no ties, small sets) or the normal approximation with
	// the correction for ties.
	P     float64 `json:"p"`
	Exact bool    `json:"exact"`
	// Superiority is U/(N1*N2): the chance that a run of the first set
	// has the larger value than a run of the second.
	Superiority float64 `json:"superiority"`
}

// MannWhitney compares two sets of values by rank.
func MannWhitney(x, y []float64) Rank {
	r := Rank{N1: len(x), N2: len(y), P: 1}
	if len(x) == 0 || len(y) == 0 {
		return r
	}
	ties := false
	for _, a := range x {
		for _, b := range y {
			switch {
			case a > b:
				r.U++
			case a == b:
				r.U += 0.5
				ties = true
			}
		}
	}
	n1, n2 := len(x), len(y)
	r.Superiority = r.U / float64(n1*n2)
	all := append(append([]float64(nil), x...), y...)
	sort.Float64s(all)
	tie := 0.0 // sum of t^3 - t over the groups of equal values
	for i := 0; i < len(all); {
		j := i
		for j < len(all) && all[j] == all[i] {
			j++
		}
		if t := float64(j - i); t > 1 {
			tie += t*t*t - t
			ties = true
		}
		i = j
	}
	if !ties && n1*n2 <= 400 {
		// Exact: the number of arrangements giving each U.
		counts := arrangements(n1, n2)
		total, lo := 0.0, math.Min(r.U, float64(n1*n2)-r.U)
		tail := 0.0
		for u, c := range counts {
			total += c
			if float64(u) <= lo+1e-9 {
				tail += c
			}
		}
		r.P, r.Exact = math.Min(1, 2*tail/total), true
		return r
	}
	n := float64(n1 + n2)
	mean := float64(n1*n2) / 2
	variance := float64(n1*n2) / 12 * (n + 1 - tie/(n*(n-1)))
	if variance <= 0 {
		return r
	}
	z := (math.Abs(r.U-mean) - 0.5) / math.Sqrt(variance)
	if z < 0 {
		z = 0
	}
	r.P = math.Erfc(z / math.Sqrt2)
	return r
}

// arrangements counts, for every U from 0 to n1*n2, the orderings of n1
// and n2 distinct values that give it.
func arrangements(n1, n2 int) []float64 {
	// f[i][j][u]: i values of the first set, j of the second.
	f := make([][][]float64, n1+1)
	for i := range f {
		f[i] = make([][]float64, n2+1)
		for j := range f[i] {
			f[i][j] = make([]float64, i*j+1)
		}
	}
	for i := 0; i <= n1; i++ {
		for j := 0; j <= n2; j++ {
			if i == 0 || j == 0 {
				f[i][j][0] = 1
				continue
			}
			for u := 0; u <= i*j; u++ {
				v := 0.0
				// The largest value is of the first set: it beats all j.
				if u-j >= 0 && u-j <= (i-1)*j {
					v += f[i-1][j][u-j]
				}
				// Or of the second: it adds nothing.
				if u <= i*(j-1) {
					v += f[i][j-1][u]
				}
				f[i][j][u] = v
			}
		}
	}
	return f[n1][n2]
}
