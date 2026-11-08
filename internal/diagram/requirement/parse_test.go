package requirement

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
	d, err := parse("requirementDiagram\nrequirement r1 {\nid: 1\ntext: hello\n}\nelement e {\ntype: sim\n}\ne - satisfies -> r1\nr1 <- traces - e")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Nodes) != 2 || d.Nodes[0].Kind != "requirement" || d.Nodes[1].Kind != "element" || len(d.Nodes[0].Fields) != 2 {
		t.Fatalf("%+v", d.Nodes)
	}
	if len(d.Relations) != 2 || d.Relations[0].From != 1 || d.Relations[0].Kind != "satisfies" || d.Relations[1].From != 1 || d.Relations[1].To != 0 {
		t.Errorf("relations %+v", d.Relations)
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"unclosed", "requirementDiagram\nrequirement r {\nid: 1", 2},
		{"unknown field", "requirementDiagram\nrequirement r {\ncolor: red\n}", 3},
		{"unknown relation", "requirementDiagram\na - likes -> b", 2},
		{"unknown block", "requirementDiagram\nwish w {\n}", 2},
	})
}
