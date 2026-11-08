package source

import (
	"fmt"
	"github.com/lea-151107/dam/internal/guard"
	"strconv"
	"strings"
	"unicode"
)

// Doc is a split source: the diagram's lines and the settings given in
// front matter and %%{init: ...}%% directives.
type Doc struct {
	Lines []Line
	// Title is the front matter title ("" = none).
	Title string
	// Theme is the Mermaid theme name ("" = default).
	Theme string
	// Vars holds themeVariables (for example primaryColor) as strings.
	Vars map[string]string
	// MainBranch is gitGraph.mainBranchName when set to other than "main".
	// The renderer does not rename the main branch; the setting makes
	// "checkout <name>" valid, so it is reported as unsupported rather than
	// left to fail as an unknown branch.
	MainBranch string
}

// applyConfig copies theme settings from a parsed configuration object.
// Later settings win, as with several directives in Mermaid.
func (d *Doc) applyConfig(cfg map[string]any) {
	if s, ok := cfg["theme"].(string); ok {
		d.Theme = strings.TrimSpace(s)
	}
	if gg, ok := cfg["gitGraph"].(map[string]any); ok {
		if s, ok := gg["mainBranchName"].(string); ok {
			d.MainBranch = ""
			if s = strings.TrimSpace(s); s != "main" {
				d.MainBranch = s
			}
		}
	}
	if vars, ok := cfg["themeVariables"].(map[string]any); ok {
		if d.Vars == nil {
			d.Vars = map[string]string{}
		}
		for k, v := range vars {
			switch v := v.(type) {
			case string:
				d.Vars[k] = v
			case float64:
				d.Vars[k] = strconv.FormatFloat(v, 'g', -1, 64)
			}
		}
	}
}

// parseFrontMatter reads the subset of YAML used in Mermaid front matter:
// nested mappings of scalars by indentation, block scalars (| and >,
// with - or +) and flow collections ({theme: dark}, [a, b]), which may
// span lines. Other constructs (lists, anchors) are skipped.
//
// firstLine is the 1-based source line of lines[0], for errors. A flow
// collection that is never closed is an error, as in Mermaid (js-yaml);
// it would otherwise swallow the keys after it.
//
// NOTE(mermaid-compat): only title and config.theme/themeVariables take
// effect; other front matter settings are ignored.
func parseFrontMatter(lines []string, firstLine int) (map[string]any, error) {
	root := map[string]any{}
	type level struct {
		indent int
		m      map[string]any
	}
	stack := []level{{-1, root}}
	for i := 0; i < len(lines); i++ {
		t := strings.TrimRightFunc(lines[i], unicode.IsSpace)
		trim := strings.TrimSpace(t)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, "- ") || trim == "-" {
			continue
		}
		indent := Indent(t)
		key, val, ok := strings.Cut(trim, ":")
		if !ok {
			continue
		}
		for len(stack) > 1 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		key = unquote(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		cur := stack[len(stack)-1].m
		switch {
		case val == "":
			m := map[string]any{}
			cur[key] = m
			stack = append(stack, level{indent, m})
			continue
		case strings.HasPrefix(val, "|") || strings.HasPrefix(val, ">"):
			// Block scalar: the more indented lines that follow.
			var body []string
			for i+1 < len(lines) {
				next := strings.TrimRightFunc(lines[i+1], unicode.IsSpace)
				if strings.TrimSpace(next) != "" && Indent(next) <= indent {
					break
				}
				body = append(body, strings.TrimSpace(next))
				i++
			}
			for len(body) > 0 && body[len(body)-1] == "" {
				body = body[:len(body)-1]
			}
			sep := "\n"
			if val[0] == '>' {
				sep = " "
			}
			cur[key] = strings.Join(body, sep)
			continue
		case val[0] == '{' || val[0] == '[':
			// Flow collection, up to its closing bracket. Comments are
			// removed first: a bracket in one ("{a: 1} # [x") is not part
			// of the collection.
			val = stripComment(val)
			text := val
			first := i
			sc := newFlowScan()
			sc.feed(text)
			if !sc.balanced() {
				var b strings.Builder
				b.WriteString(text)
				for !sc.balanced() && i+1 < len(lines) {
					i++
					more := " " + stripComment(strings.TrimSpace(lines[i]))
					sc.feed(more)
					b.WriteString(more)
				}
				text = b.String()
			}
			if !sc.balanced() {
				return nil, Errorf(firstLine+first, "unclosed flow collection in front matter")
			}
			// A value that is not a whole flow collection ("[Draft] Plan")
			// is a plain string, as YAML reads it.
			p := &json5{s: text, bare: true}
			v, err := p.value(0)
			p.space()
			if err == nil && p.i == len(p.s) {
				cur[key] = v
			} else if text == val {
				cur[key] = unquote(val)
			}
			continue
		}
		if i := strings.Index(val, " #"); i >= 0 && val[0] != '"' && val[0] != '\'' {
			val = strings.TrimSpace(val[:i])
		}
		cur[key] = unquote(val)
	}
	return root, nil
}

