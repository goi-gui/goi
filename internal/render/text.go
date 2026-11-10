//go:build !nogpu

package render

import (
	"fmt"
	"image"
	"math"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/text"
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
	// slice of them. Both are reused between frames. emoji holds the colour
	// glyphs of an op, which draw from the image atlas instead of the coverage
	// one, and emojiData caches their decoded bitmaps by size and glyph.
	quads     []glyphQuad
	spans     []glyphSpan
	placed    []placedGlyph
	emoji     []emojiQuad
	emojiSpan []glyphSpan
	emojiData map[uint32]emojiBitmap

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
	rot     [4]float32
}

// placedGlyph is one glyph's quad and atlas slot, before the shadow copies are
// derived from it. pen is the position the glyph was advanced to, so a shadow
// can resolve its own (blurred) atlas slot at the same spot.
type placedGlyph struct {
	rect [4]float32
	slot image.Rectangle
	gid  uint16
	pen  float64
}

// glyphSpan is a text op's range inside textRenderer.quads.
type glyphSpan struct{ start, n int }

// emojiBitmap is one decoded colour glyph and its offset from the pen.
type emojiBitmap struct {
	data *img.Data
	off  image.Point
}

// emojiQuad is one colour glyph, drawn from the image atlas like a picture.
type emojiQuad struct {
	rect    [4]float32
	data    *img.Data
	clip    [4]float32
	clipBox [4]float32
	clipR   float32
	rot     [4]float32
}

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
	t.emoji = t.emoji[:0]
	t.emojiSpan = t.emojiSpan[:0]
	for _, op := range list.Ops {
		start, n := len(t.quads), 0
		estart, en := len(t.emoji), 0
		if op.Kind == paint.Text {
			n, en = t.shapeOp(op, atlas)
		}
		t.spans = append(t.spans, glyphSpan{start: start, n: n})
		t.emojiSpan = append(t.emojiSpan, glyphSpan{start: estart, n: en})
	}

	// Any compaction or growth during the pass is finished, so the pixel size
	// the UVs are measured against is final for this frame.
	t.atlasW, t.atlasH = atlas.W, atlas.H
	if !atlas.Dirty.Empty() {
		t.atlasDirty = true
		atlas.ClearDirty()
	}
}

// shapeOp appends the glyph quads for one text op and returns how many of each
// kind: outline glyphs, then colour emoji.
func (t *textRenderer) shapeOp(op paint.Op, atlas *text.Atlas) (int, int) {
	if op.Text == "" {
		return 0, 0
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
	switch {
	case op.Icon:
		face = lib.IconFace(float64(op.FontSize) * scale)
	case op.FontFamily != "" && op.Italic:
		face = lib.FamilyItalicFace(float64(op.FontSize)*scale, text.Weight(op.Weight), op.FontFamily)
	case op.FontFamily != "":
		face = lib.FamilyFace(float64(op.FontSize)*scale, text.Weight(op.Weight), op.FontFamily)
	case op.Italic:
		face = lib.ItalicFace(float64(op.FontSize)*scale, text.Weight(op.Weight))
	default:
		face = lib.Face(float64(op.FontSize)*scale, text.Weight(op.Weight))
	}
	if face == nil {
		return 0, 0
	}
	runs := face.ShapeRuns(op.Text)
	advance := 0.0
	for _, r := range runs {
		advance += float64(r.Output.Advance) / 64
	}
	penX := float64(op.TextX)
	runW := advance / scale
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
	before := len(t.emoji)
	for _, run := range runs {
		if run.Face.IsEmoji() {
			penX = t.placeEmoji(run, penX, penY, scale, op, clip, clipBox)
			continue
		}
		for _, gl := range run.Output.Glyphs {
			gid := uint16(gl.GlyphID)
			if slot, ok := atlas.Slot(run.Face, gid); ok {
				placed = append(placed, placedGlyph{
					rect: glyphRect(penX, penY, scale, slot),
					slot: slot.Rect,
					gid:  gid,
					pen:  penX,
				})
			}
			penX += float64(gl.Advance) / 64 / scale
		}
	}
	t.placed = placed

	// A text shadow is the run offset and painted before the glyphs, so the
	// glyphs sit on top, exactly as CSS paints text-shadow. Its blur comes from
	// a blurred copy of the glyph in the atlas: a second, padded slot instead of
	// a fragment-shader blur, because the atlas packs glyphs edge to edge and a
	// shader tap would sample the neighbour.
	n := 0
	for _, sh := range op.TextShadows {
		blur := int(math.Round(sh.Blur * scale))
		scol := rgba(sh.Color)
		for _, p := range placed {
			rect := p.rect
			slot := p.slot
			if blur > 0 {
				ss, ok := atlas.ShadowSlot(face, p.gid, blur)
				if !ok {
					continue
				}
				rect = glyphRect(p.pen, penY, scale, ss)
				slot = ss.Rect
			}
			rect[0] += float32(sh.X)
			rect[1] += float32(sh.Y)
			t.quads = append(t.quads, glyphQuad{
				rect:    rect,
				slot:    slot,
				color:   scol,
				clip:    clip,
				clipBox: clipBox,
				clipR:   op.ClipRadius,
				rot:     glyphRot(op),
			})
			n++
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
			rot:     glyphRot(op),
		})
	}
	return n + len(placed), len(t.emoji) - before
}

