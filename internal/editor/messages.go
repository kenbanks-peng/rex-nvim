package editor

// A prompt can start after a blocking-state check but before a layout RPC is
// serviced. Keep painting redraws while that RPC waits for input; otherwise
// Neovim waits for an answer to a prompt the renderer has never displayed.
// Pane ownership still changes only on the manager goroutine. Painting here
// excludes popup geometry RPCs and defers full reconciliation until completion.
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
			m.drawFrames()
		case <-m.paint:
			m.drawFrames()
		}
	}
}
