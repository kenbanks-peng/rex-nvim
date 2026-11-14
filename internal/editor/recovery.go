package editor

func (m *manager) transportLost(p *peer) error {
	p.broken = true
	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}
	view, err := m.rex.View(m.ctx)
	if err != nil {
		return err
	}
	for _, w := range view.Windows {
		for _, l := range w.Layers {
			for _, b := range l.Blocks {
				if b.ID == p.block {
					return m.replaceRenderer(p)
				}
			}
		}
	}
	// A missing Rex block is an intentional close, unlike a broken transport.
	return m.disconnected(p)
}
func (m *manager) replaceRenderer(p *peer) error {
	if p.layer != "" && m.isExternal(p.grid) {
		view, err := m.rex.View(m.ctx)
		if err != nil {
			return err
		}
		window, _ := m.floatSpace(view)
		if window == "" {
			return nil
		}
		bounds := p.bounds
		for _, w := range view.Windows {
			for _, layer := range w.Layers {
				if layer.ID == p.layer {
					bounds = layer.Bounds
				}
			}
		}
		block, layer, err := m.rex.Float(m.ctx, window, bounds, m.command(p.grid))
		if err != nil {
			return err
		}
		if err := m.rex.CloseLayer(m.ctx, p.layer); err != nil {
			_ = m.rex.CloseLayer(m.ctx, layer)
			return err
		}
		m.panes[p.grid] = &peer{grid: p.grid, block: block, layer: layer, bounds: bounds}
		return nil
	}
	if p.layer != "" {
		// Recreate the float on the next reconciliation, retaining its Neovim window.
		if err := m.rex.CloseLayer(m.ctx, p.layer); err != nil {
			return err
		}
		delete(m.panes, p.grid)
		return nil
	}
	block, err := m.rex.Split(m.ctx, p.block, "right", .5, m.command(p.grid))
	if err != nil {
		return err
	}
	replacement := &peer{grid: p.grid, block: block, primary: p.primary}
	m.panes[p.grid] = replacement
	if p.primary {
		m.initial = block
	}
	m.layoutShape = ""
	return m.rex.Close(m.ctx, p.block)
}
