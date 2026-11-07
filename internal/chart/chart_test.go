package chart

import (
	"fmt"
	"testing"
)

func TestTicks(t *testing.T) {
	for _, tc := range []struct {
		lo, hi   float64
		n        int
		want     string
		from, to float64
	}{
		{0, 100, 5, "[0 20 40 60 80 100]", 0, 100},
		{4000, 11000, 6, "[4000 6000 8000 10000 12000]", 4000, 12000},
		{4000, 11000, 10, "[4000 5000 6000 7000 8000 9000 10000 11000]", 4000, 11000},
		{0.13, 0.91, 4, "[0 0.2 0.4 0.6 0.8 1]", 0, 1},
		{-3, 7, 5, "[-4 -2 0 2 4 6 8]", -4, 8},
		{5, 5, 5, "[5 5.2 5.4 5.6 5.8 6]", 5, 6},
	} {
		ticks, from, to := Ticks(tc.lo, tc.hi, tc.n)
		if got := fmt.Sprint(ticks); got != tc.want || from != tc.from || to != tc.to {
			t.Errorf("Ticks(%v, %v, %d) = %s [%v, %v], want %s [%v, %v]", tc.lo, tc.hi, tc.n, got, from, to, tc.want, tc.from, tc.to)
		}
	}
}

func TestFormat(t *testing.T) {
	for v, want := range map[float64]string{3: "3", -2000: "-2000", 0.25: "0.25", 1.0 / 3: "0.333333"} {
		if got := Format(v); got != want {
			t.Errorf("Format(%v) = %q, want %q", v, got, want)
		}
	}
}
