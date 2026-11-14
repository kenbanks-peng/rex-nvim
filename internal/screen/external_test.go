package screen

import (
	"strings"
	"testing"
)

func TestExternalWindowPlacementAndComposition(t *testing.T) {
	s := New()
	s.Apply(
		[]any{"grid_resize", []any{1, 40, 12}, []any{2, 20, 8}, []any{3, 10, 4}, []any{4, 5, 1}, []any{5, 5, 1}},
		[]any{"win_pos", []any{2, 1000, 0, 0, 20, 8}},
		[]any{"win_external_pos", []any{3, 1001}},
		[]any{"win_float_pos", []any{4, 1002, "NW", 3, 1, 1, false, 100}, []any{5, 1003, "NW", 1, 0, 0, false, 50}},
		[]any{"grid_line", []any{3, 0, 0, []any{[]any{"EXTERNAL"}}}, []any{4, 0, 0, []any{[]any{"CHILD"}}}, []any{5, 0, 0, []any{[]any{"ROOT!"}}}},
	)
	if v := s.Views()[1]; !v.External || v.Float || v.ExternalRoot != 3 {
		t.Fatalf("placement: %+v", v)
	}
	// Even without renderer assignment, an external grid and its descendants
	// cannot leak into the root/tiled canvas.
	tile := s.Render(2, 20, 8)
	if strings.Contains(tile, "EXTERNAL") || strings.Contains(tile, "CHILD") || !strings.Contains(tile, "ROOT!") {
		t.Fatalf("tile composition: %q", tile)
	}
	ext := s.Render(3, 10, 4)
	if !strings.Contains(ext, "EXTERNAL") || !strings.Contains(ext, "CHILD") || strings.Contains(ext, "ROOT!") {
		t.Fatalf("external composition: %q", ext)
	}
	if s.tiledCount() != 1 {
		t.Fatal("external counted as tile")
	}
	s.Apply([]any{"win_hide", []any{3}})
	if _, _, ok := s.origin(4, map[int]bool{}); ok {
		t.Fatal("hidden parent left child visible")
	}
	s.Apply([]any{"win_external_pos", []any{3, 1001}})
	if !strings.Contains(s.Render(3, 10, 4), "CHILD") {
		t.Fatal("show lost descendants")
	}
	s.Apply([]any{"win_close", []any{3}}, []any{"grid_destroy", []any{3}}, []any{"grid_resize", []any{3, 10, 4}}, []any{"win_pos", []any{3, 1004, 0, 20, 10, 4}})
	for _, v := range s.Views() {
		if v.Grid == 3 && (v.External || v.ExternalRoot != 0 || v.Hidden) {
			t.Fatalf("reused grid inherited external placement: %+v", v)
		}
	}
}
