package screen

import (
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/neovim/go-client/nvim"
)

func TestNeovimHostEvents(t *testing.T) {
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
	seen := map[string]bool{}
	nv.RegisterHandler("redraw", func(updates ...[]any) {
		mu.Lock()
		for _, u := range updates {
			if len(u) > 0 {
				if name, ok := u[0].(string); ok {
					seen[name] = true
				}
			}
		}
		s.Apply(updates...)
		mu.Unlock()
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	go nv.Serve()
	if err := nv.AttachUI(40, 10, UIOptions()); err != nil {
		t.Fatal(err)
	}
	title := "Neovim title ' $(not a shell)"
	path := t.TempDir()
	if err := nv.ExecLua("local title,path=...;vim.o.titlestring=title;vim.o.title=true;vim.o.iconstring='nvim-icon';vim.o.icon=true;vim.api.nvim_set_current_dir(path);vim.o.belloff='';vim.o.visualbell=false;vim.cmd('redraw!')", nil, title, path); err != nil {
		t.Fatal(err)
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
				t.Fatal("host event timed out")
			}
		}
	}
	wait(func() bool {
		return s.host.Title != nil && *s.host.Title == title && s.host.Directory != nil && *s.host.Directory == path && s.host.Icon != nil && *s.host.Icon == "nvim-icon"
	})
	if _, err := nv.Input("h"); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return seen["bell"] && len(s.host.Bells) > 0 })
	if err := nv.ExecLua("vim.o.visualbell=true", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := nv.Input("h"); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return seen["visual_bell"] })
}
