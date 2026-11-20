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
