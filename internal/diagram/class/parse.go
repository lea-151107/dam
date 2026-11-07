// Package class parses and lays out Mermaid class diagrams.
package class

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/layout/sugiyama"
	"github.com/lea/mermaid/internal/richtext"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
	"github.com/lea/mermaid/internal/theme"
)

// Class is a class box.
type Class struct {
	ID          string
	Label       string
	Annotations []string
	Attributes  []string
	Methods     []string
	Classes     []string
	Style       theme.Style
	Namespace   int // -1 for none
	Note        bool
}

// Relation connects two classes.
type Relation struct {
	From, To         int
	StartM, EndM     scene.Marker
	Dashed           bool
	Label            string
	FromCard, ToCard string
}

// Diagram is a parsed class diagram.
type Diagram struct {
	Dir        sugiyama.Dir
	Classes    []*Class
	Relations  []Relation
	Namespaces []string
	index      map[string]int
	classDefs  map[string]theme.Style
	// guard and limitErr enforce the element limit for every class created,
	// including those created implicitly by members and relations.
	guard    *guard.Guard
	limitErr error
}

const namePat = "([\\p{L}\\p{N}_$]+(?:~[^~]*~)?|`[^`]+`)"

// lollipopRe matches a lollipop interface: "A ()-- B" or "A --() B".
var lollipopRe = regexp.MustCompile(`\(\)\s*--|--\s*\(\)`)

var (
	relationRe = regexp.MustCompile(`^` + namePat + `\s*(?:"([^"]*)"\s*)?(<\||\*|o|<)?(--|\.\.)(\|>|\*|o|>)?\s*(?:"([^"]*)"\s*)?` + namePat + `\s*(?::\s*(.*))?$`)
	classRe    = regexp.MustCompile(`^class\s+` + namePat + `\s*(?:\["([^"]*)"\])?\s*(?::::\s*([\w-]+))?\s*(\{)?\s*(\})?$`)
	memberRe   = regexp.MustCompile(`^` + namePat + `\s*:\s*(.+)$`)
	annotRe    = regexp.MustCompile(`^<<([^>]+)>>\s*` + namePat + `?$`)
	noteForRe  = regexp.MustCompile(`^note\s+for\s+` + namePat + `\s+"(.*)"$`)
	noteRe     = regexp.MustCompile(`^note\s+"(.*)"$`)
	cssClassRe = regexp.MustCompile(`^cssClass\s+"([^"]+)"\s+([\w-]+)$`)
)

// maxNamespaceDepth bounds namespace nesting.
const maxNamespaceDepth = 1

