//go:build !nogpu

package render

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/text"

	_ "github.com/gogpu/wgpu/hal/allbackends"
)

// One pipeline draws every solid shape. The fragment shader branches on the
// shape kind, which keeps a whole window at one draw call.
//
//	kind 0: rounded rect (per-corner radius, optional border)
//	kind 1: filled circle
//	kind 2: ring or arc (thickness in misc, sweep in params.zw)
//	kind 3: drop shadow (rounded rect with a soft falloff)
//	kind 4: glyph quad sampling the atlas (text and icons), optionally blurred
//	kind 5: bitmap
//	kind 6: inset shadow (a soft band inside a rounded rect)
const shaderWGSL = `
struct Instance {
	rect: vec4f,    // x, y, w, h in logical pixels
	radius: vec4f,  // tl, tr, br, bl
	color: vec4f,
	border: vec4f,  // rgb + width
	clip: vec4f,    // x0, y0, x1, y1
	clipbox: vec4f, // x0, y0, x1, y1 of the unclipped rounded box; empty = none
	params: vec4f,  // kind, blur, startAngle, endAngle
	misc: vec4f,    // thickness, uv far (glyphs, bitmaps), clip radius
}

struct Viewport {
	size: vec4f, // logical w, logical h, scale, unused
}

@group(0) @binding(0) var<uniform> viewport: Viewport;
@group(0) @binding(1) var<storage, read> instances: array<Instance>;
@group(0) @binding(2) var atlas: texture_2d<f32>;
@group(0) @binding(3) var samp: sampler;
@group(0) @binding(4) var images: texture_2d<f32>;
@group(0) @binding(5) var imgSamp: sampler;

struct VSOut {
	@builtin(position) pos: vec4f,
	@location(0) local: vec2f,
	@location(1) size: vec2f,
	@location(2) radius: vec4f,
	@location(3) color: vec4f,
	@location(4) border: vec4f,
	@location(5) clip: vec4f,
	@location(6) pos2: vec2f,
	@location(7) params: vec4f,
	@location(8) misc: vec4f,
	@location(9) uv: vec2f,
	@location(10) clipbox: vec4f,
}

@vertex
fn vs_main(@builtin(vertex_index) vi: u32, @builtin(instance_index) ii: u32) -> VSOut {
	var corners = array<vec2f, 6>(
		vec2f(0.0, 0.0), vec2f(1.0, 0.0), vec2f(0.0, 1.0),
		vec2f(0.0, 1.0), vec2f(1.0, 0.0), vec2f(1.0, 1.0),
	);
	let inst = instances[ii];
	let c = corners[vi];
	let p = inst.rect.xy + c * inst.rect.zw;
	var out: VSOut;
	out.pos = vec4f(p.x / viewport.size.x * 2.0 - 1.0, 1.0 - p.y / viewport.size.y * 2.0, 0.0, 1.0);
	out.local = c * inst.rect.zw;
	out.size = inst.rect.zw;
	out.radius = inst.radius;
	out.color = inst.color;
	out.border = inst.border;
	out.clip = inst.clip;
	out.pos2 = p;
	out.params = inst.params;
	out.misc = inst.misc;
	out.clipbox = inst.clipbox;
	out.uv = mix(inst.params.zw, inst.misc.xy, c);
	return out;
}

fn corner_radius(r: vec4f, p: vec2f, s: vec2f) -> f32 {
	let right = p.x > s.x * 0.5;
	let bottom = p.y > s.y * 0.5;
	if (bottom) {
		if (right) { return r.z; }
		return r.w;
	}
	if (right) { return r.y; }
	return r.x;
}

// Signed distance to a rounded rectangle centred on s/2.
fn sd_round_rect(p: vec2f, s: vec2f, r: f32) -> f32 {
	let h = s * 0.5;
	let q = abs(p - h) - (h - vec2f(r));
	return length(max(q, vec2f(0.0))) + min(max(q.x, q.y), 0.0) - r;
}

const PI = 3.14159265359;

@fragment
fn fs_main(in: VSOut) -> @location(0) vec4f {
	// Hard clip against the node's content box.
	if (in.pos2.x < in.clip.x || in.pos2.y < in.clip.y ||
	    in.pos2.x > in.clip.z || in.pos2.y > in.clip.w) { discard; }

	// Optional rounded clip, so a ripple stays inside a pill or a card. The
	// rounded shape is the box the component declared, not the intersection
	// with the visible area: a card scrolled half out of view has a straight
	// cut at the viewport edge and rounded corners only where it really ends.
	let clipR = in.misc.z;
	if (clipR > 0.0) {
		var box = in.clip;
		if (in.clipbox.z > in.clipbox.x && in.clipbox.w > in.clipbox.y) {
			box = in.clipbox;
		}
		let bsize = box.zw - box.xy;
		let limit = min(bsize.x, bsize.y) * 0.5;
		let d = sd_round_rect(in.pos2 - box.xy, bsize, min(clipR, limit));
		if (d > 0.0) { discard; }
	}

	let aa = 0.5 / max(viewport.size.z, 1.0);
	let kind = in.params.x;

	if (kind > 5.5) {
		// Inset shadow: a soft band inside the shape. Spread shrinks the hole,
		// the offset moves it, and the signed distance to the hole decides the
		// band, so the shadow fades inward from the edge exactly as CSS paints
		// an inset box-shadow.
		let blur = max(in.params.y, 0.5);
		let spread = in.misc.w;
		// The hole is the shape shrunk by spread, moved by the offset. sd_round_rect
		// measures from a box's top-left, so the hole's own origin is subtracted.
		let hole = max(in.size - vec2f(2.0 * spread), vec2f(0.0));
		let holeOrigin = (in.size - hole) * 0.5 + vec2f(in.params.z, in.params.w);
		let hr = max(corner_radius(in.radius, in.local, in.size) - spread, 0.0);
		let dh = sd_round_rect(in.local - holeOrigin, hole, hr);
		// Outside the hole is where the shadow is: dh >= 0, and the edge of the
		// hole is the darkest point before the fade inward.
		let cov = smoothstep(-blur, 0.0, dh);
		if (cov <= 0.0) { discard; }
		// Keep the band inside the shape itself.
		let dr = sd_round_rect(in.local, in.size, corner_radius(in.radius, in.local, in.size));
		let inside = 1.0 - smoothstep(-aa, aa, dr);
		if (inside <= 0.0) { discard; }
		return vec4f(in.color.rgb, in.color.a * cov * inside);
	}

	if (kind > 4.5) {
		// Bitmap: the image atlas holds straight RGBA. params.y is the corner
		// radius, so a rounded Image is cropped rather than drawn as a box.
		let r = clamp(in.params.y, 0.0, min(in.size.x, in.size.y) * 0.5);
		let d = sd_round_rect(in.local, in.size, r);
		let cov = 1.0 - smoothstep(-aa, aa, d);
		if (cov <= 0.0) { discard; }
		let c = textureSample(images, imgSamp, in.uv);
		if (c.a <= 0.0) { discard; }
		return vec4f(c.rgb, c.a * cov);
	}

	if (kind > 3.5) {
		// Glyph: the atlas holds one coverage value per texel. A text shadow is
		// a separately blurred entry in the same atlas, so this stays one sample.
		let a = textureSample(atlas, samp, in.uv).r;
		return vec4f(in.color.rgb, in.color.a * a);
	}

	if (kind > 2.5) {
		// Drop shadow: the quad is the shape grown by the blur radius, so the
		// SDF is evaluated against the original shape and fades to zero at the
		// quad edge. The shape itself is drawn on top of the solid middle.
		let blur = max(in.params.y, 0.001);
		let innerSize = in.size - vec2f(2.0 * blur);
		let p = in.local - vec2f(blur);
		let r = max(corner_radius(in.radius, in.local, in.size) - blur, 0.0);
		let d = sd_round_rect(p, innerSize, r);
		let cov = 1.0 - smoothstep(-blur * 0.25, blur, d);
		if (cov <= 0.0) { discard; }
		return vec4f(in.color.rgb, in.color.a * cov);
	}

	if (kind > 0.5) {
		// Filled circle and ring/arc, both inscribed in the box.
		let c = in.size * 0.5;
		let r = min(in.size.x, in.size.y) * 0.5;
		var d = length(in.local - c) - r;
		if (kind > 1.5) {
			// A ring: keep only the band of the given thickness.
			let thick = max(in.misc.x, 0.5);
			d = abs(length(in.local - c) - (r - thick * 0.5)) - thick * 0.5;
		}
		let cov = 1.0 - smoothstep(-aa, aa, d);
		if (cov <= 0.0) { discard; }

		// Sweep mask, measured from 12 o'clock clockwise. A filled circle has no
		// sweep, so only rings and arcs pay for it.
		let v = in.local - c;
		var ang = atan2(v.x, -v.y);
		if (ang < 0.0) { ang = ang + 2.0 * PI; }
		let start = in.params.z;
		let end = in.params.w;
		if (kind > 1.5 && end - start < 2.0 * PI - 0.01) {
			var a0 = start;
			var a1 = end;
			if (a1 < a0) { a1 = a1 + 2.0 * PI; }
			var a = ang;
			if (a < a0) { a = a + 2.0 * PI; }
			if (a > a0 + 0.001 && a < a1) {
				// inside the sweep
			} else {
				discard;
			}
		}
		return vec4f(in.color.rgb, in.color.a * cov);
	}

	// Rounded rectangle with an optional border.
	let r = corner_radius(in.radius, in.local, in.size);
	let d = sd_round_rect(in.local, in.size, r);
	let cov = 1.0 - smoothstep(-aa, aa, d);
	if (cov <= 0.0) { discard; }

	let bw = in.border.w;
	if (bw > 0.0) {
		// The fill and the border band are disjoint, and each carries its own
		// anti-aliased coverage. Blend them in premultiplied space, then convert
		// back to straight alpha so neither is darkened twice at the edges.
		// The band uses its own signed distance so a 1dp border keeps full
		// coverage along curves instead of losing it to two clipped steps.
		let inner = 1.0 - smoothstep(-aa, aa, d + bw);
		let band = abs(d + bw * 0.5) - bw * 0.5;
		let fa = in.color.a * inner;
		let ba = in.border.a * (1.0 - smoothstep(-aa, aa, band));
		let a = fa + ba;
		if (a <= 0.0) { discard; }
		return vec4f((in.color.rgb * fa + in.border.rgb * ba) / a, a);
	}
	return vec4f(in.color.rgb, cov * in.color.a);
}
`

