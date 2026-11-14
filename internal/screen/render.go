package screen

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type pixel struct {
	cell
	wide, continuation bool
}

// Render produces a complete ANSI frame clipped to width by height.
// It includes floats belonging to this view, but never another tiled window.
func (s *State) Render(id, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	totalHeight := height
	chrome := s.TablineRows(id, height)
	height -= chrome
	frame := make([]pixel, width*totalHeight)
	for i := range frame {
		frame[i].text = " "
	}
	draw := func(g *grid, row, col, srcRow, srcCol, rows, cols int) {
		if g == nil {
			return
		}
		for y := 0; y < rows; y++ {
			sy, dy := srcRow+y, row+y
			if sy < 0 || sy >= g.height || dy < 0 || dy >= height {
				continue
			}
			for x := 0; x < cols; x++ {
				sx, dx := srcCol+x, col+x
				if sx < 0 || sx >= g.width || dx < 0 || dx >= width {
					continue
				}
				c := g.cells[sy*g.width+sx]
				wide := c.text != "" && sx+1 < g.width && g.cells[sy*g.width+sx+1].text == ""
				frame[dy*width+dx] = pixel{c, wide, c.text == ""}
			}
		}
	}
	g := s.grids[id]
	if g != nil {
		draw(g, 0, 0, 0, 0, g.height, g.width)
	}
	baseR, baseC, baseOK := s.origin(id, make(map[int]bool))
	root := s.grids[1]
	owner := s.cursorGrid
	if owner == 1 || owner == s.messageGrid {
		owner = s.lastWindow
	}
	commandStart := 0
	if root != nil {
		commandStart = max(0, root.height-s.commandHeight)
	}
	// Winbar is already inside the window grid. Only the root row immediately
	// below a tiled grid is its statusline; never copy an arbitrary row above it.
	if p := s.views[id]; p != nil && !p.Float && !p.External && !p.Hidden && g != nil && root != nil {
		row := p.Row + g.height
		show := s.lastStatus == 2 || (s.lastStatus == 1 && s.tiledCount() > 1)
		srcCol := p.Col
		if s.lastStatus == 3 {
			show = id == owner
			row = commandStart - 1
			srcCol = 0
		}
		if show && row >= 0 && row < commandStart && !s.tileAt(row, p.Col) {
			draw(root, min(g.height, height-1), 0, row, srcCol, 1, min(width, g.width))
		}
	}
	type layer struct {
		id, z, order, row, col int
		hasOrder               bool
	}
	var layers []layer
	for fid, p := range s.views {
		if !p.Float || fid == id || s.external[fid] {
			continue
		}
		r, c, ok := s.origin(fid, make(map[int]bool))
		if !ok || !baseOK || !s.belongsTo(fid, id) {
			continue
		}
		layers = append(layers, layer{id: fid, z: p.ZIndex, order: p.CompositionIndex, hasOrder: p.HasCompositionIndex, row: r - baseR, col: c - baseC})
	}
	// The explicit cmdheight prevents statuslines or stale win_pos dimensions
	// from being mistaken for command text. Neovim 0.12 uses a message grid.
	commandRows, messageSource := 0, 0
	message := root
	sourceStart := commandStart
	if s.messageGrid != 0 {
		message = s.grids[s.messageGrid]
		sourceStart = 0
	}
	if id != 1 && id == owner && root != nil && message != nil && !s.CmdlineActive() && !s.externalMessages {
		rows := min(s.commandHeight, root.height)
		if s.messageGrid != 0 {
			rows = min(root.height-max(0, s.messageRow), message.height)
		}
		visible := false
		for r := sourceStart; r < min(sourceStart+rows, message.height); r++ {
			for c := 0; c < message.width; c++ {
				if strings.TrimSpace(message.cells[r*message.width+c].text) != "" {
					visible = true
				}
			}
		}
		if visible {
			commandRows = min(height, rows)
			messageSource = sourceStart + rows - commandRows
			layers = append(layers, layer{id: s.messageGrid, z: s.messageZ, order: s.messageOrder, hasOrder: s.messageHasOrder, row: height - commandRows})
		}
	}

	useOrder := len(layers) > 0
	for _, l := range layers {
		useOrder = useOrder && l.hasOrder
	}
	sort.Slice(layers, func(i, j int) bool {
		a, b := layers[i], layers[j]
		if useOrder && a.order != b.order {
			return a.order < b.order
		}
		if a.z != b.z {
			return a.z < b.z
		}
		return a.id < b.id
	})
	for _, l := range layers {
		if l.id == s.messageGrid || l.id == 0 {
			draw(message, l.row, 0, messageSource, 0, commandRows, min(width, message.width))
		} else if fg := s.grids[l.id]; fg != nil {
			draw(fg, l.row, l.col, 0, 0, fg.height, fg.width)
		}
	}

	s.drawMessages(frame, id, width, height)
	cmdRow, cmdCol, cmdCursor := s.drawCommand(frame, id, width, height)
	s.drawPopup(frame, id, width, totalHeight)
	s.drawTabline(frame, id, width, totalHeight, chrome)
	var out strings.Builder
	out.WriteString(s.MouseModes())
	out.WriteString("[?25l")
	for r := 0; r < totalHeight; r++ {
		fmt.Fprintf(&out, "[%d;1H", r+1)
		lastHL := -1
		for c := 0; c < width; c++ {
			p := frame[r*width+c]
			if p.continuation {
				if c > 0 && frame[r*width+c-1].wide {
					continue
				}
				p.text = " "
			}
			if p.wide && (c+1 >= width || !frame[r*width+c+1].continuation) {
				p.text = " "
			}
			if p.hl != lastHL {
				out.WriteString(s.style(p.hl))
				lastHL = p.hl
			}
			out.WriteString(safeText(p.text))
		}
	}
	out.WriteString("[0m")
	cr, cc := s.cursorRow, s.cursorCol
	show := s.cursorGrid == id
	if (s.cursorGrid == 1 || s.cursorGrid == s.messageGrid) && id == s.lastWindow && commandRows > 0 {
		if s.cursorGrid == s.messageGrid {
			cr += s.messageRow
		}
		cr = height - commandRows + (cr - (root.height - commandRows))
		show = true
	}
	if s.CmdlineActive() {
		cr, cc, show = cmdRow, cmdCol, cmdCursor
	}
	if show && !s.busy && cr >= 0 && cr < height && cc >= 0 && cc < width {
		style := cursorStyle{shape: 2}
		color := -1
		if s.cursorMode >= 0 && s.cursorMode < len(s.cursorModes) {
			style = s.cursorModes[s.cursorMode]
			color = s.cursorColor(style.attr)
		}
		if color >= 0 {
			fmt.Fprintf(&out, "\x1b]12;#%06x\x1b\\", color&0xffffff)
		} else {
			out.WriteString("\x1b]112\x1b\\")
		}
		fmt.Fprintf(&out, "\x1b[%d q", style.shape)
		fmt.Fprintf(&out, "\x1b[%d;%dH\x1b[?25h", cr+1, cc+1)
	}
	return out.String()
}

