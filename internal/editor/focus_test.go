package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
	"github.com/neovim/go-client/nvim"
)

func TestNeovimEditorFocusTransitions(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
	if err != nil {
		t.Fatal(err)
	}
	defer nv.Close()
	nv.RegisterHandler("redraw", func(...[]any) {})
	if err := nv.AttachUI(40, 10, screen.UIOptions()); err != nil {
		t.Fatal(err)
	}
	if err := nv.ExecLua(`
 vim.g.focus_events={}
 vim.api.nvim_create_autocmd({'FocusGained','FocusLost'}, {callback=function(e) local events=vim.g.focus_events; table.insert(events,e.event); vim.g.focus_events=events end})
 vim.cmd('vsplit')
 `, nil); err != nil {
		t.Fatal(err)
	}
	var wins []int
	if err := nv.ExecLua("return vim.api.nvim_list_wins()", &wins); err != nil {
		t.Fatal(err)
	}
	a, b := &peer{grid: 2, block: "a"}, &peer{grid: 3, block: "b"}
	m := &manager{ctx: ctx, nv: nv, state: screen.New(), panes: map[int]*peer{2: a, 3: b}}
	m.state.Apply([]any{"win_pos", []any{2, wins[0], 0, 0, 20, 9}}, []any{"win_pos", []any{3, wins[1], 0, 20, 20, 9}})
	snapshot := filepath.Join(t.TempDir(), "view")
	cli := filepath.Join(t.TempDir(), "rex")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\ncat '"+snapshot+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	m.rex = &rex.Client{Session: "test", Executable: cli}
	view := func(block string) {
		t.Helper()
		if err := os.WriteFile(snapshot, []byte(`{"active_window_id":"w","windows":[{"window_id":"w","focused_block_id":"`+block+`"}]}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	send := func(p *peer, kind string) {
		t.Helper()
		if err := m.handle(incoming{p: p, msg: terminal.Message{Type: kind}}); err != nil {
			t.Fatal(err)
		}
	}
	observe := func(block string) {
		t.Helper()
		if err := m.observeRexFocus(block); err != nil {
			t.Fatal(err)
		}
	}
	flush := func() {
		t.Helper()
		if err := m.flushBlur(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	// Establish a real lost->gained baseline independent of Neovim's initial state.
	observe("other")
	send(a, "focus")
	send(a, "focus")
	send(a, "blur")
	send(b, "focus")
	send(a, "blur") // delayed old-pane loss
	flush()
	view("a")
	send(b, "blur")
	flush() // sibling selected before focus-in arrived
	send(a, "focus")
	view("a")
	send(a, "blur")
	flush()      // actual application loss, same Rex pane
	observe("a") // polling must not undo application loss
	send(a, "focus")
	observe("other") // another tab/pane
	observe("other")
	observe("b")
	var got []string
	if err := nv.ExecLua("return vim.g.focus_events", &got); err != nil {
		t.Fatal(err)
	}
	want := []string{"FocusLost", "FocusGained", "FocusLost", "FocusGained", "FocusLost", "FocusGained"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("focus transitions = %v, want %v", got, want)
	}
}
