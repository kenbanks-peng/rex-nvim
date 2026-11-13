package editor

import "github.com/kenbanks-peng/rex-nvim/internal/rex"

func editorBounds(node *rex.Node, panes map[int]*peer) [4]float64 {
	owned := map[string]bool{}
	for _, p := range panes {
		if p.layer == "" {
			owned[p.block] = true
		}
	}
	var result [4]float64
	found := false
	var walk func(*rex.Node)
	walk = func(n *rex.Node) {
		if n == nil {
			return
		}
		if owned[n.BlockID] {
			r := n.Rect
			if !found {
				result = r
				found = true
			} else {
				x, y := min(result[0], r[0]), min(result[1], r[1])
				right, bottom := max(result[0]+result[2], r[0]+r[2]), max(result[1]+result[3], r[1]+r[3])
				result = [4]float64{x, y, right - x, bottom - y}
			}
		}
		if n.Split != nil {
			walk(n.Split.Before)
			walk(n.Split.After)
		}
	}
	walk(node)
	if !found {
		return [4]float64{0, 0, 1, 1}
	}
	return result
}
