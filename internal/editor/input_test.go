package editor

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
	"github.com/neovim/go-client/nvim"
)

func TestInputPumpsThroughFocusAutocommand(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessArgs("--embed", "--headless", "--clean"))
	if err != nil {
		t.Fatal(err)
	}
	defer nv.Close()
	nv.RegisterHandler("redraw", func(...[]any) {})
	if err := nv.AttachUI(80, 24, map[string]any{"rgb": true}); err != nil {
		t.Fatal(err)
	}
	var window int
	if err := nv.ExecLua(`
 vim.cmd('vsplit')
 local other=vim.api.nvim_get_current_win()
 vim.cmd('wincmd p')
 vim.api.nvim_create_autocmd('WinEnter',{once=true,callback=function() vim.g.prompt_key=vim.fn.getcharstr() end})
 return other
 `, &window); err != nil {
		t.Fatal(err)
	}
	m := &manager{ctx: ctx, nv: nv, keys: make(chan incoming, 16)}
	p := &peer{}
	p.route.Store(&inputRoute{grid: 2, window: window})
	go m.sendKeys()
	m.keys <- incoming{p: p, msg: terminal.Message{Type: "input", Keys: "iX"}}
	time.Sleep(100 * time.Millisecond)
	m.keys <- incoming{p: p, msg: terminal.Message{Type: "input", Keys: "z"}}
	done := make(chan error, 1)
	go func() {
		for {
			var result string
			err := nv.ExecLua("return vim.g.prompt_key or ''", &result)
			if err != nil {
				done <- err
				return
			}
			if result == "z" {
				done <- nil
				return
			}
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("input worker deadlocked in focus callback")
	}
}
