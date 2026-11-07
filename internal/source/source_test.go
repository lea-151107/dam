package source

import (
	"errors"
	"testing"
)

func TestSplit(t *testing.T) {
	src := "---\ntitle: x\n---\n\n%%{init: {}}%%\ngraph TD\n  %% comment\n  A-->B  \n%%{\nmulti\n}%%\nC\n"
	lines, err := Split(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []Line{{6, "graph TD"}, {8, "  A-->B"}, {12, "C"}}
	if len(lines) != len(want) {
		t.Fatalf("got %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d: got %+v, want %+v", i, lines[i], want[i])
		}
	}
}

func TestSplitErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		line int
	}{
		{"front matter", "---\ntitle: x\n", 1},
		{"directive", "graph\n%%{init:\n", 2},
		{"utf8", "graph\n\xff", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Split(tc.src)
			var se *SyntaxError
			if !errors.As(err, &se) || se.Line != tc.line {
				t.Errorf("got %v, want syntax error at line %d", err, tc.line)
			}
		})
	}
}

func TestLabel(t *testing.T) {
	tests := []struct{ in, want string }{
		{`"quoted"`, "quoted"},
		{"a<br>b<br/>c<BR />d", "a\nb\nc\nd"},
		{"#quot;x#quot; #35; &amp; &#35;", `"x" # & #`},
		{"<b>bold</b> <i>it</i>", "\U0010fff1bold\U0010fff0 \U0010fff2it"},
		{"`**md** text`", "\U0010fff1md\U0010fff0 text"},
		{"**not markdown**", "**not markdown**"},
		// An escaped tag stays text; an entity cannot produce a marker.
		{"#lt;b#gt;x", "<b>x"},
		{"a#1114097;b &#1114098; c", "a b   c"},
		{`line\nbreak`, "line\nbreak"},
	}
	for _, tc := range tests {
		if got := Label(tc.in); got != tc.want {
			t.Errorf("Label(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSplitDoc(t *testing.T) {
	for _, tc := range []struct {
		name, src    string
		title, theme string
		vars         map[string]string
		lines        []Line
	}{
		{name: "front matter", src: "---\ntitle: Hello World\nconfig:\n  theme: forest\n  themeVariables:\n    primaryColor: \"#ff0000\"\n---\ngraph TD\nA",
			title: "Hello World", theme: "forest", vars: map[string]string{"primaryColor": "#ff0000"}, lines: []Line{{8, "graph TD"}, {9, "A"}}},
		{name: "init directive", src: "%%{init: {'theme': 'dark', \"themeVariables\": {lineColor: '#00f', fontSize: 16,}}}%%\ngraph TD",
			theme: "dark", vars: map[string]string{"lineColor": "#00f", "fontSize": "16"}, lines: []Line{{2, "graph TD"}}},
		{name: "multi-line directive", src: "%%{\n  init: {\n    'theme': 'neutral'\n  }\n}%%\ngraph TD",
			theme: "neutral", lines: []Line{{6, "graph TD"}}},
		{name: "bare directive", src: "%%{wrap}%%\nsequenceDiagram", lines: []Line{{2, "sequenceDiagram"}}},
		{name: "directive wins over front matter", src: "---\nconfig:\n  theme: forest\n---\n%%{init: {'theme': 'base'}}%%\ngraph",
			theme: "base", lines: []Line{{6, "graph"}}},
		{name: "markdown string", src: "graph TD\nA[\"`one\n  two`\"] --> B\nC", lines: []Line{{1, "graph TD"}, {2, "A[\"`one\ntwo`\"] --> B"}, {4, "C"}}},
		// Other diagram types have no markdown strings: "` is text there.
		{name: "no markdown in sequence", src: "sequenceDiagram\nA->>B: run \"`ls\"\nB->>A: ok", lines: []Line{{1, "sequenceDiagram"}, {2, "A->>B: run \"`ls\""}, {3, "B->>A: ok"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := SplitDoc(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			if d.Title != tc.title || d.Theme != tc.theme {
				t.Errorf("title %q theme %q, want %q %q", d.Title, d.Theme, tc.title, tc.theme)
			}
			if len(d.Vars) != len(tc.vars) {
				t.Errorf("vars %v, want %v", d.Vars, tc.vars)
			}
			for k, v := range tc.vars {
				if d.Vars[k] != v {
					t.Errorf("var %s = %q, want %q", k, d.Vars[k], v)
				}
			}
			if len(d.Lines) != len(tc.lines) {
				t.Fatalf("lines %+v, want %+v", d.Lines, tc.lines)
			}
			for i := range tc.lines {
				if d.Lines[i] != tc.lines[i] {
					t.Errorf("line %d: %+v, want %+v", i, d.Lines[i], tc.lines[i])
				}
			}
		})
	}
}

func TestSplitDocErrors(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		line      int
	}{
		{"bad directive", "graph TD\n%%{init: {theme: }}%%", 2},
		{"unclosed directive object", "%%{init: {theme: dark}%%\ngraph", 1},
		{"unterminated directive string", "%%{init: {'theme: 1}}%%\ngraph", 1},
		{"unterminated markdown string", "graph TD\nA --> B\nC[\"`open\nstill open", 3},
		{"unclosed front matter list", "---\ntitle: [unclosed\nconfig:\n  theme: dark\n---\ngraph TD", 2},
		{"unclosed front matter object", "\n---\ntitle: T\nconfig: {theme: dark\n---\ngraph TD", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SplitDoc(tc.src)
			var se *SyntaxError
			if !errors.As(err, &se) || se.Line != tc.line {
				t.Errorf("got %v, want syntax error at line %d", err, tc.line)
			}
		})
	}
}

func TestComposeMarks(t *testing.T) {
	// Only a character with combining marks is composed; a CJK
	// compatibility ideograph, which NFC would replace, stays as written.
	lines, err := Split("graph\nA[\"Cafe\u0301 \uf91d\"]")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := lines[1].Text, "A[\"Caf\u00e9 \uf91d\"]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
