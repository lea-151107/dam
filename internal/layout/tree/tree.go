// Package tree lays out trees horizontally (children to the right of their
// parent, subtrees stacked vertically).
package tree

import "github.com/lea-151107/dam/internal/scene"

// Node is a tree node with its size and children.
type Node struct {
	W, H     float64
	Children []int
}

// Layout places the subtrees rooted at roots in a column starting at x0
// (left edge of the roots) and y = 0. Centers are written to pos. It
// returns the total height.
func Layout(nodes []Node, roots []int, hgap, vgap, x0 float64, pos []scene.Point) float64 {
	h := make([]float64, len(nodes))
	var measure func(i int) float64
	measure = func(i int) float64 {
		n := nodes[i]
		ch := 0.0
		for k, c := range n.Children {
			if k > 0 {
				ch += vgap
			}
			ch += measure(c)
		}
		h[i] = max(n.H, ch)
		return h[i]
	}
	var place func(i int, x, y float64)
	place = func(i int, x, y float64) {
		n := nodes[i]
		pos[i] = scene.Point{X: x + float64(n.W/2), Y: y + float64(h[i]/2)}
		ch := 0.0
		for k, c := range n.Children {
			if k > 0 {
				ch += vgap
			}
			ch += h[c]
		}
		cy := y + float64((h[i]-ch)/2)
		for _, c := range n.Children {
			place(c, x+n.W+hgap, cy)
			cy += h[c] + vgap
		}
	}
	total := 0.0
	for k, r := range roots {
		if k > 0 {
			total += vgap
		}
		total += measure(r)
	}
	y := 0.0
	for _, r := range roots {
		place(r, x0, y)
		y += h[r] + vgap
	}
	return total
}
