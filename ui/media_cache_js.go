//go:build js

package ui

// mediaCache is the desktop decoder's bookkeeping. A browser plays through its
// own elements, so there is nothing to cache and nothing to probe.
type mediaCache struct{}
