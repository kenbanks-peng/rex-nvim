package editor

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
	"github.com/neovim/go-client/nvim"
)

func TestNeovimMouseInputFollowsUIState(t *testing.T) {
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
	nv.RegisterHandler("redraw", func(...[]any) {})
	if err := nv.AttachUI(40, 10, screen.UIOptions()); err != nil {
		t.Fatal(err)
	}
	if err := nv.ExecLua("vim.o.mouse='a';vim.g.clicks=0;vim.keymap.set('n','<LeftMouse>',function() vim.g.clicks=vim.g.clicks+1 end)", nil); err != nil {
		t.Fatal(err)
	}
	m := &manager{ctx: ctx, nv: nv, state: screen.New()}
	p := &peer{}
	p.route.Store(&inputRoute{grid: 0})
	click := incoming{p: p, msg: terminal.Message{Type: "mouse", Button: "left", Action: "press", Row: 1, Col: 1}}
	// A queued terminal event must not bypass mouse_off in the backend.
	m.state.Apply([]any{"mouse_off", []any{}})
	if err := m.deliverFast(click); err != nil {
		t.Fatal(err)
	}
	var clicks int
	if err := nv.ExecLua("return vim.g.clicks", &clicks); err != nil {
		t.Fatal(err)
	}
	if clicks != 0 {
		t.Fatalf("mouse_off delivered %d clicks", clicks)
	}
	m.state.Apply([]any{"mouse_on", []any{}})
	if err := m.deliverFast(click); err != nil {
		t.Fatal(err)
	}
	for {
		if err := nv.ExecLua("return vim.g.clicks", &clicks); err != nil {
			t.Fatal(err)
		}
		if clicks == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("mouse_on failed to deliver: %d", clicks)
		case <-time.After(time.Millisecond):
		}
	}
}
