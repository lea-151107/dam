package mermaid

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/lea-151107/dam/internal/guard"
	"github.com/lea-151107/dam/internal/source"
	"github.com/lea-151107/dam/internal/textgrid"
	"github.com/lea-151107/dam/internal/textmeasure"
	"github.com/lea-151107/dam/internal/theme"
)

// TestMaxNodesEverywhere checks that every way of creating an element is
// counted, including implicit creation by class/style/note/member lines.
func TestMaxNodesEverywhere(t *testing.T) {
	ids := func(prefix string, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("%s%d", prefix, i)
		}
		return out
	}
	lines := func(format string, n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, format+"\n", i)
		}
		return b.String()
	}
	const n = 60
	tests := []struct{ name, src string }{
		{"flowchart-class", "graph TD\nclass " + strings.Join(ids("n", n), ",") + " c\n"},
		{"flowchart-style", "graph TD\n" + lines("style n%d fill:#f00", n)},
		{"flowchart-edges", "graph TD\n" + strings.Join(ids("a", 10), " & ") + " --> " + strings.Join(ids("b", 10), " & ") + "\n"},
		{"class-members", "classDiagram\n" + lines("C%d : +x", n)},
		{"class-notes", "classDiagram\n" + lines(`note for C%d "x"`, n)},
		{"sequence-notes", "sequenceDiagram\n" + lines("Note over p%d: x", n)},
		{"sequence-activate", "sequenceDiagram\n" + lines("activate p%d", n)},
		{"state-style", "stateDiagram\n" + lines("style s%d fill:#f00", n)},
		{"state-desc", "stateDiagram\n" + lines("s%d : x", n)},
		{"mindmap", "mindmap\nroot\n" + lines("  c%d", n)},
		{"gantt", "gantt\n" + lines("t%d : 2020-01-01, 1d", n)},
		{"pie", "pie\n" + lines(`"s%d" : 1`, n)},
		{"gitgraph", "gitGraph\n" + lines(`commit id: "c%d"`, n)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Render(context.Background(), tc.src, Options{MaxNodes: 40})
			if !errors.Is(err, ErrTooLarge) {
				t.Fatalf("got %v, want ErrTooLarge", err)
			}
			if strings.Count(err.Error(), "too large") != 1 {
				t.Errorf("message repeats itself: %v", err)
			}
		})
	}
}

