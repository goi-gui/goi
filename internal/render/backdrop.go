//go:build !nogpu

package render

import (
	"fmt"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/goi-gui/goi/internal/paint"
)

// segUniformSize is one vec4: the first instance a pass draws.
const segUniformSize = 16

// backdropPanel is one blurred box in a frame. at is the instance its horizontal
// blur pass draws; the three instances that follow it are the vertical blur, the
// composite and the scene that continues after it, so the passes over one
// instance buffer stay contiguous.
type backdropPanel struct {
	at     int
	rect   [4]float32
	radius [4]float32
	clip   [4]float32
	blur   float32
	alpha  float32
	// clipRadius and clipBox carry a rounded ancestor clip, so a panel inside a
	// ClipRRect stays inside its corners.
	clipRadius float32
	clipBox    [4]float32
	// rot and pivot turn the composite with the element. The blur passes stay
	// axis-aligned, which a symmetric kernel hides.
	rot            float32
	pivotX, pivotY float32
}

// backdropTarget owns the offscreen scene and the two textures the blur passes
// ping-pong through. All three are the window's size, so every pass samples by
// position and a blur radius in logical pixels lands where the design asked.
type backdropTarget struct {
	width, height int

	sceneTex, tmpTex, blurTex    *wgpu.Texture
	sceneView, tmpView, blurView *wgpu.TextureView
	// segs is one small uniform per pass: passes are what tell the shader where
	// its instances begin, and a pass cannot see another's write.
	segs []*wgpu.Buffer
}

// ensureBackdrop returns the offscreen targets for the current size, creating
// them on the first blurred frame and on every resize.
func (g *GPU) ensureBackdrop(w, h int) (*backdropTarget, error) {
	if b := g.backdrop; b != nil && b.width == w && b.height == h {
		return b, nil
	}
	if g.backdrop != nil {
		g.backdrop.release()
		g.backdrop = nil
	}
	b := &backdropTarget{width: w, height: h}
	mk := func(label string) (*wgpu.Texture, *wgpu.TextureView, error) {
		tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
			Label:         label,
			Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
			MipLevelCount: 1,
			SampleCount:   1,
			Dimension:     gputypes.TextureDimension2D,
			Format:        g.format,
			Usage:         gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageTextureBinding,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("%s texture: %w", label, err)
		}
		view, err := g.device.CreateTextureView(tex, nil)
		if err != nil {
			tex.Release()
			return nil, nil, fmt.Errorf("%s view: %w", label, err)
		}
		return tex, view, nil
	}
	var err error
	if b.sceneTex, b.sceneView, err = mk("backdrop-scene"); err != nil {
		b.release()
		return nil, err
	}
	if b.tmpTex, b.tmpView, err = mk("backdrop-horizontal"); err != nil {
		b.release()
		return nil, err
	}
	if b.blurTex, b.blurView, err = mk("backdrop-blur"); err != nil {
		b.release()
		return nil, err
	}
	g.backdrop = b
	return b, nil
}

func (b *backdropTarget) release() {
	for _, v := range []*wgpu.TextureView{b.sceneView, b.tmpView, b.blurView} {
		if v != nil {
			v.Release()
		}
	}
	for _, t := range []*wgpu.Texture{b.sceneTex, b.tmpTex, b.blurTex} {
		if t != nil {
			t.Release()
		}
	}
	for _, s := range b.segs {
		s.Release()
	}
}

// seg returns the uniform naming the first instance of pass slot, writing start
// into it. The pool grows to the busiest frame and is reused after that.
func (b *backdropTarget) seg(g *GPU, slot, start int) (*wgpu.Buffer, error) {
	for len(b.segs) <= slot {
		buf, err := g.device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "backdrop-segment",
			Usage: gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst,
			Size:  segUniformSize,
		})
		if err != nil {
			return nil, fmt.Errorf("segment buffer: %w", err)
		}
		b.segs = append(b.segs, buf)
	}
	v := [4]float32{float32(start), 0, 0, 0}
	if err := g.queue.WriteBuffer(b.segs[slot], 0, floatsToBytes(v[:])); err != nil {
		return nil, err
	}
	return b.segs[slot], nil
}

// bindSegment builds the shape bind group for one pass. src replaces the image
// atlas on binding 4, which is how a backdrop pass samples the scene instead of
// a bitmap; nil keeps the atlas.
func (g *GPU) bindSegment(src *wgpu.TextureView, seg *wgpu.Buffer) (*wgpu.BindGroup, error) {
	if src == nil {
		src = g.imageView()
	}
	return g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "shapes-bindings",
		Layout: g.shapeLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: g.uniform, Size: 16},
			{Binding: 1, Buffer: g.storage, Size: g.storageSize(g.capacity)},
			{Binding: 2, TextureView: g.textView()},
			{Binding: 3, Sampler: g.textSampler()},
			{Binding: 4, TextureView: src},
			{Binding: 5, Sampler: g.imageSampler()},
			{Binding: 6, Buffer: g.gradBuf, Size: gradBufferSize()},
			{Binding: 7, Buffer: seg, Size: segUniformSize},
		},
	})
}

