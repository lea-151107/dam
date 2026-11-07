package sugiyama

import (
	"context"
	"testing"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/scene"
)

func overlap(a, b scene.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func TestNoOverlap(t *testing.T) {
	nodes := []Node{
		{W: 40, H: 20, Cluster: -1}, {W: 60, H: 20, Cluster: 0}, {W: 30, H: 30, Cluster: 0},
		{W: 50, H: 20, Cluster: 1}, {W: 20, H: 20, Cluster: -1}, {W: 80, H: 40, Cluster: 1},
	}
	clusters := []Cluster{{Parent: -1, TitleH: 10, TitleW: 30}, {Parent: 0, TitleH: 10, TitleW: 20}}
	edges := []Edge{
		{From: 0, To: 1, MinLen: 1}, {From: 1, To: 2, MinLen: 1, HasLabel: true, LabelW: 30, LabelH: 10},
		{From: 2, To: 3, MinLen: 1}, {From: 3, To: 0, MinLen: 1}, {From: 4, To: 5, MinLen: 2},
		{From: 0, To: 5, MinLen: 1}, {From: 5, To: 5, MinLen: 1},
	}
	for _, dir := range []Dir{TB, BT, LR, RL} {
		res, err := Layout(guard.New(context.Background(), 100), nodes, edges, clusters, Config{Dir: dir, NodeSep: 10, RankSep: 20, ClusterPad: 8})
		if err != nil {
			t.Fatal(err)
		}
		rect := func(i int) scene.Rect {
			c := res.Nodes[i]
			return scene.Rect{X: c.X - nodes[i].W/2, Y: c.Y - nodes[i].H/2, W: nodes[i].W, H: nodes[i].H}
		}
		for i := range nodes {
			r := rect(i)
			if r.X < -1e-6 || r.Y < -1e-6 || r.X+r.W > res.Width+1e-6 || r.Y+r.H > res.Height+1e-6 {
				t.Errorf("dir %d: node %d %+v outside %vx%v", dir, i, r, res.Width, res.Height)
			}
			for j := i + 1; j < len(nodes); j++ {
				if overlap(r, rect(j)) {
					t.Errorf("dir %d: nodes %d and %d overlap", dir, i, j)
				}
			}
			// Nodes outside a cluster must not intersect its box.
			for c, box := range res.Clusters {
				inside := false
				for _, k := range clusterChain(nodes[i].Cluster, clusters) {
					inside = inside || k == c
				}
				if !inside && overlap(r, box) {
					t.Errorf("dir %d: node %d overlaps cluster %d", dir, i, c)
				}
			}
		}
		if res.Edges[6] != nil {
			t.Errorf("self loop should have no polyline")
		}
		for i, e := range res.Edges[:6] {
			if len(e) < 2 {
				t.Errorf("edge %d has %d points", i, len(e))
			}
		}
	}
}

func TestCrossingsReduced(t *testing.T) {
	// K(2,2)-like graph drawn in the bad order initially.
	nodes := make([]Node, 6)
	for i := range nodes {
		nodes[i] = Node{W: 10, H: 10, Cluster: -1}
	}
	edges := []Edge{{From: 0, To: 3, MinLen: 1}, {From: 1, To: 2, MinLen: 1}, {From: 2, To: 5, MinLen: 1}, {From: 3, To: 4, MinLen: 1}}
	res, err := Layout(guard.New(context.Background(), 100), nodes, edges, nil, Config{NodeSep: 10, RankSep: 10})
	if err != nil {
		t.Fatal(err)
	}
	// 0→3→4 and 1→2→5 should not cross: x order must be consistent.
	if (res.Nodes[0].X < res.Nodes[1].X) != (res.Nodes[3].X < res.Nodes[2].X) {
		t.Error("edges cross")
	}
}
