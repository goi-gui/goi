package ui

import (
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/svg"
	"github.com/goi-gui/goi/internal/tree"
)

// maxRemoteImageBytes caps one download. A design may point at anything, and an
// image that decodes to a 20k by 20k bitmap would take the process down.
const maxRemoteImageBytes = 32 << 20

// remoteImageClient is shared by every window: it pools connections, and the
// timeout keeps a hanging host from leaving a placeholder pulsing forever.
var remoteImageClient = &http.Client{Timeout: 20 * time.Second}

// assetScheme marks a src that lives in the design's asset container: the fs.FS
// passed to LoadWindow, so an embed.FS in production and a directory in
// development.
const assetScheme = "goi://"

// isRemoteSrc reports whether src is an absolute http(s) URL rather than a local
// one.
func isRemoteSrc(src string) bool {
	s := strings.ToLower(src)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// isAssetSrc reports whether src names a file inside the design's container.
func isAssetSrc(src string) bool { return strings.HasPrefix(src, assetScheme) }

// readSrc returns the bytes behind a local src. `goi://path` reads the design's
// asset container; anything else is a path on this machine, relative to the
// working directory or absolute. Remote URLs never reach here: they are fetched
// in the background.
func (a *App) readSrc(src string) ([]byte, error) {
	if isAssetSrc(src) {
		if a.assets == nil {
			return nil, fmt.Errorf("%s: no asset container", src)
		}
		return fs.ReadFile(a.assets, path.Clean(strings.TrimPrefix(src, assetScheme)))
	}
	return os.ReadFile(src)
}

// loadImages fills in every Image and SVG element in a freshly built tree.
// Local files are read and decoded here, on the UI goroutine, because they are
// small and the result is needed for the first frame; decoding is cached by src,
// so a hot reload does not decode again and a failed file is not retried every
// frame.
//
// A remote src is not read here: the node is marked Loading and the fetch runs
// in the background, so a slow host cannot hold up the window. The placeholder
// breathes until the bitmap lands.
//
// shortcut: src is read once, when the tree is built or reloaded. An expression
// that changes src at runtime updates the string but not the bitmap; upgrade with
// a per-frame check on Src when an app swaps images without a reload.
func (a *App) loadImages(tr *tree.Tree) {
	if tr == nil {
		return
	}
	a.hasSVG = false
	a.hasSVGAnim = false
	for _, n := range tr.All() {
		if s, ok := n.(*tree.SVG); ok {
			a.hasSVG = true
			a.loadSVG(s)
			continue
		}
		im, ok := n.(*tree.Image)
		if !ok {
			continue
		}
		src := im.Src.Get()
		if src == "" {
			continue
		}
		if isRemoteSrc(src) {
			a.loadRemoteImage(im, src)
			continue
		}
		d, cached := a.images[src]
		if !cached {
			b, err := a.readSrc(src)
			if err != nil {
				im.LoadErr = err
				continue
			}
			var anim *img.Anim
			if d, anim, err = img.DecodeAnimated(b); err != nil {
				im.LoadErr = fmt.Errorf("%s: %w", src, err)
				continue
			}
			a.cacheImage(src, d, anim)
		}
		anim := a.anims[src]
		animData(im, d, anim)
		if anim != nil {
			a.hasAnim = true
		}
	}
}

// animData gives an animated node its own bitmap, so two elements with the same
// src advance independently instead of swapping one shared Pix.
func animData(im *tree.Image, d *img.Data, anim *img.Anim) {
	im.Anim = anim
	im.Frame, im.AnimTime, im.AnimDone = 0, 0, false
	im.Data = d
	if anim == nil || len(anim.Frames) == 0 {
		return
	}
	im.Data = &img.Data{W: anim.W, H: anim.H, Pix: anim.Frames[0].Pix}
}

// cacheImage stores a decoded bitmap for src, and its animation when it has one.
func (a *App) cacheImage(src string, d *img.Data, anim *img.Anim) {
	if a.images == nil {
		a.images = map[string]*img.Data{}
	}
	a.images[src] = d
	if anim == nil {
		return
	}
	if a.anims == nil {
		a.anims = map[string]*img.Anim{}
	}
	a.anims[src] = anim
}

// svgColor normalises a design colour for the rasterizer. SVG colours have no
// alpha channel, so 0 means "the design named none" and gets black; the alpha of
// a real colour becomes drawing opacity.
func svgColor(color uint32) uint32 {
	if color == 0 {
		return 0x000000FF
	}
	return color
}

// svgKey identifies a rasterized SVG: the same file in the same colour is one
// bitmap on the GPU atlas, however many nodes use it.
type svgKey struct {
	src   string
	color uint32
}

// loadSVG rasterizes one SVG node. A document set from Go wins over `src`, and
// an empty src leaves the node as it is.
func (a *App) loadSVG(n *tree.SVG) {
	if n.InlineGen > 0 {
		a.renderSVGData(n, n.Inline, n.Color.Get())
		return
	}
	if src := n.Src.Get(); src != "" {
		a.renderSVG(n, src, n.Color.Get())
	}
}

// renderSVGData rasterizes SVG source held in Go. It is not cached: the bytes
// belong to one node, and a set arrives once, not once per frame.
func (a *App) renderSVGData(n *tree.SVG, b []byte, color uint32) {
	n.RenderedGen = n.InlineGen
	n.RenderedColor = color
	n.LoadErr = nil
	n.Loading = false
	source, err := svg.Parse(b)
	if err != nil {
		n.Data = nil
		n.LoadErr = err
		return
	}
	n.IntrinsicW, n.IntrinsicH = source.Intrinsic()
	a.useAnimatedSVG(n, source)
	d, err := source.Render(svgColor(color))
	if err != nil {
		n.Data = nil
		n.LoadErr = err
		return
	}
	n.Data = d
}

// useAnimatedSVG records that a node plays an SMIL document, so the frame loop
// re-rasterizes it; a still file clears the state.
func (a *App) useAnimatedSVG(n *tree.SVG, source *svg.SVG) {
	if !source.Animated() {
		n.Source, n.AnimDur, n.AnimTime, n.Frame, n.AnimDone = nil, 0, 0, 0, false
		return
	}
	n.Source, n.AnimDur = source, source.Duration()
	n.AnimTime, n.Frame, n.AnimDone = 0, 0, false
	a.hasSVGAnim = true
}

// renderSVG resolves src, parses it and rasterizes it with color as
// currentColor. Local files are read from the asset container; a remote URL is
// only parsed once the fetch has landed, until then the node stays Loading.
//
// shortcut: a file is read once per app. Editing it on disk needs a restart,
// the same as a bitmap; upgrade with an mtime check in the watcher.
func (a *App) renderSVG(n *tree.SVG, src string, color uint32) {
	n.RenderedSrc, n.RenderedColor = src, color
	n.LoadErr = nil
	if src == "" {
		n.Data = nil
		n.Loading = false
		return
	}
	if isRemoteSrc(src) {
		if _, ok := a.svgRemote[src]; !ok {
			a.loadRemoteSVG(n, src)
			return
		}
	}
	source, err := a.svgSource(src)
	if err != nil {
		n.Data = nil
		n.LoadErr = err
		return
	}
	if source == nil {
		return // no asset container (a headless test, or a detached node)
	}
	n.Loading = false
	n.IntrinsicW, n.IntrinsicH = source.Intrinsic()
	a.useAnimatedSVG(n, source)
	if n.AnimDur > 0 {
		// An animated file cannot use the shared bitmap: two nodes must be able
		// to sit on different moments, and every frame replaces the pixels.
		d, err := source.RenderAtLoop(svgColor(color), n.AnimTime, n.Loop.Get())
		if err != nil {
			n.Data = nil
			n.LoadErr = fmt.Errorf("%s: %w", src, err)
			return
		}
		n.Data = d
		return
	}

	c := svgColor(color)
	key := svgKey{src: src, color: c}
	d, ok := a.svgData[key]
	if !ok {
		if d, err = source.Render(c); err != nil {
			n.Data = nil
			n.LoadErr = fmt.Errorf("%s: %w", src, err)
			return
		}
		if a.svgData == nil {
			a.svgData = map[svgKey]*img.Data{}
		}
		a.svgData[key] = d
	}
	n.Data = d
	// shortcut: a colour change rasterizes a new bitmap and the image atlas
	// keeps the superseded region, so animating Color every frame grows GPU
	// memory. Upgrade with a tint mask or in-place atlas updates if that is
	// ever needed; state changes and theme switches are fine.
}

// svgSource returns the parsed file for src, reading a local asset or using the
// bytes a remote fetch cached. A nil source with a nil error means the app has
// no asset container yet.
func (a *App) svgSource(src string) (*svg.SVG, error) {
	if s, ok := a.svgs[src]; ok {
		return s, nil
	}
	var b []byte
	var err error
	if isRemoteSrc(src) {
		b = a.svgRemote[src]
	} else if b, err = a.readSrc(src); err != nil {
		return nil, err
	}
	s, err := svg.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	if a.svgs == nil {
		a.svgs = map[string]*svg.SVG{}
	}
	a.svgs[src] = s
	return s, nil
}

// loadRemoteSVG marks the node as loading and starts one fetch per URL, which
// the placeholder breathes over until the file lands.
func (a *App) loadRemoteSVG(n *tree.SVG, src string) {
	n.Data = nil
	n.Loading = true
	a.pulsing = true
	if a.svgPending == nil {
		a.svgPending = map[string]bool{}
	}
	if a.svgPending[src] {
		return
	}
	a.svgPending[src] = true
	go func() {
		b, err := fetchRemote(src)
		// Post runs on the UI goroutine, which is the only place a node may be
		// touched.
		a.Post(func() { a.finishSVGLoad(src, b, err) })
	}()
}

// finishSVGLoad hands a finished download to the SVG nodes with that src. It
// walks the current tree, so a fetch that lands after a hot reload still fills
// in the new nodes.
func (a *App) finishSVGLoad(src string, b []byte, err error) {
	delete(a.svgPending, src)
	if err == nil {
		if a.svgRemote == nil {
			a.svgRemote = map[string][]byte{}
		}
		a.svgRemote[src] = b
	}
	if a.win == nil || a.win.tree == nil {
		return
	}
	a.win.tree.Walk(func(n tree.Node) {
		s, ok := n.(*tree.SVG)
		if !ok || s.Src.Get() != src || !s.Loading {
			return
		}
		s.Loading = false
		if err != nil {
			s.Data = nil
			s.LoadErr = err
			s.Base().MarkDirty(tree.DirtyPaint)
			return
		}
		a.renderSVG(s, src, s.Color.Get())
		// The viewBox is only known now, so width/height can change with it.
		s.Base().MarkDirty(tree.DirtyLayout)
	})
	a.needsFrame = true
}

// syncSVGs re-rasterizes an SVG whose src or colour changed from Go. It runs
// once per frame and only walks the tree while the design has an SVG.
func (a *App) syncSVGs() {
	if a.win == nil || a.win.tree == nil {
		return
	}
	// The walk is free only while nothing is pending: a design with an SVG needs
	// it every frame for a colour change, and a document set from Go marks the
	// tree dirty, which is the frame this has to run on.
	if !a.hasSVG && !a.needsFrame {
		return
	}
	a.win.tree.Walk(func(n tree.Node) {
		s, ok := n.(*tree.SVG)
		if !ok {
			return
		}
		color := s.Color.Get()
		if s.InlineGen > 0 {
			// A document from Go owns the node until another set replaces it.
			if s.InlineGen == s.RenderedGen && color == s.RenderedColor {
				return
			}
			a.renderSVGData(s, s.Inline, color)
			s.Base().MarkDirty(tree.DirtyLayout)
			return
		}
		src := s.Src.Get()
		if src == s.RenderedSrc && color == s.RenderedColor {
			return
		}
		kind := tree.DirtyPaint
		if src != s.RenderedSrc {
			// A different file may have a different viewBox, and with no
			// declared width/height that changes the element's size.
			kind = tree.DirtyLayout
		}
		a.renderSVG(s, src, color)
		s.Base().MarkDirty(kind)
	})
}

// loadRemoteImage shows the cached bitmap at once, or marks the node as loading
// and starts one fetch per URL. Several nodes may share a src, so the request is
// keyed by URL rather than by node.
func (a *App) loadRemoteImage(im *tree.Image, src string) {
	if d, ok := a.images[src]; ok {
		anim := a.anims[src]
		animData(im, d, anim)
		if anim != nil {
			a.hasAnim = true
		}
		return
	}
	im.Loading = true
	a.pulsing = true
	if a.pending == nil {
		a.pending = map[string]bool{}
	}
	if a.pending[src] {
		return
	}
	a.pending[src] = true
	go func() {
		d, anim, err := fetchRemoteImage(src)
		// Post runs on the UI goroutine, which is the only place a node may be
		// touched.
		a.Post(func() { a.finishImageLoad(src, d, anim, err) })
	}()
}

// fetchRemoteImage downloads and decodes one URL. Decoding happens here, off the
// UI goroutine: a large JPEG takes long enough to drop a frame.
func fetchRemoteImage(src string) (*img.Data, *img.Anim, error) {
	b, err := fetchRemote(src)
	if err != nil {
		return nil, nil, err
	}
	d, anim, err := img.DecodeAnimated(b)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", src, err)
	}
	return d, anim, nil
}

