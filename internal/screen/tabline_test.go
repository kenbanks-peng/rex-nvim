package screen

import (
	"strings"
	"testing"
)

func TestTablineSnapshotNavigationAndFallback(t *testing.T) {
	s := New()
	s.SetExternalTabline(true)
	s.Apply(event("grid_resize", 1, 60, 10), event("grid_resize", 2, 60, 7), event("win_pos", 2, 100, 0, 0, 60, 7),
		event("tabline_update", 3, []any{map[string]any{"tab": 3, "name": "first"}, map[string]any{"tab": 9, "name": "other"}}, 3, []any{map[string]any{"buffer": 3, "name": "buffer\x1b]52;bad\a界"}, map[string]any{"buffer": 7, "name": "second"}}),
		event("option_set", "showtabline", 2), event("grid_cursor_goto", 2, 0, 0))
	snapshot := s.Tabline()
	snapshot.Tabs[0].Name = "mutated"
	if s.Tabline().Tabs[0].Name != "first" {
		t.Fatal("snapshot aliases state")
	}
	if s.TablineRows(2, 10) != 2 {
		t.Fatal("missing external rows")
	}
	frame := s.Render(2, 60, 10)
	if !strings.Contains(frame, "Nvim tabs:") || !strings.Contains(frame, "Buffers:") || strings.Contains(frame, "]52;bad\a") {
		t.Fatalf("bad chrome: %q", frame)
	}
	if !strings.Contains(frame, "\x1b[1;1H\x1b[?25h") {
		t.Fatal("chrome shifted cursor")
	}
	for _, tc := range []struct {
		row, col, handle int
		buffer           bool
	}{{8, 59, 9, false}, {8, 0, 9, false}, {9, 59, 7, true}, {9, 0, 7, true}} {
		target, ok := s.TablineTarget(2, 60, 10, tc.row, tc.col)
		if !ok || target.Handle != tc.handle || target.Buffer != tc.buffer {
			t.Fatalf("bad hit: %+v %v", target, ok)
		}
	}
	if _, ok := s.TablineTarget(2, 60, 10, 7, 0); ok {
		t.Fatal("content intercepted")
	}
	for _, h := range []int{1, 2, 3} {
		if s.TablineRows(2, h) != 0 {
			t.Fatal("tiny pane cropped")
		}
	}
	s.Apply(event("win_float_pos", 4, 104, "NW", 1, 0, 0, true), event("win_external_pos", 5, 105))
	if s.TablineRows(4, 10) != 0 || s.TablineRows(5, 10) != 0 {
		t.Fatal("chrome in float/external")
	}
	for _, mode := range []int{0, 1, 2} {
		s.Apply(event("option_set", "showtabline", mode))
		want := 2
		if mode == 0 {
			want = 0
		}
		if s.TablineRows(2, 10) != want {
			t.Fatal("showtabline transition")
		}
	}
	s.Apply(event("tabline_update", 9, []any{map[string]any{"tab": 9, "name": "renamed"}}, 7, []any{map[string]any{"buffer": 7, "name": "new"}}), event("option_set", "showtabline", 1))
	if s.TablineRows(2, 10) != 0 || len(s.Tabline().Tabs) != 1 || len(s.Tabline().Buffers) != 1 {
		t.Fatal("closed entries retained")
	}
	s.SetExternalTabline(false)
	s.SetTabCount(2)
	s.Apply(event("grid_line", 1, 0, 0, []any{[]any{"GRID TABLINE"}}, false))
	if s.TablineRows(2, 10) != 1 || !strings.Contains(s.Render(2, 60, 10), "GRID TABLINE") {
		t.Fatal("grid fallback missing")
	}
	if _, ok := s.TablineTarget(2, 60, 10, 9, 0); !ok {
		t.Fatal("fallback tabline not hittable")
	}
}

func TestTablineOverflowAndMalformedEntries(t *testing.T) {
	s := New()
	s.SetExternalTabline(true)
	s.Apply(event("tabline_update", 4, []any{nil, map[string]any{"tab": -1, "name": "bad"}, map[string]any{"tab": 4, "name": strings.Repeat("x", 1000)}, map[string]any{"tab": 8, "name": "last"}}, 4, []any{}))
	if len(s.Tabline().Tabs) != 2 {
		t.Fatal("invalid handles retained")
	}
	for _, width := range []int{1, 8, 9, 12, 80} {
		cells, hits := navigationRow(width, "Nvim tabs:", s.tabline.Tabs, 4, false)
		if len(cells) != width || len(hits) != width {
			t.Fatal("width overflow")
		}
		if width >= 9 && (hits[0].Handle != 8 || hits[width-1].Handle != 8) {
			t.Fatal("overflow unreachable")
		}
	}
	if UIOptions(true)["ext_tabline"] != true || UIOptions(false)["ext_tabline"] != nil {
		t.Fatal("extension gating")
	}
}
