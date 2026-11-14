package screen

import "testing"

func TestPopupExternalDescendantAndAncestorLoss(t *testing.T) {
	s := popupFixture()
	s.Apply(event("grid_resize", 5, 25, 10), event("win_external_pos", 5, 105),
		event("grid_resize", 6, 15, 5), event("win_float_pos", 6, 106, "NW", 5, 2, 2, true),
		event("popupmenu_show", popupItems(20), 19, 1, 1, 6))
	s.SetExternalGrids(map[int]bool{2: true, 3: true, 4: true, 5: true})
	g, ok := s.PopupGeometry(5, 25, 10)
	if !ok || g.First+g.Height != 20 || g.RootCol != 3 {
		t.Fatalf("external descendant geometry: %+v %v", g, ok)
	}
	for _, id := range []int{2, 3, 4} {
		if _, ok := s.PopupGeometry(id, 40, 20); ok {
			t.Fatal("external menu leaked")
		}
	}
	s.Apply(event("win_hide", 5), event("win_external_pos", 5, 105))
	if _, ok := s.PopupGeometry(5, 25, 10); ok {
		t.Fatal("ancestor hide retained stale completion")
	}
}
