package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
)

func TestWindowCanvasSizeIncludesUnownedPanes(t *testing.T) {
	m := &manager{panes: map[int]*peer{2: {block: "owned", width: 60, height: 20}, 3: {block: "float", layer: "overlay", width: 1000, height: 1000}}}
	view := rex.View{Windows: []rex.Window{{Layers: []rex.Layer{{Kind: "tiled", Layout: &rex.Node{Split: &rex.Split{Before: &rex.Node{BlockID: "owned", Rect: [4]float64{0, 0, .5, .5}}, After: &rex.Node{BlockID: "other", Rect: [4]float64{.5, 0, .5, .5}}}}}}}}}
	w, h := m.windowSize(view, 60, 20)
	if w != 120 || h != 40 {
		t.Fatalf("canvas = %dx%d", w, h)
	}
}

func TestNativeWindowFittingKeepsGlobalChrome(t *testing.T) {
	m, _ := floatManager(t)
	m.mu.Lock()
	m.state.SetWindowUI(true)
	m.mu.Unlock()
	if err := m.nv.ExecLua(configure+"\nvim.o.showtabline=2;vim.o.statusline='FULL_WINDOW_STATUS';vim.cmd('vsplit');vim.cmd('split');vim.cmd('redraw!')", nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	for _, v := range m.views() {
		if !v.Float && !v.Hidden {
			m.panes[v.Grid] = &peer{grid: v.Grid, block: "initial", width: 40, height: 12}
		}
	}
	for i := 0; i < 3; i++ {
		if err := m.fit(); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.nv.Command("redraw!"); err != nil {
		t.Fatal(err)
	}
	var status int
	if err := m.nv.ExecLua("return vim.o.laststatus", &status); err != nil {
		t.Fatal(err)
	}
	if status != 3 {
		t.Fatalf("laststatus=%d", status)
	}
	time.Sleep(50 * time.Millisecond)
	m.mu.Lock()
	defer m.mu.Unlock()
	found := 0
	for _, v := range m.state.Views() {
		frame := m.state.Render(v.Grid, v.Width, v.Height)
		if strings.Contains(frame, "FULL_WINDOW_STATUS") {
			found++
			if !v.Float || v.Width != m.rootW {
				t.Fatalf("status not full-width float: %+v", v)
			}
		}
	}
	if found != 1 {
		t.Fatalf("status rendered %d times", found)
	}
}

func TestUnchangedRenderersDoNotUndoNativeResize(t *testing.T) {
	m, _ := floatManager(t)
	m.mu.Lock()
	m.state.SetWindowUI(true)
	m.mu.Unlock()
	if err := m.nv.ExecLua(configure+"\nvim.cmd('vsplit');vim.cmd('redraw!')", nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		count := 0
		for _, v := range m.views() {
			if !v.Float && !v.Hidden {
				count++
			}
		}
		if count == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	for _, v := range m.views() {
		if !v.Float && !v.Hidden {
			m.panes[v.Grid] = &peer{grid: v.Grid, block: "initial", width: 40, height: 24}
		}
	}
	if len(m.panes) != 2 {
		t.Fatalf("editing panes = %d", len(m.panes))
	}
	if err := m.fit(); err != nil {
		t.Fatal(err)
	}
	if err := m.nv.Command("vertical resize 25"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := m.fit(); err != nil {
			t.Fatal(err)
		}
	}
	var width int
	if err := m.nv.ExecLua("return vim.api.nvim_win_get_width(0)", &width); err != nil {
		t.Fatal(err)
	}
	if width != 25 {
		t.Fatalf("renderer fitting undid native resize: width=%d", width)
	}
}

func TestChromeDisconnectNeverClosesCurrentWindow(t *testing.T) {
	m, _ := floatManager(t)
	p := &peer{grid: -11, layer: "chrome"}
	m.panes[p.grid] = p
	if err := m.disconnected(p); err != nil {
		t.Fatal(err)
	}
	windows, err := m.nv.Windows()
	if err != nil || len(windows) != 1 {
		t.Fatalf("chrome closed editor: %v %v", windows, err)
	}
}

func TestChromeMouseUsesRootCoordinates(t *testing.T) {
	m, _ := floatManager(t)
	// A tabline click is resolved by Neovim, not the pane-local navigation widget.
	if err := m.nv.Command("set mouse=a showtabline=2 | tabnew | tabprevious"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	m.mu.Lock()
	m.state.SetWindowUI(true)
	m.mu.Unlock()
	p := &peer{grid: -10, width: 160, height: 1}
	m.panes[p.grid] = p
	m.routePeer(p)
	if r := p.route.Load(); r == nil || !r.chrome || !r.passive {
		t.Fatalf("chrome route = %+v", r)
	}
	if err := m.deliverFast(incoming{p: p, msg: terminal.Message{Type: "mouse", Button: "left", Action: "press", Row: 0, Col: 20}}); err != nil {
		t.Fatal(err)
	}
	var current int
	if err := m.nv.ExecLua("return vim.fn.tabpagenr()", &current); err != nil {
		t.Fatal(err)
	}
	if current != 2 {
		t.Fatalf("root tabline click selected tab %d", current)
	}
}
