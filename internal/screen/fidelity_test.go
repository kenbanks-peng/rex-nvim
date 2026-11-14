package screen

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnderlineStylesAndSpecialDefaults(t *testing.T) {
	s := New()
	for i, key := range []string{"underline", "underdouble", "undercurl", "underdotted", "underdashed"} {
		t.Run(key, func(t *testing.T) {
			s.Apply(event("hl_attr_define", 7, map[string]any{key: true}, nil, nil))
			s.Apply(event("default_colors_set", 0x112233, 0x445566, 0x010203, 1, 2))
			want := "4"
			if i > 0 {
				want = fmt.Sprintf("4:%d", i+1)
			}
			style := s.style(7)
			if !strings.Contains(style, ";"+want+";") || !strings.Contains(style, "58:2::1:2:3m") {
				t.Fatalf("wrong underline style/color: %q", style)
			}
			s.Apply(event("default_colors_set", -1, -1, 0xaabbcc, 1, 2))
			if !strings.Contains(s.style(7), "58:2::170:187:204m") {
				t.Fatal("special default frozen")
			}
			s.Apply(event("default_colors_set", -1, -1, -1, 1, 2))
			if strings.Contains(s.style(7), "58:") {
				t.Fatal("unknown special should follow terminal foreground")
			}
		})
	}
}

func TestHighlightRGBDefaultsAndRedefinition(t *testing.T) {
	s := New()
	s.Apply(event("default_colors_set", 0x112233, 0x445566, -1, 1, 2))
	s.Apply(event("hl_attr_define", 9, map[string]any{"bold": true}, map[string]any{"foreground": 1, "background": 2, "italic": true}, nil))
	if got := s.style(9); got != "\x1b[0;1;38;2;17;34;51;48;2;68;85;102m" {
		t.Fatalf("cterm overrides RGB defaults: %q", got)
	}
	s.Apply(event("default_colors_set", 0, 0xffffff, -1, 3, 4))
	if got := s.style(9); got != "\x1b[0;1;38;2;0;0;0;48;2;255;255;255m" {
		t.Fatalf("defaults not live: %q", got)
	}
	s.Apply(event("hl_attr_define", 9, map[string]any{"foreground": 0}, nil, nil))
	if strings.Contains(s.style(9), ";1;") {
		t.Fatal("redefined ID retained bold")
	}
	s.Apply(event("default_colors_set"))
	for _, id := range []int{0, 88} {
		if got := s.style(id); got != "\x1b[0m" {
			t.Fatalf("missing defaults/ID: %q", got)
		}
	}
	s.Apply(event("hl_attr_define", 0, map[string]any{"bold": true, "background": 0xffffff}, nil, nil))
	if got := s.style(0); got != "\x1b[0m" {
		t.Fatalf("ID zero is always default: %q", got)
	}
}

func TestAdditionalHighlightAttributesAndOpaqueDegradation(t *testing.T) {
	s := New()
	s.Apply(event("hl_attr_define", 1, map[string]any{"dim": true, "blink": true, "conceal": true, "overline": true}, nil, nil))
	if got := s.style(1); got != "\x1b[0;2;5;8;53m" {
		t.Fatalf("attributes: %q", got)
	}
	for _, blend := range []int{0, 50, 100} {
		s.Apply(event("hl_attr_define", 2, map[string]any{"blend": blend, "altfont": true, "url": "https://example.test/\x1b]52;bad", "background": 0x112233}, nil, nil))
		if got := s.style(2); got != "\x1b[0;48;2;17;34;51m" {
			t.Fatalf("opaque/font/URL fallback: %q", got)
		}
	}
	// Opaque degradation applies to floats too, including blank cells at blend=100.
	resize(s, 2, 2, 1)
	position(s, 2, 0, 0, 2, 1)
	line(s, 2, 0, "ab")
	resize(s, 3, 1, 1)
	s.Apply(event("grid_line", 3, 0, 0, []any{[]any{" ", 2}}), event("win_float_pos", 3, 103, "NW", 2, 0, 0, false, 50))
	if got := plain(s.Render(2, 2, 1)); got != " b" {
		t.Fatalf("blend must not expose a different background between panes: %q", got)
	}
}

