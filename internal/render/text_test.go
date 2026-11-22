//go:build !nogpu

package render

import (
	"image"
	"math"
	"testing"

	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/text"
)

// TestGlyphDensityFollowsTheTransform pins the rasterization resolution: a
// scaled or turned op must not sample a bitmap drawn at a different size.
func TestGlyphDensityFollowsTheTransform(t *testing.T) {
	tests := []struct {
		name string
		op   paint.Op
		want float64
	}{
		{"plain", paint.Op{}, 1},
		{"scaled", paint.Op{Scale: [3]float32{2, 2, 1}}, 2},
		{"shrunk", paint.Op{Scale: [3]float32{0.5, 0.5, 1}}, 1},
		{"quarter turn", paint.Op{Rotate: float32(math.Pi / 2)}, 1},
		{"turned", paint.Op{Rotate: float32(math.Pi / 4)}, 2},
		{"turned and scaled", paint.Op{Rotate: 0.3, Scale: [3]float32{2, 2, 1}}, 4},
	}
	for _, c := range tests {
		if got := glyphDensity(c.op, 1); got != c.want {
			t.Errorf("%s density = %v, want %v", c.name, got, c.want)
		}
	}
	if got := glyphDensity(paint.Op{Scale: [3]float32{10, 10, 1}}, 1); got != 4 {
		t.Errorf("capped density = %v, want 4", got)
	}
	if got := glyphDensity(paint.Op{}, 2); got != 2 {
		t.Errorf("hi-dpi density = %v, want 2", got)
	}
	if got := glyphDensity(paint.Op{Rotate: 0.3}, 2); got != 4 {
		t.Errorf("hi-dpi turned density = %v, want 4", got)
	}
}

// TestGlyphRectSnapsToTheDeviceGrid keeps the drawn position on a physical
// pixel even when the glyph rasterized at another density. The size comes from
// the raster density, so a 2x bitmap draws at its logical size.
func TestGlyphRectSnapsToTheDeviceGrid(t *testing.T) {
	slot := text.GlyphSlot{Offset: image.Pt(1, 2), Rect: image.Rect(0, 0, 8, 16)}
	got := glyphRect(10.3, 20.6, 2, 1, slot)
	want := [4]float32{11, 22, 4, 8}
	if got != want {
		t.Fatalf("rect = %v, want %v", got, want)
	}
}
