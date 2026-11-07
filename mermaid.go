// Package mermaid renders Mermaid diagrams to images in pure Go.
//
// Render returns an *image.RGBA; RenderText returns box-drawing text. Both are
// deterministic: the same input, options and Version give the same output.
package mermaid

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"strings"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"

	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/raster"
	"github.com/lea/mermaid/internal/richtext"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
	"github.com/lea/mermaid/internal/textgrid"
	"github.com/lea/mermaid/internal/textmeasure"
	"github.com/lea/mermaid/internal/theme"
)

// fitAttempt is one step of the MaxWidth strategy.
type fitAttempt struct {
	spacing float64
	wrap    float64 // label wrap width in layout units, 0 = none
}

// fitAttempts are tried in order until the drawing fits; they change
// spacing and wrapping, never the text size.
var fitAttempts = [...]fitAttempt{
	{1, 0}, {0.6, 0}, {0.6, 240}, {0.45, 160}, {0.4, 110}, {0.35, 80},
}

const (
	// minScale and maxScale bound Options.Scale. Below minScale the text is
	// under 1px and the image a few pixels across (Scale 1e-9 gave a 1×1
	// image without an error).
	minScale = 1.0 / 16
	maxScale = 16.0
	// maxShrinkSteps bounds font-size reduction with a resizable font.
	maxShrinkSteps = 4
	// minTextPx is the smallest text size (em, in output pixels) the fit
	// strategy will produce. A drawing that only fits with smaller text is
	// reported as ErrTooLarge instead of being returned unreadable.
	minTextPx = 5.0
	// slackDownscale is the bitmap reduction always allowed: the shrink loop
	// can end a few pixels too wide, and a small reduction keeps text
	// legible even below minTextPx.
	slackDownscale = 0.9
	// capPerEm and linePerEm estimate the em size of a fixed Face, whose
	// size is otherwise unknown. Cap height is about 0.7 em in Latin and CJK
	// fonts alike (Go Regular, Noto Sans, Noto Sans CJK: within 5%); line
	// height varies more (CJK fonts are about 1.45 em), so it is only the
	// fallback for faces that report no cap height.
	capPerEm  = 0.7
	linePerEm = 1.2
)

// ownTitle is implemented by diagrams that draw their own title (pie,
// gantt): the front matter title becomes theirs unless they have one.
type ownTitle interface {
	SetDefaultTitle(string)
}

// titled is implemented by diagrams with a title statement of their own
// (sequence), which wins over the front matter title.
type titled interface {
	DiagramTitle() string
}

// diagramTitle returns the title the root draws above d, and hands the
// front matter title to diagrams that draw their own.
func diagramTitle(d diagram.Layouter, doc *source.Doc) string {
	if ot, ok := d.(ownTitle); ok {
		ot.SetDefaultTitle(doc.Title)
		return ""
	}
	if tt, ok := d.(titled); ok && tt.DiagramTitle() != "" {
		return tt.DiagramTitle()
	}
	return doc.Title
}

// textDiagram is implemented by diagrams that support RenderText.
type textDiagram interface {
	Text(g *guard.Guard, maxCols int) ([]string, error)
}

func parse(g *guard.Guard, t DiagramType, lines []source.Line) (diagram.Layouter, error) {
	k := kindOf(t)
	if k == nil {
		return nil, fmt.Errorf("%w: diagram type %s", ErrUnsupported, t)
	}
	return k.parse(g, lines)
}

// Render parses src and draws it.
func Render(ctx context.Context, src string, opt Options) (img image.Image, err error) {
	defer func() {
		if r := recover(); r != nil {
			img, err = nil, fmt.Errorf("mermaid: internal error: %v", r)
		}
	}()
	out, err := render(ctx, src, opt)
	if err != nil {
		return nil, convertErr(err)
	}
	return out, nil
}

