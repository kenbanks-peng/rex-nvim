package editor

import (
	"log"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
)

// RPCs must run outside the state lock: their redraw callbacks need that lock.
func (m *manager) reportPopupGeometry() {
	m.mu.Lock()
	var geometry screen.PopupGeometry
	found := false
	for id, p := range m.panes {
		if p.suspended || p.frames == nil {
			continue
		}
		if g, ok := m.state.PopupGeometry(id, p.width, p.height); ok {
			geometry, found = g, true
			break
		}
	}
	m.mu.Unlock()
	if !found {
		m.popupKnown = false
		return
	}
	if m.popupKnown && geometry == m.popupGeometry {
		return
	}
	if err := m.nv.SetPumHeight(geometry.Height); err != nil {
		log.Printf("popup height: %v", err)
		return
	}
	if err := m.nv.SetPumBounds(float64(geometry.Width), float64(geometry.Height), float64(geometry.RootRow), float64(geometry.RootCol)); err != nil {
		log.Printf("popup bounds: %v", err)
		return
	}
	m.popupGeometry, m.popupKnown = geometry, true
}

func (m *manager) popupInput(e incoming) (bool, error) {
	r := e.p.route.Load()
	if r == nil || e.msg.Type != "mouse" {
		return false, nil
	}
	m.mu.Lock()
	index, hit := m.state.PopupTarget(r.grid, r.width, r.height, e.msg.Row, e.msg.Col)
	enabled := m.state.MouseEnabled()
	m.mu.Unlock()
	if !enabled || !hit {
		return false, nil
	}
	msg := e.msg
	if msg.Button == "wheel" {
		switch msg.Action {
		case "up":
			return true, m.typeKeys("<C-p>")
		case "down":
			return true, m.typeKeys("<C-n>")
		}
	}
	if msg.Button == "left" && msg.Action == "press" {
		return true, m.nv.SelectPopupmenuItem(index, true, true, map[string]any{})
	}
	return true, nil
}
