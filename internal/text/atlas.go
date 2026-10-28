package text

import (
	"image"
)

// Atlas is a single-channel coverage bitmap packed with glyphs. The renderer
// uploads it once and re-uploads the dirty region when glyphs are added.
type Atlas struct {
	Img  image.Gray
	W, H int

	// Dirty is the region added since the last upload.
	Dirty image.Rectangle

	// entries caches glyph bitmaps by pack key.
	entries map[GlyphKey]GlyphSlot

	// shelf packing cursor.
	penX, penY, shelfH int
}

// GlyphKey identifies one rasterized glyph.
type GlyphKey struct {
	Size int // size in 1/4 pixel
	GID  uint16
}

// GlyphSlot is where a glyph lives in the atlas.
type GlyphSlot struct {
	Rect   image.Rectangle
	Offset image.Point // from the pen to the bitmap's top-left
}

// NewAtlas creates an empty atlas of w by h pixels.
func NewAtlas(w, h int) *Atlas {
	return &Atlas{
		Img:     *image.NewGray(image.Rect(0, 0, w, h)),
		W:       w,
		H:       h,
		entries: map[GlyphKey]GlyphSlot{},
	}
}

// Slot returns the atlas location for a glyph, rasterizing it on first use.
// The bool is false when the glyph cannot be rasterized (space, or the atlas is
// full).
func (a *Atlas) Slot(face *Face, gid uint16, size int) (GlyphSlot, bool) {
	key := GlyphKey{Size: size, GID: gid}
	if s, ok := a.entries[key]; ok {
		return s, s.Rect.Dx() > 0
	}
	gimg, off, ok := face.Raster(gid)
	if !ok || gimg.Rect.Dx() == 0 || gimg.Rect.Dy() == 0 {
		// Cache the miss so a space does not rasterize every frame.
		a.entries[key] = GlyphSlot{}
		return GlyphSlot{}, false
	}
	slot, ok := a.alloc(gimg.Rect.Dx(), gimg.Rect.Dy())
	if !ok {
		return GlyphSlot{}, false // atlas full: skip rather than corrupt
	}
	for y := 0; y < gimg.Rect.Dy(); y++ {
		copy(a.Img.Pix[(slot.Min.Y+y)*a.Img.Stride+slot.Min.X:], gimg.Pix[y*gimg.Stride:y*gimg.Stride+gimg.Rect.Dx()])
	}
	a.Dirty = a.Dirty.Union(slot)
	s := GlyphSlot{Rect: slot, Offset: off}
	a.entries[key] = s
	return s, true
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
		return image.Rectangle{}, false // shortcut: no eviction; see Phase 8 notes
	}
	r := image.Rect(a.penX, a.penY, a.penX+w, a.penY+h)
	a.penX += w + 1
	if h > a.shelfH {
		a.shelfH = h
	}
	return r, true
}

// ClearDirty marks the atlas as uploaded.
func (a *Atlas) ClearDirty() { a.Dirty = image.Rectangle{} }