func render(ctx context.Context, src string, opt Options) (*image.RGBA, error) {
	g := guard.New(ctx, maxNodes(opt.MaxNodes))
	if err := g.Err(); err != nil {
		return nil, err
	}
	t, doc, err := detect(src)
	if err != nil {
		return nil, err
	}
	if err := unsupportedConfig(t, doc); err != nil {
		return nil, err
	}
	d, err := parse(g, t, doc.Lines)
	if err != nil {
		return nil, err
	}
	scale := opt.Scale
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		scale = 1
	}
	scale = math.Min(math.Max(scale, minScale), maxScale)
	th := theme.NewWith(opt.Foreground, opt.Background, doc.Theme, doc.Vars)
	title := diagramTitle(d, doc)

	// resizable reports whether text can be made smaller to fit MaxWidth.
	resizable := opt.FontFunc != nil || opt.FaceFunc != nil || opt.Face == nil
	newFace := func(s float64) (textmeasure.Faces, error) {
		px := float64(textmeasure.BaseFontSizePx * s)
		switch {
		case opt.FontFunc != nil:
			var fs textmeasure.Faces
			for st := range fs {
				f, err := opt.FontFunc(px, FontStyle(st))
				if err == nil && f == nil {
					err = fmt.Errorf("mermaid: FontFunc returned no face for %s", FontStyle(st))
				}
				if err != nil {
					return textmeasure.Faces{}, err
				}
				fs[st] = f
			}
			return fs, nil
		case opt.FaceFunc != nil:
			f, err := opt.FaceFunc(px)
			if err == nil && f == nil {
				err = fmt.Errorf("mermaid: FaceFunc returned no face")
			}
			return textmeasure.Uniform(f), err
		case opt.Face != nil:
			return textmeasure.Uniform(opt.Face), nil
		}
		return textmeasure.BuiltinFaces(px)
	}
	face, err := newFace(scale)
	if err != nil {
		return nil, err
	}
	work := 0 // search steps of all layout attempts (diagram.Env.Work)
	layout := func(face textmeasure.Faces, s float64, a fitAttempt) (*scene.Scene, error) {
		m := textmeasure.New(face, s)
		m.SetStop(func() bool { return g.Err() != nil })
		env := &diagram.Env{G: g, M: m, T: th, Spacing: a.spacing, Wrap: a.wrap, Work: &work}
		if opt.MaxWidth > 0 {
			env.MaxWidth = float64(float64(opt.MaxWidth) / s)
		}
		sc, err := d.Layout(env)
		if err == nil && title != "" {
			sc = diagram.AddTitle(sc, env, title)
		}
		// After a cancellation the measured widths are cut short (SetStop),
		// so report the cancellation rather than whatever came of them.
		if cerr := g.Err(); cerr != nil {
			return nil, cerr
		}
		return sc, err
	}
	fits := func(sc *scene.Scene, s float64) bool {
		return opt.MaxWidth <= 0 || int(math.Ceil(float64(sc.Width*s))) <= opt.MaxWidth
	}
	// small reports whether the drawing is within MaxPixels (as checked by
	// raster.Render). The compact attempts are also tried for a drawing
	// that fits MaxWidth but not MaxPixels: a wide MaxWidth (or none) let
	// such a drawing fail that a narrower MaxWidth compacted and drew.
	limit := float64(maxPixels(opt.MaxPixels))
	small := func(sc *scene.Scene, s float64) bool {
		return float64(math.Ceil(float64(sc.Width*s))*math.Ceil(float64(sc.Height*s))) <= limit
	}

	// A retry that runs out of the shared search budget (diagram.Spend)
	// keeps the layout before it, which then goes to the bitmap reduction:
	// without MaxWidth the same diagram renders from the first attempt, and
	// failing it only because MaxWidth asked for retries was inconsistent.
	exhausted := false
	var sc *scene.Scene
	var last fitAttempt
	for _, a := range fitAttempts {
		if err := g.Err(); err != nil {
			return nil, err
		}
		next, err := layout(face, scale, a)
		if err != nil {
			if sc != nil && errors.Is(err, diagram.ErrBudget) {
				exhausted = true
				break
			}
			return nil, err
		}
		if sc != nil && fits(sc, scale) && !fits(next, scale) {
			// Compacting for MaxPixels must not lose the width.
			break
		}
		sc, last = next, a
		if a == fitAttempts[0] {
			// Check the glyphs before fitting MaxWidth, so that a label the
			// font cannot draw is ErrUnsupported at every width (it was
			// ErrTooLarge, from the width of empty boxes, at small ones).
			if err := checkGlyphs(sc, face); err != nil {
				return nil, err
			}
		}
		if (fits(sc, scale) || opt.MaxWidth <= 0) && small(sc, scale) {
			break
		}
	}
	if !(fits(sc, scale) && small(sc, scale)) && resizable && !exhausted {
		// Shrink the font and lay out again, but not below minTextPx (or
		// the requested size, if that is already smaller). Also for
		// MaxPixels: without it a drawing failed with no MaxWidth (or a
		// wide one) that a narrower MaxWidth shrank and drew.
		sMin := math.Min(scale, minTextPx/textmeasure.BaseFontSizePx)
		s := scale
		for i := 0; i < maxShrinkSteps && !(fits(sc, s) && small(sc, s)) && s > sMin; i++ {
			if err := g.Err(); err != nil {
				return nil, err
			}
			f := 1.0
			if !fits(sc, s) {
				f = float64(float64(opt.MaxWidth) / (float64(sc.Width * s)))
			}
			if !small(sc, s) {
				f = math.Min(f, math.Sqrt(float64(limit/(float64(float64(sc.Width*s)*float64(sc.Height*s))))))
			}
			ns := math.Max(float64(s*f*0.98), sMin)
			nface, err := newFace(ns)
			if err != nil {
				return nil, err
			}
			next, err := layout(nface, ns, last)
			if err != nil {
				if errors.Is(err, diagram.ErrBudget) {
					break
				}
				return nil, err
			}
			s, face, sc = ns, nface, next
		}
		scale = s
	}
	if !fits(sc, scale) {
		// Only a bitmap reduction is left. Refuse one that would make the
		// text unreadable: an error lets the caller fall back (to text
		// output or the source), a sliver of pixels does not.
		need := math.Ceil(float64(sc.Width * scale))
		factor := float64(float64(opt.MaxWidth) / need)
		em := float64(textmeasure.BaseFontSizePx * scale)
		if !resizable {
			em = faceEm(face[0])
		}
		if factor < slackDownscale && float64(em*factor) < minTextPx {
			return nil, fmt.Errorf("%w: diagram needs about %d px at the smallest readable text size, MaxWidth is %d",
				guard.ErrTooLarge, int(math.Ceil(float64(need*minTextPx/em))), opt.MaxWidth)
		}
	}
	if err := g.Err(); err != nil {
		return nil, err
	}
	if err := checkGlyphs(sc, face); err != nil {
		return nil, err
	}
	img, err := raster.Render(g, sc, face, scale, th.Background, maxPixels(opt.MaxPixels))
	if err != nil {
		return nil, err
	}
	if opt.MaxWidth > 0 && img.Bounds().Dx() > opt.MaxWidth {
		// Scaling an image of MaxPixels takes a few hundred milliseconds
		// and cannot be interrupted; do not start it after the deadline.
		if err := g.Err(); err != nil {
			return nil, err
		}
		img = downscale(img, opt.MaxWidth)
		if err := g.Err(); err != nil {
			return nil, err
		}
	}
	return img, nil
}

