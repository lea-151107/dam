package class

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

// summary writes classes as id{label}[annotations;attributes;methods] and
// relations as from-to:label with cardinalities.
func summary(d *Diagram) string {
	var b strings.Builder
	for _, c := range d.Classes {
		fmt.Fprintf(&b, "%s", c.ID)
		if c.Label != "" && c.Label != c.ID {
			fmt.Fprintf(&b, "{%s}", c.Label)
		}
		fmt.Fprintf(&b, "[%s;%s;%s] ", strings.Join(c.Annotations, ","), strings.Join(c.Attributes, ","), strings.Join(c.Methods, ","))
	}
	b.WriteString("|")
	for _, r := range d.Relations {
		fmt.Fprintf(&b, " %s-%s", d.Classes[r.From].ID, d.Classes[r.To].ID)
		if r.Dashed {
			b.WriteString("(dashed)")
		}
		if r.Label != "" || r.FromCard != "" || r.ToCard != "" {
			fmt.Fprintf(&b, ":%s/%s/%s", r.Label, r.FromCard, r.ToCard)
		}
	}
	return b.String()
}

func TestParse(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"members", "classDiagram\nclass A\nA : +int x\nA : +f() bool", "A[;+int x;+f() bool] |"},
		{"body", "classDiagram\nclass A {\n<<interface>>\n+name String\n+run()\n}", "A[interface;+name String;+run()] |"},
		{"relations", "classDiagram\nA <|-- B\nC ..> D : uses\nE \"1\" *-- \"many\" F", "A[;;] B[;;] C[;;] D[;;] E[;;] F[;;] | A-B C-D(dashed):uses// E-F:/1/many"},
		{"generic", "classDiagram\nclass Box~T~", "Box{Box<T>}[;;] |"},
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
		{"unclosed body", "classDiagram\nclass A {\n+x", 2},
		{"bad statement", "classDiagram\nclass A\n<<<", 3},
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
