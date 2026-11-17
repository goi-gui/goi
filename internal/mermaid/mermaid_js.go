//go:build js

// Package mermaid renders Mermaid source with Pele's layout and resvg. The
// browser build does not carry the wasm rasterizer, so a diagram reports that
// support is not built there instead of taking the page down.
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
	return nil, errors.New("mermaid: rendering is not available in the browser build")
}

// Render always fails in the browser build.
func (*Engine) Render(string, *m3.Theme, float64) (*img.Data, float64, float64, error) {
	return nil, 0, 0, errors.New("mermaid: rendering is not available in the browser build")
}

// Close is a no-op.
func (*Engine) Close() error { return nil }