// faceEm estimates the em size in pixels of a fixed Face.
func faceEm(f font.Face) float64 {
	m := f.Metrics()
	if m.CapHeight > 0 {
		return float64(float64(m.CapHeight) / 64 / capPerEm)
	}
	if m.Height > 0 {
		return float64(float64(m.Height) / 64 / linePerEm)
	}
	// A face without metrics: assume the default size rather than divide
	// by zero (the error said "needs about -9223372036854775808 px").
	return textmeasure.BaseFontSizePx
}

func maxNodes(n int) int {
	if n <= 0 {
		return DefaultMaxNodes
	}
	return n
}

func maxPixels(n int) int {
	if n <= 0 {
		return DefaultMaxPixels
	}
	return n
}

// checkGlyphs returns ErrUnsupported for the first character of a label
// that face cannot draw. Drawing it anyway would give an empty box (tofu),
// an image that looks valid but cannot be read; an error lets the caller
// fall back to RenderText (the terminal has its own fonts) or the source.
func checkGlyphs(sc *scene.Scene, faces textmeasure.Faces) error {
	for _, it := range sc.Items {
		t, ok := it.(*scene.Text)
		if !ok {
			continue
		}
		for _, l := range t.Lines {
			for _, run := range richtext.Runs(l) {
				face := faces.For(run.Style)
				for _, r := range run.Text {
					if r == ' ' {
						continue
					}
					if _, ok := face.GlyphAdvance(r); !ok {
						return fmt.Errorf("%w: the font has no glyph for %q (U+%04X); set Options.FaceFunc to a font that has it",
							ErrUnsupported, r, r)
					}
				}
			}
		}
	}
	return nil
}

