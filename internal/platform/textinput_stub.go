//go:build !linux || x11

package platform

// InitTextInput is a no-op where the platform has no IME protocol wired up.
func (p *Window) InitTextInput() {}
