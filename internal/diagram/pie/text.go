package pie

import (
	"fmt"
	"strings"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/textgrid"
)

const maxBarCols = 40

// Text renders the pie chart as horizontal bars.
func (d *Diagram) Text(g *guard.Guard, maxCols int) ([]string, error) {
	var out []string
	if d.Title != "" {
		out = append(out, textgrid.Wrap(d.Title, maxCols)...)
		out = append(out, "")
	}
	labelW := 0
	labels := make([]string, len(d.Slices))
	for i, s := range d.Slices {
		labels[i] = strings.ReplaceAll(s.Label, "\n", " ")
		if d.ShowData {
			labels[i] += " [" + formatValue(s.Value) + "]"
		}
		labelW = max(labelW, textgrid.Width(labels[i]))
	}
	const pctW = 7 // " 100.0%"
	labelW = min(labelW, max(maxCols/2, 4))
	barW := min(maxBarCols, maxCols-labelW-pctW-2)
	if barW < 4 {
		return nil, guard.Limit("columns", labelW+pctW+6, maxCols)
	}
	for i, s := range d.Slices {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		frac := float64(s.Value / d.total)
		n := int(float64(frac*float64(barW)) + 0.5)
		l := truncate(labels[i], labelW)
		pad := strings.Repeat(" ", labelW-textgrid.Width(l))
		out = append(out, fmt.Sprintf("%s%s │%s%s %5.1f%%", l, pad, strings.Repeat("█", n), strings.Repeat(" ", barW-n), float64(frac*100)))
	}
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
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
