package screen

// HostState contains editor-wide metadata and pane-local attention requests.
// Icon is retained but has no Rex equivalent. Bells coalesce per grid until read.
type HostState struct {
	Title, Icon, Directory *string
	Bells                  map[int]bool
}

// HostPending includes host-only batches, which need not carry a grid flush.
func (s *State) HostPending() bool { return s.hostPending }

// TakeHostState retains metadata for new renderers and consumes attention requests.
func (s *State) TakeHostState() HostState {
	host := s.host
	s.host.Bells = nil
	s.hostPending = false
	return host
}
