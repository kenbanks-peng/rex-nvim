package screen

const (
	windowTop      = -10
	windowBottom   = -11
	windowMessages = -12
	windowWinbar   = -1000
)

// SetWindowUI keeps native root chrome out of the editing panes.
func (s *State) SetWindowUI(enabled bool) {
	s.windowUI = enabled
	if enabled {
		s.lastStatus = 3
	}
}

func (s *State) WindowUI() bool { return s.windowUI }

func (s *State) windowRows() (top, bottom int) {
	root := s.grids[1]
	if root == nil {
		return
	}
	if s.tablineVisible() {
		top = 1
	}
	bottom = min(root.height, s.commandHeight)
	if s.lastStatus == 3 {
		bottom = min(root.height, bottom+1)
	}
	return
}

// ContentOffset reserves the root tabline above the first row of editing panes.
func (s *State) ContentOffset(id int) int {
	if !s.windowUI {
		return 0
	}
	p := s.views[id]
	top, _ := s.windowRows()
	if p != nil && !p.Float && !p.External && p.Row == top {
		return top
	}
	return 0
}

func (s *State) windowViews() []View {
	if !s.windowUI {
		return nil
	}
	root := s.grids[1]
	if root == nil {
		return nil
	}
	top, bottom := s.windowRows()
	var out []View
	add := func(id, row, height, z int) {
		if height > 0 {
			out = append(out, View{Grid: id, Row: row, Width: root.width, Height: height, Float: true, ZIndex: z})
		}
	}
	add(windowTop, 0, top, 10)
	add(windowBottom, root.height-bottom, bottom, 10)
	for id, p := range s.views {
		g := s.grids[id]
		if !p.Float && !p.External && !p.Hidden && g != nil && g.topMargin > 0 {
			out = append(out, View{Grid: windowWinbar - id, Row: p.Row, Col: p.Col, Width: g.width, Height: min(g.height, g.topMargin), Float: true, ZIndex: 10})
		}
	}
	if g := s.grids[s.messageGrid]; g != nil {
		add(windowMessages, s.messageRow, min(g.height, root.height-s.messageRow), s.messageZ)
	}
	return out
}

func (s *State) windowRegion(id int) (*grid, View) {
	for _, v := range s.windowViews() {
		if v.Grid != id {
			continue
		}
		source, row := s.grids[1], v.Row
		if id == windowMessages {
			source, row = s.grids[s.messageGrid], 0
		} else if id < windowWinbar {
			source, row = s.grids[windowWinbar-id], 0
		}
		g := &grid{width: v.Width, height: v.Height, cells: blank(v.Width * v.Height)}
		for y := 0; y < v.Height; y++ {
			if row+y < 0 || row+y >= source.height {
				continue
			}
			copy(g.cells[y*g.width:y*g.width+min(g.width, source.width)], source.cells[(row+y)*source.width:(row+y)*source.width+min(g.width, source.width)])
		}
		return g, v
	}
	return nil, View{}
}
