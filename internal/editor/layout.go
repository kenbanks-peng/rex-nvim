package editor

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
)

func (m *manager) command(grid int) []string {
	return []string{m.executable, "_pane", m.socket, fmt.Sprint(grid)}
}

func (m *manager) reconcile() error {
	mode, err := m.nv.Mode()
	if err != nil {
		return err
	}
	if mode.Blocking {
		return nil
	}
	views := m.views()
	live := map[int]screen.View{}
	for _, v := range views {
		if !v.Hidden {
			live[v.Grid] = v
		}
	}
	// Keep the invoking terminal alive across :tabnext and closing its window.
	var primary *peer
	for _, p := range m.panes {
		if p.primary {
			primary = p
			break
		}
	}
	if primary != nil {
		if _, ok := live[primary.grid]; !ok {
			for _, v := range views {
				if v.Hidden || v.Float || v.External {
					continue
				}
				if existing := m.panes[v.Grid]; existing != nil && !existing.primary {
					m.closePane(existing)
					delete(m.panes, v.Grid)
				}
				delete(m.panes, primary.grid)
				primary.grid = v.Grid
				primary.last = ""
				m.panes[v.Grid] = primary
				m.routePeer(primary)
				break
			}
		}
	}
	for grid, p := range m.panes {
		if _, ok := live[grid]; ok || p.primary || grid == 0 {
			continue
		}
		m.closePane(p)
		delete(m.panes, grid)
	}
	// Neovim lists grids in deterministic screen order.
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].Float != views[j].Float {
			return !views[i].Float
		}
		if views[i].Row != views[j].Row {
			return views[i].Row < views[j].Row
		}
		return views[i].Col < views[j].Col
	})
	for _, v := range views {
		if v.Hidden || m.panes[v.Grid] != nil {
			continue
		}
		if v.Float || v.External {
			continue
		}
		if len(m.panes) == 0 || (len(m.panes) == 1 && m.panes[0] != nil) {
			p := m.panes[0]
			if p == nil {
				p = &peer{primary: true, block: m.initial}
			}
			delete(m.panes, 0)
			p.grid = v.Grid
			m.panes[v.Grid] = p
			m.routePeer(p)
			continue
		}
		anchor, direction, ratio := placement(v, views, m.panes)
		if anchor == nil {
			continue
		}
		block, err := m.rex.Split(m.ctx, anchor.block, direction, ratio, m.command(v.Grid))
		if err != nil {
			return err
		}
		m.panes[v.Grid] = &peer{grid: v.Grid, block: block}
	}
	if err := m.syncExternals(views); err != nil {
		return err
	}
	if err := m.syncFloats(views); err != nil {
		return err
	}
	for _, p := range m.panes {
		m.routePeer(p)
	}
	m.mu.Lock()
	cursor := m.state.CursorGrid()
	m.mu.Unlock()
	if cursor != m.lastFocus && (!m.uiFocusKnown || m.uiFocused) {
		if p := m.panes[cursor]; p != nil && p.block != "" {
			if err := m.rex.Focus(m.ctx, p.block); err != nil {
				// The block may have just been closed in Rex. The next snapshot reconciles it.
				return nil
			}
			m.lastFocus = cursor
			m.activeGrid.Store(int64(cursor))
		}
	}
	if err := m.syncTopology(); err != nil {
		return err
	}
	return m.fit()
}

func (m *manager) closePane(p *peer) {
	if p.conn != nil {
		p.conn.Close()
	}
	if p.layer != "" {
		_ = m.rex.CloseLayer(m.ctx, p.layer)
	} else {
		_ = m.rex.Close(m.ctx, p.block)
	}
}
func placement(v screen.View, views []screen.View, panes map[int]*peer) (*peer, string, float64) {
	var best *peer
	direction := "right"
	ratio := 0.5
	distance := math.MaxFloat64
	for _, a := range views {
		p := panes[a.Grid]
		if p == nil || a.Float || a.External || a.Hidden || a.Grid == v.Grid {
			continue
		}
		dx, dy := v.Col-a.Col, v.Row-a.Row
		score := math.Abs(float64(dx)) + math.Abs(float64(dy))
		dir := "right"
		r := float64(v.Width) / float64(max(1, v.Width+a.Width))
		if v.Col+v.Width <= a.Col {
			dir = "left"
		} else if a.Col+a.Width <= v.Col {
			dir = "right"
		} else {
			r = float64(v.Height) / float64(max(1, v.Height+a.Height))
			if dy < 0 {
				dir = "above"
			} else {
				dir = "below"
			}
		}
		if score < distance {
			best = p
			direction = dir
			ratio = r
			distance = score
		}
	}
	return best, direction, math.Max(.01, math.Min(.99, ratio))
}

