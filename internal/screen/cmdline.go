package screen

import (
	"sort"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
)

type commandChunk struct {
	text string
	hl   int
}
type commandLine struct {
	content                []commandChunk
	pos, indent, promptHL  int
	first, prompt, special string
	shift                  bool
}
type commandState struct {
	levels map[int]*commandLine
	block  [][]commandChunk
	owner  int
}

func (s *State) SetExternalCmdline(enabled bool) {
	s.externalCmdline = enabled
	if !enabled {
		s.command = commandState{}
		s.SetExternalMessages(false)
	}
}
func (s *State) CmdlineActive() bool {
	return s != nil && s.externalCmdline && (len(s.command.levels) > 0 || len(s.command.block) > 0)
}

// CommandInputActive includes the native grid fallback's command-entry modes.
func (s *State) CommandInputActive() bool {
	return s != nil && (s.commandInput || s.promptInput || s.CmdlineActive() || s.messages.historyOpen)
}
func (s *State) GridCommandActive() bool {
	return s != nil && ((!s.externalCmdline && s.commandInput) || s.promptInput)
}
func commandChunks(raw any) []commandChunk {
	var out []commandChunk
	for _, raw := range sequence(raw) {
		a := sequence(raw)
		if len(a) >= 2 {
			if text, ok := a[1].(string); ok {
				out = append(out, commandChunk{text, integer(a[0])})
			}
		}
	}
	return out
}
func (s *State) commandOwner() int {
	id := s.command.owner
	if id == 0 {
		id = s.cursorGrid
		if id == 1 || id == s.messageGrid {
			id = s.lastWindow
		}
	}
	if !s.external[id] {
		for _, v := range s.Views() {
			if s.external[v.Grid] && s.belongsTo(id, v.Grid) {
				return v.Grid
			}
		}
	}
	return id
}
func (s *State) commandEvent(name string, a []any) {
	at := func(i int) any {
		if i < len(a) {
			return a[i]
		}
		return nil
	}
	switch name {
	case "cmdline_show":
		if len(s.command.levels) == 0 {
			s.command.owner = s.commandOwner()
		}
		if s.command.levels == nil {
			s.command.levels = map[int]*commandLine{}
		}
		first, _ := at(2).(string)
		prompt, _ := at(3).(string)
		s.command.levels[integer(at(5))] = &commandLine{content: commandChunks(at(0)), pos: integer(at(1)), first: first, prompt: prompt, indent: max(0, integer(at(4))), promptHL: integer(at(6))}
	case "cmdline_pos":
		if c := s.command.levels[integer(at(1))]; c != nil {
			c.pos = integer(at(0))
		}
	case "cmdline_special_char":
		if c := s.command.levels[integer(at(2))]; c != nil {
			c.special, _ = at(0).(string)
			c.shift = boolean(at(1))
		}
	case "cmdline_hide":
		// Abort and acceptance both discard presentation only. Never execute, restore
		// text, or cancel a parent: Neovim sends each level's authoritative state.
		delete(s.command.levels, integer(at(0)))
		if len(s.command.levels) == 0 && len(s.command.block) == 0 {
			s.command.owner = 0
		}
	case "cmdline_block_show":
		s.command.block = nil
		for _, raw := range sequence(at(0)) {
			s.command.block = append(s.command.block, commandChunks(raw))
		}
	case "cmdline_block_append":
		s.command.block = append(s.command.block, commandChunks(at(0)))
	case "cmdline_block_hide":
		s.command.block = nil
		if len(s.command.levels) == 0 {
			s.command.owner = 0
		}
	}
}

// commandPixels converts byte-indexed protocol text to terminal cells. Segment
// the joined text so a combining sequence split across highlight chunks stays
// one grapheme. Cursor positions remain byte offsets, never rune counts.
func commandPixels(chunks []commandChunk, pos int) ([]pixel, int) {
	var text strings.Builder
	var attrs []int
	for _, c := range chunks {
		text.WriteString(c.text)
		for range len(c.text) {
			attrs = append(attrs, c.hl)
		}
	}
	raw := text.String()
	cursor := 0
	var out []pixel
	g := uniseg.NewGraphemes(raw)
	for g.Next() {
		start, end := g.Positions()
		hl := attrs[start]
		word := g.Str()
		if word == "\n" {
			out = append(out, pixel{cell: cell{text: "\n", hl: hl}})
		} else if word == "\t" {
			for range 8 - len(out)%8 {
				out = append(out, pixel{cell: cell{text: " ", hl: hl}})
			}
		} else {
			w := g.Width()
			if strings.ContainsFunc(word, unicode.IsControl) {
				word = " "
				w = 1
			}
			if w == 0 {
				continue
			}
			out = append(out, pixel{cell: cell{text: word, hl: hl}, wide: w == 2})
			if w == 2 {
				out = append(out, pixel{cell: cell{text: "", hl: hl}, continuation: true})
			}
		}
		if end <= pos {
			cursor = len(out)
		}
	}
	return out, cursor
}

