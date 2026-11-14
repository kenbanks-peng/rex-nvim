// Package screen maintains Neovim multigrid redraw state and renders pane frames.
package screen

import (
	"reflect"
	"sort"
	"strings"
)

// View describes a Neovim window grid. Row and Col are root-grid coordinates.
type View struct {
	Grid, Window, Row, Col, Width, Height int
	Float, External, Focusable, Hidden    bool
	// ExternalRoot identifies an external window containing this float.
	ExternalRoot             int
	ZIndex, CompositionIndex int
	HasCompositionIndex      bool
}

type cell struct {
	text string
	hl   int
}
type grid struct {
	width, height int
	cells         []cell
}
type placement struct {
	View
	anchor               string
	parent               int
	row, col             float64
	screenRow, screenCol int
	hasScreenPosition    bool
}
type highlight struct {
	fg, bg, sp                    int
	bold, italic, reverse, strike bool
	underline                     int
	dim, blink, conceal, overline bool
}

// State holds the latest redraw state. It is not safe for concurrent use.
type State struct {
	messages                  messageState
	externalMessages          bool
	promptInput               bool
	command                   commandState
	externalCmdline           bool
	popup                     popupmenu
	externalPopupmenu         bool
	tabline                   Tabline
	externalTabline           bool
	showTabline, gridTabCount int
	host                      HostState
	hostPending               bool
	// OnIgnoredEvent runs synchronously at most 33 times per State: once per
	// distinct ignored name (up to 32), then once with suppressed=true.
	// Names are bounded to 128 bytes; payloads are never passed to diagnostics.
	OnIgnoredEvent                                     func(name string, suppressed bool)
	ignoredEvents                                      map[string]bool
	ignoredLimit                                       bool
	grids                                              map[int]*grid
	views                                              map[int]*placement
	highlights                                         map[int]highlight
	defaults                                           highlight
	external                                           map[int]bool
	commandHeight, lastStatus, messageGrid, messageRow int
	cursorGrid, cursorRow, cursorCol, lastWindow       int
	messageZ, messageOrder                             int
	messageHasOrder                                    bool
	cursorModes                                        []cursorStyle
	cursorMode                                         int
	commandInput                                       bool
	mouseEnabled, mouseMove, busy                      bool
}

type cursorStyle struct{ shape, attr int }

func New() *State {
	return &State{
		showTabline: 1, gridTabCount: 1, grids: make(map[int]*grid), views: make(map[int]*placement),
		highlights: map[int]highlight{-2: {fg: -1, bg: -1, sp: -1}, -3: {fg: -1, bg: -1, sp: -1, reverse: true}},
		defaults:   highlight{fg: -1, bg: -1, sp: -1}, cursorGrid: 1, commandHeight: 1, lastStatus: 2, messageZ: 200,
	}
}

// SetTabCount updates showtabline=1 visibility when the grid fallback is used.
func (s *State) SetTabCount(count int) { s.gridTabCount = count }

// SetCommandHeight sets Neovim's cmdheight (including zero). The default is one.
func (s *State) SetCommandHeight(height int) { s.commandHeight = max(0, height) }

// SetLastStatus sets Neovim's laststatus: 0 never, 1 multiple windows,
// 2 per-window, or 3 global. Global status is shown only in the current pane.
func (s *State) SetLastStatus(mode int) { s.lastStatus = mode }

// SetExternalGrids snapshots grids assigned their own terminal panes (not just
// Neovim external=true windows, which View.External identifies). Such grids
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
		seen := map[int]bool{}
		for ancestor := p; ancestor != nil && !seen[ancestor.Grid]; ancestor = s.views[ancestor.parent] {
			seen[ancestor.Grid] = true
			if ancestor.External {
				v.ExternalRoot = ancestor.Grid
				break
			}
		}
		v.Row, v.Col, _ = s.origin(id, make(map[int]bool))
		if g := s.grids[id]; g != nil {
			v.Width, v.Height = g.width, g.height
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Grid < out[j].Grid })
	return out
}