// TestCancelLatency checks that a cancelled render returns promptly even
// when single loop iterations are expensive.
func TestCancelLatency(t *testing.T) {
	var b strings.Builder
	b.WriteString("gantt\ndateFormat YYYY-MM-DD\nexcludes weekends\n")
	for i := 0; i < 450; i++ {
		fmt.Fprintf(&b, "t%d : a%d, 2000-01-01, 50000d\n", i, i)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Render(ctx, b.String(), Options{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 250*time.Millisecond {
		t.Errorf("returned %v after a 50ms deadline", d)
	}
}

// TestRenderTextContract checks every fixture at a narrow width: either all
// lines fit and contain no line breaks, or ErrTooLarge is returned.
func TestRenderTextContract(t *testing.T) {
	extra := []string{
		"pie title A very long pie chart title that does not fit<br>second line\n\"a\" : 1\n",
		"gantt\ntitle Long gantt title that needs wrapping\nsection A very long section name exceeding the width\nt : 2020-01-01, 1d\n",
		"graph TD\nsubgraph S [first<br>second]\na --> b\nend\n",
		"sequenceDiagram\nA->>B: hi\nNote right of B: a long note on the last participant\nB->>B: a long self message on the last one\n",
	}
	srcs := extra
	for _, f := range fixtures(t) {
		srcs = append(srcs, readFile(t, f))
	}
	for i, src := range srcs {
		for _, cols := range []int{20, 40, 80, 0} {
			lines, err := RenderText(context.Background(), src, TextOptions{MaxCols: cols})
			if errors.Is(err, ErrUnsupported) || errors.Is(err, ErrTooLarge) {
				continue
			}
			if err != nil {
				t.Fatalf("source %d cols %d: %v", i, cols, err)
			}
			for _, l := range lines {
				if strings.ContainsAny(l, "\n\r") {
					t.Errorf("source %d: line contains a line break: %q", i, l)
				}
				if cols > 0 && textgrid.Width(l) > cols {
					t.Errorf("source %d: line is %d columns, limit %d: %q", i, textgrid.Width(l), cols, l)
				}
			}
		}
	}
	// Nothing may be cut off: the long note must appear in full.
	lines, err := RenderText(context.Background(), extra[3], TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(lines, "\n")
	for _, want := range []string{"a long note on the last participant", "a long self message on the last one"} {
		if !strings.Contains(all, want) {
			t.Errorf("text output lost %q:\n%s", want, all)
		}
	}
}

// TestEdgeToOwnSubgraph checks that an edge between a node and the subgraph
// containing it is drawn rather than dropped.
func TestEdgeToOwnSubgraph(t *testing.T) {
	src := "graph TD\nsubgraph S\n a --> S\n b\nend\n"
	withEdge, err := Render(context.Background(), src, lightOpts)
	if err != nil {
		t.Fatal(err)
	}
	without, err := Render(context.Background(), "graph TD\nsubgraph S\n a\n b\nend\n", lightOpts)
	if err != nil {
		t.Fatal(err)
	}
	if pixelsEqual(withEdge, without) {
		t.Error("edge a --> S was not drawn")
	}
	lines, err := RenderText(context.Background(), src, TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "┬") {
		t.Errorf("text output has no connection from a to S:\n%s", strings.Join(lines, "\n"))
	}
}

// TestGanttFitsWithoutShrinking checks that a gantt chart whose labels stick
// out on the right is laid out within MaxWidth at full text size (the image
// height stays that of the unconstrained layout).
func TestGanttFitsWithoutShrinking(t *testing.T) {
	src := readFile(t, "testdata/gantt_basic.mmd")
	img, err := Render(context.Background(), src, Options{MaxWidth: 800, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	full, err := Render(context.Background(), src, Options{Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() > 800 {
		t.Fatalf("width %d > 800", img.Bounds().Dx())
	}
	if img.Bounds().Dy() != full.Bounds().Dy() {
		t.Errorf("height %d differs from unconstrained %d: text was shrunk or wrapped", img.Bounds().Dy(), full.Bounds().Dy())
	}
}

// TestFaceFunc checks that a resizable external font shrinks text instead
// of falling back to scaling the bitmap.
func TestFaceFunc(t *testing.T) {
	calls := 0
	// The same face as the built-in font, so that the two paths can be
	// compared pixel for pixel.
	ff := func(px float64) (font.Face, error) {
		calls++
		return textmeasure.BuiltinFace(px)
	}
	src := readFile(t, "testdata/flowchart_subgraph.mmd")
	img, err := Render(context.Background(), src, Options{MaxWidth: 300, FaceFunc: ff, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() > 300 {
		t.Errorf("width %d > 300", img.Bounds().Dx())
	}
	if calls < 2 {
		t.Errorf("FaceFunc called %d times; text was not resized", calls)
	}
	// Same pixels as the built-in font path, which uses the same font.
	builtin, err := Render(context.Background(), src, Options{MaxWidth: 300, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	if !pixelsEqual(img, builtin) {
		t.Error("FaceFunc with Go Regular differs from the built-in font")
	}
}

func TestGanttExcludesLine(t *testing.T) {
	_, err := Render(context.Background(), "gantt\ndateFormat YYYY-MM-DD\nt : 2020-01-01, 1d\nexcludes 2020-13-45\n", Options{})
	var se *SyntaxError
	if !errors.As(err, &se) || se.Line != 4 {
		t.Errorf("got %v, want syntax error at line 4", err)
	}
}

func TestGanttLongSpan(t *testing.T) {
	src := "gantt\ndateFormat YYYY-MM-DD\nt1 : a, 1900-01-01, 36000d\nt2 : b, after a, 36000d\nt3 : c, after b, 36000d\nt4 : d, after c, 36000d\n"
	lines, err := RenderText(context.Background(), src, TextOptions{MaxCols: 70})
	if err != nil {
		t.Fatal(err)
	}
	// Four equal tasks: each bar should be about a quarter of the chart.
	for _, l := range lines {
		if strings.HasPrefix(l, "t4") {
			if n := strings.Count(l, "█"); n < 10 {
				t.Errorf("last task bar has %d cells, want about 15: %q", n, l)
			}
		}
	}
}

func TestNestedEmptyComposite(t *testing.T) {
	src := "stateDiagram\nstate A {\nstate B {\n}\n}\nX --> A\n"
	img, err := Render(context.Background(), src, lightOpts)
	if err != nil {
		t.Fatal(err)
	}
	// A wrong layout drew A and B twice side by side; the fixed one is a
	// single column.
	if img.Bounds().Dx() > 200 {
		t.Errorf("width %d suggests duplicated states", img.Bounds().Dx())
	}
}

func TestWrapWideChar(t *testing.T) {
	done := make(chan []string, 1)
	go func() { done <- textgrid.Wrap("日本", 1) }()
	select {
	case got := <-done:
		if strings.Join(got, "") != "日本" {
			t.Errorf("Wrap lost text: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wrap did not return")
	}
}

func TestGridWideOverwrite(t *testing.T) {
	g, err := textgrid.New(6, 1)
	if err != nil {
		t.Fatal(err)
	}
	g.Text(0, 0, "日本語")
	g.Set(1, 0, 'x') // overwrite the second half of 日
	line := g.Lines()[0]
	if w := textgrid.Width(line); w > 6 {
		t.Errorf("row is %d columns wide after overwrite: %q", w, line)
	}
	if !utf8.ValidString(line) {
		t.Error("invalid UTF-8")
	}
}

// TestTextControlChars checks that control characters, written raw or as
// entities, never reach RenderText output.
func TestTextControlChars(t *testing.T) {
	srcs := []string{
		"graph TD\nA[x#13;y]-->B",
		"graph LR\nA[x#27;[2Jy] --> B",
		"graph LR\nA[x\x1b[2Jy] --> B",
		"graph LR\nA[x\ty] --> B",
		"graph TD\nA[x\u2028y\u0085z]-->B",
		"pie title x#13;y\n\"a#13;b\" : 1",
		"gantt\ntitle t#27;x\nsection s#13;x\nta#7;sk : 2020-01-01, 1d\n",
		"sequenceDiagram\nA->>B: hi#27;[2J\nNote right of B: n#13;x\n",
	}
	for i, src := range srcs {
		lines, err := RenderText(context.Background(), src, TextOptions{})
		if err != nil {
			t.Fatalf("source %d: %v", i, err)
		}
		for _, l := range lines {
			if strings.IndexFunc(l, func(r rune) bool { return r < 0x20 || (r >= 0x7f && r < 0xa0) || r == 0x2028 || r == 0x2029 }) >= 0 {
				t.Errorf("source %d: control character in %q", i, l)
			}
		}
	}
}

// TestTextEmojiWidth checks that emoji with East Asian Width W take two
// cells, so box borders line up in a terminal.
func TestTextEmojiWidth(t *testing.T) {
	for _, r := range []rune{'🚀', '✅', '⌚', '⏩', '☔', '日', 'Ａ'} {
		if w := textgrid.RuneWidth(r); w != 2 {
			t.Errorf("RuneWidth(%U) = %d, want 2", r, w)
		}
	}
	for _, r := range []rune{'a', 'é', '─', '│', '▶', '⚠'} {
		if w := textgrid.RuneWidth(r); w != 1 {
			t.Errorf("RuneWidth(%U) = %d, want 1", r, w)
		}
	}
	lines, err := RenderText(context.Background(), "graph LR\nA[🚀🚀] --> B[✅]", TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// All rows of a box are equally wide when widths are counted right.
	if w0, w1 := textgrid.Width(lines[0]), textgrid.Width(lines[1]); w0 != w1 {
		t.Errorf("border row %d columns, label row %d:\n%s", w0, w1, strings.Join(lines, "\n"))
	}
}

func TestByteOrderMark(t *testing.T) {
	src := "\ufeffgraph TD\nA-->B\n"
	if typ, err := Detect(src); err != nil || typ != Flowchart {
		t.Errorf("Detect = %v, %v", typ, err)
	}
	if _, err := Render(context.Background(), src, Options{}); err != nil {
		t.Error(err)
	}
}

// TestTextUnsupportedFirst checks that unsupported diagram types give
// ErrUnsupported even when the body is invalid or too large.
func TestTextUnsupportedFirst(t *testing.T) {
	var big strings.Builder
	big.WriteString("zenuml\n")
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&big, "A.call%d()\n", i)
	}
	for _, src := range []string{big.String(), "zenuml\n{{{", "unknownDiagram\n[*] --> --> x"} {
		if _, err := RenderText(context.Background(), src, TextOptions{MaxCols: 80}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("got %v, want ErrUnsupported", err)
		}
	}
}

// registrySamples has a minimal valid source for every diagram type. A
// type added to kinds without a sample fails TestRegistry.
var registrySamples = map[DiagramType]string{
	Flowchart: "graph TD\nA", Sequence: "sequenceDiagram\nA->>B: x", Class: "classDiagram\nclass A",
	State: "stateDiagram\n[*] --> A", Pie: "pie\n\"a\" : 1", Gantt: "gantt\nt : 2020-01-01, 1d",
	GitGraph: "gitGraph\ncommit", Mindmap: "mindmap\nroot",
	ER: "erDiagram\nA ||--o{ B : has", Timeline: "timeline\n2020 : a",
	Journey: "journey\nTask: 3: Me", Quadrant: "quadrantChart\nA: [0.5, 0.5]",
	XYChart: "xychart-beta\nbar [1, 2]", Requirement: "requirementDiagram\nrequirement r {\nid: 1\n}",
	C4: "C4Context\nPerson(a, \"A\")", Kanban: "kanban\nTodo\n  a[Task]",
	Block: "block-beta\na b", Packet: "packet-beta\n0-7: \"a\"",
	Sankey: "sankey-beta\na,b,1", Radar: "radar-beta\naxis a, b, c\ncurve x{1, 2, 3}",
	Treemap: "treemap-beta\n\"a\": 1", Architecture: "architecture-beta\nservice a(server)[A]",
}

// TestRegistry checks the diagram type table: entries in DiagramType order,
// unique keywords and names, and a text flag that agrees with whether the
// parse result implements textDiagram.
func TestRegistry(t *testing.T) {
	keywords := map[string]DiagramType{}
	names := map[string]bool{}
	for i, k := range kinds {
		if int(k.typ) != i+1 {
			t.Errorf("kinds[%d] is %s (%d): entries must be in DiagramType order", i, k.name, k.typ)
		}
		if names[k.name] {
			t.Errorf("duplicate name %q", k.name)
		}
		names[k.name] = true
		for _, kw := range k.keywords {
			if prev, ok := keywords[kw]; ok {
				t.Errorf("keyword %q selects both %s and %s", kw, prev, k.typ)
			}
			keywords[kw] = k.typ
		}
		src, ok := registrySamples[k.typ]
		if !ok {
			t.Errorf("%s: no entry in registrySamples", k.name)
			continue
		}
		typ, doc, err := detect(src)
		if err != nil || typ != k.typ {
			t.Fatalf("%s: detect = %s, %v", k.name, typ, err)
		}
		d, err := parse(guardFor(), typ, doc.Lines)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := d.(textDiagram); ok != k.text {
			t.Errorf("%s: text = %v, implements textDiagram = %v", k.name, k.text, ok)
		}
	}
}

func guardFor() *guard.Guard { return guard.New(context.Background(), DefaultMaxNodes) }

// TestNoUnreadableDownscale checks that a drawing that would only fit with
// text below minTextPx is refused rather than returned as a sliver.
func TestNoUnreadableDownscale(t *testing.T) {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "n%d[label number %d] --> n%d[label number %d]\n", i, i, i+1, i+1)
	}
	for _, opt := range []Options{{MaxWidth: 300}, {MaxWidth: 300, Face: textFace(t)}} {
		img, err := Render(context.Background(), b.String(), opt)
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("got %v (%v), want ErrTooLarge", err, img.Bounds())
		}
	}
	// A small overshoot is still reduced to fit.
	img, err := Render(context.Background(), readFile(t, "testdata/flowchart_basic.mmd"), Options{MaxWidth: 400, Face: textFace(t)})
	if err != nil || img.Bounds().Dx() > 400 {
		t.Errorf("fixed Face at 400: %v, %v", err, err == nil && img.Bounds().Dx() > 400)
	}
}

func textFace(t *testing.T) font.Face {
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 14, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		t.Fatal(err)
	}
	return face
}

func TestErrorLines(t *testing.T) {
	tests := []struct {
		src, msg string
		line     int
	}{
		{"graph TD\nsubgraph S\na --> b\nc --> d\n", "not closed", 2},
		{"sequenceDiagram\nA->>B: x\nloop L\nA->>B: y\nB->>A: z\n", "not closed", 3},
		{"gantt\ndateFormat YYYY-MM-DD\nsection s\nt1 :2000-13-45, 1d\n", "invalid start date", 4},
		{"gantt\ndateFormat YYYY-MM-DD\nt0 : 2000-01-01, 1d\nt1 :2000-13-45, 1d\n", "invalid start date", 4},
	}
	for _, tc := range tests {
		_, err := Render(context.Background(), tc.src, Options{})
		var se *SyntaxError
		if !errors.As(err, &se) || se.Line != tc.line || !strings.Contains(se.Msg, tc.msg) {
			t.Errorf("%q: got %v, want line %d %q", tc.src, err, tc.line, tc.msg)
		}
	}
	// Ordinary ids are still ids.
	if _, err := Render(context.Background(), "gantt\ndateFormat YYYY-MM-DD\nt0 : a1, 2000-01-01, 1d\nt1 : b2, 1d\n", Options{}); err != nil {
		t.Error(err)
	}
}

// TestFaceEm checks the em estimate used for a fixed Face's readability.
func TestFaceEm(t *testing.T) {
	if em := faceEm(textFace(t)); em < 13.3 || em > 14.7 {
		t.Errorf("faceEm of a 14px face = %.2f, want within 5%% of 14", em)
	}
}

// TestFlowchartTextRouting checks the box-drawing output of flowcharts with
// branches and back edges: an arrow head always stands in front of a plain
// box border (no junction like "▼" over "┴"), has its own line behind it,
// and every edge label appears once, never run together with another.
func TestFlowchartTextRouting(t *testing.T) {
	srcs := []string{
		"flowchart TD\nA[開始] --> B{条件?}\nB -->|はい| C[処理1]\nB -->|いいえ| D[処理2]\nC --> E[終了]\nD --> B\n",
		"flowchart TD\nA[入力] --> B{検証OK?}\nB -->|はい| C[保存]\nB -->|いいえ| D[エラー表示]\nD --> A\nC --> E{続ける?}\nE -->|はい| A\nE -->|いいえ| F[終了]\n",
		"flowchart LR\nA[開始] --> B{判定}\nB -->|はい| C[OK]\nB -->|いいえ| D[NG]\nD --> A\n",
		"flowchart BT\nA --> B\nB --> C\nC --> A\nA --> C\n",
		"flowchart RL\nA -->|x| B\nB -->|y| C\nC -->|z| A\n",
	}
	for _, f := range []string{"testdata/flowchart_basic.mmd", "testdata/flowchart_edge_cases.mmd", "testdata/flowchart_subgraph.mmd"} {
		srcs = append(srcs, readFile(t, f))
	}
	// Bits of a box-drawing rune: up, down, left, right.
	bits := map[rune][4]bool{
		'│': {true, true, false, false}, '─': {false, false, true, true},
		'┌': {false, true, false, true}, '┐': {false, true, true, false},
		'└': {true, false, false, true}, '┘': {true, false, true, false},
		'├': {true, true, false, true}, '┤': {true, true, true, false},
		'┬': {false, true, true, true}, '┴': {true, false, true, true}, '┼': {true, true, true, true},
		// Dashed and heavy lines (dotted and thick edges).
		'┆': {true, true, false, false}, '┄': {false, false, true, true},
		'┃': {true, true, false, false}, '━': {false, false, true, true},
	}
	type arrow struct {
		dx, dy int
		back   int // index of the bit the cell behind must have
		front  int // bit the border in front must NOT have toward the arrow
	}
	arrows := map[rune]arrow{'▼': {0, 1, 1, 0}, '▲': {0, -1, 0, 1}, '▶': {1, 0, 3, 2}, '◀': {-1, 0, 2, 3}}
	for i, src := range srcs {
		lines, err := RenderText(context.Background(), src, TextOptions{})
		if err != nil {
			t.Fatalf("source %d: %v", i, err)
		}
		// Cells by column; wide characters take two.
		grid := make([][]rune, len(lines))
		for y, l := range lines {
			for _, r := range l {
				grid[y] = append(grid[y], r)
				if textgrid.RuneWidth(r) == 2 {
					grid[y] = append(grid[y], 0)
				}
			}
		}
		at := func(x, y int) rune {
			if y < 0 || y >= len(grid) || x < 0 || x >= len(grid[y]) {
				return ' '
			}
			return grid[y][x]
		}
		dump := strings.Join(lines, "\n")
		for y := range grid {
			for x, r := range grid[y] {
				a, ok := arrows[r]
				if !ok {
					continue
				}
				if b, ok := bits[at(x-a.dx, y-a.dy)]; !ok || !b[a.back] {
					t.Errorf("source %d: arrow %c at (%d,%d) has no line behind it:\n%s", i, r, x, y, dump)
				}
				if b, ok := bits[at(x+a.dx, y+a.dy)]; ok && b[a.front] {
					t.Errorf("source %d: arrow %c at (%d,%d) points at a junction %c:\n%s", i, r, x, y, at(x+a.dx, y+a.dy), dump)
				}
			}
		}
		for _, bad := range []string{"はいいいえ", "いいえはい", "▲┬", "┬▲", "▼┴"} {
			if strings.Contains(dump, bad) {
				t.Errorf("source %d: output contains %q:\n%s", i, bad, dump)
			}
		}
	}
	// In the LR chart the two labelled branches leave 判定 separately: no
	// junction on the "はい" line would let "はい" name the "いいえ" edge.
	lr, _ := RenderText(context.Background(), srcs[2], TextOptions{})
	for _, l := range lr {
		if strings.Contains(l, "はい") && strings.ContainsAny(l, "┬┴┼") {
			t.Errorf("labelled line shared with another edge: %q\n%s", l, strings.Join(lr, "\n"))
		}
	}
	// Each label of the first chart appears exactly once.
	lines, _ := RenderText(context.Background(), srcs[0], TextOptions{})
	dump := strings.Join(lines, "\n")
	if strings.Count(dump, "はい") != 1 || strings.Count(dump, "いいえ") != 1 {
		t.Errorf("labels missing or repeated:\n%s", dump)
	}
}

// TestSelfLoopLabel checks that a self loop's label does not cover the
// loop: the output has exactly as many line characters as the same chart
// without the label.
func TestSelfLoopLabel(t *testing.T) {
	lineChars := func(lines []string) int {
		n := 0
		for _, l := range lines {
			for _, r := range l {
				if strings.ContainsRune("─│┌┐└┘├┤┬┴┼▲▼◀▶", r) {
					n++
				}
			}
		}
		return n
	}
	for _, tc := range []struct{ labelled, plain, label string }{
		{"flowchart TD\nA -->|x| A\n", "flowchart TD\nA --> A\n", "x"},
		{"flowchart LR\nA -->|x| A\n", "flowchart LR\nA --> A\n", "x"},
		{"flowchart LR\nA[処理] -->|retry| A\nA --> B\n", "flowchart LR\nA[処理] --> A\nA --> B\n", "retry"},
		{"flowchart TD\nA[処理] -->|再試行| A\nA --> B[終了]\n", "flowchart TD\nA[処理] --> A\nA --> B[終了]\n", "再試行"},
		{"flowchart TD\nA[長いノード名] -->|もう一度やり直す| A\n", "flowchart TD\nA[長いノード名] --> A\n", "もう一度やり直す"},
	} {
		with, err := RenderText(context.Background(), tc.labelled, TextOptions{})
		if err != nil {
			t.Fatal(err)
		}
		without, err := RenderText(context.Background(), tc.plain, TextOptions{})
		if err != nil {
			t.Fatal(err)
		}
		dump := strings.Join(with, "\n")
		if !strings.Contains(dump, tc.label) {
			t.Errorf("label %q missing:\n%s", tc.label, dump)
		}
		if a, b := lineChars(with), lineChars(without); a != b {
			t.Errorf("%d line characters with the label, %d without: the label covers the loop\n%s\n---\n%s",
				a, b, dump, strings.Join(without, "\n"))
		}
	}
}

// TestMissingGlyph checks that a label the font cannot draw gives
// ErrUnsupported instead of an image of empty boxes, and that RenderText
// (drawn by the terminal's fonts) still works.
func TestMissingGlyph(t *testing.T) {
	src := "graph LR\nA[開始] --> B[End]"
	_, err := Render(context.Background(), src, Options{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Render with the built-in font: got %v, want ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "開") {
		t.Errorf("error %q does not name the character", err)
	}
	if _, err := RenderText(context.Background(), src, TextOptions{}); err != nil {
		t.Errorf("RenderText: %v", err)
	}
	// Latin, Greek, Cyrillic and arrows are in the built-in font.
	if _, err := Render(context.Background(), "graph LR\nA[Café αβγ Жж] --> B[→ ←]", Options{}); err != nil {
		t.Errorf("Render of characters the built-in font has: %v", err)
	}
}

// TestMaxPixels checks that the pixel limit is applied (before the image
// is allocated) and can be raised.
func TestMaxPixels(t *testing.T) {
	src := readFile(t, "testdata/flowchart_basic.mmd")
	if _, err := Render(context.Background(), src, Options{MaxPixels: 1000}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("MaxPixels 1000: got %v, want ErrTooLarge", err)
	}
	var b strings.Builder
	b.WriteString("sequenceDiagram\n")
	for i := 0; i < 450; i++ {
		fmt.Fprintf(&b, "P%d->>P%d: hello world message\n", i%20, (i*7)%20)
	}
	// About 62 million pixels at full size: over DefaultMaxPixels. The
	// 250 MB image is never allocated: the text is shrunk to fit the limit
	// (TestReReview24Fixes), or the drawing is refused.
	img, err := Render(context.Background(), b.String(), Options{})
	switch {
	case err == nil:
		if n := img.Bounds().Dx() * img.Bounds().Dy(); n > DefaultMaxPixels {
			t.Errorf("default limit: %d pixels", n)
		}
	case !errors.Is(err, ErrTooLarge):
		t.Errorf("default limit: got %v, want ErrTooLarge or an image within it", err)
	}
}

// TestTextMaxNodes checks that RenderText applies TextOptions.MaxNodes.
func TestTextMaxNodes(t *testing.T) {
	src := "graph LR\nA --> B --> C --> D --> E"
	if _, err := RenderText(context.Background(), src, TextOptions{MaxNodes: 3}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("MaxNodes 3: got %v, want ErrTooLarge", err)
	}
	if _, err := RenderText(context.Background(), src, TextOptions{}); err != nil {
		t.Errorf("default MaxNodes: %v", err)
	}
}

// TestFontFunc checks that FontFunc is asked for every style and that a
// bold label is measured with the bold face (it is wider).
func TestFontFunc(t *testing.T) {
	var asked []FontStyle
	ff := func(px float64, st FontStyle) (font.Face, error) {
		asked = append(asked, st)
		fs, err := textmeasure.BuiltinFaces(px)
		return fs[st], err
	}
	src := func(label string) string { return "graph LR\nA[\"" + label + "\"]" }
	plain, err := Render(context.Background(), src("Wide label"), Options{FontFunc: ff})
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) < 4 || asked[0] != Regular || asked[1] != Bold || asked[2] != Italic || asked[3] != BoldItalic {
		t.Errorf("FontFunc asked for %v", asked)
	}
	bold, err := Render(context.Background(), src("<b>Wide label</b>"), Options{FontFunc: ff})
	if err != nil {
		t.Fatal(err)
	}
	if bold.Bounds().Dx() <= plain.Bounds().Dx() {
		t.Errorf("bold label is not wider: %d <= %d", bold.Bounds().Dx(), plain.Bounds().Dx())
	}
}

// TestHugeValues checks that values whose range or sum overflows float64
// give a syntax error instead of hanging or panicking in the layout.
func TestHugeValues(t *testing.T) {
	for _, src := range []string{
		"xychart-beta\nbar [-1e308, 1e308]",
		"xychart-beta\ny-axis -1e308 --> 1e308\nbar [1]",
		"treemap-beta\n\"a\": 1e308\n\"b\": 1e308",
		"sankey-beta\na,b,1e308\nc,b,1e308",
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := render(ctx, src, Options{})
		cancel()
		var se *SyntaxError
		if !errors.As(convertErr(err), &se) {
			t.Errorf("%q: got %v, want a syntax error", src, err)
		}
	}
}

// TestTextAtOwnWidth checks that a diagram drawn by internal/textgraph
// comes out the same when maxCols is exactly the width it takes without a
// limit: an attempt that fits must not be skipped.
func TestTextAtOwnWidth(t *testing.T) {
	graphTypes := map[DiagramType]bool{Flowchart: true, Class: true, State: true, ER: true, Requirement: true, C4: true, Architecture: true}
	for _, f := range fixtures(t) {
		src := readFile(t, f)
		if typ, _ := Detect(src); !graphTypes[typ] {
			continue
		}
		full, err := RenderText(context.Background(), src, TextOptions{})
		if err != nil {
			t.Fatal(err)
		}
		w := 0
		for _, l := range full {
			w = max(w, textgrid.Width(l))
		}
		got, err := RenderText(context.Background(), src, TextOptions{MaxCols: w})
		if err != nil || strings.Join(got, "\n") != strings.Join(full, "\n") {
			t.Errorf("%s at its own width %d: err=%v, output changed", name(f), w, err)
		}
	}
}

// TestLooseDirective checks that a directive with unquoted values still
// renders, as in Mermaid.
func TestLooseDirective(t *testing.T) {
	src := "%%{init: {\"theme\": forest, \"flowchart\": {\"curve\": basis}}}%%\ngraph TD\nA-->B"
	if _, err := Render(context.Background(), src, Options{}); err != nil {
		t.Error(err)
	}
}

// TestNestedBlockOverflow checks that nested blocks, whose widths multiply
// (64 columns per level), give ErrTooLarge instead of overflowing the size
// checks: the text grid used to panic and the image came out 1 pixel wide.
func TestNestedBlockOverflow(t *testing.T) {
	var b strings.Builder
	b.WriteString("block-beta\n")
	for i := range 15 {
		fmt.Fprintf(&b, "block:g%d\ncolumns 64\n", i)
	}
	b.WriteString("x\n")
	for range 15 {
		b.WriteString("end\n")
	}
	src := b.String()
	if _, err := render(context.Background(), src, Options{}); !errors.Is(convertErr(err), ErrTooLarge) {
		t.Errorf("Render: got %v, want ErrTooLarge", err)
	}
	if _, err := renderText(context.Background(), src, TextOptions{}); !errors.Is(convertErr(err), ErrTooLarge) {
		t.Errorf("RenderText: got %v, want ErrTooLarge", err)
	}
}

// TestGitGraphBranchOff checks that a branch whose first commit comes after
// later commits on its parent branch is drawn leaving right after the
// commit it started from, not before its own first commit.
func TestGitGraphBranchOff(t *testing.T) {
	src := "gitGraph\ncommit id: \"A\"\nbranch dev\ncheckout main\ncommit id: \"B\"\ncommit id: \"C\"\ncheckout dev\ncommit id: \"D\""
	lines, err := RenderText(context.Background(), src, TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "●") || !strings.HasPrefix(lines[1], "├─┐") {
		t.Errorf("dev does not leave right after A:\n%s", strings.Join(lines, "\n"))
	}
}

// TestFinalReviewFixes covers the serious findings of the final review.
func TestFinalReviewFixes(t *testing.T) {
	ctx := context.Background()
	// A flowchart with many crossing edges used to take seconds as text,
	// routing every edge in all five attempts before failing.
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "n%d[node label number %02d]\n", i, i)
	}
	x := uint32(1)
	for i := 0; i < 499; i++ {
		x = x*1664525 + 1013904223
		fmt.Fprintf(&b, "n%d --> n%d\n", x>>8%60, x>>20%60)
	}
	for _, cols := range []int{80, 0} {
		start := time.Now()
		_, err := RenderText(ctx, b.String(), TextOptions{MaxCols: cols})
		if d := time.Since(start); d > 1500*time.Millisecond {
			t.Errorf("MaxCols %d: %v after %v", cols, err, d)
		}
		if err != nil && !errors.Is(err, ErrTooLarge) {
			t.Errorf("MaxCols %d: %v", cols, err)
		}
	}
	// autonumber in text output.
	lines, err := RenderText(ctx, "sequenceDiagram\nautonumber 10 5\nA->>B: x\nA->>B: y\nautonumber off\nA->>B: z", TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, "10. x") || !strings.Contains(out, "15. y") || strings.Contains(out, ". z") {
		t.Errorf("numbers in text output:\n%s", out)
	}
}

// TestMinorReviewFixes covers the minor findings of the final review.
func TestMinorReviewFixes(t *testing.T) {
	ctx := context.Background()
	text := func(src string) string {
		t.Helper()
		lines, err := RenderText(ctx, src, TextOptions{})
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		return strings.Join(lines, "\n")
	}
	// Package visibility ~ is kept; ~T~ pairs are generics.
	if out := text("classDiagram\nclass A {\n  ~int count\n  +List~int~ items\n}"); !strings.Contains(out, "~int count") || !strings.Contains(out, "List<int>") {
		t.Errorf("class members:\n%s", out)
	}
	// Front matter block scalars and flow mappings.
	doc, err := source.SplitDoc("---\ntitle: |\n  First\n  second\nconfig: {theme: dark}\n---\ngraph LR\nA-->B")
	if err != nil || doc.Title != "First\nsecond" || doc.Theme != "dark" {
		t.Errorf("front matter: title %q theme %q, %v", doc.Title, doc.Theme, err)
	}
	doc, err = source.SplitDoc("---\ntitle: >-\n  folded\n  title\n---\ngraph LR\nA-->B")
	if err != nil || doc.Title != "folded title" {
		t.Errorf("folded title %q, %v", doc.Title, err)
	}
	// Mermaid 11 participant types.
	if out := text("sequenceDiagram\nparticipant A@{ \"type\" : \"database\" }\nA->>A: x"); !strings.Contains(out, "«database»") || strings.Contains(out, "@{") {
		t.Errorf("participant type:\n%s", out)
	}
	// xychart bars grow from zero.
	out := text("xychart-beta\nx-axis [a, b]\ny-axis -10 --> 10\nbar [-5, -2]")
	var a, b string
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "a "):
			a = l
		case strings.HasPrefix(l, "b "):
			b = l
		}
	}
	if strings.Count(a, "█") <= strings.Count(b, "█") {
		t.Errorf("-5 should have the longer bar:\n%s", out)
	}
	// Circle and cross ends in text.
	if out := text("graph LR\nx --o y --x z"); !strings.Contains(out, "○") || !strings.Contains(out, "×") {
		t.Errorf("markers:\n%s", out)
	}
	// Deactivating an inactive participant is an error, as in Mermaid.
	for _, src := range []string{"sequenceDiagram\nA->>B: x\ndeactivate B", "sequenceDiagram\nA->>+B: x\nB->>-A: y\nB->>-A: z"} {
		var se *SyntaxError
		if _, err := RenderText(ctx, src, TextOptions{}); !errors.As(err, &se) {
			t.Errorf("%q: %v, want SyntaxError", src, err)
		}
	}
	// A long task with most weekdays excluded is drawn; only a week with
	// no day left is "every day is excluded".
	gantt := "gantt\ndateFormat YYYY-MM-DD\nexcludes weekends, monday, tuesday, wednesday, thursday%s\nsection S\nT :a, 2024-01-01, %s\n"
	if _, err := RenderText(ctx, fmt.Sprintf(gantt, "", "3000d"), TextOptions{}); err != nil {
		t.Errorf("long task: %v", err)
	}
	if _, err := RenderText(ctx, fmt.Sprintf(gantt, ", friday", "3d"), TextOptions{}); err == nil || !strings.Contains(err.Error(), "every day is excluded") {
		t.Errorf("all days excluded: %v", err)
	}
}

// TestReReviewFixes covers the findings of the second final review.
func TestReReviewFixes(t *testing.T) {
	ctx := context.Background()
	// architecture-beta: routing has a budget per Render call, over the
	// layout attempts for MaxWidth (it took up to 49 s and 12 GB).
	var b strings.Builder
	b.WriteString("architecture-beta\n")
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "service s%d(server)[S%d]\n", i, i)
	}
	for i := 1; i < 50; i++ {
		side := "R -- L"
		if i%2 == 0 {
			side = "B -- T"
		}
		fmt.Fprintf(&b, "s%d:%s:s%d\n", i-1, side, i)
	}
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "s%d:L -- R:s%d\n", (i*7)%50, (i*13+5)%50)
	}
	for _, mw := range []int{0, 1000} {
		start := time.Now()
		_, err := Render(ctx, b.String(), Options{MaxWidth: mw})
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("MaxWidth %d: %v after %v", mw, err, d)
		}
		if err != nil && !errors.Is(err, ErrTooLarge) {
			t.Errorf("MaxWidth %d: %v", mw, err)
		}
	}
	text := func(src string) (string, error) {
		lines, err := RenderText(ctx, src, TextOptions{})
		return strings.Join(lines, "\n"), err
	}
	// o and x right after the dashes are end markers, as in Mermaid.
	if out, err := text("flowchart LR\nA---oB\nA--text--xC"); err != nil || !strings.Contains(out, "○") || !strings.Contains(out, "×") || strings.Contains(out, "oB") {
		t.Errorf("markers: %v\n%s", err, out)
	}
	// A--B alone is an error.
	var se *SyntaxError
	if _, err := text("flowchart LR\nA-->B--C"); !errors.As(err, &se) {
		t.Errorf("incomplete link: %v", err)
	}
	// Nested generics.
	if out, err := text("classDiagram\nclass Square {\n  +getDistanceMatrix() List~List~int~~\n  ~int count\n}"); err != nil || !strings.Contains(out, "List<List<int>>") || !strings.Contains(out, "~int count") {
		t.Errorf("generics: %v\n%s", err, out)
	}
	// Participant alias and an unknown type.
	if out, err := text("sequenceDiagram\nparticipant A@{ \"type\": \"widget\", \"alias\": \"My Widget\" }\nA->>A: x"); err != nil || !strings.Contains(out, "«widget»") || !strings.Contains(out, "My Widget") {
		t.Errorf("participant: %v\n%s", err, out)
	}
}

