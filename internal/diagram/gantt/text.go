package gantt

import (
	"math"
	"strings"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/textgrid"
)

const (
	maxNameCols  = 24
	maxChartCols = 60
	minChartCols = 10
)

// Text renders the gantt chart with block characters.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	nameW := 0
	for _, t := range d.Tasks {
		nameW = max(nameW, textgrid.Width(t.Name))
	}
	nameW = min(nameW, maxNameCols, max(maxCols/3, 4))
	chartW := min(maxChartCols, maxCols-nameW-3)
	if chartW < minChartCols {
		return nil, guard.Limit("columns", nameW+3+minChartCols, maxCols)
	}
	lo, hi := d.span()
	total := seconds(lo, hi)
	col := func(tm float64) int { return int(math.Round(float64(tm / total * float64(chartW)))) }

	var out []string
	if d.Title != "" {
		out = append(out, textgrid.Wrap(d.Title, maxCols)...)
		out = append(out, "")
	}
	sec := -1
	for _, t := range d.Tasks {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		if t.Section != sec {
			sec = t.Section
			if d.Sections[sec] != "" {
				out = append(out, truncate("■ "+strings.ReplaceAll(d.Sections[sec], "\n", " "), maxCols))
			}
		}
		a := col(seconds(lo, t.Start))
		b := max(col(seconds(lo, t.End)), a+1)
		b = min(b, chartW)
		fill := "█"
		switch {
		case t.Done:
			fill = "░"
		case t.Crit:
			fill = "▓"
		case t.Active:
			fill = "▒"
		}
		bar := strings.Repeat(" ", min(a, chartW-1))
		if t.Milestone {
			bar += "◆"
		} else {
			bar += strings.Repeat(fill, max(b-a, 1))
		}
		name := truncate(strings.ReplaceAll(t.Name, "\n", " "), nameW)
		out = append(out, strings.TrimRight(name+strings.Repeat(" ", nameW-textgrid.Width(name))+" │"+bar, " "))
	}
	startL := formatTime(lo, d.AxisFormat)
	endL := formatTime(hi, d.AxisFormat)
	axis := strings.Repeat(" ", nameW) + " └" + strings.Repeat("─", chartW)
	out = append(out, axis)
	gap := chartW - textgrid.Width(startL) - textgrid.Width(endL)
	if gap >= 1 {
		out = append(out, strings.Repeat(" ", nameW+2)+startL+strings.Repeat(" ", gap)+endL)
	} else {
		out = append(out, strings.Repeat(" ", nameW+2)+startL)
	}
	return out, nil
}

func truncate(s string, w int) string {
	if textgrid.Width(s) <= w {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		rw := textgrid.RuneWidth(r)
		if n+rw > w-1 {
			break
		}
		b.WriteRune(r)
		n += rw
	}
	return b.String() + "…"
}
