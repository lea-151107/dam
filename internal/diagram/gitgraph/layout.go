package gitgraph

import (
	"cmp"
	"image/color"
	"slices"

	"github.com/lea-151107/dam/internal/diagram"
	"github.com/lea-151107/dam/internal/scene"
	"github.com/lea-151107/dam/internal/theme"
)

// Visual constants in layout units.
const (
	commitR    = 9.0
	minGap     = 44.0
	laneGap    = 56.0
	labelPad   = 8.0
	lineW      = 2.5
	tagPad     = 4.0
	labelSpace = 10.0
)

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	lh := m.LineHeight()
	sc := &scene.Scene{}

	lanes := d.lanes()
	nameW := 0.0
	for _, b := range d.Branches {
		nameW = max(nameW, m.Width(b.Name))
	}
	labelW := 0.0
	for _, c := range d.Commits {
		labelW = max(labelW, m.Width(c.ID))
	}
	horizontal := d.Orient == LR
	gap := max(float64(minGap*env.Spacing)+commitR, float64(2*commitR)+8)
	if horizontal {
		gap = max(gap, labelW+labelPad)
	}
	lg := float64(laneGap * max(env.Spacing, 0.6))
	if !horizontal {
		lg = max(lg, labelW+2*commitR+labelPad*2)
	}
	start := nameW + 2*labelPad + labelSpace + commitR
	if !horizontal {
		start = lh + 2*labelPad + labelSpace + commitR
	}
	n := len(d.Commits)
	pos := func(i int) scene.Point {
		c := d.Commits[i]
		along := start + float64(float64(i)*gap)
		if d.Orient == BT {
			along = start + float64(float64(n-1-i)*gap)
		}
		across := float64(float64(lanes[c.Branch]) * lg)
		if horizontal {
			return scene.Point{X: along, Y: across + lh}
		}
		return scene.Point{X: across + float64(nameW/2) + labelPad, Y: along}
	}
	end := start + float64(float64(max(n-1, 0))*gap) + commitR + labelSpace

	branchColor := func(b int) color.NRGBA { return t.Strong(lanes[b]) }
	// Lanes and branch labels.
	for b, br := range d.Branches {
		col := branchColor(b)
		across := float64(float64(lanes[b]) * lg)
		var a, z, lp scene.Point
		if horizontal {
			a, z = scene.Point{X: start - commitR - labelSpace/2, Y: across + lh}, scene.Point{X: end, Y: across + lh}
			lp = scene.Point{X: float64(nameW/2) + labelPad, Y: across + lh}
		} else {
			x := across + float64(nameW/2) + labelPad
			a, z = scene.Point{X: x, Y: start - commitR - labelSpace/2}, scene.Point{X: x, Y: end}
			lp = scene.Point{X: x, Y: float64(lh/2) + labelPad}
			if d.Orient == BT {
				lp.Y = end + float64(lh/2) + labelPad
			}
		}
		sc.Line(scene.Polyline([]scene.Point{a, z}), theme.Mix(t.Base, col, 0.4), 1, []float64{3, 4})
		w := m.Width(br.Name) + 2*labelPad
		r := scene.Rect{X: lp.X - float64(w/2), Y: lp.Y - float64(lh/2) - float64(labelPad/2), W: w, H: lh + labelPad}
		sc.Fill(scene.RoundRect(r, 4), theme.Mix(t.Base, t.Color(lanes[b]), 0.9), col, 1)
		sc.Label(lp.X, lp.Y, []string{br.Name}, scene.Center, t.Text)
	}
	// Edges.
	for i, c := range d.Commits {
		if err := env.G.Tick(); err != nil {
			return nil, err
		}
		cp := pos(i)
		for _, p := range c.Parents {
			pp := pos(p)
			col := branchColor(c.Branch)
			if d.Commits[p].Branch != c.Branch && c.Merge {
				col = branchColor(d.Commits[p].Branch)
			}
			path := scene.NewPath().MoveTo(pp.X, pp.Y)
			switch {
			// Same lane: a straight line. Compare lanes, not float coordinates.
			case lanes[d.Commits[p].Branch] == lanes[c.Branch]:
				path.LineTo(cp.X, cp.Y)
			case horizontal:
				mx := cp.X - float64(gap/2)
				if c.Merge && d.Commits[p].Branch != c.Branch {
					path.LineTo(mx, pp.Y)
				} else {
					mx = pp.X + float64(gap/2)
				}
				path.CubeTo(mx+float64(gap/4), pp.Y, mx+float64(gap/4)-float64(gap/4), cp.Y, mx+float64(gap/2), cp.Y)
				path.LineTo(cp.X, cp.Y)
			default:
				my := pp.Y + float64((cp.Y-pp.Y)/2)
				path.CubeTo(pp.X, my, cp.X, my, cp.X, cp.Y)
			}
			sc.Line(path, col, lineW, nil)
		}
	}
	// Commits.
	for i, c := range d.Commits {
		p := pos(i)
		col := branchColor(c.Branch)
		switch {
		case c.Type == Highlight:
			r := scene.Rect{X: p.X - commitR, Y: p.Y - commitR, W: 2 * commitR, H: 2 * commitR}
			sc.Fill(scene.RectPath(r), theme.Mix(t.Base, col, 0.5), col, 2)
		case c.Merge:
			sc.Fill(scene.Ellipse(p, commitR, commitR), col, col, 1)
			sc.Fill(scene.Ellipse(p, commitR*0.5, commitR*0.5), t.Base, color.NRGBA{}, 0)
		default:
			sc.Fill(scene.Ellipse(p, commitR, commitR), col, theme.Mix(col, t.Text, 0.3), 1)
		}
		if c.Type == Reverse {
			k := commitR * 0.55
			sc.Line(scene.Polyline([]scene.Point{{X: p.X - k, Y: p.Y - k}, {X: p.X + k, Y: p.Y + k}}), t.Base, 2, nil)
			sc.Line(scene.Polyline([]scene.Point{{X: p.X - k, Y: p.Y + k}, {X: p.X + k, Y: p.Y - k}}), t.Base, 2, nil)
		}
		if c.Cherry {
			sc.Fill(scene.Ellipse(scene.Point{X: p.X - 3, Y: p.Y + 2}, 2.5, 2.5), t.Base, color.NRGBA{}, 0)
			sc.Fill(scene.Ellipse(scene.Point{X: p.X + 3, Y: p.Y + 2}, 2.5, 2.5), t.Base, color.NRGBA{}, 0)
		}
		// Commit id.
		iw := m.Width(c.ID)
		if horizontal {
			ly := p.Y + commitR + float64(labelPad/2) + float64(lh/2)
			sc.Fill(scene.RoundRect(scene.Rect{X: p.X - float64(iw/2) - 2, Y: ly - float64(lh/2), W: iw + 4, H: lh}, 2), t.LabelBg, color.NRGBA{}, 0)
			sc.Label(p.X, ly, []string{c.ID}, scene.Center, t.Muted)
		} else {
			lx := p.X + commitR + labelPad/2
			sc.Fill(scene.RoundRect(scene.Rect{X: lx - 2, Y: p.Y - float64(lh/2), W: iw + 4, H: lh}, 2), t.LabelBg, color.NRGBA{}, 0)
			sc.Label(lx, p.Y, []string{c.ID}, scene.Left, t.Muted)
		}
		// Tags.
		ty := p.Y - commitR - tagPad - float64(lh/2) - 2
		for k := len(c.Tags) - 1; k >= 0; k-- {
			tag := c.Tags[k]
			w := m.Width(tag) + 2*tagPad
			var r scene.Rect
			if horizontal {
				r = scene.Rect{X: p.X - float64(w/2), Y: ty - float64(lh/2) - float64(tagPad/2), W: w, H: lh + tagPad}
			} else {
				r = scene.Rect{X: p.X - commitR - labelPad - w, Y: p.Y - float64(lh/2) - float64(tagPad/2) - float64(float64(len(c.Tags)-1-k)*(lh+tagPad+2)), W: w, H: lh + tagPad}
			}
			sc.Fill(scene.RoundRect(r, 3), t.NoteFill, t.NoteStroke, 1)
			sc.Label(r.X+float64(r.W/2), r.Y+float64(r.H/2), []string{tag}, scene.Center, t.Text)
			ty -= lh + tagPad + 2
		}
	}
	w, h := end, float64(float64(len(d.Branches))*lg)
	if !horizontal {
		w, h = h, end+lh+2*labelPad
	}
	return diagram.Finish(sc, env, w, h), nil
}

// lanes returns the lane of each branch: by Order, then creation.
func (d *Diagram) lanes() []int {
	lanes := make([]int, len(d.Branches))
	order := make([]int, len(d.Branches))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(d.Branches[a].Order, d.Branches[b].Order) })
	for lane, b := range order {
		lanes[b] = lane
	}
	return lanes
}
