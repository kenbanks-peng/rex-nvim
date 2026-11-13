package editor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
)

func TestFloatResizeFeedbackPreservesRequestedBounds(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "rex")
	script := `#!/bin/sh
case "$*" in
 *session.view*) echo '{"windows":[{"window_id":"window","layers":[{"layout":{"block_id":"initial","rect":[0,0,1,1]},"blocks":[{"block_id":"initial"}]}]}]}' ;;
 *session.new_layer*) echo '{"block_ids":["float"],"layer_id":"layer"}' ;;
 *) echo '{}' ;;
esac
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	m := &manager{ctx: context.Background(), rex: &rex.Client{Session: "session", Executable: executable}, initial: "initial", rootW: 80, rootH: 24, panes: map[int]*peer{2: {grid: 2, block: "initial"}}}
	v := screen.View{Grid: 3, Window: 1001, Row: 2, Col: 5, Width: 30, Height: 6, Float: true, Focusable: true}
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	p := m.panes[3]
	bounds := p.bounds
	p.floatResize = [2]int{20, 4}
	v.Width, v.Height = 20, 4
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	if p.bounds != bounds || p.floatSize != [2]int{30, 6} || p.floatResize != [2]int{} {
		t.Fatalf("feedback replaced requested geometry: %+v", p)
	}
	m.rootW = 100
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	if p.bounds[2] != .3 || p.bounds[3] != .25 {
		t.Fatalf("root resize lost requested size: %v", p.bounds)
	}
	v.Width, v.Height = 25, 5
	if err := m.syncFloats([]screen.View{v}); err != nil {
		t.Fatal(err)
	}
	if p.floatSize != [2]int{25, 5} || p.bounds[2] != .25 {
		t.Fatalf("plugin resize ignored: %+v", p)
	}
}
