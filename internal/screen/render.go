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
	frame := make([]pixel, width*height)
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
	if p := s.views[id]; p != nil && !p.Float && !p.Hidden && g != nil && root != nil {
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
	type layer struct{ id, z, row, col int }
	var layers []layer
	for fid, p := range s.views {
		if !p.Float || fid == id || s.external[fid] {
			continue
		}
		r, c, ok := s.origin(fid, make(map[int]bool))
		if !ok || !baseOK || !s.belongsTo(fid, id) {
			continue
		}
		layers = append(layers, layer{fid, p.z, r - baseR, c - baseC})
	}
	sort.Slice(layers, func(i, j int) bool {
		if layers[i].z == layers[j].z {
			return layers[i].id < layers[j].id
		}
		return layers[i].z < layers[j].z
	})
	for _, l := range layers {
		if fg := s.grids[l.id]; fg != nil {
			draw(fg, l.row, l.col, 0, 0, fg.height, fg.width)
		}
	}
	// The explicit cmdheight prevents statuslines or stale win_pos dimensions
	// from being mistaken for command text. Neovim 0.12 uses a message grid.
	commandRows := 0
	message := root
	sourceStart := commandStart
	if s.messageGrid != 0 {
		message = s.grids[s.messageGrid]
		sourceStart = 0
	}
	if id != 1 && id == owner && root != nil && message != nil {
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
			draw(message, height-commandRows, 0, sourceStart+rows-commandRows, 0, commandRows, min(width, message.width))
		}
	}

	var out strings.Builder
	out.WriteString("[?25l")
	for r := 0; r < height; r++ {
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
	if show && cr >= 0 && cr < height && cc >= 0 && cc < width {
		style := 2
		if s.cursorMode >= 0 && s.cursorMode < len(s.cursorModes) {
			style = s.cursorModes[s.cursorMode]
		}
		fmt.Fprintf(&out, "\x1b[%d q", style)
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
			if t == nil || t.Float || t.Hidden {
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
		if !p.Float && !p.Hidden {
			n++
		}
	}
	return n
}
func (s *State) tileAt(row, col int) bool {
	for id, p := range s.views {
		if p.Float || p.Hidden {
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
	if h.underline {
		codes = append(codes, "4")
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
	if h.underline && h.sp >= 0 {
		codes = append(codes, fmt.Sprintf("58;2;%d;%d;%d", (h.sp>>16)&255, (h.sp>>8)&255, h.sp&255))
	}
	return "[" + strings.Join(codes, ";") + "m"
}

func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
