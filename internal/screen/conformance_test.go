package screen

import (
	"reflect"
	"strings"
	"testing"
)

func TestProtocolHistoryClearAndNativePrompt(t *testing.T) {
	s := messageTestState()
	s.Apply(event("msg_history_show", []any{[]any{"echo", msgChunks("HISTORY_界")}}))
	s.Apply(event("msg_show", "return_prompt", msgChunks("Press ENTER"), false, false))
	text := plain(s.Render(2, 40, 8))
	if s.messages.historyOpen || !strings.Contains(text, "HISTORY_界") ||
		!strings.Contains(text, "Press ENTER") || strings.Contains(text, "History:") {
		t.Fatalf("native history prompt covered: %q", text)
	}
	s.Apply(event("msg_history_show", []any{[]any{"echo", msgChunks("NEW")}}))
	s.Apply(event("msg_showmode", msgChunks("STATUS")), event("msg_history_clear"))
	if s.messages.historyOpen || len(s.messages.history) != 0 || s.messages.offset != 0 {
		t.Fatal("history clear retained snapshot or paging state")
	}
	if !strings.Contains(plain(s.Render(2, 40, 8)), "STATUS") {
		t.Fatal("history clear erased independent status")
	}
}

func TestProtocolOptionalFieldsAndCallbackBoundaries(t *testing.T) {
	old, future := commandSetup(), commandSetup()
	for _, s := range []*State{old, future} {
		s.SetExternalMessages(true)
		s.SetExternalPopupmenu(true)
	}
	steps := []struct {
		base, extended []any
	}{
		{event("hl_attr_define", 7, map[string]any{"foreground": 0xff0000}, map[string]any{}, []any{}), event("hl_attr_define", 7, map[string]any{"foreground": 0xff0000}, map[string]any{}, []any{}, "future")},
		{event("grid_line", 2, 0, 0, []any{[]any{"界", 7}, []any{""}, []any{"é"}, []any{" ", 0, 4}}), event("grid_line", 2, 0, 0, []any{[]any{"界", 7}, []any{""}, []any{"é"}, []any{" ", 0, 4}}, false, "future")},
		{event("cmdline_show", msgChunks("echo '界é🙂'"), len("echo '界"), ":", "", 0, 1), event("cmdline_show", msgChunks("echo '界é🙂'"), len("echo '界"), ":", "", 0, 1, 0, "future")},
		{event("popupmenu_show", []any{[]any{"alpha", "", "", ""}, []any{"beta", "", "", ""}}, 0, 0, 0, -1), event("popupmenu_show", []any{[]any{"alpha", "", "", "", "future"}, []any{"beta", "", "", ""}}, 0, 0, 0, -1, "future")},
		{event("popupmenu_hide"), event("popupmenu_hide", "future")},
		{event("cmdline_hide", 1), event("cmdline_hide", 1, false, "future")},
		{event("msg_show", "echo", msgChunks("MESSAGE_界é🙂"), false, true), event("msg_show", "echo", msgChunks("MESSAGE_界é🙂"), false, true, false, nil, nil, "future")},
	}
	var batch []any
	for i, step := range steps {
		if old.Apply(step.base) {
			t.Fatalf("step %d flushed without flush", i)
		}
		batch = append(batch, step.extended)
		// Alternate callbacks split between events and nested callback batches.
		if i%2 == 1 || i == len(steps)-1 {
			if future.Apply(batch) {
				t.Fatalf("step %d nested batch invented flush", i)
			}
			batch = nil
			if old.Render(2, 30, 8) != future.Render(2, 30, 8) ||
				!reflect.DeepEqual(old.Views(), future.Views()) {
				t.Fatalf("step %d: appended fields or callback boundaries changed presentation", i)
			}
		}
	}
	for _, s := range []*State{old, future} {
		if !s.Apply([]any{"flush"}) {
			t.Fatal("bare flush lost")
		}
		if !strings.Contains(plain(s.Render(2, 30, 8)), "MESSAGE_界é🙂") {
			t.Fatal("message lost at flush")
		}
		// Empty-message clearing is a flush-level decision, not a callback-level
		// decision: a real message later in the same frame must win.
		s.Apply(event("msg_show", "empty", []any{}, false, false))
		s.Apply(event("msg_show", "echo", msgChunks("NEXT"), true, false))
		s.Apply([]any{"flush"})
		if !strings.Contains(plain(s.Render(2, 30, 8)), "NEXT") {
			t.Fatal("empty event cleared a nonempty frame")
		}
		if !s.Apply(event("msg_show", "empty", []any{}, false, false), []any{"flush"},
			event("msg_show", "echo", msgChunks("NEXT"), false, false), []any{"flush"}) ||
			!strings.Contains(plain(s.Render(2, 30, 8)), "NEXT") {
			t.Fatal("coalesced frames lost flush ordering")
		}
		s.Apply(event("msg_show", "empty", []any{}, false, false))
		if !strings.Contains(plain(s.Render(2, 30, 8)), "NEXT") {
			t.Fatal("empty message cleared before flush")
		}
		s.Apply([]any{"flush"}, event("grid_destroy", 2), event("grid_resize", 2, 12, 4), event("win_pos", 2, 999, 0, 0, 12, 4))
		if len(s.messages.visible) != 0 || s.popup.visible || s.CmdlineActive() {
			t.Fatal("flush/destroy retained a stale widget")
		}
	}
}
