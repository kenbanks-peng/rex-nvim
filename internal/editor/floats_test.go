package editor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
	"github.com/neovim/go-client/nvim"
)

func floatManager(t *testing.T) (*manager, string) {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	dir := t.TempDir()
	executable, log := filepath.Join(dir, "rex"), filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\n" + `case "$*" in
 *session.view*) if [ -f "$(dirname "$0")/view.json" ]; then cat "$(dirname "$0")/view.json"; else echo '{"windows":[{"window_id":"window","layers":[{"blocks":[{"block_id":"initial"}]}]}]}'; fi ;;
 *session.new_layer*) echo '{"block_ids":["float"],"layer_id":"layer"}' ;;
 *) echo '{}' ;;
esac
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { nv.Close() })
	m := &manager{ctx: ctx, nv: nv, state: screen.New(), rootW: 160, rootH: 45, initial: "initial",
		rex: &rex.Client{Session: "session", Executable: executable}, panes: map[int]*peer{}}
	if err := nv.RegisterHandler("redraw", func(u ...[]any) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.state.Apply(u...)
	}); err != nil {
		t.Fatal(err)
	}
	go nv.Serve()
	if err := nv.AttachUI(160, 45, map[string]any{"rgb": true, "ext_multigrid": true}); err != nil {
		t.Fatal(err)
	}
	return m, log
}

func floatView(t *testing.T, m *manager, want [4]int) screen.View {
	t.Helper()
	if err := m.nv.Command("redraw"); err != nil {
		t.Fatal(err)
	}
	var got screen.View
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		for _, v := range m.views() {
			if v.Float && !v.Hidden {
				got = v
				if [4]int{v.Row, v.Col, v.Width, v.Height} == want {
					return v
				}
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("popup = %+v, want geometry %v", got, want)
	return screen.View{}
}

func TestFloatRendererNeverResizesPluginWindow(t *testing.T) {
	for _, tc := range []struct {
		name, border string
		extra        [2]int
	}{
		{"rounded", "'rounded'", [2]int{2, 2}},
		{"partial_border", "{'','','','│','╯','─','',''}", [2]int{1, 1}},
		{"borderless", "'none'", [2]int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, log := floatManager(t)
			if err := m.nv.Command("vsplit"); err != nil {
				t.Fatal(err)
			}
			if err := m.nv.ExecLua("vim.api.nvim_open_win(vim.api.nvim_create_buf(false,true),true,{relative='editor',row=2,col=8,width=144,height=40,border="+tc.border+"})", nil); err != nil {
				t.Fatal(err)
			}
			want := [4]int{2, 8, 144 + tc.extra[0], 40 + tc.extra[1]}
			v := floatView(t, m, want)
			if err := m.syncFloats([]screen.View{v}); err != nil {
				t.Fatal(err)
			}
			bounds := m.panes[v.Grid].bounds
			if expected := [4]float64{8.0 / 160, 2.0 / 45, float64(want[2]) / 160, float64(want[3]) / 45}; bounds != expected {
				t.Fatalf("initial bounds = %v, want %v", bounds, expected)
			}
			p := &peer{grid: v.Grid, ready: make(chan struct{}), frames: make(chan string, 1)}
			if err := m.handle(incoming{p: p, msg: terminal.Message{Type: "hello", Block: "float", Width: 80, Height: 24}}); err != nil {
				t.Fatal(err)
			}
			// Assert the first startup size, not merely eventual convergence.
			check := func() {
				t.Helper()
				var size []int
				if err := m.nv.ExecLua("return {vim.api.nvim_win_get_width(...),vim.api.nvim_win_get_height(...)}", &size, v.Window); err != nil {
					t.Fatal(err)
				}
				if size[0] != 144 || size[1] != 40 {
					t.Fatalf("renderer changed plugin window to %v", size)
				}
			}
			check()
			// No intervening redraw delivery is needed to classify terminal sizes.
			for _, size := range [][2]int{{20, 4}, {100, 30}, {160, 45}, {1, 1}, {146, 42}} {
				if err := m.handle(incoming{p: p, msg: terminal.Message{Type: "resize", Width: size[0], Height: size[1]}}); err != nil {
					t.Fatal(err)
				}
				check()
			}
			v = floatView(t, m, want)
			if err := m.syncFloats([]screen.View{v}); err != nil {
				t.Fatal(err)
			}
			if p.bounds != bounds {
				t.Fatalf("terminal changed Rex bounds: %v, want %v", p.bounds, bounds)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(calls), "session.resize_layer") {
				t.Fatalf("renderer sizes wrote layer bounds: %s", calls)
			}
			// Genuine plugin updates must propagate even while the renderer is small.
			for _, size := range [][2]int{{120, 32}, {144, 40}, {30, 6}} {
				if err := m.nv.ExecLua("local win,w,h=...;vim.api.nvim_win_set_config(win,{width=w,height=h})", nil, v.Window, size[0], size[1]); err != nil {
					t.Fatal(err)
				}
				want[2], want[3] = size[0]+tc.extra[0], size[1]+tc.extra[1]
				v = floatView(t, m, want)
				if err := m.syncFloats([]screen.View{v}); err != nil {
					t.Fatal(err)
				}
				if p.bounds[2] != float64(want[2])/160 || p.bounds[3] != float64(want[3])/45 {
					t.Fatalf("plugin resize lost: %v", p.bounds)
				}
			}
		})
	}
}

func TestFloatRexPlacementDoesNotRewritePluginAnchor(t *testing.T) {
	m, log := floatManager(t)
	if err := m.nv.ExecLua(`
local a=vim.api
local parent=a.nvim_get_current_win()
a.nvim_open_win(a.nvim_create_buf(false,true),false,{
 relative='win',win=parent,anchor='SE',row=20,col=70,width=30,height=6})
`, nil); err != nil {
		t.Fatal(err)
	}
	v := floatView(t, m, [4]int{14, 40, 30, 6})
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	p := m.panes[v.Grid]
	view := rex.View{Windows: []rex.Window{{ID: "window", Layers: []rex.Layer{
		{Blocks: []rex.Block{{ID: "initial"}}},
		{ID: p.layer, Bounds: [4]float64{.1, .1, .5, .5}},
	}}}}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(log), "view.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "session.resize_layer") {
		t.Fatalf("Rex drift was not corrected: %s", calls)
	}
	if err := m.nv.ExecLua(`
local c=vim.api.nvim_win_get_config(...)
assert(c.relative=='win' and c.anchor=='SE','renderer rewrote plugin anchor')
assert(c.row==20 and c.col==70,'renderer rewrote plugin position')
`, nil, v.Window); err != nil {
		t.Fatal(err)
	}
}

