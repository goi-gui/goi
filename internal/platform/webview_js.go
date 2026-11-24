//go:build js

package platform

// InitWebview has no native view to prepare in a page.
func InitWebview() error { return nil }

// StartVideo is a no-op: the browser provides the canvas.
func StartVideo() error { return nil }

// NewWebview reports that a native web view is not part of a page build.
func NewWebview(parent *Window, onMessage func(string), onFocus func()) (Webview, error) {
	return nil, ErrWebviewUnsupported
}

// PumpWebviews has no loop of its own in a page.
func PumpWebviews() bool { return false }

// SetWebviewFocus is a no-op: this platform lets the engine take focus itself.
func SetWebviewFocus(bool) {}

// WebviewsLoading is false: this platform does not poll its engine.
func WebviewsLoading() bool { return false }
