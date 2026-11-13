package rex

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestResizeLayer(t *testing.T) {
	for _, bounds := range [][4]float64{{0, 0, 1, 1}, {.2, .15, .5, .6}} {
		c, read := fake(t, "{}")
		if err := c.ResizeLayer(context.Background(), "layer", bounds); err != nil {
			t.Fatal(err)
		}
		got := apiArgs(t, read, "session.resize_layer")
		want := map[string]any{"layer_id": "layer", "bounds": []any{bounds[0], bounds[1], bounds[2], bounds[3]}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

func TestLayerBoundsValidation(t *testing.T) {
	c := New("session")
	c.Executable = "/must-not-execute-rex"
	invalid := [][4]float64{
		{-1, 0, 1, 1}, {0, -1, 1, 1}, {0, 0, 0, 1}, {0, 0, 1, 0},
		{0, 0, -1, 1}, {0, 0, 1, -1}, {.5, 0, 1, 1}, {0, .5, 1, 1},
	}
	for i := 0; i < 4; i++ {
		for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			b := [4]float64{0, 0, 1, 1}
			b[i] = v
			invalid = append(invalid, b)
		}
	}
	for _, b := range invalid {
		if err := c.ResizeLayer(context.Background(), "layer", b); err == nil || !strings.HasPrefix(err.Error(), "rex resize layer: bounds") {
			t.Fatalf("resize %v: %v", b, err)
		}
		if _, _, err := c.Float(context.Background(), "window", b, nil); err == nil || !strings.HasPrefix(err.Error(), "rex float: bounds") {
			t.Fatalf("float %v: %v", b, err)
		}
	}
	if err := c.ResizeLayer(context.Background(), "", [4]float64{0, 0, 1, 1}); err == nil || err.Error() != "rex resize layer: layer is required" {
		t.Fatalf("missing layer: %v", err)
	}
}

func TestResizeLayerError(t *testing.T) {
	c, _ := fake(t, "{}")
	t.Setenv("REX_TEST_EXIT", "1")
	t.Setenv("REX_TEST_STDERR", "not a floating layer")
	if err := c.ResizeLayer(context.Background(), "layer", [4]float64{0, 0, 1, 1}); err == nil || !strings.Contains(err.Error(), "session.resize_layer") || !strings.Contains(err.Error(), "not a floating layer") {
		t.Fatalf("error: %v", err)
	}
}
