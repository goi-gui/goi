package img

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"time"

	"github.com/kettek/apng"
)

// An animation is decoded to full-canvas RGBA frames, so playing it is a swap
// of Pix and needs no per-frame compositing.
type Anim struct {
	W, H   int
	Frames []AnimFrame
	// Loop is how many times the animation plays; 0 means forever.
	Loop int
}

// AnimFrame is one composited frame and how long it stays on screen.
type AnimFrame struct {
	Pix   []byte
	Delay time.Duration
}

// Duration is the length of one pass through the animation.
func (a *Anim) Duration() time.Duration {
	var d time.Duration
	for _, f := range a.Frames {
		d += f.Delay
	}
	return d
}

// FrameAt returns the frame at elapsed and whether the animation has played all
// its passes. A frame's delay is its time on screen, so the lookup is a scan of
// the delays; animations have few frames, and a player calls this once a frame.
func (a *Anim) FrameAt(elapsed time.Duration) (int, bool) {
	pass := a.Duration()
	if pass <= 0 {
		return 0, true
	}
	if a.Loop > 0 && elapsed >= pass*time.Duration(a.Loop) {
		return len(a.Frames) - 1, true
	}
	t := elapsed % pass
	for i, f := range a.Frames {
		if t < f.Delay {
			return i, false
		}
		t -= f.Delay
	}
	return len(a.Frames) - 1, false
}

// Limits on a decoded animation. A remote file is untrusted, and a GIF may
// declare thousands of large frames; past these it degrades to its first frame
// instead of exhausting memory.
const (
	maxAnimFrames = 512
	maxAnimBytes  = 128 << 20
)

// DecodeAnimated decodes an image file. anim is non-nil only for an animated
// GIF or APNG, and first is its first frame either way.
func DecodeAnimated(b []byte) (first *Data, anim *Anim, err error) {
	if a, ok := decodeGIF(b); ok {
		return animFirst(a), a, nil
	}
	if isPNG(b) {
		a, err := decodeAPNG(b)
		if err != nil {
			return nil, nil, err
		}
		if a != nil {
			return animFirst(a), a, nil
		}
	}
	d, err := Decode(b)
	if err != nil {
		return nil, nil, err
	}
	return d, nil, nil
}

// animFirst is an animation's first frame as a still bitmap, which is what the
// node draws before the first tick and what a caller that ignores anim sees.
func animFirst(a *Anim) *Data { return &Data{W: a.W, H: a.H, Pix: a.Frames[0].Pix} }

func isPNG(b []byte) bool {
	return len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n"))
}

// decodeGIF returns the composited animation, or nil for a still GIF.
func decodeGIF(b []byte) (*Anim, bool) {
	g, err := gif.DecodeAll(bytes.NewReader(b))
	if err != nil || len(g.Image) < 2 {
		return nil, false
	}
	if len(g.Image) > maxAnimFrames || len(g.Image)*g.Config.Width*g.Config.Height*4 > maxAnimBytes {
		return nil, false
	}
	frames := make([]animFrame, len(g.Image))
	for i, f := range g.Image {
		delay := time.Duration(g.Delay[i]) * 10 * time.Millisecond
		if delay <= 0 {
			// A zero delay is a still frame; browsers hold it for 100ms so the
			// animation does not spin as fast as the loop allows.
			delay = 100 * time.Millisecond
		}
		dispose := disposeNone
		switch g.Disposal[i] {
		case gif.DisposalBackground:
			dispose = disposeBackground
		case gif.DisposalPrevious:
			dispose = disposePrevious
		}
		frames[i] = animFrame{img: f, x: f.Bounds().Min.X, y: f.Bounds().Min.Y, delay: delay, dispose: dispose}
	}
	loop := 1 // gif.LoopCount of -1 shows each frame once
	if g.LoopCount == 0 {
		loop = 0
	} else if g.LoopCount > 0 {
		loop = g.LoopCount + 1
	}
	return compose(g.Config.Width, g.Config.Height, frames, loop), true
}

