package screen

import "strconv"

type popupItem struct{ word, kind, menu, info string }
type popupmenu struct {
	items                    []popupItem
	selected, grid, row, col int
	visible                  bool
}

// PopupGeometry describes the clipped pane-local widget and its root-grid bounds.
type PopupGeometry struct {
	Grid, Row, Col, Width, Height, First int
	RootRow, RootCol                     int
}

func (s *State) SetExternalPopupmenu(enabled bool) {
	s.externalPopupmenu = enabled
	if !enabled {
		s.popup = popupmenu{}
	}
}

func popupLabel(item popupItem) string {
	label := item.word
	if item.kind != "" {
		label += " " + item.kind
	}
	if item.menu != "" {
		label += " " + item.menu
	}
	quoted := strconv.QuoteToASCII(label)
	return quoted[1 : len(quoted)-1]
}

func (s *State) popupOwner() int {
	p := s.popup
	if !s.externalPopupmenu || !p.visible || len(p.items) == 0 {
		return 0
	}
	if p.grid == -1 {
		if s.CmdlineActive() {
			return s.commandOwner()
		}
		return 0
	}
	if p.grid != 1 {
		if _, _, ok := s.origin(p.grid, map[int]bool{}); !ok {
			return 0
		}
		if s.external[p.grid] {
			return p.grid
		}
		for _, v := range s.Views() {
			if s.external[v.Grid] && s.belongsTo(p.grid, v.Grid) {
				return v.Grid
			}
		}
		return p.grid
	}
	for _, v := range s.Views() {
		if !v.Float && !v.External && !v.Hidden && p.row >= v.Row && p.row < v.Row+v.Height && p.col >= v.Col && p.col < v.Col+v.Width {
			return v.Grid
		}
	}
	return 0
}

func (s *State) PopupGeometry(id, width, height int) (PopupGeometry, bool) {
	if id == 0 || s.popupOwner() != id || width < 1 {
		return PopupGeometry{}, false
	}
	height -= s.TablineRows(id, height)
	if height < 1 {
		return PopupGeometry{}, false
	}
	p := s.popup
	br, bc, ok := s.origin(id, map[int]bool{})
	if !ok {
		return PopupGeometry{}, false
	}
	ar, ac, anchorOK := s.origin(p.grid, map[int]bool{})
	row, col := ar+p.row-br, ac+p.col-bc
	if p.grid == -1 {
		l := s.commandLayout(id, width, height, p.col)
		row, col, anchorOK = l.row, l.col, l.cursor
	}
	if !anchorOK {
		return PopupGeometry{}, false
	}
	if row < 0 || row >= height || col < 0 || col >= width {
		return PopupGeometry{}, false
	}
	w := 1
	for _, item := range p.items {
		w = max(w, len(popupLabel(item))+2)
	}
	w = min(width, w)
	col = min(col, width-w)
	below, above := height-row-1, row
	h := min(len(p.items), max(below, above))
	if h < 1 {
		return PopupGeometry{}, false
	}
	if p.grid == -1 && above >= h {
		row -= h
	} else if below >= h {
		row++
	} else {
		row -= h
	}
	first := 0
	if p.selected >= h {
		first = p.selected - h + 1
	}
	return PopupGeometry{Grid: id, Row: row, Col: col, Width: w, Height: h, First: first, RootRow: br + row, RootCol: bc + col}, true
}

// PopupTarget consumes only cells covered by the external menu.
func (s *State) PopupTarget(id, width, height, row, col int) (int, bool) {
	g, ok := s.PopupGeometry(id, width, height)
	if !ok || row < g.Row || row >= g.Row+g.Height || col < g.Col || col >= g.Col+g.Width {
		return 0, false
	}
	return g.First + row - g.Row, true
}

func (s *State) drawPopup(frame []pixel, id, width, height int) {
	g, ok := s.PopupGeometry(id, width, height)
	if !ok {
		return
	}
	for y := 0; y < g.Height; y++ {
		index := g.First + y
		label := " " + popupLabel(s.popup.items[index])
		hl := -2
		if index == s.popup.selected {
			hl = -3
		}
		for x := 0; x < g.Width; x++ {
			text := " "
			if x < len(label) {
				text = label[x : x+1]
			}
			frame[(g.Row+y)*width+g.Col+x] = pixel{cell: cell{text: text, hl: hl}}
		}
	}
}
