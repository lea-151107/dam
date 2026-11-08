// Package sequence parses and lays out Mermaid sequence diagrams.
package sequence

import (
	"image/color"
	"regexp"
	"strconv"
	"strings"

	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/source"
	"github.com/lea-151107/dam/internal/theme"
)

// Participant is a lifeline.
type Participant struct {
	ID    string
	Label string
	Actor bool
	// Type is the Mermaid 11 participant type other than actor
	// (database, queue, ...), "" for a plain participant.
	//
	// NOTE(mermaid-compat): Mermaid draws an icon for each type; dam shows
	// the type as a stereotype line («database») in the box.
	Type string
	// Box is the index of the box the participant was declared in, or -1.
	Box int
}

// Box groups participants under a label and a background color.
type Box struct {
	Label string
	// Color is the background; nil means the theme's default and A == 0
	// (transparent) no background.
	Color   *color.NRGBA
	Members []int
}

// parseBox reads "box [color] [label]": the color is a name, #hex,
// rgb()/rgba() or transparent.
func parseBox(text string) Box {
	word, rest, _ := strings.Cut(text, " ")
	if strings.HasPrefix(word, "rgb") {
		if i := strings.Index(text, ")"); i >= 0 {
			word, rest = text[:i+1], text[i+1:]
		}
	}
	if word == "transparent" {
		return Box{Label: source.Label(rest), Color: &color.NRGBA{}}
	}
	if c, ok := theme.ParseColor(word); ok && word != "" {
		c.A = 255
		return Box{Label: source.Label(rest), Color: &c}
	}
	return Box{Label: source.Label(text)}
}

// EventKind classifies events.
type EventKind uint8

// Event kinds.
const (
	Message EventKind = iota
	Note
	BlockStart // loop, alt, opt, par, critical, break, rect
	BlockElse  // else, and, option
	BlockEnd
	Activate
	Deactivate
	// Create makes participant From appear at the next message that
	// involves it; Destroy ends its lifeline at that message.
	Create
	Destroy
)

// ArrowEnd is the head of a message arrow.
type ArrowEnd uint8

// Arrow heads.
const (
	HeadNone ArrowEnd = iota
	HeadFilled
	HeadCross
	HeadOpen // async ")"
)

// NotePlace is where a note is attached.
type NotePlace uint8

// Note placements.
const (
	LeftOf NotePlace = iota
	RightOf
	Over
)

// Event is one step of the diagram, in source order.
type Event struct {
	Kind     EventKind
	From, To int // participants (Message); To also for notes "over A,B"
	Text     string
	Dotted   bool
	Head     ArrowEnd
	BothEnds bool
	Place    NotePlace
	Block    string // block keyword for BlockStart/BlockElse
	Number   int    // autonumber value, when Numbered
	Numbered bool   // set while autonumber is on
	ActFrom  int    // activation change on target: +1, -1
}

// Diagram is a parsed sequence diagram.
type Diagram struct {
	Title        string
	Participants []Participant
	Boxes        []Box
	Events       []Event
	index        map[string]int
	// guard and limitErr enforce the element limit for participants created
	// implicitly by messages, notes and activations.
	guard    *guard.Guard
	limitErr error
}

var (
	participantRe = regexp.MustCompile(`^(participant|actor)\s+(.+?)(?:\s+as\s+(.+))?$`)
	noteRe        = regexp.MustCompile(`(?i)^note\s+(left of|right of|over)\s+([^:]+?)\s*:\s*(.*)$`)
	// Arrow operators, longest first.
	arrows = []struct {
		op     string
		dotted bool
		head   ArrowEnd
		both   bool
	}{
		{"<<-->>", true, HeadFilled, true},
		{"<<->>", false, HeadFilled, true},
		{"-->>", true, HeadFilled, false},
		{"->>", false, HeadFilled, false},
		{"--x", true, HeadCross, false},
		{"-x", false, HeadCross, false},
		{"--)", true, HeadOpen, false},
		{"-)", false, HeadOpen, false},
		{"-->", true, HeadNone, false},
		{"->", false, HeadNone, false},
	}
)

var blockKeywords = map[string]EventKind{
	"loop": BlockStart, "alt": BlockStart, "opt": BlockStart, "par": BlockStart,
	"critical": BlockStart, "break": BlockStart, "rect": BlockStart, "box": BlockStart,
	"else": BlockElse, "and": BlockElse, "option": BlockElse,
}

// maxBlockDepth bounds nesting of loop/alt/... blocks.
const maxBlockDepth = 32

// Parse parses a sequence diagram.
// maxAutonumber bounds autonumber's start and step, so that the numbers of
// a diagram within the event limit stay far from overflow.
const maxAutonumber = 1_000_000_000

