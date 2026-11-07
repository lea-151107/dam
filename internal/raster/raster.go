// Package raster draws a scene.Scene into an *image.RGBA.
package raster

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/richtext"
	"github.com/lea/mermaid/internal/scene"
)

// flattenTol is the curve flattening tolerance in device pixels.
const flattenTol = 0.2

// maxTilePx and tileRowsPx are the largest width and height rasterized at
// once.
//
// Above 512 (floatingPointMathThreshold in x/image/vector) the rasterizer
// switches to float32 accumulation, which runs as SSE4.1 code on amd64 and
// as Go code elsewhere, and the two round differently at fractional
// coordinates: the same drawing differed between amd64 and arm64 (or
// -tags noasm) at Scale 1.5. Within the bound the accumulation is integer
// and identical everywhere.
//
// The integer rasterizer walks an edge one row at a time and truncates the
// x step of every row, so an edge drifts by up to 1/512 px per row (about
// 0.5 px over 512 rows, always to the same side). Short tiles bound the
// walk: edges are clipped to the rows of each tile, so the drift is at most
// tileRowsPx/512 px.
const (
	maxTilePx  = 512
	tileRowsPx = 32
)

// maxSidePx bounds the image width and height. The integer rasterizer
// works in 22.9 fixed point (±4M px). Edges are clipped to the rows of a
// tile, but not to its columns, so x relative to a tile reaches the width
// of the drawing (diagram.Finish sizes the canvas to contain every shape);
// a caller's large MaxPixels must not push a long, thin drawing past that.
const maxSidePx = 1 << 21

