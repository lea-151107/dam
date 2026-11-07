package mermaid

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// goldenVersionFile records the Version that the golden files were made
// with and the hash of each of them, so that a change of the golden output
// without a change of Version (which callers use in cache keys) fails.
const goldenVersionFile = "testdata/golden.version"

// TestMain runs the tests and then checks the golden files against
// Version. The check runs after the tests so that it sees the golden files
// that -update has just written.
func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 {
		if err := checkGoldenVersion(*update); err != nil {
			fmt.Fprintln(os.Stderr, "FAIL: golden version:", err)
			code = 1
		}
	}
	os.Exit(code)
}

// checkGoldenVersion compares testdata/golden with the record.
//   - A golden file that existed before and changed while Version stayed
//     the same fails, even with -update: bump Version first.
//   - A golden file removed while Version stayed the same fails too:
//     renaming a case (remove and add) would otherwise let its output
//     change unnoticed.
//   - New golden files (new test cases) do not need a new Version.
//   - Any difference from the record fails without -update, so that the
//     record is not forgotten; -update rewrites it.
func checkGoldenVersion(update bool) error {
	cur, err := goldenHashes("testdata/golden")
	if err != nil {
		return err
	}
	want := formatRecord(Version, cur)
	rec, err := os.ReadFile(goldenVersionFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if string(rec) == want {
		return nil
	}
	recVersion, old := parseRecord(string(rec))
	if recVersion == Version {
		var changed []string
		for name, h := range cur {
			if oh, ok := old[name]; ok && oh != h {
				changed = append(changed, name)
			}
		}
		for name := range old {
			if _, ok := cur[name]; !ok {
				changed = append(changed, name+" (removed)")
			}
		}
		if len(changed) > 0 {
			sort.Strings(changed)
			return fmt.Errorf("golden files changed but Version is still %s: %s; bump Version, then run go test -update",
				Version, strings.Join(changed, ", "))
		}
	}
	if update {
		return os.WriteFile(goldenVersionFile, []byte(want), 0o644)
	}
	return fmt.Errorf("%s is out of date: run go test -update", goldenVersionFile)
}

// goldenHashes returns the sha256 of every file in dir by name.
func goldenHashes(dir string) (map[string]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		out[filepath.Base(p)] = hex.EncodeToString(sum[:])
	}
	return out, nil
}

// formatRecord writes "version V" and then one "hash name" line per file,
// sorted by name.
func formatRecord(version string, hashes map[string]string) string {
	names := make([]string, 0, len(hashes))
	for n := range hashes {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	fmt.Fprintf(&b, "version %s\n", version)
	for _, n := range names {
		fmt.Fprintf(&b, "%s %s\n", hashes[n], n)
	}
	return b.String()
}

func parseRecord(s string) (version string, hashes map[string]string) {
	hashes = map[string]string{}
	for _, l := range strings.Split(s, "\n") {
		if v, ok := strings.CutPrefix(l, "version "); ok {
			version = v
		} else if h, n, ok := strings.Cut(l, " "); ok {
			hashes[n] = h
		}
	}
	return version, hashes
}

// TestNoOrphanGoldens fails on golden files that no fixture produces: a
// renamed fixture leaves its old goldens behind, and removing them is what
// makes checkGoldenVersion ask for a new Version.
func TestNoOrphanGoldens(t *testing.T) {
	want := map[string]bool{}
	for _, f := range fixtures(t) {
		base := strings.TrimSuffix(filepath.Base(f), ".mmd")
		want[base+".png"], want[base+".txt"] = true, true
		for _, p := range terminalProfiles {
			// TestGoldenTerminal writes one of the two.
			want[base+"@"+p.name+".png"], want[base+"@"+p.name+".err"] = true, true
		}
		for _, cols := range textGoldenCols {
			// TestGoldenText writes one of the two for the narrow widths.
			c := "@" + strconv.Itoa(cols)
			want[base+c+".txt"], want[base+c+".err"] = true, true
		}
	}
	files, err := filepath.Glob("testdata/golden/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if name := filepath.Base(f); !want[name] {
			t.Errorf("testdata/golden/%s belongs to no fixture: remove it (and bump Version if a fixture was renamed)", name)
		}
	}
}
