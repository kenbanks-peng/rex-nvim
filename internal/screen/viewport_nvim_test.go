package screen

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neovim/go-client/nvim"
)

func TestNeovimViewportGridFallback(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	nv, err := nvim.NewChildProcess(nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
	if err != nil {
		t.Fatal(err)
	}
	defer nv.Close()
	s := New()
	var mu sync.Mutex
	changed := make(chan struct{}, 1)
	counts := map[string]int{}
	nv.RegisterHandler("redraw", func(updates ...[]any) {
		mu.Lock()
		for _, u := range updates {
			if len(u) > 0 {
				if name, ok := u[0].(string); ok {
					counts[name]++
				}
			}
		}
		flush := s.Apply(updates...)
		mu.Unlock()
		if flush {
			select {
			case changed <- struct{}{}:
			default:
			}
		}
	})
	go nv.Serve()
	if err := nv.AttachUI(50, 16, UIOptions()); err != nil {
		t.Fatal(err)
	}
	lua := func(code string, result any) {
		t.Helper()
		if err := nv.ExecLua(code, result); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(check func() bool) {
		t.Helper()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			mu.Lock()
			ok := check()
			mu.Unlock()
			if ok {
				return
			}
			select {
			case <-changed:
			case <-timer.C:
				mu.Lock()
				for _, v := range s.Views() {
					t.Logf("view=%+v frame=%q", v, plain(s.Render(v.Grid, 50, 16)))
				}
				t.Logf("events=%v", counts)
				mu.Unlock()
				t.Fatal("viewport fallback redraw timed out")
			}
		}
	}
	var wins []int
	lua(`
vim.o.laststatus=0;vim.o.cmdheight=0;vim.o.showtabline=0
vim.o.winbar='VIEWBAR';vim.o.scrolloff=0;vim.o.number=true
local lines={};for i=1,80 do lines[i]=string.format('LINE%02d',i) end
vim.api.nvim_buf_set_lines(0,0,-1,false,lines)
_G.base=vim.api.nvim_get_current_win()
local ns=vim.api.nvim_create_namespace('viewport-test')
vim.api.nvim_buf_set_extmark(0,ns,3,0,{virt_text={{'VIRTUAL','Normal'}},virt_text_pos='eol'})
vim.api.nvim_buf_set_extmark(0,ns,4,0,{virt_lines={{{'VIRTLINES','Normal'}}}})
local b=vim.api.nvim_create_buf(false,true)
vim.api.nvim_buf_set_lines(b,0,-1,false,{'OVERLAY'})
_G.overlay=vim.api.nvim_open_win(b,false,{relative='win',win=base,bufpos={5,0},row=0,col=12,width=9,height=1,border='single'})
vim.wo[overlay].number=false;vim.wo[overlay].relativenumber=false
vim.cmd('redraw!')
return {base,overlay}
`, &wins)
	if len(wins) != 2 {
		t.Fatal(wins)
	}
	find := func(win int) *placement {
		for _, p := range s.views {
			if p.Window == win {
				return p
			}
		}
		return nil
	}
	var grid, floatGrid, oldRow int
	wait(func() bool {
		p, f := find(wins[0]), find(wins[1])
		if p == nil || f == nil {
			return false
		}
		grid, floatGrid = p.Grid, f.Grid
		frame := plain(s.Render(grid, 50, 16))
		if !strings.HasPrefix(frame, "VIEWBAR") || !strings.Contains(gridRow(s.grids[floatGrid], 0), "┌─────────┐") || !strings.Contains(frame, "LINE01") || !strings.Contains(frame, "VIRTUAL") || !strings.Contains(frame, "VIRTLINES") || !strings.Contains(frame, "OVERLAY") {
			return false
		}
		oldRow, _, _ = s.origin(floatGrid, map[int]bool{})
		return counts["win_viewport"] > 0 && counts["win_viewport_margins"] > 0
	})
	// Plugin-owned ui_watched marks may arrive, but rex-nvim never requests them.
	lua("vim.api.nvim_buf_set_extmark(0,vim.api.nvim_create_namespace('plugin-watched'),3,0,{ui_watched=true});vim.cmd('redraw!')", nil)
	wait(func() bool { return counts["win_extmark"] > 0 })
	lua("vim.fn.winrestview({topline=3,lnum=3,col=0});vim.cmd('redraw!')", nil)
	wait(func() bool {
		r, _, ok := s.origin(floatGrid, map[int]bool{})
		frame := plain(s.Render(grid, 50, 16))
		return ok && r < oldRow && strings.Contains(frame, "VIEWBAR") && strings.Contains(frame, "LINE03") && !strings.Contains(frame, "LINE01") && strings.Contains(frame, "VIRTUAL") && strings.Contains(frame, "OVERLAY")
	})
	if err := nv.TryResizeUI(38, 12); err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		w, h := s.GridSize(1)
		return w == 38 && h == 12 && strings.Contains(plain(s.Render(grid, 38, 12)), "VIEWBAR") && strings.Contains(plain(s.Render(grid, 38, 12)), "OVERLAY")
	})
	lua("vim.cmd('tabnew');vim.cmd('redraw!')", nil)
	wait(func() bool { p := s.views[grid]; return p == nil || p.Hidden })
	lua("vim.cmd('tabprevious');vim.cmd('redraw!')", nil)
	wait(func() bool {
		p := find(wins[0])
		return p != nil && !p.Hidden && strings.Contains(plain(s.Render(p.Grid, 38, 12)), "OVERLAY")
	})
	lua(fmt.Sprintf("vim.api.nvim_win_close(%d,true);vim.cmd('redraw!')", wins[1]), nil)
	wait(func() bool {
		p := find(wins[1])
		return (p == nil || p.Hidden) && !strings.Contains(plain(s.Render(grid, 38, 12)), "OVERLAY")
	})
	// The same grid-aware wheel RPC used by Rex input owns scrolling; no
	// viewport-to-buffer conversion or margin subtraction is needed.
	if err := nv.InputMouse("wheel", "down", "", grid, 2, 5); err != nil {
		t.Fatal(err)
	}
	var top int
	lua("vim.cmd('redraw!');return vim.fn.line('w0')", &top)
	if top <= 3 {
		t.Fatalf("wheel did not scroll: topline=%d", top)
	}
	wait(func() bool {
		frame := plain(s.Render(grid, 38, 12))
		return strings.Contains(frame, fmt.Sprintf("LINE%02d", top)) && !strings.Contains(frame, "LINE03") && strings.Contains(frame, "VIEWBAR")
	})
	// A complete repaint must agree with incremental scrolling/resize rendering.
	mu.Lock()
	before := s.Render(grid, 38, 12)
	previous := counts["flush"]
	mu.Unlock()
	lua("vim.cmd('redraw!')", nil)
	wait(func() bool { return counts["flush"] > previous })
	mu.Lock()
	defer mu.Unlock()
	if after := s.Render(grid, 38, 12); before != after {
		t.Fatal("incremental viewport differs from full redraw")
	}
}
