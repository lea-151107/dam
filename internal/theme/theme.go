// Package theme derives a diagram palette from a foreground and an optional
// background color.
package theme

import (
	"image/color"
	"math"
	"strconv"
	"strings"
)

// Theme holds every color used by the diagram renderers.
type Theme struct {
	Dark bool
	// Background is the canvas fill; A == 0 means transparent.
	Background color.NRGBA
	// Base is the color assumed behind the diagram, used for blending even
	// when the canvas is transparent.
	Base color.NRGBA

	Text          color.NRGBA
	Line          color.NRGBA
	NodeFill      color.NRGBA
	NodeStroke    color.NRGBA
	ClusterFill   color.NRGBA
	ClusterStroke color.NRGBA
	NoteFill      color.NRGBA
	NoteStroke    color.NRGBA
	LabelBg       color.NRGBA
	Accent        color.NRGBA
	Muted         color.NRGBA
	Critical      color.NRGBA
	Palette       []color.NRGBA
}

// paletteHues are the hues (degrees) of the categorical palette, ordered so
// that neighbours contrast.
var paletteHues = [...]float64{230, 45, 140, 300, 15, 190, 90, 260, 340, 170, 60, 210}

// New derives a Theme. A nil fg picks a default that suits bg; a nil bg
// means a transparent canvas.
func New(fg, bg color.Color) Theme { return NewWith(fg, bg, "", nil) }

// NewWith derives a Theme for a Mermaid theme name (default, neutral, dark,
// forest, base) and themeVariables. The caller's fg and bg win over both:
// they come from the terminal the diagram is shown in. Variables override
// the colors the theme derives.
//
// NOTE(mermaid-compat): unknown theme names fall back to default, and
// unknown variables or colors that do not parse are ignored, as in Mermaid.
func NewWith(fg, bg color.Color, name string, vars map[string]string) Theme {
	var t Theme
	if bg != nil {
		t.Background = toNRGBA(bg)
		t.Background.A = 255
	}
	switch {
	case fg != nil:
		t.Text = toNRGBA(fg)
		t.Text.A = 255
		t.Dark = luminance(t.Text) > 0.5
	case bg != nil:
		t.Dark = luminance(t.Background) < 0.5
	default:
		// Without colors from the caller, the dark theme picks dark mode.
		t.Dark = name == "dark"
	}
	if fg == nil {
		if t.Dark {
			t.Text = color.NRGBA{0xdd, 0xdd, 0xdd, 0xff}
		} else {
			t.Text = color.NRGBA{0x33, 0x33, 0x33, 0xff}
		}
	}
	switch {
	case bg != nil:
		t.Base = t.Background
	case t.Dark:
		t.Base = color.NRGBA{0x1e, 0x1e, 0x1e, 0xff}
	default:
		t.Base = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	}

	p := presetFor(name)
	accent := hsl(p.hue, p.sat, 0.62)
	if t.Dark {
		accent = hsl(p.hue, float64(p.sat*0.8), 0.62)
	}
	t.Accent = accent
	t.Line = Mix(t.Base, t.Text, 0.8)
	t.NodeFill = Mix(t.Base, accent, p.fill)
	t.NodeStroke = Mix(t.Base, accent, 0.9)
	t.ClusterFill = Mix(t.Base, hsl(p.clusterHue, float64(0.8*p.sat/0.55), 0.6), 0.10)
	t.ClusterStroke = Mix(t.Base, hsl(p.clusterHue, float64(0.6*p.sat/0.55), 0.45), 0.75)
	t.NoteFill = Mix(t.Base, hsl(52, 0.95, 0.65), 0.35)
	t.NoteStroke = Mix(t.Base, hsl(45, 0.7, 0.45), 0.8)
	t.LabelBg = Mix(t.Base, t.Text, 0.05)
	t.Muted = Mix(t.Base, t.Text, 0.35)
	t.Critical = Mix(t.Base, hsl(0, 0.75, 0.55), 0.85)
	for _, h := range paletteHues {
		if t.Dark {
			t.Palette = append(t.Palette, hsl(h, 0.45, 0.38))
		} else {
			t.Palette = append(t.Palette, hsl(h, 0.65, 0.78))
		}
	}
	t.applyVars(vars, fg == nil)
	return t
}

// preset is the part of a Mermaid theme that this renderer varies: the hue
// and saturation of the accent (node outlines and fills) and clusters.
type preset struct {
	hue, sat, fill, clusterHue float64
}

