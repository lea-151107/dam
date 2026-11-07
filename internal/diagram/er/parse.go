// Package er parses and lays out Mermaid entity relationship diagrams.
package er

import (
	"regexp"
	"strings"

	"github.com/lea/mermaid/internal/boxgraph"
	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/layout/sugiyama"
	"github.com/lea/mermaid/internal/scene"
	"github.com/lea/mermaid/internal/source"
	"github.com/lea/mermaid/internal/theme"
)

// Attribute is one row of an entity.
type Attribute struct {
	Type, Name string
	Keys       []string // PK, FK, UK
	Comment    string
}

// Entity is a table.
type Entity struct {
	ID         string
	Label      string
	Attributes []Attribute
	Classes    []string
	Style      theme.Style
}

// Relation joins two entities. The markers are the crow's foot
// cardinalities at each end.
type Relation struct {
	From, To    int
	FromCard    scene.Marker
	ToCard      scene.Marker
	Label       string
	Identifying bool // solid (--) rather than dashed (..)
}

// Diagram is a parsed ER diagram.
type Diagram struct {
	Dir       sugiyama.Dir
	Entities  []*Entity
	Relations []Relation
	index     map[string]int
	classDefs map[string]theme.Style
	guard     *guard.Guard
	limitErr  error
}

const namePat = `([\p{L}\p{N}_\-]+|"[^"]*")`

var (
	// "A ||--o{ B : label", "A one or more to zero or many B : label" and
	// mixtures: either form of cardinality on either side, joined by --
	// or "to" (identifying) or .. or "optionally to".
	symCard  = `\|o|\|\||\}o|\}\||o\||o\{|\|\{`
	wordCard = `only one|one or zero|zero or one|one or more|one or many|many\(1\)|1\+|zero or more|zero or many|many\(0\)|0\+|1`
	relRe    = regexp.MustCompile(`^` + namePat + `\s*(` + symCard + `|` + wordCard + `)\s*(--|\.\.|optionally to|to)\s*(` +
		symCard + `|` + wordCard + `)\s*` + namePat + `\s*:\s*(.*)$`)
	// "A {" or "A[Alias] {" or "A" alone
	entityRe = regexp.MustCompile(`^` + namePat + `(?:\s*\[\s*("[^"]*"|[^\]]*)\s*\])?\s*(\{)?\s*(?::::\s*([\w-]+))?$`)
	// "type name PK, FK \"comment\""
	attrRe = regexp.MustCompile(`^(\S+)\s+(\S+)((?:\s*(?:PK|FK|UK)\s*,?)*)\s*("[^"]*")?$`)
	// relLikeRe matches a relationship with cardinalities on both sides,
	// whatever its ends look like.
	relLikeRe  = regexp.MustCompile(`(` + symCard + `)\s*(--|\.\.)\s*(` + symCard + `)\s*\S.*:`)
	classDefRe = regexp.MustCompile(`^classDef\s+(\S+)\s+(.*)$`)
	classRe    = regexp.MustCompile(`^class\s+(\S+)\s+([\w-]+)$`)
	styleRe    = regexp.MustCompile(`^style\s+(\S+)\s+(.*)$`)
)

