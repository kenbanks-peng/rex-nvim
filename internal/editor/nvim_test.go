package editor

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/neovim/go-client/nvim"
)

func TestRealNeovimRedraw(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	nv, err := nvim.NewChildProcess(nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
	if err != nil {
		t.Fatal(err)
	}
	defer nv.Close()
	s := screen.New()
	var mu sync.Mutex
	flushed := make(chan struct{}, 1)
	nv.RegisterHandler("redraw", func(u ...[]any) {
		mu.Lock()
		defer mu.Unlock()
		if s.Apply(u...) {
			select {
			case flushed <- struct{}{}:
			default:
			}
		}
	})
	go nv.Serve()
	if err := nv.AttachUI(80, 24, map[string]any{"rgb": true, "ext_multigrid": true}); err != nil {
		t.Fatal(err)
	}
	if err := nv.ExecLua("vim.api.nvim_buf_set_lines(0,0,-1,false,{'hello rex'})", nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-flushed:
			mu.Lock()
			for _, v := range s.Views() {
				frame := s.Render(v.Grid, 80, 24)
				t.Logf("view: %+v frame prefix %.80q", v, frame)
				if strings.Contains(frame, "hello rex") {
					mu.Unlock()
					return
				}
			}
			mu.Unlock()
		case <-deadline:
			t.Fatal("no rendered buffer text")
		}
	}
}
