package rex

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestSetRatio(t *testing.T) {
	for _, direction := range []string{"horizontal", "vertical"} {
		t.Run(direction, func(t *testing.T) {
			c, read := fake(t, "{}")
			if err := c.SetRatio(context.Background(), "a", "b", direction, .3725); err != nil {
				t.Fatal(err)
			}
			got := apiArgs(t, read, "session.resize_pane")
			want := map[string]any{"before_block_id": "a", "after_block_id": "b", "direction": direction, "ratio": .3725}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}

func TestSetRatioValidation(t *testing.T) {
	c := New("work session")
	c.Executable = "/must-not-execute-rex"
	for _, tc := range []struct {
		before, after, direction string
		ratio                    float64
	}{
		{"", "b", "horizontal", .5},
		{"a", "", "horizontal", .5},
		{"a", "a", "horizontal", .5},
		{"a", "b", "right", .5},
		{"a", "b", "horizontal", 0},
		{"a", "b", "horizontal", 1},
		{"a", "b", "horizontal", -.1},
		{"a", "b", "horizontal", 1.1},
		{"a", "b", "horizontal", math.NaN()},
		{"a", "b", "horizontal", math.Inf(1)},
		{"a", "b", "horizontal", math.Inf(-1)},
	} {
		if err := c.SetRatio(context.Background(), tc.before, tc.after, tc.direction, tc.ratio); err == nil || !strings.HasPrefix(err.Error(), "rex set ratio:") {
			t.Fatalf("accepted %#v", tc)
		}
	}
}
