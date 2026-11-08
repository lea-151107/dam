// Package textgrid is a character canvas for box-drawing output. East Asian
// wide characters occupy two cells.
package textgrid

import (
	"strings"
	"unicode"

	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/richtext"
)

// MaxCells bounds the canvas size. The edge router keeps about 100 bytes
// per cell, so 4 Mi cells (a 1 MiB label of 200000 lines) took 500 MB;
// 1 Mi cells is 5000 lines of 200 columns, past any terminal.
const MaxCells = 1 << 20

// Line direction bits.
const (
	Up uint8 = 1 << iota
	Down
	Left
	Right
)

// wideCont marks the second cell of a wide character.
const wideCont = -1

// Grid is a fixed-size character canvas.
type Grid struct {
	W, H  int
	runes [][]rune
	bits  [][]uint8
	// marks holds the combining marks (Mn, Me) that follow the character
	// of a cell, by y*W+x. They take no cell of their own, but dropping
	// them changed the text: NFD "が" became "か", "café" became "cafe".
	marks map[int]string
}

// New returns an empty grid.
func New(w, h int) (*Grid, error) {
	// A negative size comes from an overflow in the caller; checking each
	// side first keeps w*h from overflowing here.
	if w < 0 || h < 0 || w > MaxCells || h > MaxCells {
		return nil, guard.Limit("text cells", MaxCells+1, MaxCells)
	}
	w, h = max(w, 1), max(h, 1)
	if err := guard.Limit("text cells", w*h, MaxCells); err != nil {
		return nil, err
	}
	g := &Grid{W: w, H: h, runes: make([][]rune, h), bits: make([][]uint8, h)}
	for y := range g.runes {
		g.runes[y] = make([]rune, w)
		g.bits[y] = make([]uint8, w)
	}
	return g, nil
}

func (g *Grid) in(x, y int) bool { return x >= 0 && y >= 0 && x < g.W && y < g.H }

// Set places rune r at (x, y), overriding line bits. Overwriting either
// half of a wide character blanks the other half so that rows stay aligned.
func (g *Grid) Set(x, y int, r rune) {
	if !g.in(x, y) {
		return
	}
	switch old := g.runes[y][x]; {
	case old == wideCont && x > 0 && r != wideCont:
		g.runes[y][x-1] = ' '
		delete(g.marks, y*g.W+x-1)
	case old > 0 && RuneWidth(old) == 2 && x+1 < g.W && g.runes[y][x+1] == wideCont:
		g.runes[y][x+1] = 0
	}
	g.runes[y][x] = r
	g.bits[y][x] = 0
	delete(g.marks, y*g.W+x)
}

// isMark reports whether r is a combining mark, which Text keeps with the
// character before it. Other zero-width characters (ZWJ and other format
// characters) are dropped: a terminal may join the characters around them
// into one glyph narrower than the cells counted for them.
func isMark(r rune) bool { return r >= 0x300 && unicode.In(r, unicode.Mn, unicode.Me) }

// Text writes s starting at (x, y).
func (g *Grid) Text(x, y int, s string) {
	prev := -1 // cell of the last character written, for combining marks
	for _, r := range s {
		w := RuneWidth(r)
		if w == 0 {
			if prev >= 0 && isMark(r) {
				if g.marks == nil {
					g.marks = map[int]string{}
				}
				g.marks[prev] += string(r)
			}
			continue
		}
		prev = -1
		if w == 2 && x+1 >= g.W {
			// A wide character does not fit in the last column.
			g.Set(x, y, ' ')
			x += w
			continue
		}
		g.Set(x, y, r)
		if g.in(x, y) && r != ' ' {
			prev = y*g.W + x
		}
		if w == 2 {
			g.Set(x+1, y, wideCont)
		}
		x += w
	}
}

// Clear erases a rectangle.
func (g *Grid) Clear(x, y, w, h int) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			if g.in(xx, yy) {
				g.runes[yy][xx] = 0
				g.bits[yy][xx] = 0
				delete(g.marks, yy*g.W+xx)
			}
		}
	}
}

func (g *Grid) addBits(x, y int, b uint8) {
	if !g.in(x, y) {
		return
	}
	if g.runes[y][x] != 0 {
		return
	}
	g.bits[y][x] |= b
}