// fetchRemote downloads one URL and returns its bytes. Image decodes them here
// too; SVG keeps them until the UI goroutine can parse and rasterize.
func fetchRemote(src string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, src, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	req.Header.Set("Accept", "image/*")
	resp, err := remoteImageClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", src, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	if len(b) > maxRemoteImageBytes {
		return nil, fmt.Errorf("%s: larger than %d bytes", src, maxRemoteImageBytes)
	}
	return b, nil
}

// finishImageLoad hands a finished download to the nodes that asked for it. It
// walks the current tree rather than remembering node pointers, so a fetch that
// lands after a hot reload still fills in the new nodes.
func (a *App) finishImageLoad(src string, d *img.Data, anim *img.Anim, err error) {
	delete(a.pending, src)
	if d != nil {
		a.cacheImage(src, d, anim)
	}
	if a.win == nil || a.win.tree == nil {
		return
	}
	a.win.tree.Walk(func(n tree.Node) {
		im, ok := n.(*tree.Image)
		if !ok || im.Src.Get() != src || !im.Loading {
			return
		}
		im.Loading = false
		if d != nil {
			animData(im, d, anim)
			if anim != nil {
				a.hasAnim = true
			}
		} else {
			im.LoadErr = err
		}
		im.Base().MarkDirty(tree.DirtyPaint)
	})
	a.needsFrame = true
}

