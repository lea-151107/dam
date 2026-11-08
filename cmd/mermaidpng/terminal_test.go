package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/png"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/lea-151107/dam"
)

func renderFixture(t *testing.T, bg color.Color) image.Image {
	t.Helper()
	src, err := os.ReadFile("../../testdata/flowchart_subgraph.mmd")
	if err != nil {
		t.Fatal(err)
	}
	img, err := mermaid.Render(context.Background(), string(src), mermaid.Options{Background: bg})
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func samePixels(t *testing.T, a, b image.Image) {
	t.Helper()
	if a.Bounds() != b.Bounds() {
		t.Fatalf("bounds %v != %v", a.Bounds(), b.Bounds())
	}
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if color.NRGBAModel.Convert(a.At(x, y)) != color.NRGBAModel.Convert(b.At(x, y)) {
				t.Fatalf("pixel (%d,%d) differs", x, y)
			}
		}
	}
}

// TestSixel decodes the sixel stream and checks that every visible pixel is
// painted exactly once and transparent ones not at all.
func TestSixel(t *testing.T) {
	for _, bg := range []color.Color{color.White, nil} {
		img := renderFixture(t, bg)
		var buf bytes.Buffer
		if err := encodeSixel(context.Background(), &buf, img); err != nil {
			t.Fatal(err)
		}
		s := buf.Bytes()
		if !bytes.HasPrefix(s, []byte("\x1bP")) || !bytes.HasSuffix(s, []byte("\x1b\\")) {
			t.Fatal("missing DCS framing")
		}
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		painted := make([]int, w*h)
		i := bytes.IndexByte(s, 'q') + 1
		num := func() int {
			j := i
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			n, _ := strconv.Atoi(string(s[j:i]))
			return n
		}
		x, y := 0, 0
		paint := func(bits byte) {
			for d := 0; d < 6; d++ {
				if bits&(1<<d) == 0 {
					continue
				}
				if x >= w || y+d >= h {
					t.Fatalf("sixel at (%d,%d) outside %dx%d", x, y+d, w, h)
				}
				painted[(y+d)*w+x]++
			}
			x++
		}
		for i < len(s) && s[i] != 0x1b {
			switch c := s[i]; {
			case c == '"' || c == '#' || c == ';':
				i++
				num()
			case c == '$':
				x = 0
				i++
			case c == '-':
				x, y = 0, y+6
				i++
			case c == '!':
				i++
				n := num()
				ch := s[i]
				i++
				for k := 0; k < n; k++ {
					paint(ch - '?')
				}
			case c >= '?' && c <= '~':
				paint(c - '?')
				i++
			default:
				t.Fatalf("unexpected byte %q at %d", c, i)
			}
		}
		for p, n := range painted {
			_, _, _, a := img.At(img.Bounds().Min.X+p%w, img.Bounds().Min.Y+p/w).RGBA()
			want := 0
			if a>>8 >= sixelAlphaCutoff {
				want = 1
			}
			if n != want {
				t.Fatalf("background %v: pixel (%d,%d) painted %d times, want %d", bg, p%w, p/w, n, want)
			}
		}
	}
}

func TestITerm2(t *testing.T) {
	img := renderFixture(t, nil)
	var buf bytes.Buffer
	if err := encodeITerm2(&buf, img); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`^\x1b\]1337;File=inline=1;size=(\d+);[^:]*:([A-Za-z0-9+/=]+)\a$`).FindStringSubmatch(buf.String())
	if m == nil {
		t.Fatalf("malformed sequence: %.80q", buf.String())
	}
	data, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil {
		t.Fatal(err)
	}
	if strconv.Itoa(len(data)) != m[1] {
		t.Errorf("size=%s, payload is %d bytes", m[1], len(data))
	}
	got, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	samePixels(t, got, img)
}

func TestKitty(t *testing.T) {
	img := renderFixture(t, nil)
	var buf bytes.Buffer
	if err := encodeKitty(&buf, img); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`\x1b_G([^;]*);([A-Za-z0-9+/=]*)\x1b\\`)
	all := re.FindAllStringSubmatch(buf.String(), -1)
	if len(all) < 2 {
		t.Fatalf("want a chunked transfer, got %d escapes", len(all))
	}
	if strings.Join(re.Split(buf.String(), -1), "") != "" {
		t.Error("bytes outside graphics escapes")
	}
	var payload strings.Builder
	for i, m := range all {
		ctrl, chunk := m[1], m[2]
		if len(chunk) > kittyChunk {
			t.Errorf("chunk %d is %d bytes", i, len(chunk))
		}
		last := i == len(all)-1
		if wantM := map[bool]string{true: "m=0", false: "m=1"}[last]; !strings.Contains(ctrl, wantM) {
			t.Errorf("chunk %d control %q lacks %s", i, ctrl, wantM)
		}
		if !strings.Contains(ctrl, "q=2") {
			t.Errorf("chunk %d control %q lacks q=2: the terminal would reply", i, ctrl)
		}
		if i == 0 && !strings.Contains(ctrl, "f=100") {
			t.Errorf("first chunk control %q lacks f=100", ctrl)
		}
		payload.WriteString(chunk)
	}
	data, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	samePixels(t, got, img)
}

