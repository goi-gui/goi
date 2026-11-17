//go:build js || nomermaid

// Package mermaid renders Mermaid source with Pele's layout and resvg. A
// browser build or a `nomermaid` build does not carry the wasm rasterizer, so
// a diagram reports that support is not built instead of taking the page down.
package mermaid

import (
	"errors"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/m3"
)

// Engine is the renderer facade; the browser build has no engine.
type Engine struct{}

// NewEngine reports that Mermaid rendering is not available in this build.
func NewEngine() (*Engine, error) {
	return nil, errors.New("mermaid: this build has no renderer (browser or -tags nomermaid)")
}

// Render always fails without the renderer.
func (*Engine) Render(string, *m3.Theme, float64, float64) (*img.Data, float64, float64, error) {
	return nil, 0, 0, errors.New("mermaid: this build has no renderer (browser or -tags nomermaid)")
}

// Close is a no-op.
func (*Engine) Close() error { return nil }
