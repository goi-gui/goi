//go:build !nogpu

package render

import (
	"fmt"
	"image"
	"math"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// textRenderer owns the glyph atlas. Glyph quads go into the same instance
// buffer as shapes (kind 4), so paint order is exactly the display-list order
// and the window stays one draw call.
//
// A frame runs in two passes. `prepare` shapes every text op and resolves its
// glyph slots, which is what lets the atlas compact safely: only glyphs the
// frame has already asked for survive, so a rebuild can never pull the texture
// out from under an instance that is about to be emitted.
type textRenderer struct {
	texture *wgpu.Texture
	view    *wgpu.TextureView
	sampler *wgpu.Sampler

	atlasW, atlasH int
	atlasDirty     bool

	// quads holds this frame's glyphs, and spans maps a text op index to its
	// slice of them. Both are reused between frames.
	quads  []glyphQuad
	spans  []glyphSpan
	placed []placedGlyph

	gpu *GPU // back-reference for buffers and the atlas library
}

// glyphQuad is one rasterized glyph, ready to become an instance. The atlas
// rect stays in pixels so the UVs can be computed after any growth.
type glyphQuad struct {
	rect    [4]float32
	slot    image.Rectangle
	color   [4]float32
	clip    [4]float32
	clipBox [4]float32
	clipR   float32
}

// placedGlyph is one glyph's quad and atlas slot, before the shadow copies are
// derived from it.
type placedGlyph struct {
	rect [4]float32
	slot image.Rectangle
}

// glyphSpan is a text op's range inside textRenderer.quads.
type glyphSpan struct{ start, n int }

func (g *GPU) initText(lib *text.Library) error {
	if lib == nil || lib.Atlas() == nil {
		return nil // no font: text is skipped, the app still runs
	}
	atlas := lib.Atlas()
	t := &textRenderer{atlasW: atlas.W, atlasH: atlas.H, atlasDirty: true, gpu: g}

	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "glyph-atlas",
		Size:          wgpu.Extent3D{Width: uint32(atlas.W), Height: uint32(atlas.H), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        gputypes.TextureFormatR8Unorm,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("atlas texture: %w", err)
	}
	t.texture = tex
	t.view, err = g.device.CreateTextureView(tex, nil)
	if err != nil {
		return fmt.Errorf("atlas view: %w", err)
	}
	t.sampler, err = g.device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        "atlas-sampler",
		AddressModeU: gputypes.AddressModeClampToEdge,
		AddressModeV: gputypes.AddressModeClampToEdge,
		AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
	})
	if err != nil {
		return fmt.Errorf("atlas sampler: %w", err)
	}

	g.text = t
	g.textLib = lib
	return nil
}

// prepare shapes every text op in the list and resolves its glyph slots. It
// fills quads/spans for emit and, at the end, notices an atlas that grew.
func (t *textRenderer) prepare(list *paint.List) {
	lib := t.gpu.textLib
	if lib == nil || lib.Atlas() == nil {
		return
	}
	atlas := lib.Atlas()
	atlas.BeginFrame()

	t.quads = t.quads[:0]
	t.spans = t.spans[:0]
	for _, op := range list.Ops {
		start, n := len(t.quads), 0
		if op.Kind == paint.Text {
			n = t.shapeOp(op, atlas)
		}
		t.spans = append(t.spans, glyphSpan{start: start, n: n})
	}

	// Any compaction or growth during the pass is finished, so the pixel size
	// the UVs are measured against is final for this frame.
	t.atlasW, t.atlasH = atlas.W, atlas.H
	if !atlas.Dirty.Empty() {
		t.atlasDirty = true
		atlas.ClearDirty()
	}
}

// shapeOp appends the glyph quads for one text op and returns how many.
func (t *textRenderer) shapeOp(op paint.Op, atlas *text.Atlas) int {
	if op.Text == "" {
		return 0
	}
	lib := t.gpu.textLib

	// Rasterize at the device resolution, then snap each glyph quad to the
	// physical pixel grid. Without the snap, a centred run lands on a half
	// pixel and the linear sampler smears every stem across two columns.
	scale := float64(t.gpu.scale)
	if scale <= 0 {
		scale = 1
	}
	var face *text.Face
	if op.Icon {
		face = lib.IconFace(float64(op.FontSize) * scale)
	} else {
		face = lib.Face(float64(op.FontSize)*scale, text.Weight(op.Weight))
	}
	if face == nil {
		return 0
	}
	out := face.Shape(op.Text)
	penX := float64(op.TextX)
	runW := float64(out.Advance) / 64 / scale
	boxW := float64(op.BoxW)
	switch op.Align {
	case paint.AlignCenter:
		penX += (boxW - runW) / 2
	case paint.AlignEnd:
		penX += boxW - runW
	}
	penY := math.Round(float64(op.TextY) * scale)
	col := rgba(op.Color)
	clip := clipVec(op.Clip)
	clipBox := clipVec(op.ClipBox)

	placed := t.placed[:0]
	for _, gl := range out.Glyphs {
		if slot, ok := atlas.Slot(face, uint16(gl.GlyphID)); ok {
			x := math.Round((penX+float64(slot.Offset.X)/scale)*scale) / scale
			y := (penY + float64(slot.Offset.Y)) / scale
			w := float64(slot.Rect.Dx()) / scale
			h := float64(slot.Rect.Dy()) / scale
			placed = append(placed, placedGlyph{
				rect: [4]float32{float32(x), float32(y), float32(w), float32(h)},
				slot: slot.Rect,
			})
		}
		penX += float64(gl.Advance) / 64 / scale
	}
	t.placed = placed

	// A text shadow is the whole run offset, painted before the glyphs so the
	// glyphs sit on top, exactly as CSS paints text-shadow. Blur is a handful of
	// offset copies rather than atlas taps: the atlas packs glyphs edge to edge,
	// so sampling past a slot would pick up its neighbour.
	n := 0
	for _, sh := range op.TextShadows {
		for _, tap := range shadowTaps(sh, scale) {
			scol := rgba(scaleAlpha(sh.Color, tap.alpha))
			for _, p := range placed {
				t.quads = append(t.quads, glyphQuad{
					rect:    [4]float32{p.rect[0] + tap.dx, p.rect[1] + tap.dy, p.rect[2], p.rect[3]},
					slot:    p.slot,
					color:   scol,
					clip:    clip,
					clipBox: clipBox,
					clipR:   op.ClipRadius,
				})
				n++
			}
		}
	}
	for _, p := range placed {
		t.quads = append(t.quads, glyphQuad{
			rect:    p.rect,
			slot:    p.slot,
			color:   col,
			clip:    clip,
			clipBox: clipBox,
			clipR:   op.ClipRadius,
		})
	}
	return n + len(placed)
}