// TestAdversarialReviewFixes covers the findings of the adversarial review.
func TestAdversarialReviewFixes(t *testing.T) {
	// raster: shapes as large as the image (nested sequence rects) took
	// tens of milliseconds each, and the context was checked only every
	// 16 shapes, so a 100 ms deadline returned after about 600 ms.
	var b strings.Builder
	b.WriteString("sequenceDiagram\n")
	for i := 0; i < 31; i++ {
		fmt.Fprintf(&b, "rect rgb(%d,200,200)\n", i*8)
	}
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "P%d->>P%d: m\n", i%12, (i+5)%12)
	}
	for i := 0; i < 31; i++ {
		b.WriteString("end\n")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Render(ctx, b.String(), Options{Background: color.White, MaxWidth: 1600})
	if d := time.Since(start); !errors.Is(err, context.DeadlineExceeded) || d > 300*time.Millisecond {
		t.Errorf("nested rects with a 100ms deadline: %v after %v", err, d)
	}

	// Scale is clamped below: 1e-9 gave a 1×1 image without an error.
	tiny, err := Render(context.Background(), "flowchart LR\nA-->B", Options{Scale: 1e-9})
	small, err2 := Render(context.Background(), "flowchart LR\nA-->B", Options{Scale: minScale})
	if err != nil || err2 != nil || tiny.Bounds() != small.Bounds() {
		t.Errorf("Scale 1e-9: %v %v, bounds %v, want %v", err, err2, tiny.Bounds(), small.Bounds())
	}

	// xychart: a subnormal range is a syntax error, not ErrTooLarge.
	var se *SyntaxError
	if _, err := Render(context.Background(), "xychart-beta\ny-axis 0 --> 1e-320\nbar [0, 1e-320]", Options{}); !errors.As(err, &se) || se.Line != 2 {
		t.Errorf("subnormal y range: %v", err)
	}

	// Front matter: an unclosed flow collection is a syntax error instead
	// of swallowing the keys after it.
	if _, err := Render(context.Background(), "---\ntitle: [unclosed\nconfig:\n  theme: dark\n---\nflowchart LR\nA-->B", Options{}); !errors.As(err, &se) || se.Line != 2 {
		t.Errorf("unclosed front matter list: %v", err)
	}
}