// gpuInstance is the per-shape vertex data. 112 bytes, matching the WGSL struct.
type gpuInstance struct {
	Rect   [4]float32
	Radius [4]float32
	Color  [4]float32
	Border [4]float32
	Clip   [4]float32
	// ClipBox is the rounded shape's own bounds (x0, y0, x1, y1) when the clip
	// is rounded. The clip rect may be a smaller intersection, so rounding it
	// would put a corner wherever the visible area happens to cut the shape.
	ClipBox [4]float32
	Params  [4]float32 // kind, blur, startAngle, endAngle
	Misc    [4]float32 // thickness, uv far (glyphs, bitmaps), clip radius
}

// GPU is the WebGPU renderer.
type GPU struct {
	instance       *wgpu.Instance
	adapter        *wgpu.Adapter
	device         *wgpu.Device
	queue          *wgpu.Queue
	surface        *wgpu.Surface
	pipeline       *wgpu.RenderPipeline
	uniform        *wgpu.Buffer
	uniformWritten [4]float32
	storage        *wgpu.Buffer
	bindG          *wgpu.BindGroup
	format         gputypes.TextureFormat

	width, height int
	scale         float32
	vsync         bool
	capacity      int
	// images is the bitmap atlas; it exists even without a font.
	images    *imageRenderer
	instances []gpuInstance

	text       *textRenderer
	textLib    *text.Library
	textLayout *wgpu.BindGroupLayout

	closed bool
}

