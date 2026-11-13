package screen

import (
	"regexp"
	"strings"
	"testing"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func plain(s string) string                { return ansi.ReplaceAllString(s, "") }
func event(name string, args ...any) []any { return []any{name, args} }
func resize(s *State, id, w, h int)        { s.Apply(event("grid_resize", id, w, h)) }
func line(s *State, id, row int, text string) {
	cells := make([]any, 0, len(text))
	for _, r := range text {
		cells = append(cells, []any{string(r)})
	}
	s.Apply(event("grid_line", id, row, 0, cells))
}
func position(s *State, id, row, col, w, h int) {
	s.Apply(event("win_pos", id, id+100, row, col, w, h))
}

func TestApplyCallbacksAndLifecycle(t *testing.T) {
	s := New()
	if s.Apply(event("grid_resize", uint64(2), int64(4), uint8(2))) {
		t.Fatal("resize flushed")
	}
	s.Apply(event("win_pos", uint8(2), uint64(45), int32(1), int16(3), 4, 2))
	if !s.Apply([]any{[]any{"flush", []any{}}}) {
		t.Fatal("nested flush ignored")
	}
	views := s.Views()
	if len(views) != 1 || views[0] != (View{Grid: 2, Window: 45, Row: 1, Col: 3, Width: 4, Height: 2, Focusable: true}) {
		t.Fatalf("%+v", views)
	}
	views[0].Hidden = true
	if s.Views()[0].Hidden {
		t.Fatal("Views exposed mutable state")
	}
	line(s, 2, 0, "abcd")
	resize(s, 2, 5, 3)
	if w, h := s.GridSize(2); w != 5 || h != 3 {
		t.Fatalf("size %d %d", w, h)
	}
	if got := plain(s.Render(2, 5, 1)); got != "abcd " {
		t.Fatalf("%q", got)
	}
	s.Apply(event("win_hide", 2))
	if !s.Views()[0].Hidden {
		t.Fatal("not hidden")
	}
	position(s, 2, 0, 0, 5, 3)
	if s.Views()[0].Hidden {
		t.Fatal("not unhidden")
	}
	s.Apply(event("grid_clear", 2))
	if got := plain(s.Render(2, 5, 1)); got != "     " {
		t.Fatalf("%q", got)
	}
	s.Apply(event("win_close", 2))
	if len(s.Views()) != 0 {
		t.Fatal("closed view remains")
	}
	if w, _ := s.GridSize(2); w != 5 {
		t.Fatal("close destroyed grid")
	}
	s.Apply(event("grid_destroy", 2))
	if w, h := s.GridSize(2); w != 0 || h != 0 {
		t.Fatal("destroy failed")
	}
}

func TestLineRepeatHighlightsAndWideClipping(t *testing.T) {
	s := New()
	resize(s, 2, 7, 2)
	s.Apply(event("grid_line", int64(2), uint64(0), int32(0), []any{
		[]any{"a", int64(4), uint64(2)}, []any{"b"}, []any{"界"}, []any{""}, []any{"x", uint8(0), uint8(2)},
	}))
	if got := plain(s.Render(2, 7, 1)); got != "aab界xx" {
		t.Fatalf("%q", got)
	}
	if got := plain(s.Render(2, 4, 1)); got != "aab " {
		t.Fatalf("half wide character: %q", got)
	}
	if s.grids[2].cells[2].hl != 4 {
		t.Fatal("highlight not inherited")
	}
	s.Apply(event("grid_line", 2, 1, -1, []any{[]any{"a", 0, 3}}))
	if got := plain(s.Render(2, 4, 2)); got != "aab aa  " {
		t.Fatalf("%q", got)
	}
}

func TestScrollRectangles(t *testing.T) {
	s := New()
	resize(s, 2, 5, 4)
	for i, text := range []string{"abcde", "fghij", "klmno", "pqrst"} {
		line(s, 2, i, text)
	}
	s.Apply(event("grid_scroll", 2, 0, 3, 1, 4, 1, 0))
	if got := plain(s.Render(2, 5, 4)); got != "aghie"+"flmnj"+"k   o"+"pqrst" {
		t.Fatalf("vertical %q", got)
	}
	s.Apply(event("grid_scroll", 2, 0, 4, 0, 5, 0, -2))
	if got := plain(s.Render(2, 5, 4)); got != "  agh"+"  flm"+"  k  "+"  pqr" {
		t.Fatalf("horizontal %q", got)
	}
	s.Apply(event("grid_scroll", 2, 1, 3, 1, 4, 99, 99))
	if got := plain(s.Render(2, 5, 4)); got != "  agh"+"    m"+"     "+"  pqr" {
		t.Fatalf("overscroll %q", got)
	}
}

func TestHighlightStylesAndDefaults(t *testing.T) {
	s := New()
	resize(s, 2, 2, 1)
	s.Apply(event("default_colors_set", int64(0x112233), uint64(0x445566), -1))
	s.Apply(event("hl_attr_define", uint64(7), map[any]any{
		"foreground": uint64(0xaabbcc), "special": int64(0x010203),
		"bold": true, "italic": true, "underline": true, "reverse": true, "strikethrough": true,
	}, map[string]any{}, []any{}))
	s.Apply(event("grid_line", 2, 0, 0, []any{[]any{"a", 7}, []any{"b", 0}}))
	frame := s.Render(2, 2, 1)
	for _, part := range []string{"0;1;3;4;7;9;38;2;170;187;204;48;2;68;85;102;58;2;1;2;3m", "0;38;2;17;34;51;48;2;68;85;102mb"} {
		if !strings.Contains(frame, part) {
			t.Fatalf("missing %q: %q", part, frame)
		}
	}
}

func TestFloatsAnchorsLayersAndParents(t *testing.T) {
	s := New()
	resize(s, 1, 12, 6)
	for _, id := range []int{2, 3} {
		resize(s, id, 6, 4)
	}
	position(s, 2, 1, 0, 6, 4)
	position(s, 3, 1, 6, 6, 4)
	for r := 0; r < 4; r++ {
		line(s, 2, r, "......")
		line(s, 3, r, "!!!!!!")
	}
	resize(s, 4, 3, 2)
	line(s, 4, 0, "abc")
	line(s, 4, 1, "def")
	s.Apply(event("win_float_pos", 4, 104, "SE", 2, 3.0, 5.0, true, 60))
	if got := plain(s.Render(2, 6, 4)); got != "......"+"..abc."+"..def."+"......" {
		t.Fatalf("anchor: %q", got)
	}
	if got := plain(s.Render(3, 6, 4)); got != strings.Repeat("!", 24) {
		t.Fatalf("leaked float: %q", got)
	}
	resize(s, 5, 2, 1)
	line(s, 5, 0, "XY")
	s.Apply(event("win_float_pos", 5, 105, "NW", 4, 0.0, 1.0, false, 70))
	if got := plain(s.Render(2, 6, 4)); got != "......"+"..aXY."+"..def."+"......" {
		t.Fatalf("nested: %q", got)
	}
	s.Apply(event("win_float_pos", 5, 105, "NW", 4, 0.0, 1.0, false, 50))
	if got := plain(s.Render(2, 6, 4)); got != "......"+"..abc."+"..def."+"......" {
		t.Fatalf("z order: %q", got)
	}
	s.Apply(event("win_hide", 4))
	if got := plain(s.Render(2, 6, 4)); got != strings.Repeat(".", 24) {
		t.Fatalf("hidden parent: %q", got)
	}
	// Root anchored float over the right-hand pane.
	s.Apply(event("win_float_pos", 5, 105, "NW", 1, 2.0, 7.0, false, 80))
	if got := plain(s.Render(3, 6, 4)); got != "!!!!!!"+"!XY!!!"+"!!!!!!"+"!!!!!!" {
		t.Fatalf("root anchor: %q", got)
	}
}

func TestRootMessagesOnlyOnCurrentPane(t *testing.T) {
	s := New()
	resize(s, 1, 12, 6)
	resize(s, 2, 6, 5)
	resize(s, 3, 6, 5)
	position(s, 2, 0, 0, 6, 5)
	position(s, 3, 0, 6, 6, 5)
	for r := 0; r < 5; r++ {
		line(s, 1, r, "│separators│")
		line(s, 2, r, "aaaaaa")
		line(s, 3, r, "bbbbbb")
	}
	line(s, 1, 5, ":write")
	s.Apply(event("grid_cursor_goto", 2, 1, 2))
	if got := plain(s.Render(2, 6, 5)); got != strings.Repeat("a", 24)+":write" {
		t.Fatalf("%q", got)
	}
	if got := plain(s.Render(3, 6, 5)); got != strings.Repeat("b", 30) {
		t.Fatalf("message leaked: %q", got)
	}
	s.Apply(event("grid_cursor_goto", 1, 5, 4))
	if s.CursorGrid() != 1 {
		t.Fatal("wrong cursor grid")
	}
	frame := s.Render(2, 6, 5)
	if !strings.HasSuffix(frame, "[5;5H[?25h") {
		t.Fatalf("root cursor: %q", frame)
	}
	line(s, 1, 5, "      ")
	if got := plain(s.Render(2, 6, 5)); got != strings.Repeat("a", 30) {
		t.Fatalf("blank overlay: %q", got)
	}
}

func TestCursorAndFrameClipping(t *testing.T) {
	s := New()
	resize(s, 2, 4, 3)
	line(s, 2, 0, "abcd")
	line(s, 2, 1, "efgh")
	s.Apply(event("grid_cursor_goto", 2, 1, 1))
	if frame := s.Render(2, 2, 2); plain(frame) != "abef" || !strings.HasSuffix(frame, "[2;2H[?25h") {
		t.Fatalf("%q", frame)
	}
	s.Apply(event("grid_cursor_goto", 3, 0, 0))
	if frame := s.Render(2, 2, 2); strings.Contains(frame, "[?25h") {
		t.Fatal("foreign cursor shown")
	}
	if got := s.Render(2, 0, 2); got != "" {
		t.Fatalf("%q", got)
	}
	if got := plain(s.Render(99, 2, 1)); got != "  " {
		t.Fatalf("unknown grid %q", got)
	}
}

func TestMalformedAndUnknownEvents(t *testing.T) {
	s := New()
	s.Apply(nil, []any{}, []any{false}, event("unknown", 1), event("grid_line"), event("grid_scroll"), event("win_float_pos"))
	if !s.Apply([]any{"flush"}) {
		t.Fatal("flush missing")
	}
	resize(s, 2, 2, 1)
	s.Apply(event("grid_line", 2, 0, 0, []any{nil, []any{}, []any{9}, []any{"x", 0, -1}, []any{"", 0, 1}}))
	if got := plain(s.Render(2, 2, 1)); got != "  " {
		t.Fatalf("terminal control text: %q", got)
	}
}

func TestCommandHeightAndFloatClipping(t *testing.T) {
	s := New()
	s.SetCommandHeight(2)
	resize(s, 1, 8, 5)
	resize(s, 2, 8, 3)
	position(s, 2, 0, 0, 8, 3)
	line(s, 2, 0, "abcdefgh")
	line(s, 2, 1, "abcdefgh")
	line(s, 2, 2, "abcdefgh")
	line(s, 1, 3, "message1")
	line(s, 1, 4, "message2")
	s.Apply(event("grid_cursor_goto", 2, 0, 0))
	if got := plain(s.Render(2, 8, 3)); got != "abcdefghmessage1message2" {
		t.Fatalf("cmdheight: %q", got)
	}
	s.Apply(event("grid_clear", 1))
	resize(s, 4, 3, 1)
	s.Apply(event("grid_line", 4, 0, 0, []any{[]any{"界"}, []any{""}, []any{"x"}}))
	s.Apply(event("win_float_pos", 4, 104, "NW", 2, 0.0, -1.0, false, 50))
	if got := plain(s.Render(2, 4, 1)); got != " xcd" {
		t.Fatalf("left clip: %q", got)
	}
	s.Apply(event("win_float_pos", 4, 104, "NW", 2, 0.0, 3.0, false, 50))
	if got := plain(s.Render(2, 4, 1)); got != "abc " {
		t.Fatalf("right clip: %q", got)
	}
}

func TestDecorationsResizedOwnerAndGlobalStatus(t *testing.T) {
	s := New()
	resize(s, 1, 10, 7)
	resize(s, 2, 5, 4)
	resize(s, 3, 4, 4)
	position(s, 2, 0, 0, 5, 1)
	position(s, 3, 0, 6, 4, 1) // stale win_pos heights
	for r := 0; r < 4; r++ {
		line(s, 2, r, "aaaaa")
		line(s, 3, r, "bbbb")
	}
	line(s, 2, 0, "BARaa") // winbar already part of grid
	line(s, 1, 4, "LEFT  RGHT")
	line(s, 1, 5, "notcmdline") // must never infer this as extra cmdheight
	line(s, 1, 6, ":cmd")
	s.Apply(event("grid_cursor_goto", 2, 1, 0))
	if got := plain(s.Render(2, 5, 5)); got != "BARaa"+strings.Repeat("a", 15)+":cmd " {
		t.Fatalf("resized command owner: %q", got)
	}
	line(s, 1, 6, "          ")
	if got := plain(s.Render(2, 5, 5)); got != "BARaa"+strings.Repeat("a", 15)+"LEFT " {
		t.Fatalf("left decoration: %q", got)
	}
	if got := plain(s.Render(3, 4, 5)); got != strings.Repeat("b", 16)+"RGHT" {
		t.Fatalf("right decoration: %q", got)
	}
	s.SetLastStatus(0)
	if got := plain(s.Render(3, 4, 5)); got != strings.Repeat("b", 16)+"    " {
		t.Fatalf("disabled status: %q", got)
	}
	s.SetLastStatus(3)
	line(s, 1, 5, "GLOBAL    ")
	if got := plain(s.Render(2, 5, 5)); got != "BARaa"+strings.Repeat("a", 15)+"GLOBA" {
		t.Fatalf("global: %q", got)
	}
	if got := plain(s.Render(3, 4, 5)); got != strings.Repeat("b", 16)+"    " {
		t.Fatalf("global duplicated: %q", got)
	}
	s.SetCommandHeight(0)
	if got := plain(s.Render(2, 5, 5)); strings.Contains(got, ":cmd") {
		t.Fatal("cmdheight=0 copied command")
	}
}

func TestExternalFloatOwnership(t *testing.T) {
	s := New()
	resize(s, 2, 6, 3)
	position(s, 2, 0, 0, 6, 3)
	for r := 0; r < 3; r++ {
		line(s, 2, r, "......")
	}
	resize(s, 4, 3, 2)
	line(s, 4, 0, "abc")
	line(s, 4, 1, "def")
	s.Apply(event("win_float_pos", 4, 104, "NW", 2, 0, 1, true, 50))
	resize(s, 5, 1, 1)
	line(s, 5, 0, "X")
	s.Apply(event("win_float_pos", 5, 105, "NW", 4, 1, 1, false, 100))
	if got := plain(s.Render(2, 6, 3)); got != ".abc...dXf........" {
		t.Fatalf("unassigned floats: %q", got)
	}
	assigned := map[int]bool{4: true}
	s.SetExternalGrids(assigned)
	delete(assigned, 4)
	if got := plain(s.Render(2, 6, 3)); got != strings.Repeat(".", 18) {
		t.Fatalf("duplicate float: %q", got)
	}
	if got := plain(s.Render(4, 3, 2)); got != "abcdXf" {
		t.Fatalf("external child lost: %q", got)
	}
	s.SetExternalGrids(map[int]bool{4: true, 5: true})
	if got := plain(s.Render(4, 3, 2)); got != "abcdef" {
		t.Fatalf("external nested duplicate: %q", got)
	}
	if got := plain(s.Render(5, 1, 1)); got != "X" {
		t.Fatalf("nested external: %q", got)
	}
	s.SetExternalGrids(nil)
	if got := plain(s.Render(2, 6, 3)); got != ".abc...dXf........" {
		t.Fatalf("unassigned again: %q", got)
	}
}

func TestMessageGrid(t *testing.T) {
	s := New()
	resize(s, 1, 8, 6)
	resize(s, 2, 8, 4)
	position(s, 2, 0, 0, 8, 4)
	line(s, 2, 0, "contents")
	s.Apply(event("grid_cursor_goto", 2, 0, 0))
	resize(s, 3, 8, 6)
	line(s, 3, 0, "-- MODE")
	s.Apply(event("msg_set_pos", 3, 5, false, " ", 200))
	if got := plain(s.Render(2, 8, 5)); got != "contents"+strings.Repeat(" ", 24)+"-- MODE " {
		t.Fatalf("message grid: %q", got)
	}
	s.Apply(event("grid_cursor_goto", 3, 0, 2))
	if got := s.Render(2, 8, 5); !strings.HasSuffix(got, "\x1b[5;3H\x1b[?25h") {
		t.Fatalf("message cursor: %q", got)
	}
	s.Apply(event("grid_clear", 3))
	if got := plain(s.Render(2, 8, 5)); got != "contents"+strings.Repeat(" ", 32) {
		t.Fatalf("cleared message: %q", got)
	}
}
