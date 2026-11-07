// Package state parses and lays out Mermaid state diagrams.
package state

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/layout/sugiyama"
	"github.com/lea/mermaid/internal/source"
	"github.com/lea/mermaid/internal/theme"
)

// Kind is a state kind.
type Kind uint8

// State kinds.
const (
	Normal Kind = iota
	Start
	End
	Choice
	Fork
	Join
	Note
)

// State is a node of the diagram.
type State struct {
	ID        string
	Label     string
	Desc      []string
	Kind      Kind
	Parent    int // composite (cluster) index or -1
	Composite int // cluster index when this state has children, or -1
	Classes   []string
	Style     theme.Style
}

// Transition is an edge.
type Transition struct {
	From, To int
	Label    string
	Note     bool // dashed note connector
}

// Composite is a state containing other states.
type Composite struct {
	State  int
	Parent int
	// Region marks a concurrent region (separated by -- inside a
	// composite): an untitled cluster whose Parent is the composite and
	// whose State is the composite's state.
	Region bool
	// Dir is the direction set inside the composite, when HasDir.
	Dir    sugiyama.Dir
	HasDir bool
}

// Diagram is a parsed state diagram.
type Diagram struct {
	Dir         sugiyama.Dir
	States      []*State
	Transitions []Transition
	Composites  []Composite
	index       map[string]int
	classDefs   map[string]theme.Style
	// guard and limitErr enforce the element limit for every state created.
	guard    *guard.Guard
	limitErr error
}

const idPat = `([\p{L}\p{N}_.\-]+|\[\*\])`

var (
	transRe   = regexp.MustCompile(`^` + idPat + `(?::::([\w-]+))?\s*-->\s*` + idPat + `(?::::([\w-]+))?\s*(?::\s*(.*))?$`)
	stateAsRe = regexp.MustCompile(`^state\s+"([^"]*)"\s+as\s+` + idPat + `\s*(\{)?$`)
	stateRe   = regexp.MustCompile(`^state\s+` + idPat + `\s*(<<(choice|fork|join)>>)?\s*(\{)?$`)
	descRe    = regexp.MustCompile(`^` + idPat + `\s*:\s*(.*)$`)
	noteRe    = regexp.MustCompile(`(?i)^note\s+(left|right)\s+of\s+` + idPat + `\s*(?::\s*(.*))?$`)
	classRe   = regexp.MustCompile(`^class\s+([^\s]+)\s+([\w-]+)$`)
	bareRe    = regexp.MustCompile(`^` + idPat + `(?::::([\w-]+))?$`)
)

// maxDepth bounds composite state nesting.
const maxDepth = 32

