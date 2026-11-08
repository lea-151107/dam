package sequence

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

// summary writes participants (with * for actors), then events as
// kind:from>to:text (with d for dotted messages).
func summary(d *Diagram) string {
	var b strings.Builder
	for _, p := range d.Participants {
		fmt.Fprintf(&b, "%s[%s]", p.ID, p.Label)
		if p.Actor {
			b.WriteString("*")
		}
		b.WriteByte(32)
	}
	b.WriteString("|")
	for _, e := range d.Events {
		fmt.Fprintf(&b, " %d:%d>%d:%s", e.Kind, e.From, e.To, e.Text)
		if e.Dotted {
			b.WriteString(":d")
		}
		if e.Block != "" {
			fmt.Fprintf(&b, ":%s", e.Block)
		}
		if e.Number != 0 {
			fmt.Fprintf(&b, "#%d", e.Number)
		}
	}
	return b.String()
}

func TestParse(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"messages", "sequenceDiagram\nA->>B: hi\nB-->>A: ok", "A[A] B[B] | 0:0>1:hi 0:1>0:ok:d"},
		{"aliases and actor", "sequenceDiagram\nparticipant A as Alice\nactor B as Bob\nA->>B: x", "A[Alice] B[Bob]* | 0:0>1:x"},
		{"note", "sequenceDiagram\nA->>B: x\nNote over A,B: n", "A[A] B[B] | 0:0>1:x 1:0>1:n"},
		{"blocks", "sequenceDiagram\nloop every\nA->>B: x\nend\nalt yes\nA->>B: y\nelse no\nB->>A: z\nend", "A[A] B[B] | 2:0>0:every:loop 0:0>1:x 4:0>0: 2:0>0:yes:alt 0:0>1:y 3:0>0:no:else 0:1>0:z 4:0>0:"},
		{"create and destroy", "sequenceDiagram\nA->>B: x\ncreate participant C\nB->>C: new\ndestroy C\nC->>B: bye", "A[A] B[B] C[C] | 0:0>1:x 7:2>0: 0:1>2:new 8:2>0: 0:2>1:bye"},
		{"autonumber", "sequenceDiagram\nautonumber\nA->>B: x\nB->>A: y", "A[A] B[B] | 0:0>1:x#1 0:1>0:y#2"},
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
		{"unclosed loop", "sequenceDiagram\nA->>B: x\nloop l\nA->>B: y", 3},
		{"stray end", "sequenceDiagram\nA->>B: x\nend", 3},
		{"else outside alt", "sequenceDiagram\nelse x", 2},
		{"bad statement", "sequenceDiagram\nA->>B: x\n???", 3},
		{"nested box", "sequenceDiagram\nbox A\nbox B\nend\nend", 3},
		{"create without participant", "sequenceDiagram\ncreate A", 2},
		{"destroy without participant", "sequenceDiagram\nA->>B: x\ndestroy", 3},
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

func TestParseBoxesAndTitle(t *testing.T) {
	d, err := parse("sequenceDiagram\ntitle: Hello\nbox Aqua Front end\nparticipant A\nactor B\nend\nbox Back\nparticipant C\nend\nbox transparent\nend\nA->>C: x")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Hello" {
		t.Errorf("title %q", d.Title)
	}
	if len(d.Boxes) != 3 {
		t.Fatalf("%d boxes", len(d.Boxes))
	}
	if b := d.Boxes[0]; b.Label != "Front end" || b.Color == nil || b.Color.B != 0xff || fmt.Sprint(b.Members) != "[0 1]" {
		t.Errorf("box 0 = %+v (color %v)", b, b.Color)
	}
	if b := d.Boxes[1]; b.Label != "Back" || b.Color != nil || fmt.Sprint(b.Members) != "[2]" {
		t.Errorf("box 1 = %+v", b)
	}
	if b := d.Boxes[2]; b.Color == nil || b.Color.A != 0 {
		t.Errorf("transparent box = %+v", b)
	}
}

// TestAutonumber covers autonumber's start, step and off (final review).
func TestAutonumber(t *testing.T) {
	d, err := parse("sequenceDiagram\nautonumber 10 5\nA->>B: x\nA->>B: y\nautonumber off\nA->>B: z\nautonumber\nB->>A: w\nautonumber -3\nB->>A: v")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ev := range d.Events {
		if ev.Kind != Message {
			continue
		}
		if ev.Numbered {
			got = append(got, fmt.Sprint(ev.Number))
		} else {
			got = append(got, "-")
		}
	}
	// autonumber without a start continues the count (20), as Mermaid's
	// renderer keeps the current number.
	if s := strings.Join(got, " "); s != "10 15 - 20 -3" {
		t.Errorf("numbers %s, want 10 15 - 20 -3", s)
	}
	d, err = parse("sequenceDiagram\nautonumber 1 0\nA->>B: x\nA->>B: y\nautonumber 0\nA->>B: z")
	if err != nil {
		t.Fatal(err)
	}
	got = got[:0]
	for _, ev := range d.Events {
		if ev.Kind == Message {
			got = append(got, fmt.Sprint(ev.Number))
		}
	}
	if s := strings.Join(got, " "); s != "1 2 3" {
		t.Errorf("zero step and start keep the current ones: %s, want 1 2 3", s)
	}
	for _, bad := range []string{"autonumber x", "autonumber 1 2 3", "autonumber 99999999999"} {
		if _, err := parse("sequenceDiagram\n" + bad + "\nA->>B: x"); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}