// Parse parses an ER diagram.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{Dir: sugiyama.TB, index: map[string]int{}, classDefs: map[string]theme.Style{}, guard: g}
	open := -1 // entity whose attribute block is open
	openLine := 0
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		if d.limitErr != nil {
			return nil, d.limitErr
		}
		t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l.Text), ";"))
		if open >= 0 {
			if t == "}" {
				open = -1
				continue
			}
			m := attrRe.FindStringSubmatch(t)
			if m == nil {
				return nil, source.Errorf(l.No, "expected an attribute (type name [PK|FK|UK] [\"comment\"])")
			}
			a := Attribute{Type: source.Label(m[1]), Name: source.Label(m[2]), Comment: source.Label(m[4])}
			for _, k := range strings.FieldsFunc(m[3], func(r rune) bool { return r == ',' || r == ' ' }) {
				a.Keys = append(a.Keys, k)
			}
			e := d.Entities[open]
			e.Attributes = append(e.Attributes, a)
			if err := g.Count("attributes", len(e.Attributes)); err != nil {
				return nil, err
			}
			continue
		}
		switch {
		case strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr") || strings.HasPrefix(t, "title"):
			// NOTE(mermaid-compat): accessibility texts do not affect the
			// image; titles come from front matter.
			continue
		}
		if rest, ok := source.CutKeyword(t, "direction"); ok {
			dir, ok := parseDir(rest)
			if !ok {
				return nil, source.Errorf(l.No, "unknown direction %q", rest)
			}
			d.Dir = dir
			continue
		}
		if m := classDefRe.FindStringSubmatch(t); m != nil {
			for _, n := range strings.Split(m[1], ",") {
				d.classDefs[n] = d.classDefs[n].Merge(theme.ParseStyle(m[2]))
			}
			continue
		}
		if m := classRe.FindStringSubmatch(t); m != nil {
			for _, id := range strings.Split(m[1], ",") {
				e := d.Entities[d.entity(id)]
				e.Classes = append(e.Classes, m[2])
			}
			continue
		}
		if m := styleRe.FindStringSubmatch(t); m != nil {
			e := d.Entities[d.entity(m[1])]
			e.Style = e.Style.Merge(theme.ParseStyle(m[2]))
			continue
		}
		if m := relRe.FindStringSubmatch(t); m != nil {
			from, to := d.entity(m[1]), d.entity(m[5])
			d.Relations = append(d.Relations, Relation{From: from, To: to, FromCard: cardOf(m[2]), ToCard: cardOf(m[4]),
				Identifying: m[3] == "--" || m[3] == "to", Label: label(m[6])})
			if err := g.Count("relations", len(d.Relations)); err != nil {
				return nil, err
			}
			continue
		}
		// A block on one line: "A { string name int age }".
		if i := strings.IndexByte(t, '{'); i > 0 && strings.HasSuffix(t, "}") {
			if m := entityRe.FindStringSubmatch(strings.TrimSpace(t[:i])); m != nil && m[3] == "" {
				e := d.entity(m[1])
				if m[2] != "" {
					d.Entities[e].Label = source.Label(m[2])
				}
				attrs, ok := oneLineAttrs(t[i+1 : len(t)-1])
				if !ok {
					return nil, source.Errorf(l.No, "expected attributes (type name [PK|FK|UK] [\"comment\"]) between { and }")
				}
				d.Entities[e].Attributes = append(d.Entities[e].Attributes, attrs...)
				if err := g.Count("attributes", len(d.Entities[e].Attributes)); err != nil {
					return nil, err
				}
				continue
			}
		}
		if m := entityRe.FindStringSubmatch(t); m != nil {
			e := d.entity(m[1])
			if m[2] != "" {
				d.Entities[e].Label = source.Label(m[2])
			}
			if m[4] != "" {
				d.Entities[e].Classes = append(d.Entities[e].Classes, m[4])
			}
			if m[3] == "{" {
				open, openLine = e, l.No
			}
			continue
		}
		if relLikeRe.MatchString(t) && (strings.Contains(t, "[") || strings.Contains(t, ":::")) {
			// NOTE(mermaid-compat): an alias or class on an end of a
			// relationship is valid Mermaid; they are supported on entity
			// statements only.
			return nil, source.Unsupported(l.No, "aliases and classes in relationships")
		}
		return nil, source.Errorf(l.No, "unrecognized statement %q", t)
	}
	if open >= 0 {
		return nil, source.Errorf(openLine, "entity %q is not closed with }", d.Entities[open].ID)
	}
	if d.limitErr != nil {
		return nil, d.limitErr
	}
	for _, e := range d.Entities {
		st := d.classDefs["default"]
		for _, c := range e.Classes {
			st = st.Merge(d.classDefs[c])
		}
		e.Style = st.Merge(e.Style)
	}
	return d, nil
}