func TestFloatRendererResizeRepaintsWithoutNeovimRedraw(t *testing.T) {
	p := &peer{grid: 3, layer: "layer", width: 30, height: 6, frames: make(chan string, 1)}
	m := &manager{state: screen.New(), panes: map[int]*peer{3: p}}
	m.state.Apply(
		[]any{"grid_resize", []any{3, 30, 6}},
		[]any{"win_float_pos", []any{3, 1001, "NW", 1, 2, 8, true, 50}},
		[]any{"grid_line", []any{3, 0, 25, []any{[]any{"RIGHT"}}}},
		[]any{"grid_line", []any{3, 5, 0, []any{[]any{"BOTTOM"}}}},
	)
	for _, size := range [][2]int{{20, 4}, {30, 6}, {1, 1}, {40, 10}} {
		if err := m.handle(incoming{p: p, msg: terminal.Message{Type: "resize", Width: size[0], Height: size[1]}}); err != nil {
			t.Fatal(err)
		}
		select {
		case frame := <-p.frames:
			visible := size[0] >= 30 && size[1] >= 6
			if strings.Contains(frame, "RIGHT") != visible || strings.Contains(frame, "BOTTOM") != visible {
				t.Fatalf("terminal %v did not clip/reveal the original grid: %q", size, frame)
			}
		default:
			t.Fatal("terminal resize did not repaint")
		}
		if w, h := m.state.GridSize(p.grid); w != 30 || h != 6 {
			t.Fatalf("terminal resized source grid: %dx%d", w, h)
		}
	}
}

func TestFloatRootResizePreservesNeovimCellGeometry(t *testing.T) {
	m, _ := floatManager(t)
	if err := m.nv.ExecLua("vim.api.nvim_open_win(vim.api.nvim_create_buf(false,true),false,{relative='editor',row=2,col=8,width=30,height=6})", nil); err != nil {
		t.Fatal(err)
	}
	v := floatView(t, m, [4]int{2, 8, 30, 6})
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	m.rootW, m.rootH = 200, 50
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	if got, want := m.panes[v.Grid].bounds, [4]float64{.04, .04, .15, .12}; got != want {
		t.Fatalf("bounds = %v, want %v", got, want)
	}
}