// Render draws sc scaled by scale. Text is drawn with face, which must be
// sized for device pixels. bg with A == 0 leaves the canvas transparent.
// An image of more than maxPixels pixels is refused before it is
// allocated.
//
// faces holds a face per style (Regular, Bold, Italic, BoldItalic), as in
// textmeasure.Faces.
func Render(g *guard.Guard, sc *scene.Scene, faces [4]font.Face, scale float64, bg color.NRGBA, maxPixels int) (*image.RGBA, error) {
	// Check the size while it is still a float: a huge drawing would
	// overflow the conversion to int and slip under the limit (or come out
	// as a 1-pixel image). The negated test also rejects NaN.
	wf, hf := math.Ceil(float64(sc.Width*scale)), math.Ceil(float64(sc.Height*scale))
	if !(float64(wf*hf) <= float64(maxPixels)) {
		return nil, fmt.Errorf("%w: drawing of %.0f×%.0f pixels exceeds limit %d", guard.ErrTooLarge, wf, hf, maxPixels)
	}
	if wf > maxSidePx || hf > maxSidePx {
		return nil, fmt.Errorf("%w: drawing of %.0f×%.0f pixels exceeds side limit %d", guard.ErrTooLarge, wf, hf, maxSidePx)
	}
	w, h := max(int(wf), 1), max(int(hf), 1)
	if err := guard.Limit("pixels", w*h, maxPixels); err != nil {
		return nil, err
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if bg.A > 0 {
		draw.Draw(dst, dst.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	}
	r := &renderer{dst: dst, faces: faces, scale: scale, g: g}
	for _, it := range sc.Items {
		switch v := it.(type) {
		case *scene.Shape:
			// A shape scans its whole bounding box (a stroked rectangle
			// too), which takes tens of milliseconds for one the size of a
			// large image. Sixteen of them between checks of the context
			// overran a deadline several times over (nested sequence
			// rects), so check before every shape; ctx.Err costs far less.
			if err := g.Err(); err != nil {
				return nil, err
			}
			r.shape(v)
		case *scene.Text:
			// A large Face makes each label slow to draw; check every one,
			// as for shapes.
			if err := g.Err(); err != nil {
				return nil, err
			}
			r.text(v)
		}
	}
	// fill stops early once the context is done; report it rather than a
	// half-drawn image.
	if err := g.Err(); err != nil {
		return nil, err
	}
	return dst, nil
}

type renderer struct {
	dst   *image.RGBA
	faces [4]font.Face
	scale float64
	ras   vector.Rasterizer
	g     *guard.Guard
}

// snap rounds to 1/64 device pixel so that results do not depend on tiny
// floating point differences between architectures.
func snap(v float64) float64 { return float64(math.Round(float64(v*64)) / 64) }

func (r *renderer) dev(p scene.Point) scene.Point {
	return scene.Point{X: snap(float64(p.X * r.scale)), Y: snap(float64(p.Y * r.scale))}
}

func (r *renderer) shape(s *scene.Shape) {
	subs := s.Path.Flatten(float64(flattenTol / r.scale))
	for i := range subs {
		for j, p := range subs[i].Pts {
			subs[i].Pts[j] = r.dev(p)
		}
	}
	if s.Fill.A > 0 {
		var polys [][]scene.Point
		for _, sp := range subs {
			if len(sp.Pts) >= 3 {
				polys = append(polys, sp.Pts)
			}
		}
		r.fill(polys, s.Fill)
	}
	if s.Stroke.Width > 0 && s.Stroke.Color.A > 0 {
		width := math.Max(float64(s.Stroke.Width*r.scale), 0.6)
		var dash []float64
		for _, d := range s.Stroke.Dash {
			dash = append(dash, float64(d*r.scale))
		}
		var polys [][]scene.Point
		for _, sp := range subs {
			pts := sp.Pts
			if sp.Closed && len(pts) > 1 {
				pts = append(append([]scene.Point(nil), pts...), pts[0])
			}
			for _, seg := range applyDash(pts, dash) {
				polys = append(polys, strokePolys(seg, width)...)
			}
		}
		r.fill(polys, s.Stroke.Color)
	}
}

// fill rasterizes polygons within their bounding box. All polygons must have
// a consistent winding so that overlaps do not cancel.
func (r *renderer) fill(polys [][]scene.Point, c color.NRGBA) {
	if len(polys) == 0 {
		return
	}
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	for _, p := range polys {
		for _, q := range p {
			x0, y0 = math.Min(x0, q.X), math.Min(y0, q.Y)
			x1, y1 = math.Max(x1, q.X), math.Max(y1, q.Y)
		}
	}
	box := image.Rect(int(math.Floor(x0))-1, int(math.Floor(y0))-1, int(math.Ceil(x1))+1, int(math.Ceil(y1))+1)
	box = box.Intersect(r.dst.Bounds())
	if box.Empty() {
		return
	}
	// The rows each polygon spans. A polygon outside the rows of a tile
	// adds nothing to it (edge clips every one of its edges away), so it
	// is skipped without looking at its edges: a long dashed line is
	// thousands of small polygons, and walking all of them for every tile
	// of a tall drawing took tens of seconds.
	spans := make([][2]float64, len(polys))
	cols := make([][2]float64, len(polys)) // the columns each polygon spans
	for k, p := range polys {
		lo, hi := math.Inf(1), math.Inf(-1)
		left, right := math.Inf(1), math.Inf(-1)
		for _, q := range p {
			lo, hi = math.Min(lo, q.Y), math.Max(hi, q.Y)
			left, right = math.Min(left, q.X), math.Max(right, q.X)
		}
		spans[k] = [2]float64{lo, hi}
		cols[k] = [2]float64{left, right}
	}
	// Polygons sorted by their top row, to find those that reach a tile
	// row without scanning all of them.
	byTop := make([]int, len(polys))
	for k := range byTop {
		byTop[k] = k
	}
	sort.SliceStable(byTop, func(a, b int) bool { return spans[byTop[a]][0] < spans[byTop[b]][0] })
	var active []int // polygons that may reach the current tile row, in input order
	next := 0
	src := image.NewUniform(c)
	// The tiles do not overlap, so each pixel is still blended once.
	for ty := box.Min.Y; ty < box.Max.Y; ty += tileRowsPx {
		top, bot := float64(ty), float64(min(ty+tileRowsPx, box.Max.Y))
		// Drop polygons that end above this row and add those that start
		// before its bottom, keeping the input order: the rasterizer sums
		// coverage in float32, so the order of the edges matters to the
		// last bit.
		kept := active[:0]
		for _, k := range active {
			if spans[k][1] > top {
				kept = append(kept, k)
			}
		}
		active = kept
		for next < len(byTop) && spans[byTop[next]][0] < bot {
			k := byTop[next]
			next++
			if spans[k][1] > top {
				i := sort.SearchInts(active, k)
				active = append(active, 0)
				copy(active[i+1:], active[i:])
				active[i] = k
			}
		}
		// One shape as large as the image takes a long time with high
		// MaxPixels; check the context for every band of rows.
		if r.g != nil && r.g.Err() != nil {
			return
		}
		if len(active) == 0 {
			// Nothing reaches these rows: drawing zero coverage with Over
			// would leave every pixel as it is.
			continue
		}
		// Only the columns the band's polygons reach: a curved edge across
		// the drawing is a narrow strip in each band, and drawing the whole
		// width of its bounding box for each of hundreds of edges took
		// seconds. Outside those columns the coverage is zero.
		left, right := math.Inf(1), math.Inf(-1)
		for _, k := range active {
			left, right = math.Min(left, cols[k][0]), math.Max(right, cols[k][1])
		}
		x0 := max(box.Min.X, int(math.Floor(left))-1)
		x1 := min(box.Max.X, int(math.Ceil(right))+1)
		for tx := x0; tx < x1; tx += maxTilePx {
			tile := image.Rect(tx, ty, min(tx+maxTilePx, x1), min(ty+tileRowsPx, box.Max.Y))
			// Polygons wholly left or right of the tile add nothing to it
			// (left of it their edges cancel at its left side).
			n := 0
			r.ras.Reset(tile.Dx(), tile.Dy())
			r.ras.DrawOp = draw.Over
			for _, k := range active {
				if cols[k][1] <= float64(tile.Min.X) || cols[k][0] >= float64(tile.Max.X) {
					continue
				}
				n++
				p := polys[k]
				for i := range p {
					r.edge(p[i], p[(i+1)%len(p)], tile)
				}
			}
			if n > 0 {
				r.ras.Draw(r.dst, tile, src, image.Point{})
			}
		}
	}
}

// edge adds the edge from a to b, clipped to the rows of tile, to the
// rasterizer, in coordinates relative to tile.
//
// The coverage of a filled polygon is the sum of the coverage of its
// directed edges, so each edge can be added on its own (MoveTo does not
// close a path). Clipping to the rows matters: the integer rasterizer
// walks an edge one row at a time from its top, truncating the x step of
// every row, also for rows above the tile. An edge starting 5000 rows
// above a tile reached it several pixels off, and arrow shafts missed
// their heads in tall drawings. Within the tile the walk is at most
// tileRowsPx rows. Parts left or right
// of the tile need no clipping: the rasterizer adds them at the tile edge,
// which is where their coverage belongs.
func (r *renderer) edge(a, b scene.Point, tile image.Rectangle) {
	top, bot := float64(tile.Min.Y), float64(tile.Max.Y)
	if a.Y <= b.Y {
		if lo, hi, ok := clipRows(a, b, top, bot); ok {
			r.line(lo, hi, tile)
		}
		return
	}
	// Clip the edge pointing down, then add it in its own direction, which
	// sets the sign of its coverage.
	if lo, hi, ok := clipRows(b, a, top, bot); ok {
		r.line(hi, lo, tile)
	}
}

// clipRows returns the part of the edge from a to b (a.Y <= b.Y) between
// the rows top and bot, or ok == false if there is none or it is
// horizontal (horizontal edges add no coverage).
func clipRows(a, b scene.Point, top, bot float64) (lo, hi scene.Point, ok bool) {
	if !(b.Y > a.Y) || b.Y <= top || a.Y >= bot {
		return lo, hi, false
	}
	at := func(y float64) scene.Point {
		t := float64((y - a.Y) / (b.Y - a.Y))
		// float64(...) keeps the compiler from fusing the multiply and
		// add, which would round differently on CPUs with FMA.
		return scene.Point{X: a.X + float64(t*(b.X-a.X)), Y: y}
	}
	lo, hi = a, b
	if a.Y < top {
		lo = at(top)
	}
	if b.Y > bot {
		hi = at(bot)
	}
	return lo, hi, true
}

// line adds the segment from p to q to the rasterizer, relative to tile.
func (r *renderer) line(p, q scene.Point, tile image.Rectangle) {
	ox, oy := float64(tile.Min.X), float64(tile.Min.Y)
	r.ras.MoveTo(float32(p.X-ox), float32(p.Y-oy))
	r.ras.LineTo(float32(q.X-ox), float32(q.Y-oy))
}

func (r *renderer) face(st richtext.Style) font.Face {
	return r.faces[st&(richtext.Bold|richtext.Italic)]
}

func (r *renderer) text(t *scene.Text) {
	m := r.faces[0].Metrics()
	lineH := float64(float64(m.Height) / 64)
	ascent := float64(float64(m.Ascent) / 64)
	descent := float64(float64(m.Descent) / 64)
	total := float64(lineH * float64(len(t.Lines)))
	cy := float64(t.Y * r.scale)
	top := cy - float64(total/2)
	d := font.Drawer{Dst: r.dst, Src: image.NewUniform(t.Color)}
	for i, line := range t.Lines {
		runs := richtext.Runs(line)
		var adv fixed.Int26_6
		for _, run := range runs {
			adv += font.MeasureString(r.face(run.Style), run.Text)
		}
		w := float64(float64(adv) / 64)
		x := float64(t.X * r.scale)
		switch t.Align {
		case scene.Center:
			x -= float64(w / 2)
		case scene.Right:
			x -= w
		}
		// Center the glyph box (ascent+descent) within the line box.
		base := top + float64(float64(i)*lineH) + float64((lineH-(ascent+descent))/2) + ascent
		d.Dot = fixed.Point26_6{X: fixed.Int26_6(math.Round(float64(x * 64))), Y: fixed.Int26_6(math.Round(float64(base * 64)))}
		for _, run := range runs {
			d.Face = r.face(run.Style)
			x0 := d.Dot.X
			d.DrawString(run.Text)
			if run.Style&richtext.Underline != 0 {
				r.underline(x0, d.Dot.X, base+float64(descent*0.4), lineH, t.Color)
			}
		}
	}
}

// underline draws a line from x0 to x1 (26.6) just below baseline y.
func (r *renderer) underline(x0, x1 fixed.Int26_6, y, lineH float64, c color.NRGBA) {
	th := math.Max(1, math.Round(float64(lineH/16)))
	y = math.Round(y)
	r.fill([][]scene.Point{{
		{X: float64(float64(x0) / 64), Y: y}, {X: float64(float64(x1) / 64), Y: y},
		{X: float64(float64(x1) / 64), Y: y + th}, {X: float64(float64(x0) / 64), Y: y + th},
	}}, c)
}
