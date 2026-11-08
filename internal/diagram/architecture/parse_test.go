package architecture

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
	d, err := parse("architecture-beta\ngroup api(cloud)[API]\nservice db(database)[DB] in api\nservice s(server)[Server]\njunction j in api\ndb:L <-[reads]-> R:s\ns{group}:T -- B:j")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Groups) != 1 || len(d.Nodes) != 3 || len(d.Edges) != 2 {
		t.Fatalf("%+v", d)
	}
	if db := d.Nodes[0]; db.Icon != "database" || db.Label != "DB" || db.Group != 0 {
		t.Errorf("db = %+v", db)
	}
	if !d.Nodes[2].Junction {
		t.Error("j is not a junction")
	}
	if e := d.Edges[0]; e.FromSide != 'L' || e.ToSide != 'R' || !e.FromArrow || !e.ToArrow || e.Label != "reads" {
		t.Errorf("edge = %+v", e)
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"unknown group", "architecture-beta\nservice a in nope", 2},
		{"unknown service", "architecture-beta\nservice a\na:L -- R:b", 3},
		{"duplicate", "architecture-beta\nservice a\nservice a", 3},
		{"bad side", "architecture-beta\nservice a\nservice b\na:X -- R:b", 4},
	})
}

// TestPlace checks the grid placement by edge sides, including a push past
// an occupied cell and a disconnected service set beside the rest.
func TestPlace(t *testing.T) {
	d, err := parse("architecture-beta\nservice a\nservice b\nservice c\nservice d\nservice e\na:R -- L:b\na:B -- T:c\nc:R -- L:d\nb:B -- T:d")
	if err != nil {
		t.Fatal(err)
	}
	pos := d.place()
	want := []gpos{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {2, 0}}
	for i, p := range want {
		if pos[i] != p {
			t.Errorf("%s at %v, want %v", d.Nodes[i].ID, pos[i], p)
		}
	}
	// b:L -- R:x with x's cell taken by a: x is pushed further left.
	d, err = parse("architecture-beta\nservice a\nservice b\nservice x\na:R -- L:b\nb:L -- R:x")
	if err != nil {
		t.Fatal(err)
	}
	if p := d.place()[2]; p != (gpos{-1, 0}) {
		t.Errorf("x at %v, want {-1 0}", p)
	}
}

// TestUnits checks how services map to the units placed at each level:
// a group is one unit in its parent, so that its frame holds only its own
// members.
func TestUnits(t *testing.T) {
	d, err := parse("architecture-beta\ngroup g[G]\ngroup h[H] in g\nservice a in g\nservice b\nservice c in h")
	if err != nil {
		t.Fatal(err)
	}
	root := d.children(-1)
	if len(root) != 2 || root[0] != (unit{-1, 0}) || root[1] != (unit{1, -1}) {
		t.Fatalf("root units %v", root)
	}
	if d.within(-1, 0, root) != 0 || d.within(-1, 2, root) != 0 || d.within(-1, 1, root) != 1 {
		t.Errorf("root: a in %d, c in %d, b in %d", d.within(-1, 0, root), d.within(-1, 2, root), d.within(-1, 1, root))
	}
	inG := d.children(0)
	if len(inG) != 2 || d.within(0, 2, inG) != 0 || d.within(0, 0, inG) != 1 || d.within(0, 1, inG) != -1 {
		t.Errorf("g units %v: c in %d, a in %d, b in %d", inG, d.within(0, 2, inG), d.within(0, 0, inG), d.within(0, 1, inG))
	}
}
