package screen

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neovim/go-client/nvim"
)

func TestNeovimMouseModeAndOptionTransitions(t *testing.T) {
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
	busyStarted, busyStopped := false, false
	nv.RegisterHandler("redraw", func(updates ...[]any) {
		mu.Lock()
		for _, u := range updates {
			if len(u) > 0 {
				if u[0] == "busy_start" {
					busyStarted = true
				}
				if u[0] == "busy_stop" {
					busyStopped = true
				}
			}
		}
		flushed := s.Apply(updates...)
		mu.Unlock()
		if flushed {
			select {
			case changed <- struct{}{}:
			default:
			}
		}
	})
	go nv.Serve()
	if err := nv.AttachUI(40, 10, UIOptions()); err != nil {
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
				t.Fatal("Neovim state transition timed out")
			}
		}
	}
	lua("vim.o.mouse='n';vim.o.mousemoveevent=false;vim.cmd('redraw!')")
	wait(func() bool { return s.mouseEnabled && !s.mouseMove && strings.Contains(s.MouseModes(), "1002h") })
	if _, err := nv.Input("i"); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return !s.mouseEnabled })
	if _, err := nv.Input("<Esc>"); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return s.mouseEnabled })
	lua("vim.o.mousemoveevent=true;vim.cmd('redraw!')")
	wait(func() bool { return s.mouseMove && strings.Contains(s.Render(s.cursorGrid, 40, 10), "1003h") })
	lua("vim.o.mousemoveevent=false;vim.o.mouse='';vim.cmd('redraw!')")
	wait(func() bool { return !s.mouseMove && !s.mouseEnabled })
	// Busy hides the cursor during an operation but must not block input.
	// getcharstr emits busy_start/stop, independently of editor mode.
	done := make(chan error, 1)
	go func() { done <- nv.ExecLua("vim.fn.getcharstr(-1,{cursor='hide'});vim.cmd('redraw!')", nil) }()
	wait(func() bool {
		return busyStarted && s.busy && !strings.Contains(s.Render(s.cursorGrid, 40, 10), "\x1b[?25h")
	})
	if _, err := nv.Input("x"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("busy operation blocked input")
	}
	wait(func() bool {
		return busyStopped && !s.busy && strings.Contains(s.Render(s.cursorGrid, 40, 10), "\x1b[?25h")
	})
}
