//go:build !nogpu

package render

import (
	"fmt"

	"github.com/goi-gui/goi/internal/paint"
)

// wipeRequest is a theme wipe's soft edge: the band in logical pixels (it can
// extend past the window while the edge enters or leaves) and the blur radius
// at its middle.
type wipeRequest struct {
	// at is the first of the three instances the wipe path draws: the two blur
	// passes and the blit.
	at   int
	band [4]float32
	blur float32
}

// wipeInstances builds the two progressive blur passes and the blit that puts
// their result on the surface. blur() peaks in the middle of the band and fades
// to nothing at its edges, so the content outside the edge stays sharp.
func wipeInstances(r *wipeRequest, lw, lh float32) []gpuInstance {
	whole := clipVec([4]float32{0, 0, lw, lh})
	pass := func(axis float32) gpuInstance {
		return gpuInstance{
			Rect:   [4]float32{0, 0, lw, lh},
			Color:  [4]float32{1, 1, 1, 1},
			Clip:   whole,
			Params: [4]float32{12, r.blur, r.band[1], r.band[1] + r.band[3]},
			// misc.y marks a progressive blur, whose radius follows the band.
			Misc: [4]float32{axis, 1, 0, 0},
		}
	}
	return []gpuInstance{pass(1), pass(2), {
		Rect:   [4]float32{0, 0, lw, lh},
		Color:  [4]float32{1, 1, 1, 1},
		Clip:   whole,
		Params: [4]float32{11, 0, 0, 0},
	}}
}

// renderWipe draws the display list into the offscreen scene, blurs it with a
// progressive kernel across the band, and puts the result on the surface. The
// scene is single-sample like a backdrop frame; the blur hides the difference,
// and going through a texture is what lets the finished content melt.
func (g *GPU) renderWipe(list *paint.List, width, height int, start int) error {
	bd, err := g.ensureBackdrop(width, height)
	if err != nil {
		return err
	}
	tex, _, err := g.surface.GetCurrentTexture()
	if err != nil {
		return err
	}
	view, err := tex.CreateView(nil)
	if err != nil {
		return err
	}
	defer view.Release()

	enc, err := g.device.CreateCommandEncoder(nil)
	if err != nil {
		return err
	}
	g.passBinds = g.passBinds[:0]
	defer func() {
		for _, bg := range g.passBinds {
			bg.Release()
		}
		g.passBinds = g.passBinds[:0]
	}()

	clear := clearColor(list)
	if err := g.shapePass(enc, bd, 0, bd.sceneView, nil, false, clear, 0, start, nil); err != nil {
		return err
	}
	// Horizontal then vertical: the same two 9-tap passes a backdrop uses, with
	// a radius that follows the band.
	if err := g.shapePass(enc, bd, 1, bd.tmpView, nil, false, clear, start, 1, bd.sceneView); err != nil {
		return err
	}
	if err := g.shapePass(enc, bd, 2, bd.blurView, nil, false, clear, start+1, 1, bd.tmpView); err != nil {
		return err
	}
	if err := g.shapePass(enc, bd, 3, view, nil, false, clear, start+2, 1, bd.blurView); err != nil {
		return err
	}

	cmdBuf, err := enc.Finish()
	if err != nil {
		return err
	}
	if _, err := g.queue.Submit(cmdBuf); err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	if err := g.surface.Present(tex); err != nil {
		return fmt.Errorf("present: %w", err)
	}
	return nil
}