// TestReReview2Fixes covers the findings of the second adversarial review.
func TestReReview2Fixes(t *testing.T) {
	ctx := context.Background()
	// xychart: a value outside the y-axis range made the canvas grow
	// (528×14044 px for 50 on a 0 --> 1 axis).
	in, err := Render(ctx, "xychart-beta\nx-axis [a, b, c]\ny-axis 0 --> 1\nbar [0.5, 1, 0.8]", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"50", "-50", "1e300"} {
		out, err := Render(ctx, "xychart-beta\nx-axis [a, b, c]\ny-axis 0 --> 1\nbar [0.5, "+v+", 0.8]", Options{})
		if err != nil || out.Bounds() != in.Bounds() {
			t.Errorf("value %s outside the axis: %v, bounds %v, want %v", v, err, out.Bounds(), in.Bounds())
		}
	}
	// radar: a range that overflows is a syntax error, also for the image
	// (it was "NaN×NaN pixels").
	var se *SyntaxError
	if _, err := Render(ctx, "radar-beta\naxis a, b, c\nmin -1e308\ncurve x{1e308, 0, 1e308}", Options{}); !errors.As(err, &se) {
		t.Errorf("radar infinite range: %v", err)
	}
	// Front matter: a bracket in a comment is not part of the collection.
	if _, err := Render(ctx, "---\nconfig:\n  themeVariables: {primaryColor: '#f00'} # [x\n---\nflowchart LR\nA-->B", Options{}); err != nil {
		t.Errorf("comment with a bracket: %v", err)
	}
}

