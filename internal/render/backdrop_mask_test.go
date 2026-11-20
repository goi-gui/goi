//go:build !nogpu

package render

import "testing"

// TestBackdropMaskInstance: a blur gradient marks the composite with its
// gradient record; a scalar blur leaves the flag clear.
func TestBackdropMaskInstance(t *testing.T) {
	panel := backdropPanel{rect: [4]float32{0, 0, 200, 100}, blur: 6, alpha: 1, grad: 3}
	inst := backdropInstances(panel, 400, 300)
	if got := inst[0].Misc[0]; got != 1 {
		t.Fatalf("horizontal axis = %v, want 1", got)
	}
	if got := inst[1].Misc[0]; got != 2 {
		t.Fatalf("vertical axis = %v, want 2", got)
	}
	if got := inst[2].Params; got[0] != 11 || got[1] != 3 || got[2] != 1 {
		t.Fatalf("composite params = %v", got)
	}
	panel.grad = -1
	inst = backdropInstances(panel, 400, 300)
	if got := inst[2].Params; got[1] != 0 || got[2] != 0 {
		t.Fatalf("scalar composite params = %v", got)
	}
}

// TestBackdropBlurRounds repeats a smaller radius for a large blur instead of
// stretching one 9-tap pass across it.
func TestBackdropBlurRounds(t *testing.T) {
	panel := backdropPanel{rect: [4]float32{0, 0, 200, 100}, blur: 100, alpha: 1, grad: -1}
	panel.rounds = blurRounds(panel.blur)
	if panel.rounds != 4 {
		t.Fatalf("rounds = %d, want 4", panel.rounds)
	}
	inst := backdropInstances(panel, 400, 300)
	if len(inst) != 2*panel.rounds+1 {
		t.Fatalf("instances = %d, want %d", len(inst), 2*panel.rounds+1)
	}
	if got := inst[0].Params[1]; got != 50 {
		t.Fatalf("per-pass radius = %v, want 50", got)
	}
	for i := 0; i < 2*panel.rounds; i++ {
		want := float32(1)
		if i%2 == 1 {
			want = 2
		}
		if got := inst[i].Misc[0]; got != want {
			t.Fatalf("pass %d axis = %v, want %v", i, got, want)
		}
	}
	if got := inst[len(inst)-1].Params[0]; got != 11 {
		t.Fatalf("last instance kind = %v, want the composite", got)
	}
}