func presetFor(name string) preset {
	switch name {
	case "neutral":
		return preset{hue: 0, sat: 0, fill: 0.12, clusterHue: 0}
	case "forest":
		return preset{hue: 110, sat: 0.45, fill: 0.22, clusterHue: 75}
	case "base":
		return preset{hue: 38, sat: 0.85, fill: 0.30, clusterHue: 200}
	}
	// default and dark
	return preset{hue: 250, sat: 0.55, fill: 0.16, clusterHue: 55}
}

// applyVars overrides colors with Mermaid themeVariables. Text and line
// colors only apply when the caller did not choose the foreground
// (Options.Foreground is "the text and line color", chosen to show on the
// terminal; a lineColor for a light page vanished on a dark one).
func (t *Theme) applyVars(vars map[string]string, textFree bool) {
	set := func(dst *color.NRGBA, keys ...string) {
		for _, k := range keys {
			if c, ok := ParseColor(vars[k]); ok && vars[k] != "" {
				c.A = 255
				*dst = c
			}
		}
	}
	set(&t.NodeFill, "primaryColor", "mainBkg")
	set(&t.NodeStroke, "primaryBorderColor", "nodeBorder")
	set(&t.LabelBg, "secondaryColor", "edgeLabelBackground")
	set(&t.ClusterFill, "tertiaryColor", "clusterBkg")
	set(&t.ClusterStroke, "tertiaryBorderColor", "clusterBorder")
	set(&t.NoteFill, "noteBkgColor")
	set(&t.NoteStroke, "noteBorderColor")
	if textFree {
		set(&t.Text, "textColor", "primaryTextColor")
		set(&t.Line, "lineColor")
	}
}

// Color returns the i-th palette color, cycling.
func (t Theme) Color(i int) color.NRGBA {
	if i < 0 {
		i = -i
	}
	return t.Palette[i%len(t.Palette)]
}

// Strong returns a more saturated variant of palette color i, for lines.
func (t Theme) Strong(i int) color.NRGBA {
	return Mix(t.Color(i), t.Text, 0.35)
}

// Mix linearly interpolates from a to b (opaque result).
func Mix(a, b color.NRGBA, f float64) color.NRGBA {
	m := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x) + float64((float64(y)-float64(x))*f)))
	}
	return color.NRGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 255}
}

func toNRGBA(c color.Color) color.NRGBA {
	return color.NRGBAModel.Convert(c).(color.NRGBA)
}

// minTextContrast is the smallest difference in luminance between a
// label and the fill behind it that Readable keeps.
const minTextContrast = 0.3

// Readable returns text, or black or white (whichever stands out more)
// when text is too close in lightness to fill. It is for the theme's text
// color on a fill the diagram chose: the caller's Foreground suits the
// terminal background, but on a node the author filled white it vanished.
// A text color the author gave (style color:...) is used as it is.
func Readable(text, fill color.NRGBA) color.NRGBA {
	if fill.A == 0 {
		return text
	}
	lf := luminance(fill)
	if math.Abs(lf-luminance(text)) >= minTextContrast {
		return text
	}
	if lf > 0.5 {
		return color.NRGBA{A: 255}
	}
	return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
}

func luminance(c color.NRGBA) float64 {
	return float64((float64(0.2126*float64(c.R)) + float64(0.7152*float64(c.G)) + float64(0.0722*float64(c.B))) / 255)
}

func hsl(h, s, l float64) color.NRGBA {
	h = float64(math.Mod(h, 360) / 360)
	hue := func(p, q, t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + float64((q-p)*6*t)
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + float64((q-p)*(float64(2.0/3)-t)*6)
		}
		return p
	}
	var q float64
	if l < 0.5 {
		q = float64(l * (1 + s))
	} else {
		q = l + s - float64(l*s)
	}
	p := float64(2*l) - q
	r, g, b := hue(p, q, h+1.0/3), hue(p, q, h), hue(p, q, h-1.0/3)
	c := func(v float64) uint8 { return uint8(math.Round(float64(v * 255))) }
	return color.NRGBA{c(r), c(g), c(b), 255}
}