// TestReReview3Fixes covers the findings of the third adversarial review.
func TestReReview3Fixes(t *testing.T) {
	// A label of one long word kept RenderText busy for seconds after the
	// deadline (textgrid.Wrap rescanned the rest of the word for every
	// line), and long labels kept Render measuring text after it.
	long := map[string]string{
		"word":     "graph TD\nA[" + strings.Repeat("x", 1<<20-20) + "]",
		"cjk":      "graph TD\nA[" + strings.Repeat("あ", (1<<20-20)/3) + "]",
		"markdown": "graph TD\nA[\"`" + strings.Repeat("**a* ", (1<<20-20)/5) + "`\"]",
		"gantt":    "gantt\nsection s\n" + strings.Repeat("x", 1<<20-40) + " :a, 2020-01-01, 1d",
	}
	for name, src := range long {
		for _, text := range []bool{false, true} {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			start := time.Now()
			var err error
			if text {
				_, err = RenderText(ctx, src, TextOptions{MaxCols: 80})
			} else {
				_, err = Render(ctx, src, Options{MaxWidth: 400})
			}
			d := time.Since(start)
			cancel()
			// RenderText may finish before the deadline; it must not
			// overrun it.
			if d > 250*time.Millisecond {
				t.Errorf("%s (text %v): returned %v (%v) after a 50ms deadline", name, text, err, d)
			}
		}
	}
	if got := textgrid.Wrap(strings.Repeat("x", 25)+" ab", 10); strings.Join(got, "|") != "xxxxxxxxxx|xxxxxxxxxx|xxxxx ab" {
		t.Errorf("Wrap long word: %q", got)
	}
	if got := textgrid.Wrap("あい", 1); strings.Join(got, "|") != "あ|い" {
		t.Errorf("Wrap wide characters in 1 column: %q", got)
	}

	// Exceeding a limit of this library is ErrTooLarge, not a syntax error.
	nested := func(open, close string, n int) string {
		return strings.Repeat(open+"\n", n) + strings.Repeat(close+"\n", n)
	}
	var sub strings.Builder
	for i := 0; i < 33; i++ {
		fmt.Fprintf(&sub, "subgraph s%d\n", i)
	}
	sub.WriteString("a\n" + strings.Repeat("end\n", 33))
	tooLarge := map[string]string{
		"block columns":  "block-beta\ncolumns 65\na",
		"block span":     "block-beta\na:65",
		"block depth":    "block-beta\n" + nested("block", "end", 17),
		"subgraph depth": "graph TD\n" + sub.String(),
		"gantt duration": "gantt\nsection s\nt :a, 2020-01-01, 73001d",
		"sequence depth": "sequenceDiagram\n" + nested("loop x\nA->>B: m", "end", 33),
	}
	for name, src := range tooLarge {
		if _, err := Render(context.Background(), src, Options{}); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: got %v, want ErrTooLarge", name, err)
		}
	}
	var se *SyntaxError
	for _, src := range []string{"block-beta\ncolumns 0\na", "block-beta\ncolumns x\na", "block-beta\na:0"} {
		if _, err := Render(context.Background(), src, Options{}); !errors.As(err, &se) {
			t.Errorf("%q: got %v, want a syntax error", src, err)
		}
	}

	// Messages quote the offending source; it is cut short.
	_, err := Render(context.Background(), "graph TD\nA"+strings.Repeat("[", 100000)+"x", Options{})
	if !errors.As(err, &se) || len(se.Msg) > maxMsgBytes+len("…") || !utf8.ValidString(se.Msg) {
		t.Errorf("long message: %d bytes", len(err.Error()))
	}
	if _, err := Detect(strings.Repeat("あ", 100000)); err == nil || len(err.Error()) > 300 {
		t.Errorf("unknown diagram type: %d bytes", len(fmt.Sprint(err)))
	}
}

// TestReReview4Fixes covers the findings of the fourth adversarial review.
func TestReReview4Fixes(t *testing.T) {
	// Lines joined into one markdown string or front matter collection
	// were rescanned for every line: an unclosed "` or [ near the top of a
	// 1 MiB block took tens of seconds, also in Detect (no context).
	slow := map[string]string{
		"markdown":     "graph TD\na[\"`" + strings.Repeat("x\n", 520000) + "\"]-->b",
		"front matter": "---\ntitle: [" + strings.Repeat("x\n", 520000) + "---\ngraph TD\na-->b",
		// RenderText: the width of an edge label was measured for every
		// column it covers.
		"edge label": "graph TD\na -- " + strings.Repeat("n", 200000) + " --> b",
		// Render: every tile of a tall drawing walked every dash of every
		// edge.
		"dashed fan": "graph LR\n" + func() string {
			var b strings.Builder
			for i := 0; i < 499; i++ {
				fmt.Fprintf(&b, "a -.-> b%d\n", i)
			}
			return b.String()
		}(),
	}
	for name, src := range slow {
		start := time.Now()
		Detect(src)
		if d := time.Since(start); d > 500*time.Millisecond {
			t.Errorf("%s: Detect took %v", name, d)
		}
		for _, text := range []bool{false, true} {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			start = time.Now()
			if text {
				RenderText(ctx, src, TextOptions{})
			} else {
				Render(ctx, src, Options{MaxWidth: 1200})
			}
			cancel()
			if d := time.Since(start); d > 400*time.Millisecond {
				t.Errorf("%s (text %v): took %v with a 100ms deadline", name, text, d)
			}
		}
	}

	// pie: values that add up to +Inf drew every slice as 0%.
	var se *SyntaxError
	src := "pie\n\"a\" : 1e308\n\"b\" : 1e308\n\"c\" : 1"
	if _, err := Render(context.Background(), src, Options{}); !errors.As(err, &se) {
		t.Errorf("pie overflow (Render): %v", err)
	}
	if _, err := RenderText(context.Background(), src, TextOptions{}); !errors.As(err, &se) {
		t.Errorf("pie overflow (RenderText): %v", err)
	}

	// The nesting limit of directives and attribute blocks is ErrTooLarge.
	deep := strings.Repeat("{a: ", 20) + "1" + strings.Repeat("}", 20)
	for _, src := range []string{
		"%%{init: " + deep + "}%%\ngraph TD\na-->b",
		"graph TD\nA@{ label: " + deep + " }",
	} {
		if _, err := Render(context.Background(), src, Options{}); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%q: got %v, want ErrTooLarge", src[:20], err)
		}
	}
	if _, err := Render(context.Background(), "%%{init: {a: }%%\ngraph TD\na-->b", Options{}); !errors.As(err, &se) {
		t.Errorf("bad directive: got %v, want a syntax error", err)
	}
}

