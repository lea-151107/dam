package state

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/layout/sugiyama"
	"github.com/lea/mermaid/internal/source"
)

func parse(src string) (*Diagram, error) {
	lines, err := source.Split(src)
	if err != nil {
		return nil, err
	}
	return Parse(guard.New(context.Background(), 500), lines)
}

// summary writes states as "id"[label] with their kind and composite
// parent (ids are quoted: start and end states have internal ids),
// parent, then transitions as from->to:label.
func summary(d *Diagram) string {
	var b strings.Builder
	for _, s := range d.States {
		fmt.Fprintf(&b, "%q[%s]k%d", s.ID, s.Label, s.Kind)
		if len(s.Desc) > 0 {
			fmt.Fprintf(&b, "%q", s.Desc)
		}
		if s.Parent >= 0 {
			fmt.Fprintf(&b, "^%d", s.Parent)
		}
		b.WriteByte(32)
	}
	b.WriteString("|")
	for _, t := range d.Transitions {
		fmt.Fprintf(&b, " %q->%q", d.States[t.From].ID, d.States[t.To].ID)
		if t.Label != "" {
			fmt.Fprintf(&b, ":%s", t.Label)
		}
	}
	return b.String()
}

func TestParse(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"start and end", "stateDiagram-v2\n[*] --> A\nA --> B : go\nB --> [*]", "\"\\x00start-1\"[]k1 \"A\"[A]k0 \"B\"[B]k0 \"\\x00end-1\"[]k2 | \"\\x00start-1\"->\"A\" \"A\"->\"B\":go \"B\"->\"\\x00end-1\""},
		{"composite", "stateDiagram-v2\nstate P {\n[*] --> C\n}\n[*] --> P", "\"P\"[P]k0 \"\\x00start0\"[]k1^0 \"C\"[C]k0^0 \"\\x00start-1\"[]k1 | \"\\x00start0\"->\"C\" \"\\x00start-1\"->\"P\""},
		{"fork", "stateDiagram-v2\nstate f <<fork>>\n[*] --> f\nf --> A\nf --> B", "\"f\"[f]k4 \"\\x00start-1\"[]k1 \"A\"[A]k0 \"B\"[B]k0 | \"\\x00start-1\"->\"f\" \"f\"->\"A\" \"f\"->\"B\""},
		{"description", "stateDiagram-v2\nA : first line\nA : second", "\"A\"[A]k0[\"first line\" \"second\"] |"},
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
		{"unclosed composite", "stateDiagram-v2\nA --> B\nstate P {\nC --> D", 3},
		{"region outside composite", "stateDiagram-v2\nA --> B\n--", 3},
		{"stray brace", "stateDiagram-v2\nA --> B\n}", 3},
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

func TestParseRegions(t *testing.T) {
	d, err := parse("stateDiagram-v2\nstate P {\ndirection LR\na --> b\n--\nc --> d\n--\ne\n}\nx --> P")
	if err != nil {
		t.Fatal(err)
	}
	if d.Dir != sugiyama.TB {
		t.Errorf("direction inside a composite changed the diagram's: %v", d.Dir)
	}
	p := d.Composites[0]
	if !p.HasDir || p.Dir != sugiyama.LR || p.Region {
		t.Errorf("composite %+v", p)
	}
	var regions []int
	for i, c := range d.Composites {
		if c.Region {
			if c.Parent != 0 {
				t.Errorf("region %d has parent %d", i, c.Parent)
			}
			regions = append(regions, i)
		}
	}
	if len(regions) != 3 {
		t.Fatalf("%d regions", len(regions))
	}
	parentOf := map[string]int{}
	for _, s := range d.States {
		parentOf[s.ID] = s.Parent
	}
	if parentOf["a"] != regions[0] || parentOf["c"] != regions[1] || parentOf["e"] != regions[2] || parentOf["x"] != -1 {
		t.Errorf("parents %v, regions %v", parentOf, regions)
	}
}
