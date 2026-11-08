package quadrant

import (
	"context"
	"errors"
	"testing"

	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/source"
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
	d, err := parse("quadrantChart\ntitle T\nx-axis Low --> High\ny-axis Bottom\nquadrant-1 Q1\nquadrant-3 Q3\nA: [0.3, 0.6]\nB:::hot: [0.9, 0.1] radius: 8\nclassDef hot color: #ff0000")
	if err != nil {
		t.Fatal(err)
	}
	if d.XLow != "Low" || d.XHigh != "High" || d.YLow != "Bottom" || d.YHigh != "" || d.Quadrants[0] != "Q1" || d.Quadrants[2] != "Q3" {
		t.Errorf("axes and quadrants: %+v", d)
	}
	if len(d.Points) != 2 {
		t.Fatalf("%d points", len(d.Points))
	}
	if p := d.Points[1]; p.Label != "B" || p.Radius != 8 || p.Fill == nil || p.Fill.R != 0xff || quadrantOf(p) != 3 {
		t.Errorf("point B = %+v", p)
	}
	if quadrantOf(d.Points[0]) != 1 {
		t.Errorf("A is in quadrant %d", quadrantOf(d.Points[0])+1)
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"out of range", "quadrantChart\nA: [0.3, 1.6]", 2},
		{"bad style", "quadrantChart\nA: [0.3, 0.6] radius: x", 2},
		{"unknown quadrant", "quadrantChart\nquadrant-5 X", 2},
		{"unknown class", "quadrantChart\nA:::nope: [0.1, 0.1]", 2},
	})
}
