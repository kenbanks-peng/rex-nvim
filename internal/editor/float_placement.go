package editor

import "github.com/kenbanks-peng/rex-nvim/internal/rex"

func (m *manager) syncFloatPlacement(view rex.View) error {
	_, space := m.floatSpace(view)
	if space[2] <= 0 || space[3] <= 0 {
		return nil
	}
	for _, w := range view.Windows {
		for _, layer := range w.Layers {
			for _, p := range m.panes {
				if p.layer == "" || p.layer != layer.ID || p.bounds == layer.Bounds {
					continue
				}
				row := (layer.Bounds[1] - space[1]) / space[3] * float64(m.rootH)
				col := (layer.Bounds[0] - space[0]) / space[2] * float64(m.rootW)
				for _, v := range m.views() {
					if v.Grid == p.grid {
						if err := m.nv.ExecLua(`
local win,row,col=...
if vim.api.nvim_win_is_valid(win) then
 local c=vim.api.nvim_win_get_config(win)
 if c.relative~='' then
  vim.api.nvim_win_set_config(win,{relative='editor',anchor='NW',row=math.max(0,row),col=math.max(0,col)})
 end
end
`, nil, v.Window, row, col); err != nil {
							return err
						}
						break
					}
				}
				p.bounds = layer.Bounds
				for _, v := range m.views() {
					if v.Grid == p.grid {
						p.geometry = geometry(v)
						break
					}
				}
			}
		}
	}
	return nil
}
