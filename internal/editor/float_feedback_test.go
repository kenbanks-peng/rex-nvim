package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/neovim/go-client/nvim"
)

func TestFloatResizeFeedbackPreservesRequestedBounds(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "rex")
	script := `#!/bin/sh
case "$*" in
 *session.view*) echo '{"windows":[{"window_id":"window","layers":[{"layout":{"block_id":"initial","rect":[0.25,0.3,0.4,0.5]},"blocks":[{"block_id":"initial"}]}]}]}' ;;
 *session.new_layer*) echo '{"block_ids":["float"],"layer_id":"layer"}' ;;
 *) echo '{}' ;;
esac
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	m := &manager{ctx: context.Background(), rex: &rex.Client{Session: "session", Executable: executable}, initial: "initial", rootW: 80, rootH: 24, panes: map[int]*peer{2: {grid: 2, block: "initial"}}}
	v := screen.View{Grid: 3, Window: 1001, Row: 2, Col: 5, Width: 30, Height: 6, Float: true, Focusable: false}
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	p := m.panes[3]
	bounds := p.bounds
	if want := [4]float64{5.0 / 80, 2.0 / 24, 30.0 / 80, 6.0 / 24}; bounds != want {
		t.Fatalf("float constrained to tiled bounds: got %v, want %v", bounds, want)
	}
	p.floatResizes = []floatResize{{size: [2]int{20, 4}}}
	v.Width, v.Height, v.ResizeID = 20, 4, 1
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	if p.bounds != bounds || p.floatSize != [2]int{30, 6} || len(p.floatResizes) != 0 {
		t.Fatalf("feedback replaced requested geometry: %+v", p)
	}
	m.rootW = 100
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	if p.bounds[2] != .3 || p.bounds[3] != .25 {
		t.Fatalf("root resize lost requested size: %v", p.bounds)
	}
	v.Width, v.Height = 25, 5
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	if p.floatSize != [2]int{25, 5} || p.bounds[2] != .25 {
		t.Fatalf("plugin resize ignored: %+v", p)
	}
}

func TestBorderedFloatFeedbackPreservesFullScreenRequest(t *testing.T) {
	for _, tc := range []struct {
		name, border string
		split        bool
		extra        [2]int
	}{
		{"rounded", "'rounded'", false, [2]int{2, 2}},
		{"rounded_split", "'rounded'", true, [2]int{2, 2}},
		{"bottom_right", "{'','','','│','╯','─','',''}", false, [2]int{1, 1}},
		{"borderless", "'none'", true, [2]int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := exec.LookPath("nvim"); err != nil {
				t.Skip("nvim unavailable")
			}
			executable := filepath.Join(t.TempDir(), "rex")
			script := `#!/bin/sh
case "$*" in
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
			if err := nv.RegisterHandler("redraw", func(u ...[]any) { m.mu.Lock(); defer m.mu.Unlock(); m.state.Apply(u...) }); err != nil {
				t.Fatal(err)
			}
			go nv.Serve()
			if err := nv.AttachUI(160, 45, map[string]any{"rgb": true, "ext_multigrid": true}); err != nil {
				t.Fatal(err)
			}
			if tc.split {
				if err := nv.Command("vsplit"); err != nil {
					t.Fatal(err)
				}
			}
			if err := nv.ExecLua("vim.api.nvim_open_win(vim.api.nvim_create_buf(false,true),true,{relative='editor',row=2,col=8,width=144,height=40,border="+tc.border+"})", nil); err != nil {
				t.Fatal(err)
			}
			requested := [2]int{144 + tc.extra[0], 40 + tc.extra[1]}
			popup := func(want [2]int) screen.View {
				t.Helper()
				if err := nv.Command("redraw"); err != nil {
					t.Fatal(err)
				}
				var got screen.View
				for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
					for _, v := range m.views() {
						if v.Float && !v.Hidden {
							got = v
							if v.Width == want[0] && v.Height == want[1] {
								return v
							}
						}
					}
					time.Sleep(time.Millisecond)
				}
				t.Fatalf("popup = %+v, want size %v", got, want)
				return screen.View{}
			}
			initial := popup(requested)
			if err := m.syncFloats([]screen.View{initial}); err != nil {
				t.Fatal(err)
			}
			p := m.panes[initial.Grid]
			bounds := p.bounds
			wantBounds := [4]float64{8.0 / 160, 2.0 / 45, float64(requested[0]) / 160, float64(requested[1]) / 45}
			if bounds != wantBounds {
				t.Fatalf("initial bounds = %v, want %v", bounds, wantBounds)
			}
			for _, size := range [][2]int{{80, 24}, requested, {80, 24}, requested} {
				p.width, p.height = size[0], size[1]
				if err := m.fitFloat(p); err != nil {
					t.Fatal(err)
				}
				got := popup(size)
				if got.Width != size[0] || got.Height != size[1] {
					t.Fatalf("terminal %v: grid %dx%d", size, got.Width, got.Height)
				}
				if err := m.syncFloats([]screen.View{got}); err != nil {
					t.Fatal(err)
				}
				if p.bounds != bounds || p.floatSize != requested {
					t.Fatalf("terminal %v replaced request: bounds=%v want=%v, size=%v want=%v", size, p.bounds, bounds, p.floatSize, requested)
				}
			}
		})
	}
}
