//go:build !js && !webview && !all

package platform

// InitWebview needs no toolkit when this build has no native web view.
func InitWebview() error { return nil }

// NewWebview reports that this build has no native web view; the tag turns it
// on (see the webview_* files).
func NewWebview(parent *Window, onMessage func(string), onFocus func()) (Webview, error) {
	return nil, ErrWebviewUnsupported
}

// PumpWebviews has no toolkit loop to advance.
func PumpWebviews() bool { return false }

// SetWebviewFocus is a no-op: this platform lets the engine take focus itself.
func SetWebviewFocus(bool) {}

// WebviewsLoading is false: this platform does not poll its engine.
func WebviewsLoading() bool { return false }
