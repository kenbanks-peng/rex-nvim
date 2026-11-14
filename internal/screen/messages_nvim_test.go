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

func TestNeovimMessages(t *testing.T) {
	for _, mode := range []string{"external", "message-grid", "all-grid"} {
		t.Run(mode, func(t *testing.T) {
			if _, err := exec.LookPath("nvim"); err != nil {
				t.Skip("nvim unavailable")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
			if err != nil {
				t.Fatal(err)
			}
			defer nv.Close()
			s := New()
			external := mode == "external"
			cmdline := mode != "all-grid"
			s.SetExternalCmdline(cmdline)
			s.SetExternalMessages(external)
			var mu sync.Mutex
			events := map[string]int{}
			nv.RegisterHandler("redraw", func(updates ...[]any) {
				mu.Lock()
				defer mu.Unlock()
				for _, u := range updates {
					if len(u) > 0 {
						if name, ok := u[0].(string); ok {
							events[name]++

						}
					}
				}
				s.Apply(updates...)
			})
			go nv.Serve()
			if err := nv.AttachUI(60, 14, UIOptions(false, false, cmdline, external)); err != nil {
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
			text := func() string { return plain(s.Render(s.commandOwner(), 60, 14)) }
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
				t.Fatalf("%s: text=%q messages=%+v events=%v", label, text(), s.messages, events)
			}
			var localHistory bool
			if err := nv.ExecLua("return vim.fn.has('nvim-0.12')==1", &localHistory); err != nil {
				t.Fatal(err)
			}
			lua("vim.o.hidden=true;vim.o.laststatus=0;vim.o.showmode=true;vim.o.showcmd=true;vim.o.ruler=true;vim.cmd('vsplit');vim.cmd('redraw!')")
			input(":echo 'ECHO_MESSAGE'<CR>")
			wait("echo", func() bool { return strings.Contains(text(), "ECHO_MESSAGE") && !s.CmdlineActive() })
			if external {
				mu.Lock()
				for _, g := range s.grids {
					var raw strings.Builder
					for _, c := range g.cells {
						raw.WriteString(c.text)
					}
					if strings.Contains(raw.String(), "ECHO_MESSAGE") {
						t.Error("external message duplicated in grid")
					}
				}
				mu.Unlock()
				input(":echon '_APPEND'<CR>")
				wait("echon append", func() bool { return strings.Contains(text(), "_APPEND") })
			}
			input(":does_not_exist_RN_KH3K<CR>")
			wait("error", func() bool { return strings.Contains(text(), "E492") })
			input("<Esc>")
			input("i")
			wait("showmode", func() bool { return strings.Contains(text(), "INSERT") })
			input("<Esc>")
			if external {
				wait("showmode clear", func() bool { return len(s.messages.mode) == 0 })
				input("2")
				wait("showcmd", func() bool { return len(s.messages.cmd) > 0 })
				input("<Esc>")
				wait("ruler", func() bool { return events["msg_ruler"] > 0 })
			}
			lua("vim.api.nvim_echo({{'HISTORY_A','ErrorMsg'}},true,{});vim.api.nvim_echo({{'HISTORY_B'}},true,{})")
			input(":messages<CR>")
			wait("history", func() bool { return strings.Contains(text(), "HISTORY_B") })
			if external && localHistory {
				wait("history event", func() bool { return events["msg_history_show"] > 0 && s.messages.historyOpen })
				mu.Lock()
				if !strings.Contains(text(), "HISTORY_A") {
					t.Error("missing earlier history")
				}
				s.MessageInput("<PageUp>")
				s.Render(s.commandOwner(), 24, 7)
				s.MessageInput("<CR>")
				if s.messages.historyOpen {
					t.Error("history not dismissed")
				}
				mu.Unlock()
			} else {
				// Native history remains a Neovim prompt, not a local viewer.
				input("<CR>")
			}
			if external && !localHistory {
				wait("API 13 history prompt dismissed", func() bool { return !s.messages.historyOpen })
				lua("vim.cmd('messages clear')")
				wait("API 13 history clear", func() bool {
					return events["msg_history_clear"] > 0 && len(s.messages.history) == 0
				})
			}
			// Interactive confirmation must remain answerable across resize.
			input(":let g:message_answer=confirm('CONFIRM_MESSAGE', \"&Yes\\n&No\", 2)<CR>")
			wait("confirmation", func() bool { return strings.Contains(text(), "(Y)es") && s.CommandInputActive() })
			if err := nv.TryResizeUI(48, 12); err != nil {
				t.Fatal(err)
			}
			input("y")

			wait("confirmation input processed", func() bool { return !s.CommandInputActive() })
			lua("assert(vim.g.message_answer==1, tostring(vim.g.message_answer))")
			wait("confirmation ends", func() bool { return !s.CommandInputActive() })
			if !external {
				input(":set nomore<CR>:echo join(map(range(1, 20), 'string(v:val)'), \"\\n\")<CR>")
				wait("hit-enter", func() bool { return strings.Contains(text(), "Press ENTER") })
				resize := make(chan error, 1)
				go func() { resize <- nv.TryResizeUI(44, 10) }()
				input("<CR>")
				select {
				case err := <-resize:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("resize did not finish after dismissal")
				}
				wait("hit-enter dismissed", func() bool { return !s.CommandInputActive() })
			}
			input(":let g:message_cmd=input('CMD_AFTER_MESSAGE: ')<CR>")
			wait("message/cmdline interaction", func() bool { return strings.Contains(text(), "CMD_AFTER_MESSAGE:") })
			input("accepted<CR>")
			lua("assert(vim.g.message_cmd=='accepted')")
			wait("input ends", func() bool { return !s.CommandInputActive() })
			if external {
				// Current-version progress IDs replace an earlier visible message even
				// when unrelated messages have subsequently been emitted.
				var modern bool
				if err := nv.ExecLua("return vim.fn.has('nvim-0.12')==1", &modern); err != nil {
					t.Fatal(err)
				}
				if modern {
					lua("vim.g.mid=vim.api.nvim_echo({{'PROGRESS_OLD'}},false,{kind='progress',source='RN-KH3K',title='test',status='running',percent=0});vim.api.nvim_echo({{'UNRELATED'}},false,{});vim.api.nvim_echo({{'PROGRESS_NEW'}},false,{id=vim.g.mid,kind='progress',source='RN-KH3K',title='test',status='success',percent=100})")
					wait("progress replacement", func() bool {
						return strings.Contains(text(), "PROGRESS_NEW") && !strings.Contains(text(), "PROGRESS_OLD")
					})
				}
			}
		})
	}
}
