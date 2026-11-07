package mermaid

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"

	"github.com/lea/mermaid/internal/outline"
	"github.com/lea/mermaid/internal/textmeasure"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fixtures returns the .mmd files in testdata.
func fixtures(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("testdata/*.mmd")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	return files
}

func readFile(t testing.TB, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func name(path string) string { return strings.TrimSuffix(filepath.Base(path), ".mmd") }

var lightOpts = Options{Background: color.White}

// jaFont is the Japanese subset font in testdata/fonts, parsed once.
var jaFont = sync.OnceValues(func() (*sfnt.Font, error) {
	b, err := os.ReadFile("testdata/fonts/NotoSansCJKjp-subset.otf")
	if err != nil {
		return nil, err
	}
	return opentype.Parse(b)
})

// optsFor returns the options for fixture f: lightOpts, plus the Japanese
// subset font for fixtures named *_ja.mmd (the built-in font has no
// Japanese glyphs). The face is drawn by internal/outline, so these
// goldens are the same on every architecture too.
func optsFor(t testing.TB, f string) Options {
	t.Helper()
	opt := lightOpts
	if strings.HasSuffix(name(f), "_ja") {
		jf, err := jaFont()
		if err != nil {
			t.Fatal(err)
		}
		opt.FaceFunc = func(px float64) (font.Face, error) { return outline.New(jf, px) }
	}
	return opt
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pixelsEqual(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if color.NRGBAModel.Convert(a.At(x, y)) != color.NRGBAModel.Convert(b.At(x, y)) {
				return false
			}
		}
	}
	return true
}

func TestGolden(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(name(f), func(t *testing.T) {
			img, err := Render(context.Background(), readFile(t, f), optsFor(t, f))
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "golden", name(f)+".png")
			if *update {
				if err := os.WriteFile(golden, encodePNG(t, img), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			fh, err := os.Open(golden)
			if err != nil {
				t.Fatalf("missing golden (run go test -update): %v", err)
			}
			defer fh.Close()
			want, err := png.Decode(fh)
			if err != nil {
				t.Fatal(err)
			}
			if !pixelsEqual(img, want) {
				t.Errorf("output differs from %s (run go test -update and inspect the diff)", golden)
			}
		})
	}
}

// terminalProfiles are the options a terminal viewer such as gvmd passes:
// a MaxWidth, a transparent background, and the terminal's colors. The
// goldens of TestGolden use none of them, so changes to the MaxWidth
// strategy (fitAttempts, font shrinking, downscaling) or to the colors
// derived from Foreground would pass them unnoticed, and Version would not
// be bumped.
var terminalProfiles = []struct {
	name string
	opt  Options
	// fixedFace passes the fixture's font as a fixed Options.Face (the
	// RFP's API) instead of FaceFunc, so that the text cannot shrink and
	// the last-resort downscale to MaxWidth is covered.
	fixedFace bool
}{
	// A dark terminal: white text, larger cells.
	{"dark800", Options{MaxWidth: 800, Scale: 1.5, Foreground: color.White}, false},
	// A narrow pane with the default colors.
	{"narrow300", Options{MaxWidth: 300}, false},
	// A narrow pane with a fixed Face.
	{"face300", Options{MaxWidth: 300}, true},
	// An opaque background (sixel) and text smaller than the default.
	{"bg07", Options{Scale: 0.7, Background: color.NRGBA{0x1e, 0x1e, 0x2e, 0xff}, Foreground: color.NRGBA{0xcd, 0xd6, 0xf4, 0xff}}, false},
}

