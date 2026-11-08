package sequence

import (
	"strconv"
	"strings"

	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/textgrid"
)

var textWraps = [...]int{0, 30, 20, 12, 8}

// Text renders the sequence diagram with box-drawing characters.
// Activations are drawn as heavy lifelines (┃).
// NOTE(mermaid-compat): participant boxes (box ... end) have no background
// in text output.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	var last []string
	for _, wrap := range textWraps {
		lines, err := d.text(g, wrap)
		if err != nil {
			return nil, err
		}
		if textgrid.MaxWidth(lines) <= maxCols {
			return lines, nil
		}
		last = lines
	}
	return nil, guard.Limit("columns", textgrid.MaxWidth(last), maxCols)
}

func (d *Diagram) text(g *guard.Guard, wrap int) ([]string, error) {
	n := len(d.Participants)
	labels := make([][]string, n)
	widths := make([]int, n)
	for i, p := range d.Participants {
		labels[i] = textgrid.Wrap(p.Label, wrap)
		if p.Type != "" {
			labels[i] = append([]string{"«" + p.Type + "»"}, labels[i]...)
		}
		widths[i] = textgrid.MaxWidth(labels[i]) + 4
	}
	gaps := make([]int, max(n-1, 0))
	for i := range gaps {
		gaps[i] = (widths[i]+widths[i+1])/2 + 3
	}
	evLines := make([][]string, len(d.Events))
	widen := func(a, b, need int) {
		if a > b {
			a, b = b, a
		}
		if a < 0 || b >= n || a == b {
			return
		}
		have := 0
		for k := a; k < b; k++ {
			have += gaps[k]
		}
		for k := a; have < need; k = a + (k-a+1)%(b-a) {
			gaps[k]++
			have++
		}
	}
	for i, ev := range d.Events {
		switch ev.Kind {
		case Message, Note, BlockStart, BlockElse:
			label := ev.Text
			if ev.Kind == Message && ev.Numbered {
				// The image draws the number in a circle at the arrow's
				// start; text writes it before the message.
				label = strconv.Itoa(ev.Number) + ". " + label
			}
			evLines[i] = textgrid.Wrap(label, wrap)
		}
		w := textgrid.MaxWidth(evLines[i])
		switch {
		case ev.Kind == Message && ev.From == ev.To:
			widen(ev.From, ev.From+1, w+5+widths[min(ev.From+1, n-1)]/2)
		case ev.Kind == Message:
			widen(ev.From, ev.To, w+4)
		case ev.Kind == Note && ev.Place == RightOf:
			widen(ev.From, ev.From+1, w+5+widths[min(ev.From+1, n-1)]/2)
		case ev.Kind == Note && ev.Place == LeftOf:
			widen(ev.From-1, ev.From, w+5+widths[max(ev.From-1, 0)]/2)
		}
	}
	cx := make([]int, n)
	left := widths[0] / 2
	for i, e := range d.Events {
		if e.Kind == Note && e.Place == LeftOf && e.From == 0 {
			left = max(left, textgrid.MaxWidth(evLines[i])+5)
		}
	}
	cx[0] = left
	for i := 1; i < n; i++ {
		cx[i] = cx[i-1] + gaps[i-1]
	}
	// Compute the exact horizontal extent of everything drawn so that the
	// canvas never clips text; shift right if anything starts left of 0.
	blockLabel := func(i int) string {
		ev := d.Events[i]
		label := ""
		if ev.Kind == BlockStart || ev.Kind == BlockElse {
			label = "[" + ev.Block + "]"
		}
		if len(evLines[i]) > 0 {
			label += " " + strings.Join(evLines[i], " ")
		}
		if label == "" {
			return ""
		}
		return " " + label + " "
	}
	extent := func() (lo, hi int) {
		lo, hi = 0, cx[n-1]+widths[n-1]-widths[n-1]/2
		for i := range d.Participants {
			lo = min(lo, cx[i]-widths[i]/2)
		}
		for i, ev := range d.Events {
			w := textgrid.MaxWidth(evLines[i])
			switch ev.Kind {
			case Message:
				if ev.From == ev.To {
					hi = max(hi, cx[ev.From]+4+w)
				} else {
					m := (cx[ev.From] + cx[ev.To]) / 2
					lo, hi = min(lo, m-w/2), max(hi, m-w/2+w)
				}
			case Note:
				x, nw := d.noteBox(ev, evLines[i], cx)
				lo, hi = min(lo, x), max(hi, x+nw)
			case BlockStart, BlockElse:
				hi = max(hi, 1+textgrid.Width(blockLabel(i)))
			}
		}
		return lo, hi
	}
	if lo, _ := extent(); lo < 0 {
		for i := range cx {
			cx[i] -= lo
		}
	}
	_, right := extent()
	headH := 0
	for i := range labels {
		headH = max(headH, len(labels[i])+2)
	}
	// Count rows.
	rows := headH
	for i, ev := range d.Events {
		switch ev.Kind {
		case Message:
			rows += len(evLines[i]) + 1
			if ev.From == ev.To {
				rows++
			}
		case Note:
			rows += len(evLines[i]) + 2
		case BlockStart, BlockElse, BlockEnd:
			rows++
		case Create:
			rows += headH
		}
	}
	rows += headH
	grid, err := textgrid.New(right+2, rows+1)
	if err != nil {
		return nil, err
	}
	// drawHeads draws the head boxes; at returns each participant's top
	// row and whether it has a box there.
	drawHeads := func(at func(p int) (int, bool)) {
		for i := range d.Participants {
			y, ok := at(i)
			if !ok {
				continue
			}
			x := cx[i] - widths[i]/2
			grid.Box(x, y, widths[i], headH, [4]rune{})
			for k, l := range labels[i] {
				grid.Text(x+(widths[i]-textgrid.Width(l))/2, y+1+k, l)
			}
		}
	}
	y := headH
	bottom := rows - headH
	// Lifelines are drawn after the events, from where each participant
	// starts (top, or where it is created) to where it ends.
	born := make([]int, n)
	dead := make([]int, n)
	createdAt := make([]int, n)
	pendingCreate := make([]bool, n)
	pendingDestroy := make([]bool, n)
	for i := range born {
		born[i], dead[i], createdAt[i] = headH-1, -1, -1
	}
	type act struct{ who, y0, y1 int }
	var acts []act
	actStart := make([][]int, n)
	type cross struct{ x, y int }
	var crosses []cross
	meet := func(row int, ps ...int) {
		for _, p := range ps {
			if pendingDestroy[p] {
				pendingDestroy[p] = false
				dead[p] = row
				crosses = append(crosses, cross{cx[p], row})
			}
		}
	}
	minX, maxX := 0, right
	for i, ev := range d.Events {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		lines := evLines[i]
		switch ev.Kind {
		case Create:
			pendingCreate[ev.From] = true
			continue
		case Destroy:
			pendingDestroy[ev.From] = true
			continue
		case Activate:
			actStart[ev.From] = append(actStart[ev.From], y)
			continue
		case Deactivate:
			if k := len(actStart[ev.From]); k > 0 {
				acts = append(acts, act{ev.From, actStart[ev.From][k-1], y - 1})
				actStart[ev.From] = actStart[ev.From][:k-1]
			}
			continue
		}
		if ev.Kind == Message {
			// A created participant's head box goes just above the
			// message that creates it.
			for _, p := range []int{ev.From, ev.To} {
				if pendingCreate[p] {
					pendingCreate[p] = false
					createdAt[p] = y
					born[p] = y + headH - 1
					y += headH
				}
			}
		}
		switch ev.Kind {
		case Message:
			if ev.From == ev.To {
				x := cx[ev.From]
				for k, l := range lines {
					grid.Text(x+4, y+k, l)
				}
				y += max(len(lines)-1, 0)
				grid.HLine(x, x+2, y)
				grid.VLine(x+2, y, y+1)
				grid.HLine(x+1, x+2, y+1)
				grid.Set(x+1, y+1, headRune(ev.Head, true))
				meet(y+1, ev.From)
				y += 2
				continue
			}
			a, b := cx[ev.From], cx[ev.To]
			for k, l := range lines {
				grid.Text((a+b)/2-textgrid.Width(l)/2, y+k, l)
			}
			y += len(lines)
			lo, hi := min(a, b)+1, max(a, b)-1
			if ev.Dotted {
				for x := lo; x <= hi; x++ {
					grid.Set(x, y, '┄')
				}
			} else {
				grid.HLine(lo, hi, y)
			}
			if b > a {
				grid.Set(hi, y, headRune(ev.Head, false))
			} else {
				grid.Set(lo, y, headRune(ev.Head, true))
			}
			meet(y, ev.From, ev.To)
			y++
		case Note:
			x, w := d.noteBox(ev, lines, cx)
			grid.Box(x, y, w, len(lines)+2, [4]rune{})
			for k, l := range lines {
				grid.Text(x+2, y+1+k, l)
			}
			y += len(lines) + 2
		case BlockStart, BlockElse, BlockEnd:
			for x := minX; x <= maxX; x++ {
				grid.Set(x, y, '┄')
			}
			if label := blockLabel(i); label != "" {
				grid.Text(minX+1, y, label)
			}
			y++
		}
	}
	for p := range actStart {
		for _, y0 := range actStart[p] {
			acts = append(acts, act{p, y0, bottom - 1})
		}
	}
	for i := range d.Participants {
		end := bottom
		if dead[i] >= 0 {
			end = dead[i]
		}
		grid.VLine(cx[i], born[i], end)
	}
	for _, a := range acts {
		grid.HeavyVLine(cx[a.who], a.y0, a.y1)
	}
	for _, c := range crosses {
		grid.Set(c.x, c.y, '×')
	}
	drawHeads(func(p int) (int, bool) {
		if createdAt[p] >= 0 {
			return createdAt[p], true
		}
		return 0, true
	})
	drawHeads(func(p int) (int, bool) { return bottom, dead[p] < 0 })
	return grid.Lines(), nil
}

// noteBox returns the left column and width of a note.
func (d *Diagram) noteBox(ev Event, lines []string, cx []int) (x, w int) {
	w = textgrid.MaxWidth(lines) + 4
	switch {
	case ev.Place == LeftOf:
		x = cx[ev.From] - 1 - w
	case ev.Place == RightOf:
		x = cx[ev.From] + 2
	default:
		a, b := min(cx[ev.From], cx[ev.To]), max(cx[ev.From], cx[ev.To])
		w = max(w, b-a+4)
		x = (a+b)/2 - w/2
	}
	return x, w
}

func headRune(h ArrowEnd, left bool) rune {
	switch h {
	case HeadCross:
		return '×'
	case HeadNone:
		return '─'
	}
	if left {
		return '◀'
	}
	return '▶'
}
