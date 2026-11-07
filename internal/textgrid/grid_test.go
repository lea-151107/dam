package textgrid

import (
	"reflect"
	"testing"
)

func TestWidth(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"abc", 3}, {"日本語", 6}, {"aあb", 4}, {"ｱ", 1}, {"Ａ", 2}, {"é", 1}, {"한글", 4},
	}
	for _, tc := range tests {
		if got := Width(tc.in); got != tc.want {
			t.Errorf("Width(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestWrap(t *testing.T) {
	tests := []struct {
		in   string
		cols int
		want []string
	}{
		{"hello world foo", 11, []string{"hello world", "foo"}},
		{"日本語の文章です", 6, []string{"日本語", "の文章", "です"}},
		{"abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"a\nb", 0, []string{"a", "b"}},
	}
	for _, tc := range tests {
		if got := Wrap(tc.in, tc.cols); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Wrap(%q, %d) = %q, want %q", tc.in, tc.cols, got, tc.want)
		}
	}
}

func TestGrid(t *testing.T) {
	g, err := New(8, 3)
	if err != nil {
		t.Fatal(err)
	}
	g.Box(0, 0, 6, 3, [4]rune{})
	g.Text(1, 1, "日本")
	g.HLine(5, 7, 1)
	want := []string{"┌────┐", "│日本├──", "└────┘"}
	if got := g.Lines(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
