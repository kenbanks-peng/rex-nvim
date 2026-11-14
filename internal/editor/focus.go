package editor

import "time"

// Focus reports are per terminal, but Neovim has one UI. Coalesce a pane's
// loss with the next pane's gain; a late loss from the old pane is ignored.
func (m *manager) setUIFocus(gained bool) error {
	if m.uiFocusKnown && m.uiFocused == gained {
		return nil
	}
	// go-client predates the typed wrapper for this API.
	if err := m.nv.Request("nvim_ui_set_focus", nil, gained); err != nil {
		return err
	}
	m.uiFocusKnown, m.uiFocused = true, gained
	return nil
}

func (m *manager) ownsBlock(block string) bool {
	if block == "" {
		return false
	}
	for _, p := range m.panes {
		if p.block == block {
			return true
		}
	}
	return false
}

// Session snapshots identify transitions to other tabs or non-editor panes,
// but cannot report OS application activation. Unchanged snapshots must not
// undo a terminal's application-focus loss.
func (m *manager) observeRexFocus(block string) error {
	if block == m.rexFocusBlock {
		return nil
	}
	m.rexFocusBlock = block
	m.blurAt = time.Time{}
	m.focusBlock = block
	return m.setUIFocus(m.ownsBlock(block))
}

func (m *manager) flushBlur(now time.Time) error {
	if m.blurAt.IsZero() || now.Before(m.blurAt) {
		return nil
	}
	view, err := m.rex.View(m.ctx)
	if err != nil {
		return err
	}
	block := ""
	for _, w := range view.Windows {
		if w.ID == view.ActiveWindowID {
			block = w.Focused
			break
		}
	}
	m.blurAt = time.Time{}
	// Rex may have selected the sibling before its terminal delivers focus-in.
	if block != m.focusBlock && m.ownsBlock(block) {
		m.focusBlock, m.rexFocusBlock = block, block
		return nil
	}
	m.rexFocusBlock = block
	return m.setUIFocus(false)
}