// scaleAlpha multiplies a colour's alpha, keeping its channels.
func scaleAlpha(c uint32, a float32) uint32 {
	old := float32(c & 0xFF)
	na := uint32(old*a + 0.5)
	if na > 0xFF {
		na = 0xFF
	}
	return c&0xFFFFFF00 | na
}

// shadowTap is one copy of a glyph run that stands in for a blurred shadow.
type shadowTap struct {
	dx, dy, alpha float32
}

// shadowRings is how many copies a blurred shadow is built from: two rings plus
// the centre, which reads as a soft edge without a second pass.
var shadowRings = []struct {
	radius, weight float32
	stops          int
}{
	{0, 1.0, 1},
	{0.45, 0.55, 6},
	{0.85, 0.4, 10},
}

// shadowTaps lays a shadow layer out as offset copies. A layer without blur is a
// single copy, which is the hard shadow CSS draws for `text-shadow: x y color`.
func shadowTaps(sh tree.ShadowLayer, scale float64) []shadowTap {
	if sh.Blur <= 0 {
		return []shadowTap{{dx: float32(sh.X), dy: float32(sh.Y), alpha: 1}}
	}
	blur := float32(sh.Blur) * float32(scale)
	var total float32
	for _, r := range shadowRings {
		total += r.weight * float32(r.stops)
	}
	out := make([]shadowTap, 0, int(total))
	for _, r := range shadowRings {
		for i := 0; i < r.stops; i++ {
			ang := 2 * math.Pi * float64(i) / float64(r.stops)
			out = append(out, shadowTap{
				dx:    float32(sh.X) + blur*r.radius*float32(math.Cos(ang)),
				dy:    float32(sh.Y) + blur*r.radius*float32(math.Sin(ang)),
				alpha: r.weight / total * 2,
			})
		}
	}
	return out
}

// emit appends the quads prepared for op i, in display-list order.
func (t *textRenderer) emit(i int) {
	if i < 0 || i >= len(t.spans) {
		return
	}
	sp := t.spans[i]
	g := t.gpu
	for _, q := range t.quads[sp.start : sp.start+sp.n] {
		g.instances = append(g.instances, gpuInstance{
			Rect:    q.rect,
			Color:   q.color,
			Clip:    q.clip,
			ClipBox: q.clipBox,
			// kind 4 is a glyph quad: params.zw is the UV origin, misc.xy is
			// the UV far corner and misc.z the rounded clip.
			Params: [4]float32{
				4, 0,
				float32(q.slot.Min.X) / float32(t.atlasW),
				float32(q.slot.Min.Y) / float32(t.atlasH),
			},
			Misc: [4]float32{
				float32(q.slot.Max.X) / float32(t.atlasW),
				float32(q.slot.Max.Y) / float32(t.atlasH), q.clipR, 0,
			},
		})
	}
}

// upload pushes the atlas if it changed since the last frame. A compaction or
// growth replaces the bitmap, so the texture is rebuilt at the new size first.
func (t *textRenderer) upload(g *GPU) error {
	atlas := g.textLib.Atlas()
	if atlas.Resized || atlas.W != t.atlasW || atlas.H != t.atlasH {
		if err := t.recreate(atlas); err != nil {
			return err
		}
	}
	if !t.atlasDirty {
		return nil
	}
	t.atlasDirty = false
	return g.queue.WriteTexture(
		&wgpu.ImageCopyTexture{Texture: t.texture},
		atlas.Img.Pix,
		&wgpu.ImageDataLayout{Offset: 0, BytesPerRow: uint32(atlas.Img.Stride), RowsPerImage: uint32(t.atlasH)},
		&wgpu.Extent3D{Width: uint32(t.atlasW), Height: uint32(t.atlasH), DepthOrArrayLayers: 1},
	)
}

// recreate swaps the glyph texture for one matching the atlas' current size.
func (t *textRenderer) recreate(atlas *text.Atlas) error {
	tex, err := t.gpu.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "glyph-atlas",
		Size:          wgpu.Extent3D{Width: uint32(atlas.W), Height: uint32(atlas.H), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        gputypes.TextureFormatR8Unorm,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("atlas texture: %w", err)
	}
	view, err := t.gpu.device.CreateTextureView(tex, nil)
	if err != nil {
		tex.Release()
		return fmt.Errorf("atlas view: %w", err)
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.texture != nil {
		t.texture.Release()
	}
	t.texture, t.view = tex, view
	t.atlasW, t.atlasH = atlas.W, atlas.H
	atlas.Resized = false
	t.atlasDirty = true
	return t.gpu.rebind()
}

func (t *textRenderer) release() {
	if t.sampler != nil {
		t.sampler.Release()
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.texture != nil {
		t.texture.Release()
	}
}
