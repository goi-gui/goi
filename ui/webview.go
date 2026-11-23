package ui

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/goi-gui/goi/internal/platform"
	"github.com/goi-gui/goi/internal/tree"
)

// webviewEntry is one native browser view and the node it belongs to.
type webviewEntry struct {
	node *tree.WebView
	view platform.Webview
	src  string
	html string
	base string
	zoom float64
	ua   string
	dev  bool
	// focus marks this view as the keyboard target when a design or Go focuses
	// it, and is also called by the platform's button-press signal.
	focus func()
}

// treeHasWebview reports whether a design uses the native web view.
func treeHasWebview(tr *tree.Tree) bool {
	found := false
	if tr == nil {
		return false
	}
	tr.Walk(func(n tree.Node) {
		if n.Kind() == "WebView" {
			found = true
		}
	})
	return found
}

// webviewFor returns the view for a node, or nil.
func (a *App) webviewFor(n *tree.WebView) *webviewEntry {
	for _, e := range a.webviews {
		if e.node == n {
			return e
		}
	}
	return nil
}

// webviewAt returns the topmost web view under a window-space point.
func (a *App) webviewAt(x, y float64) *webviewEntry {
	for i := len(a.webviews) - 1; i >= 0; i-- {
		e := a.webviews[i]
		if r, ok := a.webviewFrame(e.node); ok && hitRect(r, x, y) {
			return e
		}
	}
	return nil
}

// syncWebviews creates, moves and updates every native view. It runs after
// layout, because it needs the final window-space boxes.
func (a *App) syncWebviews(pw *platform.Window) {
	if pw == nil || a.win == nil || a.win.tree == nil {
		return
	}
	seen := map[*tree.WebView]bool{}
	a.win.tree.Walk(func(n tree.Node) {
		wv, ok := n.(*tree.WebView)
		if !ok {
			return
		}
		seen[wv] = true
		e := a.webviewFor(wv)
		if e == nil {
			// A failed creation is not retried every frame: the node lasts
			// until a reload replaces it, and the error would repeat forever.
			if a.webviewFailed[wv] {
				return
			}
			entry := &webviewEntry{node: wv, zoom: 1}
			entry.focus = func() { a.focusWebview(entry) }
			view, err := platform.NewWebview(pw,
				func(msg string) { wv.DeliverMessage(msg) },
				func() { a.focusWebview(entry) },
			)
			if err != nil {
				wv.LoadErr = err
				if a.webviewFailed == nil {
					a.webviewFailed = map[*tree.WebView]bool{}
				}
				a.webviewFailed[wv] = true
				a.reportError(fmt.Errorf("ui: web view: %w", err))
				return
			}
			wv.LoadErr = nil
			entry.view = view
			wv.SetController(entry)
			a.webviews = append(a.webviews, entry)
			e = entry
		}
		a.syncWebviewProps(e)
		if wv.TakeFocus() {
			e.Focus()
		}
		if r, ok := a.webviewFrame(wv); ok {
			e.view.SetBounds(r.X, r.Y, r.W, r.H)
			e.view.SetVisible(true)
		} else {
			e.view.SetVisible(false)
		}
	})
	kept := a.webviews[:0]
	for _, e := range a.webviews {
		if seen[e.node] {
			kept = append(kept, e)
			continue
		}
		if a.focusedWebview == e {
			a.focusedWebview = nil
		}
		e.node.SetController(nil)
		e.view.Destroy()
	}
	a.webviews = kept
	a.hasWebviews = len(a.webviews) > 0
}

// syncWebviewProps loads a changed src or html and applies the view options.
func (a *App) syncWebviewProps(e *webviewEntry) {
	n := e.node
	src, html := n.Src.Get(), n.HTML.Get()
	base := n.BaseURL.Get()
	switch {
	case html != "" && (html != e.html || base != e.base):
		e.view.LoadHTML(html, base)
		e.html, e.base = html, base
		e.src = ""
		n.RenderedHTML, n.RenderedSrc, n.Loaded = html, "", false
	case html == "" && src != "" && src != e.src:
		url, err := a.webviewURL(src)
		if err != nil {
			n.LoadErr = err
			e.src = src
			return
		}
		e.view.Load(url)
		e.src = src
		e.html = ""
		n.RenderedSrc, n.RenderedHTML, n.Loaded = src, "", false
	case html == "" && src == "" && e.src != "":
		e.view.Load("about:blank")
		e.src = ""
		n.RenderedSrc, n.Loaded = "", false
	}
	if z := n.Zoom.Get(); z != e.zoom {
		e.view.SetZoom(z)
		e.zoom = z
	}
	if ua := n.UserAgent.Get(); ua != e.ua {
		e.view.SetUserAgent(ua)
		e.ua = ua
	}
	if dev := n.Devtools.Get(); dev != e.dev {
		e.view.SetDevtools(dev)
		e.dev = dev
	}
}

