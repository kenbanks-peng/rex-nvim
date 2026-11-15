package editor

import (
	"fmt"
	"math"
	"strings"
)

type layoutNode struct {
	Kind     string        `msgpack:"kind"`
	Window   int           `msgpack:"window"`
	Width    int           `msgpack:"width"`
	Height   int           `msgpack:"height"`
	Children []*layoutNode `msgpack:"children"`
}

const readLayout = `
local function read(n)
 if n[1]=='leaf' then
  return {kind='leaf',window=n[2],width=vim.api.nvim_win_get_width(n[2]),height=vim.api.nvim_win_get_height(n[2])+(vim.fn.getwininfo(n[2])[1].winbar or 0)+1}
 end
 local children={}
 local width,height=0,0
 for i,c in ipairs(n[2]) do
  local v=read(c)
  children[#children+1]=v
  if n[1]=='row' then width=width+v.width+(i>1 and 1 or 0);height=math.max(height,v.height)
  else width=math.max(width,v.width);height=height+v.height end
 end
 return {kind=n[1],children=children,width=width,height=height}
end
return read(vim.fn.winlayout())
`

func (m *manager) nativeLayout() (*layoutNode, error) {
	var node layoutNode
	err := m.uiOperation(func() error { return m.nv.ExecLua(readLayout, &node) })
	return &node, err
}
func (n *layoutNode) shape() string {
	if n == nil {
		return ""
	}
	if n.Kind == "leaf" {
		return fmt.Sprint(n.Window)
	}
	var b strings.Builder
	b.WriteString(n.Kind)
	b.WriteByte('(')
	for _, c := range n.Children {
		b.WriteString(c.shape())
		b.WriteByte(',')
	}
	b.WriteByte(')')
	return b.String()
}
func (n *layoutNode) first() int {
	if n.Kind == "leaf" {
		return n.Window
	}
	return n.Children[0].first()
}
func sameDimensions(a, b *layoutNode) bool {
	if a == nil || b == nil || a.shape() != b.shape() || a.Width != b.Width || a.Height != b.Height {
		return false
	}
	for i := range a.Children {
		if !sameDimensions(a.Children[i], b.Children[i]) {
			return false
		}
	}
	return true
}
func (m *manager) syncTopology() error {
	node, err := m.nativeLayout()
	if err != nil {
		return err
	}
	blocks := map[int]string{}
	for _, v := range m.views() {
		if p := m.panes[v.Grid]; p != nil && !v.Float && !v.External && !v.Hidden {
			blocks[v.Window] = p.block
		}
	}
	var complete func(*layoutNode) bool
	complete = func(n *layoutNode) bool {
		if n.Kind == "leaf" {
			return blocks[n.Window] != ""
		}
		if len(n.Children) == 0 {
			return false
		}
		for _, c := range n.Children {
			if !complete(c) {
				return false
			}
		}
		return true
	}
	if !complete(node) {
		return nil
	}
	shape := node.shape()
	changed := shape != m.layoutShape
	if !changed && sameDimensions(node, m.fittedLayout) {
		return nil
	}
	// A Rex close can race its renderer's disconnect notification.
	present := func() (bool, error) {
		view, err := m.rex.View(m.ctx)
		if err != nil {
			return false, err
		}
		found := map[string]bool{}
		for _, w := range view.Windows {
			for _, l := range w.Layers {
				for _, b := range l.Blocks {
					found[b.ID] = true
				}
			}
		}
		for _, block := range blocks {
			if !found[block] {
				return false, nil
			}
		}
		return true, nil
	}
	if ok, err := present(); err != nil {
		return err
	} else if !ok {
		return nil
	}
	var apply func(*layoutNode) error
	apply = func(n *layoutNode) error {
		if n.Kind == "leaf" {
			return nil
		}
		direction, axis := "right", "horizontal"
		extent := func(c *layoutNode) int { return c.Width }
		if n.Kind == "col" {
			direction, axis = "below", "vertical"
			extent = func(c *layoutNode) int { return c.Height }
		}
		// Split representatives first, then expand each child subtree in place.
		for i := 1; i < len(n.Children); i++ {
			before, after := blocks[n.Children[i-1].first()], blocks[n.Children[i].first()]
			remaining := 0
			for _, c := range n.Children[i:] {
				remaining += extent(c)
			}
			ratio := float64(extent(n.Children[i-1])) / float64(max(1, extent(n.Children[i-1])+remaining))
			ratio = math.Max(.01, math.Min(.99, ratio))
			if changed {
				if err := m.rex.Move(m.ctx, after, direction, before, 1-ratio); err != nil {
					return err
				}
			} else {
				if err := m.rex.SetRatio(m.ctx, before, after, axis, ratio); err != nil {
					return err
				}
			}
		}
		for _, c := range n.Children {
			if err := apply(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := apply(node); err != nil {
		if ok, checkErr := present(); checkErr == nil && !ok {
			return nil
		}
		return err
	}
	if changed {
		// Rex auto-balancing may change earlier borders while blocks move.
		changed = false
		if err := apply(node); err != nil {
			return err
		}
	}
	m.layoutShape = shape
	m.fittedLayout = node
	return nil
}
