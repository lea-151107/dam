package sankey

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
	d, err := parse("sankey-beta\na,b,10\n\"c, d\",b,2.5\nb,\"e \"\"x\"\"\",12.5")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Nodes) != 4 || d.Nodes[2] != "c, d" || d.Nodes[3] != `e "x"` || len(d.Links) != 3 || d.Links[1].Value != 2.5 {
		t.Fatalf("nodes %q links %+v", d.Nodes, d.Links)
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"two fields", "sankey-beta\na,b", 2},
		{"bad value", "sankey-beta\na,b,x", 2},
		{"self", "sankey-beta\na,a,1", 2},
		{"cycle", "sankey-beta\na,b,1\nb,a,1", 3},
		{"open quote", "sankey-beta\n\"a,b,1", 2},
	})
}
