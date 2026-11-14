package editor

import (
	"testing"

	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
)

func hostPeer(grid int) *peer {
	return &peer{grid: grid, host: make(chan terminal.Message, 1)}
}

func TestHostOnlyBatchesAndPaneRouting(t *testing.T) {
	a, b := hostPeer(2), hostPeer(3)
	a.primary = true
	m := &manager{state: screen.New(), panes: map[int]*peer{2: a, 3: b}}
	title, path := "evil\x1b]52;c;payload\a$(touch nope)", "/tmp/quote';$(touch nope)\n\x1b\\"
	m.state.Apply([]any{"grid_cursor_goto", []any{3, 0, 0}})
	if m.state.Apply([]any{"set_title", []any{title}}, []any{"chdir", []any{path}}, []any{"bell", []any{}}) {
		t.Fatal("host batch pretended to flush grids")
	}
	if !m.state.HostPending() {
		t.Fatal("host-only batch would not wake manager")
	}
	m.draw()
	for _, p := range []*peer{a, b} {
		msg := <-p.host
		if msg.Title == nil || *msg.Title != title || msg.Directory == nil || *msg.Directory != path || msg.Bell != (p == b) {
			t.Fatalf("incorrect metadata/attention routing: %+v", msg)
		}
	}
	if m.state.HostPending() {
		t.Fatal("host events not drained")
	}
	m.draw()
	if len(a.host)+len(b.host) != 0 {
		t.Fatal("metadata or bell repeated with unchanged grids")
	}
	// Command-line cursor falls back to its last window, not the Rex-focused pane.
	m.state.Apply([]any{"grid_cursor_goto", []any{1, 0, 0}}, []any{"visual_bell", []any{}})
	m.draw()
	if !(<-b.host).Bell || len(a.host) != 0 {
		t.Fatal("command-line visual bell routed incorrectly")
	}
	// A late renderer receives metadata, but never a historical bell.
	c := hostPeer(4)
	m.panes[4] = c
	m.draw()
	msg := <-c.host
	if msg.Title == nil || msg.Directory == nil || msg.Bell {
		t.Fatal("late renderer missed metadata or replayed bell")
	}
	// Icon has no pane icon API; do not overwrite a title with it.
	m.state.Apply([]any{"set_icon", []any{"icon"}})
	m.draw()
	if len(a.host)+len(b.host)+len(c.host) != 0 {
		t.Fatal("unsupported icon altered host")
	}
}

func TestHostQueueRetainsBellAndLatestMetadataWithoutFrames(t *testing.T) {
	p := hostPeer(2)
	p.primary = true
	m := &manager{state: screen.New(), panes: map[int]*peer{2: p}}
	for i := 0; i < 100; i++ {
		m.state.Apply([]any{"set_title", []any{string(rune('a' + i))}}, []any{"bell", []any{}}, []any{"flush", []any{}})
		m.draw()
	}
	msg := <-p.host
	if !msg.Bell || msg.Title == nil || *msg.Title != string(rune('a'+99)) {
		t.Fatalf("lost coalesced effects: %+v", msg)
	}
	m.draw()
	if len(p.host) != 0 {
		t.Fatal("bell was cached as a frame")
	}
}

func TestHostBellMissingGridFallbacks(t *testing.T) {
	for _, target := range []string{"active", "last", "primary", "none"} {
		t.Run(target, func(t *testing.T) {
			a, b := hostPeer(2), hostPeer(3)
			a.primary = true
			m := &manager{state: screen.New(), panes: map[int]*peer{2: a, 3: b}}
			want := a
			switch target {
			case "active":
				m.activeGrid.Store(3)
				want = b
			case "last":
				m.lastFocus = 3
				want = b
			case "none":
				m.panes = map[int]*peer{}
				want = nil
			}
			m.state.Apply([]any{"grid_cursor_goto", []any{99, 0, 0}}, []any{"bell", []any{}})
			m.draw()
			for _, p := range []*peer{a, b} {
				if (len(p.host) > 0) != (p == want) {
					t.Fatal("attention escaped fallback target")
				}
				if len(p.host) > 0 && !(<-p.host).Bell {
					t.Fatal("missing attention")
				}
			}
		})
	}
}

func TestHostMetadataClearAndInvalidArguments(t *testing.T) {
	p := hostPeer(2)
	m := &manager{state: screen.New(), panes: map[int]*peer{2: p}}
	m.state.Apply([]any{"set_title", []any{"old"}}, []any{"chdir", []any{"/old"}})
	m.draw()
	<-p.host
	m.state.Apply([]any{"set_title", []any{42}}, []any{"chdir", []any{nil}})
	m.draw()
	if len(p.host) != 0 {
		t.Fatal("invalid arguments changed metadata")
	}
	m.state.Apply([]any{"set_title", []any{""}})
	m.draw()
	if msg := <-p.host; msg.Title == nil || *msg.Title != "" {
		t.Fatal("empty title did not clear host title")
	}
}