// Parse parses a class diagram.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{Dir: sugiyama.TB, index: map[string]int{}, classDefs: map[string]theme.Style{}, guard: g}
	cur := -1 // class whose body is open
	ns := -1
	curLine, nsLine := 0, 0 // where cur and ns were opened, for errors
	notes := 0
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		if d.limitErr != nil {
			return nil, d.limitErr
		}
		t := strings.TrimSpace(l.Text)
		if cur >= 0 {
			if t == "}" {
				cur = -1
				continue
			}
			if m := annotRe.FindStringSubmatch(t); m != nil && m[2] == "" {
				d.Classes[cur].Annotations = append(d.Classes[cur].Annotations, m[1])
				continue
			}
			if err := d.addMember(g, cur, t); err != nil {
				return nil, err
			}
			continue
		}
		if t == "}" {
			if ns < 0 {
				return nil, source.Errorf(l.No, "unexpected }")
			}
			ns = -1
			continue
		}
		if rest, ok := source.CutKeyword(t, "direction"); ok {
			switch strings.ToUpper(rest) {
			case "TB", "TD":
				d.Dir = sugiyama.TB
			case "BT":
				d.Dir = sugiyama.BT
			case "LR":
				d.Dir = sugiyama.LR
			case "RL":
				d.Dir = sugiyama.RL
			default:
				return nil, source.Errorf(l.No, "unknown direction %q", rest)
			}
			continue
		}
		if rest, ok := source.CutKeyword(t, "namespace"); ok {
			if ns >= 0 {
				return nil, source.Unsupported(l.No, "nested namespaces are not supported")
			}
			name := strings.TrimSpace(strings.TrimSuffix(rest, "{"))
			if !strings.HasSuffix(rest, "{") || name == "" {
				return nil, source.Errorf(l.No, "namespace needs a name and {")
			}
			d.Namespaces = append(d.Namespaces, name)
			ns = len(d.Namespaces) - 1
			nsLine = l.No
			continue
		}
		if m := classRe.FindStringSubmatch(t); m != nil {
			c := d.class(m[1], ns)
			if m[2] != "" {
				d.Classes[c].Label = source.Label(m[2])
			}
			if m[3] != "" {
				d.Classes[c].Classes = append(d.Classes[c].Classes, m[3])
			}
			if m[4] == "{" && m[5] == "" {
				cur = c
				curLine = l.No
			}
			if err := g.Count("classes", len(d.Classes)); err != nil {
				return nil, err
			}
			continue
		}
		if m := annotRe.FindStringSubmatch(t); m != nil && m[2] != "" {
			c := d.class(m[2], ns)
			d.Classes[c].Annotations = append(d.Classes[c].Annotations, m[1])
			continue
		}
		if m := noteForRe.FindStringSubmatch(t); m != nil {
			target := d.class(m[1], ns)
			n := d.note(m[2], &notes, ns)
			d.Relations = append(d.Relations, Relation{From: n, To: target, Dashed: true})
			continue
		}
		if m := noteRe.FindStringSubmatch(t); m != nil {
			d.note(m[1], &notes, ns)
			continue
		}
		if m := relationRe.FindStringSubmatch(t); m != nil {
			r := Relation{
				From: d.class(m[1], ns), To: d.class(m[7], ns),
				FromCard: m[2], ToCard: m[6], Dashed: m[4] == "..",
				StartM: marker(m[3]), EndM: marker(m[5]), Label: source.Label(m[8]),
			}
			d.Relations = append(d.Relations, r)
			if err := g.Count("relations", len(d.Relations)); err != nil {
				return nil, err
			}
			continue
		}
		if m := memberRe.FindStringSubmatch(t); m != nil {
			if err := d.addMember(g, d.class(m[1], ns), m[2]); err != nil {
				return nil, err
			}
			continue
		}
		if rest, ok := source.CutKeyword(t, "classDef"); ok {
			names, style, _ := strings.Cut(rest, " ")
			for _, n := range strings.Split(names, ",") {
				d.classDefs[n] = d.classDefs[n].Merge(theme.ParseStyle(style))
			}
			continue
		}
		if rest, ok := source.CutKeyword(t, "style"); ok {
			id, style, _ := strings.Cut(rest, " ")
			c := d.class(id, ns)
			d.Classes[c].Style = d.Classes[c].Style.Merge(theme.ParseStyle(style))
			continue
		}
		if m := cssClassRe.FindStringSubmatch(t); m != nil {
			for _, id := range strings.Split(m[1], ",") {
				c := d.class(strings.TrimSpace(id), ns)
				d.Classes[c].Classes = append(d.Classes[c].Classes, m[2])
			}
			continue
		}
		kw, _, _ := strings.Cut(t, " ")
		switch kw {
		case "click", "callback", "link", "accTitle:", "accDescr:", "title":
			// NOTE(mermaid-compat): interaction directives do not affect the image.
			continue
		}
		if lollipopRe.MatchString(t) {
			// NOTE(mermaid-compat): lollipop interfaces are valid Mermaid,
			// not drawn.
			return nil, source.Unsupported(l.No, "lollipop interfaces")
		}
		return nil, source.Errorf(l.No, "unrecognized statement %q", t)
	}
	if d.limitErr != nil {
		return nil, d.limitErr
	}
	if cur >= 0 {
		return nil, source.Errorf(curLine, "class %q body is not closed with }", d.Classes[cur].ID)
	}
	if ns >= 0 {
		return nil, source.Errorf(nsLine, "namespace %q is not closed with }", d.Namespaces[ns])
	}
	for _, c := range d.Classes {
		st := d.classDefs["default"]
		for _, k := range c.Classes {
			st = st.Merge(d.classDefs[k])
		}
		c.Style = st.Merge(c.Style)
	}
	return d, nil
}