// TestGoldenTerminal renders every fixture with terminalProfiles. The
// golden is testdata/golden/<fixture>@<profile>.png, or .err with the
// error text when the drawing does not fit (ErrTooLarge), so that a change
// between fitting and not fitting is caught too.
func TestGoldenTerminal(t *testing.T) {
	for _, f := range fixtures(t) {
		for _, p := range terminalProfiles {
			t.Run(name(f)+"@"+p.name, func(t *testing.T) {
				opt := p.opt
				opt.FaceFunc = optsFor(t, f).FaceFunc
				if p.fixedFace {
					newFace := opt.FaceFunc
					if newFace == nil {
						newFace = textmeasure.BuiltinFace
					}
					face, err := newFace(textmeasure.BaseFontSizePx)
					if err != nil {
						t.Fatal(err)
					}
					opt.Face, opt.FaceFunc = face, nil
				}
				img, err := Render(context.Background(), readFile(t, f), opt)
				base := filepath.Join("testdata", "golden", name(f)+"@"+p.name)
				var got []byte
				switch {
				case errors.Is(err, ErrTooLarge):
					got = []byte(err.Error() + "\n")
					base += ".err"
				case err != nil:
					t.Fatal(err)
				default:
					if w := img.Bounds().Dx(); opt.MaxWidth > 0 && w > opt.MaxWidth {
						t.Errorf("width %d exceeds MaxWidth %d", w, opt.MaxWidth)
					}
					base += ".png"
				}
				if *update {
					// Remove the golden of the other kind, if the case
					// changed between fitting and not fitting.
					other := strings.TrimSuffix(strings.TrimSuffix(base, ".err"), ".png")
					for _, ext := range []string{".png", ".err"} {
						if other+ext != base {
							if err := os.Remove(other + ext); err != nil && !os.IsNotExist(err) {
								t.Fatal(err)
							}
						}
					}
					if got == nil {
						got = encodePNG(t, img)
					}
					if err := os.WriteFile(base, got, 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				if got != nil {
					if want, rerr := os.ReadFile(base); rerr != nil || string(want) != string(got) {
						t.Errorf("got %q, want %s (run go test -update and inspect the diff)", got, base)
					}
					return
				}
				fh, err := os.Open(base)
				if err != nil {
					t.Fatalf("missing golden (run go test -update): %v", err)
				}
				defer fh.Close()
				want, err := png.Decode(fh)
				if err != nil {
					t.Fatal(err)
				}
				if !pixelsEqual(img, want) {
					t.Errorf("output differs from %s (run go test -update and inspect the diff)", base)
				}
			})
		}
	}
}

// textGoldenCols are the MaxCols of the RenderText goldens. 120 is
// <fixture>.txt; the narrower ones, which take the wrapping and compaction
// paths a terminal of about 80 columns uses, are <fixture>@<cols>.txt, or
// .err with the error text when the output does not fit (ErrTooLarge).
var textGoldenCols = []int{120, 80, 40}

func TestGoldenText(t *testing.T) {
	for _, f := range fixtures(t) {
		src := readFile(t, f)
		if typ, _ := Detect(src); !hasText(typ) {
			continue
		}
		for _, cols := range textGoldenCols {
			base := filepath.Join("testdata", "golden", name(f))
			if cols != 120 {
				base += "@" + strconv.Itoa(cols)
			}
			t.Run(filepath.Base(base), func(t *testing.T) {
				lines, err := RenderText(context.Background(), src, TextOptions{MaxCols: cols})
				golden, got := base+".txt", ""
				switch {
				case cols != 120 && errors.Is(err, ErrTooLarge):
					golden, got = base+".err", err.Error()+"\n"
				case err != nil:
					t.Fatal(err)
				default:
					got = strings.Join(lines, "\n") + "\n"
				}
				if *update {
					for _, ext := range []string{".txt", ".err"} {
						if base+ext != golden {
							if err := os.Remove(base + ext); err != nil && !os.IsNotExist(err) {
								t.Fatal(err)
							}
						}
					}
					if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				if want := readFile(t, golden); got != want {
					t.Errorf("text output differs from %s:\n%s", golden, got)
				}
				for _, l := range lines {
					if w := displayWidth(l); w > cols {
						t.Errorf("line is %d columns wide: %q", w, l)
					}
				}
			})
		}
	}
}

func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x1100 && (r <= 0x115f || (r >= 0x2e80 && r <= 0xa4cf) || (r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) || (r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6)) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func TestDeterministic(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(name(f), func(t *testing.T) {
			src := readFile(t, f)
			opt := optsFor(t, f)
			const runs = 4
			sums := make([][32]byte, runs)
			var wg sync.WaitGroup
			errs := make([]error, runs)
			for i := 0; i < runs; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					img, err := Render(context.Background(), src, opt)
					if err != nil {
						errs[i] = err
						return
					}
					sums[i] = sha256.Sum256(img.(*image.RGBA).Pix)
				}(i)
			}
			wg.Wait()
			for i := 0; i < runs; i++ {
				if errs[i] != nil {
					t.Fatal(errs[i])
				}
				if sums[i] != sums[0] {
					t.Fatalf("run %d produced different pixels", i)
				}
			}
		})
	}
}

