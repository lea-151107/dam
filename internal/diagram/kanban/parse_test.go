package kanban

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
	d, err := parse("kanban\nTodo\n  [Write docs]\n  t2[Fix bug]@{ ticket: X-1, priority: 'High' }\ndone[Done]\n  a")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Columns) != 2 || d.Columns[0].Label != "Todo" || d.Columns[1].Label != "Done" {
		t.Fatalf("%+v", d.Columns)
	}
	if c := d.Columns[0].Cards[1]; c.ID != "t2" || c.Label != "Fix bug" || c.Ticket != "X-1" || c.Priority != "High" {
		t.Errorf("card = %+v", c)
	}
	if c := d.Columns[1].Cards[0]; c.Label != "a" {
		t.Errorf("card = %+v", c)
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"unknown field", "kanban\nTodo\n  a[A]@{ color: red }", 3},
		{"column metadata", "kanban\nTodo@{ ticket: 1 }", 2},
	})
}