func (d *Diagram) note(text string, n *int, ns int) int {
	*n++
	id := "\x00note" + strconv.Itoa(*n)
	c := d.class(id, ns)
	d.Classes[c].Note = true
	d.Classes[c].Label = source.Label(strings.ReplaceAll(text, `\n`, "\n"))
	return c
}

func marker(s string) scene.Marker {
	switch s {
	case "<|", "|>":
		return scene.Triangle
	case "*":
		return scene.Diamond
	case "o":
		return scene.HollowDiamond
	case "<", ">":
		return scene.OpenArrow
	}
	return scene.NoMarker
}

// displayName converts generics "List~int~" to "List<int>".
func displayName(id string) string {
	return generics(strings.Trim(id, "`"))
}

func (d *Diagram) class(name string, ns int) int {
	id := name
	if i := strings.Index(name, "~"); i >= 0 {
		id = name[:i]
	}
	if i, ok := d.index[id]; ok {
		if name != id {
			d.Classes[i].Label = displayName(name)
		}
		return i
	}
	d.Classes = append(d.Classes, &Class{ID: id, Label: displayName(name), Namespace: ns})
	d.index[id] = len(d.Classes) - 1
	if err := d.guard.Count("classes", len(d.Classes)); err != nil && d.limitErr == nil {
		d.limitErr = err
	}
	return len(d.Classes) - 1
}

// addMember adds member m to class c. Members count toward MaxNodes per
// class, like the attributes of an ER entity.
func (d *Diagram) addMember(g *guard.Guard, c int, m string) error {
	m = strings.TrimSpace(m)
	if m == "" {
		return nil
	}
	// A trailing $ marks a static member (underlined), * an abstract one
	// (italic).
	// A method with a return type has it after the parentheses:
	// "+create()$ Shape".
	st := richtext.Regular
	classifier := func(c byte) richtext.Style {
		if c == '$' {
			return richtext.Underline
		}
		return richtext.Italic
	}
	if i := strings.LastIndex(m, ")"); i >= 0 && i+1 < len(m) && (m[i+1] == '$' || m[i+1] == '*') {
		st = classifier(m[i+1])
		m = m[:i+1] + m[i+2:]
	} else if n := len(m); m[n-1] == '$' || m[n-1] == '*' {
		st = classifier(m[n-1])
		m = strings.TrimRight(m, "$*")
	}
	m = generics(m)
	if st != richtext.Regular {
		m = string(richtext.Marker(st)) + m
	}
	if strings.Contains(m, "(") {
		d.Classes[c].Methods = append(d.Classes[c].Methods, m)
	} else {
		d.Classes[c].Attributes = append(d.Classes[c].Attributes, m)
	}
	return g.Count("members", len(d.Classes[c].Methods)+len(d.Classes[c].Attributes))
}

// generics writes the generic types of Mermaid's ~ notation with angle
// brackets: List~int~ is List<int>, List~List~int~~ is
// List<List<int>>. A ~ between two name characters opens a type
// argument; any other ~ closes the innermost open one, or, with none open,
// stays (the package visibility of ~int count).
func generics(s string) string {
	if !strings.Contains(s, "~") {
		return s
	}
	rs := []rune(s)
	name := func(k int) bool {
		if k < 0 || k >= len(rs) {
			return false
		}
		r := rs[k]
		return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || r == ']'
	}
	var b strings.Builder
	depth := 0
	for k, r := range rs {
		switch {
		case r != '~':
			b.WriteRune(r)
		case name(k-1) && name(k+1):
			b.WriteByte('<')
			depth++
		case depth > 0:
			b.WriteByte('>')
			depth--
		default:
			b.WriteByte('~')
		}
	}
	return b.String()
}
