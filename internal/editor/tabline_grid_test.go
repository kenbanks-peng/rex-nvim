package editor

import "testing"

func TestNeovimGridTablineMeasurement(t *testing.T) {
	m, _ := floatManager(t)
	if err := m.nv.ExecLua(configure, nil); err != nil {
		t.Fatal(err)
	}
	for _, tabs := range []int{1, 2} {
		if tabs == 2 {
			if err := m.nv.Command("tabnew"); err != nil {
				t.Fatal(err)
			}
		}
		for _, mode := range []int{0, 1, 2} {
			if err := m.nv.SetOption("showtabline", mode); err != nil {
				t.Fatal(err)
			}
			win, err := m.nv.CurrentWindow()
			if err != nil {
				t.Fatal(err)
			}
			var measured []int
			if err := m.nv.ExecLua(measureLayout, &measured, [][]int{{int(win), 80, 20}}); err != nil {
				t.Fatal(err)
			}
			want := 21
			if mode == 2 || (mode == 1 && tabs > 1) {
				want++
			}
			if measured[1] != want || measured[4] != tabs {
				t.Fatalf("grid fallback tabs=%d showtabline=%d: %v want height=%d", tabs, mode, measured, want)
			}
		}
	}
}
