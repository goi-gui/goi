package platform

import "errors"

// ErrWebviewUnsupported is returned when this build or platform has no native
// web view. The WebView element still parses and paints its placeholder.
var ErrWebviewUnsupported = errors.New("platform: native web view is not supported in this build")

// ErrWebviewUnavailable is returned when the platform web view runtime (the
// WebKitGTK libraries) is missing.
var ErrWebviewUnavailable = errors.New("platform: WebKitGTK is not installed")

// PointerKind is one pointer event forwarded from the SDL window to a view.
type PointerKind int

// Pointer events.
const (
	PointerMove PointerKind = iota
	PointerDown
	PointerUp
	PointerScroll
)

// Webview is one native browser view attached to the window. All methods run on
// the UI goroutine; the platform queues them itself where it has its own loop.
type Webview interface {
	SetBounds(x, y, w, h float64)
	SetVisible(bool)
	Load(src string)
	LoadHTML(html, base string)
	Reload()
	GoBack() bool
	GoForward() bool
	Eval(js string)
	SetZoom(float64)
	SetUserAgent(string)
	SetDevtools(bool)
	ForwardKey(k Key, mods Mods)
	ForwardText(r rune)
	ForwardPointer(kind PointerKind, x, y float64, button int, dx, dy float64)
	Focus()
	Destroy()
}
