// Package source splits Mermaid source text into numbered logical lines and
// provides helpers shared by all diagram parsers.
package source

import (
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/richtext"
)

// MaxInputBytes is the largest source accepted.
const MaxInputBytes = 1 << 20

// Line is one logical line of the source with its 1-based line number in
// the original text (front matter included).
type Line struct {
	No   int
	Text string // trailing white space removed, leading white space kept
}

// SyntaxError is a parse error at a specific line.
type SyntaxError struct {
	Line int
	Msg  string
}

func (e *SyntaxError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

// Errorf returns a *SyntaxError.
//
// An error argument that wraps guard.ErrTooLarge (a nesting limit inside
// a directive or attribute block, say) makes the result a TooLarge error
// instead: exceeding a limit of this library is not a syntax error.
func Errorf(line int, format string, args ...any) error {
	for _, a := range args {
		if e, ok := a.(error); ok && errors.Is(e, guard.ErrTooLarge) {
			return TooLarge(line, "%s", strings.ReplaceAll(fmt.Sprintf(format, args...), guard.ErrTooLarge.Error()+": ", ""))
		}
	}
	return &SyntaxError{Line: line, Msg: fmt.Sprintf(format, args...)}
}

// ErrUnsupported marks valid Mermaid that this library does not draw (a
// shape or construct it does not implement). The public API returns it as
// mermaid.ErrUnsupported: the author of the diagram cannot fix it, so it
// is not a syntax error.
var ErrUnsupported = errors.New("unsupported")

// Unsupported returns an ErrUnsupported for the construct at line.
func Unsupported(line int, format string, args ...any) error {
	return fmt.Errorf("%w: line %d: %s", ErrUnsupported, line, fmt.Sprintf(format, args...))
}

// TooLarge returns a guard.ErrTooLarge for a fixed limit of this library
// (nesting depth, column count and so on) exceeded at line. Valid Mermaid
// that this library cannot draw is not a syntax error.
func TooLarge(line int, format string, args ...any) error {
	return fmt.Errorf("%w: line %d: %s", guard.ErrTooLarge, line, fmt.Sprintf(format, args...))
}

// Split returns the lines of the diagram without front matter,
// directives, comments and blank lines. See SplitDoc.
func Split(src string) ([]Line, error) {
	d, err := SplitDoc(src)
	if err != nil {
		return nil, err
	}
	return d.Lines, nil
}

// SplitDoc removes front matter, %%{...}%% directives, %% comments and
// blank lines, and returns the remaining lines with the title and theme
// settings found in the front matter and the directives. A markdown string
// ("` ... `") that is not closed on its line continues on the next lines;
// the joined line keeps the number of its first line.
func SplitDoc(src string) (*Doc, error) {
	if len(src) > MaxInputBytes {
		return nil, guard.Limit("bytes of input", len(src), MaxInputBytes)
	}
	if !utf8.ValidString(src) {
		line := 1
		for i := 0; i < len(src); {
			r, n := utf8.DecodeRuneInString(src[i:])
			if r == utf8.RuneError && n == 1 {
				break
			}
			if r == '\n' {
				line++
			}
			i += n
		}
		return nil, Errorf(line, "input is not valid UTF-8")
	}
	// Editors on Windows prepend a byte order mark.
	src = strings.TrimPrefix(src, "\ufeff")
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	src = strings.Map(inputRune, src)
	src = composeMarks(src)
	raw := strings.Split(src, "\n")

	d := &Doc{}
	i := 0
	// Front matter must be the first non-blank line.
	for i < len(raw) && strings.TrimSpace(raw[i]) == "" {
		i++
	}
	if i < len(raw) && strings.TrimSpace(raw[i]) == "---" {
		start := i
		i++
		for i < len(raw) && strings.TrimSpace(raw[i]) != "---" {
			i++
		}
		if i == len(raw) {
			return nil, Errorf(start+1, "unterminated front matter")
		}
		fm, err := parseFrontMatter(raw[start+1:i], start+2)
		if err != nil {
			return nil, err
		}
		if t, ok := fm["title"].(string); ok {
			d.Title = Label(t)
		}
		if cfg, ok := fm["config"].(map[string]any); ok {
			d.applyConfig(cfg)
		}
		i++
	}
	// joinMarkdown is set from the header: only diagram types with
	// markdown strings continue them over lines.
	joinMarkdown := false
	var directive strings.Builder
	inDirective := false
	directiveStart := 0
	endDirective := func() error {
		body := strings.TrimSpace(directive.String())
		directive.Reset()
		body = strings.TrimSuffix(strings.TrimPrefix(body, "%%{"), "}%%")
		cfg, err := parseDirective(body)
		if err != nil {
			return Errorf(directiveStart, "directive: %v", err)
		}
		for _, k := range []string{"init", "initialize"} {
			if m, ok := cfg[k].(map[string]any); ok {
				d.applyConfig(m)
			}
		}
		return nil
	}
	for ; i < len(raw); i++ {
		t := strings.TrimRightFunc(raw[i], unicode.IsSpace)
		// A tab after the indentation separates words like a space
		// ("section\tS"); keywords are cut at spaces. The indentation keeps
		// its tabs, which count for mindmap levels.
		if body := strings.TrimLeft(t, " \t"); strings.Contains(body, "\t") {
			t = t[:len(t)-len(body)] + strings.ReplaceAll(body, "\t", " ")
		}
		trim := strings.TrimSpace(t)
		if inDirective {
			directive.WriteString(trim + "\n")
			if strings.Contains(trim, "}%%") {
				inDirective = false
				if err := endDirective(); err != nil {
					return nil, err
				}
			}
			continue
		}
		if strings.HasPrefix(trim, "%%{") {
			directiveStart = i + 1
			directive.WriteString(trim + "\n")
			if !strings.Contains(trim, "}%%") {
				inDirective = true
			} else if err := endDirective(); err != nil {
				return nil, err
			}
			continue
		}
		if trim == "" || strings.HasPrefix(trim, "%%") {
			continue
		}
		no := i + 1
		if len(d.Lines) == 0 {
			kw, _, _ := strings.Cut(strings.TrimSpace(t), " ")
			joinMarkdown = markdownTypes[strings.TrimSuffix(kw, ";")]
		} else if rest, ok := strings.CutPrefix(trim, "accDescr"); ok && strings.HasPrefix(strings.TrimSpace(rest), "{") {
			// The multi-line form "accDescr {" ... "}" becomes the one-line
			// form "accDescr: ...", which every diagram type accepts (and
			// ignores, as it does not change the drawing).
			body := strings.TrimPrefix(strings.TrimSpace(rest), "{")
			var parts []string
			for {
				if k := strings.IndexByte(body, '}'); k >= 0 {
					parts = append(parts, strings.TrimSpace(body[:k]))
					break
				}
				parts = append(parts, strings.TrimSpace(body))
				if i+1 >= len(raw) {
					return nil, Errorf(no, "unterminated accDescr")
				}
				i++
				body = raw[i]
			}
			indent := t[:len(t)-len(strings.TrimLeftFunc(t, unicode.IsSpace))]
			d.Lines = append(d.Lines, Line{No: no, Text: indent + "accDescr: " + strings.Join(strings.Fields(strings.Join(parts, " ")), " ")})
			continue
		}
		if joinMarkdown && openMarkdown(t) {
			// Count the quotes of each appended line only: "` and `" cannot
			// span the "\n" between lines, and recounting (and copying) the
			// joined text for every line would be quadratic.
			open := strings.Count(t, "\"`") - strings.Count(t, "`\"")
			var b strings.Builder
			b.WriteString(t)
			for open > 0 {
				if i+1 >= len(raw) {
					return nil, Errorf(no, "unterminated markdown string")
				}
				i++
				next := strings.TrimSpace(raw[i])
				open += strings.Count(next, "\"`") - strings.Count(next, "`\"")
				b.WriteString("\n")
				b.WriteString(next)
			}
			t = b.String()
		}
		d.Lines = append(d.Lines, Line{No: no, Text: t})
	}
	if inDirective {
		return nil, Errorf(directiveStart, "unterminated directive")
	}
	return d, nil
}

// markdownTypes are the header keywords of the diagram types whose labels
// can be markdown strings ("` ... `"). In the others "` is plain text and
// must not swallow the following lines.
var markdownTypes = map[string]bool{"graph": true, "flowchart": true, "flowchart-v2": true,
	"flowchart-elk": true, "mindmap": true}

// openMarkdown reports whether t has more markdown string openers ("`)
// than closers (`").
func openMarkdown(t string) bool {
	return strings.Count(t, "\"`") > strings.Count(t, "`\"")
}

// controlToSpace replaces control and line/paragraph separator characters
// with a space so they cannot reach the output (they would move the terminal
// cursor or start escape sequences). '\n' separates lines and '\t' counts
// for indentation, so both are kept here.
//
// Format characters (zero-width space, BOM, joiners, bidi controls) and
// variation selectors are invisible and dropped; other spaces (EM SPACE, NO-BREAK SPACE) become a
// space. Fonts often lack glyphs for them, and one of them made Render
// return ErrUnsupported for the whole diagram.
func controlToSpace(r rune) rune {
	if r != '\n' && r != '\t' && (unicode.IsControl(r) || r == 0x2028 || r == 0x2029) {
		return ' '
	}
	if r >= 0x80 && unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Variation_Selector, r) {
		// Variation selectors (U+FE0F after ⚠ for the emoji form) only
		// choose a presentation; the character is drawn without them.
		return -1
	}
	if r >= 0x80 && unicode.Is(unicode.Zs, r) {
		return ' '
	}
	return r
}

