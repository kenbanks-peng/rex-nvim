package editor

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/neovim/go-client/nvim"
)

func TestFloatTerminalSizeDoesNotEnlargePopup(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean"))
	if err != nil {
		t.Fatal(err)
	}
	defer nv.Close()
	m := &manager{nv: nv, state: screen.New(), rootW: 80, rootH: 24}
	if err := nv.RegisterHandler("redraw", func(u ...[]any) { m.mu.Lock(); defer m.mu.Unlock(); m.state.Apply(u...) }); err != nil {
		t.Fatal(err)
	}
	go nv.Serve()
	if err := nv.AttachUI(80, 24, map[string]any{"rgb": true, "ext_multigrid": true}); err != nil {
		t.Fatal(err)
	}
	if err := nv.ExecLua("vim.api.nvim_open_win(vim.api.nvim_create_buf(false,true),true,{relative='editor',row=15,col=40,width=30,height=6})", nil); err != nil {
		t.Fatal(err)
	}
	if err := nv.Command("redraw"); err != nil {
		t.Fatal(err)
	}
	var popup screen.View
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline) && popup.Grid == 0; {
		for _, v := range m.views() {
			if v.Float {
				popup = v
			}
		}
		if popup.Grid == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	if popup.Grid == 0 {
		t.Fatal("missing popup")
	}
	p := &peer{grid: popup.Grid, geometry: geometry(popup)}
	for _, size := range [][2]int{{80, 24}, {20, 4}, {80, 24}} {
		p.width, p.height = size[0], size[1]
		if err := m.fitFloat(p); err != nil {
			t.Fatal(err)
		}
		if err := nv.Command("redraw"); err != nil {
			t.Fatal(err)
		}
		wantW, wantH := min(30, size[0]), min(6, size[1])
		var got screen.View
		for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
			for _, v := range m.views() {
				if v.Grid == p.grid {
					got = v
				}
			}
			if got.Width == wantW && got.Height == wantH {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if got.Width != wantW || got.Height != wantH {
			t.Fatalf("terminal %v: popup %dx%d, want %dx%d", size, got.Width, got.Height, wantW, wantH)
		}
	}
}
