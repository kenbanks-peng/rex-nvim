package editor

import (
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/neovim/go-client/nvim"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTabRedraw(t *testing.T) {
	nv, err := nvim.NewChildProcess(nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean"))
	if err != nil {
		t.Skip(err)
	}
	defer nv.Close()
	s := screen.New()
	var mu sync.Mutex
	nv.RegisterHandler("redraw", func(u ...[]any) { mu.Lock(); defer mu.Unlock(); s.Apply(u...) })
	go nv.Serve()
	nv.AttachUI(80, 24, map[string]any{"rgb": true, "ext_multigrid": true})
	nv.ExecLua("vim.api.nvim_buf_set_lines(0,0,-1,false,{'REX_SHARED_BUFFER'});vim.cmd('vsplit');vim.cmd('split');vim.cmd('close');vim.cmd('close');vim.cmd('tabnew');vim.cmd('tabprevious');vim.cmd('redraw')", nil)
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, v := range s.Views() {
		t.Logf("%+v", v)
		if !v.Hidden && !v.Float {
			f := s.Render(v.Grid, 80, 24)
			t.Logf("%.150q", f)
			if strings.Contains(f, "REX_SHARED_BUFFER") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("missing rendered original tab")
	}
}
