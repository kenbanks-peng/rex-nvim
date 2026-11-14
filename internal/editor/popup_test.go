package editor

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/neovim/go-client/nvim"
)

func TestPopupsUseFullEditorCanvas(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	for _, split := range []string{"vsplit", "rightbelow vsplit", "split", "belowright split"} {
		for _, anchor := range []string{"NW", "NE", "SW", "SE"} {
			t.Run(split+"/"+anchor, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessArgs("--embed", "--headless", "--clean"))
				if err != nil {
					t.Fatal(err)
				}
				defer nv.Close()
				nv.RegisterHandler("redraw", func(...[]any) {})
				if err := nv.ExecLua(configure, nil); err != nil {
					t.Fatal(err)
				}
				if err := nv.AttachUI(120, 40, map[string]any{"ext_multigrid": true}); err != nil {
					t.Fatal(err)
				}
				if err := nv.ExecLua(`
local split,anchor=...
vim.cmd(split)
local a=vim.api
local owner=a.nvim_get_current_win()
local south=anchor:sub(1,1)=='S'
local east=anchor:sub(2,2)=='E'
local row=(vim.o.lines+(south and 30 or -30))/2
local col=(vim.o.columns+(east and 90 or -90))/2
local popup=a.nvim_open_win(a.nvim_create_buf(false,true),false,{
 relative='editor',width=88,height=28,border='single',anchor=anchor,row=row,col=col})
local child=a.nvim_open_win(a.nvim_create_buf(false,true),false,{
 relative='win',win=popup,row=1,col=2,width=84,height=26,focusable=false})
local function check()
 local c=a.nvim_win_get_config(popup)
 assert(c.relative=='editor' and c.anchor==anchor,'editor popup was reanchored')
 assert(c.row==row and c.col==col,'editor popup was repositioned')
 assert(a.nvim_win_get_width(popup)==88 and a.nvim_win_get_height(popup)==28,'popup constrained to pane')
 local nested=a.nvim_win_get_config(child)
 assert(nested.relative=='win' and nested.win==popup,'nested anchor changed')
 assert(a.nvim_win_get_width(child)==84 and a.nvim_win_get_height(child)==26,'nested popup constrained')
 assert(not nested.focusable and a.nvim_get_current_win()==owner,'overlay stole focus')
end
check()
a.nvim_win_set_width(owner,30)
a.nvim_win_set_height(owner,10)
vim.cmd('redraw!')
check()
a.nvim_win_set_config(popup,{width=100,height=30})
assert(a.nvim_win_get_width(popup)==100 and a.nvim_win_get_height(popup)==30,'plugin resize constrained')
`, nil, split, anchor); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