// focusWebview makes a view the keyboard target. SDL only sends committed text
// while its text input is active, so the view turns it on with a small caret
// area the platform ignores for a native page.
func (a *App) focusWebview(e *webviewEntry) {
	a.focusedWebview = e
	a.reassertTextInput()
}

// reassertTextInput asks SDL for text input again. GTK enables its own input
// method when the view takes focus, and on the shared Wayland connection that
// takes the text input focus from SDL, so committed text would stop arriving.
func (a *App) reassertTextInput() {
	if d, ok := a.driver.(*gpuDriver); ok && d.win != nil {
		d.win.SetTextInput(false, 0, 0, 0, 0)
		d.win.SetTextInput(true, 0, 0, 1, 1)
	}
}

// blurWebview returns the keyboard to the design.
func (a *App) blurWebview() {
	if a.focusedWebview == nil {
		return
	}
	a.focusedWebview = nil
	if d, ok := a.driver.(*gpuDriver); ok && d.win != nil {
		d.win.SetTextInput(false, 0, 0, 0, 0)
	}
}

// webviewURL turns a goi:// embedded asset into a file URL. The design
// container is extracted once, because WebKit resolves the page's relative CSS,
// scripts and images from the directory it was loaded from.
func (a *App) webviewURL(src string) (string, error) {
	if !strings.HasPrefix(src, "goi://") {
		return src, nil
	}
	if a.assets == nil {
		return "", fmt.Errorf("web view: goi:// needs an embedded design")
	}
	rel := strings.TrimLeft(strings.TrimPrefix(src, "goi://"), "/")
	if a.webviewDir == "" {
		dir, err := os.MkdirTemp("", "goi-webview-")
		if err != nil {
			return "", err
		}
		err = fs.WalkDir(a.assets, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return os.MkdirAll(filepath.Join(dir, filepath.FromSlash(path)), 0o755)
			}
			in, err := a.assets.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.Create(filepath.Join(dir, filepath.FromSlash(path)))
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, in)
			return err
		})
		if err != nil {
			os.RemoveAll(dir)
			return "", err
		}
		a.webviewDir = dir
	}
	path := filepath.Join(a.webviewDir, filepath.FromSlash(rel))
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("web view: %s: %w", src, err)
	}
	return "file://" + filepath.ToSlash(path), nil
}

// webviewFrame is the window-space rect a view may occupy. A native view
// cannot follow a rotation, a scale, a clip or a partial opacity, so a node
// under any of those is hidden rather than drawn in the wrong place.
func (a *App) webviewFrame(n tree.Node) (tree.Rect, bool) {
	r := absoluteRect(n)
	if r.W <= 0 || r.H <= 0 {
		return r, false
	}
	for p := n; p != nil; p = p.Base().Parent() {
		b := p.Base()
		if !b.Visible() || b.Opacity() == 0 {
			return r, false
		}
		if !b.Rotation().IsZero() || !b.Scaling().IsIdentity() {
			return r, false
		}
		if p != n && b.ClipsChildren() {
			pr := absoluteRect(p)
			clip := b.ScrollClip(pr)
			if !rectInside(clip, r) {
				return r, false
			}
		}
	}
	return r, true
}

func rectInside(outer, inner tree.Rect) bool {
	return inner.X >= outer.X && inner.Y >= outer.Y &&
		inner.Right() <= outer.Right() && inner.Bottom() <= outer.Bottom()
}

// Reload is the controller method the node forwards to.
func (e *webviewEntry) Reload()         { e.view.Reload() }
func (e *webviewEntry) GoBack() bool    { return e.view.GoBack() }
func (e *webviewEntry) GoForward() bool { return e.view.GoForward() }
func (e *webviewEntry) Eval(js string)  { e.view.Eval(js) }
func (e *webviewEntry) Focus() {
	if e.focus != nil {
		e.focus()
	}
	e.view.Focus()
}

// closeWebviews destroys every native view, for a window close or a reload.
func (a *App) closeWebviews() {
	for _, e := range a.webviews {
		e.node.SetController(nil)
		e.view.Destroy()
	}
	a.webviews = nil
	a.hasWebviews = false
	a.webviewFailed = nil
	a.blurWebview()
	a.pressWebview = nil
	if a.webviewDir != "" {
		os.RemoveAll(a.webviewDir)
		a.webviewDir = ""
	}
}
