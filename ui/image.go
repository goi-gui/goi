package ui

import (
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ksckaan1/goi/internal/img"
	"github.com/ksckaan1/goi/internal/svg"
	"github.com/ksckaan1/goi/internal/tree"
)

// maxRemoteImageBytes caps one download. A design may point at anything, and an
// image that decodes to a 20k by 20k bitmap would take the process down.
const maxRemoteImageBytes = 32 << 20

// remoteImageClient is shared by every window: it pools connections, and the
// timeout keeps a hanging host from leaving a placeholder pulsing forever.
var remoteImageClient = &http.Client{Timeout: 20 * time.Second}

// isRemoteSrc reports whether src is an absolute http(s) URL rather than a path
// inside the design's asset container.
func isRemoteSrc(src string) bool {
	s := strings.ToLower(src)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
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
		if a.assets == nil {
			continue
		}
		d, cached := a.images[src]
		if !cached {
			b, err := fs.ReadFile(a.assets, path.Clean(src))
			if err != nil {
				im.LoadErr = err
				continue
			}
			if d, err = img.Decode(b); err != nil {
				im.LoadErr = fmt.Errorf("%s: %w", src, err)
				continue
			}
			a.cacheImage(src, d)
		}
		im.Data = d
	}
}

// cacheImage stores a decoded bitmap for src.
func (a *App) cacheImage(src string, d *img.Data) {
	if a.images == nil {
		a.images = map[string]*img.Data{}
	}
	a.images[src] = d
}

// svgKey identifies a rasterized SVG: the same file in the same colour is one
// bitmap on the GPU atlas, however many nodes use it.
type svgKey struct {
	src   string
	color uint32
}

// loadSVG rasterizes one SVG node. An empty src leaves the node as it is.
func (a *App) loadSVG(n *tree.SVG) {
	if src := n.Src.Get(); src != "" {
		a.renderSVG(n, src, n.Color.Get())
	}
}

// renderSVG reads, parses and rasterizes src with color as currentColor. The
// parsed file and the bitmap are cached, so a reload and a second node with the
// same src do not repeat the work.
//
// shortcut: an SVG is read once per app. Editing the file on disk needs a
// restart, the same as a bitmap; upgrade with an mtime check in the watcher.
func (a *App) renderSVG(n *tree.SVG, src string, color uint32) {
	n.Data = nil
	n.RenderedSrc, n.RenderedColor = src, color
	n.LoadErr = nil
	if isRemoteSrc(src) {
		n.LoadErr = fmt.Errorf("%s: SVG src must be a local path, not a URL", src)
		return
	}
	if a.assets == nil {
		return
	}
	source, ok := a.svgs[src]
	if !ok {
		b, err := fs.ReadFile(a.assets, path.Clean(src))
		if err != nil {
			n.LoadErr = err
			return
		}
		if source, err = svg.Parse(b); err != nil {
			n.LoadErr = fmt.Errorf("%s: %w", src, err)
			return
		}
		if a.svgs == nil {
			a.svgs = map[string]*svg.SVG{}
		}
		a.svgs[src] = source
	}
	n.IntrinsicW, n.IntrinsicH = source.Intrinsic()

	// SVG colours have no alpha channel, so 0 means "the design named none"
	// and gets black; the alpha of a real colour becomes drawing opacity.
	c := color
	if c == 0 {
		c = 0x000000FF
	}
	key := svgKey{src: src, color: c}
	d, ok := a.svgData[key]
	if !ok {
		var err error
		if d, err = source.Render(c); err != nil {
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

// syncSVGs re-rasterizes an SVG whose src or colour changed from Go. It runs
// once per frame and only walks the tree while the design has an SVG.
func (a *App) syncSVGs() {
	if !a.hasSVG || a.win == nil || a.win.tree == nil {
		return
	}
	a.win.tree.Walk(func(n tree.Node) {
		s, ok := n.(*tree.SVG)
		if !ok {
			return
		}
		src, color := s.Src.Get(), s.Color.Get()
		if src == s.RenderedSrc && color == s.RenderedColor {
			return
		}
		a.renderSVG(s, src, color)
		s.Base().MarkDirty(tree.DirtyPaint)
	})
}

// loadRemoteImage shows the cached bitmap at once, or marks the node as loading
// and starts one fetch per URL. Several nodes may share a src, so the request is
// keyed by URL rather than by node.
func (a *App) loadRemoteImage(im *tree.Image, src string) {
	if d, ok := a.images[src]; ok {
		im.Data = d
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
		d, err := fetchRemoteImage(src)
		// Post runs on the UI goroutine, which is the only place a node may be
		// touched.
		a.Post(func() { a.finishImageLoad(src, d, err) })
	}()
}

// fetchRemoteImage downloads and decodes one URL. Decoding happens here, off the
// UI goroutine: a large JPEG takes long enough to drop a frame.
func fetchRemoteImage(src string) (*img.Data, error) {
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
	d, err := img.Decode(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	return d, nil
}

// finishImageLoad hands a finished download to the nodes that asked for it. It
// walks the current tree rather than remembering node pointers, so a fetch that
// lands after a hot reload still fills in the new nodes.
func (a *App) finishImageLoad(src string, d *img.Data, err error) {
	delete(a.pending, src)
	if d != nil {
		a.cacheImage(src, d)
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
			im.Data = d
		} else {
			im.LoadErr = err
		}
		im.Base().MarkDirty(tree.DirtyPaint)
	})
	a.needsFrame = true
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
		im, ok := n.(*tree.Image)
		if !ok || !im.Loading {
			return
		}
		im.Phase = phase
		im.Base().MarkDirty(tree.DirtyPaint)
		still = true
	})
	if !still {
		a.pulsing = false
		a.loadClock = 0
	}
	return still
}
