package editor

import (
	"math"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
)

func (m *manager) isExternal(grid int) bool {
	for _, v := range m.views() {
		if v.Grid == grid {
			return v.External && !v.Hidden
		}
	}
	return false
}

// External windows have no position on Neovim's root canvas. Give them an
// initial centered layer; afterwards Rex owns placement and terminal sizing.
func (m *manager) syncExternals(views []screen.View) error {
	var needed bool
	for _, v := range views {
		needed = needed || (v.External && !v.Hidden && m.panes[v.Grid] == nil)
	}
	if !needed {
		return nil
	}
	view, err := m.rex.View(m.ctx)
	if err != nil {
		return err
	}
	window, _ := m.floatSpace(view)
	if window == "" {
		return nil
	}
	for _, v := range views {
		if !v.External || v.Hidden || m.panes[v.Grid] != nil {
			continue
		}
		w := math.Min(1, math.Max(.001, float64(v.Width)/float64(max(1, m.rootW))))
		h := math.Min(1, math.Max(.001, float64(v.Height)/float64(max(1, m.rootH))))
		bounds := [4]float64{(1 - w) / 2, (1 - h) / 2, w, h}
		block, layer, err := m.rex.Float(m.ctx, window, bounds, m.command(v.Grid))
		if err != nil {
			return err
		}
		m.panes[v.Grid] = &peer{grid: v.Grid, block: block, layer: layer, bounds: bounds}
	}
	return nil
}

func (m *manager) fitRenderer(p *peer) error {
	if m.isExternal(p.grid) {
		if p.width < 1 || p.height < 1 {
			return nil
		}
		m.mu.Lock()
		w, h := m.state.GridSize(p.grid)
		m.mu.Unlock()
		if w != p.width || h != p.height {
			return m.nv.TryResizeUIGrid(p.grid, p.width, p.height)
		}
		return nil
	}
	return m.fit()
}
