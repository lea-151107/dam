// Package kanban parses and lays out Mermaid kanban boards.
package kanban

import (
	"fmt"
	"image/color"
	"regexp"
	"sort"
	"strings"

	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
	"github.com/lea/mermaid/internal/textgrid"
	"github.com/lea/mermaid/internal/theme"
)

// Card is an item in a column.
type Card struct {
	ID, Label                  string
	Ticket, Assigned, Priority string
}

// Column holds cards.
type Column struct {
	ID, Label string
	Cards     []Card
}

// Diagram is a parsed kanban board.
type Diagram struct {
	Columns []Column
}

var itemRe = regexp.MustCompile(`^([\p{L}\p{N}_\-]*)\s*(?:\[(.*)\])?\s*(?:@\{(.*)\})?$`)

// Parse parses a kanban board. The least indented lines are columns; the
// lines indented more are cards in the column above them.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{}
	colIndent := -1
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		if strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr") {
			// NOTE(mermaid-compat): accessibility texts do not affect the image.
			continue
		}
		ind := source.Indent(l.Text)
		m := itemRe.FindStringSubmatch(t)
		if m == nil {
			return nil, source.Errorf(l.No, "expected id[label] or [label]")
		}
		label := m[2]
		if label == "" {
			label = m[1]
		}
		if label == "" {
			return nil, source.Errorf(l.No, "empty item")
		}
		if colIndent < 0 || ind <= colIndent {
			colIndent = ind
			if m[3] != "" {
				return nil, source.Errorf(l.No, "columns take no @{...} metadata")
			}
			d.Columns = append(d.Columns, Column{ID: m[1], Label: source.Label(label)})
		} else {
			c := Card{ID: m[1], Label: source.Label(label)}
			if m[3] != "" {
				attrs, err := source.ParseAttrs(m[3])
				if err != nil {
					return nil, source.Errorf(l.No, "metadata: %v", err)
				}
				// In key order, so that the error for an unknown key is
				// always the same one.
				keys := make([]string, 0, len(attrs))
				for k := range attrs {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					s := source.Label(fmt.Sprint(attrs[k]))
					switch k {
					case "ticket":
						c.Ticket = s
					case "assigned":
						c.Assigned = s
					case "priority":
						c.Priority = s
					default:
						return nil, source.Errorf(l.No, "unknown card field %q", k)
					}
				}
			}
			col := &d.Columns[len(d.Columns)-1]
			col.Cards = append(col.Cards, c)
		}
		n := 0
		for _, c := range d.Columns {
			n += 1 + len(c.Cards)
		}
		if err := g.Count("columns and cards", n); err != nil {
			return nil, err
		}
	}
	if len(d.Columns) == 0 {
		return nil, source.Errorf(lines[len(lines)-1].No, "kanban has no columns")
	}
	return d, nil
}

// Visual constants in layout units.
const (
	colW    = 200.0
	colGap  = 14.0
	pad     = 10.0
	cardGap = 8.0
	stripeW = 5.0
	radius  = 6.0
	lineW   = 1.2
	metaGap = 4.0
)

// Layout implements diagram.Layouter: columns side by side, cards stacked.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	lh := m.LineHeight()
	w := float64(colW * max(env.Spacing, 0.6))
	if env.Wrap > 0 {
		w = min(w, env.Wrap+2*pad)
	}
	inner := w - 2*pad
	sc := &scene.Scene{}
	bottom := 0.0
	for i, col := range d.Columns {
		x := float64(float64(i) * (w + colGap))
		title := m.Lines(col.Label, inner)
		_, th := m.Size(title)
		y := pad + th + pad
		type placed struct {
			r     scene.Rect
			lines []string
			card  Card
		}
		var cards []placed
		for _, c := range col.Cards {
			lines := m.Lines(c.Label, inner-2*pad)
			_, h := m.Size(lines)
			h += float64(2 * pad)
			if c.Ticket != "" || c.Assigned != "" {
				h += lh + metaGap
			}
			cards = append(cards, placed{scene.Rect{X: x + pad, Y: y, W: inner, H: h}, lines, c})
			y += h + cardGap
		}
		colH := y - cardGap + pad
		if len(cards) == 0 {
			colH = pad + th + pad
		}
		bottom = max(bottom, colH)
		sc.Fill(scene.RoundRect(scene.Rect{X: x, Y: 0, W: w, H: colH}, radius), theme.Mix(t.Base, t.Accent, 0.08), color.NRGBA{}, 0)
		sc.Label(x+float64(w/2), pad+float64(th/2), title, scene.Center, t.Text)
		for _, p := range cards {
			sc.Fill(scene.RoundRect(p.r, radius), t.Base, t.NodeStroke, lineW)
			if s, ok := stripe(t, p.card.Priority); ok {
				sc.Fill(scene.RoundRect(scene.Rect{X: p.r.X, Y: p.r.Y, W: stripeW, H: p.r.H}, stripeW/2), s, s, 0)
			}
			_, h := m.Size(p.lines)
			sc.Label(p.r.X+pad, p.r.Y+pad+float64(h/2), p.lines, scene.Left, t.Text)
			my := p.r.Y + p.r.H - pad - float64(lh/2)
			if p.card.Ticket != "" {
				sc.Label(p.r.X+pad, my, []string{p.card.Ticket}, scene.Left, t.Muted)
			}
			if p.card.Assigned != "" {
				sc.Label(p.r.X+p.r.W-pad, my, []string{p.card.Assigned}, scene.Right, t.Muted)
			}
		}
	}
	width := float64(float64(len(d.Columns))*(w+colGap)) - colGap
	return diagram.Finish(sc, env, width, bottom), nil
}

// stripe returns the color of the stripe that marks a card's priority.
func stripe(t theme.Theme, p string) (color.NRGBA, bool) {
	switch strings.ToLower(p) {
	case "very high":
		return t.Critical, true
	case "high":
		return theme.Mix(t.Critical, t.Color(1), 0.5), true
	case "low":
		return t.Strong(0), true
	case "very low":
		return t.Muted, true
	}
	return color.NRGBA{}, false
}

// Text renders each column as a heading with its cards as a list; the
// ticket, assignee and priority follow in parentheses.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	if maxCols < minCols {
		return nil, guard.Limit("columns", minCols, maxCols)
	}
	var out []string
	for i, col := range d.Columns {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, textgrid.Wrap("■ "+strings.ReplaceAll(col.Label, "\n", " "), maxCols)...)
		for _, c := range col.Cards {
			text := strings.ReplaceAll(c.Label, "\n", " ")
			var meta []string
			for _, s := range []string{c.Ticket, c.Assigned, c.Priority} {
				if s != "" {
					meta = append(meta, s)
				}
			}
			if len(meta) > 0 {
				text += " (" + strings.Join(meta, ", ") + ")"
			}
			for k, l := range textgrid.Wrap(text, maxCols-4) {
				prefix := "    "
				if k == 0 {
					prefix = "  • "
				}
				out = append(out, prefix+l)
			}
		}
	}
	return out, nil
}

// minCols is the narrowest output that leaves room for a card.
const minCols = 8
