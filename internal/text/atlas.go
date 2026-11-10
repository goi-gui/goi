package text

import (
	"image"
)

// Atlas is a single-channel coverage bitmap packed with glyphs. The renderer
// uploads it once and re-uploads when glyphs are added.
//
// The atlas never silently loses a glyph. When the shelves run out it first
// compacts: every entry not used by the frame currently being built is dropped,
// the survivors are copied into a fresh bitmap, and packing starts over. If the
// working set alone does not fit, the bitmap grows (up to maxAtlasDim) and the
// renderer recreates its texture.
type Atlas struct {
	Img  image.Gray
	W, H int

	// Dirty is the region added since the last upload.
	Dirty image.Rectangle

	// entries caches glyph placement by pack key.
	entries map[GlyphKey]glyphEntry

	// frame is the current paint pass, and stamp records the last one that used
	// an entry. Compaction keeps only the entries stamped with frame.
	frame uint64
	stamp map[GlyphKey]uint64

	// shelf packing cursor.
	penX, penY, shelfH int

	// Resized is set when the bitmap changed size, so the renderer must rebuild
	// its texture instead of uploading into the old one.
	Resized bool
	// Compactions counts rebuilds; tests read it to prove the path ran.
	Compactions int
}

// maxAtlasDim caps growth so a pathological working set cannot allocate
// without bound. 4096² of coverage is 16MB.
const maxAtlasDim = 4096

// GlyphKey identifies one rasterized glyph. Blur is 0 for the glyph itself and
// the blur radius in device pixels for a shadow copy, which is a distinct atlas
// entry because its bitmap is grown and softened.
type GlyphKey struct {
	Font uint16
	Size int // size in 1/4 pixel
	GID  uint16
	Blur int
}

// GlyphSlot is where a glyph lives in the atlas.
type GlyphSlot struct {
	Rect   image.Rectangle
	Offset image.Point // from the pen to the bitmap's top-left
}

// glyphEntry is a slot plus the face it came from, so a compaction can copy the
// pixels without rasterizing again.
type glyphEntry struct {
	GlyphSlot
	face *Face
}

// NewAtlas creates an empty atlas of w by h pixels.
func NewAtlas(w, h int) *Atlas {
	return &Atlas{
		Img:     *image.NewGray(image.Rect(0, 0, w, h)),
		W:       w,
		H:       h,
		entries: map[GlyphKey]glyphEntry{},
		stamp:   map[GlyphKey]uint64{},
	}
}

// BeginFrame marks a new paint pass. Compaction uses it to tell the glyphs the
// frame actually needs from the ones left over from before.
func (a *Atlas) BeginFrame() { a.frame++ }

// Slot returns the atlas location for a glyph, rasterizing it on first use.
// The bool is false only when the glyph has no bitmap (a space) — a full atlas
// is handled by compacting or growing, never by skipping.
func (a *Atlas) Slot(face *Face, gid uint16) (GlyphSlot, bool) {
	return a.slot(face, gid, 0)
}

// maxShadowBlur caps the cached blur so a wild value cannot allocate without
// bound. A text shadow past 40px is past readable anyway.
const maxShadowBlur = 40

// ShadowSlot returns the atlas location of a glyph's shadow: the glyph's
// coverage blurred by blur device pixels, in a bitmap grown to hold the soft
// edge. The returned Offset already includes that growth, so laying it out like
// a normal glyph puts the blurred copy exactly where the glyph sits.
func (a *Atlas) ShadowSlot(face *Face, gid uint16, blur int) (GlyphSlot, bool) {
	if blur <= 0 {
		return a.slot(face, gid, 0)
	}
	if blur > maxShadowBlur {
		blur = maxShadowBlur
	}
	return a.slot(face, gid, blur)
}

// slot is Slot for a glyph, or its shadow when blur is positive.
func (a *Atlas) slot(face *Face, gid uint16, blur int) (GlyphSlot, bool) {
	key := GlyphKey{Font: face.Tag(), Size: int(face.Size() * 4), GID: gid, Blur: blur}
	a.stamp[key] = a.frame
	if e, ok := a.entries[key]; ok {
		return e.GlyphSlot, e.Rect.Dx() > 0
	}
	gimg, off, ok := face.Raster(gid)
	if !ok || gimg.Rect.Dx() == 0 || gimg.Rect.Dy() == 0 {
		// Cache the miss so a space does not rasterize every frame.
		a.entries[key] = glyphEntry{}
		return GlyphSlot{}, false
	}
	if blur > 0 {
		var pad int
		gimg, pad = blurGray(gimg, blur)
		off.X -= pad
		off.Y -= pad
	}
	slot, ok := a.place(key, face, gimg, off)
	if !ok {
		return GlyphSlot{}, false
	}
	return slot, true
}

// place allocates room for gimg, compacting first and then growing. Compaction
// only reclaims entries the current frame does not need; growth is what makes a
// working set larger than the bitmap survive.
func (a *Atlas) place(key GlyphKey, face *Face, gimg image.Gray, off image.Point) (GlyphSlot, bool) {
	w, h := gimg.Rect.Dx(), gimg.Rect.Dy()
	rect, ok := a.alloc(w, h)
	if !ok && a.compact() {
		rect, ok = a.alloc(w, h)
	}
	for !ok && a.grow() {
		rect, ok = a.alloc(w, h)
	}
	if !ok {
		return GlyphSlot{}, false
	}
	a.blit(rect, gimg)
	a.Dirty = a.Dirty.Union(rect)
	a.entries[key] = glyphEntry{GlyphSlot: GlyphSlot{Rect: rect, Offset: off}, face: face}
	return GlyphSlot{Rect: rect, Offset: off}, true
}

