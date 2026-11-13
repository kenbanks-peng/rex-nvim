package rex

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fake(t *testing.T, response string) (*Client, func() []string) {
	t.Helper()
	dir := t.TempDir()
	executable, log := filepath.Join(dir, "rex"), filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\0' \"$@\" > \"$REX_TEST_LOG\"\nprintf '%s' \"$REX_TEST_RESPONSE\"\nprintf '%s' \"$REX_TEST_STDERR\" >&2\nexit \"$REX_TEST_EXIT\"\n"
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REX_TEST_LOG", log)
	t.Setenv("REX_TEST_RESPONSE", response)
	t.Setenv("REX_TEST_STDERR", "")
	t.Setenv("REX_TEST_EXIT", "0")
	c := New("work session")
	c.Executable = executable
	return c, func() []string {
		t.Helper()
		b, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
	}
}

func apiArgs(t *testing.T, read func() []string, method string) map[string]any {
	t.Helper()
	args := read()
	want := []string{"--autostart=false", "--timeout=5s", "api", "call", "-s", "work session", method}
	if len(args) != 8 || !reflect.DeepEqual(args[:7], want) {
		t.Fatalf("args = %q", args)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(args[7]), &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSplitArguments(t *testing.T) {
	for _, direction := range []string{"right", "left", "above", "below"} {
		t.Run(direction, func(t *testing.T) {
			c, read := fake(t, `{"block_ids":["new","anchor"]}`)
			command := []string{"nvim", "a file", "--option", "$(touch nope)", "quote'\""}
			id, err := c.Split(context.Background(), "anchor", direction, .3, command)
			if err != nil || id != "new" {
				t.Fatalf("split = %q, %v", id, err)
			}
			want := []string{"--autostart=false", "--timeout=5s", "split", "-s", "work session", "-b", "anchor", "--split=" + direction, "--ratio", "30", "--shell=none", "--focus=false", "--json", "--"}
			want = append(want, command...)
			if got := read(); !reflect.DeepEqual(got, want) {
				t.Fatalf("got %q; want %q", got, want)
			}
		})
	}
}

func TestSplitRatio(t *testing.T) {
	for _, tc := range []struct {
		ratio float64
		pct   string
	}{{0, "1"}, {1, "99"}, {.256, "26"}} {
		c, read := fake(t, `{"block_ids":["new"]}`)
		if _, err := c.Split(context.Background(), "a", "right", tc.ratio, nil); err != nil {
			t.Fatal(err)
		}
		if got := read()[9]; got != tc.pct {
			t.Fatalf("ratio %v = %s, want %s", tc.ratio, got, tc.pct)
		}
	}
}

func TestMoveArguments(t *testing.T) {
	for _, tc := range []struct {
		direction, axis, side string
		ratio                 float64
	}{
		{"right", "horizontal", "after", .7}, {"left", "horizontal", "before", .3},
		{"above", "vertical", "before", .3}, {"below", "vertical", "after", .7},
	} {
		t.Run(tc.direction, func(t *testing.T) {
			c, read := fake(t, "{}")
			if err := c.Move(context.Background(), "b", tc.direction, "a", .3); err != nil {
				t.Fatal(err)
			}
			got := apiArgs(t, read, "session.move_block")
			want := map[string]any{"block_id": "b", "anchor_block_id": "a", "direction": tc.axis, "side": tc.side, "ratio": tc.ratio}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}

func TestSimpleCalls(t *testing.T) {
	for _, tc := range []struct {
		method, key string
		invoke      func(*Client) error
	}{
		{"session.focus_block", "block_id", func(c *Client) error { return c.Focus(context.Background(), "target") }},
		{"block.close", "block_id", func(c *Client) error { return c.Close(context.Background(), "target") }},
		{"session.close_layer", "layer_id", func(c *Client) error { return c.CloseLayer(context.Background(), "target") }},
	} {
		t.Run(tc.method, func(t *testing.T) {
			c, read := fake(t, "{}")
			if err := tc.invoke(c); err != nil {
				t.Fatal(err)
			}
			if got := apiArgs(t, read, tc.method); !reflect.DeepEqual(got, map[string]any{tc.key: "target"}) {
				t.Fatalf("args: %#v", got)
			}
		})
	}
}

func TestView(t *testing.T) {
	c, read := fake(t, `{"active_window_id":"w","windows":[{"window_id":"w","focused_block_id":"a","layers":[{"layer_id":"l","kind":"tiled","bounds":[0,0,1,1],"blocks":[{"block_id":"a"},{"block_id":"b"}],"layout":{"rect":[0,0,1,1],"split":{"split_id":"s","direction":"horizontal","ratio":0.4,"before":{"block_id":"a","rect":[0,0,0.4,1]},"after":{"block_id":"b","rect":[0.4,0,0.6,1]}}}}]}]}`)
	v, err := c.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(apiArgs(t, read, "session.view")) != 0 {
		t.Fatal("unexpected args")
	}
	if v.ActiveWindowID != "w" || len(v.Windows) != 1 {
		t.Fatalf("view %#v", v)
	}
	w := v.Windows[0]
	if w.ID != "w" || w.Focused != "a" || len(w.Layers) != 1 {
		t.Fatalf("window %#v", w)
	}
	l := w.Layers[0]
	if l.ID != "l" || l.Kind != "tiled" || l.Bounds != [4]float64{0, 0, 1, 1} || l.Blocks[1].ID != "b" {
		t.Fatalf("layer %#v", l)
	}
	s := l.Layout.Split
	if s.ID != "s" || s.Direction != "horizontal" || s.Ratio != .4 || s.Before.BlockID != "a" || s.After.BlockID != "b" || s.After.Rect != [4]float64{.4, 0, .6, 1} {
		t.Fatalf("split %#v", s)
	}
}

func TestFloat(t *testing.T) {
	for _, window := range []string{"w", ""} {
		c, read := fake(t, `{"block_ids":["b"],"layer_id":"l"}`)
		bounds := [4]float64{.1, .2, .8, .6}
		command := []string{"nvim", "a file", "--flag"}
		b, l, err := c.Float(context.Background(), window, bounds, command)
		if err != nil || b != "b" || l != "l" {
			t.Fatalf("float %q %q %v", b, l, err)
		}
		got := apiArgs(t, read, "session.new_layer")
		want := map[string]any{"bounds": []any{.1, .2, .8, .6}, "focus": false,
			"layout": map[string]any{"block": map[string]any{"flavor": "com.superlogical.terminal.shell", "options": map[string]any{"shell": "none", "command": []any{"nvim", "a file", "--flag"}}}}}
		if window != "" {
			want["window_id"] = window
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v; want %#v", got, want)
		}
	}
}

func TestInvalidInputs(t *testing.T) {
	c := New("session")
	c.Executable = filepath.Join(t.TempDir(), "must-not-execute")
	for _, ratio := range []float64{-1, 1.1, math.NaN(), math.Inf(1)} {
		if _, err := c.Split(context.Background(), "a", "right", ratio, nil); err == nil {
			t.Fatalf("accepted %v", ratio)
		}
		if err := c.Move(context.Background(), "b", "left", "a", ratio); err == nil {
			t.Fatalf("accepted %v", ratio)
		}
	}
	if _, err := c.Split(context.Background(), "a", "up", .5, nil); err == nil {
		t.Fatal("accepted up")
	}
	if err := c.Move(context.Background(), "a", "left", "a", .5); err == nil {
		t.Fatal("accepted self move")
	}
	for _, bounds := range [][4]float64{{0, 0, 0, 1}, {-.1, 0, 1, 1}, {.5, 0, 1, 1}, {0, 0, 1, math.NaN()}} {
		if _, _, err := c.Float(context.Background(), "w", bounds, nil); err == nil {
			t.Fatalf("accepted %v", bounds)
		}
	}
}

func TestErrors(t *testing.T) {
	t.Run("command", func(t *testing.T) {
		c, _ := fake(t, "details")
		t.Setenv("REX_TEST_EXIT", "7")
		t.Setenv("REX_TEST_STDERR", "permission denied")
		err := c.Focus(context.Background(), "b")
		if err == nil || !strings.Contains(err.Error(), "permission denied") || !strings.Contains(err.Error(), "session.focus_block") || !strings.Contains(err.Error(), "exit status 7") {
			t.Fatalf("error %v", err)
		}
	})
	t.Run("decode", func(t *testing.T) {
		c, _ := fake(t, "not JSON")
		_, err := c.View(context.Background())
		if err == nil || !strings.Contains(err.Error(), "not JSON") {
			t.Fatalf("error %v", err)
		}
	})
	t.Run("missing IDs", func(t *testing.T) {
		c, _ := fake(t, "{}")
		if _, err := c.Split(context.Background(), "a", "right", .5, nil); err == nil {
			t.Fatal("missing split ID")
		}
		if _, _, err := c.Float(context.Background(), "w", [4]float64{0, 0, 1, 1}, nil); err == nil {
			t.Fatal("missing float IDs")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		c, _ := fake(t, "{}")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := c.Focus(ctx, "b"); !errors.Is(err, context.Canceled) {
			t.Fatalf("error %v", err)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		c, _ := fake(t, "{}")
		if err := os.WriteFile(c.Executable, []byte("#!/bin/sh\nexec sleep 10\n"), 0700); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		start := time.Now()
		if err := c.Focus(ctx, "b"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error %v", err)
		}
		if time.Since(start) > time.Second {
			t.Fatal("cancellation was not prompt")
		}
	})
}
