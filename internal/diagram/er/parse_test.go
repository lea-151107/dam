package er

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

// summary writes entities as id[label](attributes) and relations as
// from-cards-to:label with the crow's foot marker values, d for dashed.
func summary(d *Diagram) string {
	var b strings.Builder
	for _, e := range d.Entities {
		fmt.Fprintf(&b, "%s[%s](", e.ID, e.Label)
		for i, a := range e.Attributes {
			if i > 0 {
				b.WriteString(";")
			}
			fmt.Fprintf(&b, "%s %s %s %s", a.Type, a.Name, strings.Join(a.Keys, ","), a.Comment)
		}
		b.WriteString(") ")
	}
	b.WriteString("|")
	for _, r := range d.Relations {
		fmt.Fprintf(&b, " %s-%d-%d-%s:%s", d.Entities[r.From].ID, r.FromCard, r.ToCard, d.Entities[r.To].ID, r.Label)
		if !r.Identifying {
			b.WriteString("(d)")
		}
	}
	return b.String()
}

func TestParse(t *testing.T) {
	// Marker values: OneOnly 9, ZeroOrOne 10, OneOrMore 11, ZeroOrMore 12.
	for _, tc := range []struct{ name, src, want string }{
		{"symbols", "erDiagram\nA ||--o{ B : has\nB }|..|{ C : \"uses it\"", "A[A]() B[B]() C[C]() | A-9-12-B:has B-11-11-C:uses it(d)"},
		{"words", "erDiagram\nA one or more to zero or many B : x\nB only one optionally to zero or one C : y", "A[A]() B[B]() C[C]() | A-11-12-B:x B-9-10-C:y(d)"},
		{"attributes", "erDiagram\nT {\n  string id PK, FK \"the key\"\n  int n\n}", "T[T](string id PK,FK the key;int n  ) |"},
		{"alias", "erDiagram\np[\"Person\"] {\nstring name\n}\np ||--|| q : is", "p[Person](string name  ) q[q]() | p-9-9-q:is"},
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
		{"unclosed entity", "erDiagram\nA ||--o{ B : x\nA {\nstring n", 3},
		{"bad attribute", "erDiagram\nA {\nstring\n}", 3},
		{"bad relation", "erDiagram\nA ||--o B", 2},
		{"bad direction", "erDiagram\ndirection XY", 2},
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
