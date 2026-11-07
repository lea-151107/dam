package mermaid

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font/basicfont"

	"github.com/lea/mermaid/internal/textgrid"
)

// FuzzRender feeds arbitrary input to every parser and layout. It calls the
// unexported render so that panics are not hidden by Render's recover.
func FuzzRender(f *testing.F) {
	files, _ := filepath.Glob("testdata/*.mmd")
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err == nil {
			f.Add(string(b))
		}
	}
	// Boundary numbers (MaxInt, overflow, subnormals), which random
	// mutation rarely builds digit by digit. (The packet overflows are in
	// TestReReview13Fixes: as seeds they steer the fuzzer to packets of
	// tens of thousands of bits, whose MaxPixels-sized downscale passes
	// the time limit below only without the parallel fuzz load.)
	for _, s := range []string{"graph", "sequenceDiagram\nA->>A:", "gantt\na:1d", "mindmap\n a\n  b\n c", "pie\n\"\":0",
		"treemap-beta\n\"a\"\n  \"b\": 5e-324", "pie\n\"a\": 1e308\n\"b\": 1e308", "block-beta\ncolumns 9223372036854775807\na"} {
		f.Add(s)
	}
	// One minimal source per registered diagram type, so that a new type
	// is fuzzed as soon as it is registered.
	for _, k := range kinds {
		f.Add(registrySamples[k.typ])
	}
	f.Fuzz(func(t *testing.T, src string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		start := time.Now()
		// The options vary with the input, so that the fit retries, the
		// shrink loop, a fixed Face and tiny widths are fuzzed too (a
		// state diagram panic was found only with other options).
		opt := fuzzOptions[len(src)%len(fuzzOptions)]
		img, err := render(ctx, src, opt)
		if time.Since(start) > 2*time.Second {
			t.Fatalf("render took %v", time.Since(start))
		}
		checkErr(t, src, err)
		if err == nil && img.Bounds().Dx() > opt.MaxWidth {
			t.Fatalf("image is %d px wide, MaxWidth is %d", img.Bounds().Dx(), opt.MaxWidth)
		}
		// renderText has no recover, so panics in the text renderers fail
		// the fuzz run instead of turning into an "internal error".
		start = time.Now()
		cols := []int{80, 40, 7}[len(src)%3]
		lines, err := renderText(ctx, src, TextOptions{MaxCols: cols, MaxNodes: 60})
		if time.Since(start) > 2*time.Second {
			t.Fatalf("renderText took %v", time.Since(start))
		}
		checkErr(t, src, err)
		for _, l := range lines {
			if w := textgrid.Width(l); w > cols {
				t.Fatalf("line %q is %d columns wide", l, w)
			}
		}
	})
}

// fuzzOptions are the Render options FuzzRender cycles through. All have
// a MaxWidth, which keeps the drawings small enough for the time limit.
var fuzzOptions = []Options{
	{MaxNodes: 60, MaxWidth: 300},
	{MaxNodes: 60, MaxWidth: 37, Scale: 2.3},
	{MaxNodes: 60, MaxWidth: 200, Face: basicfont.Face7x13},
	{MaxNodes: 60, MaxWidth: 120, Scale: 0.5},
}

// checkErr fails unless err is one of the documented errors, with a
// SyntaxError on a line of src (lines end at \n, \r\n or \r).
func checkErr(t *testing.T, src string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	err = convertErr(err)
	var se *SyntaxError
	switch {
	case errors.As(err, &se):
		n := strings.Count(strings.ReplaceAll(src, "\r\n", "\n"), "\n") + strings.Count(strings.ReplaceAll(src, "\r\n", ""), "\r") + 1
		if se.Line < 1 || se.Line > n {
			t.Fatalf("syntax error on line %d of %d: %v", se.Line, n, se)
		}
	case errors.Is(err, ErrUnsupported), errors.Is(err, ErrTooLarge),
		errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
	default:
		t.Fatalf("unexpected error type: %v", err)
	}
}
