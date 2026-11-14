package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
)

func TestNeovimExternalTablineLifecycleAndMeasurement(t *testing.T) {
	m, _ := floatManager(t)
	if err := m.nv.DetachUI(); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.state.SetExternalTabline(true)
	m.mu.Unlock()
	if err := m.nv.ExecLua(configure, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.nv.AttachUI(160, 45, screen.UIOptions(true)); err != nil {
		t.Fatal(err)
	}
	lua := func(code string) {
		t.Helper()
		if err := m.nv.ExecLua(code, nil); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() screen.Tabline { m.mu.Lock(); defer m.mu.Unlock(); return m.state.Tabline() }
	wait := func(check func(screen.Tabline) bool) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
			lua("vim.cmd('redraw!')")
			if check(snapshot()) {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("tabline did not converge: %+v", snapshot())
	}
	lua("vim.o.updatecount=0;vim.o.mouse='a';vim.o.showtabline=2;vim.api.nvim_buf_set_name(0,'first');FirstTab=vim.api.nvim_get_current_tabpage();FirstBuf=vim.api.nvim_get_current_buf();vim.cmd('vsplit');vim.cmd('tabnew');vim.api.nvim_buf_set_name(0,'second');SecondBuf=vim.api.nvim_get_current_buf()")
	wait(func(s screen.Tabline) bool {
		return len(s.Tabs) == 2 && len(s.Buffers) == 2 && strings.Contains(s.Tabs[1].Name, "second")
	})
	initial := snapshot()
	if initial.CurrentTab != initial.Tabs[1].Handle || initial.CurrentBuffer != initial.Buffers[1].Handle {
		t.Fatal("active handles out of sync")
	}
	if err := m.selectNavigation(screen.TablineTarget{Handle: initial.Tabs[0].Handle}); err != nil {
		t.Fatal(err)
	}
	wait(func(s screen.Tabline) bool { return s.CurrentTab == initial.Tabs[0].Handle })
	// Only the selected tab's two windows may enter pane topology.
	var layoutWindows []int
	lua("assert(vim.api.nvim_get_current_tabpage()==FirstTab)")
	if err := m.nv.ExecLua("return vim.api.nvim_tabpage_list_wins(0)", &layoutWindows); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); ; {
		visible := 0
		for _, v := range m.views() {
			if !v.Hidden && !v.Float && !v.External {
				visible++
			}
		}
		if visible == len(layoutWindows) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("inactive-tab grids visible: %+v", m.views())
		}
		time.Sleep(time.Millisecond)
	}
	node, err := m.nativeLayout()
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != "row" || len(node.Children) != 2 {
		t.Fatalf("wrong active topology: %+v", node)
	}
	// Buffer selection uses buffer handles, not similarly numbered tab handles.
	if err := m.selectNavigation(screen.TablineTarget{Handle: initial.Buffers[1].Handle, Buffer: true}); err != nil {
		t.Fatal(err)
	}
	lua("assert(vim.api.nvim_get_current_tabpage()==FirstTab);vim.api.nvim_buf_set_name(0,'renamed')")
	wait(func(s screen.Tabline) bool {
		for _, b := range s.Buffers {
			if b.Handle == initial.Buffers[1].Handle && strings.Contains(b.Name, "renamed") {
				return true
			}
		}
		return false
	})
	// Selection must not force-abandon a modified buffer.
	lua("vim.o.hidden=false;Protected=vim.api.nvim_create_buf(true,false);vim.api.nvim_set_current_buf(Protected);vim.api.nvim_buf_set_lines(0,0,-1,false,{'unsaved'})")
	if err := m.selectNavigation(screen.TablineTarget{Handle: initial.Buffers[0].Handle, Buffer: true}); err == nil {
		t.Fatal("modified buffer abandoned")
	}
	lua("assert(vim.api.nvim_get_current_buf()==Protected);vim.bo.modified=false;vim.api.nvim_set_current_buf(FirstBuf);vim.api.nvim_buf_delete(Protected,{})")
	// Real mouse delivery uses the input pump and the pane's immutable route.
	m.keys = make(chan incoming, 16)
	go m.sendKeys()
	var p *peer
	for _, v := range m.views() {
		if !v.Hidden && !v.Float && !v.External {
			p = &peer{grid: v.Grid, width: 80, height: 20}
			m.routePeer(p)
			break
		}
	}
	if p == nil {
		t.Fatal("no active renderer")
	}
	m.keys <- incoming{p: p, msg: terminal.Message{Type: "mouse", Button: "left", Action: "press", Row: 18, Col: 79}}
	wait(func(s screen.Tabline) bool { return s.CurrentTab == initial.Tabs[1].Handle })
	lua("vim.cmd('tabclose');vim.api.nvim_set_current_buf(FirstBuf);vim.cmd('bdelete '..SecondBuf)")
	wait(func(s screen.Tabline) bool { return len(s.Tabs) == 1 && len(s.Buffers) == 1 })
	// Closing the second tab restores exactly the first tab's split topology.
	for _, mode := range []int{0, 1, 2, 0, 2} {
		if err := m.nv.SetOption("showtabline", mode); err != nil {
			t.Fatal(err)
		}
		wait(func(s screen.Tabline) bool {
			m.mu.Lock()
			defer m.mu.Unlock()
			for _, v := range m.state.Views() {
				if !v.Hidden && !v.Float && !v.External {
					want := 0
					if mode == 2 {
						want = 2
					}
					return m.state.TablineRows(v.Grid, 20) == want
				}
			}
			return false
		})
		var sizes [][]int
		m.panes = map[int]*peer{}
		for _, v := range m.views() {
			if !v.Hidden && !v.Float && !v.External {
				m.panes[v.Grid] = &peer{grid: v.Grid, width: 80, height: 20}
				h := 20
				if mode == 2 {
					h -= 2
				}
				sizes = append(sizes, []int{v.Window, 80, h})
			}
		}
		if len(sizes) != 2 {
			t.Fatalf("inactive-tab pane leak: %+v", m.views())
		}
		var measured []int
		if err := m.nv.ExecLua(measureLayout, &measured, sizes); err != nil {
			t.Fatal(err)
		}
		want := 21
		if mode == 2 {
			want = 19
		}
		if measured[1] != want {
			t.Fatalf("duplicated root tabline mode=%d size=%v", mode, measured)
		}
		if err := m.fit(); err != nil {
			t.Fatal(err)
		}
		if m.rootH != want {
			t.Fatalf("fit ignored chrome: %d != %d", m.rootH, want)
		}
	}
	// A stale click after a close is rejected by Neovim, not retargeted by index.
	if err := m.selectNavigation(screen.TablineTarget{Handle: initial.Tabs[1].Handle}); err == nil {
		t.Fatal("closed handle selected")
	}
}
