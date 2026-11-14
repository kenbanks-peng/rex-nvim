package screen

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neovim/go-client/nvim"
)

func TestNeovimCursorModes(t *testing.T) {
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
	if err := nv.AttachUI(40, 10, map[string]any{"rgb": true, "ext_multigrid": true}); err != nil {
		t.Fatal(err)
	}
	lua := func(code string) {
		t.Helper()
		if err := nv.ExecLua(code, nil); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(want string) {
		t.Helper()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			mu.Lock()
			w, h := s.GridSize(s.CursorGrid())
			frame := s.Render(s.CursorGrid(), w, h)
			mu.Unlock()
			if strings.Contains(frame, want) {
				return
			}
			select {
			case <-changed:
			case <-timer.C:
				t.Fatalf("waiting for %q; last frame: %q", want, frame)
			}
		}
	}
	lua("vim.o.guicursor='n:block-blinkon0,i:ver25-blinkon0,r:hor20-blinkon0';vim.cmd('redraw!')")
	wait("\x1b[2 q")
	if _, err := nv.Input("i"); err != nil {
		t.Fatal(err)
	}
	wait("\x1b[6 q")
	if _, err := nv.Input("<Esc>R"); err != nil {
		t.Fatal(err)
	}
	wait("\x1b[4 q")
	if _, err := nv.Input("<Esc>"); err != nil {
		t.Fatal(err)
	}
	wait("\x1b[2 q")
	lua("vim.o.guicursor='n:ver25-blinkon500-blinkoff500';vim.cmd('redraw!')")
	wait("\x1b[5 q")
	lua("vim.o.guicursor='';vim.cmd('redraw!')")
	wait("\x1b[2 q")

	lua("vim.api.nvim_set_hl(0,'FidelityCursor',{bg='#aabbcc',fg='#010203'});vim.o.guicursor='n:block-FidelityCursor';vim.cmd('redraw!')")
	wait("\x1b]12;#aabbcc\x1b\\")
	lua("vim.api.nvim_set_hl(0,'Normal',{fg='#112233',bg='#445566'});vim.cmd('redraw!')")
	wait("38;2;17;34;51;48;2;68;85;102")
	lua(`vim.api.nvim_buf_set_lines(0,0,-1,false,{'ABCDE'})
local ns=vim.api.nvim_create_namespace('fidelity')
for i,key in ipairs({'underline','underdouble','undercurl','underdotted','underdashed'}) do
  vim.api.nvim_set_hl(0,'Fidelity'..i,{[key]=true,sp='#010203'})
  vim.api.nvim_buf_set_extmark(0,ns,0,i-1,{end_col=i,hl_group='Fidelity'..i})
end
vim.cmd('redraw!')`)
	for _, style := range []string{";4;", ";4:2;", ";4:3;", ";4:4;", ";4:5;"} {
		wait(style)
	}
	wait("58:2::1:2:3")
	lua("vim.o.guicursor='';vim.cmd('redraw!')")
	wait("\x1b]112\x1b\\")
}
