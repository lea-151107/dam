package mermaid

import (
	"image/color"

	"golang.org/x/image/font"
)

// DefaultMaxNodes is used when Options.MaxNodes or TextOptions.MaxNodes
// is 0.
const DefaultMaxNodes = 500

// DefaultMaxPixels is used when Options.MaxPixels is 0. It is 4096×4096,
// which keeps one RGBA image at 64 MiB.
const DefaultMaxPixels = 4096 * 4096

// Options controls rendering.
type Options struct {
	// MaxWidth is the maximum image width in pixels (0 = unlimited). The
	// layout is compacted, labels are wrapped and the text size is reduced
	// (built-in font or FaceFunc), down to about 5px. If the drawing still
	// does not fit, the image is scaled down, but only as long as the text
	// stays at least about 5px (or the reduction is under 10%); otherwise
	// Render returns ErrTooLarge rather than an unreadable image.
	MaxWidth int
	// Scale multiplies all sizes (0 means 1). With the built-in font the
	// text size is 14px × Scale. Values are clamped to the range 1/16 to 16.
	Scale float64
	// Face is used for all text. nil selects the built-in Go Regular font,
	// which covers Latin, Greek and Cyrillic only. A label with a character
	// the font has no glyph for (Japanese with the built-in font, say)
	// makes Render return ErrUnsupported instead of drawing empty boxes;
	// pass a font that has it with FaceFunc. A font.Face is not safe for concurrent
	// use, so do not share one between concurrent Render calls.
	//
	// A fixed Face cannot be made smaller, so when a diagram does not fit
	// MaxWidth even after compaction the whole image is scaled down as a
	// last resort. Use FaceFunc to avoid that.
	Face font.Face
	// FaceFunc, when set, takes precedence over Face. It must return a face
	// of the given size in pixels (for example opentype.NewFace with
	// DPI 72). The text size is 14px × Scale, and it is reduced instead of
	// scaling the image when the diagram does not fit MaxWidth. Each Render
	// call creates its own faces, so concurrent calls are safe if FaceFunc
	// returns a new face on every call.
	FaceFunc func(sizePx float64) (font.Face, error)
	// FontFunc, when set, takes precedence over FaceFunc and Face. It is
	// called with each FontStyle and returns the face for bold and italic
	// text (Markdown strings, <b> and <i> in labels) as well as regular
	// text; returning the regular face for a style the font family lacks
	// is fine. Like FaceFunc it must return a new face for every call when
	// Render is called concurrently. With FaceFunc or Face, all styles use
	// the same face.
	FontFunc func(sizePx float64, style FontStyle) (font.Face, error)
	// Foreground is the text and line color (nil picks a default suited to
	// Background). With Background nil too there is nothing to go by and
	// the colors suit a light background, so a transparent image for a dark
	// terminal needs Foreground.
	Foreground color.Color
	// Background fills the canvas; nil leaves it transparent. A non-nil
	// color is always drawn opaque: its alpha is ignored, so
	// color.Transparent gives black. Use nil for a transparent image.
	Background color.Color
	// MaxNodes limits the number of nodes, edges, messages and similar
	// elements (0 = DefaultMaxNodes).
	MaxNodes int
	// MaxPixels limits the width × height of the image before any final
	// downscaling to MaxWidth (0 = DefaultMaxPixels). Larger drawings
	// return ErrTooLarge before the image is allocated. Each pixel takes
	// 4 bytes.
	MaxPixels int
}

// TextOptions controls RenderText.
type TextOptions struct {
	// MaxCols is the maximum line width in terminal columns, with East
	// Asian wide characters counting as two (0 = unlimited). Wider output
	// returns ErrTooLarge.
	MaxCols int
	// MaxNodes limits the number of elements as in Options
	// (0 = DefaultMaxNodes).
	MaxNodes int
}

// FontStyle selects a face in Options.FontFunc.
type FontStyle int

// Font styles.
const (
	Regular FontStyle = iota
	Bold
	Italic
	BoldItalic
)

// String returns the style name.
func (s FontStyle) String() string {
	switch s {
	case Regular:
		return "regular"
	case Bold:
		return "bold"
	case Italic:
		return "italic"
	case BoldItalic:
		return "bold italic"
	}
	return "unknown"
}
