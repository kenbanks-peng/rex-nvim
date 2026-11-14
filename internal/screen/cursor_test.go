package screen

import (
	"fmt"
	"strings"
	"testing"
)

func TestModeCursorShapes(t *testing.T) {
	for _, shape := range []struct {
		name string
		code int
	}{{"block", 2}, {"horizontal", 4}, {"vertical", 6}, {"unknown", 2}} {
		for _, blink := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/blink=%t", shape.name, blink), func(t *testing.T) {
				s := New()
				resize(s, 2, 4, 2)
				s.Apply(event("grid_cursor_goto", 2, 0, 0))
				mode := map[string]any{"cursor_shape": shape.name}
				if blink {
					mode["blinkon"], mode["blinkoff"] = 500, 500
				}
				s.Apply(event("mode_info_set", true, []any{mode}), event("mode_change", "normal", 0))
				code := shape.code
				if blink {
					code--
				}
				want := fmt.Sprintf("[%d q", code)
				if frame := s.Render(2, 4, 2); !strings.Contains(frame, want) {
					t.Fatalf("missing %q in %q", want, frame)
				}
				if frame := s.Render(3, 4, 2); strings.Contains(frame, " q") {
					t.Fatalf("cursor shape emitted in inactive pane: %q", frame)
				}
			})
		}
	}
}

func TestCursorModeChangesAndSettings(t *testing.T) {
	s := New()
	resize(s, 2, 4, 2)
	s.Apply(event("grid_cursor_goto", 2, 0, 0))
	check := func(code int) {
		t.Helper()
		want := fmt.Sprintf("[%d q", code)
		if frame := s.Render(2, 4, 2); !strings.Contains(frame, want) {
			t.Fatalf("missing %q in %q", want, frame)
		}
	}
	check(2)
	modes := []any{
		map[string]any{"cursor_shape": "block"},
		map[string]any{"cursor_shape": "vertical"},
		map[string]any{"cursor_shape": "horizontal", "blinkon": 500, "blinkoff": 0},
	}
	s.Apply(event("mode_info_set", true, modes), event("mode_change", "insert", 1))
	check(6)
	s.Apply(event("mode_change", "replace", 2))
	check(4)
	s.Apply(event("mode_change", "normal", 0))
	check(2)
	s.Apply(event("mode_change", "insert", 1))
	s.Apply(event("mode_info_set", true, []any{modes[0], modes[2]}))
	check(4)
	s.Apply(event("mode_info_set", false, modes))
	check(2)
	s.Apply(event("mode_info_set", true, modes), event("mode_change", "invalid", -1))
	check(2)
	s.Apply(event("mode_change", "invalid", 99))
	check(2)
}
