package timeline

import (
	"context"
	"errors"
	"testing"

	"github.com/lea/mermaid/internal/guard"
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
	d, err := parse("timeline\ntitle T\nsection S1\n2002 : A\n2004 : B : C\n     : D\nsection S2\n2005")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "T" || len(d.Sections) != 2 || len(d.Periods) != 3 {
		t.Fatalf("%+v", d)
	}
	if p := d.Periods[1]; p.Title != "2004" || len(p.Events) != 3 || p.Events[2] != "D" || p.Section != 0 {
		t.Errorf("period 1 = %+v", p)
	}
	if p := d.Periods[2]; p.Title != "2005" || len(p.Events) != 0 || p.Section != 1 {
		t.Errorf("period 2 = %+v", p)
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"continuation first", "timeline\n: A", 2},
		{"no periods", "timeline\ntitle T", 2},
	})
}