// inputRune is controlToSpace that also replaces style markers
// (internal/richtext): in the input they are text, never markup.
func inputRune(r rune) rune {
	if richtext.IsMarker(r) {
		return ' '
	}
	return controlToSpace(r)
}

// composeMarks composes each character followed by combining marks (NFC).
// Text pasted from macOS is often decomposed: e + U+0301 for é. The fonts
// have é but no combining marks, so the whole diagram was ErrUnsupported.
// Only those sequences change: NFC of the whole text would also replace
// CJK compatibility ideographs (U+F900...), which people write on purpose.
func composeMarks(s string) string {
	if !strings.ContainsFunc(s, isMark) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		_, n := utf8.DecodeRuneInString(s)
		end := n
		for end < len(s) {
			r, m := utf8.DecodeRuneInString(s[end:])
			if !isMark(r) {
				break
			}
			end += m
		}
		if end > n {
			b.WriteString(norm.NFC.String(s[:end]))
		} else {
			b.WriteString(s[:n])
		}
		s = s[end:]
	}
	return b.String()
}

func isMark(r rune) bool { return unicode.Is(unicode.Mn, r) }

// Header splits the first line into its keyword and the rest.
func Header(lines []Line) (keyword, rest string) {
	if len(lines) == 0 {
		return "", ""
	}
	t := strings.TrimSpace(lines[0].Text)
	end := strings.IndexFunc(t, func(r rune) bool { return unicode.IsSpace(r) || r == ';' || r == ':' })
	if end < 0 {
		return t, ""
	}
	return t[:end], strings.TrimSpace(t[end:])
}

