package mermaid

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/source"
)

// DiagramType identifies a kind of Mermaid diagram.
type DiagramType int

// Supported diagram types.
const (
	Unknown DiagramType = iota
	Flowchart
	Sequence
	Class
	State
	Pie
	Gantt
	GitGraph
	Mindmap
	ER
	Timeline
	Journey
	Quadrant
	XYChart
	Requirement
	C4
	Kanban
	Block
	Packet
	Sankey
	Radar
	Treemap
	Architecture
)

// String returns the type name.
func (t DiagramType) String() string {
	if k := kindOf(t); k != nil {
		return k.name
	}
	return "unknown"
}

// Detect returns the diagram type of src without parsing the body. It
// returns ErrUnsupported (wrapped) for diagram types that cannot be
// rendered, *SyntaxError when no diagram is present, and ErrTooLarge
// (with Unknown) for input over the size limit, which Render refuses too.
func Detect(src string) (DiagramType, error) {
	t, _, err := detect(src)
	return t, convertErr(err)
}

func detect(src string) (DiagramType, *source.Doc, error) {
	doc, err := source.SplitDoc(src)
	if err != nil {
		return Unknown, nil, err
	}
	lines := doc.Lines
	if len(lines) == 0 {
		return Unknown, nil, &source.SyntaxError{Line: 1, Msg: "no diagram found"}
	}
	kw, _ := source.Header(lines)
	t := keywordType(kw)
	if t == Unknown && kw == "---" {
		// Front matter after a directive or other text: a mistake in the
		// source, not a diagram type.
		return Unknown, nil, &source.SyntaxError{Line: lines[0].No, Msg: "front matter must come first"}
	}
	if t == Unknown {
		return Unknown, nil, fmt.Errorf("%w: diagram type %q", ErrUnsupported, truncateMsg(kw))
	}
	return t, doc, nil
}

// convertErr maps internal errors to the public error types.
func convertErr(err error) error {
	if err == nil {
		return nil
	}
	var se *source.SyntaxError
	if errors.As(err, &se) {
		return &SyntaxError{Line: se.Line, Msg: truncateMsg(se.Msg)}
	}
	if errors.Is(err, guard.ErrTooLarge) {
		detail := strings.TrimPrefix(err.Error(), guard.ErrTooLarge.Error()+": ")
		return fmt.Errorf("%w: %s", ErrTooLarge, truncateMsg(detail))
	}
	if errors.Is(err, source.ErrUnsupported) {
		detail := strings.TrimPrefix(err.Error(), source.ErrUnsupported.Error()+": ")
		return fmt.Errorf("%w: %s", ErrUnsupported, truncateMsg(detail))
	}
	return err
}

// maxMsgBytes bounds SyntaxError.Msg. Messages quote the offending source,
// which may be as long as the input; callers show them as one-line warnings.
const maxMsgBytes = 200

// truncateMsg cuts msg to at most maxMsgBytes plus an ellipsis, at a
// character boundary.
func truncateMsg(msg string) string {
	if len(msg) <= maxMsgBytes {
		return msg
	}
	n := maxMsgBytes
	for n > 0 && !utf8.RuneStart(msg[n]) {
		n--
	}
	return msg[:n] + "…"
}