// alloc reserves a rectangle with a shelf packer.
func (a *Atlas) alloc(w, h int) (image.Rectangle, bool) {
	if w > a.W {
		return image.Rectangle{}, false
	}
	if a.penX+w > a.W {
		a.penX = 0
		a.penY += a.shelfH + 1
		a.shelfH = 0
	}
	if a.penY+h > a.H {
		return image.Rectangle{}, false
	}
	r := image.Rect(a.penX, a.penY, a.penX+w, a.penY+h)
	a.penX += w + 1
	if h > a.shelfH {
		a.shelfH = h
	}
	return r, true
}

// blit copies one rasterized glyph into the atlas at rect.
func (a *Atlas) blit(rect image.Rectangle, gimg image.Gray) {
	for y := 0; y < gimg.Rect.Dy(); y++ {
		copy(a.Img.Pix[(rect.Min.Y+y)*a.Img.Stride+rect.Min.X:],
			gimg.Pix[y*gimg.Stride:y*gimg.Stride+gimg.Rect.Dx()])
	}
}

// compact rebuilds the atlas from just the entries this frame used. It reports
// whether anything survived; the caller grows when only the working set is too
// large for the current bitmap.
func (a *Atlas) compact() bool {
	live := make(map[GlyphKey]glyphEntry, len(a.entries))
	for key, e := range a.entries {
		if e.Rect.Dx() == 0 {
			continue // a cached miss such as a space
		}
		if a.stamp[key] != a.frame {
			delete(a.entries, key)
			delete(a.stamp, key)
			continue
		}
		live[key] = e
	}
	if len(live) == 0 {
		return false
	}

	old := a.Img
	oldEntries := make(map[GlyphKey]glyphEntry, len(live))
	for k, v := range live {
		oldEntries[k] = v
	}
	// Nothing left to win back: the caller grows instead of rebuilding twice.
	if len(oldEntries) >= len(a.entries) && a.penX == 0 && a.penY == 0 {
		return false
	}
	a.Img = *image.NewGray(image.Rect(0, 0, a.W, a.H))
	a.penX, a.penY, a.shelfH = 0, 0, 0
	a.entries = map[GlyphKey]glyphEntry{}
	a.Compactions++

	for key, e := range oldEntries {
		rect, ok := a.alloc(e.Rect.Dx(), e.Rect.Dy())
		if !ok {
			// Still no room: the caller grows and the entries it could not place
			// stay out, but they are re-rasterized on the next request.
			continue
		}
		copyRect(&a.Img, rect, &old, e.Rect)
		e.Rect = rect
		a.entries[key] = e
	}
	a.Dirty = image.Rect(0, 0, a.W, a.H)
	return len(a.entries) > 0
}

// grow doubles the shorter side (or both at maxAtlasDim) and repacks into it.
func (a *Atlas) grow() bool {
	if a.W >= maxAtlasDim && a.H >= maxAtlasDim {
		return false
	}
	w, h := a.W, a.H
	if w == h {
		w *= 2
	} else if w < h {
		w *= 2
	} else {
		h *= 2
	}
	if w > maxAtlasDim {
		w = maxAtlasDim
	}
	if h > maxAtlasDim {
		h = maxAtlasDim
	}
	if w == a.W && h == a.H {
		return false
	}

	live := a.entries
	old := a.Img
	a.Img = *image.NewGray(image.Rect(0, 0, w, h))
	a.W, a.H = w, h
	a.penX, a.penY, a.shelfH = 0, 0, 0
	a.entries = make(map[GlyphKey]glyphEntry, len(live))
	a.Compactions++

	for key, e := range live {
		if e.Rect.Dx() == 0 {
			a.entries[key] = e
			continue
		}
		rect, ok := a.alloc(e.Rect.Dx(), e.Rect.Dy())
		if !ok {
			continue
		}
		copyRect(&a.Img, rect, &old, e.Rect)
		e.Rect = rect
		a.entries[key] = e
	}
	a.Dirty = image.Rect(0, 0, a.W, a.H)
	a.Resized = true
	return true
}

// copyRect copies src[srcRect] into dst at dstRect.
func copyRect(dst *image.Gray, dstRect image.Rectangle, src *image.Gray, srcRect image.Rectangle) {
	w, h := srcRect.Dx(), srcRect.Dy()
	for y := 0; y < h; y++ {
		copy(dst.Pix[(dstRect.Min.Y+y)*dst.Stride+dstRect.Min.X:],
			src.Pix[(srcRect.Min.Y+y)*src.Stride+srcRect.Min.X:(srcRect.Min.Y+y)*src.Stride+srcRect.Min.X+w])
	}
}

// ClearDirty marks the atlas as uploaded.
func (a *Atlas) ClearDirty() { a.Dirty = image.Rectangle{} }
