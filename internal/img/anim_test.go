package img

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
	"time"

	"github.com/kettek/apng"
)

func solid(c color.RGBA, w, h int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, c)
		}
	}
	return m
}

// TestDecodeAnimatedGIF checks the player's view of a GIF: composited frames,
// their delays and the total duration.
func TestDecodeAnimatedGIF(t *testing.T) {
	pal := color.Palette{color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}}
	p0 := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	p1 := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	for i := range p1.Pix {
		p1.Pix[i] = 1
	}
	var buf bytes.Buffer
	err := gif.EncodeAll(&buf, &gif.GIF{
		Image:  []*image.Paletted{p0, p1},
		Delay:  []int{5, 5},
		Config: image.Config{Width: 4, Height: 4, ColorModel: pal},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, anim, err := DecodeAnimated(buf.Bytes())
	if err != nil || anim == nil {
		t.Fatalf("anim = %v, err = %v", anim, err)
	}
	if first.W != 4 || first.H != 4 {
		t.Fatalf("first frame %dx%d", first.W, first.H)
	}
	if len(anim.Frames) != 2 || anim.Duration() != 100*time.Millisecond {
		t.Fatalf("frames = %d, duration = %v", len(anim.Frames), anim.Duration())
	}
	if anim.Frames[0].Pix[0] != 255 || anim.Frames[0].Pix[2] != 0 {
		t.Fatalf("frame 0 = %v, want red", anim.Frames[0].Pix[:4])
	}
	if anim.Frames[1].Pix[2] != 255 {
		t.Fatalf("frame 1 = %v, want blue", anim.Frames[1].Pix[:4])
	}
	if i, done := anim.FrameAt(60 * time.Millisecond); i != 1 || done {
		t.Fatalf("at 60ms = frame %d done=%v, want the second frame", i, done)
	}
}

// TestDecodeAnimatedAPNG checks the same for an APNG, including a sub-frame
// with a background dispose: the next frame does not keep its pixels.
func TestDecodeAnimatedAPNG(t *testing.T) {
	red := solid(color.RGBA{255, 0, 0, 255}, 4, 4)
	green := solid(color.RGBA{0, 255, 0, 255}, 4, 4)
	blue := solid(color.RGBA{0, 0, 255, 255}, 2, 2)
	var buf bytes.Buffer
	err := apng.Encode(&buf, apng.APNG{Frames: []apng.Frame{
		{Image: red, DelayNumerator: 1, DelayDenominator: 10},
		{Image: green, DelayNumerator: 1, DelayDenominator: 10},
		{Image: blue, XOffset: 1, YOffset: 1, DelayNumerator: 1, DelayDenominator: 10, DisposeOp: apng.DISPOSE_OP_BACKGROUND},
		{Image: solid(color.RGBA{255, 0, 0, 255}, 1, 1), DelayNumerator: 1, DelayDenominator: 10},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, anim, err := DecodeAnimated(buf.Bytes())
	if err != nil || anim == nil {
		t.Fatalf("anim = %v, err = %v", anim, err)
	}
	if len(anim.Frames) != 4 {
		t.Fatalf("frames = %d, want 4", len(anim.Frames))
	}
	// The sub-frame only covers the middle; pixel (0,0) stays green.
	if anim.Frames[2].Pix[0] != 0 || anim.Frames[2].Pix[1] != 255 {
		t.Fatalf("sub-frame corner = %v, want the previous green", anim.Frames[2].Pix[:4])
	}
	// Its middle is blue.
	i := (1*4 + 1) * 4
	if anim.Frames[2].Pix[i+2] != 255 {
		t.Fatalf("sub-frame middle = %v, want blue", anim.Frames[2].Pix[i:i+4])
	}
	// After the background dispose, the cleared rect does not keep the blue.
	if anim.Frames[3].Pix[i+3] != 0 || anim.Frames[3].Pix[i+2] == 255 {
		t.Fatalf("frame after dispose = %v, want the rect cleared", anim.Frames[3].Pix[i:i+4])
	}
}

// TestAnimFrameAtLoops checks the clock: an infinite loop wraps, and a finite
// one reports when it has played its passes.
func TestAnimFrameAtLoops(t *testing.T) {
	frames := []AnimFrame{{Delay: 100 * time.Millisecond}, {Delay: 100 * time.Millisecond}}
	forever := &Anim{Frames: frames}
	if i, done := forever.FrameAt(250 * time.Millisecond); i != 0 || done {
		t.Fatalf("wrapped to frame %d done=%v, want frame 0 still playing", i, done)
	}
	twice := &Anim{Frames: frames, Loop: 2}
	if i, done := twice.FrameAt(399 * time.Millisecond); i != 1 || done {
		t.Fatalf("at 399ms = frame %d done=%v, want the last frame playing", i, done)
	}
	if i, done := twice.FrameAt(400 * time.Millisecond); i != 1 || !done {
		t.Fatalf("at 400ms = frame %d done=%v, want it finished", i, done)
	}
}

// TestStillImageHasNoAnim keeps every other format on the still path.
func TestStillImageHasNoAnim(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, solid(color.RGBA{1, 2, 3, 255}, 4, 4)); err != nil {
		t.Fatal(err)
	}
	_, anim, err := DecodeAnimated(buf.Bytes())
	if err != nil || anim != nil {
		t.Fatalf("anim = %v, err = %v, want a still image", anim, err)
	}
}
