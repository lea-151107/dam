// Package gitgraph parses and lays out Mermaid git graphs.
package gitgraph

import (
	"fmt"
	"hash/fnv"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/source"
)

// CommitType is the visual type of a commit.
type CommitType uint8

// Commit types.
const (
	Normal CommitType = iota
	Reverse
	Highlight
)

// Commit is one node of the graph.
type Commit struct {
	ID      string
	Branch  int
	Parents []int
	Tags    []string
	Type    CommitType
	Merge   bool
	Cherry  bool
}

// Branch is a named lane.
type Branch struct {
	Name  string
	Order int
	head  int
}

// Orientation of the graph.
type Orientation uint8

// Orientations.
const (
	LR Orientation = iota
	TB
	BT
)

// Diagram is a parsed git graph.
type Diagram struct {
	Orient   Orientation
	Commits  []Commit
	Branches []Branch
	index    map[string]int // branch name → index
	ids      map[string]int // commit id → index
}

var optRe = regexp.MustCompile(`(\w+)\s*:\s*("([^"]*)"|[^\s"]+)`)

// Parse parses a git graph.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{index: map[string]int{}, ids: map[string]int{}}
	_, rest := source.Header(lines)
	switch strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), ":")) {
	case "", "LR:", "LR":
	case "TB:", "TB":
		d.Orient = TB
	case "BT:", "BT":
		d.Orient = BT
	default:
		return nil, source.Errorf(lines[0].No, "unknown orientation %q", rest)
	}
	d.Branches = append(d.Branches, Branch{Name: "main", head: -1})
	d.index["main"] = 0
	cur := 0
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l.Text), ";"))
		kw, rest, _ := strings.Cut(t, " ")
		rest = strings.TrimSpace(rest)
		opts := map[string]string{}
		for _, m := range optRe.FindAllStringSubmatch(rest, -1) {
			v := m[2]
			if strings.HasPrefix(v, `"`) {
				v = m[3]
			}
			opts[m[1]] = v
		}
		switch kw {
		case "commit":
			c := Commit{Branch: cur}
			if h := d.Branches[cur].head; h >= 0 {
				c.Parents = []int{h}
			}
			if err := d.add(&c, opts, l.No); err != nil {
				return nil, err
			}
		case "branch":
			name := firstWord(rest)
			if name == "" {
				return nil, source.Errorf(l.No, "branch needs a name")
			}
			if _, ok := d.index[name]; ok {
				return nil, source.Errorf(l.No, "branch %q already exists", name)
			}
			order := len(d.Branches)
			if o, ok := opts["order"]; ok {
				// Parse as 64 bits and clamp, so that a large order sorts
				// the same on 32-bit platforms.
				n, err := strconv.ParseInt(o, 10, 64)
				if err != nil {
					return nil, source.Errorf(l.No, "invalid order %q", o)
				}
				order = int(min(max(n, math.MinInt32), math.MaxInt32))
			}
			d.Branches = append(d.Branches, Branch{Name: name, Order: order, head: d.Branches[cur].head})
			cur = len(d.Branches) - 1
			d.index[name] = cur
			if err := g.Count("branches", len(d.Branches)); err != nil {
				return nil, err
			}
		case "checkout", "switch":
			b, ok := d.index[firstWord(rest)]
			if !ok {
				return nil, source.Errorf(l.No, "unknown branch %q", firstWord(rest))
			}
			cur = b
		case "merge":
			b, ok := d.index[firstWord(rest)]
			if !ok {
				return nil, source.Errorf(l.No, "unknown branch %q", firstWord(rest))
			}
			if b == cur {
				return nil, source.Errorf(l.No, "cannot merge a branch into itself")
			}
			other := d.Branches[b].head
			if other < 0 {
				return nil, source.Errorf(l.No, "branch %q has no commits", d.Branches[b].Name)
			}
			head := d.Branches[cur].head
			if head == other {
				return nil, source.Errorf(l.No, "branch %q is already merged", d.Branches[b].Name)
			}
			c := Commit{Branch: cur, Merge: true}
			if head >= 0 {
				c.Parents = append(c.Parents, head)
			}
			c.Parents = append(c.Parents, other)
			if err := d.add(&c, opts, l.No); err != nil {
				return nil, err
			}
		case "cherry-pick":
			id := opts["id"]
			src, ok := d.ids[id]
			if !ok {
				return nil, source.Errorf(l.No, "unknown commit id %q", id)
			}
			if d.Commits[src].Branch == cur {
				return nil, source.Errorf(l.No, "cannot cherry-pick a commit from the same branch")
			}
			c := Commit{Branch: cur, Cherry: true, Tags: []string{"cherry-pick: " + id}}
			if h := d.Branches[cur].head; h >= 0 {
				c.Parents = []int{h}
			}
			delete(opts, "id")
			if err := d.add(&c, opts, l.No); err != nil {
				return nil, err
			}
		case "accTitle:", "accDescr:", "title":
			continue
		default:
			return nil, source.Errorf(l.No, "unrecognized statement %q", t)
		}
		if err := g.Count("commits", len(d.Commits)); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func firstWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return strings.Trim(f[0], `"`)
}

func (d *Diagram) add(c *Commit, opts map[string]string, line int) error {
	seq := len(d.Commits)
	c.ID = opts["id"]
	if c.ID == "" {
		// Deterministic replacement for Mermaid's random ids.
		h := fnv.New32a()
		fmt.Fprintf(h, "%d", seq)
		c.ID = fmt.Sprintf("%d-%07x", seq, h.Sum32()&0xfffffff)
	}
	if _, dup := d.ids[c.ID]; dup {
		return source.Errorf(line, "duplicate commit id %q", c.ID)
	}
	if tag := opts["tag"]; tag != "" {
		c.Tags = append(c.Tags, tag)
	}
	switch strings.ToUpper(opts["type"]) {
	case "", "NORMAL":
	case "REVERSE":
		c.Type = Reverse
	case "HIGHLIGHT":
		c.Type = Highlight
	default:
		return source.Errorf(line, "unknown commit type %q", opts["type"])
	}
	d.ids[c.ID] = seq
	d.Commits = append(d.Commits, *c)
	d.Branches[c.Branch].head = seq
	return nil
}
