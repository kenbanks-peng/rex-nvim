package screen

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neovim/go-client/nvim"
)

// Exercise every independently selectable presentation, including the messages
// prerequisite, against the server rather than only comparing option maps.
func TestNeovimPresentationConformance(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	for mask := 0; mask < 16; mask++ {
		t.Run(fmt.Sprintf("presentations-%04b", mask), func(t *testing.T) {
			tab, pum, cmd, msg := mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0
			msg = msg && cmd
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
			if err != nil {
				t.Fatal(err)
			}
			defer nv.Close()
			s := New()
			s.SetExternalTabline(tab)
			s.SetExternalPopupmenu(pum)
			s.SetExternalCmdline(cmd)
			s.SetExternalMessages(msg)
			var mu sync.Mutex
			events := map[string]int{}
			flushes := 0
			if err := nv.RegisterHandler("redraw", func(updates ...[]any) {
				mu.Lock()
				defer mu.Unlock()
				for _, u := range updates {
					if len(u) > 0 {
						if name, ok := u[0].(string); ok {
							events[name]++
						}
					}
				}
				if s.Apply(updates...) {
					flushes++
				}
			}); err != nil {
				t.Fatal(err)
			}
			go nv.Serve()
			if err := nv.AttachUI(60, 16, UIOptions(tab, pum, cmd, mask&8 != 0)); err != nil {
				t.Fatal(err)
			}
			lua := func(code string, result any) {
				t.Helper()
				if err := nv.ExecLua(code, result); err != nil {
					t.Fatal(err)
				}
			}
			var ui map[string]any
			lua("return vim.api.nvim_list_uis()[1]", &ui)
			for key, want := range map[string]bool{
				"rgb": true, "ext_linegrid": true, "ext_multigrid": true,
				"ext_tabline": tab, "ext_popupmenu": pum, "ext_cmdline": cmd,
				"ext_messages": msg, "ext_wildmenu": false, "ext_hlstate": false,
				"ext_termcolors": false,
			} {
				got, _ := ui[key].(bool)
				if got != want {
					t.Fatalf("negotiated %s=%v, want %v: %v", key, ui[key], want, ui)
				}
			}
			input := func(keys string) {
				t.Helper()
				if _, err := nv.Input(keys); err != nil {
					t.Fatal(err)
				}
			}
			text := func() string {
				var out strings.Builder
				for _, v := range s.Views() {
					if !v.Hidden {
						out.WriteString(plain(s.Render(v.Grid, 60, 16)))
					}
				}
				return out.String()
			}
			wait := func(label string, check func() bool) {
				t.Helper()
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					mu.Lock()
					ok := flushes > 0 && check()
					mu.Unlock()
					if ok {
						return
					}
					time.Sleep(5 * time.Millisecond)
				}
				mu.Lock()
				defer mu.Unlock()
				t.Fatalf("%s: events=%v text=%q", label, events, text())
			}
			lua("vim.o.laststatus=0;vim.o.hidden=true;vim.o.completeopt='menuone,noselect';vim.api.nvim_buf_set_lines(0,0,-1,false,{'BUFFER_界é🙂'});vim.cmd('vsplit');vim.cmd('redraw!')", nil)
			wait("Unicode buffer in both panes", func() bool { return strings.Count(text(), "BUFFER_界é🙂") == 2 })
			input(":echo 'INTEGRATED_界é🙂'")
			wait("command", func() bool { return strings.Contains(text(), ":echo 'INTEGRATED_界é🙂'") })
			if err := nv.Request("nvim_ui_set_focus", nil, false); err != nil {
				t.Fatal(err)
			}
			if err := nv.TryResizeUI(48, 14); err != nil {
				t.Fatal(err)
			}
			if err := nv.Request("nvim_ui_set_focus", nil, true); err != nil {
				t.Fatal(err)
			}
			wait("resized command", func() bool {
				w, h := s.GridSize(1)
				return w == 48 && h == 14 && strings.Contains(text(), "INTEGRATED_界é🙂")
			})
			input("<CR>")
			wait("command becomes message", func() bool {
				return !s.CommandInputActive() && strings.Contains(text(), "INTEGRATED_界é🙂") && !strings.Contains(text(), ":echo")
			})
			if pum && cmd {
				lua("vim.o.wildoptions='pum';vim.o.wildmode='full'", nil)
				input(":set wild<Tab>")
				wait("command completion after message", func() bool {
					_, visible := s.PopupGeometry(s.commandOwner(), 48, 14)
					return s.popup.grid == -1 && visible && s.CmdlineActive()
				})
				input("<C-c><C-c>")
				wait("command completion cancelled", func() bool { return !s.CmdlineActive() && !s.popup.visible })
			}
			input("i")
			lua("vim.fn.complete(1,{'completeOne','completeTwo'});vim.cmd('redraw!')", nil)
			wait("completion", func() bool { return strings.Contains(text(), "completeOne") })
			input("<C-n><C-y><Esc>")
			wait("accepted completion", func() bool { return !s.popup.visible && strings.Contains(text(), "completeOne") })
			lua("assert(vim.api.nvim_get_current_line():find('completeOne',1,true));vim.cmd('tabnew');vim.api.nvim_buf_set_lines(0,0,-1,false,{'SECOND_TAB'});vim.cmd('redraw!')", nil)
			wait("tab switch hides old grids", func() bool { return strings.Contains(text(), "SECOND_TAB") && !strings.Contains(text(), "BUFFER_界") })
			lua("vim.cmd('tabprevious');vim.cmd('redraw!')", nil)
			wait("tab restores grids", func() bool { return strings.Contains(text(), "BUFFER_界") && !strings.Contains(text(), "SECOND_TAB") })
			mu.Lock()
			for event, enabled := range map[string]bool{
				"tabline_update": tab, "popupmenu_show": pum, "popupmenu_hide": pum,
				"cmdline_show": cmd, "cmdline_hide": cmd, "msg_show": msg,
			} {
				if (events[event] > 0) != enabled {
					t.Errorf("%s count=%d enabled=%v", event, events[event], enabled)
				}
			}
			mu.Unlock()
			if err := nv.DetachUI(); err != nil {
				t.Fatal(err)
			}
			var count int
			lua("return #vim.api.nvim_list_uis()", &count)
			if count != 0 {
				t.Fatalf("UI remains attached: %d", count)
			}
			var modified bool
			lua("return vim.bo.modified", &modified)
			if !modified {
				t.Fatal("detach discarded modified buffer")
			}
		})
	}
}
