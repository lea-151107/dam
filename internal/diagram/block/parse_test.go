package block

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
	d, err := parse("block-beta\ncolumns 3\na[\"A [x]\"]:2 b((B))\nblock:g:3\ncolumns 2\nc space:2\nend\narr<[\"Go\"]>(down)\na --> b\nb -- \"lbl\" --- c")
	if err != nil {
		t.Fatal(err)
	}
	r := d.Root
	if r.Columns != 3 || len(r.Items) != 4 {
		t.Fatalf("root %+v", r)
	}
	if a := r.Items[0]; a.Label != "A [x]" || a.Span != 2 || a.Shape != Rect {
		t.Errorf("a = %+v", a)
	}
	if b := r.Items[1]; b.Shape != Circle {
		t.Errorf("b = %+v", b)
	}
	g := r.Items[2]
	if g.Shape != Group || g.Span != 3 || g.Columns != 2 || len(g.Items) != 2 || g.Items[1].Shape != Space || g.Items[1].Span != 2 {
		t.Errorf("group = %+v", g)
	}
	if arr := r.Items[3]; arr.Shape != Arrow || arr.ArrowDir != "down" || arr.Label != "Go" {
		t.Errorf("arrow = %+v", arr)
	}
	if len(d.Edges) != 2 || !d.Edges[0].Arrow || d.Edges[1].Arrow || d.Edges[1].Label != "lbl" {
		t.Errorf("edges %+v", d.Edges)
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"unclosed block", "block-beta\na\nblock:g\nb", 3},
		{"stray end", "block-beta\nend", 2},
		{"bad columns", "block-beta\ncolumns x", 2},
		{"unknown edge end", "block-beta\na\na --> z", 3},
		{"bad arrow", "block-beta\nx<[\"X\"]>(sideways)", 2},
	})
}