// TestReReview5Fixes covers the findings of the fifth adversarial review.
func TestReReview5Fixes(t *testing.T) {
	ctx := context.Background()
	// gantt: "every day is excluded" leaked as an untyped error.
	var se *SyntaxError
	src := "gantt\ndateFormat YYYY-MM-DD\nexcludes monday,tuesday,wednesday,thursday,friday,saturday,sunday\nsection s\nA : 2024-01-06, 3d"
	if _, err := Render(ctx, src, Options{}); !errors.As(err, &se) || se.Line != 5 {
		t.Errorf("every day excluded: %v", err)
	}

	// The multi-line accDescr is accepted like the one-line form.
	for _, src := range []string{
		"graph TD\naccDescr {\nmulti\nline\n}\na-->b",
		"sequenceDiagram\naccDescr {\nmulti line\n}\nA->>B: hi",
		"classDiagram\naccDescr { one line }\nclass A",
		"stateDiagram-v2\naccDescr {\nx\n}\n[*] --> A",
		"erDiagram\n  accDescr {\n  x\n  }\n  A ||--o{ B : has",
		"gitGraph\naccDescr {\nx\n}\ncommit",
	} {
		if _, err := Render(ctx, src, Options{}); err != nil {
			t.Errorf("%q: %v", src, err)
		}
		if _, err := RenderText(ctx, src, TextOptions{}); err != nil {
			t.Errorf("%q (text): %v", src, err)
		}
	}
	if _, err := Render(ctx, "graph TD\naccDescr {\nnot closed\na-->b", Options{}); !errors.As(err, &se) || se.Line != 2 {
		t.Errorf("unterminated accDescr: %v", err)
	}

	// A single shape as large as a huge MaxPixels image no longer keeps
	// Render past its deadline.
	var b strings.Builder
	b.WriteString("sequenceDiagram\n")
	for i := 0; i < 20000; i++ {
		b.WriteString("A->>B: m\n")
	}
	dctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Render(dctx, b.String(), Options{MaxNodes: 1e6, MaxPixels: math.MaxInt})
	if d := time.Since(start); d > 800*time.Millisecond {
		t.Errorf("huge sequence: %v after %v with a 300ms deadline", err, d)
	}
}

// TestReReview6Fixes covers the findings of the sixth adversarial review.
func TestReReview6Fixes(t *testing.T) {
	ctx := context.Background()
	// RenderText dropped combining marks: NFD "が" became "か". (They are
	// now composed on input, TestReReview19Marks.)
	nfd := "か\u3099 cafe\u0301"
	lines, err := RenderText(ctx, "graph TD\nA[\""+nfd+"\"]", TextOptions{MaxCols: 40})
	if err != nil || !strings.Contains(strings.Join(lines, "\n"), "\u304c caf\u00e9") {
		t.Errorf("combining marks: %v %q", err, lines)
	}
	for _, l := range lines {
		if w := textgrid.Width(l); w > 40 {
			t.Errorf("line %q is %d columns", l, w)
		}
	}

	// Class members and requirement fields count toward MaxNodes.
	members := "classDiagram\nclass A {\n" + strings.Repeat("+int x\n", 11) + "}"
	fields := "requirementDiagram\nrequirement r {\nid: 1\n" + strings.Repeat("text: x\n", 10) + "}"
	for _, src := range []string{members, fields} {
		if _, err := RenderText(ctx, src, TextOptions{MaxNodes: 10}); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%q: got %v, want ErrTooLarge", src[:20], err)
		}
	}

	// Invalid UTF-8 is reported on its line.
	var se *SyntaxError
	if _, err := Render(ctx, "graph TD\na-->b\nb-->c\nc-->\xff", Options{}); !errors.As(err, &se) || se.Line != 4 {
		t.Errorf("invalid UTF-8: %v", err)
	}
	// Front matter after a directive is a syntax error, not an
	// unsupported diagram type.
	if _, err := Render(ctx, "%%{init: {}}%%\n---\ntitle: x\n---\ngraph TD\na-->b", Options{}); !errors.As(err, &se) || se.Line != 2 {
		t.Errorf("front matter after a directive: %v", err)
	}
}

// TestReReview7Fixes covers the findings of the seventh adversarial review.
func TestReReview7Fixes(t *testing.T) {
	ctx := context.Background()
	// Statements after "graph TD;" on the header line were read as part
	// of the direction.
	one, err1 := RenderText(ctx, "graph TD;A-->B;B-->C", TextOptions{})
	lines, err2 := RenderText(ctx, "graph TD\nA-->B\nB-->C", TextOptions{})
	if err1 != nil || err2 != nil || strings.Join(one, "\n") != strings.Join(lines, "\n") {
		t.Errorf("one-line flowchart: %v %v\n%s", err1, err2, strings.Join(one, "\n"))
	}
	// packet: a bit count near MaxInt overflowed past the bit limit and the
	// field disappeared.
	for _, src := range []string{
		"packet-beta\n0-7: \"a\"\n+9223372036854775807: \"b\"",
		"packet-beta\n0-7: \"a\"\n+9223372036854775807: \"b\"\n+8: \"c\"",
	} {
		if _, err := Render(ctx, src, Options{}); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%q: got %v, want ErrTooLarge", src, err)
		}
	}
	// A Face without metrics gave "needs about -9223372036854775808 px".
	_, err := Render(ctx, "gantt\nsection s\na long task name here : 2020-01-01, 30d", Options{MaxWidth: 50, Face: zeroMetrics{}})
	if err != nil && strings.Contains(err.Error(), "-9223372036854775808") {
		t.Errorf("zero metrics: %v", err)
	}
}

// zeroMetrics is a font.Face with zero metrics and fixed advances.
type zeroMetrics struct{ font.Face }

func (zeroMetrics) Metrics() font.Metrics                   { return font.Metrics{} }
func (zeroMetrics) GlyphAdvance(rune) (fixed.Int26_6, bool) { return 7 << 6, true }
func (zeroMetrics) Kern(rune, rune) fixed.Int26_6           { return 0 }
func (zeroMetrics) GlyphBounds(rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	return fixed.Rectangle26_6{}, 7 << 6, true
}
func (zeroMetrics) Glyph(fixed.Point26_6, rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	return image.Rectangle{}, nil, image.Point{}, 7 << 6, false
}
func (zeroMetrics) Close() error { return nil }

// TestReReview8Fixes covers the findings of the eighth adversarial review.
func TestReReview8Fixes(t *testing.T) {
	ctx := context.Background()
	// RenderText of a node with hundreds of children spent seconds (and
	// tens of GB of allocations) outside the routing step limit, also with
	// no deadline.
	var b strings.Builder
	b.WriteString("graph TD\n")
	for i := 0; i < 499; i++ {
		fmt.Fprintf(&b, "r --> n%d[%s]\n", i, strings.Repeat("x", 200))
	}
	start := time.Now()
	_, err := RenderText(ctx, b.String(), TextOptions{})
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("wide fan: %v after %v", err, d)
	}
	// Numbers too large for int are a size limit on every platform (on
	// 32-bit ones Atoi fails from 2^31 on).
	for _, src := range []string{
		"block-beta\ncolumns 3000000000\na",
		"block-beta\na:3000000000",
		"packet-beta\n0-7: \"a\"\n+3000000000: \"b\"",
		"packet-beta\n0-99999999999999999999: \"a\"",
	} {
		if _, err := Render(ctx, src, Options{}); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%q: got %v, want ErrTooLarge", src, err)
		}
	}
	if _, err := Render(ctx, "gitGraph\ncommit\nbranch b order: 3000000000\ncommit", Options{}); err != nil {
		t.Errorf("large branch order: %v", err)
	}
}

// TestReReview9Fixes covers the findings of the ninth adversarial review.
func TestReReview9Fixes(t *testing.T) {
	// themeVariables.lineColor overrode Options.Foreground, which is the
	// text and line color: a black line vanished on a dark terminal.
	src := "%%{init:{'themeVariables':{'lineColor':'#000000'}}}%%\ngraph TD\nA-->B"
	plain := "graph TD\nA-->B"
	opt := Options{Foreground: color.White}
	a, err1 := Render(context.Background(), src, opt)
	b, err2 := Render(context.Background(), plain, opt)
	if err1 != nil || err2 != nil || !pixelsEqual(a, b) {
		t.Errorf("lineColor with Foreground: %v %v, images differ", err1, err2)
	}
	// Without Foreground it still applies.
	a, _ = Render(context.Background(), src, Options{})
	b, _ = Render(context.Background(), plain, Options{})
	if a == nil || b == nil || pixelsEqual(a, b) {
		t.Errorf("lineColor without Foreground had no effect")
	}
}

// TestReReview10Fixes covers the findings of the tenth adversarial review.
func TestReReview10Fixes(t *testing.T) {
	// RenderText returned a result after the deadline: wrapping a long
	// title is outside the guard.
	src := "pie title " + strings.Repeat("word ", 200000) + "\n\"a\" : 1"
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)
	if _, err := RenderText(ctx, src, TextOptions{MaxCols: 80}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("after the deadline: got %v, want DeadlineExceeded", err)
	}
}

