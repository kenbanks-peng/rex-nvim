package editor

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
	"github.com/neovim/go-client/nvim"
)

func TestCommandInputAcrossPaneFocusAndResize(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
	if err != nil {
		t.Fatal(err)
	}
	defer nv.Close()
	m := &manager{ctx: ctx, nv: nv, state: screen.New(), keys: make(chan incoming, 16), panes: map[int]*peer{}}
	m.state.SetExternalCmdline(true)
	nv.RegisterHandler("redraw", func(updates ...[]any) { m.mu.Lock(); m.state.Apply(updates...); m.mu.Unlock() })
	go nv.Serve()
	if err := nv.AttachUI(60, 12, screen.UIOptions(false, false, true)); err != nil {
		t.Fatal(err)
	}
	if err := nv.ExecLua(configure+"\nvim.cmd('vsplit');vim.cmd('redraw!')", nil); err != nil {
		t.Fatal(err)
	}
	wait := func(check func() bool) {
		t.Helper()
		end := time.Now().Add(3 * time.Second)
		for time.Now().Before(end) {
			m.mu.Lock()
			ok := check()
			m.mu.Unlock()
			if ok {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("command input state timed out")
	}
	wait(func() bool { return len(m.state.Views()) == 2 })
	views := m.views()
	for _, v := range views {
		p := &peer{grid: v.Grid, width: 30, height: 12}
		p.route.Store(&inputRoute{grid: v.Grid, window: v.Window, width: 30, height: 12})
		m.panes[v.Grid] = p
	}
	var owner int
	wait(func() bool { owner = m.state.CursorGrid(); return m.panes[owner] != nil })
	var sibling *peer
	for grid, p := range m.panes {
		if grid != owner {
			sibling = p
		}
	}
	m.activeGrid.Store(int64(owner))
	go m.sendKeys()
	m.keys <- incoming{p: m.panes[owner], msg: terminal.Message{Type: "input", Keys: ":let g:command_result='界'"}}
	wait(func() bool { return m.state.CmdlineActive() })
	// Host pane focus must not issue a non-fast window RPC while command input is
	// blocking. Input from that sibling still edits Neovim's shared command line.
	if err := m.focus(sibling.grid); err != nil {
		t.Fatal(err)
	}
	if m.activeGrid.Load() != int64(owner) {
		t.Fatal("prompt changed input window")
	}
	for _, height := range []int{0, 1, 2} {
		// ext_cmdline alone does not remove message space. Preserve user cmdheight
		// rather than enabling ext_messages or forcing cmdheight=0.
		if err := nv.ExecLua("vim.o.cmdheight=...;vim.g.rex_nvim_cmdheight=...", nil, height); err != nil {
			t.Fatal(err)
		}
		var size []int
		if err := nv.ExecLua(measureLayout, &size, [][]int{{views[0].Window, 30, 10}, {views[1].Window, 30, 10}}); err != nil {
			t.Fatal(err)
		}
		if len(size) != 5 || size[1] != 10+height || size[2] != height {
			t.Fatalf("cmdheight=%d dimensions %v", height, size)
		}
	}
	if err := nv.TryResizeUI(44, 9); err != nil {
		t.Fatal(err)
	}
	m.keys <- incoming{p: sibling, msg: terminal.Message{Type: "input", Keys: "<Left>X<CR>"}}
	wait(func() bool { return !m.state.CommandInputActive() })
	var value string
	if err := nv.ExecLua("return vim.g.command_result", &value); err != nil || value != "界X" {
		t.Fatalf("shared command edited %q: %v", value, err)
	}
	m.keys <- incoming{p: sibling, msg: terminal.Message{Type: "input", Keys: "iSIBLING<Esc>"}}
	wait(func() bool {
		return m.state.CursorGrid() == sibling.grid && strings.Contains(m.state.Render(sibling.grid, 30, 12), "SIBLING")
	})
	var text []string
	if err := nv.ExecLua("return vim.api.nvim_buf_get_lines(0,0,-1,false)", &text); err != nil || !strings.Contains(strings.Join(text, ""), "SIBLING") {
		t.Fatalf("post-command window focus %v: %v", text, err)
	}
}

func TestGridCommandDefersNativeFittingAndFocus(t *testing.T) {
	s := screen.New()
	s.Apply([]any{"mode_change", []any{"cmdline_normal", 0}})
	m := &manager{state: s}
	if !s.GridCommandActive() || !s.CommandInputActive() {
		t.Fatal("grid command mode not recognized")
	}
	// nil RPC client proves these paths perform no window/layout calls.
	if err := m.fit(); err != nil {
		t.Fatal(err)
	}
	if err := m.focus(2); err != nil {
		t.Fatal(err)
	}
	s.Apply([]any{"mode_change", []any{"normal", 0}})
	if s.GridCommandActive() || s.CommandInputActive() {
		t.Fatal("command mode outlived command")
	}
}