var (
	entityRe = regexp.MustCompile(`&?#([A-Za-z]+|[0-9]+);`)
	brRe     = regexp.MustCompile(`(?i)<br\s*/?>`)
)

// Label normalizes a label as written in the source: it strips surrounding
// quotes, decodes entities, turns <br> into line breaks, and turns inline
// formatting (<b>, <i>, <u>, and **bold** and *italic* in markdown strings)
// into style markers (internal/richtext).
func Label(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	markdown := false
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		s = s[1 : len(s)-1]
		markdown = true
	}
	s = brRe.ReplaceAllString(s, "\n")
	// Markup runs before entities are decoded, so that an escaped tag
	// (#lt;b#gt;) stays text.
	s = richtext.Markup(s, markdown)
	// NOTE(mermaid-compat): Mermaid writes entities as #quot; or #35;.
	// The HTML form &#35; is accepted as well.
	s = entityRe.ReplaceAllStringFunc(s, func(m string) string {
		m = strings.TrimPrefix(m, "&")
		if m[1] >= '0' && m[1] <= '9' {
			return noMarkers(html.UnescapeString("&#" + m[1:]))
		}
		return noMarkers(html.UnescapeString("&" + m[1:]))
	})
	s = unescapeKeepingMarkers(s)
	s = strings.ReplaceAll(s, `\n`, "\n")
	// Entities such as #13; or #27; decode to control characters.
	s = strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		return controlToSpace(r)
	}, s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.Join(lines, "\n")
}

// Indent returns the visual indentation width of s (tab = 4 columns).
func Indent(s string) int {
	n := 0
	for _, r := range s {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// CutKeyword reports whether t starts with keyword followed by white space or
// end of string, and returns the remainder.
func CutKeyword(t, keyword string) (string, bool) {
	if !strings.HasPrefix(t, keyword) {
		return "", false
	}
	rest := t[len(keyword):]
	if rest == "" {
		return "", true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	if !unicode.IsSpace(r) {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// noMarkers replaces style markers that an entity decoded to.
func noMarkers(s string) string {
	return strings.Map(func(r rune) rune {
		if richtext.IsMarker(r) {
			return ' '
		}
		return r
	}, s)
}

// unescapeKeepingMarkers decodes HTML entities in the text between the
// markers that Markup produced, so that an entity cannot become a marker.
func unescapeKeepingMarkers(s string) string {
	var b strings.Builder
	start := 0
	for i, r := range s {
		if richtext.IsMarker(r) {
			b.WriteString(noMarkers(html.UnescapeString(s[start:i])))
			b.WriteRune(r)
			start = i + utf8.RuneLen(r)
		}
	}
	b.WriteString(noMarkers(html.UnescapeString(s[start:])))
	return b.String()
}
