package screen

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neovim/go-client/nvim"
)

func TestNeovimExternalCmdline(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
	if err != nil {
		t.Fatal(err)
	}
	defer nv.Close()
	s := New()
	s.SetExternalCmdline(true)
	s.SetExternalPopupmenu(true)
	var mu sync.Mutex
	events := map[string]int{}
	aborted := false
	nv.RegisterHandler("redraw", func(updates ...[]any) {
		mu.Lock()
		defer mu.Unlock()
		for _, u := range updates {
			if len(u) == 0 {
				continue
			}
			name, _ := u[0].(string)
			events[name]++
			if name == "cmdline_hide" {
				for _, raw := range u[1:] {
					a := sequence(raw)
					if len(a) > 1 && boolean(a[1]) {
						aborted = true
					}
				}
			}
		}
		s.Apply(updates...)
	})
	go nv.Serve()
	if err := nv.AttachUI(40, 12, UIOptions(false, true, true)); err != nil {
		t.Fatal(err)
	}
	lua := func(code string) {
		t.Helper()
		if err := nv.ExecLua(code, nil); err != nil {
			t.Fatal(err)
		}
	}
	input := func(keys string) {
		t.Helper()
		if _, err := nv.Input(keys); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(label string, check func() bool) {
		t.Helper()
		end := time.Now().Add(3 * time.Second)
		for time.Now().Before(end) {
			mu.Lock()
			ok := check()
			mu.Unlock()
			if ok {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("%s: levels=%+v block=%+v events=%v", label, s.command.levels, s.command.block, events)
	}
	text := func() string { return plain(s.Render(s.commandOwner(), 40, 10)) }
	lua("vim.o.cmdheight=2;vim.cmd('vsplit');vim.cmd('redraw!')")
	input(":echo '界é🙂'")
	wait("colon", func() bool { return strings.Contains(text(), ":echo '界é🙂'") })
	mu.Lock()
	for _, g := range s.grids {
		var raw strings.Builder
		for _, c := range g.cells {
			raw.WriteString(c.text)
		}
		if strings.Contains(raw.String(), "echo '界é🙂'") {
			t.Error("command line duplicated in grid")
		}
	}
	mu.Unlock()
	input("<Left>")
	wait("position", func() bool { return events["cmdline_pos"] > 0 })
	input("<C-r>")
	wait("special", func() bool { return events["cmdline_special_char"] > 0 })
	input("=1+2")
	wait("nested expression", func() bool { return len(s.command.levels) == 2 && strings.Contains(text(), "1+2") })
	input("<Esc>")
	wait("nested cancellation", func() bool { return len(s.command.levels) == 1 && strings.Contains(text(), "echo") })
	if err := nv.Request("nvim_ui_set_focus", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := nv.TryResizeUI(24, 8); err != nil {
		t.Fatal(err)
	}
	if err := nv.Request("nvim_ui_set_focus", nil, true); err != nil {
		t.Fatal(err)
	}
	wait("resize retains prompt", func() bool { w, h := s.GridSize(1); return w == 24 && h == 8 && s.CmdlineActive() })
	input("<C-c>")
	wait("cancel", func() bool { return !s.CmdlineActive() })
	input("/界")
	wait("search", func() bool { return strings.Contains(text(), "/界") })
	input("<C-c>")
	wait("search cancel", func() bool { return !s.CmdlineActive() })
	input(":let g:should_not_execute=1")
	wait("cancelled colon prompt", func() bool { return strings.Contains(text(), "should_not_execute") })
	input("<C-c>")
	wait("colon cancellation", func() bool { return !s.CommandInputActive() })
	lua("assert(vim.fn.exists('g:should_not_execute')==0)")
	// input() blocks a non-fast RPC; fast input remains authoritative.
	result := make(chan error, 1)
	go func() {
		result <- nv.ExecLua("vim.g.answer=vim.fn.input({prompt='问: ',default='界é',highlight=function(s) return {{0,#s,'ErrorMsg'}} end})", nil)
	}()
	wait("input prompt highlights", func() bool {
		c := s.command.levels[1]
		if c == nil || c.prompt != "问: " || !strings.Contains(text(), "界é") {
			return false
		}
		for _, ch := range c.content {
			if ch.hl > 0 {
				return true
			}
		}
		return false
	})
	input("<End><BS>z<CR>")
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("input() deadlocked")
	}
	var answer string
	if err := nv.ExecLua("return vim.g.answer", &answer); err != nil || answer != "界z" {
		t.Fatalf("input edited %q: %v", answer, err)
	}
	wait("input finished", func() bool { return !s.CmdlineActive() })
	go func() { result <- nv.ExecLua("pcall(vim.fn.input,'CANCEL_INPUT: ')", nil) }()
	wait("cancel input prompt", func() bool { return strings.Contains(text(), "CANCEL_INPUT: ") })
	input("<C-c>")
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("cancelled input() deadlocked")
	}
	wait("input cancellation", func() bool { return !s.CommandInputActive() })
	input(":function RNCommandBlock()<CR>")
	wait("block show", func() bool {
		return events["cmdline_block_show"] > 0 && strings.Contains(text(), "function RNCommandBlock()")
	})
	input("echo 'BODY'<CR>")
	wait("block append", func() bool { return events["cmdline_block_append"] > 0 && strings.Contains(text(), "BODY") })
	input("endfunction<CR>")
	wait("block hide", func() bool {
		return len(s.command.block) == 0 && !s.CmdlineActive() && events["cmdline_block_hide"] > 0
	})
	lua("assert(vim.fn.exists('*RNCommandBlock')==1)")
	input(":function RNAbortedBlock()<CR>")
	wait("second block", func() bool { return len(s.command.block) > 0 })
	input("<C-c>")
	wait("block cancellation", func() bool { return !s.CmdlineActive() })
	// Command completion with a -1 anchor cannot silently disappear.
	lua("vim.o.wildoptions='pum';vim.o.wildmode='full'")
	input(":set wild<Tab>")
	wait("command completion", func() bool {
		_, ok := s.PopupGeometry(s.commandOwner(), 40, 10)
		return s.popup.grid == -1 && ok
	})
	input("<C-c><C-c>")
	wait("completion cancel", func() bool { return !s.CmdlineActive() })
	var supportsAbort bool
	// The abort flag was appended in API 14; older supported servers omit it.
	if err := nv.ExecLua("return vim.fn.has('nvim-0.12')==1", &supportsAbort); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if supportsAbort && !aborted {
		t.Error("no abort flag (Neovim 0.12+)")
	}
	mu.Unlock()
}
