// Package packet parses and lays out Mermaid packet diagrams
// (packet-beta): bit fields drawn in rows of 32 bits.
package packet

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
	"github.com/lea/mermaid/internal/textgrid"
	"github.com/lea/mermaid/internal/theme"
)

// Field is a run of bits.
type Field struct {
	Start, End int // inclusive
	Label      string
}

// Diagram is a parsed packet diagram.
type Diagram struct {
	Title  string
	Fields []Field
}

// bitsPerRow is the width of a row; maxBits bounds the packet size.
const (
	bitsPerRow = 32
	maxBits    = 1 << 16
)

var fieldRe = regexp.MustCompile(`^(\+?\d+)(?:\s*-\s*(\d+))?\s*:\s*"([^"]*)"$`)

// Parse parses a packet diagram.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{}
	next := 0
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		if r, ok := source.CutKeyword(t, "title"); ok {
			d.Title = source.Label(r)
			continue
		}
		if strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr") {
			// NOTE(mermaid-compat): accessibility texts do not affect the image.
			continue
		}
		m := fieldRe.FindStringSubmatch(t)
		if m == nil {
			return nil, source.Errorf(l.No, `expected start-end: "label"`)
		}
		var f Field
		if n, ok := strings.CutPrefix(m[1], "+"); ok {
			count, err := strconv.Atoi(n)
			if errors.Is(err, strconv.ErrRange) && m[2] == "" && !strings.HasPrefix(n, "-") {
				// Too large for int (on 32-bit platforms already near 2^31).
				return nil, guard.Limit("bits", maxBits+1, maxBits)
			}
			if err != nil || count < 1 || m[2] != "" {
				return nil, source.Errorf(l.No, "invalid bit count %q", m[1])
			}
			if count > maxBits {
				// next+count-1 would overflow for a count near MaxInt and
				// slip past the check of f.End below.
				return nil, guard.Limit("bits", count, maxBits)
			}
			f.Start, f.End = next, next+count-1
		} else {
			start, err := strconv.Atoi(m[1])
			if errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(m[1], "-") {
				return nil, guard.Limit("bits", maxBits+1, maxBits)
			}
			if err != nil {
				return nil, source.Errorf(l.No, "invalid bit %q", m[1])
			}
			end := start
			if m[2] != "" {
				if end, err = strconv.Atoi(m[2]); errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(m[2], "-") {
					return nil, guard.Limit("bits", maxBits+1, maxBits)
				} else if err != nil || end < start {
					return nil, source.Errorf(l.No, "invalid range %s-%s", m[1], m[2])
				}
			}
			if start != next {
				return nil, source.Errorf(l.No, "field starts at bit %d, expected %d", start, next)
			}
			f.Start, f.End = start, end
		}
		if f.End >= maxBits {
			return nil, guard.Limit("bits", min(f.End, maxBits)+1, maxBits)
		}
		f.Label = source.Label(m[3])
		d.Fields = append(d.Fields, f)
		next = f.End + 1
		if err := g.Count("fields", len(d.Fields)); err != nil {
			return nil, err
		}
	}
	if len(d.Fields) == 0 {
		return nil, source.Errorf(lines[len(lines)-1].No, "packet has no fields")
	}
	return d, nil
}

// DiagramTitle returns the title given with the title statement.
func (d *Diagram) DiagramTitle() string { return d.Title }

// segment is the part of a field within one row.
type segment struct {
	field      int
	row        int
	start, end int // bits within the row, inclusive
}

func (d *Diagram) segments() []segment {
	var out []segment
	for i, f := range d.Fields {
		for b := f.Start; b <= f.End; {
			row := b / bitsPerRow
			end := min(f.End, row*bitsPerRow+bitsPerRow-1)
			out = append(out, segment{i, row, b - row*bitsPerRow, end - row*bitsPerRow})
			b = end + 1
		}
	}
	return out
}

// Visual constants in layout units.
const (
	bitW   = 20.0
	rowH   = 34.0
	rowGap = 6.0
	lineW  = 1.2
	numGap = 2.0
)

// Layout implements diagram.Layouter: each row has 32 bit columns; fields
// are boxes with their first and last bit numbers above.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	bw := bitW
	if env.MaxWidth > 0 {
		bw = min(bw, max(float64(env.ContentWidth()/bitsPerRow), 8))
	}
	lh := m.LineHeight()
	numH := float64(lh * 0.8)
	sc := &scene.Scene{}
	bottom := 0.0
	for _, s := range d.segments() {
		if err := env.G.Tick(); err != nil {
			return nil, err
		}
		y := float64(float64(s.row)*(rowH+numH+numGap+rowGap)) + numH + numGap
		r := scene.Rect{X: float64(float64(s.start) * bw), Y: y, W: float64(float64(s.end-s.start+1) * bw), H: rowH}
		sc.Fill(scene.RectPath(r), theme.Mix(t.Base, t.Color(s.field), 0.85), t.NodeStroke, lineW)
		lines := m.Lines(d.Fields[s.field].Label, r.W-4)
		sc.Label(r.X+float64(r.W/2), r.Y+float64(r.H/2), lines, scene.Center, t.Text)
		f := d.Fields[s.field]
		first, last := s.start+s.row*bitsPerRow, s.end+s.row*bitsPerRow
		if first == f.Start {
			sc.Label(r.X+2, y-numGap-float64(numH/2), []string{strconv.Itoa(first)}, scene.Left, t.Muted)
		}
		if last == f.End && last != first {
			sc.Label(r.X+r.W-2, y-numGap-float64(numH/2), []string{strconv.Itoa(last)}, scene.Right, t.Muted)
		}
		bottom = max(bottom, r.Y+r.H)
	}
	return diagram.Finish(sc, env, float64(bitsPerRow*bw), bottom), nil
}

// Text draws the rows with two columns per bit when they fit, and lists
// the fields otherwise.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	if 2*bitsPerRow+1 > maxCols {
		var out []string
		for _, f := range d.Fields {
			out = append(out, textgrid.Wrap(fmt.Sprintf("%d-%d  %s", f.Start, f.End, strings.ReplaceAll(f.Label, "\n", " ")), maxCols)...)
		}
		return out, nil
	}
	segs := d.segments()
	rows := segs[len(segs)-1].row + 1
	// Each row takes a line of bit numbers and a box of three lines.
	grid, err := textgrid.New(2*bitsPerRow+1, 4*rows)
	if err != nil {
		return nil, err
	}
	for _, s := range segs {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		x, y := 2*s.start, 4*s.row+1
		w := 2*(s.end-s.start+1) + 1
		grid.Box(x, y, w, 3, [4]rune{})
		label := strings.ReplaceAll(d.Fields[s.field].Label, "\n", " ")
		if room := w - 2; textgrid.Width(label) > room {
			label = shorten(label, room)
		}
		grid.Text(x+(w-textgrid.Width(label))/2, y+1, label)
		f := d.Fields[s.field]
		if first := s.start + s.row*bitsPerRow; first == f.Start {
			grid.Text(x, y-1, strconv.Itoa(first))
		}
	}
	return grid.Lines(), nil
}

// shorten cuts s to w columns, ending with … .
func shorten(s string, w int) string {
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		rw := textgrid.RuneWidth(r)
		if n+rw > w-1 {
			break
		}
		b.WriteRune(r)
		n += rw
	}
	return b.String() + "…"
}
