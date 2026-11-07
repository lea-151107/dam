// Package richtext carries bold, italic and underline in label strings.
//
// Labels travel through every diagram package as plain strings. A style
// change is encoded as a marker rune from the end of plane 16 (private
// use), which sets the style of the text after it until the next marker or
// the end of the line. Measuring and drawing split a line into runs; the
// text renderers treat markers as zero-width and strip them. The source
// package replaces marker runes found in the input, so that they can only
// come from markup.
package richtext

import "strings"

// Style is a set of text attributes.
type Style uint8

// Text attributes.
const (
	Bold Style = 1 << iota
	Italic
	Underline
)

// Regular is the style without attributes.
const Regular Style = 0

// base is the marker for Regular; Marker(s) is base + s.
const base rune = 0x10FFF0

// Marker returns the rune that switches to style s.
func Marker(s Style) rune { return base + rune(s&(Bold|Italic|Underline)) }

// IsMarker reports whether r is a style marker.
func IsMarker(r rune) bool { return r >= base && r <= base+rune(Bold|Italic|Underline) }

// Run is a piece of a line in one style.
type Run struct {
	Text  string
	Style Style
}

// Runs splits a line into runs. The style at the start is Regular. Empty
// runs are dropped.
func Runs(s string) []Run {
	if !strings.ContainsFunc(s, IsMarker) {
		if s == "" {
			return nil
		}
		return []Run{{Text: s}}
	}
	var out []Run
	st := Regular
	start := 0
	for i, r := range s {
		if !IsMarker(r) {
			continue
		}
		if i > start {
			out = append(out, Run{Text: s[start:i], Style: st})
		}
		st = Style(r - base)
		start = i + len(string(r))
	}
	if start < len(s) {
		out = append(out, Run{Text: s[start:], Style: st})
	}
	return out
}

// Strip removes the markers from s.
func Strip(s string) string {
	if !strings.ContainsFunc(s, IsMarker) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if IsMarker(r) {
			return -1
		}
		return r
	}, s)
}

// End returns the style in effect at the end of s, which starts in style
// st.
func End(s string, st Style) Style {
	for _, r := range s {
		if IsMarker(r) {
			st = Style(r - base)
		}
	}
	return st
}

// Carry makes each line start in the style the previous line ended in, so
// that splitting a styled line (wrapping, line breaks) keeps the style.
func Carry(lines []string) []string {
	st := Regular
	for i, l := range lines {
		if st != Regular {
			lines[i] = string(Marker(st)) + l
		}
		st = End(lines[i], Regular)
	}
	return lines
}

// Markup converts inline formatting to markers: the HTML tags b, strong,
// i, em and u, and with markdown also **bold**, __bold__, *italic* and
// _italic_. Other tags in tagNames are removed.
func Markup(s string, markdown bool) string {
	var b strings.Builder
	st := Regular
	set := func(n Style) {
		if n != st {
			st = n
			b.WriteRune(Marker(st))
		}
	}
	for i := 0; i < len(s); {
		if s[i] == '<' {
			if name, closing, n := tag(s[i:]); n > 0 {
				var bit Style
				switch name {
				case "b", "strong":
					bit = Bold
				case "i", "em":
					bit = Italic
				case "u":
					bit = Underline
				}
				if closing {
					set(st &^ bit)
				} else {
					set(st | bit)
				}
				i += n
				continue
			}
		}
		if markdown && (s[i] == '*' || s[i] == '_') {
			bit := Italic
			n := 1
			if i+1 < len(s) && s[i+1] == s[i] {
				bit, n = Bold, 2
			}
			if delimiter(s, i, n, st&bit != 0) {
				set(st ^ bit)
				i += n
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return strings.TrimSuffix(b.String(), string(Marker(Regular)))
}

// delimiter reports whether the run of n '*' or '_' at s[i] opens (or,
// when closing, closes) emphasis, as Markdown does for the common cases:
// an underscore inside a word (snake_case) is text, and an opening
// delimiter without a matching closing one later is text.
func delimiter(s string, i, n int, closing bool) bool {
	d := s[i : i+n]
	if d[0] == '_' {
		before := i > 0 && isWordByte(s[i-1])
		after := i+n < len(s) && isWordByte(s[i+n])
		if before && after {
			return false
		}
	}
	if closing {
		return true
	}
	return strings.Contains(s[i+n:], d)
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

// tagNames are the inline tags Markup understands; others stay as text.
var tagNames = map[string]bool{"b": true, "strong": true, "i": true, "em": true, "u": true,
	"small": true, "span": true, "sub": true, "sup": true}

// tag parses an inline tag at the start of s and returns its name, whether
// it closes, and its length; n is 0 when s does not start with one.
func tag(s string) (name string, closing bool, n int) {
	end := strings.IndexByte(s, '>')
	if end < 0 {
		return "", false, 0
	}
	body := s[1:end]
	if strings.HasPrefix(body, "/") {
		closing = true
		body = body[1:]
	}
	name, _, _ = strings.Cut(body, " ")
	name = strings.ToLower(strings.TrimSuffix(name, "/"))
	if !tagNames[name] {
		return "", false, 0
	}
	return name, closing, end + 1
}