// Join connects two orthogonally adjacent cells with a line segment.
func (g *Grid) Join(x0, y0, x1, y1 int) {
	switch {
	case x1 == x0+1 && y1 == y0:
		g.addBits(x0, y0, Right)
		g.addBits(x1, y1, Left)
	case x1 == x0-1 && y1 == y0:
		g.addBits(x0, y0, Left)
		g.addBits(x1, y1, Right)
	case y1 == y0+1 && x1 == x0:
		g.addBits(x0, y0, Down)
		g.addBits(x1, y1, Up)
	case y1 == y0-1 && x1 == x0:
		g.addBits(x0, y0, Up)
		g.addBits(x1, y1, Down)
	}
}

// TrimLeft removes the indentation common to all non-empty lines.
func TrimLeft(lines []string) []string {
	cut := -1
	for _, l := range lines {
		if l == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " "))
		if cut < 0 || n < cut {
			cut = n
		}
	}
	if cut <= 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if len(l) >= cut {
			out[i] = l[cut:]
		}
	}
	return out
}

// HLine draws a horizontal line between x0 and x1 inclusive.
func (g *Grid) HLine(x0, x1, y int) {
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	for x := x0; x <= x1; x++ {
		var b uint8
		if x > x0 {
			b |= Left
		}
		if x < x1 {
			b |= Right
		}
		g.addBits(x, y, b)
	}
}

// VLine draws a vertical line between y0 and y1 inclusive.
func (g *Grid) VLine(x, y0, y1 int) {
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	for y := y0; y <= y1; y++ {
		var b uint8
		if y > y0 {
			b |= Up
		}
		if y < y1 {
			b |= Down
		}
		g.addBits(x, y, b)
	}
}

// Box draws a rectangle outline and clears its interior. Corner runes may
// be given to override the default square corners (0 keeps them).
func (g *Grid) Box(x, y, w, h int, corners [4]rune) {
	if w < 2 || h < 2 {
		return
	}
	g.Clear(x+1, y+1, w-2, h-2)
	g.HLine(x, x+w-1, y)
	g.HLine(x, x+w-1, y+h-1)
	g.VLine(x, y, y+h-1)
	g.VLine(x+w-1, y, y+h-1)
	pts := [4][2]int{{x, y}, {x + w - 1, y}, {x, y + h - 1}, {x + w - 1, y + h - 1}}
	for i, c := range corners {
		if c != 0 {
			g.Set(pts[i][0], pts[i][1], c)
		}
	}
}

// Rounded corners for Box.
var Rounded = [4]rune{'╭', '╮', '╰', '╯'}

// Slanted corners for Box (decision shapes).
var Slanted = [4]rune{'/', '\\', '\\', '/'}

func boxRune(b uint8) rune {
	switch b {
	case Up | Down, Up, Down:
		return '│'
	case Left | Right, Left, Right:
		return '─'
	case Down | Right:
		return '┌'
	case Down | Left:
		return '┐'
	case Up | Right:
		return '└'
	case Up | Left:
		return '┘'
	case Up | Down | Right:
		return '├'
	case Up | Down | Left:
		return '┤'
	case Down | Left | Right:
		return '┬'
	case Up | Left | Right:
		return '┴'
	case Up | Down | Left | Right:
		return '┼'
	}
	return ' '
}

// HeavyVLine turns the plain vertical line cells of column x between rows
// y0 and y1 into heavy ones (┃), leaving junctions and text alone.
func (g *Grid) HeavyVLine(x, y0, y1 int) {
	for y := y0; y <= y1; y++ {
		g.Restyle(x, y, '━', '┃')
	}
}

// Restyle draws the line through (x, y) with h when it runs straight
// across or v when it runs straight down; junctions, corners and text are
// left alone. It turns lines dashed (┄ ┆) or heavy (━ ┃).
func (g *Grid) Restyle(x, y int, h, v rune) {
	if !g.in(x, y) || g.runes[y][x] != 0 {
		return
	}
	switch g.bits[y][x] {
	case Left | Right:
		g.runes[y][x] = h
	case Up | Down:
		g.runes[y][x] = v
	}
}

