package editor

import (
	"fmt"
	"math"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
)

// Renderer cells and normalized Rex rectangles give the whole window canvas,
// even when Neovim owns only a subset of its tiled blocks.
func (m *manager) windowSize(view rex.View, width, height int) (int, int) {
	left, top, right, bottom := 1.0, 1.0, 0.0, 0.0
	var visit func(*rex.Node)
	visit = func(n *rex.Node) {
		if n == nil {
			return
		}
		if n.Split != nil {
			visit(n.Split.Before)
			visit(n.Split.After)
			return
		}
		for _, p := range m.panes {
			if p.layer != "" || p.block != n.BlockID || p.width < 1 || p.height < 1 {
				continue
			}
			if n.Rect[2] > 0 && n.Rect[3] > 0 {
				left, top = min(left, n.Rect[0]), min(top, n.Rect[1])
				right, bottom = max(right, n.Rect[0]+n.Rect[2]), max(bottom, n.Rect[1]+n.Rect[3])
			}
		}
	}
	for _, w := range view.Windows {
		for _, l := range w.Layers {
			if l.Kind == "tiled" {
				visit(l.Layout)
			}
		}
	}
	if right > left {
		width = int(math.Round(float64(width) / (right - left)))
	}
	if bottom > top {
		height = int(math.Round(float64(height) / (bottom - top)))
	}
	return width, height
}

// Presentation can change while a non-fast layout RPC waits on a native prompt.
// Only Rex operations run here; Neovim window fitting remains deferred.
func (m *manager) syncFloatingPresentation() error {
	views := m.views()
	live := map[int]bool{}
	for _, v := range views {
		if !v.Hidden {
			live[v.Grid] = true
		}
	}
	for grid, p := range m.panes {
		if p.layer != "" && !live[grid] {
			m.closePane(p)
			delete(m.panes, grid)
		}
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
	return nil
}

func (m *manager) fitWindow(sizes [][]int) error {
	var size []int
	if err := m.uiOperation(func() error { return m.nv.ExecLua(measureLayout, &size, sizes, true) }); err != nil {
		return err
	}
	view, err := m.rex.View(m.ctx)
	if err != nil {
		return err
	}
	w, h := m.windowSize(view, size[0], size[1])
	w, h = max(12, w), max(3, h)
	fit := fmt.Sprint(sizes, w, h, size[2:])
	if fit == m.windowFit {
		return nil
	}
	if w != m.rootW || h != m.rootH {
		if err := m.uiOperation(func() error { return m.nv.TryResizeUI(w, h) }); err != nil {
			return err
		}
		m.rootW, m.rootH = w, h
	}
	if err := m.uiOperation(func() error {
		return m.nv.ExecLua(`
local sizes = ...
local top = (vim.o.showtabline==2 or (vim.o.showtabline==1 and #vim.api.nvim_list_tabpages()>1)) and 1 or 0
local bottom = vim.o.cmdheight + 1
vim.g.rex_nvim_fitting = true
for _,s in ipairs(sizes) do
 if vim.api.nvim_win_is_valid(s[1]) then
  local pos = vim.api.nvim_win_get_position(s[1])
  local oldheight = vim.api.nvim_win_get_height(s[1])
  local winbar = vim.fn.getwininfo(s[1])[1].winbar or 0
  local first = pos[1] == top
  local last = pos[1]+oldheight+winbar >= vim.o.lines-bottom
  local height = math.max(1,s[3]-winbar-(first and top or 0)-(last and bottom or 1))
  if vim.api.nvim_win_get_width(s[1]) ~= s[2] then pcall(vim.api.nvim_win_set_width,s[1],s[2]) end
  if oldheight ~= height then pcall(vim.api.nvim_win_set_height,s[1],height) end
 end
end
if vim.o.cmdheight ~= vim.g.rex_nvim_cmdheight then vim.o.cmdheight = vim.g.rex_nvim_cmdheight end
vim.g.rex_nvim_fitting = false
`, nil, sizes)
	}); err != nil {
		return err
	}
	m.mu.Lock()
	m.state.SetCommandHeight(size[2])
	m.state.SetLastStatus(3)
	m.state.SetTabCount(size[4])
	m.mu.Unlock()
	m.fittedLayout, err = m.nativeLayout()
	if err == nil {
		m.windowFit = fit
	}
	return err
}
