package editor

import "log"

// A prompt can start after a blocking-state check but before a layout RPC is
// serviced. Keep painting redraws while that RPC waits for input; otherwise
// Neovim waits for an answer to a prompt the renderer has never displayed.
// Pane ownership still changes only on the manager goroutine. Painting here
// excludes popup geometry RPCs and fitting. Rex-only presentation still follows
// expanded native message grids while the layout operation waits.
func (m *manager) uiOperation(operation func() error) error {
	done := make(chan error, 1)
	go func() { done <- operation() }()
	dirty := false
	defer func() {
		if dirty {
			select {
			case m.wake <- struct{}{}:
			default:
			}
		}
	}()
	for {
		select {
		case err := <-done:
			return err
		case <-m.wake:
			dirty = true
			m.mu.Lock()
			windowUI := m.state.WindowUI()
			m.mu.Unlock()
			if windowUI {
				if err := m.syncFloatingPresentation(); err != nil {
					log.Printf("prompt presentation: %v", err)
				}
			}
			m.drawFrames()
		case <-m.paint:
			m.drawFrames()
		}
	}
}