func TestMaxWidth(t *testing.T) {
	for _, f := range fixtures(t) {
		for _, w := range []int{200, 400, 640} {
			opt := optsFor(t, f)
			opt.MaxWidth = w
			img, err := Render(context.Background(), readFile(t, f), opt)
			if w < 400 && errors.Is(err, ErrTooLarge) {
				// Allowed: the drawing would need text below minTextPx.
				t.Logf("%s at %d: %v", name(f), w, err)
				continue
			}
			if err != nil {
				t.Fatalf("%s at %d: %v", name(f), w, err)
			}
			if got := img.Bounds().Dx(); got > w {
				t.Errorf("%s: width %d exceeds MaxWidth %d", name(f), got, w)
			}
		}
	}
}

func TestThemesAndScale(t *testing.T) {
	src := readFile(t, "testdata/flowchart_basic.mmd")
	tests := []struct {
		name string
		opt  Options
	}{
		{"transparent", Options{}},
		{"dark", Options{Foreground: color.NRGBA{0xee, 0xee, 0xee, 0xff}, Background: color.Black}},
		{"dark-bg-only", Options{Background: color.NRGBA{0x10, 0x10, 0x10, 0xff}}},
		{"scale2", Options{Scale: 2, Background: color.White}},
	}
	base, err := Render(context.Background(), src, lightOpts)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			img, err := Render(context.Background(), src, tc.opt)
			if err != nil {
				t.Fatal(err)
			}
			if tc.opt.Background == nil {
				if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
					t.Errorf("corner alpha = %d, want transparent", a)
				}
			}
			if tc.opt.Scale == 2 {
				if got, want := img.Bounds().Dx(), 2*base.Bounds().Dx(); got < want-4 || got > want+4 {
					t.Errorf("scaled width %d, want about %d", got, want)
				}
			}
		})
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		src  string
		want DiagramType
		err  error
	}{
		{"graph TD\nA-->B", Flowchart, nil},
		{"flowchart LR\nA-->B", Flowchart, nil},
		{"---\ntitle: x\n---\n%% c\nsequenceDiagram\nA->>B: hi", Sequence, nil},
		{"%%{init: {}}%%\nclassDiagram\nA <|-- B", Class, nil},
		{"stateDiagram-v2\n[*] --> A", State, nil},
		{"pie\n\"a\": 1", Pie, nil},
		{"gantt\nx :2020-01-01, 1d", Gantt, nil},
		{"gitGraph\ncommit", GitGraph, nil},
		{"mindmap\nroot", Mindmap, nil},
		{"erDiagram\nA ||--o{ B : has", ER, nil},
		{"journey\ntitle x", Journey, nil},
		{"zenuml\nA.b()", Unknown, ErrUnsupported},
	}
	for _, tc := range tests {
		got, err := Detect(tc.src)
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("Detect(%q) = %v, %v; want %v, %v", tc.src, got, err, tc.want, tc.err)
		}
	}
	var se *SyntaxError
	if _, err := Detect("  \n\n%% only comments\n"); !errors.As(err, &se) {
		t.Errorf("empty input: got %v, want SyntaxError", err)
	}
}

func TestSyntaxErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		line int
	}{
		{"flowchart-unclosed-subgraph", "graph TD\nsubgraph one\nA-->B\n", 2},
		{"flowchart-bad-shape", "graph TD\nA[oops --> B\n", 2},
		{"flowchart-front-matter", "---\ntitle: t\n---\ngraph TD\nA-->B\nA -- text\n", 6},
		{"flowchart-bad-direction", "graph XY\nA-->B", 1},
		{"sequence-bad", "sequenceDiagram\nA->>B: ok\nthis is wrong\n", 3},
		{"sequence-end", "sequenceDiagram\nA->>B: ok\nend\n", 3},
		{"sequence-unclosed", "sequenceDiagram\nloop x\nA->>B: ok\n", 2},
		{"class-unclosed", "classDiagram\nclass A {\n+x\n", 2},
		{"class-bad", "classDiagram\n???\n", 2},
		{"state-unclosed", "stateDiagram\nstate A {\nB --> C\n", 2},
		{"state-bad", "stateDiagram\nA -> B\n", 2},
		{"pie-bad", "pie\n\"a\" : x\n", 2},
		{"pie-negative", "pie\n\"a\" : -1\n", 2},
		{"gantt-unknown-id", "gantt\ndateFormat YYYY-MM-DD\nt : after nope, 1d\n", 3},
		{"gantt-cycle", "gantt\na : x, after y, 1d\nb : y, after x, 1d\n", 2},
		{"git-unknown-branch", "gitGraph\ncommit\ncheckout nope\n", 3},
		{"git-merge-self", "gitGraph\ncommit\nmerge main\n", 3},
		{"mindmap-two-roots", "mindmap\nroot\nother\n", 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Render(context.Background(), tc.src, Options{})
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("got %v, want *SyntaxError", err)
			}
			if se.Line != tc.line {
				t.Errorf("line = %d, want %d (%v)", se.Line, tc.line, se)
			}
			if errors.Is(err, ErrUnsupported) {
				t.Error("syntax error must not match ErrUnsupported")
			}
		})
	}
}

func TestLimits(t *testing.T) {
	var b strings.Builder
	b.WriteString("graph TD\n")
	for i := 0; i < 60; i++ {
		b.WriteString("n")
		b.WriteString(strings.Repeat("x", i%5))
		b.WriteString(string(rune('a'+i%26)) + string(rune('a'+i/26)))
		b.WriteString(" --> m\n")
	}
	_, err := Render(context.Background(), b.String(), Options{MaxNodes: 20})
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("MaxNodes: got %v, want ErrTooLarge", err)
	}
	if _, err := Render(context.Background(), b.String(), Options{}); err != nil {
		t.Errorf("default limit: %v", err)
	}
	huge := "graph TD\n" + strings.Repeat("A-->B\n", 300000)
	if _, err := Render(context.Background(), huge, Options{}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("huge input: got %v, want ErrTooLarge", err)
	}
}

func TestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := readFile(t, "testdata/flowchart_basic.mmd")
	if _, err := Render(ctx, src, Options{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Render: got %v, want context.Canceled", err)
	}
	if _, err := RenderText(ctx, src, TextOptions{MaxCols: 80}); !errors.Is(err, context.Canceled) {
		t.Errorf("RenderText: got %v, want context.Canceled", err)
	}

	// A large dense graph must stop at the deadline.
	var b strings.Builder
	b.WriteString("graph TD\n")
	for i := 0; i < 400; i++ {
		b.WriteString("a")
		b.WriteString(itoa(i % 40))
		b.WriteString(" --> b")
		b.WriteString(itoa((i * 7) % 40))
		b.WriteString("\n")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel2()
	time.Sleep(2 * time.Millisecond)
	if _, err := Render(ctx2, b.String(), Options{MaxNodes: 1000}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("deadline: got %v, want context.DeadlineExceeded", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for ; i > 0; i /= 10 {
		d = append([]byte{byte('0' + i%10)}, d...)
	}
	return string(d)
}

func TestRenderTextUnsupported(t *testing.T) {
	_, err := RenderText(context.Background(), "zenuml\nA.b()", TextOptions{MaxCols: 80})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("got %v, want ErrUnsupported", err)
	}
	_, err = RenderText(context.Background(), readFile(t, "testdata/flowchart_basic.mmd"), TextOptions{MaxCols: 10})
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("narrow: got %v, want ErrTooLarge", err)
	}
}

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}
