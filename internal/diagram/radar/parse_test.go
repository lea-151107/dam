package radar

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
	d, err := parse("radar-beta\naxis a[\"A\"], b, c\ncurve x[\"X\"]{1, 2, 3}, y{c: 9, a: 4}\ngraticule circle\nticks 4\nshowLegend false")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Axes) != 3 || d.Axes[0].Label != "A" || len(d.Curves) != 2 || !d.Circle || d.Ticks != 4 || d.ShowLegend {
		t.Fatalf("%+v", d)
	}
	if y := d.Curves[1]; y.Values[0] != 4 || y.Values[1] != 0 || y.Values[2] != 9 {
		t.Errorf("named values %v", y.Values)
	}
	if d.Max != 10 {
		t.Errorf("max %v, want the nice value above the data", d.Max)
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"two axes", "radar-beta\naxis a, b", 2},
		{"wrong count", "radar-beta\naxis a, b, c\ncurve x{1, 2}", 3},
		{"unknown axis", "radar-beta\naxis a, b, c\ncurve x{d: 1}", 3},
		{"bad graticule", "radar-beta\ngraticule star", 2},
		{"infinite range", "radar-beta\naxis a, b, c\nmin -1e308\ncurve x{1e308, 0, 1e308}", 4},
		{"subnormal range", "radar-beta\naxis a, b, c\nmax 1e-320\ncurve x{0, 1e-320, 0}", 4},
	})
}
