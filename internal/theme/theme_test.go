package theme

import (
	"image/color"
	"testing"
)

func TestParseColor(t *testing.T) {
	tests := []struct {
		in   string
		want color.NRGBA
		ok   bool
	}{
		{"#f00", color.NRGBA{255, 0, 0, 255}, true},
		{"#00ff0080", color.NRGBA{0, 255, 0, 128}, true},
		{"rgb(1, 2, 3)", color.NRGBA{1, 2, 3, 255}, true},
		{"rgba(1,2,3,0.5)", color.NRGBA{1, 2, 3, 128}, true},
		{"White", color.NRGBA{255, 255, 255, 255}, true},
		{"#12345", color.NRGBA{}, false},
		{"nope", color.NRGBA{}, false},
	}
	for _, tc := range tests {
		got, ok := ParseColor(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseColor(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseStyle(t *testing.T) {
	s := ParseStyle("fill:#f9f,stroke:#333,stroke-width:4px,color:#fff,stroke-dasharray: 5 5")
	if s.Fill == nil || *s.Fill != (color.NRGBA{0xff, 0x99, 0xff, 0xff}) {
		t.Errorf("fill = %v", s.Fill)
	}
	if s.StrokeWidth != 4 || !s.Dashed || s.Color == nil || s.Stroke == nil {
		t.Errorf("style = %+v", s)
	}
}

func TestNewDark(t *testing.T) {
	if th := New(color.White, nil); !th.Dark {
		t.Error("light foreground should select a dark theme")
	}
	if th := New(nil, color.Black); !th.Dark || th.Background.A != 255 {
		t.Error("black background should select a dark theme")
	}
	if th := New(nil, nil); th.Dark || th.Background.A != 0 {
		t.Error("default should be light and transparent")
	}
}
