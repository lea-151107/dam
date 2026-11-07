package mermaid

import (
	"os/exec"
	"path"
	"strings"
	"testing"
)

// checkLayering fails for every import between internal packages (those
// starting with prefix) that rules does not allow. Keys and values of
// rules are paths relative to prefix and may be path.Match patterns.
func checkLayering(t *testing.T, prefix string, rules map[string][]string) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not available")
	}
	out, err := exec.Command("go", "list", "-f", "{{.ImportPath}} {{join .Imports \" \"}}", "./internal/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		pkg := strings.TrimPrefix(f[0], prefix)
		allowed, ok := matchRule(rules, pkg)
		if !ok {
			t.Errorf("internal/%s has no entry in the layering table", pkg)
			continue
		}
		for _, imp := range f[1:] {
			dep, internal := strings.CutPrefix(imp, prefix)
			if internal && !matchAny(allowed, dep) {
				t.Errorf("internal/%s must not import internal/%s", pkg, dep)
			}
		}
	}
}

// matchRule returns the rule for pkg. An exact key wins over a pattern.
func matchRule(rules map[string][]string, pkg string) ([]string, bool) {
	if r, ok := rules[pkg]; ok {
		return r, true
	}
	for k, r := range rules {
		if ok, _ := path.Match(k, pkg); ok {
			return r, true
		}
	}
	return nil, false
}

func matchAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, s); ok {
			return true
		}
	}
	return false
}
