//go:build !nogpu

package render

import (
	"fmt"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/ksckaan1/goui/internal/paint"
	"github.com/ksckaan1/goui/internal/text"

	_ "github.com/gogpu/wgpu/hal/allbackends"
)

const shaderWGSL = `
struct Instance {
	rect: vec4f,    // x, y, w, h in logical pixels
	radius: vec4f,  // tl, tr, br, bl
	color: vec4f,
	border: vec4f,  // rgb + width
}

struct Viewport {
	size: vec4f, // logical w, logical h, scale, unused
}

@group(0) @binding(0) var<uniform> viewport: Viewport;
@group(0) @binding(1) var<storage, read> instances: array<Instance>;

struct VSOut {
	@builtin(position) pos: vec4f,
	@location(0) local: vec2f,
	@location(1) size: vec2f,
	@location(2) radius: vec4f,
	@location(3) color: vec4f,
	@location(4) border: vec4f,
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

@fragment
fn fs_main(in: VSOut) -> @location(0) vec4f {
	let r = corner_radius(in.radius, in.local, in.size);
	let h = in.size * 0.5;
	let q = abs(in.local - h) - (h - vec2f(r));
	let d = length(max(q, vec2f(0.0))) + min(max(q.x, q.y), 0.0) - r;

	// AA width in logical pixels, widened so it stays one device pixel.
	let aa = 0.5 / max(viewport.size.z, 1.0);
	let coverage = 1.0 - smoothstep(-aa, aa, d);
	if (coverage <= 0.0) { discard; }

	let bw = in.border.w;
	if (bw > 0.0) {
		let inner = 1.0 - smoothstep(-aa, aa, d + bw);
		let edge = max(coverage - inner, 0.0);
		let rgb = in.color.rgb * inner + in.border.rgb * edge;
		let a = in.color.a * inner + in.border.a * edge;
		return vec4f(rgb, a * coverage);
	}
	return vec4f(in.color.rgb, coverage * in.color.a);
}
`

// gpuInstance is the per-rect vertex data. 64 bytes, matching the WGSL struct.
type gpuInstance struct {
	Rect   [4]float32
	Radius [4]float32
	Color  [4]float32
	Border [4]float32
}

// GPU is the WebGPU renderer.
type GPU struct {
	instance *wgpu.Instance
	adapter  *wgpu.Adapter
	device   *wgpu.Device
	queue    *wgpu.Queue
	surface  *wgpu.Surface
	pipeline *wgpu.RenderPipeline
	uniform  *wgpu.Buffer
	storage  *wgpu.Buffer
	bindG    *wgpu.BindGroup
	format   gputypes.TextureFormat

	width, height int
	scale         float32
	capacity      int
	instances     []gpuInstance

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
	g := &GPU{instance: inst, scale: 1, capacity: 256}

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

	if err := g.initPipeline(); err != nil {
		g.Release()
		return nil, err
	}
	g.textLib = text.NewLibrary()
	if err := g.initText(g.textLib); err != nil {
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
	shader, err := g.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "sdf", WGSL: shaderWGSL})
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
		Label: "sdf-layout",
		Entries: []gputypes.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gputypes.ShaderStageVertex | gputypes.ShaderStageFragment,
				Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}},
			{Binding: 1, Visibility: gputypes.ShaderStageVertex,
				Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}},
		},
	})
	if err != nil {
		return fmt.Errorf("bind group layout: %w", err)
	}
	defer layout.Release()

	g.bindG, err = g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "sdf-bindings",
		Layout: layout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: g.uniform, Size: 16},
			{Binding: 1, Buffer: g.storage, Size: g.storageSize(g.capacity)},
		},
	})
	if err != nil {
		return fmt.Errorf("bind group: %w", err)
	}

	pl, err := g.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "sdf-pipeline-layout",
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
	}
	g.pipeline, err = g.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "sdf",
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

// TextLibrary exposes the font library so layout can measure real text.
func (g *GPU) TextLibrary() any { return g.textLib }

// Text returns the font library typed, for the platform layer.
func (g *GPU) Text() *text.Library { return g.textLib }

