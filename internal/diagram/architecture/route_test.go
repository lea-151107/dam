package architecture

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/scene"
)

// crosses reports whether segment a-b runs through the inside of r.
func crosses(a, b scene.Point, r scene.Rect) bool {
	if a.Y == b.Y {
		return a.Y > r.Y && a.Y < r.Y+r.H && math.Max(a.X, b.X) > r.X && math.Min(a.X, b.X) < r.X+r.W
	}
	return a.X > r.X && a.X < r.X+r.W && math.Max(a.Y, b.Y) > r.Y && math.Min(a.Y, b.Y) < r.Y+r.H
}

// TestRouteAroundServices routes the edge of a cycle (c:R -- L:a) that the
// old L-shaped path drew straight through all three services.
func TestRouteAroundServices(t *testing.T) {
	work := 0
	rt := &router{env: &diagram.Env{G: guard.New(context.Background(), 500), Work: &work}}
	rects := []scene.Rect{{X: 0, Y: 0, W: 80, H: 40}, {X: 150, Y: 0, W: 80, H: 40}, {X: 300, Y: 0, W: 80, H: 40}}
	const out = 23.0
	a1 := scene.Point{X: 380 + out, Y: 20} // right of C
	b1 := scene.Point{X: -out, Y: 20}      // left of A
	path, err := rt.routeOrth(a1, b1, scene.Point{X: 1}, scene.Point{X: -1}, rects, out, newOccupancy())
	if err != nil || path == nil {
		t.Fatalf("no path: %v", err)
	}
	if path[0] != a1 || path[len(path)-1] != b1 {
		t.Errorf("path %v does not run from a1 to b1", path)
	}
	for k := 1; k < len(path); k++ {
		a, b := path[k-1], path[k]
		if a.X != b.X && a.Y != b.Y {
			t.Errorf("segment %v-%v is not orthogonal", a, b)
		}
		for _, r := range rects {
			if crosses(a, b, r) {
				t.Errorf("segment %v-%v crosses service %v", a, b, r)
			}
		}
	}
	// A second edge on the same way avoids running along the first.
	used := newOccupancy()
	used.add(path)
	again, err := rt.routeOrth(a1, b1, scene.Point{X: 1}, scene.Point{X: -1}, rects, out, used)
	if err != nil || again == nil {
		t.Fatalf("no second path: %v", err)
	}
	for k := 1; k < len(again)-1; k++ {
		// The ends must meet the services' sides; the middle stays clear.
		if hit, _ := used.covers(again[k-1], again[k]); k > 1 && k < len(again)-2 && hit {
			t.Errorf("second path runs along the first at %v-%v", again[k-1], again[k])
		}
	}
}

// TestRouteBudget checks that the search stops at the Render call's
// budget instead of running on (a diagram near the element limit used to
// take tens of seconds).
func TestRouteBudget(t *testing.T) {
	work := maxRouteWork - 10
	rt := &router{env: &diagram.Env{G: guard.New(context.Background(), 500), Work: &work}}
	var rects []scene.Rect
	for i := 0; i < 40; i++ {
		rects = append(rects, scene.Rect{X: float64(i%8) * 150, Y: float64(i/8) * 100, W: 80, H: 40})
	}
	_, err := rt.routeOrth(scene.Point{X: -20, Y: 20}, scene.Point{X: 1200, Y: 420}, scene.Point{X: -1}, scene.Point{X: 1}, rects, 23, newOccupancy())
	if !errors.Is(err, guard.ErrTooLarge) {
		t.Errorf("over budget: %v, want ErrTooLarge", err)
	}
}