// svgFrameStep quantizes SMIL playback: the document is re-rasterized only when
// the quantized time changes, so a 240Hz window does not pay 240 parses a second.
const svgFrameStep = time.Second / 30

// tickAnimations advances every animated Image and SVG, in one walk of the
// visible tree. It reports whether a frame actually changed, so the loop paints
// only when a picture moved, and how long until the next frame is due, so the
// loop can sleep instead of spinning at the display rate for an 8 fps GIF.
//
// The walk is skipped outright when the design has no animation, and the tree
// is pruned at hidden nodes, so a clip on a background page costs nothing.
func (a *App) tickAnimations(dt float64) (changed bool, wait time.Duration) {
	if (!a.hasAnim && !a.hasSVGAnim) || a.win == nil || a.win.tree == nil {
		return false, 0
	}
	step := time.Duration(dt * float64(time.Second))
	a.win.tree.WalkVisible(func(n tree.Node) {
		var due time.Duration
		switch v := n.(type) {
		case *tree.Image:
			var c bool
			c, due = tickImage(v, step)
			changed = changed || c
		case *tree.SVG:
			var c bool
			c, due = tickSVG(v, step)
			changed = changed || c
		}
		if due > 0 && (wait == 0 || due < wait) {
			wait = due
		}
	})
	return changed, wait
}

