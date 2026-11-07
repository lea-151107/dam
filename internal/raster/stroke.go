package raster

import (
	"math"

	"github.com/lea/mermaid/internal/scene"
)

// joinSegments is the polygon resolution of round joins and caps.
const joinSegments = 12

// strokePolys converts a polyline into positively oriented polygons: one
// quad per segment plus a disk at each vertex (round joins and caps).
func strokePolys(pts []scene.Point, width float64) [][]scene.Point {
	hw := float64(width / 2)
	var out [][]scene.Point
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		d := b.Sub(a)
		if d.Len() < 1e-6 {
			continue
		}
		n := scene.Point{X: -d.Y, Y: d.X}.Norm().Mul(hw)
		out = append(out, orient([]scene.Point{a.Add(n), b.Add(n), b.Sub(n), a.Sub(n)}))
	}
	if hw >= 0.75 {
		for i, p := range pts {
			// Interior joins of nearly straight lines need no disk.
			if i > 0 && i+1 < len(pts) && straight(pts[i-1], p, pts[i+1]) {
				continue
			}
			out = append(out, disk(p, hw))
		}
	}
	if len(out) == 0 && len(pts) > 0 {
		out = append(out, disk(pts[0], hw))
	}
	return out
}

func straight(a, b, c scene.Point) bool {
	u, v := b.Sub(a).Norm(), c.Sub(b).Norm()
	return float64(u.X*v.X)+float64(u.Y*v.Y) > 0.999
}

func disk(c scene.Point, r float64) []scene.Point {
	pts := make([]scene.Point, joinSegments)
	for i := range pts {
		a := float64(2 * math.Pi * float64(i) / joinSegments)
		pts[i] = scene.Point{X: c.X + float64(r*math.Cos(a)), Y: c.Y + float64(r*math.Sin(a))}
	}
	return pts
}

func orient(p []scene.Point) []scene.Point {
	area := 0.0
	for i := range p {
		q := p[(i+1)%len(p)]
		// float64(...): no FMA, so the sign is the same on every CPU.
		area += float64(p[i].X*q.Y) - float64(q.X*p[i].Y)
	}
	if area < 0 {
		for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
			p[i], p[j] = p[j], p[i]
		}
	}
	return p
}

// applyDash splits a polyline into dashes. An empty pattern returns the
// polyline unchanged.
func applyDash(pts []scene.Point, dash []float64) [][]scene.Point {
	if len(dash) == 0 || len(pts) < 2 {
		return [][]scene.Point{pts}
	}
	total := 0.0
	for _, d := range dash {
		total += d
	}
	if total <= 0 {
		return [][]scene.Point{pts}
	}
	var out [][]scene.Point
	idx := 0
	left := dash[0]
	on := true
	var cur []scene.Point
	if on {
		cur = []scene.Point{pts[0]}
	}
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		seg := b.Sub(a)
		l := seg.Len()
		pos := 0.0
		for l-pos > left {
			pos += left
			p := a.Add(seg.Mul(float64(pos / l)))
			if on {
				cur = append(cur, p)
				out = append(out, cur)
				cur = nil
			} else {
				cur = []scene.Point{p}
			}
			on = !on
			idx = (idx + 1) % len(dash)
			left = dash[idx]
			if left <= 0 {
				left = 1e-3
			}
		}
		left -= l - pos
		if on {
			cur = append(cur, b)
		}
	}
	if on && len(cur) > 1 {
		out = append(out, cur)
	}
	return out
}
