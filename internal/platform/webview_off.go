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
