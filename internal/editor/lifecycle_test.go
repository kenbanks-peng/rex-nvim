package editor

import (
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/neovim/go-client/nvim"
)

func TestDetachBounded(t *testing.T) {
	want := errors.New("RPC closed")
	if err := detachUI(func() error { return want }, time.Second); !errors.Is(err, want) {
		t.Fatal(err)
	}
	release := make(chan struct{})
	defer close(release)
	start := time.Now()
	if err := detachUI(func() error { <-release; return nil }, 10*time.Millisecond); err == nil {
		t.Fatal("unbounded detach")
	}
	if time.Since(start) > time.Second {
		t.Fatal("detach blocked cleanup")
	}
}

func TestNeovimDetachReattachPreservesEditor(t *testing.T) {
	for _, mode := range []string{"grid", "external", "message-grid"} {
		t.Run(mode, func(t *testing.T) { testDetachReattachPreservesEditor(t, mode) })
	}
}

func testDetachReattachPreservesEditor(t *testing.T, mode string) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	nv, err := nvim.NewChildProcess(nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
	if err != nil {
		t.Fatal(err)
	}
	defer nv.Close()
	if err := nv.RegisterHandler("redraw", func(...[]any) {}); err != nil {
		t.Fatal(err)
	}
	external := mode != "grid"
	options := screen.UIOptions(external, external, external, mode == "external")
	if err := nv.AttachUI(80, 24, options); err != nil {
		t.Fatal(err)
	}
	if err := nv.ExecLua("vim.api.nvim_buf_set_lines(0,0,-1,false,{'unsaved lifecycle'}); vim.cmd('vsplit')", nil); err != nil {
		t.Fatal(err)
	}
	if err := detachUI(nv.DetachUI, time.Second); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := nv.ExecLua("return #vim.api.nvim_list_uis()", &count); err != nil || count != 0 {
		t.Fatalf("detach: %d %v", count, err)
	}
	if err := nv.AttachUI(90, 30, options); err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err := nv.ExecLua("return #vim.api.nvim_list_uis()==1 and #vim.api.nvim_list_wins()==2 and vim.bo.modified and vim.api.nvim_get_current_line()=='unsaved lifecycle'", &preserved); err != nil || !preserved {
		t.Fatalf("reattach lost editor: %v %v", preserved, err)
	}
}
