package stats

import (
	"math"
	"testing"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestWilson(t *testing.T) {
	lo, hi := Wilson(8, 10, Z95)
	if !near(lo, 0.4902, 0.0005) || !near(hi, 0.9433, 0.0005) {
		t.Errorf("8 of 10: %.4f to %.4f", lo, hi)
	}
	if lo, hi := Wilson(5, 5, Z95); !near(lo, 0.5655, 0.0005) || hi != 1 {
		t.Errorf("5 of 5: %.4f to %.4f", lo, hi)
	}
	if lo, hi := Wilson(0, 0, Z95); lo != 0 || hi != 1 {
		t.Errorf("nothing: %v to %v", lo, hi)
	}
}

func TestFisher(t *testing.T) {
	// Known values of the two-sided exact test.
	if p := Fisher(8, 2, 1, 5); !near(p, 0.03497, 0.0001) {
		t.Errorf("8/2 against 1/5: %.5f", p)
	}
	if p := Fisher(3, 1, 1, 3); !near(p, 0.48571, 0.0001) {
		t.Errorf("3/1 against 1/3: %.5f", p)
	}
	if p := Fisher(5, 0, 5, 0); p != 1 {
		t.Errorf("5 of 5 against 5 of 5: %v", p)
	}
}

func TestMannWhitney(t *testing.T) {
	r := MannWhitney([]float64{1, 2, 3}, []float64{4, 5, 6})
	if r.U != 0 || !r.Exact || !near(r.P, 0.1, 1e-9) || r.Superiority != 0 {
		t.Errorf("apart: %+v", r)
	}
	r = MannWhitney([]float64{1, 2, 3, 4, 5}, []float64{6, 7, 8, 9, 10})
	if !near(r.P, 2.0/252, 1e-9) {
		t.Errorf("five against five, apart: %+v", r)
	}
	// 8 of 25 pairs: the exact two-sided p of U = 8 at five against five
	// is 106/252.
	r = MannWhitney([]float64{1, 3, 5, 7, 12}, []float64{2, 8, 9, 10, 11})
	if r.U != 8 || !near(r.P, 106.0/252, 1e-9) {
		t.Errorf("interleaved: %+v", r)
	}
	// Ties: the approximation, and no claim of exactness.
	r = MannWhitney([]float64{1, 1, 2, 2}, []float64{1, 2, 2, 3})
	if r.Exact || r.P <= 0 || r.P > 1 {
		t.Errorf("ties: %+v", r)
	}
	if r := MannWhitney(nil, []float64{1}); r.P != 1 {
		t.Errorf("an empty set: %+v", r)
	}
}

func TestSummarise(t *testing.T) {
	s := Summarise([]float64{4, 1, 3, 2, 5})
	if s.N != 5 || s.Min != 1 || s.Q1 != 2 || s.Median != 3 || s.Q3 != 4 || s.Max != 5 || s.Mean != 3 || !near(s.SD, 1.5811, 0.0001) {
		t.Errorf("%+v", s)
	}
	if s := Summarise(nil); s.N != 0 {
		t.Errorf("%+v", s)
	}
}
