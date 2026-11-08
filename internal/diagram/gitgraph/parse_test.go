package gitgraph

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

// summary writes branches, then commits as id@branch with parents, tags
// and merge/cherry-pick flags.
func summary(d *Diagram) string {
	var b strings.Builder
	for _, br := range d.Branches {
		fmt.Fprintf(&b, "%s ", br.Name)
	}
	b.WriteString("|")
	for _, c := range d.Commits {
		fmt.Fprintf(&b, " %s@%s%v", c.ID, d.Branches[c.Branch].Name, c.Parents)
		if len(c.Tags) > 0 {
			fmt.Fprintf(&b, "%v", c.Tags)
		}
		if c.Merge {
			b.WriteString("M")
		}
		if c.Cherry {
			b.WriteString("C")
		}
	}
	return b.String()
}

func TestParse(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"branch and merge", "gitGraph\ncommit id: \"a\"\nbranch dev\ncommit id: \"b\"\ncheckout main\nmerge dev id: \"m\" tag: \"v1\"", "main dev | a@main[] b@dev[0] m@main[0 1][v1]M"},
		{"auto ids", "gitGraph\ncommit\ncommit", "main | 0-50ca8af@main[] 1-40ca71c@main[0]"},
		{"cherry-pick", "gitGraph\ncommit id: \"a\"\nbranch dev\ncommit id: \"b\"\ncheckout main\ncherry-pick id: \"b\"", "main dev | a@main[] b@dev[0] 2-70cabd5@main[0][cherry-pick: b]C"},
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
		{"unknown branch", "gitGraph\ncommit\ncheckout nope", 3},
		{"duplicate branch", "gitGraph\ncommit\nbranch dev\nbranch dev", 4},
		{"unknown commit", "gitGraph\ncommit\ncherry-pick id: \"x\"", 3},
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
