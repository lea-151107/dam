// Package textmeasure measures and wraps label text with a font.Face,
// returning sizes in layout units (pixels at Scale = 1).
package textmeasure

import (
	"strings"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/lea/mermaid/internal/outline"
	"github.com/lea/mermaid/internal/richtext"
)

// BaseFontSizePx is the built-in font size at Scale = 1.
const BaseFontSizePx = 14

// Faces holds one face per text style, indexed by the style's bold and
// italic bits: Regular, Bold, Italic, BoldItalic. Underline uses the face
// of the other bits.
type Faces [4]font.Face

// Uniform returns Faces that use f for every style.
func Uniform(f font.Face) Faces { return Faces{f, f, f, f} }

// For returns the face for style st.
func (fs Faces) For(st richtext.Style) font.Face { return fs[st&(richtext.Bold|richtext.Italic)] }

// Measurer measures text. It is not safe for concurrent use because
// font.Face implementations are not.
type Measurer struct {
	faces   Faces
	scale   float64 // device px per layout unit
	lineH   float64 // layout units
	ascent  float64 // layout units
	stop    func() bool
	stopped bool // stop reported true; set by Width
	runes   int  // characters measured, for the stop checks
}

// stopCheckRunes is how many characters Width measures (over all calls)
// between calls to the stop function set with SetStop.
const stopCheckRunes = 4096

// SetStop makes Width give up early once stop reports true, so that a very
// long label cannot keep a canceled Render busy. The widths returned after
// that are meaningless; the caller must check for cancellation before using
// the layout.
func (m *Measurer) SetStop(stop func() bool) { m.stop = stop }

// New returns a Measurer for faces, which render at scale device pixels
// per layout unit. Line metrics come from the regular face.
func New(faces Faces, scale float64) *Measurer {
	m := faces[0].Metrics()
	h := toF(m.Height)
	if h <= 0 {
		h = toF(m.Ascent + m.Descent)
	}
	return &Measurer{faces: faces, scale: scale, lineH: float64(h / scale), ascent: float64(toF(m.Ascent) / scale)}
}

// builtinTTFs are the embedded Go fonts by style index.
var builtinTTFs = [4][]byte{goregular.TTF, gobold.TTF, goitalic.TTF, gobolditalic.TTF}

// BuiltinFaces returns the embedded Go fonts (Regular, Bold, Italic, Bold
// Italic) at sizePx device pixels. Their glyphs are drawn by
// internal/outline so that they are the same on every architecture.
func BuiltinFaces(sizePx float64) (Faces, error) {
	var fs Faces
	for i, ttf := range builtinTTFs {
		f, err := opentype.Parse(ttf)
		if err != nil {
			return Faces{}, err
		}
		if fs[i], err = outline.New(f, sizePx); err != nil {
			return Faces{}, err
		}
	}
	return fs, nil
}

// BuiltinFace returns the embedded Go Regular font at sizePx device pixels.
func BuiltinFace(sizePx float64) (font.Face, error) {
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}
	return outline.New(f, sizePx)
}

func toF(v fixed.Int26_6) float64 { return float64(float64(v) / 64) }

// Scale returns device pixels per layout unit.
func (m *Measurer) Scale() float64 { return m.scale }

// LineHeight returns the height of one line.
func (m *Measurer) LineHeight() float64 { return m.lineH }

// Ascent returns the font ascent.
func (m *Measurer) Ascent() float64 { return m.ascent }

// Width returns the advance width of a single line, measuring each style
// run with its face.
func (m *Measurer) Width(s string) float64 {
	var w fixed.Int26_6
	if m.stopped {
		return 0
	}
	for _, r := range richtext.Runs(s) {
		// The same sum as font.MeasureString, with a cancellation check.
		f := m.faces.For(r.Style)
		prev := rune(-1)
		for _, c := range r.Text {
			if prev >= 0 {
				w += f.Kern(prev, c)
			}
			a, _ := f.GlyphAdvance(c)
			w += a
			prev = c
			if m.runes++; m.runes%stopCheckRunes == 0 && m.stop != nil && m.stop() {
				m.stopped = true
				return float64(toF(w) / m.scale)
			}
		}
	}
	return float64(toF(w) / m.scale)
}

// Lines splits label on line breaks and wraps each line to maxW (0 = no
// wrapping). A style that is on at a break continues on the next line.
func (m *Measurer) Lines(label string, maxW float64) []string {
	if label == "" {
		return nil
	}
	var out []string
	for _, l := range richtext.Carry(strings.Split(label, "\n")) {
		if maxW <= 0 || m.Width(l) <= maxW {
			out = append(out, l)
			continue
		}
		out = append(out, richtext.Carry(m.wrap(l, maxW))...)
	}
	return out
}

// Size returns the bounding size of lines.
func (m *Measurer) Size(lines []string) (w, h float64) {
	for _, l := range lines {
		w = max(w, m.Width(l))
	}
	return w, float64(float64(len(lines)) * m.lineH)
}

func (m *Measurer) wrap(line string, maxW float64) []string {
	var out []string
	cur := ""
	for _, tok := range Tokens(line) {
		if m.stopped {
			return out
		}
		cand := cur + tok
		if cur != "" && m.Width(strings.TrimRight(cand, " ")) > maxW {
			out = append(out, strings.TrimRight(cur, " "))
			cur = strings.TrimLeft(tok, " ")
			continue
		}
		cur = cand
	}
	if strings.TrimSpace(cur) != "" {
		out = append(out, strings.TrimRight(cur, " "))
	}
	return out
}

// Tokens splits s into wrap units: words with their trailing space, and
// individual CJK characters (which may break anywhere).
func Tokens(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteRune(r)
			flush()
		case isCJK(r):
			flush()
			out = append(out, string(r))
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) ||
		(r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef)
}
