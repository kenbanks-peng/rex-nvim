package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/neovim/go-client/nvim"
)

func TestOverlappingFloatResizeFeedbackDoesNotResizeLayer(t *testing.T) {
	for _, coalesced := range []bool{false, true} {
		name := "ordered"
		if coalesced {
			name = "coalesced"
		}
		t.Run(name, func(t *testing.T) { testFloatResizeFeedback(t, coalesced) })
	}
}

func testFloatResizeFeedback(t *testing.T, coalesced bool) {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	dir := t.TempDir()
	executable, log := filepath.Join(dir, "rex"), filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\n" + `case "$*" in
 *session.view*) echo '{"windows":[{"window_id":"window","layers":[{"blocks":[{"block_id":"initial"}]}]}]}' ;;
 *session.new_layer*) echo '{"block_ids":["float"],"layer_id":"layer"}' ;;
 *) echo '{}' ;;
esac
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean"))
	if err != nil {
		t.Fatal(err)
	}
	defer nv.Close()
	m := &manager{ctx: ctx, nv: nv, state: screen.New(), rootW: 160, rootH: 45, initial: "initial",
		rex: &rex.Client{Session: "session", Executable: executable}, panes: map[int]*peer{}}
	// Hold redraw delivery so two terminal resizes can overtake their feedback.
	redraws := make(chan [][]any, 256)
	if err := nv.RegisterHandler("redraw", func(u ...[]any) { redraws <- u }); err != nil {
		t.Fatal(err)
	}
	go nv.Serve()
	if err := nv.AttachUI(160, 45, map[string]any{"rgb": true, "ext_multigrid": true}); err != nil {
		t.Fatal(err)
	}
	if err := nv.ExecLua("vim.api.nvim_open_win(vim.api.nvim_create_buf(false,true),true,{relative='editor',row=2,col=8,width=144,height=40,border='rounded'})", nil); err != nil {
		t.Fatal(err)
	}
	next := func(w, h int) screen.View {
		t.Helper()
		if err := nv.Command("redraw"); err != nil {
			t.Fatal(err)
		}
		for {
			select {
			case u := <-redraws:
				m.state.Apply(u...)
				for _, v := range m.views() {
					if v.Float && v.Width == w && v.Height == h {
						return v
					}
				}
			case <-ctx.Done():
				t.Fatalf("missing %dx%d redraw", w, h)
				return screen.View{}
			}
		}
	}
	initial := next(146, 42)
	if err := m.syncFloats([]screen.View{initial}); err != nil {
		t.Fatal(err)
	}
	p := m.panes[initial.Grid]
	bounds := p.bounds
	sizes := [][2]int{{80, 24}, {100, 30}, {146, 42}, {100, 30}}
	for _, size := range sizes {
		p.width, p.height = size[0], size[1]
		if err := m.fitFloat(p); err != nil {
			t.Fatal(err)
		}
		if err := nv.Command("redraw"); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.syncFloats([]screen.View{initial}); err != nil {
		t.Fatal(err)
	}
	for i, size := range sizes {
		v := next(size[0], size[1])
		if coalesced && i < len(sizes)-1 {
			continue
		}
		if err := m.syncFloats([]screen.View{v}); err != nil {
			t.Fatal(err)
		}
		if p.bounds != bounds || p.floatSize != [2]int{146, 42} {
			t.Fatalf("late feedback %v resized layer: bounds=%v size=%v", size, p.bounds, p.floatSize)
		}
	}
	if len(p.floatResizes) != 0 {
		t.Fatalf("unacknowledged resizes: %v", p.floatResizes)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "session.resize_layer") {
		t.Fatalf("feedback wrote layer bounds: %s", calls)
	}
	// Moving a constrained window must not turn its fitted size into a request.
	if err := nv.ExecLua("vim.api.nvim_win_set_config(...,{relative='editor',row=3,col=9})", nil, initial.Window); err != nil {
		t.Fatal(err)
	}
	for {
		v := next(100, 30)
		if v.Row != 3 || v.Col != 9 {
			continue
		}
		if err := m.syncFloats([]screen.View{v}); err != nil {
			t.Fatal(err)
		}
		break
	}
	if p.floatSize != [2]int{146, 42} || p.bounds[2] != bounds[2] || p.bounds[3] != bounds[3] {
		t.Fatalf("move replaced requested size: %+v", p)
	}
	// A genuine plugin size change must still reach Rex.
	if err := nv.ExecLua("vim.api.nvim_win_set_config(...,{width=120,height=32})", nil, initial.Window); err != nil {
		t.Fatal(err)
	}
	v := next(122, 34)
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	if p.floatSize != [2]int{122, 34} || p.bounds == bounds {
		t.Fatalf("plugin resize ignored: %+v", p)
	}
}
