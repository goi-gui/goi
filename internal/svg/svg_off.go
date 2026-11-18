//go:build !all && !svg

// Package svg parses and rasterizes SVG files. A build without the `svg`
// feature drops the rasterizer (and oksvg/rasterx with it): SVG elements report an error instead
// of drawing, which is what an app that only uses bitmaps wants.
package svg

import (
	"errors"
	"time"

	"github.com/goi-gui/goi/internal/img"
)

// SVG is a parsed document. The stub keeps the type so a design can still hold
// one; nothing parses.
type SVG struct{}

// ErrNoSVG is what a build without the rasterizer reports.
var ErrNoSVG = errors.New("svg: this build has no SVG support; build with -tags svg")

// Parse reports that this build cannot read an SVG.
func Parse(b []byte) (*SVG, error) { return nil, ErrNoSVG }

func (s *SVG) Duration() time.Duration                { return 0 }
func (s *SVG) Animated() bool                         { return false }
func (s *SVG) PlayLength() time.Duration              { return 0 }
func (s *SVG) Intrinsic() (w, h float64)              { return 0, 0 }
func (s *SVG) Render(color uint32) (*img.Data, error) { return nil, ErrNoSVG }
func (s *SVG) RenderAt(color uint32, elapsed time.Duration) (*img.Data, error) {
	return nil, ErrNoSVG
}
func (s *SVG) RenderAtLoop(color uint32, elapsed time.Duration, loop bool) (*img.Data, error) {
	return nil, ErrNoSVG
}
