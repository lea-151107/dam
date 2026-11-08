package pie

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

// summary writes the title, showData and the slices.
func summary(d *Diagram) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q show=%v |", d.Title, d.ShowData)
	for _, s := range d.Slices {
		fmt.Fprintf(&b, " %s=%g", s.Label, s.Value)
	}
	return b.String()
}

func TestParse(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"basic", "pie\n\"Dogs\" : 386\n\"Cats\" : 85.5", "\"\" show=false | Dogs=386 Cats=85.5"},
		{"title inline", "pie title Pets\n\"a\" : 1", "\"Pets\" show=false | a=1"},
		{"showData", "pie showData\ntitle T\n\"a\" : 1\n\"b\" : 2", "\"T\" show=true | a=1 b=2"},
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
		{"bad value", "pie\n\"a\" : 1\n\"b\" : x", 3},
		{"negative", "pie\n\"a\" : -1", 2},
		{"missing quote", "pie\n\"a : 1", 2},
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
