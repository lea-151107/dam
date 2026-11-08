package flowchart

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/source"
)

func parse(src string) (*Graph, error) {
	lines, err := source.Split(src)
	if err != nil {
		return nil, err
	}
	return Parse(guard.New(context.Background(), 500), lines)
}

// summary writes nodes as id[label] (with the shape number when it is not
// a rectangle and the subgraph index when inside one), then edges as
// from-kind->to:label, then subgraphs as id/title^parent. Shapes and
// kinds are the constant values (Round 1, Circle 5, Rhombus 7; Thick 1,
// Dotted 2).
func summary(g *Graph) string {
	var b strings.Builder
	for _, n := range g.Nodes {
		fmt.Fprintf(&b, "%s[%s]", n.ID, n.Label)
		if n.Shape != Rect {
			fmt.Fprintf(&b, "s%d", n.Shape)
		}
		if n.Cluster >= 0 {
			fmt.Fprintf(&b, "@%d", n.Cluster)
		}
		b.WriteByte(' ')
	}
	b.WriteString("|")
	for _, e := range g.Edges {
		fmt.Fprintf(&b, " %s-%d->%s", g.Nodes[e.From].ID, e.Kind, g.Nodes[e.To].ID)
		if e.Label != "" {
			fmt.Fprintf(&b, ":%s", e.Label)
		}
	}
	b.WriteString(" |")
	for _, s := range g.Subgraphs {
		fmt.Fprintf(&b, " %s/%s^%d", s.ID, s.Title, s.Parent)
	}
	return b.String()
}

func TestParse(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"chain", "graph TD\nA --> B --> C", "A[A] B[B] C[C] | A-0->B B-0->C |"},
		{"labels and shapes", "graph LR\nA[Start] -->|go| B(Round)\nB -.-> C{Choice}\nC ==> D((Circle))", "A[Start] B[Round]s1 C[Choice]s7 D[Circle]s5 | A-0->B:go B-2->C C-1->D |"},
		{"inline label", "graph TD\nA -- text --> B", "A[A] B[B] | A-0->B:text |"},
		{"ampersand", "graph TD\nA & B --> C", "A[A] B[B] C[C] | A-0->C B-0->C |"},
		{"subgraph", "graph TD\nsubgraph one [Title]\na --> b\nend\nb --> c", "a[a]@0 b[b]@0 c[c] | a-0->b b-0->c | one/Title^-1"},
		{"nested subgraph", "graph TD\nsubgraph outer\nsubgraph inner\nx\nend\nend", "x[x]@1 | | outer/outer^-1 inner/inner^0"},
		{"entities", "graph TD\nA[\"a #amp; b\"]", "A[a & b] | |"},
		{"mermaid 11 shapes", "graph LR\nA@{ shape: cyl, label: \"DB\" } e1@--> B@{ shape: doc }\ne1@{ curve: linear, animate: true }", "A[DB]s4 B[B]s14 | A-0->B |"},
		{"multi-line markdown label", "graph LR\nA[\"`one\ntwo`\"]", "A[one\ntwo] | |"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := parse(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			if got := summary(g); got != tc.want {
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
		{"unclosed subgraph", "graph TD\nA\nsubgraph s\nB", 3},
		{"stray end", "graph TD\nA\nend", 3},
		{"bad bracket", "graph TD\nA --> B\nC[unclosed", 3},
		{"unknown direction", "graph TD\nsubgraph s\ndirection XY\nend", 3},
		{"unknown shape", "graph TD\nA --> B\nC@{ shape: blob }", 3},
		{"unclosed attributes", "graph TD\nA@{ shape: rect", 2},
		{"duplicate edge id", "graph TD\nA e1@--> B\nB e1@--> C", 3},
		{"edge id on several edges", "graph TD\nA & B e1@--> C", 2},
		{"unknown curve", "graph TD\nA e1@--> B\ne1@{ curve: zigzag }", 3},
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

// TestLinkTextWithoutSpaces checks the label forms without a space after
// the opening dashes, which Mermaid reads as link text (final review).
func TestLinkTextWithoutSpaces(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"graph LR\nA--Yes-->B", "A[A] B[B] | A-0->B:Yes |"},
		{"graph LR\nA==big==>B", "A[A] B[B] | A-1->B:big |"},
		{"graph LR\nA-.dot.->B", "A[A] B[B] | A-2->B:dot |"},
		{"graph LR\nA -- sp --> B", "A[A] B[B] | A-0->B:sp |"},
		{"graph LR\nA---B", "A[A] B[B] | A-0->B |"},
	} {
		g, err := parse(c.src)
		if err != nil {
			t.Errorf("%q: %v", c.src, err)
			continue
		}
		if got := summary(g); !strings.HasPrefix(got, c.want) {
			t.Errorf("%q:\n got %s\nwant %s…", c.src, got, c.want)
		}
	}
}
