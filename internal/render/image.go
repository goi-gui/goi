//go:build !nogpu

package render

import (
	"fmt"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/ksckaan1/goi/internal/img"
	"github.com/ksckaan1/goi/internal/paint"
)

// imageRenderer packs bitmaps into one RGBA texture and draws them as quads
// (kind 5), the same way glyphs use the coverage atlas. One texture and one
// draw call keep paint order intact; the alternative, a bind group per image,
// would split the frame into a draw call per image.
//
// shortcut: the atlas is a fixed 2048x2048 with a shelf packer and no eviction,
// so images past that budget are downscaled to fit and an image that cannot fit
// at all is skipped. Upgrade to per-image textures with a mip chain when a real
// app needs many large photos.
type imageRenderer struct {
	texture *wgpu.Texture
	view    *wgpu.TextureView
	sampler *wgpu.Sampler

	w, h  int
	pix   []byte
	gpu   *GPU
	shelf img.Shelf
	// regions maps a decoded image to its rect in the atlas.
	regions map[*img.Data]img.Region
	dirty   bool
}

func (g *GPU) initImages() error {
	const size = 2048
	t := &imageRenderer{
		gpu: g,
		w:   size, h: size,
		pix:     make([]byte, size*size*4),
		shelf:   img.Shelf{W: size, H: size},
		regions: map[*img.Data]img.Region{},
		dirty:   true,
	}
	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "image-atlas",
		Size:          wgpu.Extent3D{Width: size, Height: size, DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        gputypes.TextureFormatRGBA8Unorm,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("image atlas texture: %w", err)
	}
	t.texture = tex
	if t.view, err = g.device.CreateTextureView(tex, nil); err != nil {
		return fmt.Errorf("image atlas view: %w", err)
	}
	t.sampler, err = g.device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        "image-sampler",
		AddressModeU: gputypes.AddressModeClampToEdge,
		AddressModeV: gputypes.AddressModeClampToEdge,
		AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
	})
	if err != nil {
		return fmt.Errorf("image sampler: %w", err)
	}
	g.images = t
	return nil
}

// addOp appends the quad for one bitmap op, packing the image on first use.
func (t *imageRenderer) addOp(op paint.Op) {
	d := op.Src
	if d == nil {
		return
	}
	r, ok := t.regions[d]
	if !ok {
		if r, ok = t.pack(d); !ok {
			return
		}
	}
	u0, v0 := float32(r.X)/float32(t.w), float32(r.Y)/float32(t.h)
	u1, v1 := float32(r.X+r.W)/float32(t.w), float32(r.Y+r.H)/float32(t.h)
	// The op's UV is a crop of the whole image; map it into the packed rect.
	// params.zw holds the near corner and misc.xy the far one, the same layout
	// a glyph quad uses, so the vertex shader needs no change.
	cu0, cv0 := u0, v0
	cu1, cv1 := u1, v1
	uv := op.UV
	t.gpu.instances = append(t.gpu.instances, gpuInstance{
		Rect:  op.Rect,
		Color: [4]float32{1, 1, 1, 1},
		Clip:  clipVec(op.Clip),
		// params.y is the corner radius: the bitmap branch rounds its own
		// crop, and misc.xy is already the UV far corner.
		Params: [4]float32{5, op.ClipRadius, cu0 + uv[0]*(cu1-cu0), cv0 + uv[1]*(cv1-cv0)},
		Misc:   [4]float32{cu0 + uv[2]*(cu1-cu0), cv0 + uv[3]*(cv1-cv0), 0, 0},
	})
}

// pack places d in the atlas, downscaling it when the space left is smaller.
func (t *imageRenderer) pack(d *img.Data) (img.Region, bool) {
	w, h := d.W, d.H
	if w > t.w || h > t.h {
		s := min(float64(t.w)/float64(w), float64(t.h)/float64(h))
		w, h = max(1, int(float64(w)*s)), max(1, int(float64(h)*s))
	}
	x, y, ok := t.shelf.Place(w, h)
	if !ok {
		// Still no room: halve the image until it fits, then give up.
		for tries := 0; tries < 4 && !ok; tries++ {
			w, h = max(1, w/2), max(1, h/2)
			x, y, ok = t.shelf.Place(w, h)
		}
		if !ok {
			return img.Region{}, false
		}
	}
	src := d
	if w != d.W || h != d.H {
		src = img.Scale(d, w, h)
	}
	for row := 0; row < h; row++ {
		copy(t.pix[((y+row)*t.w+x)*4:], src.Pix[row*src.W*4:(row*src.W+src.W)*4])
	}
	t.dirty = true
	r := img.Region{X: x, Y: y, W: w, H: h}
	t.regions[d] = r
	return r, true
}

// upload pushes the atlas if a new image was packed.
func (t *imageRenderer) upload(g *GPU) error {
	if !t.dirty {
		return nil
	}
	t.dirty = false
	return g.queue.WriteTexture(
		&wgpu.ImageCopyTexture{Texture: t.texture},
		t.pix,
		&wgpu.ImageDataLayout{Offset: 0, BytesPerRow: uint32(t.w * 4), RowsPerImage: uint32(t.h)},
		&wgpu.Extent3D{Width: uint32(t.w), Height: uint32(t.h), DepthOrArrayLayers: 1},
	)
}

func (t *imageRenderer) release() {
	if t == nil {
		return
	}
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