// placeEmoji appends the colour quads for one emoji run and returns the pen
// position after it. A glyph with no colour bitmap is skipped rather than
// drawn as a box.
func (t *textRenderer) placeEmoji(run text.ShapedRun, penX, penY, scale float64, op paint.Op, clip, clipBox [4]float32) float64 {
	for _, gl := range run.Output.Glyphs {
		gid := uint16(gl.GlyphID)
		if e, ok := t.colorGlyph(run.Face, gid); ok {
			x := math.Round(penX*scale) / scale
			y := (penY + float64(e.off.Y)) / scale
			t.emoji = append(t.emoji, emojiQuad{
				rect: [4]float32{
					float32(x), float32(y),
					float32(e.data.W) / float32(scale), float32(e.data.H) / float32(scale),
				},
				data:    e.data,
				clip:    clip,
				clipBox: clipBox,
				clipR:   op.ClipRadius,
				rot:     glyphRot(op),
			})
		}
		penX += float64(gl.Advance) / 64 / scale
	}
	return penX
}

// colorGlyph returns the decoded colour glyph for a face and glyph. Two sizes of
// the same emoji are two entries, because the bitmap is rasterized at the size
// it is drawn.
func (t *textRenderer) colorGlyph(face *text.Face, gid uint16) (emojiBitmap, bool) {
	key := uint32(int(face.Size()*4))<<16 | uint32(gid)
	if e, ok := t.emojiData[key]; ok {
		return e, true
	}
	pix, off, ok := face.ColorGlyph(gid)
	if !ok {
		return emojiBitmap{}, false
	}
	e := emojiBitmap{data: img.FromImage(pix), off: off}
	if t.emojiData == nil {
		t.emojiData = map[uint32]emojiBitmap{}
	}
	t.emojiData[key] = e
	return e, true
}

// glyphRot carries a text op's rotation onto every glyph quad.
func glyphRot(op paint.Op) [4]float32 {
	return [4]float32{op.Rotate, op.PivotX, op.PivotY, 0}
}

// glyphRect is the quad a slot covers, given the run's pen position. The x is
// snapped to the physical pixel grid so a centred run does not smear a stem
// across two columns; the slot's own offset already carries any blur growth.
func glyphRect(penX, penY, scale float64, slot text.GlyphSlot) [4]float32 {
	x := math.Round((penX+float64(slot.Offset.X)/scale)*scale) / scale
	y := (penY + float64(slot.Offset.Y)) / scale
	w := float64(slot.Rect.Dx()) / scale
	h := float64(slot.Rect.Dy()) / scale
	return [4]float32{float32(x), float32(y), float32(w), float32(h)}
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
			Rot: q.rot,
		})
	}
	// Colour emoji come after the run's outline glyphs and sample the image
	// atlas, so they are bitmap instances (kind 5) like any other picture.
	if i < len(t.emojiSpan) && g.images != nil {
		es := t.emojiSpan[i]
		for _, q := range t.emoji[es.start : es.start+es.n] {
			key := g.images.keyForSize(q.data, float64(q.rect[2]), float64(q.rect[3]))
			slot, ok := g.images.regions[key]
			if !ok {
				var packed img.Region
				if packed, ok = g.images.pack(q.data, key); !ok {
					continue
				}
				slot = imageSlot{r: packed, gen: q.data.Gen}
			}
			r := slot.r
			u0 := float32(r.X) / float32(g.images.w)
			v0 := float32(r.Y) / float32(g.images.h)
			u1 := float32(r.X+r.W) / float32(g.images.w)
			v1 := float32(r.Y+r.H) / float32(g.images.h)
			g.instances = append(g.instances, gpuInstance{
				Rect:    q.rect,
				Color:   [4]float32{1, 1, 1, 1},
				Clip:    q.clip,
				ClipBox: q.clipBox,
				Params:  [4]float32{5, q.clipR, u0, v0},
				Misc:    [4]float32{u1, v1, q.clipR, 0},
				Rot:     q.rot,
			})
		}
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
