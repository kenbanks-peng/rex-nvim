package editor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
)

func TestFloatStackingTracksCompositionWithoutFocusOrChurn(t *testing.T) {
	dir := t.TempDir()
	exe, log := filepath.Join(dir, "rex"), filepath.Join(dir, "calls")
	script := "#!/bin/sh\n" +
		"echo \"$*\" >> '" + log + "'\n" +
		"case \"$*\" in\n *session.view*) cat '" + filepath.Join(dir, "view.json") + "';;\n *) echo '{}';;\nesac\n"
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	bounds := [4]float64{0, 0, .1, .1}
	m := &manager{ctx: context.Background(), rex: &rex.Client{Session: "session", Executable: exe}, initial: "initial", rootW: 10, rootH: 10,
		panes: map[int]*peer{
			2: {grid: 2, layer: "a", bounds: bounds},
			3: {grid: 3, layer: "b", bounds: bounds},
			4: {grid: 4, layer: "c", bounds: bounds},
		}}
	views := []screen.View{
		{Grid: 2, Float: true, Width: 1, Height: 1, ZIndex: 50, CompositionIndex: 3, HasCompositionIndex: true},
		{Grid: 3, Float: true, Width: 1, Height: 1, ZIndex: 100, CompositionIndex: 2, HasCompositionIndex: true},
		{Grid: 4, Float: true, Width: 1, Height: 1, ZIndex: 200, CompositionIndex: 1, HasCompositionIndex: true},
	}
	setOrder := func(ids ...string) {
		t.Helper()
		layers := []rex.Layer{{ID: "tiled", Blocks: []rex.Block{{ID: "initial"}}}}
		for _, id := range ids {
			layers = append(layers, rex.Layer{ID: id, Bounds: bounds})
		}
		raw, err := json.Marshal(rex.View{Windows: []rex.Window{{ID: "window", Layers: layers}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "view.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(log, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	calls := func() string {
		t.Helper()
		data, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	setOrder("a", "unrelated", "b", "c")
	if err := m.syncFloats(views); err != nil {
		t.Fatal(err)
	}
	got := calls()
	for _, args := range []string{`"layer_id":"b","relative_layer_id":"c"`, `"layer_id":"a","relative_layer_id":"b"`} {
		if !strings.Contains(got, args) {
			t.Fatalf("missing restack %s: %s", args, got)
		}
	}
	for _, method := range []string{"session.focus", "session.resize", "session.close", "session.new"} {
		if strings.Contains(got, method) {
			t.Fatalf("restacking changed focus/geometry/lifetime: %s", got)
		}
	}
	if strings.Contains(got, `"layer_id":"unrelated"`) {
		t.Fatal("unrelated layer moved")
	}
	// Already-correct order produces no writes.
	setOrder("c", "b", "unrelated", "a")
	if err := m.syncFloats(views); err != nil {
		t.Fatal(err)
	}
	if got := calls(); strings.Contains(got, "session.raise_layer") {
		t.Fatalf("redundant restack: %s", got)
	}
	// Older protocol events fall back to zindex, then grid ID.
	for i := range views {
		views[i].HasCompositionIndex = false
		views[i].ZIndex = 50
	}
	setOrder("c", "b", "a")
	if err := m.syncFloats(views); err != nil {
		t.Fatal(err)
	}
	if got := calls(); !strings.Contains(got, `"layer_id":"b","relative_layer_id":"a"`) || !strings.Contains(got, `"layer_id":"c","relative_layer_id":"b"`) {
		t.Fatalf("old event order: %s", got)
	}
}