// A root-anchored float belongs to the tiled view under its anchor. A float
// anchored to a window (or another float) stays with that ancestor.
func (s *State) belongsTo(floatID, target int) bool {
	p := s.views[floatID]
	seen := make(map[int]bool)
	for p != nil && p.Float {
		if seen[p.Grid] {
			return false
		}
		seen[p.Grid] = true
		if s.external[p.Grid] && p.Grid != target {
			return false
		}
		if p.parent == target {
			return true
		}
		if p.parent == 1 {
			if target == 1 {
				return true
			}
			t := s.views[target]
			if t == nil || t.Float || t.External || t.Hidden {
				return false
			}
			r, c, ok := s.origin(p.Grid, make(map[int]bool))
			// Use the anchor point rather than top-left for south/east anchors.
			w, h := s.GridSize(p.Grid)
			if strings.HasPrefix(p.anchor, "S") {
				r += h - 1
			}
			if strings.HasSuffix(p.anchor, "E") {
				c += w - 1
			}
			w, h = s.GridSize(target)
			return ok && r >= t.Row && r < t.Row+h && c >= t.Col && c < t.Col+w
		}
		p = s.views[p.parent]
	}
	return false
}

func (s *State) tiledCount() int {
	n := 0
	for _, p := range s.views {
		if !p.Float && !p.External && !p.Hidden {
			n++
		}
	}
	return n
}
func (s *State) tileAt(row, col int) bool {
	for id, p := range s.views {
		if p.Float || p.External || p.Hidden {
			continue
		}
		w, h := s.GridSize(id)
		if row >= p.Row && row < p.Row+h && col >= p.Col && col < p.Col+w {
			return true
		}
	}
	return false
}