// named holds the CSS named colors (CSS Color Module Level 4).
var named = map[string]color.NRGBA{
	"aliceblue": {0xf0, 0xf8, 0xff, 255}, "antiquewhite": {0xfa, 0xeb, 0xd7, 255}, "aqua": {0x00, 0xff, 0xff, 255},
	"aquamarine": {0x7f, 0xff, 0xd4, 255}, "azure": {0xf0, 0xff, 0xff, 255}, "beige": {0xf5, 0xf5, 0xdc, 255},
	"bisque": {0xff, 0xe4, 0xc4, 255}, "black": {0x00, 0x00, 0x00, 255}, "blanchedalmond": {0xff, 0xeb, 0xcd, 255},
	"blue": {0x00, 0x00, 0xff, 255}, "blueviolet": {0x8a, 0x2b, 0xe2, 255}, "brown": {0xa5, 0x2a, 0x2a, 255},
	"burlywood": {0xde, 0xb8, 0x87, 255}, "cadetblue": {0x5f, 0x9e, 0xa0, 255}, "chartreuse": {0x7f, 0xff, 0x00, 255},
	"chocolate": {0xd2, 0x69, 0x1e, 255}, "coral": {0xff, 0x7f, 0x50, 255}, "cornflowerblue": {0x64, 0x95, 0xed, 255},
	"cornsilk": {0xff, 0xf8, 0xdc, 255}, "crimson": {0xdc, 0x14, 0x3c, 255}, "cyan": {0x00, 0xff, 0xff, 255},
	"darkblue": {0x00, 0x00, 0x8b, 255}, "darkcyan": {0x00, 0x8b, 0x8b, 255}, "darkgoldenrod": {0xb8, 0x86, 0x0b, 255},
	"darkgray": {0xa9, 0xa9, 0xa9, 255}, "darkgreen": {0x00, 0x64, 0x00, 255}, "darkgrey": {0xa9, 0xa9, 0xa9, 255},
	"darkkhaki": {0xbd, 0xb7, 0x6b, 255}, "darkmagenta": {0x8b, 0x00, 0x8b, 255}, "darkolivegreen": {0x55, 0x6b, 0x2f, 255},
	"darkorange": {0xff, 0x8c, 0x00, 255}, "darkorchid": {0x99, 0x32, 0xcc, 255}, "darkred": {0x8b, 0x00, 0x00, 255},
	"darksalmon": {0xe9, 0x96, 0x7a, 255}, "darkseagreen": {0x8f, 0xbc, 0x8f, 255}, "darkslateblue": {0x48, 0x3d, 0x8b, 255},
	"darkslategray": {0x2f, 0x4f, 0x4f, 255}, "darkslategrey": {0x2f, 0x4f, 0x4f, 255}, "darkturquoise": {0x00, 0xce, 0xd1, 255},
	"darkviolet": {0x94, 0x00, 0xd3, 255}, "deeppink": {0xff, 0x14, 0x93, 255}, "deepskyblue": {0x00, 0xbf, 0xff, 255},
	"dimgray": {0x69, 0x69, 0x69, 255}, "dimgrey": {0x69, 0x69, 0x69, 255}, "dodgerblue": {0x1e, 0x90, 0xff, 255},
	"firebrick": {0xb2, 0x22, 0x22, 255}, "floralwhite": {0xff, 0xfa, 0xf0, 255}, "forestgreen": {0x22, 0x8b, 0x22, 255},
	"fuchsia": {0xff, 0x00, 0xff, 255}, "gainsboro": {0xdc, 0xdc, 0xdc, 255}, "ghostwhite": {0xf8, 0xf8, 0xff, 255},
	"gold": {0xff, 0xd7, 0x00, 255}, "goldenrod": {0xda, 0xa5, 0x20, 255}, "gray": {0x80, 0x80, 0x80, 255},
	"green": {0x00, 0x80, 0x00, 255}, "greenyellow": {0xad, 0xff, 0x2f, 255}, "grey": {0x80, 0x80, 0x80, 255},
	"honeydew": {0xf0, 0xff, 0xf0, 255}, "hotpink": {0xff, 0x69, 0xb4, 255}, "indianred": {0xcd, 0x5c, 0x5c, 255},
	"indigo": {0x4b, 0x00, 0x82, 255}, "ivory": {0xff, 0xff, 0xf0, 255}, "khaki": {0xf0, 0xe6, 0x8c, 255},
	"lavender": {0xe6, 0xe6, 0xfa, 255}, "lavenderblush": {0xff, 0xf0, 0xf5, 255}, "lawngreen": {0x7c, 0xfc, 0x00, 255},
	"lemonchiffon": {0xff, 0xfa, 0xcd, 255}, "lightblue": {0xad, 0xd8, 0xe6, 255}, "lightcoral": {0xf0, 0x80, 0x80, 255},
	"lightcyan": {0xe0, 0xff, 0xff, 255}, "lightgoldenrodyellow": {0xfa, 0xfa, 0xd2, 255}, "lightgray": {0xd3, 0xd3, 0xd3, 255},
	"lightgreen": {0x90, 0xee, 0x90, 255}, "lightgrey": {0xd3, 0xd3, 0xd3, 255}, "lightpink": {0xff, 0xb6, 0xc1, 255},
	"lightsalmon": {0xff, 0xa0, 0x7a, 255}, "lightseagreen": {0x20, 0xb2, 0xaa, 255}, "lightskyblue": {0x87, 0xce, 0xfa, 255},
	"lightslategray": {0x77, 0x88, 0x99, 255}, "lightslategrey": {0x77, 0x88, 0x99, 255}, "lightsteelblue": {0xb0, 0xc4, 0xde, 255},
	"lightyellow": {0xff, 0xff, 0xe0, 255}, "lime": {0x00, 0xff, 0x00, 255}, "limegreen": {0x32, 0xcd, 0x32, 255},
	"linen": {0xfa, 0xf0, 0xe6, 255}, "magenta": {0xff, 0x00, 0xff, 255}, "maroon": {0x80, 0x00, 0x00, 255},
	"mediumaquamarine": {0x66, 0xcd, 0xaa, 255}, "mediumblue": {0x00, 0x00, 0xcd, 255}, "mediumorchid": {0xba, 0x55, 0xd3, 255},
	"mediumpurple": {0x93, 0x70, 0xdb, 255}, "mediumseagreen": {0x3c, 0xb3, 0x71, 255}, "mediumslateblue": {0x7b, 0x68, 0xee, 255},
	"mediumspringgreen": {0x00, 0xfa, 0x9a, 255}, "mediumturquoise": {0x48, 0xd1, 0xcc, 255}, "mediumvioletred": {0xc7, 0x15, 0x85, 255},
	"midnightblue": {0x19, 0x19, 0x70, 255}, "mintcream": {0xf5, 0xff, 0xfa, 255}, "mistyrose": {0xff, 0xe4, 0xe1, 255},
	"moccasin": {0xff, 0xe4, 0xb5, 255}, "navajowhite": {0xff, 0xde, 0xad, 255}, "navy": {0x00, 0x00, 0x80, 255},
	"oldlace": {0xfd, 0xf5, 0xe6, 255}, "olive": {0x80, 0x80, 0x00, 255}, "olivedrab": {0x6b, 0x8e, 0x23, 255},
	"orange": {0xff, 0xa5, 0x00, 255}, "orangered": {0xff, 0x45, 0x00, 255}, "orchid": {0xda, 0x70, 0xd6, 255},
	"palegoldenrod": {0xee, 0xe8, 0xaa, 255}, "palegreen": {0x98, 0xfb, 0x98, 255}, "paleturquoise": {0xaf, 0xee, 0xee, 255},
	"palevioletred": {0xdb, 0x70, 0x93, 255}, "papayawhip": {0xff, 0xef, 0xd5, 255}, "peachpuff": {0xff, 0xda, 0xb9, 255},
	"peru": {0xcd, 0x85, 0x3f, 255}, "pink": {0xff, 0xc0, 0xcb, 255}, "plum": {0xdd, 0xa0, 0xdd, 255},
	"powderblue": {0xb0, 0xe0, 0xe6, 255}, "purple": {0x80, 0x00, 0x80, 255}, "rebeccapurple": {0x66, 0x33, 0x99, 255},
	"red": {0xff, 0x00, 0x00, 255}, "rosybrown": {0xbc, 0x8f, 0x8f, 255}, "royalblue": {0x41, 0x69, 0xe1, 255},
	"saddlebrown": {0x8b, 0x45, 0x13, 255}, "salmon": {0xfa, 0x80, 0x72, 255}, "sandybrown": {0xf4, 0xa4, 0x60, 255},
	"seagreen": {0x2e, 0x8b, 0x57, 255}, "seashell": {0xff, 0xf5, 0xee, 255}, "sienna": {0xa0, 0x52, 0x2d, 255},
	"silver": {0xc0, 0xc0, 0xc0, 255}, "skyblue": {0x87, 0xce, 0xeb, 255}, "slateblue": {0x6a, 0x5a, 0xcd, 255},
	"slategray": {0x70, 0x80, 0x90, 255}, "slategrey": {0x70, 0x80, 0x90, 255}, "snow": {0xff, 0xfa, 0xfa, 255},
	"springgreen": {0x00, 0xff, 0x7f, 255}, "steelblue": {0x46, 0x82, 0xb4, 255}, "tan": {0xd2, 0xb4, 0x8c, 255},
	"teal": {0x00, 0x80, 0x80, 255}, "thistle": {0xd8, 0xbf, 0xd8, 255}, "tomato": {0xff, 0x63, 0x47, 255},
	"turquoise": {0x40, 0xe0, 0xd0, 255}, "violet": {0xee, 0x82, 0xee, 255}, "wheat": {0xf5, 0xde, 0xb3, 255},
	"white": {0xff, 0xff, 0xff, 255}, "whitesmoke": {0xf5, 0xf5, 0xf5, 255}, "yellow": {0xff, 0xff, 0x00, 255},
	"yellowgreen": {0x9a, 0xcd, 0x32, 255},
	"transparent": {0, 0, 0, 0},
}

