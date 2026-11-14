// Package screen maintains Neovim multigrid redraw state and renders pane frames.
package screen

import (
	"reflect"
	"sort"
)

// View describes a Neovim window grid. Row and Col are root-grid coordinates.
type View struct {
	Grid, Window, Row, Col, Width, Height int
	Float, Focusable, Hidden              bool
	ResizeID                              uint64 // Increases for each grid_resize event.
}

type cell struct {
	text string
	hl   int
}
type grid struct {
	width, height int
	cells         []cell
	resizeID      uint64
}
type placement struct {
	View
	anchor   string
	parent   int
	row, col float64
	z        int
}
type highlight struct {
	fg, bg, sp                               int
	bold, italic, underline, reverse, strike bool
}

// State holds the latest redraw state. It is not safe for concurrent use.
type State struct {
	grids                                              map[int]*grid
	views                                              map[int]*placement
	highlights                                         map[int]highlight
	defaults                                           highlight
	external                                           map[int]bool
	commandHeight, lastStatus, messageGrid, messageRow int
	cursorGrid, cursorRow, cursorCol, lastWindow       int
}

func New() *State {
	return &State{
		grids: make(map[int]*grid), views: make(map[int]*placement),
		highlights: make(map[int]highlight),
		defaults:   highlight{fg: -1, bg: -1, sp: -1}, cursorGrid: 1, commandHeight: 1, lastStatus: 2,
	}
}

// SetCommandHeight sets Neovim's cmdheight (including zero). The default is one.
func (s *State) SetCommandHeight(height int) { s.commandHeight = max(0, height) }

// SetLastStatus sets Neovim's laststatus: 0 never, 1 multiple windows,
// 2 per-window, or 3 global. Global status is shown only in the current pane.
func (s *State) SetLastStatus(mode int) { s.lastStatus = mode }

// SetExternalGrids snapshots grids assigned their own terminal panes. Such grids
// and their descendants are composited only within that external pane.
func (s *State) SetExternalGrids(grids map[int]bool) {
	s.external = make(map[int]bool, len(grids))
	for id, external := range grids {
		if external {
			s.external[id] = true
		}
	}
}

// CursorGrid returns the actual Neovim cursor grid (including root grid 1).
func (s *State) CursorGrid() int { return s.cursorGrid }

// GridSize returns a grid's dimensions, or zeroes for an unknown grid.
func (s *State) GridSize(id int) (int, int) {
	if g := s.grids[id]; g != nil {
		return g.width, g.height
	}
	return 0, 0
}

// Views returns a deterministic snapshot of all window placements, including hidden ones.
func (s *State) Views() []View {
	out := make([]View, 0, len(s.views))
	for id, p := range s.views {
		v := p.View
		v.Row, v.Col, _ = s.origin(id, make(map[int]bool))
		if g := s.grids[id]; g != nil {
			v.Width, v.Height, v.ResizeID = g.width, g.height, g.resizeID
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Grid < out[j].Grid })
	return out
}

// Apply accepts redraw event arrays: ["grid_resize", [grid, width, height], ...].
// A single nested callback batch is accepted as well. Unknown events are ignored.
func (s *State) Apply(updates ...[]any) bool {
	flushed := false
	for _, u := range updates {
		if len(u) == 0 {
			continue
		}
		name, ok := u[0].(string)
		if !ok {
			for _, event := range u {
				if s.Apply(sequence(event)) {
					flushed = true
				}
			}
			continue
		}
		if name == "flush" {
			flushed = true
			continue
		}
		for _, args := range u[1:] {
			s.apply(name, sequence(args))
		}
	}
	return flushed
}

