package screen

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Inspect actual Rex terminal modes in an isolated session, without focusing it.
func TestRexMouseAndBusyModes(t *testing.T) {
	if os.Getenv("REX_NVIM_HOST_TEST") != "1" {
		t.Skip("set REX_NVIM_HOST_TEST=1 for disposable Rex session")
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
	framePath := filepath.Join(t.TempDir(), "frame")
	s := New()
	resize(s, 1, 4, 2)
	s.Apply(event("grid_cursor_goto", 1, 0, 0))
	publish := func() {
		t.Helper()
		if err := os.WriteFile(framePath, []byte(s.Render(1, 4, 2)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	publish()
	code := `import sys,time
last=None
while True:
 data=open(sys.argv[1],'rb').read()
 if data!=last:
  sys.stdout.buffer.write(data);sys.stdout.flush();last=data
 time.sleep(.02)
`
	var created struct {
		Session string `json:"session_id"`
		Windows []struct {
			Blocks []string `json:"block_ids"`
		} `json:"initial_windows"`
	}
	if err := json.Unmarshal(run("new", "rex-nvim-ui-state", "--json", "--shell=none", "--", "python3", "-c", code, framePath), &created); err != nil {
		t.Fatal(err)
	}
	if created.Session == "" {
		t.Fatal("missing session")
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
	check := func(button, anyMouse, sgr, cursor bool) {
		t.Helper()
		for {
			var result struct {
				Modes []struct {
					Number int
					Set    bool
				}
			}
			if err := json.Unmarshal(run("block", "call", "-s", created.Session, "-b", block, "modes"), &result); err != nil {
				t.Fatal(err)
			}
			modes := map[int]bool{}
			for _, m := range result.Modes {
				modes[m.Number] = m.Set
			}
			if !modes[1000] && modes[1002] == button && modes[1003] == anyMouse && modes[1006] == sgr && modes[25] == cursor {
				return
			}
			if ctx.Err() != nil {
				t.Fatalf("wrong Rex modes: %v", modes)
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	check(false, false, false, true)
	s.Apply(event("mouse_on"))
	publish()
	check(true, false, true, true)
	s.Apply(event("option_set", "mousemoveevent", true), event("busy_start"))
	publish()
	check(false, true, true, false)
	s.Apply(event("mouse_off"), event("busy_stop"))
	publish()
	check(false, false, false, true)
}
