package mermaid

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// TestNoFMA compiles the module for arm64 and amd64 v3 and fails if the
// compiler fused any multiply and add into an FMA instruction. Fusion
// changes the last bit of results, and a layout value on a boundary (a
// point exactly on a node outline, say) then changes the drawing: the
// golden images, made on amd64 without FMA, would not hold on Apple
// Silicon. Products are wrapped in float64(...) to prevent it (see
// docs/CODING_GUIDELINES.md §6).
func TestNoFMA(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the module twice")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
	fused := regexp.MustCompile(`\(([^)]*\.go:\d+)\)\s+(VFN?M(ADD|SUB)\w*|FN?M(ADD|SUB)[DS])\b`)
	for _, env := range [][]string{{"GOARCH=arm64"}, {"GOARCH=amd64", "GOAMD64=v3"}} {
		cmd := exec.Command("go", "build", "-a", "-gcflags=github.com/lea/mermaid/...=-S", "./...")
		cmd.Env = append(append(os.Environ(), "CGO_ENABLED=0"), env...)
		out, err := cmd.CombinedOutput()
		if err != nil && !strings.Contains(string(out), "TEXT") {
			t.Fatalf("%v: %v\n%s", env, err, out)
		}
		seen := map[string]bool{}
		for _, m := range fused.FindAllStringSubmatch(string(out), -1) {
			if loc := m[1]; !seen[loc] && !strings.HasSuffix(strings.Split(loc, ":")[0], "_test.go") {
				seen[loc] = true
				t.Errorf("%v: fused multiply-add at %s", env, loc)
			}
		}
	}
}
