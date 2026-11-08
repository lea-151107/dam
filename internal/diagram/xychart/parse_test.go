package xychart

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
	d, err := parse("xychart-beta horizontal\ntitle \"Sales\"\nx-axis \"Month\" [jan, \"feb, late\", mar]\ny-axis \"Revenue\" 0 --> 100\nbar \"A\" [10, 20, 30]\nline [5, 15, 25]")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Horizontal || d.Title != "Sales" || d.XTitle != "Month" || d.YTitle != "Revenue" || d.YMin != 0 || d.YMax != 100 {
		t.Errorf("%+v", d)
	}
	if len(d.Categories) != 3 || d.Categories[1] != "feb, late" {
		t.Errorf("categories %q", d.Categories)
	}
	if len(d.Series) != 2 || d.Series[0].Name != "A" || d.Series[0].Line || !d.Series[1].Line || d.Series[1].Values[2] != 25 {
		t.Errorf("series %+v", d.Series)
	}
	// Without a y range, it covers the data and zero.
	d, err = parse("xychart-beta\nx-axis 1 --> 5\nbar [3, 7]")
	if err != nil {
		t.Fatal(err)
	}
	if d.YMin != 0 || d.YMax != 7 || d.XMin != 1 || d.XMax != 5 || d.xLabel(1) != "5" {
		t.Errorf("range %v..%v, x %v..%v, label %q", d.YMin, d.YMax, d.XMin, d.XMax, d.xLabel(1))
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"bad value", "xychart-beta\nbar [1, x]", 2},
		{"bad range", "xychart-beta\ny-axis 10 --> 5\nbar [1]", 2},
		{"subnormal y range", "xychart-beta\ny-axis 0 --> 1e-320\nbar [0]", 2},
		{"subnormal x range", "xychart-beta\nx-axis 0 --> 1e-320\nbar [1]", 2},
		{"subnormal values", "xychart-beta\nbar [0, 1e-320]", 2},
		{"unknown statement", "xychart-beta\nbar [1]\npie [2]", 3},
		{"no data", "xychart-beta\ntitle T", 2},
	})
}
