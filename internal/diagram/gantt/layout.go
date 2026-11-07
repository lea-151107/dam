package gantt

import (
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/lea/mermaid/internal/diagram"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/theme"
)

// Visual constants in layout units.
const (
	barH         = 20.0
	barGap       = 6.0
	sectionPad   = 12.0
	defaultWidth = 800.0
	minChartW    = 200.0
	tickSpacing  = 90.0 // minimum distance between axis labels
	titleGap     = 12.0
	axisGap      = 6.0
	textPad      = 5.0
	maxTicks     = 200
)

// seconds returns b-a in seconds without the ±292-year saturation of
// time.Duration.
func seconds(a, b time.Time) float64 {
	return float64(b.Unix()-a.Unix()) + float64(float64(b.Nanosecond()-a.Nanosecond())/1e9)
}

// span returns the overall time range.
func (d *Diagram) span() (time.Time, time.Time) {
	lo, hi := d.Tasks[0].Start, d.Tasks[0].End
	for _, t := range d.Tasks {
		if t.Start.Before(lo) {
			lo = t.Start
		}
		if t.End.After(hi) {
			hi = t.End
		}
	}
	if !hi.After(lo) {
		hi = lo.Add(24 * time.Hour)
	}
	return lo, hi
}

// maxFitPasses bounds the relayouts that shrink the chart until labels
// sticking out on the right fit into MaxWidth.
const maxFitPasses = 4

// Layout implements diagram.Layouter.
func (d *Diagram) Layout(env *diagram.Env) (*scene.Scene, error) {
	// The chart fills MaxWidth, so a label placed right of the last bar
	// would make the drawing wider than allowed. Shrink the chart by the
	// excess and lay out again instead of letting the caller shrink text.
	shrink := 0.0
	for pass := 0; ; pass++ {
		sc, err := d.layout(env, shrink)
		if err != nil || env.MaxWidth <= 0 || pass == maxFitPasses {
			return sc, err
		}
		excess := sc.Width - env.MaxWidth
		if excess <= 0.5 {
			return sc, nil
		}
		shrink += excess + 1
	}
}