// shapePass draws count instances into target, beginning at the first instance
// the pass' segment uniform names. load keeps what the target already holds.
func (g *GPU) shapePass(enc *wgpu.CommandEncoder, bd *backdropTarget, slot int, target *wgpu.TextureView, load bool, clear gputypes.Color, start, count int, src *wgpu.TextureView) error {
	seg, err := bd.seg(g, slot, start)
	if err != nil {
		return err
	}
	bg, err := g.bindSegment(src, seg)
	if err != nil {
		return err
	}
	g.passBinds = append(g.passBinds, bg)
	att := wgpu.RenderPassColorAttachment{
		View:       target,
		LoadOp:     gputypes.LoadOpClear,
		StoreOp:    gputypes.StoreOpStore,
		ClearValue: clear,
	}
	if load {
		att.LoadOp = gputypes.LoadOpLoad
	}
	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{att},
	})
	if err != nil {
		return err
	}
	if count > 0 {
		pass.SetPipeline(g.pipeline)
		pass.SetBindGroup(0, bg, nil)
		pass.SetVertexBuffer(0, g.storage, 0)
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: uint32(count)})
	}
	return pass.End()
}

// renderBackdrop draws a frame that contains at least one blurred backdrop. The
// scene goes into an offscreen texture, and every panel splits it: the passes
// before the panel, the two blur passes over what they drew, the composite of
// the blurred copy, and then the rest of the frame. The finished scene is blitted
// onto the surface with its last instance.
func (g *GPU) renderBackdrop(list *paint.List, width, height int, panels []backdropPanel) error {
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
	slot, start := 0, 0
	load := false
	for _, p := range panels {
		if err := g.shapePass(enc, bd, slot, bd.sceneView, load, clear, start, p.at-start, nil); err != nil {
			return err
		}
		slot++
		load = true
		// Horizontal then vertical: a quadratic kernel from two 9-tap passes.
		// Each samples what the previous one wrote, so the blur is separable.
		if err := g.shapePass(enc, bd, slot, bd.tmpView, true, clear, p.at, 1, bd.sceneView); err != nil {
			return err
		}
		slot++
		if err := g.shapePass(enc, bd, slot, bd.blurView, true, clear, p.at+1, 1, bd.tmpView); err != nil {
			return err
		}
		slot++
		if err := g.shapePass(enc, bd, slot, bd.sceneView, true, clear, p.at+2, 1, bd.blurView); err != nil {
			return err
		}
		slot++
		start = p.at + 3
	}
	// The last instance copies the scene to the surface; everything before it is
	// the scene itself.
	blit := len(g.instances) - 1
	if err := g.shapePass(enc, bd, slot, bd.sceneView, load, clear, start, blit-start, nil); err != nil {
		return err
	}
	slot++
	if err := g.shapePass(enc, bd, slot, view, false, clear, blit, 1, bd.sceneView); err != nil {
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

// backdropInstances builds the three instances a panel needs: the two blur
// passes, which cover the box grown by the kernel, and the composite, which
// samples the result masked to the box. lw and lh are the logical window size,
// the space the blur radius is measured in.
func backdropInstances(p backdropPanel, lw, lh float32) []gpuInstance {
	pad := p.blur*2 + 2
	whole := clipVec([4]float32{0, 0, lw, lh})
	pass := func(axis float32) gpuInstance {
		return gpuInstance{
			Rect:   [4]float32{p.rect[0] - pad, p.rect[1] - pad, p.rect[2] + 2*pad, p.rect[3] + 2*pad},
			Clip:   whole,
			Params: [4]float32{12, p.blur, 0, 0},
			Misc:   [4]float32{axis, 0, 0, 0},
		}
	}
	return []gpuInstance{
		pass(1),
		pass(2),
		{
			Rect:    p.rect,
			Radius:  p.radius,
			Color:   [4]float32{1, 1, 1, p.alpha},
			Clip:    clipVec(p.clip),
			ClipBox: clipVec(p.clipBox),
			Params:  [4]float32{11, 0, 0, 0},
			Misc:    [4]float32{0, 0, p.clipRadius, 0},
			Rot:     [4]float32{p.rot, p.pivotX, p.pivotY, 0},
		},
	}
}