func TestDetectTerminal(t *testing.T) {
	tests := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"TERM": "xterm-kitty"}, outKitty},
		{map[string]string{"KITTY_WINDOW_ID": "1", "TERM": "xterm-256color"}, outKitty},
		{map[string]string{"TERM_PROGRAM": "ghostty"}, outKitty},
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, outITerm2},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, outITerm2},
		{map[string]string{"LC_TERMINAL": "iTerm2", "TERM": "screen"}, outITerm2},
		{map[string]string{"TERM": "foot"}, outSixel},
		{map[string]string{"TERM": "tmux-256color", "TERM_PROGRAM": "tmux", "KITTY_WINDOW_ID": "1", "TMUX": "/tmp/t,1,0"}, outKitty},
		{map[string]string{}, outSixel},
	}
	for _, tc := range tests {
		if got := detectTerminal(func(k string) string { return tc.env[k] }); got != tc.want {
			t.Errorf("%v: got %s, want %s", tc.env, got, tc.want)
		}
	}
}

func TestResolveFormat(t *testing.T) {
	if _, err := resolveFormat("bmp", func(string) string { return "" }); err == nil {
		t.Error("unknown format accepted")
	}
	if f, err := resolveFormat(outAuto, func(k string) string { return map[string]string{"TERM": "xterm-kitty"}[k] }); err != nil || f != outKitty {
		t.Errorf("auto: %s, %v", f, err)
	}
}

// TestTmuxPassthrough checks that inside tmux every kitty/iTerm2 escape is
// wrapped for passthrough, and that unwrapping gives back the original.
func TestTmuxPassthrough(t *testing.T) {
	img := renderFixture(t, nil)
	for _, format := range []string{outKitty, outITerm2, outSixel} {
		var plain, wrapped bytes.Buffer
		if err := writeImage(context.Background(), &plain, img, format, false); err != nil {
			t.Fatal(err)
		}
		if err := writeImage(context.Background(), &wrapped, img, format, true); err != nil {
			t.Fatal(err)
		}
		if format == outSixel {
			if !bytes.Equal(plain.Bytes(), wrapped.Bytes()) {
				t.Error("sixel should not be wrapped (tmux handles it natively)")
			}
			continue
		}
		// Inside passthrough an escaped ESC is doubled, so the payload is a
		// run of "ESC ESC" pairs and non-ESC bytes, ended by a lone ESC \.
		re := regexp.MustCompile(`\x1bPtmux;((?:\x1b\x1b|[^\x1b])*)\x1b\\(\n?)`)
		w := wrapped.String()
		if rest := re.ReplaceAllString(w, ""); rest != "" {
			t.Fatalf("%s: bytes outside passthrough: %q", format, rest[:min(len(rest), 40)])
		}
		var un strings.Builder
		for _, m := range re.FindAllStringSubmatch(w, -1) {
			un.WriteString(strings.ReplaceAll(m[1], "\x1b\x1b", "\x1b"))
			un.WriteString(m[2])
		}
		if un.String() != plain.String() {
			t.Errorf("%s: unwrapped stream differs from the plain one", format)
		}
	}
}

func TestEncodeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := writeImage(ctx, &bytes.Buffer{}, renderFixture(t, nil), outSixel, false); err == nil {
		t.Error("cancelled encoding succeeded")
	}
}

// TestWebSafeCube checks the index formula quantize relies on.
func TestWebSafeCube(t *testing.T) {
	for r := 0; r < 6; r++ {
		for g := 0; g < 6; g++ {
			for b := 0; b < 6; b++ {
				want := color.RGBA{uint8(r * 51), uint8(g * 51), uint8(b * 51), 0xff}
				if got := palette.WebSafe[r*36+g*6+b]; got != want {
					t.Fatalf("WebSafe[%d] = %v, want %v", r*36+g*6+b, got, want)
				}
			}
		}
	}
}

// TestTmuxLargeITerm2 checks that a large iTerm2 image inside tmux is split
// into multipart sequences that each stay under tmux's buffer limit, and
// that the parts reassemble into the original PNG.
func TestTmuxLargeITerm2(t *testing.T) {
	var src strings.Builder
	src.WriteString("sequenceDiagram\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&src, "P%d->>P%d: message number %d with some text\n", i%12, (i+5)%12, i)
	}
	// The image is meant to be large: allow more than DefaultMaxPixels.
	img, err := mermaid.Render(context.Background(), src.String(), mermaid.Options{Background: color.White, MaxPixels: 32 << 20})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := writeImage(context.Background(), &buf, img, outITerm2, true); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`\x1bPtmux;((?:\x1b\x1b|[^\x1b])*)\x1b\\`)
	seqs := re.FindAllStringSubmatch(buf.String(), -1)
	if len(seqs) < 3 {
		t.Fatalf("got %d sequences; want MultipartFile, FilePart…, FileEnd (output %d bytes)", len(seqs), buf.Len())
	}
	var payload strings.Builder
	for i, m := range seqs {
		if len(m[0]) > tmuxSeqLimit+64 {
			t.Errorf("sequence %d is %d bytes", i, len(m[0]))
		}
		body := strings.ReplaceAll(m[1], "\x1b\x1b", "\x1b")
		switch {
		case i == 0 && !strings.HasPrefix(body, "\x1b]1337;MultipartFile="):
			t.Fatalf("first sequence %.40q", body)
		case i == len(seqs)-1 && body != "\x1b]1337;FileEnd\a":
			t.Fatalf("last sequence %.40q", body)
		case i > 0 && i < len(seqs)-1:
			part, ok := strings.CutPrefix(body, "\x1b]1337;FilePart=")
			if !ok {
				t.Fatalf("sequence %d %.40q", i, body)
			}
			payload.WriteString(strings.TrimSuffix(part, "\a"))
		}
	}
	data, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	samePixels(t, got, img)
}
