package screen

import (
	"fmt"
	"strings"
	"testing"
)

func messageTestState() *State {
	s := New()
	s.SetExternalCmdline(true)
	s.SetExternalMessages(true)
	s.Apply([]any{"grid_resize", []any{1, 40, 10}, []any{2, 20, 8}, []any{3, 20, 8}},
		[]any{"win_pos", []any{2, 100, 0, 0, 20, 8}, []any{3, 101, 0, 20, 20, 8}},
		[]any{"grid_cursor_goto", []any{2, 0, 0}})
	return s
}
func msgChunks(text string) []any { return []any{[]any{0, text}} }
func showMessage(s *State, text string, replace, history, appendText bool, id any) {
	s.Apply([]any{"msg_show", []any{"echo", msgChunks(text), replace, history, appendText, id, "typed_cmd"}})
}
func TestExternalMessageSemantics(t *testing.T) {
	s := messageTestState()
	showMessage(s, "FIRST", false, true, false, nil)
	showMessage(s, "SECOND", false, false, false, nil)
	showMessage(s, "REPLACED", true, false, false, nil)
	showMessage(s, "_APPEND", false, false, true, nil)
	text := plain(s.Render(2, 40, 10))
	if !strings.Contains(text, "FIRST") || !strings.Contains(text, "REPLACED_APPEND") || strings.Contains(text, "SECOND") {
		t.Fatal(text)
	}
	showMessage(s, "PROGRESS_A", false, false, false, 42)
	showMessage(s, "OTHER", false, false, false, "other")
	showMessage(s, "PROGRESS_B", false, false, false, uint64(42))
	text = plain(s.Render(2, 40, 10))
	if strings.Contains(text, "PROGRESS_A") || strings.Count(text, "PROGRESS_B") != 1 || !strings.Contains(text, "OTHER") {
		t.Fatal(text)
	}
	s.Apply([]any{"msg_showmode", []any{msgChunks("-- INSERT --")}}, []any{"msg_showcmd", []any{msgChunks("2d")}}, []any{"msg_ruler", []any{msgChunks("1,1")}})
	s.Apply([]any{"msg_clear", []any{}})
	text = plain(s.Render(2, 40, 10))
	if strings.Contains(text, "FIRST") || !strings.Contains(text, "-- INSERT --  2d  1,1") {
		t.Fatal(text)
	}
	s.Apply([]any{"msg_showmode", []any{[]any{}}}, []any{"msg_showcmd", []any{[]any{}}}, []any{"msg_ruler", []any{[]any{}}})
	if strings.Contains(plain(s.Render(2, 40, 10)), "INSERT") {
		t.Fatal("status not cleared")
	}
	showMessage(s, "ONLY_ONCE", false, true, false, nil)
	if strings.Contains(plain(s.Render(3, 40, 10)), "ONLY_ONCE") {
		t.Fatal("sibling duplicate")
	}
	s.Apply([]any{"grid_cursor_goto", []any{3, 0, 0}})
	if !strings.Contains(plain(s.Render(3, 20, 5)), "ONLY_ONCE") || strings.Contains(plain(s.Render(2, 20, 5)), "ONLY_ONCE") {
		t.Fatal("message did not follow active pane")
	}
	s.Apply([]any{"flush"})
	s.Apply([]any{"msg_show", []any{"empty", []any{}, false, false}}, []any{"flush"})
	if len(s.messages.visible) != 0 {
		t.Fatal("empty echo did not clear")
	}
}
func TestMessageHistoryBoundsPagingAndDismissal(t *testing.T) {
	s := messageTestState()
	var history []any
	for i := 0; i < 200; i++ {
		history = append(history, []any{"echomsg", msgChunks(fmt.Sprintf("HISTORY_%03d", i)), false})
	}
	s.Apply([]any{"msg_history_show", []any{history, false}})
	if len(s.messages.history) != messageLimit {
		t.Fatal(len(s.messages.history))
	}
	text := plain(s.Render(2, 40, 6))
	if !strings.Contains(text, "HISTORY_199") || !strings.Contains(text, "History:") {
		t.Fatal(text)
	}
	if strings.Contains(plain(s.Render(3, 40, 6)), "HISTORY_") {
		t.Fatal("history duplicated")
	}
	if !s.MessageInput("<PageUp>") {
		t.Fatal("page up not consumed")
	}
	text = plain(s.Render(2, 40, 6))
	if strings.Contains(text, "HISTORY_199") || !strings.Contains(text, "HISTORY_194") {
		t.Fatal(text)
	}
	s.Apply([]any{"msg_history_show", []any{[]any{[]any{"echo", msgChunks("ONE"), false}, []any{"echo", msgChunks("_TWO"), true}}, true}})
	if len(s.messages.history) != 1 || !strings.Contains(plain(s.Render(2, 40, 6)), "ONE_TWO") {
		t.Fatal("snapshot append/replace")
	}
	if !s.MessageInput("<CR>") || s.messages.historyOpen {
		t.Fatal("history dismiss")
	}
	for i := 0; i < 200; i++ {
		showMessage(s, strings.Repeat("界", 5000), false, true, false, nil)
	}
	if len(s.messages.visible) != messageLimit {
		t.Fatal("unbounded messages")
	}
	for _, e := range s.messages.visible {
		n := 0
		for _, c := range e.content {
			n += len(c.text)
		}
		if n > messageBytes {
			t.Fatal("unbounded message bytes")
		}
	}
	s.Apply([]any{"msg_clear", []any{}})
	showMessage(s, "SAFE界é🙂\x1b]2;INJECT\a", false, false, false, nil)
	frame := s.Render(2, 20, 4)
	if strings.Contains(frame, "\x1b]2;INJECT") || strings.Contains(frame, "\a") || !strings.Contains(plain(frame), "SAFE界é🙂") {
		t.Fatal("unsafe or broken Unicode message", frame)
	}
}
func TestMessagesPrerequisitesAndNativePrompts(t *testing.T) {
	s := New()
	s.SetExternalMessages(true)
	if s.externalMessages {
		t.Fatal("messages enabled without cmdline renderer")
	}
	if UIOptions(false, false, false, true)["ext_messages"] != nil {
		t.Fatal("messages implicitly enabled absent cmdline")
	}
	s = messageTestState()
	for _, mode := range []string{"return_prompt", "confirm", "confirm_sub", "number_prompt", "more"} {
		s.Apply([]any{"mode_change", []any{mode, 0}})
		if !s.CommandInputActive() || !s.GridCommandActive() || s.MessageInput("<CR>") {
			t.Fatal("native prompt intercepted", mode)
		}
	}
	s.Apply([]any{"mode_change", []any{"normal", 0}})
	if s.CommandInputActive() {
		t.Fatal("stale prompt")
	}
}
