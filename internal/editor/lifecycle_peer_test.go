package editor

import (
	"context"
	"testing"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
)

func TestRendererSuspensionKeepsPeerAndSiblings(t *testing.T) {
	p := &peer{grid: 2, primary: true, frames: make(chan string, 1), control: make(chan terminal.Message, 1), width: 20, height: 3}
	sibling := &peer{grid: 3, frames: make(chan string, 1), width: 20, height: 3}
	m := &manager{ctx: context.Background(), state: screen.New(), panes: map[int]*peer{2: p, 3: sibling}}
	if err := m.handle(incoming{p: p, msg: terminal.Message{Type: "suspend"}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-p.control; msg.Type != "suspend" {
		t.Fatal(msg)
	}
	m.draw()
	if len(p.frames) != 0 || len(sibling.frames) != 1 {
		t.Fatal("suspension affected sibling or drew suspended renderer")
	}
	if err := m.handle(incoming{p: p, msg: terminal.Message{Type: "resume"}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-p.control; msg.Type != "resume" {
		t.Fatal(msg)
	}
	if m.panes[2] != p || m.panes[3] != sibling || p.suspended || len(p.frames) != 1 {
		t.Fatal("resume did not reuse peer and force repaint")
	}
	// A delayed event from the previous connection cannot suspend its replacement.
	old := &peer{grid: 2}
	if err := m.handle(incoming{p: old, msg: terminal.Message{Type: "suspend"}}); err != nil {
		t.Fatal(err)
	}
	if p.suspended {
		t.Fatal("stale transport suspended current peer")
	}
}
