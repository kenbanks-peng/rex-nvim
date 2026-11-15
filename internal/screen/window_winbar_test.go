package screen

import (
	"strings"
	"testing"
)

func TestTiledWinbarUsesFloatingRegion(t *testing.T) {
	s := New()
	s.SetWindowUI(true)
	resize(s, 1, 40, 12)
	resize(s, 2, 19, 10)
	position(s, 2, 0, 0, 19, 10)
	line(s, 2, 0, "WINDOW WINBAR")
	line(s, 2, 1, "BUFFER TEXT")
	s.Apply(event("win_viewport_margins", 2, 102, 1, 0, 0, 0))
	found := false
	for _, v := range s.Views() {
		frame := plain(s.Render(v.Grid, v.Width, v.Height))
		if v.Grid == 2 && strings.Contains(frame, "WINDOW WINBAR") {
			t.Fatal("winbar leaked into editing block")
		}
		if v.Grid < windowWinbar {
			found = true
			if !v.Float || v.Row != 0 || v.Col != 0 || v.Width != 19 || v.Height != 1 || !strings.HasPrefix(frame, "WINDOW WINBAR") {
				t.Fatalf("winbar = %+v %q", v, frame)
			}
		}
	}
	if !found {
		t.Fatal("no floating winbar")
	}
	s.Apply(event("win_viewport_margins", 2, 999, 0, 0, 0, 0))
	if s.grids[2].topMargin != 1 {
		t.Fatal("foreign-window margins applied")
	}
	s.Apply(event("win_hide", 2))
	for _, v := range s.Views() {
		if v.Grid < windowWinbar {
			t.Fatal("hidden winbar remains")
		}
	}
	s.Apply(event("grid_destroy", 2))
	resize(s, 2, 19, 10)
	position(s, 2, 0, 0, 19, 10)
	for _, v := range s.Views() {
		if v.Grid < windowWinbar {
			t.Fatal("reused grid inherited winbar")
		}
	}
}