// Parse parses a state diagram.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{Dir: sugiyama.TB, index: map[string]int{}, classDefs: map[string]theme.Style{}, guard: g}
	var stack []int     // composite indices
	var stackLine []int // source line of each open composite, for errors
	scope := func() int {
		if len(stack) == 0 {
			return -1
		}
		return stack[len(stack)-1]
	}
	open := func(s int, no int) error {
		if len(stack) >= maxDepth {
			return source.TooLarge(no, "states nested too deeply")
		}
		if d.States[s].Composite < 0 {
			// The composite sits where its state is, which is not the
			// open scope when the state was first named elsewhere ("state
			// A { B }" and then "state B { ... }" at the top). Taking the
			// scope made the members of A (B) and the descendants of A
			// disagree, and an edge to A found no state to attach to.
			parent := d.States[s].Parent
			depth := 1
			for p := parent; p >= 0; p = d.Composites[p].Parent {
				depth++
			}
			if depth > maxDepth {
				return source.TooLarge(no, "states nested too deeply")
			}
			d.Composites = append(d.Composites, Composite{State: s, Parent: parent})
			d.States[s].Composite = len(d.Composites) - 1
		}
		stack = append(stack, d.States[s].Composite)
		stackLine = append(stackLine, no)
		return nil
	}
	notes := 0
	for i := 1; i < len(lines); i++ {
		l := lines[i]
		if err := g.Tick(); err != nil {
			return nil, err
		}
		if d.limitErr != nil {
			return nil, d.limitErr
		}
		t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l.Text), ";"))
		switch {
		case t == "}":
			if len(stack) == 0 {
				return nil, source.Errorf(l.No, "unexpected }")
			}
			stack = stack[:len(stack)-1]
			stackLine = stackLine[:len(stackLine)-1]
			continue
		case t == "--":
			if len(stack) == 0 {
				return nil, source.Errorf(l.No, "-- outside of a composite state")
			}
			if err := d.newRegion(stack); err != nil {
				return nil, err
			}
			continue
		}
		if rest, ok := source.CutKeyword(t, "direction"); ok {
			var dir sugiyama.Dir
			switch strings.ToUpper(rest) {
			case "TB", "TD":
				dir = sugiyama.TB
			case "BT":
				dir = sugiyama.BT
			case "LR":
				dir = sugiyama.LR
			case "RL":
				dir = sugiyama.RL
			default:
				return nil, source.Errorf(l.No, "unknown direction %q", rest)
			}
			if c := scope(); c >= 0 {
				// Inside a composite it applies to the composite only.
				if d.Composites[c].Region {
					c = d.Composites[c].Parent
				}
				d.Composites[c].Dir, d.Composites[c].HasDir = dir, true
			} else {
				d.Dir = dir
			}
			continue
		}
		if m := transRe.FindStringSubmatch(t); m != nil {
			from := d.endpoint(m[1], scope(), true)
			to := d.endpoint(m[3], scope(), false)
			d.addClass(from, m[2])
			d.addClass(to, m[4])
			d.Transitions = append(d.Transitions, Transition{From: from, To: to, Label: source.Label(m[5])})
			if err := g.Count("transitions", len(d.Transitions)); err != nil {
				return nil, err
			}
			if err := g.Count("states", len(d.States)); err != nil {
				return nil, err
			}
			continue
		}
		if m := stateAsRe.FindStringSubmatch(t); m != nil {
			s := d.state(m[2], scope())
			d.States[s].Label = source.Label(m[1])
			if m[3] == "{" {
				if err := open(s, l.No); err != nil {
					return nil, err
				}
			}
			continue
		}
		if m := stateRe.FindStringSubmatch(t); m != nil {
			s := d.state(m[1], scope())
			switch m[3] {
			case "choice":
				d.States[s].Kind = Choice
			case "fork":
				d.States[s].Kind = Fork
			case "join":
				d.States[s].Kind = Join
			}
			if m[4] == "{" {
				if err := open(s, l.No); err != nil {
					return nil, err
				}
			}
			continue
		}
		if m := noteRe.FindStringSubmatch(t); m != nil {
			target := d.state(m[2], scope())
			text := m[3]
			if !strings.Contains(t, ":") {
				var body []string
				for i++; i < len(lines) && strings.TrimSpace(lines[i].Text) != "end note"; i++ {
					body = append(body, strings.TrimSpace(lines[i].Text))
				}
				if i == len(lines) {
					return nil, source.Errorf(l.No, "note is not closed with end note")
				}
				text = strings.Join(body, "\n")
			}
			notes++
			n := d.state("\x00note"+strconv.Itoa(notes), scope())
			d.States[n].Kind = Note
			d.States[n].Label = source.Label(text)
			from, to := n, target
			if strings.EqualFold(m[1], "right") {
				from, to = target, n
			}
			d.Transitions = append(d.Transitions, Transition{From: from, To: to, Note: true})
			continue
		}
		if rest, ok := source.CutKeyword(t, "classDef"); ok {
			name, style, _ := strings.Cut(rest, " ")
			d.classDefs[name] = d.classDefs[name].Merge(theme.ParseStyle(style))
			continue
		}
		if m := classRe.FindStringSubmatch(t); m != nil {
			for _, id := range strings.Split(m[1], ",") {
				d.addClass(d.state(id, scope()), m[2])
			}
			continue
		}
		if rest, ok := source.CutKeyword(t, "style"); ok {
			id, style, _ := strings.Cut(rest, " ")
			s := d.state(id, scope())
			d.States[s].Style = d.States[s].Style.Merge(theme.ParseStyle(style))
			continue
		}
		if m := descRe.FindStringSubmatch(t); m != nil && m[1] != "[*]" {
			s := d.state(m[1], scope())
			d.States[s].Desc = append(d.States[s].Desc, source.Label(m[2]))
			continue
		}
		if m := bareRe.FindStringSubmatch(t); m != nil && m[1] != "[*]" {
			d.addClass(d.state(m[1], scope()), m[2])
			continue
		}
		kw, _, _ := strings.Cut(t, " ")
		switch kw {
		case "hide", "scale", "click", "accTitle:", "accDescr:", "title":
			// NOTE(mermaid-compat): presentation directives without effect here.
			continue
		}
		return nil, source.Errorf(l.No, "unrecognized statement %q", t)
	}
	if d.limitErr != nil {
		return nil, d.limitErr
	}
	if len(stack) > 0 {
		return nil, source.Errorf(stackLine[len(stackLine)-1], "state %q is not closed with }", d.States[d.Composites[stack[len(stack)-1]].State].ID)
	}
	for _, s := range d.States {
		st := d.classDefs["default"]
		for _, c := range s.Classes {
			st = st.Merge(d.classDefs[c])
		}
		s.Style = st.Merge(s.Style)
	}
	return d, nil
}

func (d *Diagram) addClass(s int, c string) {
	if c != "" {
		d.States[s].Classes = append(d.States[s].Classes, c)
	}
}

// endpoint resolves "[*]" to the start or end pseudo state of the scope.
func (d *Diagram) endpoint(id string, scope int, from bool) int {
	if id != "[*]" {
		return d.state(id, scope)
	}
	kind, name := End, "end"
	if from {
		kind, name = Start, "start"
	}
	s := d.state("\x00"+name+strconv.Itoa(scope), scope)
	d.States[s].Kind = kind
	d.States[s].Label = ""
	return s
}

func (d *Diagram) state(id string, scope int) int {
	if i, ok := d.index[id]; ok {
		return i
	}
	d.States = append(d.States, &State{ID: id, Label: id, Parent: scope, Composite: -1})
	d.index[id] = len(d.States) - 1
	if err := d.guard.Count("states", len(d.States)); err != nil && d.limitErr == nil {
		d.limitErr = err
	}
	return len(d.States) - 1
}

// newRegion starts a concurrent region in the composite at the top of
// stack, which it replaces. The first -- moves what the composite already
// holds into a first region.
func (d *Diagram) newRegion(stack []int) error {
	top := stack[len(stack)-1]
	owner := top
	if d.Composites[top].Region {
		owner = d.Composites[top].Parent
	} else {
		d.Composites = append(d.Composites, Composite{State: d.Composites[owner].State, Parent: owner, Region: true})
		first := len(d.Composites) - 1
		for _, s := range d.States {
			if s.Parent == owner {
				s.Parent = first
			}
		}
		for i := range d.Composites[:first] {
			if d.Composites[i].Parent == owner && i != first {
				d.Composites[i].Parent = first
			}
		}
	}
	d.Composites = append(d.Composites, Composite{State: d.Composites[owner].State, Parent: owner, Region: true})
	stack[len(stack)-1] = len(d.Composites) - 1
	return d.guard.Count("states", len(d.States)+len(d.Composites))
}
