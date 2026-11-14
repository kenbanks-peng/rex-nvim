package editor

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
	"github.com/neovim/go-client/nvim"
)

func TestPassiveFloatInputStaysInCurrentWindow(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
	if err != nil {
		t.Fatal(err)
	}
	defer nv.Close()
	var wins []int
	if err := nv.ExecLua(`
local a=vim.api
local owner=a.nvim_get_current_win()
local popup=a.nvim_open_win(a.nvim_create_buf(false,true),false,{
 relative='editor',row=1,col=1,width=20,height=3,focusable=false})
return {owner,popup}
`, &wins); err != nil {
		t.Fatal(err)
	}
	m := &manager{ctx: ctx, nv: nv, keys: make(chan incoming, 1)}
	p := &peer{}
	p.route.Store(&inputRoute{grid: 3, window: wins[1], passive: true})
	m.activeGrid.Store(2)
	done := make(chan struct{})
	go func() { defer close(done); m.sendKeys() }()
	defer func() { cancel(); <-done }()
	m.keys <- incoming{p: p, msg: terminal.Message{Type: "input", Keys: "iPASSIVE<Esc>"}}
	for {
		var text string
		if err := nv.ExecLua("return vim.api.nvim_get_current_line()", &text); err != nil {
			t.Fatal(err)
		}
		if text == "PASSIVE" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("input did not reach the editor")
		case <-time.After(time.Millisecond):
		}
	}
	current, err := nv.CurrentWindow()
	if err != nil {
		t.Fatal(err)
	}
	if int(current) != wins[0] {
		t.Fatal("display-only float took focus")
	}
}

func TestFloatSpaceSurvivesOriginalBlockClosure(t *testing.T) {
	m := &manager{initial: "closed", panes: map[int]*peer{2: {block: "successor"}}}
	view := rex.View{Windows: []rex.Window{{ID: "window", Layers: []rex.Layer{{
		Blocks: []rex.Block{{ID: "successor"}},
		Layout: &rex.Node{BlockID: "successor", Rect: [4]float64{.2, .3, .4, .5}},
	}}}}}
	window, bounds := m.floatSpace(view)
	if window != "window" || bounds != [4]float64{0, 0, 1, 1} {
		t.Fatalf("float canvas: %q %v", window, bounds)
	}
}

func TestRouteTracksFloatFocusability(t *testing.T) {
	m := &manager{state: screen.New()}
	m.state.Apply([]any{"win_float_pos", []any{3, 1001, "NW", 1, 0, 0, false, 50}})
	p := &peer{grid: 3}
	m.routePeer(p)
	if !p.route.Load().passive {
		t.Fatal("non-focusable float has active input route")
	}
	m.state.Apply([]any{"win_float_pos", []any{3, 1001, "NW", 1, 0, 0, true, 50}})
	m.routePeer(p)
	if p.route.Load().passive {
		t.Fatal("route did not follow changed focusability")
	}
}