// ParseColor parses #rgb, #rrggbb, #rrggbbaa, rgb(r,g,b), rgba(r,g,b,a) and
// a few CSS color names.
func ParseColor(s string) (color.NRGBA, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if c, ok := named[s]; ok {
		return c, true
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 || len(h) == 4 {
			var b strings.Builder
			for _, r := range h {
				b.WriteRune(r)
				b.WriteRune(r)
			}
			h = b.String()
		}
		if len(h) != 6 && len(h) != 8 {
			return color.NRGBA{}, false
		}
		v, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return color.NRGBA{}, false
		}
		if len(h) == 6 {
			return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, true
		}
		return color.NRGBA{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
	}
	for _, fn := range []string{"rgba(", "rgb("} {
		if strings.HasPrefix(s, fn) && strings.HasSuffix(s, ")") {
			parts := strings.Split(s[len(fn):len(s)-1], ",")
			if len(parts) < 3 {
				return color.NRGBA{}, false
			}
			var v [4]float64
			v[3] = 1
			for i := 0; i < len(parts) && i < 4; i++ {
				f, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
				if err != nil {
					return color.NRGBA{}, false
				}
				v[i] = f
			}
			cl := func(f float64) uint8 { return uint8(math.Max(0, math.Min(255, math.Round(f)))) }
			return color.NRGBA{cl(v[0]), cl(v[1]), cl(v[2]), cl(float64(v[3] * 255))}, true
		}
	}
	return color.NRGBA{}, false
}

