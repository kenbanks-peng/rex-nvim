package editor

import (
	"math"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
)

func geometry(v screen.View) [4]int { return [4]int{v.Row, v.Col, v.Width, v.Height} }
func (m *manager) floatSpace(view rex.View) (string, [4]float64) {
	for _, w := range view.Windows {
		for _, l := range w.Layers {
			for _, b := range l.Blocks {
				if b.ID == m.initial {
					return w.ID, editorBounds(l.Layout, m.panes)
				}
			}
		}
	}
	return "", [4]float64{}
}
func (m *manager) syncFloats(views []screen.View) error {
	needed := false
	for _, v := range views {
		if v.Float && v.Focusable && !v.Hidden {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	view, err := m.rex.View(m.ctx)
	if err != nil {
		return err
	}
	window, space := m.floatSpace(view)
	if window == "" {
		return nil
	}
	root := [2]int{m.rootW, m.rootH}
	for _, v := range views {
		if !v.Float || !v.Focusable || v.Hidden {
			continue
		}
		p := m.panes[v.Grid]
		if p != nil && p.geometry == geometry(v) && p.floatRoot == root {
			continue
		}
		x := space[0] + float64(v.Col)/float64(max(1, m.rootW))*space[2]
		y := space[1] + float64(v.Row)/float64(max(1, m.rootH))*space[3]
		x = math.Max(0, math.Min(.999, x))
		y = math.Max(0, math.Min(.999, y))
		width := math.Max(.001, math.Min(float64(v.Width)/float64(max(1, m.rootW))*space[2], 1-x))
		height := math.Max(.001, math.Min(float64(v.Height)/float64(max(1, m.rootH))*space[3], 1-y))
		bounds := [4]float64{x, y, width, height}
		if p == nil {
			block, layer, err := m.rex.Float(m.ctx, window, bounds, m.command(v.Grid))
			if err != nil {
				return err
			}
			p = &peer{grid: v.Grid, block: block, layer: layer}
			m.panes[v.Grid] = p
		} else if p.bounds != bounds {
			if err := m.rex.ResizeLayer(m.ctx, p.layer, bounds); err != nil {
				return err
			}
		}
		p.bounds, p.geometry, p.floatRoot = bounds, geometry(v), root
	}
	return nil
}
func (m *manager) fitFloat(p *peer) error {
	if p.width < 1 || p.height < 1 {
		return nil
	}
	var view *screen.View
	for _, v := range m.views() {
		if v.Grid == p.grid {
			v := v
			view = &v
			break
		}
	}
	if view == nil {
		return nil
	}
	if view.Width != p.width || view.Height != p.height {
		if err := m.nv.TryResizeUIGrid(p.grid, p.width, p.height); err != nil {
			return err
		}
	}
	// A terminal has headers/padding not measured in Neovim cells. Its size is
	// feedback, not a new request to resize the Rex layer again.
	for _, v := range m.views() {
		if v.Grid == p.grid {
			p.geometry = geometry(v)
			break
		}
	}
	p.floatRoot = [2]int{m.rootW, m.rootH}
	return nil
}
