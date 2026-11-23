//go:build !js && (webview || all) && !linux && !windows && !darwin

package platform

// InitWebview needs no toolkit when this platform has no native web view.
func InitWebview() error { return nil }

// NewWebview reports that this platform has no native web view.
func NewWebview(parent *Window, onMessage func(string), onFocus func()) (Webview, error) {
	return nil, ErrWebviewUnsupported
}

// PumpWebviews has no toolkit loop to advance.
func PumpWebviews() bool { return false }
