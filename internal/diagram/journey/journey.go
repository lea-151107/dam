// Package journey parses and lays out Mermaid user journey diagrams.
package journey

import (
	"math"
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

// Task is one step of the journey.
type Task struct {
	Name    string
	Score   int // 0 to 5
	Actors  []int
	Section int // -1 before the first section
}

// Diagram is a parsed user journey.
type Diagram struct {
	Title    string
	Sections []string
	Actors   []string
	Tasks    []Task
	actorIdx map[string]int
}

// maxScore is the best score; faces are drawn on a scale from 0 to it.
const maxScore = 5

var taskRe = regexp.MustCompile(`^(.+?)\s*:\s*(\d+)\s*(?::\s*(.*))?$`)

// Parse parses a user journey.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{actorIdx: map[string]int{}}
	section := -1
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		if r, ok := source.CutKeyword(t, "title"); ok {
			d.Title = source.Label(r)
			continue
		}
		if r, ok := source.CutKeyword(t, "section"); ok {
			d.Sections = append(d.Sections, source.Label(r))
			section = len(d.Sections) - 1
			continue
		}
		if strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr") {
			// NOTE(mermaid-compat): accessibility texts do not affect the image.
			continue
		}
		m := taskRe.FindStringSubmatch(t)
		if m == nil {
			return nil, source.Errorf(l.No, "expected task: score: actors")
		}
		score, err := strconv.Atoi(m[2])
		if err != nil || score > maxScore {
			return nil, source.Errorf(l.No, "score %q is not between 0 and %d", m[2], maxScore)
		}
		task := Task{Name: source.Label(m[1]), Score: score, Section: section}
		for _, a := range strings.Split(m[3], ",") {
			if a = strings.TrimSpace(a); a == "" {
				continue
			}
			i, ok := d.actorIdx[a]
			if !ok {
				i = len(d.Actors)
				d.actorIdx[a] = i
				d.Actors = append(d.Actors, source.Label(a))
			}
			task.Actors = append(task.Actors, i)
		}
		d.Tasks = append(d.Tasks, task)
		if err := g.Count("tasks and actors", len(d.Tasks)+len(d.Actors)); err != nil {
			return nil, err
		}
	}
	if len(d.Tasks) == 0 {
		return nil, source.Errorf(lines[len(lines)-1].No, "journey has no tasks")
	}
	return d, nil
}

// DiagramTitle returns the title given with the title statement.
func (d *Diagram) DiagramTitle() string { return d.Title }

// Visual constants in layout units.
const (
	padX      = 10.0
	padY      = 8.0
	taskW     = 120.0
	gap       = 12.0
	bandGap   = 8.0
	levelH    = 24.0 // height of one score step
	faceR     = 13.0
	actorR    = 5.0
	legendGap = 24.0
	lineW     = 1.3
	radius    = 5.0
	// actorColor offsets actor colors in the palette from the section
	// colors, so that the dots stand out on the task boxes.
	actorColor = 6
)

// Layout implements diagram.Layouter: actors on the left, then section
// bands, task boxes and faces placed by score.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	m := env.M
	t := env.T
	sc := &scene.Scene{}
	lh := m.LineHeight()
	// Legend.
	legendW := 0.0
	for i, a := range d.Actors {
		w := m.Width(a)
		legendW = max(legendW, w+3*actorR+6)
		y := float64(lh*float64(i)) + float64(lh/2)
		sc.Fill(scene.Ellipse(scene.Point{X: actorR, Y: y}, actorR, actorR), t.Color(actorColor+i), t.Strong(actorColor+i), 1)
		sc.Label(3*actorR, y, []string{a}, scene.Left, t.Text)
	}
	x0 := 0.0
	if legendW > 0 {
		x0 = legendW + legendGap
	}
	tw := float64(taskW * math.Max(env.Spacing, 0.7))
	if env.Wrap > 0 {
		tw = math.Min(tw, env.Wrap)
	}
	labels := make([][]string, len(d.Tasks))
	boxH := 0.0
	for i, task := range d.Tasks {
		labels[i] = m.Lines(task.Name, tw-2*padX)
		_, h := m.Size(labels[i])
		boxH = max(boxH, h+float64(2*padY)+float64(2*actorR))
	}
	xs := make([]float64, len(d.Tasks))
	for i := range d.Tasks {
		xs[i] = x0 + float64(float64(i)*(tw+gap))
	}
	width := x0 + float64(float64(len(d.Tasks))*(tw+gap)) - gap
	y := 0.0
	colorOf := func(task Task, i int) int {
		if task.Section >= 0 {
			return task.Section
		}
		return i
	}
	if len(d.Sections) > 0 {
		bandH := lh + 2*padY
		for s, name := range d.Sections {
			lo, hi := math.Inf(1), math.Inf(-1)
			for i, task := range d.Tasks {
				if task.Section == s {
					lo, hi = math.Min(lo, xs[i]), math.Max(hi, xs[i]+tw)
				}
			}
			if lo > hi {
				continue
			}
			r := scene.Rect{X: lo, Y: 0, W: hi - lo, H: bandH}
			sc.Fill(scene.RoundRect(r, radius), theme.Mix(t.Base, t.Color(s), 0.85), t.Strong(s), lineW)
			sc.Label(r.X+float64(r.W/2), r.Y+float64(r.H/2), m.Lines(name, r.W-float64(2*padX)), scene.Center, t.Text)
		}
		y = bandH + bandGap
	}
	faceTop := y + boxH + levelH
	bottom := faceTop + maxScore*levelH + faceR
	for i, task := range d.Tasks {
		k := colorOf(task, i)
		r := scene.Rect{X: xs[i], Y: y, W: tw, H: boxH}
		cx := r.X + float64(r.W/2)
		fy := faceTop + float64(float64(maxScore-task.Score)*levelH)
		sc.Line(scene.Polyline([]scene.Point{{X: cx, Y: r.Y + r.H}, {X: cx, Y: bottom}}), t.Muted, 1, []float64{4, 3})
		sc.Fill(scene.RoundRect(r, radius), theme.Mix(t.Base, t.Color(k), 0.85), t.Strong(k), lineW)
		sc.Label(cx, r.Y+padY+float64(float64(len(labels[i]))*lh/2), labels[i], scene.Center, t.Text)
		for a, actor := range task.Actors {
			p := scene.Point{X: r.X + padX + actorR + float64(float64(a)*(float64(2*actorR)+3)), Y: r.Y + r.H - float64(padY/2) - actorR}
			sc.Fill(scene.Ellipse(p, actorR, actorR), t.Color(actorColor+actor), t.Strong(actorColor+actor), 1)
		}
		drawFace(sc, t, scene.Point{X: cx, Y: fy}, task.Score)
	}
	return diagram.Finish(sc, env, width, bottom), nil
}

