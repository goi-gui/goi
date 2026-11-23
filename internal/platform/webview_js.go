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
