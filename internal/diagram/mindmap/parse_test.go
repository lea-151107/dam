package mindmap

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// summary writes nodes as label(shape)^parent.
func summary(d *Diagram) string {
	var b strings.Builder
	for _, n := range d.Nodes {
		fmt.Fprintf(&b, "%s(%d)^%d ", n.Label, n.Shape, n.Parent)
	}
	return b.String()
}

func TestParse(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"tree", "mindmap\nroot\n  a\n    a1\n  b", "root(0)^-1 a(0)^0 a1(0)^1 b(0)^0 "},
		{"shapes", "mindmap\nroot((R))\n  sq[Square]\n  rd(Round)\n  hx{{Hex}}", "R(3)^-1 Square(1)^0 Round(2)^0 Hex(6)^0 "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := parse(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			if got := summary(d); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		line      int
	}{
		{"two roots", "mindmap\nroot\nother", 3},
	} {
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