func (c *commandLine) pixels(pos int) ([]pixel, int) {
	prefix := []commandChunk{{c.first, 0}, {c.prompt, c.promptHL}, {strings.Repeat(" ", c.indent), 0}}
	pre, _ := commandPixels(prefix, 0)
	body, cursor := commandPixels(c.content, max(0, pos))
	cursor += len(pre)
	out := append(pre, body...)
	if c.special != "" {
		special, _ := commandPixels([]commandChunk{{c.special, 0}}, 0)
		if c.shift {
			out = append(append(append([]pixel{}, out[:cursor]...), special...), out[cursor:]...)
		} else {
			end := min(len(out), cursor+len(special))
			if end < len(out) && out[end].continuation {
				end++
			}
			out = append(append(append([]pixel{}, out[:cursor]...), special...), out[end:]...)
		}
	}
	return out, cursor
}

type commandLayout struct {
	rows     [][]pixel
	row, col int
	cursor   bool
}

func wrapCommand(pixels []pixel, width int, cursor int) commandLayout {
	l := commandLayout{rows: [][]pixel{blankPixels(width)}}
	y, x := 0, 0
	for i, p := range pixels {
		if p.wide && width > 1 && x == width-1 {
			y++
			x = 0
			l.rows = append(l.rows, blankPixels(width))
		}
		if i == cursor {
			l.row, l.col = y, x
			l.cursor = true
		}
		if p.text == "\n" {
			y++
			x = 0
			l.rows = append(l.rows, blankPixels(width))
			continue
		}
		if p.wide && width == 1 {
			p = pixel{cell: cell{text: " ", hl: p.hl}}
		}
		if p.continuation && width == 1 {
			continue
		}
		l.rows[y][x] = p
		x++
		if x == width {
			y++
			x = 0
			l.rows = append(l.rows, blankPixels(width))
		}
	}
	if cursor == len(pixels) {
		l.row, l.col = y, x
		l.cursor = true
	} else if x == 0 && y > 0 && pixels[len(pixels)-1].text != "\n" {
		l.rows = l.rows[:len(l.rows)-1]
	}
	return l
}
func blankPixels(width int) []pixel {
	out := make([]pixel, width)
	for i := range out {
		out[i].text = " "
	}
	return out
}
func (s *State) commandLayout(id, width, height, anchor int) commandLayout {
	if !s.CmdlineActive() || s.commandOwner() != id || width < 1 || height < 1 {
		return commandLayout{}
	}
	var l commandLayout
	anchorRow, anchorCol := -1, 0
	for _, line := range s.command.block {
		p, _ := commandPixels(line, 0)
		l.rows = append(l.rows, wrapCommand(p, width, -1).rows...)
	}
	var levels []int
	for level := range s.command.levels {
		levels = append(levels, level)
	}
	sort.Ints(levels)
	for _, level := range levels {
		c := s.command.levels[level]
		p, cursor := c.pixels(c.pos)
		next := wrapCommand(p, width, cursor)
		if anchor >= 0 && level == levels[len(levels)-1] {
			_, at := c.pixels(anchor)
			point := wrapCommand(p, width, at)
			anchorRow, anchorCol = len(l.rows)+point.row, point.col
		}
		l.row, l.col, l.cursor = len(l.rows)+next.row, next.col, true
		l.rows = append(l.rows, next.rows...)
	}
	start := max(0, len(l.rows)-height)
	if l.cursor && l.row < start {
		start = l.row
	}
	end := min(len(l.rows), start+height)
	l.rows = l.rows[start:end]
	l.row = l.row - start + height - len(l.rows)
	if anchorRow >= 0 {
		l.row, l.col = anchorRow-start+height-len(l.rows), anchorCol
	}
	return l
}
func (s *State) drawCommand(frame []pixel, id, width, height int) (int, int, bool) {
	l := s.commandLayout(id, width, height, -1)
	for y, row := range l.rows {
		copy(frame[(height-len(l.rows)+y)*width:], row)
	}
	return l.row, l.col, l.cursor
}
