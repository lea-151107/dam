package sequence

import (
	"cmp"
	"image/color"
	"math"
	"slices"
	"strconv"

	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/theme"
)

// Visual constants in layout units.
const (
	actorMinW    = 90.0
	actorPadX    = 14.0
	actorPadY    = 10.0
	actorGap     = 50.0
	stickH       = 36.0
	msgGap       = 22.0
	labelGap     = 4.0
	selfLoopW    = 34.0
	selfLoopH    = 22.0
	notePad      = 8.0
	noteGap      = 10.0
	activationW  = 10.0
	blockPad     = 10.0
	blockNest    = 8.0
	blockTabPadX = 8.0
	lineW        = 1.4
	markerSize   = 9.0
	numberR      = 8.0
	boxPad       = 8.0 // space around the participants of a box
	crossR       = 7.0 // half size of the cross that ends a destroyed lifeline
)

var dottedDash = []float64{4, 3}

type actorGeo struct {
	lines []string
	w, h  float64 // box size (for stick figures: label block)
}

type block struct {
	kind     string
	text     []string
	y0, y1   float64
	lo, hi   int
	depth    int
	dividers []divider
}

type divider struct {
	y    float64
	text []string
}

type activation struct {
	who    int
	y0, y1 float64
	depth  int
}

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	sp := env.Spacing
	n := len(d.Participants)

	actors := make([]actorGeo, n)
	headH := 0.0
	for i, p := range d.Participants {
		lines := env.Label(p.Label)
		if p.Type != "" {
			lines = append([]string{"«" + p.Type + "»"}, lines...)
		}
		tw, th := m.Size(lines)
		a := actorGeo{lines: lines, w: max(tw+float64(2*actorPadX), float64(actorMinW*math.Min(1, sp+0.2))), h: th + float64(2*actorPadY)}
		if p.Actor {
			a.h = stickH + th + 4
		}
		actors[i] = a
		headH = max(headH, a.h)
	}

	// Horizontal positions: minimum gaps, then widen gaps for wide labels.
	gaps := make([]float64, max(n-1, 0))
	for i := range gaps {
		gaps[i] = float64((actors[i].w+actors[i+1].w)/2) + float64(actorGap*sp)
	}
	type span struct {
		a, b int
		need float64
	}
	var spans []span
	need := func(a, b int, dist float64) {
		if a > b {
			a, b = b, a
		}
		if a == b || a < 0 || b >= n {
			return
		}
		spans = append(spans, span{a, b, dist})
	}
	evLines := make([][]string, len(d.Events))
	for i, ev := range d.Events {
		switch ev.Kind {
		case Message:
			evLines[i] = env.Label(ev.Text)
			tw, _ := m.Size(evLines[i])
			if ev.From == ev.To {
				need(ev.From, ev.From+1, float64(actors[ev.From].w/2)+selfLoopW+tw+float64(actorGap*sp/2))
			} else {
				need(ev.From, ev.To, tw+2*numberR+24)
			}
		case Note:
			evLines[i] = env.Label(ev.Text)
			tw, _ := m.Size(evLines[i])
			nw := tw + 2*notePad
			switch {
			case ev.Place == LeftOf:
				need(ev.From-1, ev.From, nw+noteGap+float64(actors[max(ev.From-1, 0)].w/2)+float64(actorGap*sp/2))
			case ev.Place == RightOf:
				need(ev.From, ev.From+1, nw+noteGap+float64(actors[min(ev.From+1, n-1)].w/2)+float64(actorGap*sp/2))
			case ev.From == ev.To:
				need(ev.From-1, ev.From, float64(nw/2)+float64(actors[max(ev.From-1, 0)].w/2)+float64(actorGap*sp/2))
				need(ev.From, ev.From+1, float64(nw/2)+float64(actors[min(ev.From+1, n-1)].w/2)+float64(actorGap*sp/2))
			}
		case BlockStart, BlockElse:
			evLines[i] = env.Label(ev.Text)
		}
	}
	slices.SortStableFunc(spans, func(x, y span) int { return cmp.Compare(x.b-x.a, y.b-y.a) })
	for _, s := range spans {
		if err := env.G.Tick(); err != nil {
			return nil, err
		}
		have := 0.0
		for k := s.a; k < s.b; k++ {
			have += gaps[k]
		}
		if have < s.need {
			extra := float64((s.need - have) / float64(s.b-s.a))
			for k := s.a; k < s.b; k++ {
				gaps[k] += extra
			}
		}
	}
	cx := make([]float64, n)
	cx[0] = float64(actors[0].w / 2)
	for i := 1; i < n; i++ {
		cx[i] = cx[i-1] + gaps[i-1]
	}

	// Vertical pass.
	sc := &scene.Scene{}
	var fg []func() // drawn after lifelines, in order
	lh := m.LineHeight()
	y := headH + msgGap
	var stack []*block
	var blocks []*block
	active := make([][]float64, n) // stack of start y per participant
	var acts []activation
	// Created participants start where they are created, destroyed ones
	// end where they are destroyed.
	born := make([]float64, n)      // lifeline start
	createdAt := make([]float64, n) // top of the head box; -1 = at the top
	dead := make([]float64, n)      // lifeline end; -1 = at the bottom
	pendingCreate := make([]bool, n)
	pendingDestroy := make([]bool, n)
	for i := range born {
		born[i], createdAt[i], dead[i] = headH, -1, -1
	}
	// meet handles creation and destruction at a message at height yy
	// between from and to; it returns how much to shorten the arrow at
	// its end (to reach a newly drawn head box) and the extra height used.
	meet := func(from, to int, yy float64) (shorten, extra float64) {
		for _, p := range []int{from, to} {
			if pendingCreate[p] {
				pendingCreate[p] = false
				a := actors[p]
				createdAt[p] = yy - float64(a.h/2)
				born[p] = yy + float64(a.h/2)
				extra = max(extra, float64(a.h/2))
				if p == to {
					shorten = float64(a.w / 2)
				}
			}
			if pendingDestroy[p] {
				pendingDestroy[p] = false
				dead[p] = yy
				c := scene.Point{X: cx[p], Y: yy}
				fg = append(fg, func() {
					sc.Line(scene.Polyline([]scene.Point{{X: c.X - crossR, Y: c.Y - crossR}, {X: c.X + crossR, Y: c.Y + crossR}}), t.Line, lineW*1.5, nil)
					sc.Line(scene.Polyline([]scene.Point{{X: c.X - crossR, Y: c.Y + crossR}, {X: c.X + crossR, Y: c.Y - crossR}}), t.Line, lineW*1.5, nil)
				})
			}
		}
		return shorten, extra
	}
	touch := func(ps ...int) {
		for _, b := range stack {
			for _, p := range ps {
				b.lo, b.hi = min(b.lo, p), max(b.hi, p)
			}
		}
	}
	edgeX := func(p int, toward float64) float64 {
		depth := len(active[p])
		if depth == 0 {
			return cx[p]
		}
		off := float64(activationW/2) + float64(float64(depth-1)*activationW/2)
		if toward < cx[p] {
			return cx[p] - float64(activationW/2) + float64(float64(depth-1)*activationW/2)
		}
		return cx[p] + off
	}
	for i, ev := range d.Events {
		if err := env.G.Tick(); err != nil {
			return nil, err
		}
		switch ev.Kind {
		case Message:
			touch(ev.From, ev.To)
			lines := evLines[i]
			_, th := m.Size(lines)
			y += th + labelGap
			col := t.Line
			var dash []float64
			if ev.Dotted {
				dash = dottedDash
			}
			head := markerOf(ev.Head)
			if ev.From == ev.To {
				x0 := edgeX(ev.From, math.Inf(1))
				yy := y
				num, numbered := ev.Number, ev.Numbered
				fg = append(fg, func() {
					if numbered {
						defer func() {
							r := numberRadius(m, num)
							sc.Fill(scene.Ellipse(scene.Point{X: x0, Y: yy}, r, r), t.Line, color.NRGBA{}, 0)
							sc.Label(x0, yy, []string{strconv.Itoa(num)}, scene.Center, t.Base)
						}()
					}
					p := scene.NewPath().MoveTo(x0, yy).
						CubeTo(x0+selfLoopW, yy-4, x0+selfLoopW, yy+selfLoopH+4, x0+markerSize*0.8, yy+selfLoopH)
					sc.Line(p, col, lineW, dash)
					if head != scene.NoMarker {
						sc.DrawMarker(head, scene.Point{X: x0, Y: yy + selfLoopH}, scene.Point{X: x0 + markerSize, Y: yy + selfLoopH}, markerSize, col, t.Base, lineW)
					}
					sc.Label(x0+selfLoopW+4, yy-float64(th/2)-float64(labelGap/2)+float64(selfLoopH/2), lines, scene.Left, t.Text)
				})
				y += selfLoopH
			} else {
				x0 := edgeX(ev.From, cx[ev.To])
				x1 := edgeX(ev.To, cx[ev.From])
				yy := y
				shorten, extra := meet(ev.From, ev.To, yy)
				if x1 > x0 {
					x1 -= shorten
				} else {
					x1 += shorten
				}
				y += extra
				num, numbered := ev.Number, ev.Numbered
				both := ev.BothEnds
				fg = append(fg, func() {
					a, b := scene.Point{X: x0, Y: yy}, scene.Point{X: x1, Y: yy}
					pts := scene.Shorten([]scene.Point{a, b}, float64(scene.MarkerLen(head, markerSize)*0.8))
					if both {
						pts = scene.ShortenStart(pts, float64(scene.MarkerLen(head, markerSize)*0.8))
					}
					sc.Line(scene.Polyline(pts), col, lineW, dash)
					if head != scene.NoMarker {
						sc.DrawMarker(head, b, a, markerSize, col, t.Base, lineW)
						if both {
							sc.DrawMarker(head, a, b, markerSize, col, t.Base, lineW)
						}
					}
					sc.Label(float64((x0+x1)/2), yy-labelGap-float64(th/2), lines, scene.Center, t.Text)
					if numbered {
						r := numberRadius(m, num)
						sc.Fill(scene.Ellipse(a, r, r), t.Line, color.NRGBA{}, 0)
						sc.Label(a.X, a.Y, []string{strconv.Itoa(num)}, scene.Center, t.Base)
					}
				})
			}
			y += msgGap
		case Note:
			touch(ev.From, ev.To)
			lines := evLines[i]
			tw, th := m.Size(lines)
			w, h := tw+float64(2*notePad), th+float64(2*notePad)
			var x0 float64
			switch ev.Place {
			case LeftOf:
				x0 = cx[ev.From] - noteGap - w
			case RightOf:
				x0 = cx[ev.From] + noteGap
			default:
				a, b := min(cx[ev.From], cx[ev.To]), max(cx[ev.From], cx[ev.To])
				if ev.From != ev.To {
					a -= actorGap / 2
					b += actorGap / 2
				}
				w = max(w, b-a)
				x0 = float64((a+b)/2) - float64(w/2)
			}
			r := scene.Rect{X: x0, Y: y, W: w, H: h}
			fg = append(fg, func() {
				sc.Fill(scene.RectPath(r), t.NoteFill, t.NoteStroke, lineW*0.8)
				sc.Label(r.X+float64(r.W/2), r.Y+float64(r.H/2), lines, scene.Center, t.Text)
			})
			y += h + msgGap
		case BlockStart:
			b := &block{kind: ev.Block, text: evLines[i], y0: y, lo: n, hi: -1, depth: len(stack)}
			stack = append(stack, b)
			blocks = append(blocks, b)
			_, th := m.Size(evLines[i])
			y += max(th, lh) + 2*blockPad + labelGap
		case BlockElse:
			b := stack[len(stack)-1]
			b.dividers = append(b.dividers, divider{y: y, text: evLines[i]})
			_, th := m.Size(evLines[i])
			y += max(th, lh) + blockPad + labelGap
		case BlockEnd:
			b := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			b.y1 = y
			if len(stack) > 0 && b.hi >= 0 {
				p := stack[len(stack)-1]
				p.lo, p.hi = min(p.lo, b.lo), max(p.hi, b.hi)
			}
			y += msgGap / 2
		case Create:
			pendingCreate[ev.From] = true
		case Destroy:
			pendingDestroy[ev.From] = true
		case Activate:
			active[ev.From] = append(active[ev.From], y-msgGap)
		case Deactivate:
			if k := len(active[ev.From]); k > 0 {
				acts = append(acts, activation{who: ev.From, y0: active[ev.From][k-1], y1: y - msgGap, depth: k - 1})
				active[ev.From] = active[ev.From][:k-1]
			}
		}
	}
	for p := range active {
		for k, y0 := range active[p] {
			acts = append(acts, activation{who: p, y0: y0, y1: y, depth: k})
		}
	}
	bottom := y

	// Block backgrounds.
	maxDepth := 0
	for _, b := range blocks {
		maxDepth = max(maxDepth, b.depth)
	}
	blockRect := func(b *block) scene.Rect {
		lo, hi := b.lo, b.hi
		if hi < 0 {
			lo, hi = 0, 0
		}
		inset := float64(float64(b.depth) * blockNest)
		x0 := cx[lo] - float64(actors[lo].w/2) - blockPad + inset
		x1 := cx[hi] + float64(actors[hi].w/2) + blockPad - inset
		tw, _ := m.Size(append([]string{b.kind + "   [ ]"}, b.text...))
		for _, dv := range b.dividers {
			w, _ := m.Size(dv.text)
			tw = max(tw, w)
		}
		if need := float64(2*tw) + 4*blockTabPadX; x1-x0 < need {
			c := float64((x0 + x1) / 2)
			x0, x1 = c-float64(need/2), c+float64(need/2)
		}
		return scene.Rect{X: x0, Y: b.y0, W: x1 - x0, H: b.y1 - b.y0}
	}
	for _, b := range blocks {
		if b.kind == "rect" {
			sc.Fill(scene.RectPath(blockRect(b)), theme.Mix(t.Base, t.Accent, 0.08), color.NRGBA{}, 0)
		}
	}
	// Lifelines.
	for i := range d.Participants {
		end := bottom
		if dead[i] >= 0 {
			end = dead[i]
		}
		sc.Line(scene.Polyline([]scene.Point{{X: cx[i], Y: born[i]}, {X: cx[i], Y: end}}), t.Muted, 1, nil)
	}
	// Activations.
	for _, a := range acts {
		x := cx[a.who] - float64(activationW/2) + float64(float64(a.depth)*activationW/2)
		r := scene.Rect{X: x, Y: a.y0, W: activationW, H: math.Max(a.y1-a.y0, 4)}
		sc.Fill(scene.RectPath(r), t.NodeFill, t.NodeStroke, 1)
	}
	for _, f := range fg {
		f()
	}
	// Block frames and labels.
	for _, b := range blocks {
		if b.kind == "rect" {
			continue
		}
		r := blockRect(b)
		sc.Line(scene.RectPath(r), t.Muted, 1, nil)
		tab := []string{b.kind}
		tw, _ := m.Size(tab)
		tabW, tabH := tw+2*blockTabPadX, lh+6
		tp := scene.Polygon([]scene.Point{
			{X: r.X, Y: r.Y}, {X: r.X + tabW, Y: r.Y}, {X: r.X + tabW, Y: r.Y + float64(tabH*0.65)},
			{X: r.X + tabW - 6, Y: r.Y + tabH}, {X: r.X, Y: r.Y + tabH},
		})
		sc.Fill(tp, t.NodeFill, t.Muted, 1)
		sc.Label(r.X+blockTabPadX, r.Y+float64(tabH/2), tab, scene.Left, t.Text)
		if len(b.text) > 0 {
			sc.Label(r.X+tabW+float64((r.W-tabW)/2), r.Y+float64(blockPad/2)+float64(float64(len(b.text))*lh/2)+2, bracket(b.text), scene.Center, t.Text)
		}
		for _, dv := range b.dividers {
			sc.Line(scene.Polyline([]scene.Point{{X: r.X, Y: dv.y}, {X: r.X + r.W, Y: dv.y}}), t.Muted, 1, []float64{4, 3})
			if len(dv.text) > 0 {
				sc.Label(r.X+float64(r.W/2), dv.y+float64(blockPad/2)+float64(float64(len(dv.text))*lh/2), bracket(dv.text), scene.Center, t.Text)
			}
		}
	}
	// Actor boxes at top (or where created) and bottom (unless destroyed).
	for i, p := range d.Participants {
		a := actors[i]
		top := headH - a.h
		if createdAt[i] >= 0 {
			top = createdAt[i]
		}
		drawActor(sc, t, p.Actor, cx[i], top, a, m.LineHeight())
		if dead[i] < 0 {
			drawActor(sc, t, p.Actor, cx[i], bottom, a, m.LineHeight())
		}
	}
	d.drawBoxes(sc, env, cx, actors, bottom+headH)
	return diagram.Finish(sc, env, cx[n-1]+float64(actors[n-1].w/2), bottom+headH), nil
}