func (d *Diagram) layout(env *diagram.Env, shrink float64) (*scene.Scene, error) {
	m := env.M
	t := env.T
	sc := &scene.Scene{}
	lh := m.LineHeight()

	secLines := make([][]string, len(d.Sections))
	leftW := 0.0
	for i, s := range d.Sections {
		secLines[i] = m.Lines(s, max(env.Wrap, 0))
		w, _ := m.Size(secLines[i])
		leftW = max(leftW, w)
	}
	if leftW > 0 {
		leftW += 2 * sectionPad
	}
	chartW := float64(defaultWidth * math.Max(env.Spacing, 0.5))
	if env.MaxWidth > 0 {
		chartW = env.MaxWidth - leftW - 2*diagram.Margin - shrink
	}
	chartW = math.Max(chartW, minChartW)

	lo, hi := d.span()
	total := seconds(lo, hi)
	xOf := func(tm time.Time) float64 { return leftW + float64(seconds(lo, tm)/total*chartW) }

	y := 0.0
	if d.Title != "" {
		title := env.Label(d.Title)
		_, th := m.Size(title)
		sc.Label(leftW+float64(chartW/2), float64(th/2), title, scene.Center, t.Text)
		y = th + titleGap
	}
	top := y
	rowH := barH + barGap

	// Section bands.
	type band struct {
		y0, y1 float64
		sec    int
	}
	var bands []band
	for i, task := range d.Tasks {
		ry := top + float64(float64(i)*rowH)
		if len(bands) == 0 || bands[len(bands)-1].sec != task.Section {
			bands = append(bands, band{y0: ry, sec: task.Section})
		}
		bands[len(bands)-1].y1 = ry + rowH
	}
	bottom := top + float64(float64(len(d.Tasks))*rowH)
	for i, b := range bands {
		col := theme.Mix(t.Base, t.Color(b.sec), 0.35)
		if i%2 == 1 {
			col = theme.Mix(t.Base, t.Color(b.sec), 0.2)
		}
		sc.Fill(scene.RectPath(scene.Rect{X: 0, Y: b.y0, W: leftW + chartW, H: b.y1 - b.y0}), col, color.NRGBA{}, 0)
		if len(secLines[b.sec]) > 0 {
			sc.Label(sectionPad, float64((b.y0+b.y1)/2), secLines[b.sec], scene.Left, t.Text)
		}
	}

	// Grid and axis.
	ticks, err := d.ticks(lo, hi, chartW, m.Width(formatTime(hi, d.AxisFormat)))
	if err != nil {
		return nil, err
	}
	for _, tk := range ticks {
		x := xOf(tk)
		sc.Line(scene.Polyline([]scene.Point{{X: x, Y: top}, {X: x, Y: bottom}}), theme.Mix(t.Base, t.Text, 0.15), 1, nil)
		sc.Label(x, bottom+axisGap+float64(lh/2), []string{formatTime(tk, d.AxisFormat)}, scene.Center, t.Text)
	}
	sc.Line(scene.Polyline([]scene.Point{{X: leftW, Y: bottom}, {X: leftW + chartW, Y: bottom}}), t.Muted, 1, nil)

	// Bars.
	for i, task := range d.Tasks {
		if err := env.G.Tick(); err != nil {
			return nil, err
		}
		ry := top + float64(float64(i)*rowH) + float64(barGap/2)
		x0, x1 := xOf(task.Start), xOf(task.End)
		fill, stroke := t.NodeFill, t.NodeStroke
		switch {
		case task.Crit && task.Done:
			fill, stroke = theme.Mix(t.Base, t.Critical, 0.35), t.Critical
		case task.Crit:
			fill, stroke = theme.Mix(t.Base, t.Critical, 0.6), t.Critical
		case task.Done:
			fill, stroke = theme.Mix(t.Base, t.Text, 0.25), theme.Mix(t.Base, t.Text, 0.5)
		case task.Active:
			fill = theme.Mix(t.Base, t.Accent, 0.35)
		default:
			fill = theme.Mix(t.Base, t.Accent, 0.65)
		}
		label := []string{task.Name}
		tw := m.Width(task.Name)
		if task.Milestone {
			c := scene.Point{X: x0, Y: ry + barH/2}
			h := barH / 2
			sc.Fill(scene.Polygon([]scene.Point{{X: c.X, Y: c.Y - h}, {X: c.X + h, Y: c.Y}, {X: c.X, Y: c.Y + h}, {X: c.X - h, Y: c.Y}}), fill, stroke, 1)
			sc.Label(c.X+h+textPad, c.Y, label, scene.Left, t.Text)
			continue
		}
		w := math.Max(x1-x0, 2)
		sc.Fill(scene.RoundRect(scene.Rect{X: x0, Y: ry, W: w, H: barH}, 3), fill, stroke, 1)
		switch {
		case tw+2*textPad <= w:
			sc.Label(x0+float64(w/2), ry+float64(barH/2), label, scene.Center, t.Text)
		case x0+w+textPad+tw <= leftW+chartW || x0-textPad-tw < leftW:
			// Right of the bar; also when the left side would run into the
			// section column.
			sc.Label(x0+w+textPad, ry+barH/2, label, scene.Left, t.Text)
		default:
			sc.Label(x0-textPad, ry+barH/2, label, scene.Right, t.Text)
		}
	}
	return diagram.Finish(sc, env, leftW+chartW, bottom+axisGap+lh), nil
}