func TestCursorHighlightResolutionAndReset(t *testing.T) {
	s := New()
	resize(s, 2, 2, 1)
	s.Apply(event("default_colors_set", 0x112233, 0x445566, -1), event("grid_cursor_goto", 2, 0, 0))
	check := func(attr int, want string) {
		t.Helper()
		s.Apply(event("mode_info_set", true, []any{map[string]any{"attr_id": attr, "cursor_shape": "vertical"}}))
		frame := s.Render(2, 2, 1)
		if !strings.Contains(frame, want) {
			t.Fatalf("attr %d missing %q: %q", attr, want, frame)
		}
		if strings.Contains(s.Render(3, 2, 1), "\x1b]12") {
			t.Fatal("color emitted in inactive pane")
		}
	}
	check(0, "\x1b]12;#112233\x1b\\")
	s.Apply(event("hl_attr_define", 7, map[string]any{"foreground": 0xaabbcc, "background": 0x010203}, nil, nil))
	check(7, "\x1b]12;#010203\x1b\\")
	s.Apply(event("hl_attr_define", 7, map[string]any{"reverse": true, "foreground": 0xaabbcc}, nil, nil))
	check(7, "\x1b]12;#aabbcc\x1b\\")
	s.Apply(event("grid_line", 2, 0, 0, []any{[]any{"x", 7}}))
	check(0, "\x1b]12;#445566\x1b\\")
	check(99, "\x1b]112\x1b\\")
	s.Apply(event("mode_info_set", false, nil))
	if !strings.Contains(s.Render(2, 2, 1), "\x1b]112\x1b\\") {
		t.Fatal("disabled cursor style retained color")
	}
	s.Apply(event("hl_attr_define", 8, map[string]any{}, nil, nil))
	check(8, "\x1b]12;#445566\x1b\\")
	s.Apply(event("default_colors_set", -1, -1, -1))
	check(8, "\x1b]112\x1b\\")
}

func TestCursorLanguageMapDegradesToPrimaryHighlight(t *testing.T) {
	s := New()
	resize(s, 2, 1, 1)
	s.Apply(event("grid_cursor_goto", 2, 0, 0),
		event("hl_attr_define", 7, map[string]any{"background": 0x112233, "blend": 100}, nil, nil),
		event("hl_attr_define", 8, map[string]any{"background": 0xaabbcc}, nil, nil),
		event("mode_info_set", true, []any{map[string]any{"attr_id": 7, "attr_id_lm": 8}}))
	if got := s.Render(2, 1, 1); !strings.Contains(got, "\x1b]12;#112233\x1b\\") {
		t.Fatalf("primary opaque cursor fallback: %q", got)
	}
}

func TestCursorPercentageAndTimingDegradeToNativeShape(t *testing.T) {
	for _, percent := range []int{0, 1, 25, 99, 100} {
		for _, times := range [][3]int{{0, 0, 0}, {100, 1, 900}, {900, 900, 1}, {100, 500, 0}, {100, 0, 500}} {
			s := New()
			resize(s, 2, 1, 1)
			s.Apply(event("grid_cursor_goto", 2, 0, 0), event("mode_info_set", true, []any{map[string]any{
				"cursor_shape": "vertical", "cell_percentage": percent, "blinkwait": times[0], "blinkon": times[1], "blinkoff": times[2],
			}}))
			code := 6
			if times[1] > 0 && times[2] > 0 {
				code = 5
			}
			if !strings.Contains(s.Render(2, 1, 1), fmt.Sprintf("\x1b[%d q", code)) {
				t.Fatalf("percent=%d timings=%v", percent, times)
			}
		}
	}
}