// tickImage advances one GIF or APNG and reports whether its pixels changed,
// plus how long until its next frame is due.
func tickImage(im *tree.Image, step time.Duration) (bool, time.Duration) {
	if im.Anim == nil || im.Paused.Get() || im.Data == nil {
		return false, 0
	}
	if im.AnimDone && !im.Loop.Get() {
		return false, 0
	}
	im.AnimDone = false
	im.AnimTime += step
	// A design that turned looping off plays the first pass only, whatever the
	// file declares, and holds the last frame at the end.
	at := im.AnimTime
	oneShot := false
	if !im.Loop.Get() {
		if pass := im.Anim.Duration(); pass > 0 && at >= pass {
			at, oneShot = pass-1, true
		}
	}
	i, done := im.Anim.FrameAt(at)
	if oneShot {
		done = true
	}
	if done {
		im.AnimDone = true // a finite loop holds its last frame
	}
	// Only swap pixels when the frame actually changed, which is what keeps a
	// slow GIF cheap in a fast window.
	if i != im.Frame || im.Data.Pix == nil {
		im.Frame = i
		im.Data.Pix = im.Anim.Frames[i].Pix
		// The generation makes the renderer upload this image's atlas region
		// again instead of packing the new frame beside the old one.
		im.Data.Gen++
		im.Base().MarkDirty(tree.DirtyPaint)
		return true, 0
	}
	if im.AnimDone {
		return false, 0
	}
	return false, im.Anim.UntilNextFrame(i, im.AnimTime)
}