// ticks chooses axis tick times.
func (d *Diagram) ticks(lo, hi time.Time, chartW, labelW float64) ([]time.Time, error) {
	spanSec := seconds(lo, hi)
	maxN := max(1, int(float64(chartW/math.Max(tickSpacing, labelW+16))))
	day := 24 * time.Hour
	type step struct {
		dur    time.Duration
		months int
	}
	var st step
	switch {
	case d.TickMonths > 0:
		st = step{months: d.TickMonths}
	case d.TickInterval > 0:
		st = step{dur: d.TickInterval}
	default:
		cands := []step{
			{dur: time.Minute}, {dur: 5 * time.Minute}, {dur: 15 * time.Minute}, {dur: time.Hour},
			{dur: 3 * time.Hour}, {dur: 6 * time.Hour}, {dur: 12 * time.Hour}, {dur: day},
			{dur: 2 * day}, {dur: 7 * day}, {dur: 14 * day}, {months: 1}, {months: 3}, {months: 6},
			{months: 12}, {months: 60}, {months: 120}, {months: 600},
		}
		st = cands[len(cands)-1]
		// Steps finer than axisFormat shows gave repeated labels (12 hour
		// ticks labelled 2020-01-01, 2020-01-01, 2020-01-02, ...).
		res, resMonths := formatResolution(d.AxisFormat)
		for _, c := range cands {
			if c.months < resMonths || c.months == 0 && (resMonths > 0 || c.dur < res) {
				continue
			}
			n := 0
			if c.months > 0 {
				n = int(float64(spanSec/(float64(86400*30.44*float64(c.months))))) + 1
			} else {
				n = int(float64(spanSec/c.dur.Seconds())) + 1
			}
			if n <= maxN {
				st = c
				break
			}
		}
	}
	var start time.Time
	switch {
	case st.months > 0:
		start = time.Date(lo.Year(), lo.Month(), 1, 0, 0, 0, 0, time.UTC)
	case st.dur >= day:
		start = time.Date(lo.Year(), lo.Month(), lo.Day(), 0, 0, 0, 0, time.UTC)
	default:
		start = lo.Truncate(st.dur)
	}
	var out []time.Time
	for tk, i := start, 0; !tk.After(hi); i++ {
		if i > maxTicks*4 {
			break
		}
		if !tk.Before(lo) {
			out = append(out, tk)
			if len(out) > maxTicks {
				break
			}
		}
		if st.months > 0 {
			tk = start.AddDate(0, st.months*(i+1), 0)
		} else {
			tk = tk.Add(st.dur)
		}
	}
	return out, nil
}

// formatResolution returns the finest unit an axis format shows: a
// duration, or for formats without days a number of months (0 when the
// format shows days or finer). A format with no time directive at all
// has no resolution (0, 0).
func formatResolution(f string) (time.Duration, int) {
	has := map[byte]bool{}
	for i := 0; i+1 < len(f); i++ {
		if f[i] == '%' {
			i++
			has[f[i]] = true
		}
	}
	uses := func(cs string) bool {
		for i := range len(cs) {
			if has[cs[i]] {
				return true
			}
		}
		return false
	}
	switch {
	case uses("S"):
		return 0, 0 // seconds and finer: every step is shown
	case uses("M"):
		return time.Minute, 0
	case uses("HIp"):
		return time.Hour, 0
	case uses("deaAj"):
		return 24 * time.Hour, 0
	case uses("UWV"):
		return 7 * 24 * time.Hour, 0
	case uses("mbB"):
		return 0, 1
	case uses("Yy"):
		return 0, 12
	}
	return 0, 0
}

// formatTime formats tm with a strftime-style pattern.
func formatTime(tm time.Time, f string) string {
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		if f[i] != '%' || i+1 == len(f) {
			b.WriteByte(f[i])
			continue
		}
		i++
		switch f[i] {
		case 'Y':
			b.WriteString(tm.Format("2006"))
		case 'y':
			b.WriteString(tm.Format("06"))
		case 'm':
			b.WriteString(tm.Format("01"))
		case 'd':
			b.WriteString(tm.Format("02"))
		case 'e':
			b.WriteString(strconv.Itoa(tm.Day()))
		case 'b':
			b.WriteString(tm.Format("Jan"))
		case 'B':
			b.WriteString(tm.Format("January"))
		case 'a':
			b.WriteString(tm.Format("Mon"))
		case 'A':
			b.WriteString(tm.Format("Monday"))
		case 'H':
			b.WriteString(tm.Format("15"))
		case 'I':
			b.WriteString(tm.Format("03"))
		case 'M':
			b.WriteString(tm.Format("04"))
		case 'S':
			b.WriteString(tm.Format("05"))
		case 'p':
			b.WriteString(tm.Format("PM"))
		case 'j':
			b.WriteString(tm.Format("002"))
		case 'U', 'W', 'V':
			_, w := tm.ISOWeek()
			b.WriteString(strconv.Itoa(w))
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(f[i])
		}
	}
	return b.String()
}