// Lines renders the grid with trailing spaces and blank trailing rows
// removed.
func (g *Grid) Lines() []string {
	out := make([]string, 0, g.H)
	for y := 0; y < g.H; y++ {
		var b strings.Builder
		for x := 0; x < g.W; x++ {
			r := g.runes[y][x]
			switch {
			case r == wideCont:
				continue
			case r != 0:
				b.WriteRune(r)
				b.WriteString(g.marks[y*g.W+x])
			case g.bits[y][x] != 0:
				b.WriteRune(boxRune(g.bits[y][x]))
			default:
				b.WriteByte(' ')
			}
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	return out
}

// RuneWidth returns the number of cells r occupies: 0 for control,
// format and combining characters (Text keeps the combining ones with the
// character before them and drops the others), 2 for East Asian Wide
// and Fullwidth characters, 1 otherwise.
//
//go:generate go run gen_width.go
func RuneWidth(r rune) int {
	switch {
	case r < 0x20 || (r >= 0x7f && r < 0xa0), richtext.IsMarker(r):
		return 0
	case r < 0x300:
		return 1
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Zl, unicode.Zp):
		return 0
	case r < wideRanges[0][0]:
		return 1
	}
	lo, hi := 0, len(wideRanges)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		switch {
		case r < wideRanges[m][0]:
			hi = m
		case r > wideRanges[m][1]:
			lo = m + 1
		default:
			return 2
		}
	}
	return 1
}

// Sanitize replaces characters that would move the terminal cursor or start
// an escape sequence (C0/C1 controls other than '\n', U+2028, U+2029) with a
// space. '\n' is kept because callers use it as the line separator.
func Sanitize(s string) string {
	s = richtext.Strip(s)
	if strings.IndexFunc(s, isUnsafe) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		if isUnsafe(r) {
			return ' '
		}
		return r
	}, s)
}

func isUnsafe(r rune) bool {
	return r != '\n' && (unicode.IsControl(r) || r == 0x2028 || r == 0x2029 || isBidi(r))
}

// isBidi reports whether r is a bidirectional formatting character. A
// terminal that implements bidi reverses the rest of the line after one,
// which garbles the output and can disguise text.
func isBidi(r rune) bool {
	return r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}

// Width returns the cell width of s.
func Width(s string) int {
	n := 0
	for _, r := range s {
		n += RuneWidth(r)
	}
	return n
}

// Wrap splits label into lines no wider than cols (0 = no limit). Words
// longer than cols are broken.
func Wrap(label string, cols int) []string {
	if label == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(label, "\n") {
		if cols <= 0 || Width(line) <= cols {
			out = append(out, line)
			continue
		}
		cur, curW := "", 0
		flush := func() {
			if strings.TrimSpace(cur) != "" {
				out = append(out, strings.TrimRight(cur, " "))
			}
			cur, curW = "", 0
		}
		for _, tok := range splitTokens(line) {
			tw := Width(strings.TrimRight(tok, " "))
			if curW > 0 && curW+tw > cols {
				flush()
				tok = strings.TrimLeft(tok, " ")
			}
			tokW := Width(tok)
			if tokW > cols {
				// Break an over-long token in one pass; rescanning the rest
				// for every piece would be quadratic in the token length.
				start, hw := 0, 0
				for i, r := range tok {
					rw := RuneWidth(r)
					// Always take at least one rune so that a character wider
					// than cols cannot stall the loop.
					if hw+rw > cols && hw > 0 {
						cur += tok[start:i]
						flush()
						start, hw = i, 0
					}
					hw += rw
				}
				tok, tokW = tok[start:], hw
				if tokW > cols {
					// A single character wider than cols gets a line of its own.
					cur += tok
					flush()
					tok, tokW = "", 0
				}
			}
			cur += tok
			curW += tokW
		}
		flush()
	}
	return out
}

func splitTokens(s string) []string {
	var out []string
	var b strings.Builder
	for _, r := range s {
		b.WriteRune(r)
		if r == ' ' || RuneWidth(r) == 2 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// MaxWidth returns the widest line.
func MaxWidth(lines []string) int {
	w := 0
	for _, l := range lines {
		w = max(w, Width(l))
	}
	return w
}
