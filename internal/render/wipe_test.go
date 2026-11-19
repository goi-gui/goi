//go:build !nogpu

package render

import "testing"

// TestWipeInstances: the two blur passes carry the band and the progressive
// flag, the second pass is vertical, and the blit samples their result.
func TestWipeInstances(t *testing.T) {
	r := &wipeRequest{band: [4]float32{0, 100, 400, 200}, blur: 10}
	got := wipeInstances(r, 400, 500)
	if len(got) != 3 {
		t.Fatalf("instances = %d, want 3", len(got))
	}
	for i, axis := range []float32{1, 2} {
		if got[i].Params[0] != 12 || got[i].Params[1] != 10 {
			t.Fatalf("blur %d params = %v", i, got[i].Params)
		}
		if got[i].Params[2] != 100 || got[i].Params[3] != 300 {
			t.Fatalf("blur %d band = %v", i, got[i].Params[2:])
		}
		if got[i].Misc[0] != axis || got[i].Misc[1] != 1 {
			t.Fatalf("blur %d misc = %v, want axis %v and progressive", i, got[i].Misc, axis)
		}
		if got[i].Rect != [4]float32{0, 0, 400, 500} {
			t.Fatalf("blur %d rect = %v, want the whole window", i, got[i].Rect)
		}
	}
	if got[2].Params[0] != 11 {
		t.Fatalf("blit kind = %v, want a texture composite", got[2].Params[0])
	}
}