// stripComment removes a YAML comment from a line of a flow collection: a
// # at the start or after a space, outside quotes. Quotes are recognized
// as in balanced.
func stripComment(s string) string {
	var quote byte
	prev := byte('{')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
			continue
		case (c == '"' || c == '\'') && strings.IndexByte("{[,:", prev) >= 0:
			quote = c
		case c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return strings.TrimSpace(s[:i])
		}
		if c != ' ' && c != '\t' {
			prev = c
		}
	}
	return s
}

// balanced reports whether the brackets of a flow collection are closed
// (outside quotes). A quote starts a string only where a value starts,
// after { [ , or :, so that a bare value like Bob's does not.
func balanced(s string) bool {
	sc := newFlowScan()
	sc.feed(s)
	return sc.balanced()
}

// flowScan is the state of balanced over text fed piece by piece, so that
// a collection continued over many lines is scanned once, not again for
// every line.
type flowScan struct {
	depth int
	quote byte
	prev  byte // last non-space character outside strings
}

func newFlowScan() flowScan { return flowScan{prev: '{'} }

func (sc *flowScan) balanced() bool { return sc.depth <= 0 }

func (sc *flowScan) feed(s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case sc.quote != 0:
			if c == sc.quote {
				sc.quote = 0
			}
			continue
		case (c == '"' || c == '\'') && strings.IndexByte("{[,:", sc.prev) >= 0:
			sc.quote = c
		case c == '{' || c == '[':
			sc.depth++
		case c == '}' || c == ']':
			sc.depth--
		}
		if c != ' ' && c != '\t' {
			sc.prev = c
		}
	}
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// parseDirective parses the body of %%{ ... }%%, a JSON5-like object
// without its outer braces ("init: {'theme': 'dark'}").
func parseDirective(body string) (map[string]any, error) {
	// Bare words are values too ("curve: basis"): a loosely written
	// setting should not keep the diagram from rendering.
	p := &json5{s: "{" + body + "}", bare: true}
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.space()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("unexpected %q", p.rest())
	}
	return v.(map[string]any), nil
}

// ParseAttrs parses the body of a Mermaid 11 attribute block,
// "A@{ shape: rect, label: \"Text\" }" without the braces. Values may be
// quoted or bare words; the result maps keys to strings, numbers or bools.
func ParseAttrs(body string) (map[string]any, error) {
	p := &json5{s: "{" + body + "}", bare: true}
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.space()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("unexpected %q", p.rest())
	}
	return v.(map[string]any), nil
}

// maxConfigDepth bounds the nesting of directive objects.
const maxConfigDepth = 16

// json5 parses the part of JSON5 that Mermaid directives use: objects,
// arrays, single or double quoted strings, unquoted keys, numbers, true,
// false and null.
type json5 struct {
	s string
	i int
	// bare accepts unquoted words as string values, as YAML flow
	// mappings do ("shape: rect").
	bare bool
}

