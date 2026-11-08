package mermaid

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDependencies enforces the pure Go policy: no cgo, no external
// commands, no network, and only the standard library plus x/image.
func TestDependencies(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not available")
	}
	out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}} {{.Standard}} {{.Module}}", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		pkg, std := f[0], f[1] == "true"
		switch {
		case pkg == "runtime/cgo", pkg == "os/exec", pkg == "net", strings.HasPrefix(pkg, "net/"), pkg == "plugin":
			t.Errorf("forbidden dependency %s", pkg)
		case std:
		case strings.HasPrefix(pkg, "github.com/lea-151107/dam"), strings.HasPrefix(pkg, "golang.org/x/image/"):
		case pkg == "golang.org/x/sys/cpu", strings.HasPrefix(pkg, "golang.org/x/text/"):
			// Pure Go: CPU feature detection for vector (via x/image) and
			// x/text (legacy encodings for sfnt; unicode/norm, imported by
			// internal/source to compose combining marks).
		default:
			t.Errorf("unexpected third-party dependency %s", pkg)
		}
	}

	// Our own packages must not import unsafe or perform file I/O.
	out, err = exec.Command("go", "list", "-f", "{{.ImportPath}} {{join .Imports \" \"}}", "./...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if strings.Contains(f[0], "/cmd/") {
			continue
		}
		for _, imp := range f[1:] {
			if imp == "unsafe" || imp == "os" || imp == "os/exec" || imp == "syscall" {
				t.Errorf("%s imports %s", f[0], imp)
			}
		}
	}
}

// layers lists, for every internal package, the internal packages it may
// import (CODING_GUIDELINES.md §3). Keys and values are relative to
// internal/ and may use path.Match patterns. A new package must be added
// here (and to the guideline's diagram) before it can be used.
var layers = map[string][]string{
	"diagram/*":   {"boxgraph", "chart", "diagram", "guard", "layout/*", "richtext", "scene", "source", "textgraph", "textgrid", "textmeasure", "theme"},
	"textgraph":   {"guard", "layout/*", "scene", "textgrid"},
	"boxgraph":    {"diagram", "layout/*", "scene", "textgraph", "textgrid"},
	"diagram":     {"guard", "scene", "textmeasure", "theme"},
	"layout/*":    {"guard", "scene"},
	"source":      {"guard", "richtext"},
	"raster":      {"guard", "richtext", "scene"},
	"textgrid":    {"guard", "richtext"},
	"textmeasure": {"outline", "richtext"},
	"chart":       nil,
	"richtext":    nil,
	"outline":     {"curve"},
	"curve":       nil,
	"scene":       nil,
	"theme":       nil,
	"guard":       nil,
}

// TestLayering enforces the one-way dependency direction between internal
// packages, including that diagram types do not import each other.
func TestLayering(t *testing.T) {
	checkLayering(t, "github.com/lea-151107/dam/internal/", layers)
}
