package screen

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in because this check creates (and removes) its own Rex session.
func TestRexRenderingFidelity(t *testing.T) {
	if os.Getenv("REX_NVIM_HOST_TEST") != "1" {
		t.Skip("set REX_NVIM_HOST_TEST=1 for a disposable Rex session")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	run := func(args ...string) []byte {
		t.Helper()
		out, err := exec.CommandContext(ctx, "rex", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("rex %v: %v: %s", args, err, out)
		}
		return out
	}
	s := New()
	resize(s, 2, 6, 3)
	s.Apply(event("default_colors_set", 0x112233, 0x445566, -1))
	cells := []any{}
	for i, key := range []string{"underline", "underdouble", "undercurl", "underdotted", "underdashed"} {
		s.Apply(event("hl_attr_define", i+1, map[string]any{key: true, "special": 0x010203}, nil, nil))
		cells = append(cells, []any{string(rune('A' + i)), i + 1})
	}
	s.Apply(event("hl_attr_define", 6, map[string]any{"dim": true, "blink": true, "conceal": true, "overline": true}, nil, nil))
	cells = append(cells, []any{"F", 6})
	s.Apply(event("grid_line", 2, 0, 0, cells), event("grid_cursor_goto", 2, 2, 0),
		event("hl_attr_define", 9, map[string]any{"background": 0xaabbcc}, nil, nil),
		event("mode_info_set", true, []any{map[string]any{"attr_id": 9, "cursor_shape": "vertical"}}))
	path := filepath.Join(t.TempDir(), "frame")
	if err := os.WriteFile(path, []byte(s.Render(2, 6, 3)), 0600); err != nil {
		t.Fatal(err)
	}
	// Query the terminal, not just emitted bytes, to prove OSC 12 was applied.
	code := `
import os,sys,tty,select,time
tty.setraw(0)
sys.stdout.buffer.write(open(sys.argv[1],'rb').read()+bytes([27])+b']12;?'+bytes([27,92]))
sys.stdout.flush()
if select.select([0],[],[],3)[0]:
    print('CURSOR_REPLY='+repr(os.read(0,1024)),flush=True)
time.sleep(30)
`
	var created struct {
		Session string `json:"session_id"`
		Windows []struct {
			Blocks []string `json:"block_ids"`
		} `json:"initial_windows"`
	}
	if err := json.Unmarshal(run("new", "rex-nvim-fidelity", "--json", "--shell=none", "--", python, "-c", code, path), &created); err != nil {
		t.Fatal(err)
	}
	if created.Session == "" {
		t.Fatal("missing session ID")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(cleanup, "rex", "kill", created.Session).CombinedOutput(); err != nil {
			t.Errorf("cleanup: %v: %s", err, out)
		}
	})
	if len(created.Windows) == 0 || len(created.Windows[0].Blocks) == 0 {
		t.Fatal("missing block")
	}
	block := created.Windows[0].Blocks[0]
	for {
		data := run("block", "call", "-s", created.Session, "-b", block, "format", `{"format":"vt","trim":true}`)
		var result struct{ Content string }
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(plain(result.Content), "CURSOR_REPLY=") {
			for _, want := range []string{"[4m", "[4:2m", "[4:3m", "[4:4m", "[4:5m", "[2m", "[5m", "[8m", "[53m", "58;2;1;2;3m", "38;2;17;34;51m", "48;2;68;85;102m", "12;rgb:aaaa/bbbb/cccc"} {
				if !strings.Contains(result.Content, want) {
					t.Errorf("host lost %q: %q", want, result.Content)
				}
			}
			return
		}
		if ctx.Err() != nil {
			t.Fatal("host cursor reply timed out")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