// downscale is the last resort when text cannot be made small enough (a
// fixed Face, or the minimum text size was reached).
func downscale(src *image.RGBA, w int) *image.RGBA {
	b := src.Bounds()
	h := max(1, int(math.Round(float64(float64(b.Dy())*float64(w)/float64(b.Dx())))))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

// RenderText draws src with box-drawing characters, one string per line,
// each at most opt.MaxCols columns wide (East Asian wide characters count
// as two). Diagram types without a text renderer return ErrUnsupported.
func RenderText(ctx context.Context, src string, opt TextOptions) (lines []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			lines, err = nil, fmt.Errorf("mermaid: internal error: %v", r)
		}
	}()
	lines, err = renderText(ctx, src, opt)
	if err != nil {
		return nil, convertErr(err)
	}
	return lines, nil
}

// renderText is RenderText without the recover, so that FuzzRender sees
// panics in the text renderers. Its errors are not yet converted.
func renderText(ctx context.Context, src string, opt TextOptions) ([]string, error) {
	g := guard.New(ctx, maxNodes(opt.MaxNodes))
	if err := g.Err(); err != nil {
		return nil, err
	}
	t, doc, err := detect(src)
	if err != nil {
		return nil, err
	}
	// Decide support before parsing so that unsupported types always give
	// ErrUnsupported, never a syntax or size error.
	if !hasText(t) {
		return nil, fmt.Errorf("%w: no text renderer for %s", ErrUnsupported, t)
	}
	if err := unsupportedConfig(t, doc); err != nil {
		return nil, err
	}
	d, err := parse(g, t, doc.Lines)
	if err != nil {
		return nil, err
	}
	td, ok := d.(textDiagram)
	if !ok {
		return nil, fmt.Errorf("%w: no text renderer for %s", ErrUnsupported, t)
	}
	maxCols := opt.MaxCols
	if maxCols <= 0 {
		maxCols = math.MaxInt32
	}
	title := diagramTitle(d, doc)
	out, err := td.Text(g, maxCols)
	if err != nil {
		return nil, err
	}
	if title != "" {
		out = append(append(textgrid.Wrap(title, maxCols), ""), out...)
	}
	// Wrapping a long title is outside the guard; do not return a result
	// after the deadline, as Render does not either.
	if err := g.Err(); err != nil {
		return nil, err
	}
	return checkText(out, maxCols)
}

// hasText reports whether diagram type t has a text renderer.
func hasText(t DiagramType) bool {
	k := kindOf(t)
	return k != nil && k.text
}

// checkText enforces the RenderText contract for every diagram type: no
// embedded line breaks or control characters and no line wider than
// maxCols.
func checkText(lines []string, maxCols int) ([]string, error) {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		for _, part := range strings.Split(l, "\n") {
			part = strings.TrimRight(textgrid.Sanitize(part), " ")
			if w := textgrid.Width(part); w > maxCols {
				return nil, guard.Limit("columns", w, maxCols)
			}
			out = append(out, part)
		}
	}
	return out, nil
}

// unsupportedConfig returns ErrUnsupported for configuration that changes
// what the source means (not only how it looks), which the renderers do
// not follow. Other settings are ignored, as the README says.
func unsupportedConfig(t DiagramType, doc *source.Doc) error {
	if t == GitGraph && doc.MainBranch != "" {
		return fmt.Errorf("%w: gitGraph mainBranchName %q", ErrUnsupported, doc.MainBranch)
	}
	return nil
}