// entity returns the index of entity id, creating it if necessary.
func (d *Diagram) entity(id string) int {
	id = strings.Trim(strings.TrimSpace(id), `"`)
	if i, ok := d.index[id]; ok {
		return i
	}
	d.Entities = append(d.Entities, &Entity{ID: id, Label: source.Label(id)})
	d.index[id] = len(d.Entities) - 1
	if err := d.guard.Count("entities", len(d.Entities)); err != nil && d.limitErr == nil {
		d.limitErr = err
	}
	return len(d.Entities) - 1
}

// oneLineAttrs parses the attributes of a block written on one line, where
// Mermaid's grammar separates them by whitespace like the lines of a block.
func oneLineAttrs(body string) ([]Attribute, bool) {
	var toks []string
	for s := strings.TrimSpace(body); s != ""; s = strings.TrimSpace(s) {
		if s[0] == '"' {
			j := strings.IndexByte(s[1:], '"')
			if j < 0 {
				return nil, false
			}
			toks, s = append(toks, s[:j+2]), s[j+2:]
			continue
		}
		j := strings.IndexFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || r == '"' })
		if j < 0 {
			j = len(s)
		}
		toks, s = append(toks, s[:j]), s[j:]
	}
	isKey := func(t string) bool {
		k := strings.TrimSuffix(t, ",")
		return k == "PK" || k == "FK" || k == "UK" || t == ","
	}
	var attrs []Attribute
	for len(toks) > 0 {
		if len(toks) < 2 || toks[0][0] == '"' || toks[1][0] == '"' {
			return nil, false
		}
		a := Attribute{Type: source.Label(toks[0]), Name: source.Label(toks[1])}
		toks = toks[2:]
		for len(toks) > 0 && isKey(toks[0]) {
			if k := strings.TrimSuffix(toks[0], ","); k != "" && k != "," {
				a.Keys = append(a.Keys, k)
			}
			toks = toks[1:]
		}
		if len(toks) > 0 && toks[0][0] == '"' {
			a.Comment, toks = source.Label(toks[0]), toks[1:]
		}
		attrs = append(attrs, a)
	}
	return attrs, true
}

func label(s string) string {
	return source.Label(strings.TrimSpace(s))
}

// cardOf maps a cardinality, a symbol on either side of the line or words.
func cardOf(s string) scene.Marker {
	switch s {
	case "||", "only one", "1":
		return scene.OneOnly
	case "|o", "o|", "one or zero", "zero or one":
		return scene.ZeroOrOne
	case "}|", "|{", "one or more", "one or many", "many(1)", "1+":
		return scene.OneOrMore
	}
	return scene.ZeroOrMore // "}o", "o{", "zero or more", "zero or many", "many(0)", "0+"
}

func parseDir(s string) (sugiyama.Dir, bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "TB", "TD":
		return sugiyama.TB, true
	case "BT":
		return sugiyama.BT, true
	case "LR":
		return sugiyama.LR, true
	case "RL":
		return sugiyama.RL, true
	}
	return 0, false
}

// graph converts the diagram to a box graph: each entity is a header with
// its attributes as rows of type, name, keys and comment.
func (d *Diagram) graph() *boxgraph.Graph {
	g := &boxgraph.Graph{Dir: d.Dir}
	for _, e := range d.Entities {
		n := boxgraph.Node{Header: []string{e.Label}, Cluster: -1,
			Fill: e.Style.Fill, Stroke: e.Style.Stroke, Text: e.Style.Color}
		if len(e.Attributes) > 0 {
			var rows [][]string
			for _, a := range e.Attributes {
				rows = append(rows, []string{a.Type, a.Name, strings.Join(a.Keys, ","), a.Comment})
			}
			n.Sections = [][][]string{rows}
		}
		g.Nodes = append(g.Nodes, n)
	}
	for _, r := range d.Relations {
		g.Edges = append(g.Edges, boxgraph.Edge{From: r.From, To: r.To, Label: r.Label,
			Start: r.FromCard, End: r.ToCard, Dashed: !r.Identifying})
	}
	return g
}