// decodeAPNG returns the animation, or nil when the file is a plain PNG. The
// decoder also reads a plain PNG, so it is only used when the file has an acTL
// chunk.
func decodeAPNG(b []byte) (*Anim, error) {
	if !bytes.Contains(b, []byte("acTL")) {
		return nil, nil
	}
	a, err := apng.DecodeAll(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("apng: %w", err)
	}
	if len(a.Frames) < 2 {
		return nil, nil
	}
	if len(a.Frames) > maxAnimFrames || len(a.Frames)*frameBytes(a.Frames) > maxAnimBytes {
		return nil, nil
	}
	// The canvas is the default image's size; APNG guarantees it covers every
	// frame, so a frame's bounds never exceed it.
	bd := a.Frames[0].Image.Bounds()
	frames := make([]animFrame, 0, len(a.Frames))
	for _, f := range a.Frames {
		if f.IsDefault {
			continue // the poster image, not part of the animation
		}
		delay := time.Duration(f.GetDelay() * float64(time.Second))
		if delay <= 0 {
			delay = 100 * time.Millisecond
		}
		blend := blendOver
		if f.BlendOp == apng.BLEND_OP_SOURCE {
			blend = blendSource
		}
		dispose := disposeNone
		switch f.DisposeOp {
		case apng.DISPOSE_OP_BACKGROUND:
			dispose = disposeBackground
		case apng.DISPOSE_OP_PREVIOUS:
			dispose = disposePrevious
		}
		frames = append(frames, animFrame{img: f.Image, x: f.XOffset, y: f.YOffset, delay: delay, blend: blend, dispose: dispose})
	}
	if len(frames) < 2 {
		return nil, nil
	}
	loop := int(a.LoopCount)
	return compose(bd.Dx(), bd.Dy(), frames, loop), nil
}

// frameBytes estimates one frame's size for the memory guard.
func frameBytes(frames []apng.Frame) int {
	for _, f := range frames {
		if !f.IsDefault {
			bd := f.Image.Bounds()
			return bd.Dx() * bd.Dy() * 4
		}
	}
	return 4
}

type disposeOp uint8

const (
	disposeNone disposeOp = iota
	disposeBackground
	disposePrevious
)

type blendOp uint8

const (
	blendOver blendOp = iota
	blendSource
)

// animFrame is one decoded frame before compositing: a sub-image at an offset,
// with the blend and dispose rules its format gave it.
type animFrame struct {
	img     image.Image
	x, y    int
	delay   time.Duration
	blend   blendOp
	dispose disposeOp
}

// compose flattens sub-frames onto one canvas, following the APNG/GIF rules:
// blend decides replace or over, and dispose decides what the next frame finds.
func compose(w, h int, frames []animFrame, loop int) *Anim {
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	out := &Anim{W: w, H: h, Loop: loop, Frames: make([]AnimFrame, len(frames))}
	for i, f := range frames {
		var saved *image.RGBA
		if f.dispose == disposePrevious {
			saved = cloneRGBA(canvas)
		}
		bd := f.img.Bounds()
		r := image.Rect(f.x, f.y, f.x+bd.Dx(), f.y+bd.Dy()).Intersect(canvas.Bounds())
		op := draw.Over
		if f.blend == blendSource {
			op = draw.Src
		}
		if !r.Empty() {
			draw.Draw(canvas, r, f.img, bd.Min.Add(image.Pt(r.Min.X-f.x, r.Min.Y-f.y)), op)
		}
		out.Frames[i] = AnimFrame{Pix: append([]byte(nil), canvas.Pix...), Delay: f.delay}
		switch f.dispose {
		case disposeBackground:
			for y := r.Min.Y; y < r.Max.Y; y++ {
				clear(canvas.Pix[y*canvas.Stride+r.Min.X*4 : y*canvas.Stride+r.Max.X*4])
			}
		case disposePrevious:
			canvas = saved
		}
	}
	return out
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Rect)
	copy(dst.Pix, src.Pix)
	return dst
}
