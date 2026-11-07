package mermaid

import (
	"errors"
	"fmt"
)

// ErrUnsupported is returned for input this library cannot draw: diagram
// types it does not render, diagram types without a text renderer
// (RenderText), and labels with characters the font has no glyph for
// (Render). Callers usually fall back to RenderText or to the source.
var ErrUnsupported = errors.New("mermaid: unsupported")

// ErrTooLarge is returned when the input exceeds a size limit (such as
// Options.MaxNodes, the input length, or RenderText's maxCols), or when the
// drawing cannot fit Options.MaxWidth with readable text. Callers usually
// fall back to RenderText or to showing the source.
var ErrTooLarge = errors.New("mermaid: diagram too large")

// SyntaxError reports a parse error. Line is 1-based and counts lines of the
// original source, including any front matter.
type SyntaxError struct {
	Line int
	Msg  string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("mermaid: line %d: %s", e.Line, e.Msg)
}