func (s *State) style(id int) string {
	h, ok := s.highlights[id]
	if !ok || id == 0 {
		h = highlight{fg: -1, bg: -1, sp: -1}
	}
	fg, bg := h.fg, h.bg
	if fg < 0 {
		fg = s.defaults.fg
	}
	if bg < 0 {
		bg = s.defaults.bg
	}
	codes := []string{"0"}
	if h.bold {
		codes = append(codes, "1")
	}
	if h.italic {
		codes = append(codes, "3")
	}
	if h.underline == 1 {
		codes = append(codes, "4")
	} else if h.underline > 1 {
		codes = append(codes, fmt.Sprintf("4:%d", h.underline))
	}
	for _, attr := range []struct {
		enabled bool
		code    string
	}{{h.dim, "2"}, {h.blink, "5"}, {h.conceal, "8"}, {h.overline, "53"}} {
		if attr.enabled {
			codes = append(codes, attr.code)
		}
	}
	if h.reverse {
		codes = append(codes, "7")
	}
	if h.strike {
		codes = append(codes, "9")
	}
	if fg >= 0 {
		codes = append(codes, fmt.Sprintf("38;2;%d;%d;%d", (fg>>16)&255, (fg>>8)&255, fg&255))
	}
	if bg >= 0 {
		codes = append(codes, fmt.Sprintf("48;2;%d;%d;%d", (bg>>16)&255, (bg>>8)&255, bg&255))
	}
	sp := h.sp
	if sp < 0 {
		sp = s.defaults.sp
	}
	if h.underline > 0 && sp >= 0 {
		codes = append(codes, fmt.Sprintf("58:2::%d:%d:%d", (sp>>16)&255, (sp>>8)&255, sp&255))
	}
	return "[" + strings.Join(codes, ";") + "m"
}

// Terminals expose one cursor color, not a separate cursor glyph foreground.
// attr_id=0 inverts the cell; the cursor fill is its effective foreground.
func (s *State) cursorColor(id int) int {
	if id == 0 {
		hl := 0
		if g := s.grids[s.cursorGrid]; g != nil && s.cursorRow >= 0 && s.cursorRow < g.height && s.cursorCol >= 0 && s.cursorCol < g.width {
			hl = g.cells[s.cursorRow*g.width+s.cursorCol].hl
		}
		h, known := s.highlights[hl]
		fg, bg := s.defaults.fg, s.defaults.bg
		if hl != 0 && known {
			if h.fg >= 0 {
				fg = h.fg
			}
			if h.bg >= 0 {
				bg = h.bg
			}
		}
		if hl != 0 && known && h.reverse {
			return bg
		}
		return fg
	}
	h, ok := s.highlights[id]
	if !ok {
		return -1
	}
	color := h.bg
	if h.reverse {
		color = h.fg
	}
	if color < 0 {
		color = s.defaults.bg
		if h.reverse {
			color = s.defaults.fg
		}
	}
	return color
}

func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// MouseEnabled reports Neovim's mode-dependent mouse policy.
func (s *State) MouseEnabled() bool { return s.mouseEnabled }

// MouseModes is a complete snapshot, so dropped/replayed frames and new peers
// cannot retain stale host reporting modes.
func (s *State) MouseModes() string {
	if !s.mouseEnabled {
		return "\x1b[?1000l\x1b[?1003l\x1b[?1002l\x1b[?1006l"
	}
	if s.mouseMove {
		return "\x1b[?1000l\x1b[?1002l\x1b[?1006h\x1b[?1003h"
	}
	return "\x1b[?1000l\x1b[?1003l\x1b[?1006h\x1b[?1002h"
}
