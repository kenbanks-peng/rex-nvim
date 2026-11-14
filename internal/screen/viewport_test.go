package screen

import (
	"reflect"
	"strings"
	"testing"
)

// Metadata describes the already-updated grid, not another transform to apply.
func TestViewportMetadataGridFallbackLifecycle(t *testing.T) {
	s, baseline := New(), New()
	apply := func(updates ...[]any) {
		t.Helper()
		baseline.Apply(updates...)
		s.Apply(updates...)
		// Include stale/foreign window metadata and future appended fields.
		s.Apply(event("win_viewport", 2, 999, 100, 104, 101, 0, 500, 80, "future"),
			event("win_viewport_margins", 2, 999, 1, 1, 1, 1),
			event("win_extmark", 2, 999, 3, 7, 1, 1))
		if !reflect.DeepEqual(s.Views(), baseline.Views()) {
			t.Fatal("metadata changed placement")
		}
		for _, id := range []int{1, 2, 3} {
			if s.Render(id, 8, 5) != baseline.Render(id, 8, 5) {
				t.Fatalf("metadata changed grid %d", id)
			}
		}
	}
	apply(event("grid_resize", 1, 8, 6), event("grid_resize", 2, 8, 4),
		event("win_pos", 2, 10, 0, 0, 8, 4),
		event("grid_line", 2, 0, 0, []any{[]any{"WINBAR"}}),
		event("grid_line", 2, 1, 0, []any{[]any{"FIRST"}}),
		event("grid_line", 2, 2, 0, []any{[]any{"SECOND"}}),
		event("grid_resize", 3, 3, 1), event("win_float_pos", 3, 11, "NW", 2, 2, 1, false),
		event("grid_line", 3, 0, 0, []any{[]any{"POP"}}))
	apply(event("grid_scroll", 2, 1, 4, 0, 8, 1, 0),
		event("grid_line", 2, 3, 0, []any{[]any{"THIRD"}}))
	if frame := plain(s.Render(2, 8, 4)); !strings.HasPrefix(frame, "WINBAR") || !strings.Contains(frame, "SECOND") || !strings.Contains(frame, "POP") {
		t.Fatalf("scroll lost decorations/overlay: %q", frame)
	}
	apply(event("grid_resize", 2, 6, 3), event("win_pos", 2, 10, 0, 0, 6, 3))
	apply(event("win_hide", 3))
	if strings.Contains(plain(s.Render(2, 8, 4)), "POP") {
		t.Fatal("hidden overlay remains")
	}
	apply(event("win_float_pos", 3, 11, "NW", 2, 1, 0, false))
	if !strings.Contains(plain(s.Render(2, 8, 4)), "POP") {
		t.Fatal("overlay did not return")
	}
	apply(event("win_close", 3), event("grid_destroy", 3), event("win_close", 2), event("grid_destroy", 2))
	apply(event("grid_resize", 2, 8, 4), event("win_pos", 2, 20, 0, 0, 8, 4),
		event("grid_line", 2, 0, 0, []any{[]any{"REUSED"}}))
	if frame := plain(s.Render(2, 8, 4)); !strings.HasPrefix(frame, "REUSED") || strings.Contains(frame, "POP") || strings.Contains(frame, "WINBAR") {
		t.Fatalf("grid reuse leaked old content/overlay: %q", frame)
	}
}

func TestViewportMetadataPolicy(t *testing.T) {
	for _, name := range []string{"win_viewport", "win_viewport_margins", "win_extmark"} {
		c, ok := eventCapability(name)
		if !ok || c.Status != "unsupported" || handledEvents[name] {
			t.Fatalf("unconsumed metadata must remain explicit: %s %+v", name, c)
		}
	}
}