// TargetKind names the native handle pair passed to NewGPU.
type TargetKind int

// Target kinds.
const (
	TargetWayland TargetKind = iota
	TargetX11
)

// NewGPU creates a renderer for an existing native window.
// display and window are the platform handles: X11 Display*/Window or Wayland
// wl_display*/wl_surface*. The kind must match, because the legacy
// CreateSurface sniffs the environment and picks the wrong backend when a
// Wayland session also has DISPLAY set.
func NewGPU(display, window uintptr, kind TargetKind) (*GPU, error) {
	inst, err := wgpu.CreateInstance(nil)
	if err != nil {
		return nil, fmt.Errorf("wgpu instance: %w", err)
	}
	g := &GPU{instance: inst, scale: 1, vsync: true, capacity: 256}

	target := wgpu.SurfaceTargetFromWaylandSurface(display, window)
	if kind == TargetX11 {
		target = wgpu.SurfaceTargetFromXlibWindow(display, window)
	}
	surface, err := inst.CreateSurfaceUnsafe(target)
	if err != nil {
		inst.Release()
		return nil, fmt.Errorf("wgpu surface: %w", err)
	}
	g.surface = surface

	adapter, err := inst.RequestAdapter(&wgpu.RequestAdapterOptions{CompatibleSurface: surface})
	if err != nil {
		g.Release()
		return nil, fmt.Errorf("wgpu adapter: %w", err)
	}
	g.adapter = adapter

	device, err := adapter.RequestDevice(nil)
	if err != nil {
		g.Release()
		return nil, fmt.Errorf("wgpu device: %w", err)
	}
	g.device = device
	g.queue = device.Queue()

	caps := adapter.GetSurfaceCapabilities(surface)
	if caps == nil || len(caps.Formats) == 0 {
		g.Release()
		return nil, fmt.Errorf("wgpu surface exposes no formats")
	}
	g.format = pickFormat(caps.Formats)

	g.textLib = text.NewLibrary()
	// The atlas binding is part of the shape pipeline, so it must exist first.
	if err := g.initText(g.textLib); err != nil {
		g.Release()
		return nil, err
	}
	if err := g.initImages(); err != nil {
		g.Release()
		return nil, err
	}
	if err := g.initPipeline(); err != nil {
		g.Release()
		return nil, err
	}
	return g, nil
}

