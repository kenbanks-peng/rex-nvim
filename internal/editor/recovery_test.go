package editor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
)

func TestLastRendererTransportFailureKeepsEditorWindow(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "rex")
	script := `#!/bin/sh
case "$*" in
 *session.view*) echo '{"windows":[{"layers":[{"blocks":[{"block_id":"original"}]}]}]}' ;;
 *split*) echo '{"block_ids":["replacement"]}' ;;
 *) echo '{}' ;;
esac
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	p := &peer{grid: 2, block: "original", primary: true}
	m := &manager{ctx: context.Background(), rex: &rex.Client{Session: "session", Executable: executable}, panes: map[int]*peer{2: p}, initial: "original"}
	if err := m.transportLost(p); err != nil {
		t.Fatal(err)
	}
	replacement := m.panes[2]
	if replacement == nil || replacement.block != "replacement" || !replacement.primary {
		t.Fatalf("lost editor window: %+v", replacement)
	}
	if m.initial != "replacement" {
		t.Fatal("recovery lost editor anchor")
	}
}
func TestFailedRendererRecoveryRetainsWindow(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "rex")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	p := &peer{grid: 2, block: "original", primary: true}
	m := &manager{ctx: context.Background(), rex: &rex.Client{Session: "session", Executable: executable}, panes: map[int]*peer{2: p}}
	err := m.transportLost(p)
	if !rex.IsRecoverable(err) {
		t.Fatalf("want recoverable failure, got %v", err)
	}
	if m.panes[2] != p || !p.broken {
		t.Fatal("failed recovery discarded the window")
	}
}
