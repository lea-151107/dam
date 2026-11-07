// Package chart holds helpers shared by the chart diagrams (xychart,
// radar): nice axis ticks and number formatting.
package chart

import (
	"math"
	"strconv"
)

// maxTicks bounds the number of ticks Ticks returns.
const maxTicks = 50

// MinSpan is the smallest axis range (hi - lo) that charts accept. Below
// it the tick step is subnormal, powers of ten lose precision or overflow,
// and positions along the axis stop being meaningful.
const MinSpan = 1e-300

// Ticks returns about n tick values covering [lo, hi] at a step of 1, 2
// or 5 times a power of ten, and the range they span (which contains
// [lo, hi]).
func Ticks(lo, hi float64, n int) (ticks []float64, from, to float64) {
	if !(hi > lo) {
		hi = lo + 1
	}
	if math.IsInf(hi-lo, 0) || math.IsNaN(hi-lo) {
		// No finite step covers the range; callers reject such input.
		return []float64{lo, hi}, lo, hi
	}
	n = max(n, 2)
	raw := float64((hi - lo) / float64(n))
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	step := mag
	for _, k := range []float64{1, 2, 5, 10} {
		if raw <= float64(k*mag) {
			step = float64(k * mag)
			break
		}
	}
	// Round to the step's decimals so that labels read 0.6, not
	// 0.6000000000000001.
	scale := math.Pow(10, math.Max(0, -math.Floor(math.Log10(step))))
	clean := func(v float64) float64 { return float64(math.Round(float64(v*scale)) / scale) }
	from = clean(float64(math.Floor(float64(lo/step)) * step))
	to = clean(float64(math.Ceil(float64(hi/step)) * step))
	for i := 0; i <= maxTicks; i++ {
		v := clean(from + float64(float64(i)*step))
		if v > to+float64(step/2) {
			break
		}
		ticks = append(ticks, v)
	}
	return ticks, from, to
}

// Format writes v compactly: integers without a fraction, others with up
// to four significant decimals.
func Format(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'g', 6, 64)
}