// pickFormat prefers a non-sRGB unorm format so literal hex colors stay exact.
func pickFormat(formats []gputypes.TextureFormat) gputypes.TextureFormat {
	for _, want := range []gputypes.TextureFormat{gputypes.TextureFormatBGRA8Unorm, gputypes.TextureFormatRGBA8Unorm} {
		for _, f := range formats {
			if f == want {
				return f
			}
		}
	}
	return formats[0]
}

func (g *GPU) initPipeline() error {
	shader, err := g.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "shapes", WGSL: shaderWGSL})
	if err != nil {
		return fmt.Errorf("shader: %w", err)
	}
	defer shader.Release()

	g.uniform, err = g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "viewport",
		Usage: gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst,
		Size:  16,
	})
	if err != nil {
		return fmt.Errorf("uniform buffer: %w", err)
	}
	g.storage, err = g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "instances",
		Usage: gputypes.BufferUsageStorage | gputypes.BufferUsageCopyDst,
		Size:  g.storageSize(g.capacity),
	})
	if err != nil {
		return fmt.Errorf("storage buffer: %w", err)
	}

	layout, err := g.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "shapes-layout",
		Entries: shapeLayoutEntries(),
	})
	if err != nil {
		return fmt.Errorf("bind group layout: %w", err)
	}
	defer layout.Release()
	if err := g.rebind(); err != nil {
		return fmt.Errorf("bind group: %w", err)
	}

	pl, err := g.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "shapes-pipeline-layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{layout},
	})
	if err != nil {
		return fmt.Errorf("pipeline layout: %w", err)
	}
	defer pl.Release()

	blend := &gputypes.BlendState{
		Color: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorSrcAlpha, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd},
		Alpha: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd},
	}
	attrs := []gputypes.VertexAttribute{
		{Format: gputypes.VertexFormatFloat32x4, Offset: 0, ShaderLocation: 0},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 16, ShaderLocation: 1},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 32, ShaderLocation: 2},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 48, ShaderLocation: 3},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 64, ShaderLocation: 4},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 80, ShaderLocation: 5},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 96, ShaderLocation: 6},
	}
	g.pipeline, err = g.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "shapes",
		Layout: pl,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []gputypes.VertexBufferLayout{{
				ArrayStride: uint64(unsafe.Sizeof(gpuInstance{})),
				StepMode:    gputypes.VertexStepModeInstance,
				Attributes:  attrs,
			}},
		},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets:    []gputypes.ColorTargetState{{Format: g.format, Blend: blend, WriteMask: gputypes.ColorWriteMaskAll}},
		},
		Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, FrontFace: gputypes.FrontFaceCCW, CullMode: gputypes.CullModeNone},
		Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xFFFFFFFF},
	})
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}
	return nil
}