func TestWrapMetadataDoesNotReflowOrScrollFrames(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		s := New()
		resize(s, 2, 3, 2)
		s.Apply(event("grid_line", 2, 0, 0, []any{[]any{"a", 0, 3}}, wrap), event("grid_line", 2, 1, 0, []any{[]any{"b", 0, 3}}, false))
		for i := 0; i < 2; i++ {
			if got := plain(s.Render(2, 3, 2)); got != "aaabbb" {
				t.Fatalf("wrap=%t: %q", wrap, got)
			}
		}
		if got := plain(s.Render(2, 2, 2)); got != "aabb" {
			t.Fatalf("clipping reflowed: %q", got)
		}
	}
}

func TestFloatCompositionOrderAndResolvedCoordinates(t *testing.T) {
	s := New()
	resize(s, 1, 8, 5)
	resize(s, 2, 8, 4)
	position(s, 2, 0, 0, 8, 4)
	for _, id := range []int{3, 4} {
		resize(s, id, 1, 1)
	}
	line(s, 3, 0, "A")
	line(s, 4, 0, "B")
	// compindex wins over grid ID and zindex; screen coordinates win over anchors.
	s.Apply(event("win_float_pos", 3, 103, "SE", 2, 99.0, 99.0, false, 90, 5, 1, 2),
		event("win_float_pos", 4, 104, "NW", 2, 99.0, 99.0, false, 100, 4, 1, 2))
	for _, v := range s.Views() {
		if v.Grid == 3 && (!v.HasCompositionIndex || v.CompositionIndex != 5 || v.ZIndex != 90 || v.Row != 1 || v.Col != 2) {
			t.Fatalf("host placement/order metadata lost: %+v", v)
		}
	}
	if got := plain(s.Render(2, 8, 4)); got[10] != 'A' {
		t.Fatalf("composition order/resolved position: %q", got)
	}
	// Reconfiguration without new fields drops stale coordinates and composition order.
	s.Apply(event("win_float_pos", 3, 103, "NW", 2, 0, 0, false, 50),
		event("win_float_pos", 4, 104, "NW", 2, 0, 0, false, 50))
	if got := plain(s.Render(2, 8, 4)); got[0] != 'B' {
		t.Fatalf("old protocol tie-break: %q", got)
	}
	s.Apply(event("win_hide", 3), event("win_float_pos", 4, 104, "NW", 3, 0, 0, false, 100, 10, 0, 0))
	if got := plain(s.Render(2, 8, 4)); strings.Contains(got, "B") {
		t.Fatalf("hidden ancestry bypassed: %q", got)
	}
}

func TestMessageCompositionOrderAndScrolledDegradation(t *testing.T) {
	s := New()
	resize(s, 1, 4, 4)
	resize(s, 2, 4, 3)
	position(s, 2, 0, 0, 4, 3)
	s.Apply(event("grid_cursor_goto", 2, 0, 0))
	resize(s, 3, 4, 3)
	line(s, 3, 0, "msg1")
	line(s, 3, 1, "msg2")
	line(s, 3, 2, "msg3")
	resize(s, 4, 1, 1)
	line(s, 4, 0, "F")
	s.Apply(event("win_float_pos", 4, 104, "NW", 2, 2, 0, false, 250, 6, 2, 0), event("msg_set_pos", 3, 1, true, "-", 200, 5))
	// Scrolled messages clip from the bottom; optional separator is not invented.
	if got := plain(s.Render(2, 4, 2)); got != "msg2msg3" {
		t.Fatalf("message clipping: %q", got)
	}
	if got := plain(s.Render(2, 4, 3)); got != "msg1msg2Fsg3" {
		t.Fatalf("float above message: %q", got)
	}
	s.Apply(event("msg_set_pos", 3, 1, false, "-", 200, 7))
	if got := plain(s.Render(2, 4, 3)); got != "msg1msg2msg3" {
		t.Fatalf("message composition order: %q", got)
	}
}
