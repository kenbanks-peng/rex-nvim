package editor

import (
	"math"
	"slices"
	"sort"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
)

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
		if v.Float && v.ExternalRoot == 0 && !v.Hidden {
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
	observed := map[string][4]float64{}
	var layerOrder []string
	for _, w := range view.Windows {
		for _, layer := range w.Layers {
			observed[layer.ID] = layer.Bounds
			if w.ID == window {
				layerOrder = append(layerOrder, layer.ID)
			}
		}
	}
	for _, v := range views {
		if !v.Float || v.ExternalRoot != 0 || v.Hidden {
			continue
		}
		// Neovim owns float geometry. Renderer dimensions only clip its output.
		x := space[0] + float64(v.Col)/float64(max(1, m.rootW))*space[2]
		y := space[1] + float64(v.Row)/float64(max(1, m.rootH))*space[3]
		x = math.Max(0, math.Min(.999, x))
		y = math.Max(0, math.Min(.999, y))
		width := math.Max(.001, math.Min(float64(v.Width)/float64(max(1, m.rootW))*space[2], 1-x))
		height := math.Max(.001, math.Min(float64(v.Height)/float64(max(1, m.rootH))*space[3], 1-y))
		bounds := [4]float64{x, y, width, height}
		p := m.panes[v.Grid]
		if p == nil {
			block, layer, err := m.rex.Float(m.ctx, window, bounds, m.command(v.Grid))
			if err != nil {
				return err
			}
			p = &peer{grid: v.Grid, block: block, layer: layer}
			m.panes[v.Grid] = p
			layerOrder = append(layerOrder, layer)
		} else {
			current := p.bounds
			if actual, ok := observed[p.layer]; ok {
				current = actual
			}
			if current != bounds {
				if err := m.rex.ResizeLayer(m.ctx, p.layer, bounds); err != nil {
					return err
				}
			}
		}
		p.bounds = bounds
	}
	var floats []screen.View
	owned := map[string]bool{}
	useComposition := true
	for _, v := range views {
		if v.Float && v.ExternalRoot == 0 && !v.Hidden {
			if p := m.panes[v.Grid]; p != nil && p.layer != "" {
				floats = append(floats, v)
				owned[p.layer] = true
				useComposition = useComposition && v.HasCompositionIndex
			}
		}
	}
	sort.Slice(floats, func(i, j int) bool {
		a, b := floats[i], floats[j]
		if useComposition && a.CompositionIndex != b.CompositionIndex {
			return a.CompositionIndex < b.CompositionIndex
		}
		if a.ZIndex != b.ZIndex {
			return a.ZIndex < b.ZIndex
		}
		return a.Grid < b.Grid
	})
	var actual, desired []string
	for _, layer := range layerOrder {
		if owned[layer] {
			actual = append(actual, layer)
		}
	}
	for _, v := range floats {
		desired = append(desired, m.panes[v.Grid].layer)
	}
	if !slices.Equal(actual, desired) {
		for i := 1; i < len(desired); i++ {
			if err := m.rex.RaiseLayer(m.ctx, desired[i], desired[i-1]); err != nil {
				return err
			}
		}
	}
	return nil
}
