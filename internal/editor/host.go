package editor

import "github.com/kenbanks-peng/rex-nvim/internal/terminal"

// Called with state locked. Metadata belongs to the shared UI, so every owned
// renderer receives it. Attention belongs only to the Neovim cursor's pane.
func (m *manager) drawHost() {
	host := m.state.TakeHostState()
	bells := map[*peer]bool{}
	for grid := range host.Bells {
		p := m.panes[grid]
		if p == nil {
			p = m.panes[int(m.activeGrid.Load())]
		}
		if p == nil {
			p = m.panes[m.lastFocus]
		}
		if p == nil {
			for _, candidate := range m.panes {
				if candidate.primary {
					p = candidate
					break
				}
			}
		}
		if p != nil {
			bells[p] = true
		}
	}
	for _, p := range m.panes {
		if p.host == nil {
			continue
		}
		msg := terminal.Message{Type: "host", Bell: bells[p]}
		if host.Title != nil && (p.hostTitle == nil || *host.Title != *p.hostTitle) {
			msg.Title, p.hostTitle = host.Title, host.Title
		}
		if host.Directory != nil && (p.hostDirectory == nil || *host.Directory != *p.hostDirectory) {
			msg.Directory, p.hostDirectory = host.Directory, host.Directory
		}
		if msg.Title == nil && msg.Directory == nil && !msg.Bell {
			continue
		}
		// Keep latest metadata and at most one attention request while a slow
		// renderer drains. Host effects never enter the replaceable frame cache.
		select {
		case old := <-p.host:
			if msg.Title == nil {
				msg.Title = old.Title
			}
			if msg.Directory == nil {
				msg.Directory = old.Directory
			}
			msg.Bell = msg.Bell || old.Bell
		default:
		}
		p.host <- msg
	}
}
