//go:build !nogpu

package render

import (
	"fmt"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/paint"
)

// imageRenderer packs bitmaps into one RGBA texture and draws them as quads
// (kind 5), the same way glyphs use the coverage atlas. One texture and one
// draw call keep paint order intact; the alternative, a bind group per image,
// would split the frame into a draw call per image.
//
// The atlas starts at 2048x2048 and grows on demand (up to 4096), so a screen
// full of photos does not silently lose one. Only at the cap does an image get
// downscaled, and the last resort is skipping the op. A real photo wall wants
// per-image textures with a mip chain.
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
	// resized is set when the atlas grew, so the texture must be rebuilt.
	resized bool
}

// Image atlases are capped so a pathological set cannot allocate without bound.
const maxImageAtlasDim = 4096

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

// prepare packs every image the frame needs before any quad is emitted, so a
// growth during packing cannot leave earlier instances pointing at stale UVs.
func (t *imageRenderer) prepare(list *paint.List) {
	for _, op := range list.Ops {
		if op.Kind != paint.Bitmap || op.Src == nil {
			continue
		}
		if _, ok := t.regions[op.Src]; !ok {
			t.pack(op.Src)
		}
	}
}

// addOp appends the quad for one bitmap op. prepare has already packed it.
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
		Rect:    op.Rect,
		Color:   [4]float32{1, 1, 1, 1},
		Clip:    clipVec(op.Clip),
		ClipBox: clipVec(op.ClipBox),
		// params.y rounds the bitmap's own crop; misc.xy is the UV far corner
		// and misc.z the rounded clip, which the bitmap branch does not touch.
		Params: [4]float32{5, op.ClipRadius, cu0 + uv[0]*(cu1-cu0), cv0 + uv[1]*(cv1-cv0)},
		Misc:   [4]float32{cu0 + uv[2]*(cu1-cu0), cv0 + uv[3]*(cv1-cv0), op.ClipRadius, 0},
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
	// Grow before downscaling: a sharper image is worth the texture memory.
	for tries := 0; !ok && tries < 2; tries++ {
		if !t.grow() {
			break
		}
		x, y, ok = t.shelf.Place(w, h)
	}
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

// grow doubles the shorter side of the atlas, moving the packed images across.
// It reports whether the atlas changed.
func (t *imageRenderer) grow() bool {
	if t.w >= maxImageAtlasDim && t.h >= maxImageAtlasDim {
		return false
	}
	nw, nh := t.w, t.h
	switch {
	case nw == nh, nw < nh:
		nw *= 2
	default:
		nh *= 2
	}
	if nw > maxImageAtlasDim {
		nw = maxImageAtlasDim
	}
	if nh > maxImageAtlasDim {
		nh = maxImageAtlasDim
	}
	if nw == t.w && nh == t.h {
		return false
	}

	oldPix, ow := t.pix, t.w
	t.pix = make([]byte, nw*nh*4)
	t.shelf = img.Shelf{W: nw, H: nh}
	t.w, t.h = nw, nh

	regions := make(map[*img.Data]img.Region, len(t.regions))
	for d, r := range t.regions {
		x, y, ok := t.shelf.Place(r.W, r.H)
		if !ok {
			continue // repacked on the next request
		}
		for row := 0; row < r.H; row++ {
			copy(t.pix[((y+row)*nw+x)*4:],
				oldPix[((r.Y+row)*ow+r.X)*4:((r.Y+row)*ow+r.X+r.W)*4])
		}
		regions[d] = img.Region{X: x, Y: y, W: r.W, H: r.H}
	}
	t.regions = regions
	t.resized = true
	t.dirty = true
	return true
}

// upload pushes the atlas if a new image was packed.
func (t *imageRenderer) upload(g *GPU) error {
	if t.resized {
		if err := t.recreate(g); err != nil {
			return err
		}
	}
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

// recreate rebuilds the image texture at the atlas' current size.
func (t *imageRenderer) recreate(g *GPU) error {
	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "image-atlas",
		Size:          wgpu.Extent3D{Width: uint32(t.w), Height: uint32(t.h), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        gputypes.TextureFormatRGBA8Unorm,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("image atlas texture: %w", err)
	}
	view, err := g.device.CreateTextureView(tex, nil)
	if err != nil {
		tex.Release()
		return fmt.Errorf("image atlas view: %w", err)
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.texture != nil {
		t.texture.Release()
	}
	t.texture, t.view = tex, view
	t.resized = false
	return g.rebind()
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
