//go:build !nogpu

package render

import (
	"fmt"
	"math"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/ksckaan1/goui/internal/paint"
	"github.com/ksckaan1/goui/internal/text"
)

// textRenderer owns the glyph atlas. Glyph quads go into the same instance
// buffer as shapes (kind 4), so paint order is exactly the display-list order
// and the window stays one draw call.
type textRenderer struct {
	texture *wgpu.Texture
	view    *wgpu.TextureView
	sampler *wgpu.Sampler

	atlasW, atlasH int
	atlasDirty     bool

	gpu *GPU // back-reference for buffers and the atlas library
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

// addOp converts one text or icon op into glyph instances, rasterizing into the
// atlas on first use.
func (t *textRenderer) addOp(op paint.Op) {
	if op.Text == "" {
		return
	}
	g := t.gpu
	lib := g.textLib

	// Rasterize at the device resolution, then snap each glyph quad to the
	// physical pixel grid. Without the snap, a centred run lands on a half
	// pixel and the linear sampler smears every stem across two columns.
	scale := float64(g.scale)
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
		return
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

	for _, gl := range out.Glyphs {
		slot, ok := lib.Atlas().Slot(face, uint16(gl.GlyphID))
		if ok {
			x := math.Round((penX+float64(slot.Offset.X)/scale)*scale) / scale
			y := (penY + float64(slot.Offset.Y)) / scale
			w := float64(slot.Rect.Dx()) / scale
			h := float64(slot.Rect.Dy()) / scale
			g.instances = append(g.instances, gpuInstance{
				Rect:  [4]float32{float32(x), float32(y), float32(w), float32(h)},
				Color: col,
				Clip:  clip,
				// kind 4 is a glyph quad: params.zw is the UV origin and
				// misc.xy is the UV far corner.
				Params: [4]float32{4, 0,
					float32(slot.Rect.Min.X) / float32(t.atlasW),
					float32(slot.Rect.Min.Y) / float32(t.atlasH)},
				Misc: [4]float32{
					float32(slot.Rect.Max.X) / float32(t.atlasW),
					float32(slot.Rect.Max.Y) / float32(t.atlasH), 0, 0},
			})
		}
		penX += float64(gl.Advance) / 64 / scale
	}
	if !lib.Atlas().Dirty.Empty() {
		t.atlasDirty = true
		lib.Atlas().ClearDirty()
	}
}

// upload pushes the atlas if it changed since the last frame.
func (t *textRenderer) upload(g *GPU) error {
	if !t.atlasDirty {
		return nil
	}
	t.atlasDirty = false
	atlas := g.textLib.Atlas()
	return g.queue.WriteTexture(
		&wgpu.ImageCopyTexture{Texture: t.texture},
		atlas.Img.Pix,
		&wgpu.ImageDataLayout{Offset: 0, BytesPerRow: uint32(atlas.Img.Stride), RowsPerImage: uint32(t.atlasH)},
		&wgpu.Extent3D{Width: uint32(t.atlasW), Height: uint32(t.atlasH), DepthOrArrayLayers: 1},
	)
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
