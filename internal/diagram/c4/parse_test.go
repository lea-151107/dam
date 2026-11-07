package c4

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
	d, err := parse("C4Container\ntitle T\nSystem_Boundary(b, \"Bank\") {\nContainer(web, \"Web, App\", \"Go\", \"Serves pages\")\nContainerDb_Ext(db, \"DB\")\n}\nPerson(u, \"User\")\nBiRel(u, web, \"Uses\", \"HTTPS\")\nUpdateElementStyle(u, $bgColor=\"red\")")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "T" || len(d.Elements) != 3 || len(d.Boundaries) != 1 || len(d.Rels) != 1 {
		t.Fatalf("%+v", d)
	}
	web := d.Elements[0]
	if web.Label != "Web, App" || web.Kind != "container" || web.Techn != "Go" || web.Descr != "Serves pages" || web.Boundary != 0 {
		t.Errorf("web = %+v", web)
	}
	if db := d.Elements[1]; db.Variant != "db" || !db.External {
		t.Errorf("db = %+v", db)
	}
	if u := d.Elements[2]; u.Kind != "person" || u.Boundary != -1 || u.Fill == nil {
		t.Errorf("user = %+v", u)
	}
	if r := d.Rels[0]; !r.Both || r.Techn != "HTTPS" || r.From != 2 || r.To != 0 {
		t.Errorf("rel = %+v", r)
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"unclosed boundary", "C4Context\nBoundary(b, \"B\") {\nPerson(p, \"P\")", 2},
		{"unknown macro", "C4Context\nRobot(r, \"R\")", 2},
		{"not a call", "C4Context\nPerson p", 2},
		{"stray brace", "C4Context\n}", 2},
	})
}
