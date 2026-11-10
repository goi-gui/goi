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
//
// Each image is packed at its own drawn size, halved while it stays more than
// twice the quad's device size. A 512px logo drawn at 30dp would otherwise be
// minified 17x by a bilinear sampler that can only read 2x2 texels, which is
// what makes small pictures look grainy; packing it near 2x the drawn size
// keeps every mip a box filter over the original.
type imageRenderer struct {
	texture *wgpu.Texture
	view    *wgpu.TextureView
	sampler *wgpu.Sampler

	w, h  int
	pix   []byte
	gpu   *GPU
	shelf img.Shelf
	// regions maps one packed size of an image to its rect in the atlas. The
	// same picture drawn small and large holds two entries, each filtered for
	// its own size.
	regions map[imageKey]imageSlot
	// dirty is set when pix changed since the last upload.
	dirty bool
	// resized is set when the atlas grew, so the texture must be rebuilt.
	resized bool
}

// Image atlases are capped so a pathological set cannot allocate without bound.
const maxImageAtlasDim = 4096

// imageKey identifies one packed copy of an image: the source, and the size it
// was scaled to.
type imageKey struct {
	src *img.Data
	w   int
	h   int
}

// imageSlot is a packed rect together with the animation generation it holds,
// so a frame that swapped Pix in place is uploaded again instead of packed a
// second time.
type imageSlot struct {
	r   img.Region
	gen int
}

func (g *GPU) initImages() error {
	const size = 2048
	t := &imageRenderer{
		gpu: g,
		w:   size, h: size,
		pix:     make([]byte, size*size*4),
		shelf:   img.Shelf{W: size, H: size},
		regions: map[imageKey]imageSlot{},
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
		key := t.keyFor(op)
		if slot, ok := t.regions[key]; ok {
			// An animation swaps Pix in place and bumps Gen; the region stays,
			// only its pixels are refreshed.
			if slot.gen != op.Src.Gen {
				t.writeRegion(op.Src, slot.r)
				slot.gen = op.Src.Gen
				t.regions[key] = slot
			}
			continue
		}
		t.pack(op.Src, key)
	}
}

// keyFor is the atlas key one op asks for: the image, packed at a size close to
// its drawn one. Halving keeps the number of distinct sizes per image small, and
// each one stays a clean box filter of the original.
func (t *imageRenderer) keyFor(op paint.Op) imageKey {
	return t.keyForSize(op.Src, float64(op.Rect[2]), float64(op.Rect[3]))
}

// keyForSize is keyFor for a picture drawn at dw by dh logical pixels, which is
// what a colour emoji quad has.
func (t *imageRenderer) keyForSize(d *img.Data, dw, dh float64) imageKey {
	scale := float64(t.gpu.scale)
	if scale <= 0 {
		scale = 1
	}
	dw *= scale
	dh *= scale
	w, h := d.W, d.H
	// Stop at the power of two that is 2x to 4x the drawn size. The sampler then
	// reads no more than a 4x4 footprint per pixel, which its 2x2 taps plus the
	// box filtering here turn into a smooth edge instead of speckle.
	for w > 1 && h > 1 && (float64(w) > 4*dw+1 || float64(h) > 4*dh+1) {
		w, h = w/2, h/2
	}
	return imageKey{src: d, w: w, h: h}
}

// addOp appends the quad for one bitmap op. prepare has already packed it.
func (t *imageRenderer) addOp(op paint.Op) {
	d := op.Src
	if d == nil {
		return
	}
	key := t.keyFor(op)
	slot, ok := t.regions[key]
	if !ok {
		var r img.Region
		if r, ok = t.pack(d, key); !ok {
			return
		}
		slot = imageSlot{r: r, gen: d.Gen}
	}
	r := slot.r
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
		Rot:    [4]float32{op.Rotate, op.PivotX, op.PivotY, 0},
	})
}

// pack places d in the atlas at key's size, shrinking further only when the
// space left is smaller. The region is filed under key, which is what the caller
// looked up, and under the size it actually got when that differs.
func (t *imageRenderer) pack(d *img.Data, key imageKey) (img.Region, bool) {
	w, h := key.w, key.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
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
	r := img.Region{X: x, Y: y, W: w, H: h}
	t.writeRegion(src, r)
	slot := imageSlot{r: r, gen: d.Gen}
	t.regions[key] = slot
	if key.w != w || key.h != h {
		// The atlas was too full for the requested size: the same shrunken copy
		// answers that key too, so nothing repacks it every frame.
		t.regions[imageKey{src: d, w: w, h: h}] = slot
	}
	return r, true
}

// writeRegion copies an image's pixels into an existing atlas rect, scaling when
// the packed rect is smaller than the source.
func (t *imageRenderer) writeRegion(d *img.Data, r img.Region) {
	src := d
	if r.W != d.W || r.H != d.H {
		src = img.Scale(d, r.W, r.H)
	}
	for row := 0; row < r.H; row++ {
		copy(t.pix[((r.Y+row)*t.w+r.X)*4:], src.Pix[row*src.W*4:(row*src.W+src.W)*4])
	}
	t.dirty = true
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

	regions := make(map[imageKey]imageSlot, len(t.regions))
	for k, slot := range t.regions {
		r := slot.r
		x, y, ok := t.shelf.Place(r.W, r.H)
		if !ok {
			continue // repacked on the next request
		}
		for row := 0; row < r.H; row++ {
			copy(t.pix[((y+row)*nw+x)*4:],
				oldPix[((r.Y+row)*ow+r.X)*4:((r.Y+row)*ow+r.X+r.W)*4])
		}
		slot.r = img.Region{X: x, Y: y, W: r.W, H: r.H}
		regions[k] = slot
	}
	t.regions = regions
	t.resized = true
	t.dirty = true
	return true
}

// upload pushes the atlas if a new image was packed or an animation frame
// changed its region.
//
// shortcut: the whole texture goes every time. Writing only the changed rect
// looked cheaper but the gogpu 0.34 staging path does not land a sub-rect copy,
// and the animation simply froze; a full write is 16 MiB at the 2048 default,
// which the frame clock keeps to the clip's own rate. Revisit when that path is
// fixed or the atlas default grows.
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
