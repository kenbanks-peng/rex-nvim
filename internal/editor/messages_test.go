package editor

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
	"github.com/neovim/go-client/nvim"
)

func TestMessagePromptsAcrossPaneFocusAndResize(t *testing.T) {
	for _, external := range []bool{true, false} {
		name := "grid"
		if external {
			name = "external"
		}
		t.Run(name, func(t *testing.T) {
			if _, err := exec.LookPath("nvim"); err != nil {
				t.Skip("nvim unavailable")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
			if err != nil {
				t.Fatal(err)
			}
			defer nv.Close()
			m := &manager{ctx: ctx, nv: nv, state: screen.New(), keys: make(chan incoming, 16), wake: make(chan struct{}, 16), paint: make(chan struct{}, 1), panes: map[int]*peer{}}
			m.state.SetExternalCmdline(true)
			m.state.SetExternalMessages(external)
			nv.RegisterHandler("redraw", func(updates ...[]any) { m.mu.Lock(); m.state.Apply(updates...); m.mu.Unlock() })
			go nv.Serve()
			if err := nv.AttachUI(60, 14, screen.UIOptions(false, false, true, external)); err != nil {
				t.Fatal(err)
			}
			if err := nv.ExecLua(configure+"\nvim.cmd('vsplit');vim.cmd('redraw!')", nil); err != nil {
				t.Fatal(err)
			}
			wait := func(label string, check func() bool) {
				t.Helper()
				end := time.Now().Add(3 * time.Second)
				for time.Now().Before(end) {
					m.mu.Lock()
					ok := check()
					m.mu.Unlock()
					if ok {
						return
					}
					time.Sleep(5 * time.Millisecond)
				}
				mode, err := nv.Mode()
				m.mu.Lock()
				frame := m.state.Render(m.state.CursorGrid(), 60, 14)
				m.mu.Unlock()
				t.Fatalf("%s: mode=%+v err=%v frame=%q", label, mode, err, frame)
			}
			wait("two views", func() bool { return len(m.state.Views()) == 2 })
			views := m.views()
			for _, v := range views {
				p := &peer{grid: v.Grid, width: 30, height: 14, frames: make(chan string, 1)}
				p.route.Store(&inputRoute{grid: v.Grid, window: v.Window, width: 30, height: 14})
				m.panes[v.Grid] = p
			}
			window, err := nv.CurrentWindow()
			if err != nil {
				t.Fatal(err)
			}
			var owner int
			wait("owner", func() bool {
				owner = m.state.CursorGrid()
				p := m.panes[owner]
				return p != nil && p.route.Load().window == int(window)
			})
			var sibling *peer
			for grid, p := range m.panes {
				if grid != owner {
					sibling = p
				}
			}
			var localHistory bool
			if err := nv.ExecLua("return vim.fn.has('nvim-0.12')==1", &localHistory); err != nil {
				t.Fatal(err)
			}
			m.activeGrid.Store(int64(owner))
			go m.sendKeys()
			send := func(p *peer, keys string) { m.keys <- incoming{p: p, msg: terminal.Message{Type: "input", Keys: keys}} }
			send(m.panes[owner], ":let g:answer=confirm('PANE_CONFIRM', \"&Yes\\n&No\", 2)<CR>")
			wait("confirmation", func() bool {
				return m.state.CommandInputActive() && strings.Contains(m.state.Render(owner, 40, 12), "(Y)es")
			})
			if err := m.focus(sibling.grid); err != nil {
				t.Fatal(err)
			}
			if m.activeGrid.Load() != int64(owner) {
				t.Fatal("focus changed prompt window")
			}
			if err := nv.TryResizeUI(48, 10); err != nil {
				t.Fatal(err)
			}
			send(sibling, "y")
			wait("prompt dismissed", func() bool { return !m.state.CommandInputActive() })
			if err := nv.ExecLua("assert(vim.g.answer==1)", nil); err != nil {
				t.Fatal(err)
			}
			if external {
				send(m.panes[owner], ":echomsg 'LOCAL_HISTORY'<CR>")
				wait("echo", func() bool {
					return strings.Contains(m.state.Render(owner, 40, 12), "LOCAL_HISTORY") && !m.state.CommandInputActive()
				})
				send(m.panes[owner], ":messages<CR>")
				wait("history presentation", func() bool {
					frame := m.state.Render(owner, 60, 14)
					if localHistory {
						return m.state.CommandInputActive() && strings.Contains(frame, "History:")
					}
					return strings.Contains(frame, "Press ENTER") && !strings.Contains(frame, "History:")
				})
				if err := m.focus(sibling.grid); err != nil {
					t.Fatal(err)
				}
				if localHistory {
					send(sibling, "<PageUp>")
				}
				send(sibling, "<CR>")
				wait("history dismissal", func() bool { return !m.state.CommandInputActive() })
			}
			if !external {
				send(m.panes[owner], ":set nomore<CR>:echo join(map(range(1,20), 'string(v:val)'), \"\\n\")<CR>")
				wait("hit-enter", func() bool { return strings.Contains(m.state.Render(owner, 60, 14), "Press ENTER") })
				if !m.blockingInput() {
					t.Fatal("hit-enter blocking state missing")
				}
				if err := m.focus(sibling.grid); err != nil {
					t.Fatal(err)
				}
				if err := m.fit(); err != nil {
					t.Fatal(err)
				}
				// A resize request already in flight can block behind hit-enter.
				// The manager must paint the prompt while that request is pending.
				resized := make(chan error, 1)
				go func() { resized <- m.uiOperation(func() error { return nv.TryResizeUI(44, 10) }) }()
				m.wake <- struct{}{}
				select {
				case frame := <-m.panes[owner].frames:
					if !strings.Contains(frame, "Press ENTER") {
						t.Fatal("in-flight RPC hid prompt")
					}
				case <-ctx.Done():
					t.Fatal("in-flight RPC stopped painting")
				}
				send(sibling, "<CR>")
				wait("hit-enter dismissal", func() bool { return !strings.Contains(m.state.Render(owner, 60, 14), "Press ENTER") })
			}
			send(sibling, "iAFTER_MESSAGE<Esc>")
			wait("sibling editable", func() bool {
				return m.state.CursorGrid() == sibling.grid && strings.Contains(m.state.Render(sibling.grid, 40, 12), "AFTER_MESSAGE")
			})
		})
	}
}
