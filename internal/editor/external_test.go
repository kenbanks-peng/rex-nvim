package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
)

func externalView(t *testing.T, m *manager, window int, visible bool) screen.View {
	t.Helper()
	var got screen.View
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if err := m.nv.Command("redraw"); err != nil {
			t.Fatal(err)
		}
		for _, v := range m.views() {
			if v.Window == window {
				got = v
				if v.External && v.Hidden != visible {
					return v
				}
			}
		}
		if !visible {
			found := false
			for _, v := range m.views() {
				found = found || v.Window == window && !v.Hidden
			}
			if !found {
				return got
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("external window %d visible=%v: %+v", window, visible, got)
	return got
}

func TestNeovimExternalWindowLifecycle(t *testing.T) {
	m, log := floatManager(t)
	if err := m.nv.ExecLua(configure, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.nv.Command("redraw"); err != nil {
		t.Fatal(err)
	}
	for _, v := range m.views() {
		if !v.Float && !v.External && !v.Hidden {
			m.panes[v.Grid] = &peer{grid: v.Grid, block: "initial", primary: true}
		}
	}
	before, err := m.nativeLayout()
	if err != nil {
		t.Fatal(err)
	}
	var win int
	if err := m.nv.ExecLua("local a=vim.api; return a.nvim_open_win(a.nvim_create_buf(false,true),true,{external=true,width=30,height=6})", &win); err != nil {
		t.Fatal(err)
	}
	v := externalView(t, m, win, true)
	if v.Float || v.Width != 30 || v.Height != 6 {
		t.Fatalf("not a genuine external: %+v", v)
	}
	if err := m.reconcile(); err != nil {
		t.Fatal(err)
	}
	p := m.panes[v.Grid]
	if p == nil || p.layer == "" || p.primary {
		t.Fatalf("external joined tiles: %+v", p)
	}
	if err := m.focus(v.Grid); err != nil {
		t.Fatal(err)
	}
	if err := m.nv.ExecLua("assert(vim.api.nvim_get_current_win()==...)", nil, win); err != nil {
		t.Fatal(err)
	}
	renderer := &peer{grid: v.Grid, frames: make(chan string, 1)}
	if err := m.handle(incoming{p: renderer, msg: terminal.Message{Type: "hello", Block: p.block, Width: 42, Height: 9}}); err != nil {
		t.Fatal(err)
	}
	p = renderer
	for _, size := range [][2]int{{42, 9}, {19, 4}, {51, 12}} {
		if err := m.handle(incoming{p: p, msg: terminal.Message{Type: "resize", Width: size[0], Height: size[1]}}); err != nil {
			t.Fatal(err)
		}
		if err := m.nv.ExecLua("local w,x,y=...; assert(vim.api.nvim_win_get_width(w)==x and vim.api.nvim_win_get_height(w)==y)", nil, win, size[0], size[1]); err != nil {
			t.Fatal(err)
		}
		v = externalView(t, m, win, true)
		for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
			v = externalView(t, m, win, true)
			if v.Width == size[0] && v.Height == size[1] {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if v.Width != size[0] || v.Height != size[1] {
			t.Fatalf("per-grid resize did not reach redraw state: %+v, want %v", v, size)
		}
	}
	after, err := m.nativeLayout()
	if err != nil || !sameDimensions(before, after) {
		t.Fatalf("external resize changed tiles: %+v %+v %v", before, after, err)
	}
	if m.rootW != 160 || m.rootH != 45 {
		t.Fatal("external resized root UI")
	}

	// Floats anchored to this independent canvas stay inside its renderer.
	var child int
	if err := m.nv.ExecLua("local a=vim.api; return a.nvim_open_win(a.nvim_create_buf(false,true),false,{relative='win',win=...,row=1,col=2,width=8,height=2})", &child, win); err != nil {
		t.Fatal(err)
	}
	if err := m.nv.Command("redraw"); err != nil {
		t.Fatal(err)
	}
	found := false
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline) && !found; {
		for _, f := range m.views() {
			if f.Window == child {
				found = f.Float && f.ExternalRoot == v.Grid
				if err := m.syncFloats([]screen.View{f}); err != nil {
					t.Fatal(err)
				}
				if m.panes[f.Grid] != nil {
					t.Fatal("external child escaped into root Rex space")
				}
			}
		}
		time.Sleep(time.Millisecond)
	}
	if !found {
		t.Fatalf("external child placement lost: child=%d external=%+v views=%+v", child, v, m.views())
	}
	if err := m.nv.ExecLua("vim.api.nvim_win_close(...,true)", nil, child); err != nil {
		t.Fatal(err)
	}

	// Recovery retains the host's moved/resized geometry, not initial bounds.
	bounds := [4]float64{.1, .2, .6, .5}
	raw, _ := json.Marshal(rex.View{Windows: []rex.Window{{ID: "window", Layers: []rex.Layer{
		{Blocks: []rex.Block{{ID: "initial"}}},
		{ID: p.layer, Bounds: bounds, Blocks: []rex.Block{{ID: p.block}}},
	}}}})
	if err := os.WriteFile(filepath.Join(filepath.Dir(log), "view.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.transportLost(p); err != nil {
		t.Fatal(err)
	}
	p = m.panes[v.Grid]
	if p == nil || p.bounds != bounds {
		t.Fatalf("recovery lost external geometry: %+v", p)
	}
	if err := m.nv.ExecLua("assert(vim.api.nvim_win_is_valid(...))", nil, win); err != nil {
		t.Fatal(err)
	}

	// Intentional host close must not discard a modified, non-hidden buffer.
	if err := m.nv.ExecLua("local a=vim.api; local b=a.nvim_win_get_buf(...); vim.bo[b].buftype=''; vim.bo[b].bufhidden=''; a.nvim_buf_set_lines(b,0,-1,false,{'unsaved'}); vim.o.hidden=false", nil, win); err != nil {
		t.Fatal(err)
	}
	if err := m.disconnected(p); err != nil {
		t.Fatal(err)
	}
	if err := m.nv.ExecLua("local w=...; assert(vim.api.nvim_win_is_valid(w)); assert(vim.bo[vim.api.nvim_win_get_buf(w)].modified)", nil, win); err != nil {
		t.Fatal(err)
	}
	if err := m.reconcile(); err != nil {
		t.Fatal(err)
	}
	if m.panes[v.Grid] == nil {
		t.Fatal("rejected close did not recreate layer")
	}
	if err := m.nv.ExecLua("vim.bo[vim.api.nvim_win_get_buf(...)].modified=false", nil, win); err != nil {
		t.Fatal(err)
	}

	// Hiding and reopening the same window must release/recreate only its layer.
	if err := m.nv.ExecLua("vim.api.nvim_win_hide(...)", nil, win); err != nil {
		t.Fatal(err)
	}
	externalView(t, m, win, false)
	if err := m.reconcile(); err != nil {
		t.Fatal(err)
	}
	if m.panes[v.Grid] != nil {
		t.Fatal("hidden external retained layer")
	}
	var reopened int
	if err := m.nv.ExecLua("local a=vim.api; return a.nvim_open_win(a.nvim_create_buf(false,true),true,{external=true,width=20,height=5})", &reopened); err != nil {
		t.Fatal(err)
	}
	v = externalView(t, m, reopened, true)
	if err := m.reconcile(); err != nil {
		t.Fatal(err)
	}
	if err := m.nv.ExecLua("vim.api.nvim_win_close(...,false)", nil, reopened); err != nil {
		t.Fatal(err)
	}
	externalView(t, m, reopened, false)
	if err := m.reconcile(); err != nil {
		t.Fatal(err)
	}
	if m.panes[v.Grid] != nil {
		t.Fatal("closed external retained layer")
	}
	after, err = m.nativeLayout()
	if err != nil || !sameDimensions(before, after) {
		t.Fatal("lifecycle corrupted tiled layout")
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), "session.move") || strings.Contains(string(calls), "block.split") {
		t.Fatalf("external touched tiled topology: %s", calls)
	}
}