func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{index: map[string]int{}, guard: g}
	// autonumber state: on, the next number and the step.
	numbering, nextNumber, numberStep := false, 1, 1
	// active counts the open activations of each participant; Mermaid
	// rejects deactivating one that is not active.
	active := map[int]int{}
	activation := func(kind EventKind, who, line int) error {
		if kind == Deactivate {
			if active[who] == 0 {
				return source.Errorf(line, "%q is deactivated but not active", d.Participants[who].ID)
			}
			active[who]--
		} else {
			active[who]++
		}
		d.Events = append(d.Events, Event{Kind: kind, From: who})
		return nil
	}
	box := -1 // open box
	creating := false
	var stack []string
	var stackLine []int // source line of each open block, for errors
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		if d.limitErr != nil {
			return nil, d.limitErr
		}
		// Every event counts, not only messages.
		if err := g.Count("events", len(d.Events)); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l.Text), ";"))
		kw, _, _ := strings.Cut(t, " ")
		switch {
		case kw == "autonumber":
			// autonumber [start [step]] or autonumber off.
			args := strings.Fields(t)[1:]
			if len(args) == 1 && args[0] == "off" {
				numbering = false
				continue
			}
			if len(args) > 2 {
				return nil, source.Errorf(l.No, "autonumber takes a start and a step, or off")
			}
			// As in Mermaid's renderer (start || current, step ||
			// current), a missing or zero start or step keeps the current
			// one: autonumber after autonumber off continues the count.
			numbering = true
			for k, a := range args {
				v, err := strconv.Atoi(a)
				if err != nil || v < -maxAutonumber || v > maxAutonumber {
					return nil, source.Errorf(l.No, "invalid autonumber value %q", a)
				}
				switch {
				case v == 0:
				case k == 0:
					nextNumber = v
				default:
					numberStep = v
				}
			}
			continue
		case kw == "title" || strings.HasPrefix(t, "title:"):
			d.Title = source.Label(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(t, "title"), ":")))
			continue
		case strings.HasPrefix(t, "accTitle") || strings.HasPrefix(t, "accDescr") || kw == "links" || kw == "link" || kw == "properties" || kw == "details":
			// NOTE(mermaid-compat): accessibility texts and menu links are
			// not drawn.
			continue
		case kw == "destroy":
			rest := strings.TrimSpace(t[len(kw):])
			if rest == "" {
				return nil, source.Errorf(l.No, "destroy needs a participant")
			}
			d.Events = append(d.Events, Event{Kind: Destroy, From: d.participant(rest)})
			continue
		case kw == "create":
			t = strings.TrimSpace(t[len(kw):])
			kw, _, _ = strings.Cut(t, " ")
			if kw != "participant" && kw != "actor" {
				return nil, source.Errorf(l.No, "create needs participant or actor")
			}
			creating = true
		}
		if m := participantRe.FindStringSubmatch(t); m != nil {
			id := strings.TrimSpace(m[2])
			kind := m[1]
			// Mermaid 11: participant A@{ "type": "database" }.
			if k := strings.Index(id, "@{"); k > 0 && strings.HasSuffix(id, "}") {
				attrs, err := source.ParseAttrs(id[k+2 : len(id)-1])
				if err != nil {
					return nil, source.Errorf(l.No, "invalid participant attributes: %v", err)
				}
				id = strings.TrimSpace(id[:k])
				// NOTE(mermaid-compat): keys other than type and alias are
				// ignored, and a type dam does not know is shown like the
				// others («type»), so that newer Mermaid input still draws.
				if typ, ok := attrs["type"].(string); ok && typ != "participant" {
					kind = strings.TrimSpace(typ)
				}
				if alias, ok := attrs["alias"].(string); ok && m[3] == "" {
					m[3] = alias
				}
			}
			label := id
			if m[3] != "" {
				label = m[3]
			}
			i := d.participant(id)
			d.Participants[i].Label = source.Label(label)
			d.Participants[i].Actor = kind == "actor"
			d.Participants[i].Type = ""
			if kind != "actor" && kind != "participant" {
				d.Participants[i].Type = kind
			}
			if box >= 0 && d.Participants[i].Box < 0 {
				d.Participants[i].Box = box
				d.Boxes[box].Members = append(d.Boxes[box].Members, i)
			}
			if creating {
				d.Events = append(d.Events, Event{Kind: Create, From: i})
				creating = false
			}
			if err := g.Count("participants", len(d.Participants)); err != nil {
				return nil, err
			}
			continue
		}
		if m := noteRe.FindStringSubmatch(t); m != nil {
			ev := Event{Kind: Note, Text: source.Label(m[3])}
			switch strings.ToLower(m[1]) {
			case "left of":
				ev.Place = LeftOf
			case "right of":
				ev.Place = RightOf
			default:
				ev.Place = Over
			}
			names := strings.Split(m[2], ",")
			if len(names) > 2 {
				return nil, source.Errorf(l.No, "a note can span at most two participants")
			}
			ev.From = d.participant(strings.TrimSpace(names[0]))
			ev.To = ev.From
			if len(names) == 2 {
				ev.To = d.participant(strings.TrimSpace(names[1]))
			}
			d.Events = append(d.Events, ev)
			continue
		}
		if k, ok := blockKeywords[kw]; ok {
			text := strings.TrimSpace(t[len(kw):])
			if k == BlockStart {
				if len(stack) >= maxBlockDepth {
					return nil, source.TooLarge(l.No, "blocks nested too deeply")
				}
				stack = append(stack, kw)
				stackLine = append(stackLine, l.No)
				if kw == "box" {
					if box >= 0 {
						return nil, source.Errorf(l.No, "boxes cannot be nested")
					}
					d.Boxes = append(d.Boxes, parseBox(text))
					box = len(d.Boxes) - 1
					continue
				}
			} else if len(stack) == 0 || stack[len(stack)-1] == "box" {
				return nil, source.Errorf(l.No, "%s outside of a block", kw)
			}
			if kw == "rect" || kw == "box" {
				text = ""
			}
			d.Events = append(d.Events, Event{Kind: k, Block: kw, Text: source.Label(text)})
			continue
		}
		if t == "end" {
			if len(stack) == 0 {
				return nil, source.Errorf(l.No, "end without block")
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			stackLine = stackLine[:len(stackLine)-1]
			if top == "box" {
				box = -1
			} else {
				d.Events = append(d.Events, Event{Kind: BlockEnd})
			}
			continue
		}
		if rest, ok := source.CutKeyword(t, "activate"); ok {
			if err := activation(Activate, d.participant(rest), l.No); err != nil {
				return nil, err
			}
			continue
		}
		if rest, ok := source.CutKeyword(t, "deactivate"); ok {
			if err := activation(Deactivate, d.participant(rest), l.No); err != nil {
				return nil, err
			}
			continue
		}
		ev, ok, err := d.message(t, l.No)
		if err != nil {
			return nil, err
		}
		if !ok {
			if _, isPar := source.CutKeyword(t, "par_over"); isPar || t == "par_over" {
				// NOTE(mermaid-compat): valid Mermaid, not drawn.
				return nil, source.Unsupported(l.No, "par_over blocks")
			}
			return nil, source.Errorf(l.No, "unrecognized statement %q", t)
		}
		if numbering {
			ev.Number, ev.Numbered = nextNumber, true
			nextNumber += numberStep
		}
		d.Events = append(d.Events, ev)
		if ev.ActFrom != 0 {
			k := Activate
			who := ev.To
			if ev.ActFrom < 0 {
				k, who = Deactivate, ev.From
			}
			if err := activation(k, who, l.No); err != nil {
				return nil, err
			}
		}
		if err := g.Count("messages", len(d.Events)); err != nil {
			return nil, err
		}
	}
	if d.limitErr != nil {
		return nil, d.limitErr
	}
	if err := g.Count("events", len(d.Events)); err != nil {
		return nil, err
	}
	if len(stack) > 0 {
		return nil, source.Errorf(stackLine[len(stackLine)-1], "%s block is not closed with end", stack[len(stack)-1])
	}
	if len(d.Participants) == 0 {
		return nil, source.Errorf(lines[0].No, "sequence diagram has no participants")
	}
	return d, nil
}

