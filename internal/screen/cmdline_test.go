package screen

import (
	"strings"
	"testing"
)

func commandSetup() *State {
	s := New()
	s.SetExternalCmdline(true)
	resize(s, 1, 30, 10)
	resize(s, 2, 30, 8)
	resize(s, 3, 30, 8)
	position(s, 2, 0, 0, 30, 8)
	position(s, 3, 0, 31, 30, 8)
	s.Apply(event("grid_cursor_goto", 2, 0, 0))
	return s
}
func showCommand(s *State, text string, pos, level int) {
	s.Apply(event("cmdline_show", []any{[]any{0, text}}, pos, ":", "", 0, level))
}
func TestCommandLevelsBlocksAndCancellation(t *testing.T) {
	s := commandSetup()
	showCommand(s, "parent", 6, 1)
	s.Apply(event("cmdline_special_char", "^", true, 1))
	if got := plain(s.Render(2, 30, 8)); !strings.Contains(got, ":parent^") {
		t.Fatal(got)
	}
	showCommand(s, "child", 2, 2)
	if got := plain(s.Render(2, 30, 8)); !strings.Contains(got, ":parent^") || !strings.Contains(got, ":child") {
		t.Fatal(got)
	}
	if got := plain(s.Render(3, 30, 8)); strings.Contains(got, "parent") {
		t.Fatal("prompt leaked to sibling")
	}
	s.Apply(event("cmdline_hide", 2, true))
	if got := plain(s.Render(2, 30, 8)); strings.Contains(got, "child") || !strings.Contains(got, "parent") {
		t.Fatal(got)
	}
	showCommand(s, "parent", 6, 1) // clears special char
	if strings.Contains(plain(s.Render(2, 30, 8)), "^") {
		t.Fatal("stale special")
	}
	s.Apply(event("cmdline_block_show", []any{[]any{[]any{0, "function Foo()"}}}),
		event("cmdline_block_append", []any{[]any{0, "echo 'body'"}}))
	if got := plain(s.Render(2, 30, 8)); !strings.Contains(got, "function Foo()") || !strings.Contains(got, "echo 'body'") {
		t.Fatal(got)
	}
	s.Apply(event("cmdline_hide", 1, true), event("cmdline_block_hide"))
	if s.CmdlineActive() || strings.Contains(plain(s.Render(2, 30, 8)), "body") {
		t.Fatal("cancel retained presentation")
	}
	showCommand(s, "new", 3, 1)
	if strings.Contains(plain(s.Render(2, 30, 8)), "parent") {
		t.Fatal("level reused stale text")
	}
	s.SetExternalCmdline(false)
	if strings.Contains(plain(s.Render(2, 30, 8)), "new") {
		t.Fatal("disabled renderer painted prompt")
	}
}
func TestCommandUnicodeHighlightWrapAndPosition(t *testing.T) {
	s := commandSetup()
	s.Apply(event("hl_attr_define", 7, map[string]any{"foreground": 0xff0000}),
		event("cmdline_show", []any{[]any{7, "界e"}, []any{0, "́🙂x"}}, len("界é🙂"), "", "问: ", 1, 1, 7))
	c := s.command.levels[1]
	p, cur := c.pixels(c.pos)
	// Prompt 4 cells, indent 1, content 2+1+2.
	if cur != 10 || p[5].text != "界" || !p[5].wide || p[7].text != "é" || p[7].hl != 7 {
		t.Fatalf("cursor=%d pixels=%+v", cur, p)
	}
	if !strings.Contains(s.Render(2, 30, 8), "38;2;255;0;0") {
		t.Fatal("lost highlight")
	}
	s.Apply(event("cmdline_pos", len("界"), 1))
	l := s.commandLayout(2, 6, 3, -1)
	if !l.cursor || l.col != 2 || l.row != 2 {
		t.Fatalf("%+v", l)
	}
	s.Apply(event("cmdline_special_char", "^", false, 1))
	p, _ = c.pixels(c.pos)
	if p[7].text != "^" {
		t.Fatalf("overwrite: %+v", p)
	}
	for _, width := range []int{1, 2, 5, 30} {
		for _, height := range []int{1, 2, 8} {
			l := s.commandLayout(2, width, height, -1)
			if l.row < 0 || l.row >= height || l.col < 0 || l.col >= width || len(l.rows) > height {
				t.Fatalf("clipped cursor %dx%d: %+v", width, height, l)
			}
			s.Render(2, width, height)
		}
	}
	showCommand(s, strings.Repeat("x", 100), 2, 1)
	l = s.commandLayout(2, 5, 2, -1)
	if l.row != 0 || l.col != 3 {
		t.Fatalf("cursor-follow viewport %+v", l)
	}
}
func TestCommandPopupAnchorAndGridFallback(t *testing.T) {
	s := commandSetup()
	s.SetExternalPopupmenu(true)
	showCommand(s, "界abc", len("界abc"), 1)
	s.Apply(event("popupmenu_show", []any{[]any{"abc", "", "", ""}}, 0, 0, len("界"), -1))
	g, ok := s.PopupGeometry(2, 30, 8)
	if !ok || g.Col != 3 || g.Row >= 7 {
		t.Fatalf("command popup %+v %v", g, ok)
	}
	if _, ok := s.PopupGeometry(3, 30, 8); ok {
		t.Fatal("popup leaked")
	}
	s.Apply(event("cmdline_hide", 1, false))
	if _, ok := s.PopupGeometry(2, 30, 8); ok {
		t.Fatal("stale command popup")
	}
	if UIOptions(false, false, true)["ext_cmdline"] != true || UIOptions(false, false, false)["ext_cmdline"] != nil {
		t.Fatal("attachment gating")
	}
	s.SetExternalCmdline(false)
	line(s, 1, 9, ":GRID_FALLBACK")
	if !strings.Contains(plain(s.Render(2, 30, 8)), ":GRID_FALLBACK") {
		t.Fatal("lost grid fallback")
	}
}

func TestCommandCompletionUsesVisibleCursorViewport(t *testing.T) {
	s := commandSetup()
	s.SetExternalPopupmenu(true)
	showCommand(s, strings.Repeat("x", 100), 100, 1)
	s.Apply(event("popupmenu_show", []any{[]any{"item", "", "", ""}}, 0, 0, 0, -1))
	if _, ok := s.PopupGeometry(2, 10, 3); ok {
		t.Fatal("offscreen anchor scrolled command viewport")
	}
	s.Apply(event("popupmenu_show", []any{[]any{"item", "", "", ""}}, 0, 0, 90, -1))
	g, ok := s.PopupGeometry(2, 10, 3)
	if !ok || g.Row != 0 || g.Col != 1 {
		t.Fatalf("visible anchor %+v %v", g, ok)
	}
}
func TestCommandOwnerComposesInExternalRenderer(t *testing.T) {
	s := commandSetup()
	resize(s, 5, 20, 5)
	s.Apply(event("win_external_pos", 5, 105))
	resize(s, 6, 10, 2)
	s.Apply(event("win_float_pos", 6, 106, "NW", 5, 0, 0, true, 50), event("grid_cursor_goto", 6, 0, 0))
	s.SetExternalGrids(map[int]bool{2: true, 3: true, 5: true})
	showCommand(s, "EXTERNAL_PROMPT", 15, 1)
	if !strings.Contains(plain(s.Render(5, 20, 5)), "EXTERNAL_PROMPT") || strings.Contains(plain(s.Render(2, 20, 5)), "EXTERNAL_PROMPT") {
		t.Fatal("command owner ignored composed external descendant")
	}
}
