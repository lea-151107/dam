// Command mermaidpng renders Mermaid source from stdin to a PNG on stdout,
// or with --out sixel/iterm2/kitty/auto, to an inline terminal image.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"

	"github.com/lea/mermaid"
)

func main() {
	width := flag.Int("width", 0, "maximum width in pixels")
	scale := flag.Float64("scale", 1, "scale factor")
	dark := flag.Bool("dark", false, "dark theme")
	transparent := flag.Bool("transparent", false, "transparent background")
	fontPath := flag.String("font", "", "TrueType/OpenType font file")
	fontSize := flag.Float64("fontsize", 14, "font size in pixels when --font is given (scales the whole drawing, like --scale)")
	text := flag.Int("text", -1, "render as text with this many columns instead of PNG (0 = unlimited)")
	out := flag.String("out", outPNG, "output format: png, sixel, iterm2, kitty, or auto (pick a terminal protocol from the environment)")
	timeout := flag.Duration("timeout", 10*time.Second, "render timeout")
	maxPixels := flag.Int("maxpixels", 0, "maximum image size in pixels before scaling to --width (0 = library default)")
	flag.Usage = usage
	flag.Parse()
	format, err := resolveFormat(*out, os.Getenv)
	if err != nil {
		fail(err)
	}
	if format != outPNG && *width == 0 {
		// A terminal cannot show an image of unbounded width, and encoding
		// one can produce tens of megabytes of escape sequences.
		*width = defaultTerminalWidth
	}

	src, err := io.ReadAll(os.Stdin)
	if err != nil {
		fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if *text >= 0 {
		lines, err := mermaid.RenderText(ctx, string(src), mermaid.TextOptions{MaxCols: *text})
		if err != nil {
			fail(err)
		}
		fmt.Println(strings.Join(lines, "\n"))
		return
	}

	opt := mermaid.Options{MaxWidth: *width, Scale: *scale, MaxPixels: *maxPixels}
	if *dark {
		opt.Foreground = color.NRGBA{0xe0, 0xe0, 0xe0, 0xff}
		opt.Background = color.NRGBA{0x1e, 0x1e, 0x1e, 0xff}
	} else {
		opt.Background = color.White
	}
	if *transparent {
		opt.Background = nil
	}
	if *fontPath != "" {
		f, err := loadFont(*fontPath)
		if err != nil {
			fail(err)
		}
		// The library asks FaceFunc for 14px × Scale and relies on getting
		// exactly that size (its readability limit is computed from it), so
		// --fontsize is expressed as a Scale; padding and lines scale along.
		if *fontSize > 0 {
			opt.Scale = float64(*scale * *fontSize / 14)
		}
		opt.FaceFunc = func(px float64) (font.Face, error) {
			return opentype.NewFace(f, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingNone})
		}
	}
	img, err := mermaid.Render(ctx, string(src), opt)
	if err != nil {
		fail(err)
	}
	if err := writeImage(ctx, os.Stdout, img, format, inTmux(os.Getenv)); err != nil {
		fail(err)
	}
}

// defaultTerminalWidth is the MaxWidth used for terminal output when --width
// is not given. The terminal's pixel size is not queried.
const defaultTerminalWidth = 1000

// usage prints the options with two dashes. The flag package accepts both
// -name and --name; the help shows the GNU-style form.
func usage() {
	w := flag.CommandLine.Output()
	fmt.Fprintf(w, "Usage: %s [options] < input.mmd\n\nOptions:\n", os.Args[0])
	flag.VisitAll(func(f *flag.Flag) {
		name, help := flag.UnquoteUsage(f)
		line := "  --" + f.Name
		if name != "" {
			line += " " + name
		}
		fmt.Fprintf(w, "%s\n    \t%s", line, strings.ReplaceAll(help, "\n", "\n    \t"))
		if def := f.DefValue; def != "" && def != "0" && def != "false" {
			if name == "string" {
				def = strconv.Quote(def)
			}
			fmt.Fprintf(w, " (default %s)", def)
		}
		fmt.Fprintln(w)
	})
}

func loadFont(path string) (*opentype.Font, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if f, err := opentype.Parse(b); err == nil {
		return f, nil
	}
	coll, err := opentype.ParseCollection(b)
	if err != nil {
		return nil, err
	}
	return coll.Font(0)
}

func fail(err error) {
	var se *mermaid.SyntaxError
	switch {
	case errors.As(err, &se):
		fmt.Fprintf(os.Stderr, "syntax error: %v\n", err)
	case errors.Is(err, mermaid.ErrUnsupported):
		fmt.Fprintf(os.Stderr, "unsupported: %v\n", err)
	default:
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(1)
}