func (d *Diagram) participant(id string) int {
	if i, ok := d.index[id]; ok {
		return i
	}
	d.Participants = append(d.Participants, Participant{ID: id, Label: source.Label(id), Box: -1})
	d.index[id] = len(d.Participants) - 1
	if err := d.guard.Count("participants", len(d.Participants)); err != nil && d.limitErr == nil {
		d.limitErr = err
	}
	return len(d.Participants) - 1
}

func (d *Diagram) message(t string, line int) (Event, bool, error) {
	colon := strings.Index(t, ":")
	head := t
	text := ""
	if colon >= 0 {
		head, text = t[:colon], t[colon+1:]
	}
	for _, a := range arrows {
		i := strings.Index(head, a.op)
		if i <= 0 {
			continue
		}
		from := strings.TrimSpace(head[:i])
		to := strings.TrimSpace(head[i+len(a.op):])
		ev := Event{Kind: Message, Dotted: a.dotted, Head: a.head, BothEnds: a.both, Text: source.Label(text)}
		switch {
		case strings.HasPrefix(to, "+"):
			ev.ActFrom = 1
			to = strings.TrimSpace(to[1:])
		case strings.HasPrefix(to, "-"):
			ev.ActFrom = -1
			to = strings.TrimSpace(to[1:])
		}
		if from == "" || to == "" {
			return ev, false, source.Errorf(line, "message needs a sender and a receiver")
		}
		ev.From = d.participant(from)
		ev.To = d.participant(to)
		return ev, true, nil
	}
	return Event{}, false, nil
}

// DiagramTitle returns the title given with the title statement.
func (d *Diagram) DiagramTitle() string { return d.Title }
