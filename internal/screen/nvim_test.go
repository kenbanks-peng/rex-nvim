package screen

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neovim/go-client/nvim"
)

// Verify the protocol assumptions against Neovim rather than synthesizing a
// root winbar or assuming popupmenu grids have normal window handles.
func TestNeovimDecorationsAndCompletion(t *testing.T) {
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
	nv.RegisterHandler("redraw", func(updates ...[]any) {
		mu.Lock()
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
	if err := nv.AttachUI(50, 14, map[string]any{"rgb": true, "ext_multigrid": true, "ext_popupmenu": false}); err != nil {
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
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		for {
			mu.Lock()
			ok := check()
			mu.Unlock()
			if ok {
				return
			}
			select {
			case <-changed:
			case <-deadline.C:
				t.Fatal("timed out waiting for redraw")
			}
		}
	}
	lua("vim.o.laststatus=2;vim.o.statusline='STATUS';vim.o.winbar='WINBAR';vim.cmd('vsplit');vim.api.nvim_buf_set_lines(0,0,-1,false,{'BODY'});vim.cmd('redraw!')")
	wait(func() bool {
		count := 0
		for _, v := range s.Views() {
			if v.Float || v.Hidden {
				continue
			}
			count++
			g := s.grids[v.Grid]
			if g == nil {
				return false
			}
			frame := plain(s.Render(v.Grid, v.Width, v.Height+1))
			if !strings.HasPrefix(frame, "WINBAR") || !strings.Contains(frame, "STATUS") {
				return false
			}
			// Root statusline is outside win_pos height; winbar is grid row 0.
			if !strings.HasPrefix(gridRow(g, 0), "WINBAR") {
				return false
			}
			if !strings.Contains(gridRow(s.grids[1], v.Row+g.height), "STATUS") {
				return false
			}
		}
		return count == 2
	})
	lua("vim.cmd('startinsert')")
	if _, err := nv.Input("abc"); err != nil {
		t.Fatal(err)
	}
	lua("vim.fn.complete(1,{'abcdef','abcxyz'});vim.cmd('redraw!')")
	popup := 0
	wait(func() bool {
		for _, v := range s.Views() {
			if !v.Float || v.Hidden || v.Focusable {
				continue
			}
			if strings.Contains(plain(s.Render(v.Grid, v.Width, v.Height)), "abcdef") {
				popup = v.Grid
				return true
			}
		}
		return false
	})
	mu.Lock()
	parent := s.views[popup].parent
	w, h := s.GridSize(parent)
	if frame := plain(s.Render(parent, w, h)); !strings.Contains(frame, "abcxyz") {
		mu.Unlock()
		t.Fatalf("completion not composited: %q", frame)
	}
	s.SetExternalGrids(map[int]bool{popup: true})
	if frame := plain(s.Render(parent, w, h)); strings.Contains(frame, "abcxyz") {
		mu.Unlock()
		t.Fatal("external completion duplicated")
	}
	mu.Unlock()
	// Cancellation emits win_hide or grid_destroy, depending on Neovim version.
	if _, err := nv.Input(""); err != nil {
		t.Fatal(err)
	}
	lua("vim.cmd('redraw!')")
	wait(func() bool { p := s.views[popup]; return p == nil || p.Hidden })
}

func gridRow(g *grid, row int) string {
	if g == nil || row < 0 || row >= g.height {
		return ""
	}
	var b strings.Builder
	for _, c := range g.cells[row*g.width : (row+1)*g.width] {
		b.WriteString(c.text)
	}
	return b.String()
}