// Format returns the surface format in use, for diagnostics.
func (g *GPU) Format() uint32 { return uint32(g.format) }

func (g *GPU) storageSize(n int) uint64 {
	return uint64(n) * uint64(unsafe.Sizeof(gpuInstance{}))
}

// Resize reconfigures the swapchain for a new framebuffer size.
func (g *GPU) Resize(width, height int, scale float32) error {
	if g.closed || width <= 0 || height <= 0 {
		return nil
	}
	if width == g.width && height == g.height && scale == g.scale {
		return nil
	}
	g.width, g.height, g.scale = width, height, scale
	return g.surface.Configure(g.device, &wgpu.SurfaceConfiguration{
		Width:       uint32(width),
		Height:      uint32(height),
		Format:      g.format,
		Usage:       gputypes.TextureUsageRenderAttachment,
		PresentMode: gputypes.PresentModeFifo,
		AlphaMode:   gputypes.CompositeAlphaModeOpaque,
	})
}

// Render converts the display list to instances, uploads them and draws.
// shortcut: one instance per op, one draw call, no batching across clips yet.
// The renderer ignores clipping for now; nested content is clipped by layout.
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
	for _, op := range list.Ops {
		if op.Kind != paint.Fill {
			continue // text arrives with the text package
		}
		border := rgba(op.BorderColor)
		border[3] = op.BorderWidth
		inst := gpuInstance{
			Rect:   op.Rect,
			Radius: op.Radius,
			Color:  rgba(op.Color),
			Border: border,
		}
		if op.BorderColor == 0 || op.BorderWidth <= 0 {
			inst.Border[3] = 0
		}
		g.instances = append(g.instances, inst)
	}
	if len(g.instances) > g.capacity {
		if err := g.grow(len(g.instances)); err != nil {
			return err
		}
	}

	uniform := []float32{
		float32(width) / maxf(g.scale, 0.01),
		float32(height) / maxf(g.scale, 0.01),
		g.scale, 0,
	}
	if err := g.queue.WriteBuffer(g.uniform, 0, floatsToBytes(uniform)); err != nil {
		return err
	}
	if len(g.instances) > 0 {
		if err := g.queue.WriteBuffer(g.storage, 0, instancesToBytes(g.instances)); err != nil {
			return err
		}
	}

	if g.text != nil {
		g.text.build(g, list)
		if len(g.text.inst) > 0 {
			if err := g.text.upload(g); err != nil {
				return err
			}
			if err := g.queue.WriteBuffer(g.text.storage, 0, glyphsToBytes(g.text.inst)); err != nil {
				return err
			}
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
			ClearValue: gputypes.Color{R: 1, G: 0, B: 1, A: 1},
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
	if g.text != nil && len(g.text.inst) > 0 {
		pass.SetPipeline(g.text.pipeline)
		pass.SetBindGroup(0, g.text.bindG, nil)
		pass.SetVertexBuffer(0, g.text.storage, 0)
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: uint32(len(g.text.inst))})
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
	layout, err := g.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "sdf-layout",
		Entries: []gputypes.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gputypes.ShaderStageVertex | gputypes.ShaderStageFragment,
				Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}},
			{Binding: 1, Visibility: gputypes.ShaderStageVertex,
				Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}},
		},
	})
	if err != nil {
		buf.Release()
		return err
	}
	defer layout.Release()
	bg, err := g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: layout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: g.uniform, Size: 16},
			{Binding: 1, Buffer: buf, Size: g.storageSize(capacity)},
		},
	})
	if err != nil {
		buf.Release()
		return err
	}
	g.storage.Release()
	g.bindG.Release()
	g.storage, g.bindG, g.capacity = buf, bg, capacity
	return nil
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

func rgba(c uint32) [4]float32 {
	const s = 1.0 / 255.0
	return [4]float32{
		float32((c>>24)&0xFF) * s,
		float32((c>>16)&0xFF) * s,
		float32((c>>8)&0xFF) * s,
		float32(c&0xFF) * s,
	}
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

func glyphsToBytes(glyphs []glyphInstance) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&glyphs[0])), len(glyphs)*int(unsafe.Sizeof(glyphInstance{})))
}