// drawFace draws a face that smiles for good scores and frowns for bad ones.
func drawFace(sc *scene.Scene, t theme.Theme, c scene.Point, score int) {
	fill := theme.Mix(t.Base, t.Color(2), 0.7) // green-ish
	switch {
	case score <= 2:
		fill = theme.Mix(t.Base, t.Critical, 0.5)
	case score == 3:
		fill = theme.Mix(t.Base, t.Color(1), 0.7)
	}
	sc.Fill(scene.Ellipse(c, faceR, faceR), fill, t.Line, lineW)
	for _, dx := range []float64{-faceR * 0.35, faceR * 0.35} {
		sc.Fill(scene.Ellipse(scene.Point{X: c.X + dx, Y: c.Y - faceR*0.25}, 1.6, 1.6), t.Line, t.Line, 0)
	}
	mouthY := c.Y + faceR*0.35
	w := faceR * 0.45
	p := scene.NewPath().MoveTo(c.X-w, mouthY)
	switch {
	case score >= 4:
		p.QuadTo(c.X, mouthY+faceR*0.35, c.X+w, mouthY)
	case score <= 2:
		p.QuadTo(c.X, mouthY-faceR*0.35, c.X+w, mouthY)
	default:
		p.LineTo(c.X+w, mouthY)
	}
	sc.Line(p, t.Line, lineW, nil)
}

// Text renders the journey as a table under section headings: task,
// score as ●●●○○, and actors.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	nameW := 0
	for _, task := range d.Tasks {
		nameW = max(nameW, textgrid.Width(strings.ReplaceAll(task.Name, "\n", " ")))
	}
	const indent = 2
	nameW = min(nameW, max(maxCols-indent-maxScore-2-minCols, minCols))
	room := maxCols - indent - nameW - 2 - maxScore - 2
	if room < 0 {
		return nil, guard.Limit("columns", indent+nameW+2+maxScore, maxCols)
	}
	var out []string
	section := -1
	for _, task := range d.Tasks {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		if task.Section != section && task.Section >= 0 {
			section = task.Section
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, textgrid.Wrap("■ "+d.Sections[section], maxCols)...)
		}
		names := textgrid.Wrap(strings.ReplaceAll(task.Name, "\n", " "), nameW)
		score := strings.Repeat("●", task.Score) + strings.Repeat("○", maxScore-task.Score)
		var actors []string
		for _, a := range task.Actors {
			actors = append(actors, d.Actors[a])
		}
		who := []string{""}
		if room > 0 {
			who = textgrid.Wrap(strings.Join(actors, ", "), room)
		}
		for k := 0; k < max(len(names), len(who), 1); k++ {
			n, w, s := "", "", ""
			if k < len(names) {
				n = names[k]
			}
			if k < len(who) {
				w = who[k]
			}
			if k == 0 {
				s = score
			} else {
				s = strings.Repeat(" ", maxScore)
			}
			out = append(out, strings.TrimRight(strings.Repeat(" ", indent)+n+strings.Repeat(" ", nameW-textgrid.Width(n))+"  "+s+"  "+w, " "))
		}
	}
	return out, nil
}

// minCols is the narrowest column for task names.
const minCols = 6
