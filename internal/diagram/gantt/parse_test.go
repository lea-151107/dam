package gantt

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// summary writes sections, then tasks as name(id)@section with dates and
// flags.
func summary(d *Diagram) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q %v |", d.Title, d.Sections)
	for _, t := range d.Tasks {
		fmt.Fprintf(&b, " %s(%s)@%d %s..%s", t.Name, t.ID, t.Section, t.Start.Format("01-02"), t.End.Format("01-02"))
		for _, f := range []struct {
			on   bool
			name string
		}{{t.Done, "done"}, {t.Active, "active"}, {t.Crit, "crit"}, {t.Milestone, "milestone"}} {
			if f.on {
				b.WriteString("," + f.name)
			}
		}
	}
	return b.String()
}

func TestParse(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"after", "gantt\ndateFormat YYYY-MM-DD\ntitle T\nsection S\nfirst :a1, 2024-01-01, 3d\nsecond :after a1, 2d", "\"T\" [S] | first(a1)@0 01-01..01-04 second()@0 01-04..01-06"},
		{"flags", "gantt\ndateFormat YYYY-MM-DD\nsection A\nt1 :done, 2024-02-01, 1d\nsection B\nt2 :crit, active, 2024-02-02, 1w\nm :milestone, 2024-02-10, 0d", "\"\" [A B] | t1()@0 02-01..02-02,done t2()@1 02-02..02-09,active,crit m()@1 02-10..02-10,milestone"},
		{"excludes weekends", "gantt\ndateFormat YYYY-MM-DD\nexcludes weekends\nt :2024-01-05, 2d", "\"\" [] | t()@0 01-05..01-09"},
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
		{"unknown after", "gantt\ndateFormat YYYY-MM-DD\nt :after nope, 1d", 3},
		{"bad date", "gantt\ndateFormat YYYY-MM-DD\nt :2024-13-45, 1d", 3},
		{"bad duration", "gantt\ndateFormat YYYY-MM-DD\nt :2024-01-01, 3x", 3},
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

func TestTickLabelsDistinct(t *testing.T) {
	// A few days at a wide width chose 12-hour ticks, and the date-only
	// axisFormat labelled each date twice.
	for _, src := range []string{
		"gantt\ndateFormat YYYY-MM-DD\nsection S\na : 2020-01-01, 3d\nb : 2020-01-02, 2d",
		"gantt\ndateFormat YYYY-MM-DD\naxisFormat %b %Y\na : 2020-01-01, 40d",
		"gantt\ndateFormat YYYY-MM-DD HH:mm\naxisFormat %H:%M\na : 2020-01-01 10:00, 2h",
	} {
		d, err := parse(src)
		if err != nil {
			t.Fatal(err)
		}
		lo, hi := d.span()
		for _, w := range []float64{300, 800, 2000} {
			ticks, err := d.ticks(lo, hi, w, 60)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, tk := range ticks {
				l := formatTime(tk, d.AxisFormat)
				if seen[l] {
					t.Errorf("%q at width %v: label %q repeats", src, w, l)
				}
				seen[l] = true
			}
		}
	}
}