// shapeLayoutEntries is the one bind-group shape the pipeline and any rebuilt
// bind group share: viewport, instances, atlas, sampler.
func shapeLayoutEntries() []gputypes.BindGroupLayoutEntry {
	return []gputypes.BindGroupLayoutEntry{
		{
			Binding: 0, Visibility: gputypes.ShaderStageVertex | gputypes.ShaderStageFragment,
			Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform},
		},
		{
			Binding: 1, Visibility: gputypes.ShaderStageVertex,
			Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage},
		},
		{Binding: 2, Visibility: gputypes.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{
			SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D, Multisampled: false,
		}},
		{
			Binding: 3, Visibility: gputypes.ShaderStageFragment,
			Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering},
		},
		{Binding: 4, Visibility: gputypes.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{
			SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D, Multisampled: false,
		}},
		{
			Binding: 5, Visibility: gputypes.ShaderStageFragment,
			Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering},
		},
	}
}

func (g *GPU) storageSize(n int) uint64 {
	return uint64(n) * uint64(unsafe.Sizeof(gpuInstance{}))
}

// TextLibrary exposes the font library so layout can measure real text.
func (g *GPU) TextLibrary() any { return g.textLib }

// Text returns the font library typed, for the platform layer.
func (g *GPU) Text() *text.Library { return g.textLib }

// Resize reconfigures the swapchain for a new framebuffer size.
func (g *GPU) Resize(width, height int, scale float32) error {
	if g.closed || width <= 0 || height <= 0 {
		return nil
	}
	if width == g.width && height == g.height && scale == g.scale {
		return nil
	}
	g.width, g.height, g.scale = width, height, scale
	return g.configure()
}

// SetVSync ties presentation to the display refresh. Turning it off lets the
// app draw faster than the panel, which is what makes the FPS overlay show the
// renderer's rate. A surface without a non-vsync mode stays on vsync and the
// error says so.
func (g *GPU) SetVSync(on bool) error {
	if g.closed || g.vsync == on {
		return nil
	}
	g.vsync = on
	if err := g.configure(); err != nil && !on {
		g.vsync = true
		_ = g.configure()
		return err
	}
	return nil
}

func (g *GPU) configure() error {
	if g.closed || g.width <= 0 || g.height <= 0 {
		return nil
	}
	mode := gputypes.PresentModeFifo
	if !g.vsync {
		mode = gputypes.PresentModeMailbox
	}
	return g.surface.Configure(g.device, &wgpu.SurfaceConfiguration{
		Width:       uint32(g.width),
		Height:      uint32(g.height),
		Format:      g.format,
		Usage:       gputypes.TextureUsageRenderAttachment,
		PresentMode: mode,
		AlphaMode:   gputypes.CompositeAlphaModeOpaque,
	})
}