// Apply accepts redraw event arrays: ["grid_resize", [grid, width, height], ...].
// A single nested callback batch is accepted as well. Unhandled events are
// ignored with bounded diagnostics, preserving forward compatibility.
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
		if !handledEvents[name] {
			s.ignoreEvent(name)
			continue
		}
		if name == "flush" {
			s.finishMessages()
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
	case "msg_show", "msg_clear", "msg_showmode", "msg_showcmd", "msg_ruler", "msg_history_show", "msg_history_clear":
		s.messageEvent(name, a)
	case "cmdline_show", "cmdline_pos", "cmdline_special_char", "cmdline_hide", "cmdline_block_show", "cmdline_block_append", "cmdline_block_hide":
		s.commandEvent(name, a)
	case "popupmenu_show":
		s.popup = popupmenu{selected: n(1), row: n(2), col: n(3), grid: n(4), visible: true}
		for _, raw := range sequence(at(0)) {
			fields := sequence(raw)
			item := popupItem{}
			for i, dst := range []*string{&item.word, &item.kind, &item.menu, &item.info} {
				if i < len(fields) {
					*dst, _ = fields[i].(string)
				}
			}
			s.popup.items = append(s.popup.items, item)
		}
		if s.popup.selected < -1 || s.popup.selected >= len(s.popup.items) {
			s.popup.selected = -1
		}
	case "popupmenu_select":
		s.popup.selected = n(0)
		if s.popup.selected < -1 || s.popup.selected >= len(s.popup.items) {
			s.popup.selected = -1
		}
	case "popupmenu_hide":
		s.popup = popupmenu{}
	case "tabline_update":
		s.tabline = Tabline{CurrentTab: n(0), Tabs: tablineItems(at(1), "tab"), CurrentBuffer: n(2), Buffers: tablineItems(at(3), "buffer")}
	case "set_title", "set_icon", "chdir":
		if value, ok := at(0).(string); ok {
			switch name {
			case "set_title":
				s.host.Title = &value
			case "set_icon":
				s.host.Icon = &value
			case "chdir":
				s.host.Directory = &value
			}
			s.hostPending = true
		}
	case "bell", "visual_bell":
		if s.host.Bells == nil {
			s.host.Bells = make(map[int]bool)
		}
		target := s.cursorGrid
		if target == 1 || target == s.messageGrid {
			target = s.lastWindow
		}
		s.host.Bells[target] = true
		s.hostPending = true
	case "grid_resize":
		w, h := max(0, n(1)), max(0, n(2))
		next := &grid{width: w, height: h, cells: blank(w * h)}
		if g != nil {
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
		if s.popup.grid == id || s.belongsTo(s.popup.grid, id) {
			s.popup = popupmenu{}
		}
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
		// wrap is advisory: full frames explicitly position rows and never reflow.
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
	case "win_external_pos":
		s.views[id] = &placement{View: View{Grid: id, Window: n(1), External: true, Focusable: true}}
	case "win_float_pos":
		p := &placement{View: View{Grid: id, Window: n(1), Float: true, Focusable: boolean(at(6)), ZIndex: 50}, parent: n(3), row: number(at(4)), col: number(at(5))}
		p.anchor, _ = at(2).(string)
		if len(a) > 7 {
			p.ZIndex = n(7)
		}
		if len(a) > 8 {
			p.CompositionIndex, p.HasCompositionIndex = n(8), true
		}
		if len(a) > 10 {
			p.screenRow, p.screenCol, p.hasScreenPosition = n(9), n(10), true
		}
		s.views[id] = p
	case "win_hide":
		if s.popup.grid == id || s.belongsTo(s.popup.grid, id) {
			s.popup = popupmenu{}
		}
		if p := s.views[id]; p != nil {
			p.Hidden = true
		}
	case "win_close":
		if s.popup.grid == id || s.belongsTo(s.popup.grid, id) {
			s.popup = popupmenu{}
		}
		delete(s.views, id)
	case "msg_set_pos":
		s.messageGrid, s.messageRow = id, n(1)
		s.messageZ, s.messageHasOrder = 200, len(a) > 5
		if len(a) > 4 {
			s.messageZ = n(4)
		}
		s.messageOrder = n(5)
	case "mode_info_set":
		s.cursorModes = nil
		if boolean(at(0)) {
			for _, raw := range sequence(at(1)) {
				mode := attributes(raw)
				style := 2
				switch mode["cursor_shape"] {
				case "horizontal":
					style = 4
				case "vertical":
					style = 6
				}
				if integer(mode["blinkon"]) > 0 && integer(mode["blinkoff"]) > 0 {
					style--
				}
				s.cursorModes = append(s.cursorModes, cursorStyle{shape: style, attr: integer(mode["attr_id"])})
			}
		}
	case "mouse_on":
		s.mouseEnabled = true
	case "mouse_off":
		s.mouseEnabled = false
	case "busy_start":
		s.busy = true
	case "busy_stop":
		s.busy = false
	case "option_set":
		if at(0) == "showtabline" {
			s.showTabline = n(1)
		}
		// Cell widths, fonts, colors and widgets already arrive in grids.
		if at(0) == "mousemoveevent" {
			if value, ok := at(1).(bool); ok {
				s.mouseMove = value
			}
		}
	case "mode_change":
		mode, _ := at(0).(string)
		s.commandInput = strings.HasPrefix(mode, "cmdline")
		s.promptInput = mode == "return_prompt" || mode == "confirm" || mode == "confirm_sub" || mode == "number_prompt" || mode == "more"
		s.cursorMode = n(1)
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
		for i, key := range []string{"underline", "underdouble", "undercurl", "underdotted", "underdashed"} {
			if boolean(m[key]) {
				h.underline = i + 1
			}
		}
		h.dim, h.blink = boolean(m["dim"]), boolean(m["blink"])
		h.conceal, h.overline = boolean(m["conceal"]), boolean(m["overline"])
		// RGB and cterm maps are independent styles. Missing RGB keys use
		// defaults, never cterm overrides. Blend/fonts/URLs degrade to opaque text.
		h.reverse = boolean(m["reverse"])
		h.strike = boolean(m["strikethrough"])
		s.highlights[id] = h
	case "default_colors_set":
		s.defaults.fg, s.defaults.bg, s.defaults.sp = -1, -1, -1
		if len(a) > 0 {
			s.defaults.fg = n(0)
		}
		if len(a) > 1 {
			s.defaults.bg = n(1)
		}
		if len(a) > 2 {
			s.defaults.sp = n(2)
		}
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
	if p.hasScreenPosition {
		return p.screenRow, p.screenCol, true
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