// drawBoxes puts the participant boxes behind everything drawn so far:
// a background around their members from above the head boxes to below
// the bottom ones, with the label on top.
func (d *Diagram) drawBoxes(sc *scene.Scene, env *diagram.Env, cx []float64, actors []actorGeo, bottom float64) {
	if len(d.Boxes) == 0 {
		return
	}
	t := env.T
	back := &scene.Scene{}
	for _, b := range d.Boxes {
		if len(b.Members) == 0 {
			continue
		}
		x0, x1 := math.Inf(1), math.Inf(-1)
		for _, p := range b.Members {
			x0 = min(x0, cx[p]-float64(actors[p].w/2)-boxPad)
			x1 = max(x1, cx[p]+float64(actors[p].w/2)+boxPad)
		}
		lines := env.Label(b.Label)
		_, th := env.M.Size(lines)
		top := -boxPad - th
		r := scene.Rect{X: x0, Y: top, W: x1 - x0, H: bottom + boxPad - top}
		fill := theme.Mix(t.Base, t.Accent, 0.07)
		if b.Color != nil {
			fill = *b.Color
		}
		if fill.A > 0 {
			back.Fill(scene.RectPath(r), fill, color.NRGBA{}, 0)
		}
		back.Label(r.X+float64(r.W/2), top+float64(th/2)+float64(boxPad/4), lines, scene.Center, theme.Readable(t.Text, fill))
	}
	sc.Items = append(back.Items, sc.Items...)
	// Messages, actor names and other labels in the theme's text color now
	// lie on a box fill the author chose (box Aqua): keep them readable
	// against the last filled shape under each label's anchor, which is the
	// box, or a head box or note drawn over it.
	type filled struct {
		r scene.Rect
		c color.NRGBA
	}
	var under []filled
	for _, it := range sc.Items {
		switch v := it.(type) {
		case *scene.Shape:
			if v.Fill.A > 0 && v.Path != nil {
				under = append(under, filled{v.Path.Bounds(), v.Fill})
			}
		case *scene.Text:
			if v.Color != t.Text {
				continue
			}
			for k := len(under) - 1; k >= 0; k-- {
				r := under[k].r
				if v.X >= r.X && v.X <= r.X+r.W && v.Y >= r.Y && v.Y <= r.Y+r.H {
					v.Color = theme.Readable(t.Text, under[k].c)
					break
				}
			}
		}
	}
}