// Render converts the display list to instances, uploads them and draws.
func (g *GPU) Render(list *paint.List, width, height int) error {
	if g.closed || width <= 0 || height <= 0 {
		return nil
	}
	if width != g.width || height != g.height {
		if err := g.Resize(width, height, g.scale); err != nil {
			return err
		}
	}

	g.instances = g.instances[:0]
	// Reserve every glyph this frame needs before emitting, so an atlas
	// compaction can only drop entries the frame does not reference.
	if g.text != nil {
		g.text.prepare(list)
	}
	if g.images != nil {
		g.images.prepare(list)
	}
	for i, op := range list.Ops {
		switch op.Kind {
		case paint.Fill:
			g.instances = append(g.instances, gpuInstance{
				Rect:    snapRect(op.Rect, g.scale),
				Radius:  op.Radius,
				Color:   rgba(op.Color),
				Border:  borderVec(op.BorderColor, op.BorderWidth),
				Clip:    clipVec(op.Clip),
				ClipBox: clipVec(op.ClipBox),
				Params:  [4]float32{shapeKind(op.Shape), 0, op.StartAngle, op.EndAngle},
				Misc:    [4]float32{op.Thickness, 0, op.ClipRadius, 0},
			})
		case paint.Shadow:
			kind := float32(3)
			if op.Inset {
				kind = 6
			}
			g.instances = append(g.instances, gpuInstance{
				Rect:    snapRect(op.Rect, g.scale),
				Radius:  op.Radius,
				Color:   rgba(op.Color),
				Clip:    clipVec(op.Clip),
				ClipBox: clipVec(op.ClipBox),
				Params:  [4]float32{kind, op.Blur, op.OffsetX, op.OffsetY},
				Misc:    [4]float32{0, 0, op.ClipRadius, op.Spread},
			})
		case paint.Text:
			if g.text != nil {
				g.text.emit(i)
			}
		case paint.Bitmap:
			g.images.addOp(op)
		}
	}
	if len(g.instances) > g.capacity {
		if err := g.grow(len(g.instances)); err != nil {
			return err
		}
	}

	// The viewport uniform only changes on a resize or a scale change. Writing
	// it every frame is not free: wgpu's Vulkan backend drains the queue on
	// every Queue.WriteBuffer, which costs a frame whenever it lands badly.
	uniform := [4]float32{
		float32(width) / maxf(g.scale, 0.01),
		float32(height) / maxf(g.scale, 0.01),
		g.scale, 0,
	}
	if uniform != g.uniformWritten {
		g.uniformWritten = uniform
		if err := g.queue.WriteBuffer(g.uniform, 0, floatsToBytes(uniform[:])); err != nil {
			return err
		}
	}
	if len(g.instances) > 0 {
		if err := g.queue.WriteBuffer(g.storage, 0, instancesToBytes(g.instances)); err != nil {
			return err
		}
	}
	if g.text != nil {
		if err := g.text.upload(g); err != nil {
			return err
		}
	}
	if g.images != nil {
		if err := g.images.upload(g); err != nil {
			return err
		}
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
	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       view,
			LoadOp:     gputypes.LoadOpClear,
			StoreOp:    gputypes.StoreOpStore,
			ClearValue: clearColor(list),
		}},
	})
	if err != nil {
		return err
	}
	if len(g.instances) > 0 {
		pass.SetPipeline(g.pipeline)
		pass.SetBindGroup(0, g.bindG, nil)
		pass.SetVertexBuffer(0, g.storage, 0)
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: uint32(len(g.instances))})
	}
	if err := pass.End(); err != nil {
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

// grow reallocates the instance buffer and its bind group.
func (g *GPU) grow(n int) error {
	capacity := g.capacity * 2
	for capacity < n {
		capacity *= 2
	}
	buf, err := g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "instances",
		Usage: gputypes.BufferUsageStorage | gputypes.BufferUsageCopyDst,
		Size:  g.storageSize(capacity),
	})
	if err != nil {
		return err
	}
	g.storage.Release()
	g.storage, g.capacity = buf, capacity
	return g.rebind()
}

// rebind rebuilds the bind group from the current buffers and textures. A
// recreated atlas texture (growth) changes the view, so the group must follow.
func (g *GPU) rebind() error {
	layout, err := g.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "shapes-layout",
		Entries: shapeLayoutEntries(),
	})
	if err != nil {
		return err
	}
	defer layout.Release()
	bg, err := g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "shapes-bindings",
		Layout: layout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: g.uniform, Size: 16},
			{Binding: 1, Buffer: g.storage, Size: g.storageSize(g.capacity)},
			{Binding: 2, TextureView: g.textView()},
			{Binding: 3, Sampler: g.textSampler()},
			{Binding: 4, TextureView: g.imageView()},
			{Binding: 5, Sampler: g.imageSampler()},
		},
	})
	if err != nil {
		return err
	}
	if g.bindG != nil {
		g.bindG.Release()
	}
	g.bindG = bg
	return nil
}

