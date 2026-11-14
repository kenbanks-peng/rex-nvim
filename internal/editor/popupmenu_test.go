package editor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
	"github.com/neovim/go-client/nvim"
)

func TestNeovimExternalPopupmenu(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
	if err != nil {
		t.Fatal(err)
	}
	defer nv.Close()
	m := &manager{ctx: ctx, nv: nv, state: screen.New(), panes: map[int]*peer{}}
	m.state.SetExternalPopupmenu(true)
	nv.RegisterHandler("redraw", func(updates ...[]any) { m.mu.Lock(); m.state.Apply(updates...); m.mu.Unlock() })
	go nv.Serve()
	if err := nv.AttachUI(80, 24, screen.UIOptions(false, true)); err != nil {
		t.Fatal(err)
	}
	lua := func(code string) {
		t.Helper()
		if err := nv.ExecLua(code, nil); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			m.mu.Lock()
			ok := check()
			m.mu.Unlock()
			if ok {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("popup redraw timeout")
	}
	lua("vim.o.mouse='a';vim.o.completeopt='menuone,noselect';vim.cmd('vsplit')")
	for _, floating := range []bool{false, true} {
		if floating {
			lua("vim.api.nvim_open_win(vim.api.nvim_create_buf(false,true),true,{relative='editor',row=3,col=45,width=25,height=10})")
		}
		lua("vim.api.nvim_buf_set_lines(0,0,-1,false,{'a'});vim.api.nvim_win_set_cursor(0,{1,1});vim.cmd('startinsert!')")
		if _, err := nv.Input("a"); err != nil {
			t.Fatal(err)
		}
		lua("local items={};for i=1,30 do items[i]=string.format('alpha%02d',i) end;vim.fn.complete(1,items);vim.cmd('redraw!')")
		var id int
		wait(func() bool {
			id = m.state.CursorGrid()
			for _, v := range m.state.Views() {
				if v.Grid == id {
					m.panes = map[int]*peer{id: {grid: id, width: v.Width, height: v.Height, frames: make(chan string, 1)}}
					m.state.SetExternalGrids(map[int]bool{id: true})
					g, ok := m.state.PopupGeometry(id, v.Width, v.Height)
					return ok && g.Height > 0 && strings.Contains(m.state.Render(id, v.Width, v.Height), "alpha01")
				}
			}
			return false
		})
		p := m.panes[id]
		m.reportPopupGeometry()
		var position map[string]any
		if err := nv.ExecLua("return vim.fn.pum_getpos()", &position); err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]int{"height": m.popupGeometry.Height, "width": m.popupGeometry.Width, "row": m.popupGeometry.RootRow, "col": m.popupGeometry.RootCol} {
			if fmt.Sprint(position[key]) != fmt.Sprint(want) {
				t.Fatalf("geometry %s=%v, want %d (%v)", key, position[key], want, position)
			}
		}
		if _, err := nv.Input("<C-n><C-n><PageDown>"); err != nil {
			t.Fatal(err)
		}
		lua("vim.cmd('redraw!')")
		p.width, p.height = 14, 6
		m.draw()
		if m.popupGeometry.Width > 14 || m.popupGeometry.Height > 6 {
			t.Fatal("resize bounds not clipped")
		}
		// Selection API used by mouse keeps Neovim authoritative.
		p.route.Store(&inputRoute{grid: id, width: p.width, height: p.height})
		m.mu.Lock()
		g, ok := m.state.PopupGeometry(id, p.width, p.height)
		m.mu.Unlock()
		if !ok {
			t.Fatal("popup lost on resize")
		}
		consumed, err := m.popupInput(incoming{p: p, msg: terminal.Message{Type: "mouse", Button: "left", Action: "press", Row: g.Row, Col: g.Col}})
		if err != nil || !consumed {
			t.Fatalf("mouse selection: %v %v", consumed, err)
		}
		wait(func() bool { _, ok := m.state.PopupGeometry(id, p.width, p.height); return !ok })
		lua("vim.cmd('stopinsert');vim.cmd('startinsert')")
		if _, err := nv.Input("a"); err != nil {
			t.Fatal(err)
		}
		lua("vim.fn.complete(1,{'alphaCANCEL','alphaOTHER'});vim.cmd('redraw!')")
		p.width, p.height = 40, 20
		wait(func() bool { _, ok := m.state.PopupGeometry(id, p.width, p.height); return ok })
		if _, err := nv.Input("<C-e><Esc>"); err != nil {
			t.Fatal(err)
		}
		wait(func() bool { _, ok := m.state.PopupGeometry(id, p.width, p.height); return !ok })
		if floating {
			lua("vim.api.nvim_win_close(0,true)")
		}
	}
}
