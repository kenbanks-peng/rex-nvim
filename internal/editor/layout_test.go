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

func TestHelloRetainsDimensions(t *testing.T) {
	m := &manager{state: screen.New(), panes: map[int]*peer{}, initial: "initial"}
	target := &peer{grid: 2, primary: true, block: "initial"}
	m.panes[2] = target
	// No views yet means fit is a no-op: isolate renderer registration.
	p := &peer{grid: 2, frames: make(chan string, 1)}
	if err := m.handle(incoming{p: p, msg: terminal.Message{Type: "hello", Width: 91, Height: 37}}); err != nil {
		t.Fatal(err)
	}
	if m.panes[2] != p || p.width != 91 || p.height != 37 || !p.primary {
		t.Fatalf("renderer lost metadata: %+v", p)
	}
}
func TestPlacement(t *testing.T) {
	anchor := screen.View{Grid: 2, Width: 40, Height: 20, Row: 20, Col: 40}
	for _, tc := range []struct {
		row, col int
		want     string
	}{{20, 0, "left"}, {20, 80, "right"}, {0, 40, "above"}, {40, 40, "below"}} {
		next := screen.View{Grid: 4, Width: 40, Height: 20, Row: tc.row, Col: tc.col}
		p := &peer{block: "anchor"}
		got, dir, ratio := placement(next, []screen.View{anchor, next}, map[int]*peer{2: p})
		if got != p || dir != tc.want || ratio != .5 {
			t.Fatalf("got %v %s %f, want %s", got, dir, ratio, tc.want)
		}
	}
}
func TestFittingDoesNotGrowCommandHeight(t *testing.T) {
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
	m := &manager{nv: nv, ctx: ctx, state: screen.New(), panes: map[int]*peer{}, rootW: 80, rootH: 24}
	nv.RegisterHandler("redraw", func(u ...[]any) { m.mu.Lock(); m.state.Apply(u...); m.mu.Unlock() })
	go nv.Serve()
	if err := nv.ExecLua(configure, nil); err != nil {
		t.Fatal(err)
	}
	if err := nv.AttachUI(80, 24, map[string]any{"rgb": true, "ext_multigrid": true}); err != nil {
		t.Fatal(err)
	}
	initial, err := m.nativeLayout()
	if err != nil {
		t.Fatal(err)
	}
	if initial.Kind != "leaf" || initial.Window == 0 || initial.Width != 80 {
		t.Fatalf("RPC layout decoding failed: %+v", initial)
	}
	for _, cmd := range []string{"vsplit", "split", "close", "close", "tabnew", "tabprevious"} {
		if err := nv.Command(cmd); err != nil {
			t.Fatal(err)
		}
		// Flush makes callback state available before measuring, independent of frame cadence.
		if err := nv.Command("redraw"); err != nil {
			t.Fatal(err)
		}
		for _, v := range m.views() {
			if !v.Hidden && !v.Float {
				m.panes[v.Grid] = &peer{grid: v.Grid, width: 80, height: 24}
			} else {
				delete(m.panes, v.Grid)
			}
		}
		for i := 0; i < 3; i++ {
			if err := m.fit(); err != nil {
				t.Fatal(err)
			}
		}
		var ch int
		if err := nv.ExecLua("return vim.o.cmdheight", &ch); err != nil {
			t.Fatal(err)
		}
		if ch != 1 {
			t.Fatalf("%s: cmdheight grew to %d", cmd, ch)
		}
		if m.rootH > 50 {
			t.Fatalf("%s: root height grew to %d", cmd, m.rootH)
		}
	}
}
