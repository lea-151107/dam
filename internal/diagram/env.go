// Package diagram holds the environment shared by all diagram renderers.
package diagram

import (
	"errors"
	"image/color"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/textmeasure"
	"github.com/lea/mermaid/internal/theme"
)

// Env is passed to every layout call.
type Env struct {
	G *guard.Guard
	M *textmeasure.Measurer
	T theme.Theme
	// Spacing multiplies gaps between elements; < 1 compacts the layout.
	Spacing float64
	// Wrap is the label wrap width in layout units (0 = no wrapping).
	Wrap float64
	// MaxWidth is the target width in layout units (0 = unlimited). Layouts
	// whose width is a free parameter (gantt) use it directly.
	MaxWidth float64
	// Work counts the search steps of layouts that search (architecture
	// edge routing) over all attempts of one Render call, which retries
	// the layout to fit MaxWidth; nil when not counted.
	Work *int
}

// Spend adds n steps to Work and fails with ErrTooLarge past limit.
func (e *Env) Spend(n, limit int, what string) error {
	if e.Work == nil {
		return nil
	}
	*e.Work += n
	if *e.Work > limit {
		return budgetError{guard.Limit(what, *e.Work, limit)}
	}
	return nil
}

// ErrBudget matches (errors.Is) the errors of Spend. Render tells them
// apart from other limits: a retry that runs out of the budget falls back
// to the layout before it.
var ErrBudget = errors.New("work budget exhausted")

// budgetError is a Spend failure: ErrTooLarge, and also ErrBudget.
type budgetError struct{ err error }

func (b budgetError) Error() string        { return b.err.Error() }
func (b budgetError) Unwrap() error        { return b.err }
func (b budgetError) Is(target error) bool { return target == ErrBudget }

// Label splits and wraps a label.
func (e *Env) Label(s string) []string { return e.M.Lines(s, e.Wrap) }

// Margin is the space around every diagram, in layout units.
const Margin = 8

// Layouter turns a parsed diagram into a scene.
type Layouter interface {
	Layout(env *Env) (*scene.Scene, error)
}

// Finish computes the bounds of everything drawn (at least w×h from the
// origin), moves the content so that it starts at Margin, and sets the
// canvas size.
func Finish(sc *scene.Scene, env *Env, w, h float64) *scene.Scene {
	bb := scene.Rect{W: w, H: h}
	for _, it := range sc.Items {
		switch v := it.(type) {
		case *scene.Shape:
			if len(v.Path.Pts) == 0 {
				continue
			}
			bb = bb.Union(v.Path.Bounds().Inset(float64(v.Stroke.Width / 2)))
		case *scene.Text:
			tw, th := env.M.Size(v.Lines)
			x := v.X - float64(tw/2)
			switch v.Align {
			case scene.Left:
				x = v.X
			case scene.Right:
				x = v.X - tw
			}
			bb = bb.Union(scene.Rect{X: x, Y: v.Y - float64(th/2), W: tw, H: th})
		}
	}
	sc.Translate(scene.Point{X: Margin - bb.X, Y: Margin - bb.Y})
	sc.Width = bb.W + 2*Margin
	sc.Height = bb.H + 2*Margin
	return sc
}

// titleGap is the space between a title and the diagram, in layout units.
const titleGap = 8

// AddTitle draws title (wrapped like labels) centered above the diagram
// in sc, which Finish has completed, and grows the canvas to fit it.
func AddTitle(sc *scene.Scene, env *Env, title string) *scene.Scene {
	lines := env.Label(title)
	if len(lines) == 0 {
		return sc
	}
	tw, th := env.M.Size(lines)
	w := max(sc.Width, tw+2*Margin)
	sc.Translate(scene.Point{X: float64((w - sc.Width) / 2), Y: th + titleGap})
	sc.Width = w
	sc.Height += th + titleGap
	sc.Label(float64(w/2), Margin+float64(th/2), lines, scene.Center, env.T.Text)
	return sc
}

// Readable is theme.Readable, for packages below diagram that may not
// import theme.
func Readable(text, fill color.NRGBA) color.NRGBA { return theme.Readable(text, fill) }

// strokeSlack is how far strokes may reach past the shapes they outline
// (Finish counts half of each stroke width): diagrams that size their
// content to MaxWidth leave this much room.
const strokeSlack = 4

// ContentWidth returns the width a diagram may give its content so that,
// with the margins and strokes added by Finish, it fits MaxWidth. Sized to
// MaxWidth - 2*Margin, packet, quadrant and treemap came out 1-2 px too
// wide at every width and were shrunk and then scaled as a bitmap.
func (e *Env) ContentWidth() float64 { return e.MaxWidth - 2*Margin - strokeSlack }
