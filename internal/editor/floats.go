package editor

import (
	"math"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
)

type floatResize struct {
	size  [2]int
	after uint64
}

func geometry(v screen.View) [4]int { return [4]int{v.Row, v.Col, v.Width, v.Height} }
func (m *manager) ownsTiledBlock(block string) bool {
	for _, p := range m.panes {
		if p.layer == "" && p.block == block {
			return true
		}
	}
	return block == m.initial
}
func (m *manager) floatSpace(view rex.View) (string, [4]float64) {
	for _, w := range view.Windows {
		for _, l := range w.Layers {
			for _, b := range l.Blocks {
				if m.ownsTiledBlock(b.ID) {
					return w.ID, [4]float64{0, 0, 1, 1}
				}
			}
		}
	}
	return "", [4]float64{}
}
func (m *manager) syncFloats(views []screen.View) error {
	needed := false
	for _, v := range views {
		if v.Float && !v.Hidden {
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
		if !v.Float || v.Hidden {
			continue
		}
		p := m.panes[v.Grid]
		if p != nil {
			// Size alone cannot acknowledge a resize: an older snapshot may match it.
			for i := len(p.floatResizes) - 1; i >= 0; i-- {
				resize := p.floatResizes[i]
				if v.ResizeID > resize.after && resize.size == [2]int{v.Width, v.Height} {
					p.geometry = geometry(v)
					p.floatResizes = p.floatResizes[i+1:]
					break
				}
			}
		}
		if p != nil && p.geometry == geometry(v) && p.floatRoot == root {
			continue
		}
		size := [2]int{v.Width, v.Height}
		if p != nil {
			if p.geometry[2] == v.Width && p.geometry[3] == v.Height && p.floatSize != [2]int{} {
				size = p.floatSize
			} else {
				p.floatResizes = nil
			}
		}
		x := space[0] + float64(v.Col)/float64(max(1, m.rootW))*space[2]
		y := space[1] + float64(v.Row)/float64(max(1, m.rootH))*space[3]
		x = math.Max(0, math.Min(.999, x))
		y = math.Max(0, math.Min(.999, y))
		width := math.Max(.001, math.Min(float64(size[0])/float64(max(1, m.rootW))*space[2], 1-x))
		height := math.Max(.001, math.Min(float64(size[1])/float64(max(1, m.rootH))*space[3], 1-y))
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
		p.bounds, p.geometry, p.floatRoot, p.floatSize = bounds, geometry(v), root, size
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
	if p.floatSize == [2]int{} {
		p.floatSize = [2]int{view.Width, view.Height}
	}
	// Terminal space can constrain a popup, but must not replace its requested size.
	w, h := min(p.width, p.floatSize[0]), min(p.height, p.floatSize[1])
	target := [2]int{w, h}
	pending := len(p.floatResizes)
	if pending > 0 && p.floatResizes[pending-1].size == target {
		return nil
	}
	if view.Width != w || view.Height != h || pending > 0 {
		var border []int
		if err := m.nv.ExecLua(`
local b=vim.api.nvim_win_get_config(...).border or {}
local function edge(i)
 if type(b)~='table' or #b==0 then return 0 end
 local c=b[(i-1)%#b+1]
 if type(c)=='table' then c=c[1] end
 return c~='' and 1 or 0
end
return {edge(4)+edge(8),edge(2)+edge(6)}
`, &border, view.Window); err != nil {
			return err
		}
		// Redraw grids include borders; try_resize_grid takes content dimensions.
		w, h = max(border[0]+1, w), max(border[1]+1, h)
		if pending > 0 && p.floatResizes[pending-1].size == [2]int{w, h} {
			return nil
		}
		if pending == 0 && view.Width == w && view.Height == h {
			return nil
		}
		// Outstanding requests each produce a later grid_resize, even for repeated sizes.
		after := view.ResizeID
		if pending > 0 {
			after = max(after, p.floatResizes[pending-1].after+1)
		}
		// Flush each applied resize before another request can coalesce with it.
		if err := m.nv.TryResizeUIGrid(p.grid, w-border[0], h-border[1]); err != nil {
			return err
		}
		p.floatResizes = append(p.floatResizes, floatResize{size: [2]int{w, h}, after: after})
		if err := m.nv.Command("redraw"); err != nil {
			return err
		}
	}
	return nil
}
