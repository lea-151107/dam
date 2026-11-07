// Package outline provides a font.Face for an sfnt font whose glyph masks
// are the same on every architecture.
//
// opentype.Face hands quadratic and cubic segments to the float32 curve
// flattening in golang.org/x/image/vector, which the compiler may fuse
// into multiply-adds (arm64, GOAMD64=v3), so its masks differ slightly
// between machines. This Face takes the outline in 26.6 fixed point from
// sfnt, flattens curves with integer arithmetic (internal/curve) and gives
// the rasterizer straight lines only, whose coverage does not depend on
// fusion.
package outline

import (
	"image"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/lea/mermaid/internal/curve"
)

// Face draws glyphs of f at a size of ppem pixels per em. Metrics,
// advances and kerning come from opentype.Face. Like every font.Face it is
// not safe for concurrent use.
type Face struct {
	font.Face // metrics, advances, bounds and kerning
	f         *sfnt.Font
	ppem      fixed.Int26_6
	buf       sfnt.Buffer
	ras       vector.Rasterizer
}

// New returns a Face for f at sizePx pixels per em, without hinting.
func New(f *sfnt.Font, sizePx float64) (*Face, error) {
	of, err := opentype.NewFace(f, &opentype.FaceOptions{Size: sizePx, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, err
	}
	// The same expression as opentype.NewFace (DPI 72), so that glyphs
	// are drawn at exactly the scale of the advances it reports.
	ppem := fixed.Int26_6(0.5 + (float64(sizePx * 72 * 64 / 72)))
	return &Face{Face: of, f: f, ppem: ppem}, nil
}

// Glyph implements font.Face.
func (fc *Face) Glyph(dot fixed.Point26_6, r rune) (dr image.Rectangle, mask image.Image, maskp image.Point, advance fixed.Int26_6, ok bool) {
	advance, ok = fc.GlyphAdvance(r)
	if !ok {
		return image.Rectangle{}, nil, image.Point{}, 0, false
	}
	idx, err := fc.f.GlyphIndex(&fc.buf, r)
	if err != nil || idx == 0 {
		return image.Rectangle{}, nil, image.Point{}, 0, false
	}
	segs, err := fc.f.LoadGlyph(&fc.buf, idx, fc.ppem, nil)
	if err != nil {
		return image.Rectangle{}, nil, image.Point{}, 0, false
	}
	// Absolute device coordinates in 26.6; sfnt's y axis points down.
	abs := func(p fixed.Point26_6) fixed.Point26_6 { return p.Add(dot) }
	var lo, hi fixed.Point26_6
	first := true
	for _, s := range segs {
		for _, p := range s.Args[:nArgs(s.Op)] {
			p = abs(p)
			if first {
				lo, hi, first = p, p, false
				continue
			}
			lo.X, lo.Y = min(lo.X, p.X), min(lo.Y, p.Y)
			hi.X, hi.Y = max(hi.X, p.X), max(hi.Y, p.Y)
		}
	}
	if first {
		// No outline (a space): nothing to draw.
		return image.Rectangle{}, image.NewAlpha(image.Rectangle{}), image.Point{}, advance, true
	}
	dr = image.Rect(lo.X.Floor(), lo.Y.Floor(), hi.X.Ceil(), hi.Y.Ceil())
	if dr.Empty() {
		return image.Rectangle{}, image.NewAlpha(image.Rectangle{}), image.Point{}, advance, true
	}
	fc.ras.Reset(dr.Dx(), dr.Dy())
	origin := fixed.Point26_6{X: fixed.I(dr.Min.X), Y: fixed.I(dr.Min.Y)}
	// Multiples of 1/64 within a glyph box are exact in float32.
	lineTo := func(p fixed.Point26_6) {
		p = p.Sub(origin)
		fc.ras.LineTo(float32(p.X)/64, float32(p.Y)/64)
	}
	var pen fixed.Point26_6
	started := false
	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			if started {
				fc.ras.ClosePath()
			}
			pen = abs(s.Args[0])
			p := pen.Sub(origin)
			fc.ras.MoveTo(float32(p.X)/64, float32(p.Y)/64)
			started = true
		case sfnt.SegmentOpLineTo:
			pen = abs(s.Args[0])
			lineTo(pen)
		case sfnt.SegmentOpQuadTo:
			p1, p2 := abs(s.Args[0]), abs(s.Args[1])
			curve.Quad(pen, p1, p2, lineTo)
			pen = p2
		case sfnt.SegmentOpCubeTo:
			p1, p2, p3 := abs(s.Args[0]), abs(s.Args[1]), abs(s.Args[2])
			curve.Cube(pen, p1, p2, p3, lineTo)
			pen = p3
		}
	}
	if started {
		fc.ras.ClosePath()
	}
	a := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
	fc.ras.Draw(a, a.Bounds(), image.Opaque, image.Point{})
	return dr, a, image.Point{}, advance, true
}

func nArgs(op sfnt.SegmentOp) int {
	switch op {
	case sfnt.SegmentOpQuadTo:
		return 2
	case sfnt.SegmentOpCubeTo:
		return 3
	}
	return 1
}
