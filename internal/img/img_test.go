package img

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestDecodePNGAndScale(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			src.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	d, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if d.W != 8 || d.H != 4 || len(d.Pix) != 8*4*4 {
		t.Fatalf("decoded %dx%d with %d bytes", d.W, d.H, len(d.Pix))
	}
	if d.Pix[0] != 10 || d.Pix[3] != 255 {
		t.Fatalf("pixel = %v", d.Pix[:4])
	}

	small := Scale(d, 2, 1)
	if small.W != 2 || small.H != 1 {
		t.Fatalf("scaled to %dx%d", small.W, small.H)
	}
	if small.Pix[0] != 10 || small.Pix[3] != 255 {
		t.Fatalf("scaled pixel = %v", small.Pix[:4])
	}

	if _, err := Decode([]byte("not an image")); err == nil {
		t.Fatal("garbage decoded")
	}
}

func TestShelfWrapsRows(t *testing.T) {
	s := Shelf{W: 10, H: 10}
	if x, y, ok := s.Place(6, 4); !ok || x != 0 || y != 0 {
		t.Fatalf("first = %d,%d %v", x, y, ok)
	}
	// 6 wide does not fit beside the first, so it moves to a new shelf.
	if x, y, ok := s.Place(6, 4); !ok || x != 0 || y != 4 {
		t.Fatalf("second = %d,%d %v", x, y, ok)
	}
	if _, _, ok := s.Place(6, 6); ok {
		t.Fatal("placed an image past the bottom")
	}
	if _, _, ok := s.Place(11, 1); ok {
		t.Fatal("placed an image wider than the atlas")
	}
}

// BenchmarkScaleVsScaleInto is the video frame's per-frame cost: the allocating
// form re-sizes a 1280x720 frame to its packed 640x360 copy, and the scratch
// form writes into the buffer the renderer already has.
func BenchmarkScaleVsScaleInto(b *testing.B) {
	src := &Data{W: 1280, H: 720, Pix: make([]byte, 1280*720*4)}
	b.Run("Scale", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = Scale(src, 640, 360)
		}
	})
	dst := &Data{W: 640, H: 360, Pix: make([]byte, 640*360*4)}
	b.Run("ScaleInto", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ScaleInto(src, dst)
		}
	})
}