func (s *State) apply(name string, a []any) {
	at := func(i int) any {
		if i < len(a) {
			return a[i]
		}
		return nil
	}
	n := func(i int) int { return integer(at(i)) }
	id := n(0)
	g := s.grids[id]
	switch name {
	case "grid_resize":
		w, h := max(0, n(1)), max(0, n(2))
		next := &grid{width: w, height: h, cells: blank(w * h), resizeID: 1}
		if g != nil {
			next.resizeID = g.resizeID + 1
			for r := 0; r < min(h, g.height); r++ {
				copy(next.cells[r*w:r*w+min(w, g.width)], g.cells[r*g.width:r*g.width+min(w, g.width)])
			}
		}
		s.grids[id] = next
	case "grid_clear":
		if g != nil {
			g.cells = blank(len(g.cells))
		}
	case "grid_destroy":
		delete(s.grids, id)
		delete(s.views, id)
		if id == s.messageGrid {
			s.messageGrid = 0
			s.messageRow = 0
		}
	case "grid_line":
		if g == nil || n(1) < 0 || n(1) >= g.height {
			return
		}
		row, col, hl := n(1), n(2), 0
		for _, raw := range sequence(at(3)) {
			c := sequence(raw)
			if len(c) == 0 {
				continue
			}
			t, ok := c[0].(string)
			if !ok {
				continue
			}
			if len(c) > 1 {
				hl = integer(c[1])
			}
			repeat := 1
			if len(c) > 2 {
				repeat = max(0, integer(c[2]))
			}
			for i := 0; i < repeat; i++ {
				if col >= 0 && col < g.width {
					g.cells[row*g.width+col] = cell{t, hl}
				}
				col++
			}
		}
	case "grid_scroll":
		if g == nil {
			return
		}
		top, bottom, left, right := max(0, n(1)), min(g.height, n(2)), max(0, n(3)), min(g.width, n(4))
		old := append([]cell(nil), g.cells...)
		for r := top; r < bottom; r++ {
			for c := left; c < right; c++ {
				srcR, srcC := r+n(5), c+n(6)
				v := cell{text: " "}
				if srcR >= top && srcR < bottom && srcC >= left && srcC < right {
					v = old[srcR*g.width+srcC]
				}
				g.cells[r*g.width+c] = v
			}
		}
	case "win_pos":
		s.views[id] = &placement{View: View{Grid: id, Window: n(1), Row: n(2), Col: n(3), Width: n(4), Height: n(5), Focusable: true}}
	case "win_float_pos":
		p := &placement{View: View{Grid: id, Window: n(1), Float: true, Focusable: boolean(at(6))}, parent: n(3), row: number(at(4)), col: number(at(5)), z: 50}
		p.anchor, _ = at(2).(string)
		if len(a) > 7 {
			p.z = n(7)
		}
		s.views[id] = p
	case "win_hide":
		if p := s.views[id]; p != nil {
			p.Hidden = true
		}
	case "win_close":
		delete(s.views, id)
	case "msg_set_pos":
		s.messageGrid, s.messageRow = id, n(1)
	case "grid_cursor_goto":
		s.cursorGrid, s.cursorRow, s.cursorCol = id, n(1), n(2)
		if id != 1 && id != s.messageGrid {
			s.lastWindow = id
		}
	case "hl_attr_define":
		h := highlight{fg: -1, bg: -1, sp: -1}
		m := attributes(at(1))
		if v, ok := m["foreground"]; ok {
			h.fg = integer(v)
		}
		if v, ok := m["background"]; ok {
			h.bg = integer(v)
		}
		if v, ok := m["special"]; ok {
			h.sp = integer(v)
		}
		h.bold = boolean(m["bold"])
		h.italic = boolean(m["italic"])
		h.underline = boolean(m["underline"]) || boolean(m["undercurl"]) || boolean(m["underdouble"]) || boolean(m["underdotted"]) || boolean(m["underdashed"])
		h.reverse = boolean(m["reverse"])
		h.strike = boolean(m["strikethrough"])
		s.highlights[id] = h
	case "default_colors_set":
		s.defaults.fg, s.defaults.bg, s.defaults.sp = n(0), n(1), n(2)
	}
}

func blank(n int) []cell {
	out := make([]cell, n)
	for i := range out {
		out[i].text = " "
	}
	return out
}
func sequence(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	r := reflect.ValueOf(v)
	if !r.IsValid() || (r.Kind() != reflect.Slice && r.Kind() != reflect.Array) {
		return nil
	}
	a := make([]any, r.Len())
	for i := range a {
		a[i] = r.Index(i).Interface()
	}
	return a
}
func number(v any) float64 {
	r := reflect.ValueOf(v)
	if !r.IsValid() {
		return 0
	}
	switch r.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(r.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(r.Uint())
	case reflect.Float32, reflect.Float64:
		return r.Float()
	}
	return 0
}
func integer(v any) int  { return int(number(v)) }
func boolean(v any) bool { b, _ := v.(bool); return b }
func attributes(v any) map[string]any {
	out := make(map[string]any)
	r := reflect.ValueOf(v)
	if !r.IsValid() || r.Kind() != reflect.Map {
		return out
	}
	it := r.MapRange()
	for it.Next() {
		if k, ok := it.Key().Interface().(string); ok {
			out[k] = it.Value().Interface()
		}
	}
	return out
}

// origin resolves float anchors recursively into root coordinates.
func (s *State) origin(id int, seen map[int]bool) (int, int, bool) {
	if id == 1 {
		return 0, 0, true
	}
	p := s.views[id]
	if p == nil || p.Hidden || seen[id] {
		return 0, 0, false
	}
	if !p.Float {
		return p.Row, p.Col, true
	}
	seen[id] = true
	r, c, ok := s.origin(p.parent, seen)
	if !ok {
		return 0, 0, false
	}
	r += int(p.row)
	c += int(p.col)
	w, h := s.GridSize(id)
	if len(p.anchor) > 0 && p.anchor[0] == 'S' {
		r -= h
	}
	if len(p.anchor) > 1 && p.anchor[1] == 'E' {
		c -= w
	}
	return r, c, true
}