func (p *json5) rest() string {
	r := p.s[p.i:]
	if len(r) > 20 {
		r = r[:20]
	}
	return r
}

func (p *json5) space() {
	for p.i < len(p.s) && unicode.IsSpace(rune(p.s[p.i])) {
		p.i++
	}
}

func (p *json5) value(depth int) (any, error) {
	if depth > maxConfigDepth {
		return nil, fmt.Errorf("%w: nested deeper than %d levels", guard.ErrTooLarge, maxConfigDepth)
	}
	p.space()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("unexpected end of directive")
	}
	switch c := p.s[p.i]; {
	case c == '{':
		p.i++
		m := map[string]any{}
		for {
			p.space()
			if p.i < len(p.s) && p.s[p.i] == '}' {
				p.i++
				return m, nil
			}
			key, err := p.key()
			if err != nil {
				return nil, err
			}
			p.space()
			if p.i < len(p.s) && (p.s[p.i] == ',' || p.s[p.i] == '}') {
				// A bare key, as in %%{wrap}%%.
				m[key] = true
				if !p.sep('}') {
					return nil, fmt.Errorf("unexpected %q", p.rest())
				}
				continue
			}
			if p.i >= len(p.s) || p.s[p.i] != ':' {
				return nil, fmt.Errorf("missing : after %q", key)
			}
			p.i++
			v, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			m[key] = v
			if !p.sep('}') {
				return nil, fmt.Errorf("unexpected %q", p.rest())
			}
		}
	case c == '[':
		p.i++
		var a []any
		for {
			p.space()
			if p.i < len(p.s) && p.s[p.i] == ']' {
				p.i++
				return a, nil
			}
			v, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
			if !p.sep(']') {
				return nil, fmt.Errorf("unexpected %q", p.rest())
			}
		}
	case c == '"' || c == '\'':
		return p.str()
	default:
		start := p.i
		stop := ",}]: \t\n"
		if p.bare {
			// A bare value runs to the next separator, spaces included,
			// as a YAML plain scalar does ("fontFamily: Bob Smith").
			stop = ",}]\n"
		}
		for p.i < len(p.s) && !strings.ContainsRune(stop, rune(p.s[p.i])) {
			p.i++
		}
		w := strings.TrimSpace(p.s[start:p.i])
		switch w {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null":
			return nil, nil
		}
		if f, err := strconv.ParseFloat(w, 64); err == nil {
			return f, nil
		}
		if p.bare && w != "" {
			return w, nil
		}
		return nil, fmt.Errorf("unexpected %q", w)
	}
}

// sep consumes a comma (a trailing one is allowed) and reports whether the
// container goes on or ends with close; it does not consume close.
func (p *json5) sep(close byte) bool {
	p.space()
	if p.i < len(p.s) && p.s[p.i] == ',' {
		p.i++
		return true
	}
	return p.i < len(p.s) && p.s[p.i] == close
}

func (p *json5) key() (string, error) {
	if p.i < len(p.s) && (p.s[p.i] == '"' || p.s[p.i] == '\'') {
		return p.str()
	}
	start := p.i
	for p.i < len(p.s) && (p.s[p.i] == '_' || p.s[p.i] == '$' || p.s[p.i] == '-' ||
		unicode.IsLetter(rune(p.s[p.i])) || unicode.IsDigit(rune(p.s[p.i]))) {
		p.i++
	}
	if p.i == start {
		return "", fmt.Errorf("expected a key at %q", p.rest())
	}
	return p.s[start:p.i], nil
}

func (p *json5) str() (string, error) {
	q := p.s[p.i]
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		p.i++
		switch {
		case c == q:
			return b.String(), nil
		case c == '\\' && p.i < len(p.s):
			b.WriteByte(p.s[p.i])
			p.i++
		default:
			b.WriteByte(c)
		}
	}
	return "", fmt.Errorf("unterminated string in directive")
}