// TestReReview11Fixes covers the findings of the eleventh adversarial review.
func TestReReview11Fixes(t *testing.T) {
	// Bidi controls reached RenderText's output through titles and pie
	// labels, reversing the rest of the line in bidi terminals.
	for _, src := range []string{
		"---\ntitle: x\u202eyz\n---\npie\n\"a\" : 1",
		"pie\n\"x\u2066y\u202ez\" : 1",
		"graph TD\nA[\"a\u202eb\"]",
	} {
		lines, err := RenderText(context.Background(), src, TextOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range lines {
			if strings.ContainsAny(l, "\u202e\u2066") {
				t.Errorf("%q: bidi control in %q", src, l)
			}
		}
	}
}

// TestReReview12Fixes covers the findings of the twelfth adversarial review.
func TestReReview12Fixes(t *testing.T) {
	// Valid Mermaid that this library does not draw is ErrUnsupported, not
	// a syntax error the author could fix.
	for _, src := range []string{
		"flowchart TD\nA@{ shape: fork }",
		"flowchart TD\nA@{ shape: cloud }",
		"flowchart TD\nA@{ icon: \"fa:user\", form: \"square\" }",
		"classDiagram\nnamespace A {\nnamespace B {\nclass C\n}\n}",
	} {
		_, err := Render(context.Background(), src, Options{})
		var se *SyntaxError
		if !errors.Is(err, ErrUnsupported) || errors.As(err, &se) {
			t.Errorf("%q: got %v, want ErrUnsupported", src, err)
		}
	}
	// An unknown shape name is still a syntax error.
	var se *SyntaxError
	if _, err := Render(context.Background(), "flowchart TD\nA@{ shape: blob }", Options{}); !errors.As(err, &se) {
		t.Errorf("unknown shape: %v", err)
	}
}

// TestReReview13Fixes covers the findings of the thirteenth adversarial review.
func TestReReview13Fixes(t *testing.T) {
	ctx := context.Background()
	// packet: a field ending at MaxInt overflowed the bit count, the limit
	// check returned nil, and Render dereferenced a nil diagram.
	src := "packet-beta\n0-15: \"a\"\n16-9223372036854775807: \"b\"\n"
	if _, err := Render(ctx, src, Options{}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("packet MaxInt end (Render): %v", err)
	}
	if _, err := RenderText(ctx, src, TextOptions{}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("packet MaxInt end (RenderText): %v", err)
	}
	if err := guard.Limit("x", -1, 10); err == nil {
		t.Error("guard.Limit accepted an overflowed (negative) count")
	}
	// treemap: a subnormal sum made the layout NaN ("+Inf×NaN pixels").
	var se *SyntaxError
	for _, src := range []string{
		"treemap-beta\n\"a\"\n  \"b\": 1e-310\n",
		"treemap-beta\n\"a\"\n  \"b\": 5e-324\n",
		"treemap-beta\n\"a\"\n  \"b\": 1e-320\n  \"c\": 1e-320\n",
	} {
		if _, err := Render(ctx, src, Options{}); !errors.As(err, &se) {
			t.Errorf("%q: got %v, want a syntax error", src, err)
		}
	}
}

// TestReReview14Fixes covers the findings of the fourteenth adversarial review.
func TestReReview14Fixes(t *testing.T) {
	ctx := context.Background()
	// A tab between words did not separate keywords ("section\tS").
	for _, src := range []string{
		"gantt\ndateFormat\tYYYY-MM-DD\nsection\tS\nt : 2020-01-01, 1d",
		"sequenceDiagram\nloop\tx\nA->>B: m\nend",
		"gitGraph\ncommit\tid: \"a\"",
		"classDiagram\nclass A {\n+int\tx\n}",
	} {
		if _, err := Render(ctx, src, Options{}); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
	// Invisible format characters and other spaces made the whole diagram
	// ErrUnsupported with the built-in font.
	for _, s := range []string{"\u200b", "\ufeff", "\u2003", "\u00a0", "\u2060"} {
		src := "graph TD\nA[a" + s + "b]-->B"
		if _, err := Render(ctx, src, Options{}); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	// Errors found after parsing point at the line of the element.
	var se *SyntaxError
	if _, err := Render(ctx, "block-beta\na\nb\nc\na --> x\nd\ne\nf", Options{}); !errors.As(err, &se) || se.Line != 5 {
		t.Errorf("edge to unknown block: %v", err)
	}
	if _, err := Render(ctx, "radar-beta\naxis a, b, c\ncurve x{1, 2}\ncurve y{1, 2, 3}\n", Options{}); !errors.As(err, &se) || se.Line != 3 {
		t.Errorf("curve with too few values: %v", err)
	}
}

// TestReReview15Fixes covers the findings of the fifteenth adversarial review.
func TestReReview15Fixes(t *testing.T) {
	ctx := context.Background()
	// A white label on a node the author filled white vanished with
	// Foreground white.
	white := color.NRGBA{255, 255, 255, 255}
	img, err := Render(ctx, "graph LR\nB[second]\nstyle B fill:#ffffff", Options{Foreground: color.White})
	if err != nil {
		t.Fatal(err)
	}
	colors := map[color.NRGBA]bool{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA); c.A == 255 && c != white {
				colors[c] = true
			}
		}
	}
	if len(colors) == 0 {
		t.Error("white on white: no visible label")
	}
	// An author's text color is kept.
	if c := theme.Readable(white, white); c == white {
		t.Error("Readable kept white on white")
	}
	// gantt: dateFormat with milliseconds.
	if _, err := Render(ctx, "gantt\ndateFormat HH:mm:ss.SSS\nsection s\na :00:00:00.000, 10ms", Options{}); err != nil {
		t.Errorf("ss.SSS: %v", err)
	}
	if _, err := Render(ctx, "gantt\ndateFormat Do MMM YYYY\nsection s\na :1st Jan 2020, 1d", Options{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Do: got %v, want ErrUnsupported", err)
	}
	// A missing glyph is ErrUnsupported at every MaxWidth (it was
	// ErrTooLarge at small widths).
	for _, w := range []int{0, 200, 30} {
		if _, err := Render(ctx, "graph LR\nA[日本語のラベル] --> B[もう一つ] --> C[三つ目]", Options{MaxWidth: w}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("MaxWidth %d: got %v, want ErrUnsupported", w, err)
		}
	}
}

// TestReReview16Fixes covers the findings of the sixteenth adversarial review.
func TestReReview16Fixes(t *testing.T) {
	// Diagrams that size themselves to MaxWidth came out 1-2 px too wide
	// (strokes past the content) and were shrunk and then scaled as a
	// bitmap to exactly MaxWidth. They now fit without that.
	for _, c := range []struct {
		f string
		w int
	}{{"packet_basic", 300}, {"packet_basic", 600}, {"treemap_basic", 300}, {"treemap_basic", 600}, {"quadrant_basic", 300}} {
		img, err := Render(context.Background(), readFile(t, "testdata/"+c.f+".mmd"), Options{MaxWidth: c.w})
		if err != nil {
			t.Fatal(err)
		}
		if w := img.Bounds().Dx(); w >= c.w {
			t.Errorf("%s at MaxWidth %d: %d px wide, so scaled to fit", c.f, c.w, w)
		}
	}
}

func TestReReview17Fixes(t *testing.T) {
	ctx := context.Background()
	// An architecture diagram that renders without MaxWidth failed with
	// MaxWidth because the fit retries used up the shared routing budget.
	// A retry out of budget now keeps the layout before it; what fails is
	// the width itself.
	sides := []string{"L", "R", "T", "B"}
	var b strings.Builder
	b.WriteString("architecture-beta\n")
	for i := range 5 {
		fmt.Fprintf(&b, "group g%d(cloud)[G%d]\n", i, i)
	}
	for i := range 20 {
		fmt.Fprintf(&b, "service s%d(server)[Service number %d] in g%d\n", i, i, i%5)
	}
	for i := range 150 {
		fmt.Fprintf(&b, "s%d:%s -- %s:s%d\n", i%20, sides[i%4], sides[(i/4)%4], (i*7+3)%20)
	}
	if _, err := Render(ctx, b.String(), Options{}); err != nil {
		t.Fatalf("without MaxWidth: %v", err)
	}
	for _, w := range []int{800, 2400} {
		_, err := Render(ctx, b.String(), Options{MaxWidth: w})
		if err != nil && strings.Contains(err.Error(), "routing steps") {
			t.Errorf("MaxWidth %d: %v", w, err)
		}
	}

	// erDiagram cardinalities in words or symbols, joined by any of
	// --, .., "to" and "optionally to".
	for _, rel := range []string{
		"A one or more--zero or many B : r",
		"A only one .. 0+ B : r",
		"A ||--zero or more B : r",
		"A 1+ optionally to o{ B : r",
		"A only one to zero or more B : r",
		"A ||--o{ B : r",
	} {
		if _, err := Render(ctx, "erDiagram\n"+rel, Options{}); err != nil {
			t.Errorf("%q: %v", rel, err)
		}
	}

	// A gantt task with no start date to follow is valid Mermaid (it
	// starts today); not drawing it is a limit of this package.
	_, err := Render(ctx, "gantt\ndateFormat YYYY-MM-DD\nA task : 30d", Options{})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("first task without a start: %v, want ErrUnsupported", err)
	}

	// mindmap icons and classes are left out of the drawing, as the
	// README says.
	if _, err := Render(ctx, "mindmap\n root\n  A\n  ::icon(fa fa-book)\n  B:::urgent", Options{}); err != nil {
		t.Errorf("mindmap icon and class: %v", err)
	}
}

func TestReReview18Fixes(t *testing.T) {
	// Sums and ranges that overflow point at the line that overflowed
	// them, not at the last line of the diagram.
	for _, c := range []struct {
		src  string
		line int
	}{
		{"pie\n\"a\" : 1e308\n\"b\" : 1e308\n\"c\" : 1\n", 3},
		{"sankey-beta\na,b,1e308\na,c,1e308\nb,c,1\n", 3},
		{"treemap-beta\n\"s\"\n  \"a\": 1e308\n  \"b\": 1e308\n\"t\": 1\n", 4},
		{"xychart-beta\nline [1e308]\nline [-1e308]\nbar [1]\n", 3},
	} {
		_, err := Render(context.Background(), c.src, Options{})
		var se *SyntaxError
		if !errors.As(err, &se) || se.Line != c.line {
			t.Errorf("%q: %v, want a syntax error on line %d", c.src, err, c.line)
		}
	}
}

func TestReReview19Fixes(t *testing.T) {
	// A state named inside a composite and given its own body later, at
	// the top, panicked (index -1) when an edge ended at the outer
	// composite.
	for _, src := range []string{
		"stateDiagram-v2\nstate A {\n  B\n}\nstate B {\n  [*] --> A\n}",
		"stateDiagram-v2\nstate A {\n  B\n}\nstate B {\n  C\n}\nC --> A\nA --> C",
		"stateDiagram-v2\nstate A {\n  B\n}\nstate B {\n  --\n C\n}\nA --> B",
	} {
		if _, err := render(context.Background(), src, Options{}); err != nil {
			t.Errorf("%q: %v", src, err)
		}
		if _, err := renderText(context.Background(), src, TextOptions{MaxCols: 120}); err != nil {
			t.Errorf("text %q: %v", src, err)
		}
	}
}

func TestReReview19Ticks(t *testing.T) {
	// More radar ticks than the package draws is its limit, not a syntax
	// error (README: limits are ErrTooLarge).
	for _, n := range []string{"1000000000", "99999999999999999999999"} {
		_, err := Render(context.Background(), "radar-beta\naxis a, b, c\ncurve x{1, 2, 3}\nticks "+n, Options{})
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("ticks %s: %v, want ErrTooLarge", n, err)
		}
	}
	var se *SyntaxError
	if _, err := Render(context.Background(), "radar-beta\naxis a, b, c\ncurve x{1, 2, 3}\nticks 0", Options{}); !errors.As(err, &se) {
		t.Errorf("ticks 0: %v, want a syntax error", err)
	}
}

