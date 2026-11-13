package editor

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/neovim/go-client/nvim"
)

func TestPopupsStayWithinOriginatingPane(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	for _, split := range []string{"vsplit", "rightbelow vsplit", "split", "belowright split"} {
		for _, anchor := range []string{"NW", "NE", "SW", "SE"} {
			t.Run(split+"/"+anchor, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessServe(false), nvim.ChildProcessArgs("--embed", "--headless", "--clean"))
				if err != nil {
					t.Fatal(err)
				}
				defer nv.Close()
				if err := nv.RegisterHandler("redraw", func(...[]any) {}); err != nil {
					t.Fatal(err)
				}
				go nv.Serve()
				lua := func(code string, args ...any) {
					t.Helper()
					if err := nv.ExecLua(code, nil, args...); err != nil {
						t.Fatal(err)
					}
				}
				lua("_G.rex_raw_open=vim.api.nvim_open_win")
				lua(configure)
				if err := nv.AttachUI(120, 40, map[string]any{"ext_multigrid": true}); err != nil {
					t.Fatal(err)
				}
				lua(`
local split,anchor=...
vim.cmd(split)
local a=vim.api
local owner=a.nvim_get_current_win()
local south=anchor:sub(1,1)=='S'
local east=anchor:sub(2,2)=='E'
local config={relative='editor',width=88,height=28,border='single',anchor=anchor,
 row=(vim.o.lines+(south and 30 or -30))/2,
 col=(vim.o.columns+(east and 90 or -90))/2}
local popup=a.nvim_open_win(a.nvim_create_buf(false,true),true,config)
local child=a.nvim_open_win(a.nvim_create_buf(false,true),false,{
 relative='win',win=popup,row=1,col=2,width=84,height=26,focusable=false})
_G.rex_test={owner=owner,popup=popup,child=child,config=config}
`, split, anchor)
				check := `
local a=vim.api
local t=rex_test
local function inside(win,parent,border)
 local p,q=a.nvim_win_get_position(win),a.nvim_win_get_position(parent)
 local c=a.nvim_win_get_config(win)
 assert(c.relative=='win' and c.win==parent,'popup lost its pane')
 local row,col=p[1]-q[1],p[2]-q[2]
 -- A bordered parent reports its outer origin; children start in its content.
 local inset=parent==t.popup and 1 or 0
 assert(row>=inset and col>=inset,'popup starts outside its pane')
 assert(row+a.nvim_win_get_height(win)+border<=a.nvim_win_get_height(parent)+inset,'popup exceeds pane height')
 assert(col+a.nvim_win_get_width(win)+border<=a.nvim_win_get_width(parent)+inset,'popup exceeds pane width')
end
inside(t.popup,t.owner,2)
inside(t.child,t.popup,0)
`
				lua(check)
				lua(`
local a=vim.api
local p,q=a.nvim_win_get_position(rex_test.popup),a.nvim_win_get_position(rex_test.owner)
assert(math.abs(2*(p[1]-q[1])+a.nvim_win_get_height(rex_test.popup)+2-a.nvim_win_get_height(rex_test.owner))<=1,'popup not vertically centered in its pane')
assert(math.abs(2*(p[2]-q[2])+a.nvim_win_get_width(rex_test.popup)+2-a.nvim_win_get_width(rex_test.owner))<=1,'popup not horizontally centered in its pane')
`)
				lua(`
local a=vim.api
for _,win in ipairs(a.nvim_tabpage_list_wins(0)) do
 if a.nvim_win_get_config(win).relative=='' and win~=rex_test.owner then a.nvim_set_current_win(win);break end
end
a.nvim_win_set_config(rex_test.popup,{width=100})
a.nvim_win_set_config(rex_test.child,{height=30})
local nested=a.nvim_open_win(a.nvim_create_buf(false,true),true,{
 relative='win',win=rex_test.popup,row=0,col=0,width=10,height=2})
local overlay=a.nvim_open_win(a.nvim_create_buf(false,true),false,{
 relative='editor',row=0,col=0,width=10,height=2})
assert(a.nvim_win_get_config(overlay).win==rex_test.owner,'nested popup changed owners')
a.nvim_win_close(overlay,true)
a.nvim_win_close(nested,true)
`)
				lua(check)
				lua(`
vim.api.nvim_set_current_win(rex_test.owner)
vim.api.nvim_win_set_width(rex_test.owner,30)
vim.api.nvim_win_set_height(rex_test.owner,10)
vim.cmd('redraw!')
`)
				lua(check)
				lua(`
vim.api.nvim_win_set_config(rex_test.popup,{relative='editor',width=100,height=30,row=0,col=0})
vim.api.nvim_win_set_config(rex_test.child,{relative='win',win=rex_test.popup,row=1,col=2,width=90,height=28})
`)
				lua(check)
				lua(`
local a=vim.api
a.nvim_win_set_width(rex_test.owner,60)
a.nvim_win_set_height(rex_test.owner,20)
vim.cmd('redraw!')
`)
				lua(`
local a=vim.api
assert(a.nvim_win_get_width(rex_test.popup)==math.min(100,a.nvim_win_get_width(rex_test.owner)-2),'popup did not grow with its pane')
assert(a.nvim_win_get_height(rex_test.popup)==math.min(30,a.nvim_win_get_height(rex_test.owner)-2),'popup height did not recover')
`)
				lua(check)
				lua(`
local a=vim.api
local raw=rex_raw_open(a.nvim_create_buf(false,true),false,{
 relative='editor',row=0,col=0,width=10,height=2})
a.nvim_win_set_config(raw,{zindex=222})
assert(a.nvim_win_get_config(raw).relative=='editor','unmanaged popup changed on an options-only update')
a.nvim_win_set_config(raw,{relative='editor',row=0,col=0})
assert(a.nvim_win_get_config(raw).win==rex_test.owner,'partial popup configuration lost its pane')
a.nvim_win_close(raw,true)
`)
			})
		}
	}
}