func (m *manager) syncRex() error {
	view, err := m.rex.View(m.ctx)
	if err != nil {
		if strings.Contains(err.Error(), "session_not_found") || (strings.Contains(err.Error(), "session target") && strings.Contains(err.Error(), "not found")) {
			return errNoPanes
		}
		return err
	}
	present := map[string]bool{}
	focused := ""
	for _, w := range view.Windows {
		for _, l := range w.Layers {
			for _, b := range l.Blocks {
				present[b.ID] = true
			}
		}
		if w.ID == view.ActiveWindowID {
			focused = w.Focused
		}
	}
	if err := m.observeRexFocus(focused); err != nil {
		return err
	}
	for grid, p := range m.panes {
		if !present[p.block] {
			if err := m.disconnected(p); err != nil {
				return err
			}
			continue
		}
		if p.broken {
			if err := m.replaceRenderer(p); err != nil {
				return err
			}
			continue
		}
		if p.block == focused && grid != m.lastFocus {
			if err = m.focus(grid); err != nil {
				return err
			}
		}
	}
	return m.reconcile()
}

const measureLayout = `
local sizes = ...
local bywin = {}
for _,s in ipairs(sizes) do bywin[s[1]] = s end
local function measure(node)
 if node[1] == 'leaf' then
  local s=bywin[node[2]]
  if s then return s[2],s[3] end
  return vim.api.nvim_win_get_width(node[2]),vim.api.nvim_win_get_height(node[2])+1
 end
 local w,h=0,0
 for i,child in ipairs(node[2]) do
  local cw,ch=measure(child)
  if node[1]=='row' then w=w+cw+(i>1 and 1 or 0);h=math.max(h,ch)
  else w=math.max(w,cw);h=h+ch end
 end
 return w,h
end
local w,h=measure(vim.fn.winlayout())
local tabline = (vim.o.showtabline==2 or (vim.o.showtabline==1 and #vim.api.nvim_list_tabpages()>1)) and 1 or 0
for _,ui in ipairs(vim.api.nvim_list_uis()) do if ui.ext_tabline then tabline=0;break end end
return {w,h+vim.g.rex_nvim_cmdheight+tabline,vim.g.rex_nvim_cmdheight,vim.o.laststatus,#vim.api.nvim_list_tabpages()}
`

func (m *manager) fit() error {
	m.mu.Lock()
	gridCommand := m.state.GridCommandActive()
	m.mu.Unlock()
	// Native window fitting clears Neovim's grid command line during entry.
	// Keep clipping the existing grids until entry ends, then fit on that redraw.
	if gridCommand || m.blockingInput() {
		return nil
	}
	var sizes [][]int
	for _, v := range m.views() {
		p := m.panes[v.Grid]
		if p == nil || p.width < 1 || p.height < 1 || v.Hidden || v.Float || v.External {
			continue
		}
		m.mu.Lock()
		rows := m.state.TablineRows(v.Grid, p.height)
		m.mu.Unlock()
		sizes = append(sizes, []int{v.Window, p.width, p.height - rows})
	}
	if len(sizes) == 0 {
		return nil
	}
	var size []int
	if err := m.uiOperation(func() error { return m.nv.ExecLua(measureLayout, &size, sizes) }); err != nil {
		return err
	}
	if len(size) != 5 {
		return fmt.Errorf("invalid Neovim layout dimensions")
	}
	m.mu.Lock()
	m.state.SetCommandHeight(size[2])
	m.state.SetLastStatus(size[3])
	m.state.SetTabCount(size[4])
	m.mu.Unlock()
	w, h := max(12, size[0]), max(3, size[1])
	if w != m.rootW || h != m.rootH {
		if err := m.uiOperation(func() error { return m.nv.TryResizeUI(w, h) }); err != nil {
			return err
		}
		m.rootW, m.rootH = w, h
	}
	if err := m.uiOperation(func() error {
		return m.nv.ExecLua(`
 vim.g.rex_nvim_fitting = true
 for _,s in ipairs(...) do
  if vim.api.nvim_win_is_valid(s[1]) then
   if vim.api.nvim_win_get_width(s[1])~=s[2] then pcall(vim.api.nvim_win_set_width,s[1],s[2]) end
   local h=math.max(1,s[3]-(vim.o.laststatus==0 and 0 or 1))
   if vim.api.nvim_win_get_height(s[1])~=h then pcall(vim.api.nvim_win_set_height,s[1],h) end
  end
 end
 if vim.o.cmdheight ~= vim.g.rex_nvim_cmdheight then vim.o.cmdheight = vim.g.rex_nvim_cmdheight end
 vim.g.rex_nvim_fitting = false
 `, nil, sizes)
	}); err != nil {
		return err
	}
	var err error
	m.fittedLayout, err = m.nativeLayout()
	return err
}