func bracket(lines []string) []string {
	out := append([]string(nil), lines...)
	out[0] = "[" + out[0]
	out[len(out)-1] += "]"
	return out
}

func markerOf(h ArrowEnd) scene.Marker {
	switch h {
	case HeadFilled:
		return scene.Arrow
	case HeadCross:
		return scene.Cross
	case HeadOpen:
		return scene.OpenArrow
	}
	return scene.NoMarker
}

func drawActor(sc *scene.Scene, t theme.Theme, stick bool, cx, top float64, a actorGeo, lh float64) {
	if !stick {
		r := scene.Rect{X: cx - float64(a.w/2), Y: top, W: a.w, H: a.h}
		sc.Fill(scene.RoundRect(r, 3), t.NodeFill, t.NodeStroke, lineW)
		sc.Label(cx, top+float64(a.h/2), a.lines, scene.Center, t.Text)
		return
	}
	headR := 6.0
	hy := top + headR + 1
	sc.Fill(scene.Ellipse(scene.Point{X: cx, Y: hy}, headR, headR), t.NodeFill, t.NodeStroke, lineW)
	body := []scene.Point{{X: cx, Y: hy + headR}, {X: cx, Y: hy + headR + 14}}
	sc.Line(scene.Polyline(body), t.NodeStroke, lineW, nil)
	sc.Line(scene.Polyline([]scene.Point{{X: cx - 10, Y: hy + headR + 5}, {X: cx + 10, Y: hy + headR + 5}}), t.NodeStroke, lineW, nil)
	sc.Line(scene.Polyline([]scene.Point{{X: cx - 9, Y: hy + headR + 26}, {X: cx, Y: hy + headR + 14}, {X: cx + 9, Y: hy + headR + 26}}), t.NodeStroke, lineW, nil)
	sc.Label(cx, top+stickH+float64(float64(len(a.lines))*lh/2)+2, a.lines, scene.Center, t.Text)
}

// numberRadius is the radius of the circle of an autonumber: numberR, or
// wider for numbers that do not fit (autonumber 1000).
func numberRadius(m interface{ Width(string) float64 }, num int) float64 {
	return max(numberR, float64(m.Width(strconv.Itoa(num))/2)+3)
}
