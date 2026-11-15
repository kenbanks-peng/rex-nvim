package screen

import (
	"fmt"
	"strconv"
)

// TablineItem uses a Neovim handle, never a Rex window/block ID.
type TablineItem struct {
	Handle int
	Name   string
}
type Tabline struct {
	CurrentTab, CurrentBuffer int
	Tabs, Buffers             []TablineItem
}
type TablineTarget struct {
	Handle int
	Buffer bool
}

func (s *State) ExternalTabline() bool           { return s.externalTabline }
func (s *State) SetExternalTabline(enabled bool) { s.externalTabline = enabled }
func (s *State) Tabline() Tabline {
	t := s.tabline
	t.Tabs = append([]TablineItem(nil), t.Tabs...)
	t.Buffers = append([]TablineItem(nil), t.Buffers...)
	return t
}
func tablineItems(raw any, key string) []TablineItem {
	var items []TablineItem
	for _, v := range sequence(raw) {
		a := attributes(v)
		id := integer(a[key])
		name, ok := a["name"].(string)
		if id > 0 && ok {
			items = append(items, TablineItem{id, name})
		}
	}
	return items
}
func (s *State) tablineVisible() bool {
	return s.showTabline == 2 || (s.showTabline == 1 && s.tabCount() > 1)
}
func (s *State) tabCount() int {
	if s.externalTabline {
		return len(s.tabline.Tabs)
	}
	return s.gridTabCount
}

// TablineRows reserves pane-local navigation, not root-grid rows. Small panes
// retain content; all navigation remains available through native commands.
func (s *State) TablineRows(id, height int) int {
	p := s.views[id]
	if s.windowUI {
		return 0
	}
	if p == nil || p.Hidden || p.Float || p.External || !s.tablineVisible() {
		return 0
	}
	if s.externalTabline {
		if height >= 4 {
			return 2
		}
		return 0
	}
	if height >= 3 {
		return 1
	}
	return 0
}

// Each row always exposes previous/next selection, even when the list exceeds
// the terminal width. ASCII escaping gives labels unambiguous cell/hit widths.
func navigationRow(width int, title string, items []TablineItem, current int, buffer bool) ([]cell, []TablineTarget) {
	cells := blank(width)
	hits := make([]TablineTarget, width)
	put := func(start int, text string, target TablineTarget) {
		for i, r := range text {
			if start+i >= 0 && start+i < width {
				cells[start+i].text = string(r)
				hits[start+i] = target
			}
		}
	}
	if width < 9 || len(items) == 0 {
		return cells, hits
	}
	active := 0
	for i, v := range items {
		if v.Handle == current {
			active = i
			break
		}
	}
	target := func(i int) TablineTarget { return TablineTarget{items[(i+len(items))%len(items)].Handle, buffer} }
	x := 2
	put(x, title, TablineTarget{})
	x += len(title)
	for i := active; i < len(items) && x < width-2; i++ {
		name := strconv.QuoteToASCII(items[i].Name)
		name = name[1 : len(name)-1]
		label := fmt.Sprintf(" %d:%s ", i+1, name)
		if i == active {
			label = "[" + label[1:len(label)-1] + "]"
		}
		room := width - 2 - x
		if len(label) > room {
			label = label[:room]
		}
		put(x, label, target(i))
		x += len(label)
	}
	put(0, "<", target(active-1))
	put(width-1, ">", target(active+1))
	return cells, hits
}
func (s *State) TablineTarget(id, width, height, row, col int) (TablineTarget, bool) {
	rows := s.TablineRows(id, height)
	if rows == 0 || row < height-rows || row >= height || col < 0 || col >= width {
		return TablineTarget{}, false
	}
	if !s.externalTabline {
		return TablineTarget{}, true
	}
	var hits []TablineTarget
	if row == height-2 {
		_, hits = navigationRow(width, "Nvim tabs:", s.tabline.Tabs, s.tabline.CurrentTab, false)
	} else {
		_, hits = navigationRow(width, "Buffers:", s.tabline.Buffers, s.tabline.CurrentBuffer, true)
	}
	return hits[col], true
}
func (s *State) drawTabline(frame []pixel, id, width, height, rows int) {
	if rows == 0 {
		return
	}
	if !s.externalTabline {
		if g := s.grids[1]; g != nil {
			for x := 0; x < min(width, g.width); x++ {
				c := g.cells[x]
				wide := c.text != "" && x+1 < g.width && g.cells[x+1].text == ""
				frame[(height-1)*width+x] = pixel{cell: c, wide: wide, continuation: c.text == ""}
			}
		}
		return
	}
	for i, list := range [][]TablineItem{s.tabline.Tabs, s.tabline.Buffers} {
		title, current := "Nvim tabs:", s.tabline.CurrentTab
		if i == 1 {
			title, current = "Buffers:", s.tabline.CurrentBuffer
		}
		cells, _ := navigationRow(width, title, list, current, i == 1)
		for x, c := range cells {
			frame[(height-2+i)*width+x] = pixel{cell: c}
		}
	}
}
