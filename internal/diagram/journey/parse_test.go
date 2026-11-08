package journey

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
	d, err := parse("journey\ntitle Day\nsection Work\nMake tea: 5: Me\nDo work: 1: Me, Cat\nRest: 3")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Day" || len(d.Tasks) != 3 || len(d.Actors) != 2 {
		t.Fatalf("%+v", d)
	}
	if task := d.Tasks[1]; task.Name != "Do work" || task.Score != 1 || len(task.Actors) != 2 || task.Section != 0 {
		t.Errorf("task 1 = %+v", task)
	}
	if task := d.Tasks[2]; task.Score != 3 || len(task.Actors) != 0 {
		t.Errorf("task 2 = %+v", task)
	}
}

func TestParseErrors(t *testing.T) {
	checkErrors(t, []struct {
		name, src string
		line      int
	}{
		{"no score", "journey\nsection S\nTask: Me", 3},
		{"score too high", "journey\nTask: 9: Me", 2},
	})
}
