package screen

import (
	"strings"
	"testing"
)

func TestMouseBusyAndOptions(t *testing.T) {
	s := New()
	resize(s, 1, 4, 2)
	s.Apply(event("grid_cursor_goto", 1, 0, 0))
	check := func(mouse, cursor string) {
		t.Helper()
		frame := s.Render(1, 4, 2)
		if !strings.Contains(frame, mouse) || strings.Contains(frame, "\x1b[?25h") != (cursor == "visible") {
			t.Fatalf("wrong host state: %q", frame)
		}
	}
	check("\x1b[?1006l", "visible")
	s.Apply(event("mouse_on"), event("busy_start"))
	check("\x1b[?1002h", "hidden")
	// State is boolean, not a counter; repeated busy notifications are idempotent.
	s.Apply(event("busy_start"), event("busy_stop"), event("option_set", "mousemoveevent", true))
	check("\x1b[?1003h", "visible")
	s.Apply(event("option_set", "mousemoveevent", false), event("busy_stop"))
	check("\x1b[?1002h", "visible")
	before := s.Render(1, 4, 2)
	s.Apply(event("option_set", "guifont", "\x1b]52;unsafe"), event("option_set", "ambiwidth", "double"), event("option_set", "mousemoveevent", "bad"))
	if s.Render(1, 4, 2) != before {
		t.Fatal("unused/malformed options changed rendering")
	}
	s.Apply(event("mouse_off"))
	check("\x1b[?1006l", "visible")
}
