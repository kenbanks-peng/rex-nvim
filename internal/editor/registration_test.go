package editor

import (
	"testing"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
)

func TestLateHelperCannotReplaceReassignedPrimary(t *testing.T) {
	primary := &peer{grid: 7, block: "original", primary: true}
	m := &manager{state: screen.New(), panes: map[int]*peer{7: primary}}
	late := &peer{grid: 7, ready: make(chan struct{})}
	if err := m.handle(incoming{p: late, msg: terminal.Message{Type: "hello", Block: "closed-secondary", Width: 80, Height: 24}}); err != nil {
		t.Fatal(err)
	}
	if m.panes[7] != primary {
		t.Fatal("late helper hijacked invoking renderer")
	}
	select {
	case <-late.ready:
	default:
		t.Fatal("rejected helper was not released")
	}
}