// The accessors keep rebind working before one of the sub-renderers exists
// (a build with no font, or a test that never initialised images).
func (g *GPU) textView() *wgpu.TextureView {
	if g.text == nil {
		return nil
	}
	return g.text.view
}

func (g *GPU) textSampler() *wgpu.Sampler {
	if g.text == nil {
		return nil
	}
	return g.text.sampler
}

func (g *GPU) imageView() *wgpu.TextureView {
	if g.images == nil {
		return nil
	}
	return g.images.view
}

func (g *GPU) imageSampler() *wgpu.Sampler {
	if g.images == nil {
		return nil
	}
	return g.images.sampler
}

// Release frees every GPU resource. Safe to call twice.
func (g *GPU) Release() {
	if g == nil || g.closed {
		return
	}
	g.closed = true
	if g.text != nil {
		g.text.release()
	}
	if g.textLayout != nil {
		g.textLayout.Release()
	}
	if g.pipeline != nil {
		g.pipeline.Release()
	}
	if g.bindG != nil {
		g.bindG.Release()
	}
	if g.storage != nil {
		g.storage.Release()
	}
	if g.uniform != nil {
		g.uniform.Release()
	}
	if g.surface != nil {
		g.surface.Release()
	}
	if g.device != nil {
		g.device.Release()
	}
	if g.adapter != nil {
		g.adapter.Release()
	}
	if g.instance != nil {
		g.instance.Release()
	}
}

// shapeKind maps a paint shape to the shader's kind: 0 rect, 1 circle/arc,
// 2 shadow. Arc and Circle share the ring code.
// clearColor is the surface color behind the display list. The window rarely
// paints its own background, so the theme's surface is what the page shows: using
// it here means switching the theme also switches the page, and saves a
// full-window fill every frame.
func clearColor(list *paint.List) gputypes.Color {
	if list == nil || list.Theme == nil {
		return gputypes.Color{R: 0, G: 0, B: 0, A: 1}
	}
	c := list.Theme.Colors.Surface
	return gputypes.Color{
		R: float64((c>>24)&0xFF) / 255,
		G: float64((c>>16)&0xFF) / 255,
		B: float64((c>>8)&0xFF) / 255,
		A: 1,
	}
}

// snapRect rounds a box's edges to whole device pixels. A 1dp border at a
// fractional x starts on two rows at 90% and 10% coverage, which reads as a
// broken line; snapped, both edges land on one row each. Adjacent boxes share
// their edges, so snapping them all keeps them flush.
func snapRect(r [4]float32, scale float32) [4]float32 {
	s := scale
	if s <= 0 {
		s = 1
	}
	x0 := math.Round(float64(r[0])*float64(s)) / float64(s)
	y0 := math.Round(float64(r[1])*float64(s)) / float64(s)
	x1 := math.Round(float64(r[0]+r[2])*float64(s)) / float64(s)
	y1 := math.Round(float64(r[1]+r[3])*float64(s)) / float64(s)
	return [4]float32{float32(x0), float32(y0), float32(x1 - x0), float32(y1 - y0)}
}

func shapeKind(s paint.Shape) float32 {
	switch s {
	case paint.Circle:
		return 1
	case paint.Arc:
		return 2
	}
	return 0
}

// borderVec packs a border color and width; a zero width disables the border.
func borderVec(c uint32, w float32) [4]float32 {
	if c == 0 || w <= 0 {
		return [4]float32{0, 0, 0, 0}
	}
	v := rgba(c)
	v[3] = w
	return v
}

func rgba(c uint32) [4]float32 {
	const s = 1.0 / 255.0
	return [4]float32{
		float32((c>>24)&0xFF) * s,
		float32((c>>16)&0xFF) * s,
		float32((c>>8)&0xFF) * s,
		float32(c&0xFF) * s,
	}
}

// clipVec converts a display-list clip to x0, y0, x1, y1.
func clipVec(c [4]float32) [4]float32 {
	return [4]float32{c[0], c[1], c[0] + c[2], c[1] + c[3]}
}

func floatsToBytes(f []float32) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&f[0])), len(f)*4)
}

func instancesToBytes(inst []gpuInstance) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&inst[0])), len(inst)*int(unsafe.Sizeof(gpuInstance{})))
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
