package screen

import (
	"fmt"
	"strings"
	"testing"
)

func popupFixture() *State {
	s := New()
	s.SetExternalPopupmenu(true)
	s.Apply(event("grid_resize", 1, 80, 24), event("grid_resize", 2, 40, 20), event("win_pos", 2, 102, 0, 0, 40, 20),
		event("grid_resize", 3, 40, 20), event("win_pos", 3, 103, 0, 40, 40, 20),
		event("grid_resize", 4, 18, 8), event("win_float_pos", 4, 104, "NW", 3, 2, 3, true),
		event("grid_cursor_goto", 3, 1, 1))
	return s
}
func popupItems(count int) []any {
	var items []any
	for i := 0; i < count; i++ {
		items = append(items, []any{fmt.Sprintf("item%02d", i), "K", "source", "documentation"})
	}
	return items
}
func TestPopupPlacementScrollingAndLifecycle(t *testing.T) {
	s := popupFixture()
	s.SetExternalGrids(map[int]bool{2: true, 3: true, 4: true})
	for _, id := range []int{2, 3, 4} {
		s.Apply(event("popupmenu_show", popupItems(30), -1, 1, 2, id))
		g, ok := s.PopupGeometry(id, 18, 8)
		if !ok || g.Grid != id || g.Col != 1 || g.Row != 2 || g.Height != 6 {
			t.Fatalf("geometry: %+v %v", g, ok)
		}
		r, c, _ := s.origin(id, map[int]bool{})
		if g.RootRow != r+2 || g.RootCol != c+1 {
			t.Fatal("wrong root bounds")
		}
		for _, other := range []int{2, 3, 4} {
			if other != id && strings.Contains(plain(s.Render(other, 18, 8)), "item00") {
				t.Fatal("menu leaked to sibling")
			}
		}
		s.Apply(event("popupmenu_select", 29))
		g, _ = s.PopupGeometry(id, 18, 8)
		if g.First != 24 || !strings.Contains(plain(s.Render(id, 18, 8)), "item29") || !strings.Contains(s.Render(id, 18, 8), ";7") {
			t.Fatal("selection did not scroll/highlight")
		}
		if index, hit := s.PopupTarget(id, 18, 8, g.Row+5, 1); !hit || index != 29 {
			t.Fatal("wrong selection hit")
		}
		resized, ok := s.PopupGeometry(id, 10, 4)
		if !ok || resized.Height != 2 || resized.Width != 10 || resized.First != 28 {
			t.Fatalf("resize: %+v", resized)
		}
		s.Apply(event("popupmenu_hide"))
		if _, ok := s.PopupGeometry(id, 18, 8); ok {
			t.Fatal("hide retained menu")
		}
	}
	// Root anchors route to the tile containing the anchor; a composed float
	// routes to its owning pane instead of duplicating in adjacent panes.
	s.Apply(event("popupmenu_show", popupItems(2), 0, 2, 42, 1))
	if _, ok := s.PopupGeometry(3, 40, 20); !ok {
		t.Fatal("root anchor not routed")
	}
	s.SetExternalGrids(map[int]bool{2: true, 3: true})
	s.Apply(event("popupmenu_show", popupItems(2), 0, 1, 1, 4))
	if _, ok := s.PopupGeometry(3, 40, 20); !ok {
		t.Fatal("composed float not routed")
	}
	for _, ev := range []string{"win_hide", "win_close", "grid_destroy"} {
		s = popupFixture()
		s.Apply(event("popupmenu_show", popupItems(2), 0, 1, 1, 4), event(ev, 4), event("grid_resize", 4, 18, 8), event("win_float_pos", 4, 104, "NW", 3, 2, 3, true))
		if _, ok := s.PopupGeometry(4, 18, 8); ok {
			t.Fatalf("%s retained stale menu on grid reuse", ev)
		}
	}
}
func TestPopupSafeLabelsAndGating(t *testing.T) {
	s := popupFixture()
	s.Apply(event("popupmenu_show", []any{[]any{"word]52;evila界", "K", "menu", "info"}}, 0, 1, 1, 2))
	frame := s.Render(2, 80, 20)
	if strings.Contains(frame, "]52;evil") || !strings.Contains(plain(frame), `\u754c`) {
		t.Fatalf("unsafe label: %q", frame)
	}
	s.SetExternalPopupmenu(false)
	if _, ok := s.PopupGeometry(2, 80, 20); ok {
		t.Fatal("disabled widget rendered")
	}
	if UIOptions(true, true)["ext_popupmenu"] != true || UIOptions(true, false)["ext_popupmenu"] != nil {
		t.Fatal("extension gating")
	}
}
