package packet

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
	d, err := parse("packet-beta\ntitle P\n0-15: \"A\"\n+8: \"B\"\n24: \"C\"\n25-40: \"D\"")
	if err != nil {
		t.Fatal(err)
	}
	want := []Field{{0, 15, "A"}, {16, 23, "B"}, {24, 24, "C"}, {25, 40, "D"}}
	if d.Title != "P" || len(d.Fields) != len(want) {
		t.Fatalf("%+v", d)
	}
	for i, f := range want {
		if d.Fields[i] != f {
			t.Errorf("field %d = %+v, want %+v", i, d.Fields[i], f)
		}
	}
	// D crosses a row boundary: two segments.
	segs := d.segments()
	if n := len(segs); n != 5 || segs[3].end != 31 || segs[4].row != 1 || segs[4].end != 8 {
		t.Errorf("segments %+v", segs)
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"gap", "packet-beta\n0-7: \"A\"\n9-10: \"B\"", 3},
		{"backwards", "packet-beta\n0-7: \"A\"\n8-6: \"B\"", 3},
		{"no quotes", "packet-beta\n0-7: A", 2},
	})
}
