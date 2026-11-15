package screen

import (
	"strings"
	"testing"
)

func TestWindowChromeIsSharedNotRepeatedInSplits(t *testing.T) {
	s := New()
	s.SetWindowUI(true)
	resize(s, 1, 40, 10)
	resize(s, 2, 19, 8)
	resize(s, 3, 20, 8)
	position(s, 2, 0, 0, 19, 8)
	position(s, 3, 0, 20, 20, 8)
	line(s, 2, 0, "left content")
	line(s, 3, 0, "right content")
	line(s, 1, 8, "GLOBAL STATUS ACROSS BOTH SPLITS")
	line(s, 1, 9, ":full window command")
	s.Apply(event("grid_cursor_goto", 1, 9, 3))
	for _, id := range []int{2, 3} {
		frame := plain(s.Render(id, 20, 10))
		if strings.Contains(frame, "GLOBAL") || strings.Contains(frame, ":full") {
			t.Fatalf("chrome leaked into tile %d: %q", id, frame)
		}
	}
	var chrome []View
	for _, v := range s.Views() {
		if v.Grid < 0 {
			chrome = append(chrome, v)
		}
	}
	if len(chrome) != 1 || chrome[0].Width != 40 || chrome[0].Row != 8 || chrome[0].Height != 2 {
		t.Fatalf("chrome = %+v", chrome)
	}
	frame := s.Render(chrome[0].Grid, 40, 2)
	if text := plain(frame); !strings.Contains(text, "GLOBAL STATUS ACROSS BOTH SPLITS") || !strings.Contains(text, ":full window command") {
		t.Fatalf("chrome = %q", text)
	}
	if !strings.Contains(frame, "\x1b[2;4H\x1b[?25h") {
		t.Fatalf("command cursor not translated: %q", frame)
	}
}

func TestWindowTablineAndMessageLayers(t *testing.T) {
	s := New()
	s.SetWindowUI(true)
	resize(s, 1, 30, 12)
	resize(s, 2, 30, 9)
	position(s, 2, 1, 0, 30, 9)
	s.Apply(event("option_set", "showtabline", 2))
	line(s, 1, 0, "CUSTOM TABLINE")
	line(s, 2, 0, "first buffer row")
	if got := plain(s.Render(2, 30, 12)); !strings.HasPrefix(got, strings.Repeat(" ", 30)+"first buffer row") {
		t.Fatalf("tabline reservation = %q", got)
	}
	if s.TablineRows(2, 12) != 0 {
		t.Fatal("pane-local navigation reserved")
	}
	resize(s, 4, 30, 4)
	line(s, 4, 0, "native message")
	s.Apply(event("msg_set_pos", 4, 8, false, " ", 200, 5))
	s.Apply(event("grid_cursor_goto", 4, 1, 7))
	views := s.windowViews()
	if len(views) != 3 {
		t.Fatalf("layers = %+v", views)
	}
	g, v := s.windowRegion(windowMessages)
	if g == nil || v.Row != 8 || v.Height != 4 || v.ZIndex != 200 {
		t.Fatalf("message geometry = %+v", v)
	}
	if got := s.Render(windowMessages, 30, 4); !strings.Contains(plain(got), "native message") || !strings.Contains(got, "\x1b[2;8H\x1b[?25h") {
		t.Fatalf("message frame = %q", got)
	}
	s.Apply(event("grid_destroy", 4))
	if len(s.windowViews()) != 2 {
		t.Fatal("destroyed message layer remains")
	}
	s.Apply(event("option_set", "showtabline", 0))
	if len(s.windowViews()) != 1 {
		t.Fatal("hidden tabline remains")
	}
}
