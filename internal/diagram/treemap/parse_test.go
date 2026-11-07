package treemap

import (
	"context"
	"errors"
	"testing"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
)

func parse(src string) (*Diagram, error) {
	lines, err := source.Split(src)
	if err != nil {
		return nil, err
	}
	return Parse(guard.New(context.Background(), 500), lines)
}

func checkErrors(t *testing.T, cases []struct {
	name, src string
	line      int
}) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parse(tc.src)
			var se *source.SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("got %v, want a syntax error", err)
			}
			if se.Line != tc.line {
				t.Errorf("line %d, want %d (%s)", se.Line, tc.line, se.Msg)
			}
		})
	}
}

func TestParse(t *testing.T) {
	d, err := parse("treemap-beta\n\"A\"\n  \"A1\": 10\n  \"A2\"\n    \"A21\": 5:::hot\n\"B\": 7\nclassDef hot fill:#f00")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Nodes) != 6 || len(d.Nodes[0].Children) != 2 {
		t.Fatalf("%d nodes", len(d.Nodes))
	}
	if a := d.Nodes[1]; a.Label != "A" || a.Value != 15 || len(a.Children) != 2 {
		t.Errorf("A = %+v", a)
	}
	if leaf := d.Nodes[4]; leaf.Label != "A21" || leaf.Class != "hot" || leaf.Style.Fill == nil {
		t.Errorf("A21 = %+v", leaf)
	}
}

func TestSquarify(t *testing.T) {
	rs := squarify([]float64{6, 6, 4, 3, 2, 2, 1}, sceneRect(6, 4))
	area := 0.0
	for _, r := range rs {
		area += r.W * r.H
	}
	if area < 23.99 || area > 24.01 {
		t.Errorf("areas sum to %v, want 24", area)
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"no quotes", "treemap-beta\nA: 1", 2},
		{"child of leaf", "treemap-beta\n\"A\": 1\n  \"B\": 2", 3},
		{"bad value", "treemap-beta\n\"A\": x", 2},
	})
}

func sceneRect(w, h float64) scene.Rect { return scene.Rect{W: w, H: h} }