func TestReReview19Marks(t *testing.T) {
	// Decomposed Latin text (as pasted from macOS) draws with the built-in
	// fonts: e + U+0301 is composed to \u00e9, which they have.
	if _, err := Render(context.Background(), "graph LR\nA[\"Cafe\u0301\"] --> B", Options{}); err != nil {
		t.Fatalf("NFD label: %v", err)
	}
}

func TestReReview20Fixes(t *testing.T) {
	ctx := context.Background()
	// An erDiagram block on one line is valid Mermaid.
	one, err := RenderText(ctx, "erDiagram\n  CUSTOMER { string name PK \"the name\" int age }\n  CUSTOMER ||--o{ ORDER : places", TextOptions{MaxCols: 120})
	if err != nil {
		t.Fatalf("one-line block: %v", err)
	}
	multi, err := RenderText(ctx, "erDiagram\n  CUSTOMER {\n    string name PK \"the name\"\n    int age\n  }\n  CUSTOMER ||--o{ ORDER : places", TextOptions{MaxCols: 120})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(one, "\n") != strings.Join(multi, "\n") {
		t.Errorf("one-line block:\n%s\nwant\n%s", strings.Join(one, "\n"), strings.Join(multi, "\n"))
	}
	var se *SyntaxError
	if _, err := Render(ctx, "erDiagram\n  CUSTOMER { string }", Options{}); !errors.As(err, &se) || se.Line != 2 {
		t.Errorf("attribute without a name: %v, want a syntax error on line 2", err)
	}
	// gantt vert markers (Mermaid 11) are valid Mermaid, not drawn.
	if _, err := Render(ctx, "gantt\ndateFormat YYYY-MM-DD\nA : a1, 2024-01-01, 3d\nDeadline : vert, v1, 2024-01-02, 0d", Options{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("vert marker: %v, want ErrUnsupported", err)
	}
	// Detect reports input over the size limit as ErrTooLarge.
	if _, err := Detect("graph TD\n" + strings.Repeat("A-->B\n", 200000)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Detect of a huge input: %v, want ErrTooLarge", err)
	}
}

func TestReReview21Fixes(t *testing.T) {
	ctx := context.Background()
	// Valid Mermaid that is not drawn is ErrUnsupported, not a syntax
	// error (README).
	for _, src := range []string{
		"sequenceDiagram\n A->>B: x\n par_over p\n A->>B: y\n end",
		"classDiagram\n A ()-- E",
		"classDiagram\n foo --() bar",
		"requirementDiagram\nrequirement r {\nid: 1\n}\nclassDef c fill:#f00\nr:::c",
	} {
		if _, err := Render(ctx, src, Options{}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
	}
	// A drawing over MaxPixels at full width failed with a wide MaxWidth
	// (or none) while a narrower one compacted it and drew: the compact
	// attempts are now tried for MaxPixels too.
	r := rand.New(rand.NewSource(1))
	var b strings.Builder
	b.WriteString("classDiagram\n")
	for range 340 {
		fmt.Fprintf(&b, "C%d --> C%d\n", r.Intn(150), r.Intn(150))
	}
	for _, w := range []int{0, 2400, 6000} {
		img, err := Render(ctx, b.String(), Options{MaxWidth: w})
		if err != nil {
			t.Errorf("MaxWidth %d: %v", w, err)
		} else if w > 0 && img.Bounds().Dx() > w {
			t.Errorf("MaxWidth %d: %d px wide", w, img.Bounds().Dx())
		}
	}
}

func TestReReview22Fixes(t *testing.T) {
	ctx := context.Background()
	// A variation selector (the emoji warning sign is U+26A0 U+FE0F) made Render return
	// ErrUnsupported for the whole diagram: fonts have no glyph for it.
	if _, err := Render(ctx, "graph TD\nA[\"ok\ufe0f \u00a9\ufe0e\"] --> B", Options{}); err != nil {
		t.Errorf("variation selectors: %v", err)
	}
	// (The gantt tick labels are in internal/diagram/gantt.)
}

func TestReReview23Fixes(t *testing.T) {
	ctx := context.Background()
	// Valid Mermaid this package does not follow is ErrUnsupported, not a
	// syntax error (an unknown branch) or a silently wrong drawing.
	for _, src := range []string{
		"%%{init: {'gitGraph': {'mainBranchName': 'trunk'}}}%%\ngitGraph\ncommit\nbranch dev\ncheckout trunk\ncommit",
		"---\nconfig:\n  gitGraph:\n    mainBranchName: trunk\n---\ngitGraph\ncommit",
		"gantt\ndateFormat YYYY-MM-DD\nexcludes weekends\nweekend friday\na : 2024-01-01, 3d",
		"gantt\ndateFormat YYYY-MM-DD\nexcludes weekends\nincludes 2024-01-06\na : 2024-01-01, 10d",
		"erDiagram\nCUSTOMER[\"Customer Name\"] ||--o{ ORDER : places",
		"erDiagram\nA:::foo ||--o{ B : r",
	} {
		if _, err := Render(ctx, src, Options{}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
		if _, err := RenderText(ctx, src, TextOptions{MaxCols: 120}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("text %q: %v, want ErrUnsupported", src, err)
		}
	}
	// What did work still does: mainBranchName main, includes without
	// excludes.
	for _, src := range []string{
		"%%{init: {'gitGraph': {'mainBranchName': 'main'}}}%%\ngitGraph\ncommit",
		"gantt\ndateFormat YYYY-MM-DD\nincludes 2024-01-06\na : 2024-01-01, 10d",
	} {
		if _, err := Render(ctx, src, Options{}); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
	// RenderText's canvas is bounded at about a million cells: a 1 MiB
	// label of 200000 lines took 500 MB.
	_, err := RenderText(ctx, "graph TD\nA[\""+strings.Repeat("a<br>", 200000)+"\"]-->B", TextOptions{MaxCols: 80})
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("200000-line label: %v, want ErrTooLarge", err)
	}
}

func TestReReview24Fixes(t *testing.T) {
	ctx := context.Background()
	// A drawing over MaxPixels with no MaxWidth failed while MaxWidth 1000
	// shrank its text and drew it: the text now shrinks for MaxPixels too.
	var b strings.Builder
	b.WriteString("requirementDiagram\n")
	for i := range 100 {
		fmt.Fprintf(&b, "requirement r%d {\nid: %d\n}\n", i, i)
	}
	x := uint32(12345) // xorshift, as the reviewer generated it
	rnd := func() int { x ^= x << 13; x ^= x >> 17; x ^= x << 5; return int(x % 100) }
	for range 390 {
		a, c := rnd(), rnd()
		fmt.Fprintf(&b, "r%d - satisfies -> r%d\n", a, c)
	}
	for _, w := range []int{1000, 0} {
		if _, err := Render(ctx, b.String(), Options{MaxWidth: w}); err != nil {
			t.Errorf("MaxWidth %d: %v", w, err)
		}
	}
	// Unix times beyond a JavaScript Date wrapped around in the task end
	// and drew a reversed axis without an error.
	var se *SyntaxError
	_, err := Render(ctx, "gantt\ndateFormat X\na : 9223372036854775807, 1s\nb : 0, 1s", Options{})
	if !errors.As(err, &se) || se.Line != 3 {
		t.Errorf("MaxInt64 seconds: %v, want a syntax error on line 3", err)
	}
	if _, err := Render(ctx, "gantt\ndateFormat X\na : 8640000000000, 1s\nb : 0, 1s", Options{}); err != nil {
		t.Errorf("the last JavaScript date: %v", err)
	}
}