// Style is a subset of the CSS properties accepted by classDef and style.
type Style struct {
	Fill, Stroke, Color *color.NRGBA
	StrokeWidth         float64 // 0 = unset
	Dashed              bool
}

// ParseStyle parses "fill:#f9f,stroke:#333,stroke-width:4px". Unknown
// properties are ignored because they only affect presentation.
func ParseStyle(s string) Style {
	var st Style
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' }) {
		k, v, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(strings.ToLower(k))
		v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
		switch k {
		case "fill", "background", "background-color":
			if c, ok := ParseColor(v); ok {
				st.Fill = &c
			}
		case "stroke", "border-color":
			if c, ok := ParseColor(v); ok {
				st.Stroke = &c
			}
		case "color":
			if c, ok := ParseColor(v); ok {
				st.Color = &c
			}
		case "stroke-width":
			if f, err := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64); err == nil && f >= 0 && f <= 20 {
				st.StrokeWidth = f
			}
		case "stroke-dasharray":
			st.Dashed = v != "" && v != "none" && v != "0"
		}
	}
	return st
}

// Merge overlays o on s.
func (s Style) Merge(o Style) Style {
	if o.Fill != nil {
		s.Fill = o.Fill
	}
	if o.Stroke != nil {
		s.Stroke = o.Stroke
	}
	if o.Color != nil {
		s.Color = o.Color
	}
	if o.StrokeWidth != 0 {
		s.StrokeWidth = o.StrokeWidth
	}
	if o.Dashed {
		s.Dashed = true
	}
	return s
}