// tickSVG advances one SMIL document and reports whether its pixels changed,
// plus how long until the next 30 Hz step is due. The document is re-rasterized
// at most once per svgFrameStep.
func tickSVG(s *tree.SVG, step time.Duration) (bool, time.Duration) {
	if s.Source == nil || s.AnimDur <= 0 || s.Paused.Get() || s.Data == nil {
		return false, 0
	}
	if s.AnimDone && !s.Loop.Get() {
		return false, 0
	}
	s.AnimDone = false
	s.AnimTime += step
	end := s.Source.PlayLength()
	if !s.Loop.Get() {
		// loop: false means the first pass, whatever repeatCount says.
		end = s.AnimDur
	}
	if end > 0 && s.AnimTime >= end {
		s.AnimDone = true
	}
	frame := int(s.AnimTime / svgFrameStep)
	if frame == s.Frame {
		if s.AnimDone {
			return false, 0
		}
		return false, svgFrameStep - s.AnimTime%svgFrameStep
	}
	s.Frame = frame
	d, err := s.Source.RenderAtLoop(svgColor(s.RenderedColor), s.AnimTime, s.Loop.Get())
	if err != nil {
		s.LoadErr = err
		return false, 0
	}
	// The pixels go into the node's existing bitmap and only its generation
	// changes, so the renderer rewrites that one atlas rect. A new *img.Data
	// per frame would pack a fresh region each time, and the atlas would fill
	// up until images were downscaled away.
	writePix(s.Data, d)
	s.Base().MarkDirty(tree.DirtyPaint)
	return true, 0
}

// writePix copies one decoded frame over a node's bitmap, keeping the pointer
// the atlas is keyed by, and bumps the generation the renderer watches.
func writePix(dst, src *img.Data) {
	if dst.W == src.W && dst.H == src.H && len(dst.Pix) == len(src.Pix) {
		copy(dst.Pix, src.Pix)
		dst.Gen++
		return
	}
	dst.W, dst.H, dst.Pix = src.W, src.H, src.Pix
	dst.Gen++
}

// loadPulsePeriod is how long one breath of the loading placeholder takes.
const loadPulsePeriod = 1.2 // seconds

// tickImageLoads advances the loading placeholder and reports whether a frame is
// still needed. It only walks the tree while something is actually loading, so an
// idle window pays nothing.
func (a *App) tickImageLoads(dt float64) bool {
	if !a.pulsing || a.win == nil || a.win.tree == nil {
		return false
	}
	a.loadClock += dt
	t := math.Mod(a.loadClock, loadPulsePeriod) / loadPulsePeriod
	phase := 0.5 - 0.5*math.Cos(2*math.Pi*t)

	still := false
	a.win.tree.Walk(func(n tree.Node) {
		switch v := n.(type) {
		case *tree.Image:
			if !v.Loading {
				return
			}
			v.Phase = phase
			v.Base().MarkDirty(tree.DirtyPaint)
			still = true
		case *tree.SVG:
			if !v.Loading {
				return
			}
			v.Phase = phase
			v.Base().MarkDirty(tree.DirtyPaint)
			still = true
		}
	})
	if !still {
		a.pulsing = false
		a.loadClock = 0
	}
	return still
}
