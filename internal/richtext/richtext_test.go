package richtext

import (
	"reflect"
	"testing"
)

// show replaces markers by {style} for readable comparisons.
func show(s string) string {
	var out []rune
	for _, r := range s {
		if IsMarker(r) {
			out = append(out, '{', '0'+rune(r-base), '}')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func TestMarkup(t *testing.T) {
	for _, tc := range []struct {
		in       string
		markdown bool
		want     string
	}{
		{"plain", false, "plain"},
		{"a <b>bold</b> word", false, "a {1}bold{0} word"},
		{"<i>it <b>both</b></i>", false, "{2}it {3}both{2}"},
		{"<u>u</u> <span class='x'>s</span>", false, "{4}u{0} s"},
		{"x < y > z", false, "x < y > z"},
		{"**bold** and *it*", true, "{1}bold{0} and {2}it"},
		{"__b__ _i_", true, "{1}b{0} {2}i"},
		{"a*b", false, "a*b"},
		{"snake_case_name", true, "snake_case_name"},
		{"5 * 3 = 15", true, "5 * 3 = 15"},
		{"_lead_ and **x", true, "{2}lead{0} and **x"},
	} {
		if got := show(Markup(tc.in, tc.markdown)); got != tc.want {
			t.Errorf("Markup(%q, %v) = %q, want %q", tc.in, tc.markdown, got, tc.want)
		}
	}
}

func TestRunsAndCarry(t *testing.T) {
	s := Markup("a <b>bc</b>d", false)
	want := []Run{{"a ", Regular}, {"bc", Bold}, {"d", Regular}}
	if got := Runs(s); !reflect.DeepEqual(got, want) {
		t.Errorf("Runs = %+v, want %+v", got, want)
	}
	if got := Strip(s); got != "a bcd" {
		t.Errorf("Strip = %q", got)
	}
	lines := Carry([]string{string(Marker(Bold)) + "one", "two", string(Marker(Regular)) + "three"})
	if got := []string{show(lines[0]), show(lines[1]), show(lines[2])}; !reflect.DeepEqual(got, []string{"{1}one", "{1}two", "{1}{0}three"}) {
		t.Errorf("Carry = %q", got)
	}
}
