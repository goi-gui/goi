// Command spike is the Phase 0 tech-freeze proof: a GLFW window with a WebGPU
// surface, a HiDPI-aware viewport uniform, and instanced SDF rounded rects
// with borders and anti-aliasing.
//
// It is throwaway code that only exists to validate the bindings, the surface
// path and the shader approach before the real packages are written.
package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"unsafe"

	"github.com/go-gl/glfw/v3.4/glfw"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"

	_ "github.com/gogpu/wgpu/hal/allbackends"
)

const shaderWGSL = `
struct Instance {
	rect: vec4f,   // x, y, w, h in logical pixels
	radius: vec4f, // per-corner radius: tl, tr, br, bl
	color: vec4f,
	border: vec4f, // rgb + width
}

struct Viewport {
	size: vec4f, // w, h in logical pixels, device pixel ratio, unused
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

	// Anti-aliasing width in logical pixels, widened for HiDPI.
	let aa = 0.5 / max(viewport.size.z, 1.0);
	let coverage = 1.0 - smoothstep(-aa, aa, d);
	if (coverage <= 0.0) { discard; }

	let bw = in.border.w;
	if (bw > 0.0) {
		let inner = 1.0 - smoothstep(-aa, aa, d + bw);
		let edge = coverage - inner;
		return vec4f(in.color.rgb * inner + in.border.rgb * edge, coverage);
	}
	return vec4f(in.color.rgb, coverage * in.color.a);
}
`

type instance struct {
	Rect   [4]float32
	Radius [4]float32
	Color  [4]float32
	Border [4]float32
}

type app struct {
	win       *glfw.Window
	instance  *wgpu.Instance
	adapter   *wgpu.Adapter
	device    *wgpu.Device
	queue     *wgpu.Queue
	surface   *wgpu.Surface
	pipeline  *wgpu.RenderPipeline
	uniform   *wgpu.Buffer
	storage   *wgpu.Buffer
	bindGroup *wgpu.BindGroup
	format    gputypes.TextureFormat
	capacity  int // instances the storage buffer can hold

	width, height int
	scale         float32
	limit         int // stop after N frames when > 0 (smoke test)
	drawn         int
}

func main() {
	runtime.LockOSThread()

	if err := glfw.Init(); err != nil {
		log.Fatalf("glfw init: %v", err)
	}
	defer glfw.Terminate()

	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	glfw.WindowHint(glfw.ScaleToMonitor, glfw.True)
	win, err := glfw.CreateWindow(640, 400, "goi spike", nil, nil)
	if err != nil {
		log.Fatalf("glfw window: %v", err)
	}
	defer win.Destroy()

	a := &app{win: win, limit: intFromEnv("SPIKE_FRAMES", 0)}
	if err := a.initGPU(); err != nil {
		log.Fatalf("init gpu: %v", err)
	}
	defer a.release()

	for !win.ShouldClose() {
		glfw.PollEvents()
		if err := a.frame(); err != nil {
			log.Fatalf("frame: %v", err)
		}
		a.drawn++
		if a.limit > 0 && a.drawn >= a.limit {
			break
		}
	}
	fmt.Printf("rendered %d frames, %dx%d @%.2fx\n", a.drawn, a.width, a.height, a.scale)
}

func intFromEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var n int
	fmt.Sscanf(v, "%d", &n)
	return n
}

func floatFromEnv(key string, def float32) float32 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var f float32
	fmt.Sscanf(v, "%g", &f)
	return f
}

// pickFormat prefers a non-sRGB unorm target so authored sRGB hex colors can be// written as-is; sRGB formats would need a color-space conversion in the shader.
func pickFormat(formats []gputypes.TextureFormat) gputypes.TextureFormat {
	for _, want := range []gputypes.TextureFormat{gputypes.TextureFormatBGRA8Unorm, gputypes.TextureFormatRGBA8Unorm} {
		for _, f := range formats {
			if f == want {
				return f
			}
		}
	}
	return 0
}

func (a *app) initGPU() error {
	inst, err := wgpu.CreateInstance(nil)
	if err != nil {
		return fmt.Errorf("create instance: %w", err)
	}
	a.instance = inst

	a.surface, err = a.createSurface()
	if err != nil {
		return fmt.Errorf("create surface: %w", err)
	}

	adapter, err := inst.RequestAdapter(&wgpu.RequestAdapterOptions{CompatibleSurface: a.surface})
	if err != nil {
		return fmt.Errorf("request adapter: %w", err)
	}
	a.adapter = adapter
	if info := adapter.Info(); true {
		fmt.Printf("adapter: %s (%v)\n", info.Name, info.Backend)
	}

	device, err := adapter.RequestDevice(nil)
	if err != nil {
		return fmt.Errorf("request device: %w", err)
	}
	a.device = device
	a.queue = device.Queue()

	caps := adapter.GetSurfaceCapabilities(a.surface)
	if caps == nil || len(caps.Formats) == 0 {
		return fmt.Errorf("surface reports no formats")
	}
	a.format = pickFormat(caps.Formats)
	if a.format == 0 {
		return fmt.Errorf("no non-sRGB unorm surface format in %v", caps.Formats)
	}
	fmt.Printf("surface formats: %v -> %v\n", caps.Formats, a.format)

	shader, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "sdf", WGSL: shaderWGSL})
	if err != nil {
		return fmt.Errorf("shader: %w", err)
	}
	defer shader.Release()

	a.uniform, err = device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "viewport",
		Usage: gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst,
		Size:  16,
	})
	if err != nil {
		return fmt.Errorf("uniform buffer: %w", err)
	}

	a.capacity = 64
	a.storage, err = device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "instances",
		Usage: gputypes.BufferUsageStorage | gputypes.BufferUsageCopyDst,
		Size:  uint64(a.capacity) * uint64(unsafe.Sizeof(instance{})),
	})
	if err != nil {
		return fmt.Errorf("storage buffer: %w", err)
	}

	layout, err := device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "sdf-layout",
		Entries: []gputypes.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gputypes.ShaderStageVertex | gputypes.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}},
			{Binding: 1, Visibility: gputypes.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}},
		},
	})
	if err != nil {
		return fmt.Errorf("bind group layout: %w", err)
	}
	defer layout.Release()

	a.bindGroup, err = device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "sdf-bindings",
		Layout: layout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: a.uniform, Size: 16},
			{Binding: 1, Buffer: a.storage, Size: uint64(a.capacity) * uint64(unsafe.Sizeof(instance{}))},
		},
	})
	if err != nil {
		return fmt.Errorf("bind group: %w", err)
	}

	pipelineLayout, err := device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "sdf-pipeline-layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{layout},
	})
	if err != nil {
		return fmt.Errorf("pipeline layout: %w", err)
	}
	defer pipelineLayout.Release()

	blend := &gputypes.BlendState{
		Color: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorSrcAlpha, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd},
		Alpha: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorSrcAlpha, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd},
	}
	attrs := []gputypes.VertexAttribute{
		{Format: gputypes.VertexFormatFloat32x4, Offset: 0, ShaderLocation: 0},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 16, ShaderLocation: 1},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 32, ShaderLocation: 2},
		{Format: gputypes.VertexFormatFloat32x4, Offset: 48, ShaderLocation: 3},
	}
	a.pipeline, err = device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "sdf",
		Layout: pipelineLayout,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []gputypes.VertexBufferLayout{{
				ArrayStride: uint64(unsafe.Sizeof(instance{})),
				StepMode:    gputypes.VertexStepModeInstance,
				Attributes:  attrs,
			}},
		},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets:    []gputypes.ColorTargetState{{Format: a.format, Blend: blend, WriteMask: gputypes.ColorWriteMaskAll}},
		},
		Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, FrontFace: gputypes.FrontFaceCCW, CullMode: gputypes.CullModeNone},
		Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xFFFFFFFF},
	})
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}
	return nil
}

func (a *app) configureSurface() error {
	return a.surface.Configure(a.device, &wgpu.SurfaceConfiguration{
		Width:       uint32(a.width),
		Height:      uint32(a.height),
		Format:      a.format,
		Usage:       gputypes.TextureUsageRenderAttachment,
		AlphaMode:   gputypes.CompositeAlphaModeOpaque,
		PresentMode: gputypes.PresentModeFifo,
	})
}

func (a *app) frame() error {
	fbw, fbh := a.win.GetFramebufferSize()
	xs, _ := a.win.GetContentScale()
	a.scale = xs
	if s := floatFromEnv("SPIKE_SCALE", 0); s > 0 {
		a.scale = s // simulate HiDPI: forces the logical/device split without a 2x monitor
	}
	if fbw == 0 || fbh == 0 {
		return nil // minimized
	}
	if fbw != a.width || fbh != a.height {
		a.width, a.height = fbw, fbh
		if err := a.configureSurface(); err != nil {
			return err
		}
		fmt.Printf("resize: %dx%d\n", a.width, a.height)
	}

	logicalW := float32(a.width) / a.scale
	logicalH := float32(a.height) / a.scale
	uniform := []float32{logicalW, logicalH, a.scale, 0}
	uniformBytes := unsafe.Slice((*byte)(unsafe.Pointer(&uniform[0])), 16)
	if err := a.queue.WriteBuffer(a.uniform, 0, uniformBytes); err != nil {
		return err
	}

	scene := buildScene(logicalW, logicalH)
	sceneBytes := unsafe.Slice((*byte)(unsafe.Pointer(&scene[0])), len(scene)*int(unsafe.Sizeof(instance{})))
	if err := a.queue.WriteBuffer(a.storage, 0, sceneBytes); err != nil {
		return err
	}

	tex, _, err := a.surface.GetCurrentTexture()
	if err != nil {
		return err
	}

	view, err := tex.CreateView(nil)
	if err != nil {
		return err
	}
	defer view.Release()

	encoder, err := a.device.CreateCommandEncoder(nil)
	if err != nil {
		return err
	}
	pass, err := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       view,
			LoadOp:     gputypes.LoadOpClear,
			StoreOp:    gputypes.StoreOpStore,
			ClearValue: gputypes.Color{R: 0.12, G: 0.13, B: 0.16, A: 1},
		}},
	})
	if err != nil {
		return err
	}
	pass.SetPipeline(a.pipeline)
	pass.SetBindGroup(0, a.bindGroup, nil)
	pass.SetVertexBuffer(0, a.storage, 0)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: uint32(len(scene))})
	if err := pass.End(); err != nil {
		return err
	}

	cmdbuf, err := encoder.Finish()
	if err != nil {
		return err
	}
	if _, err := a.queue.Submit(cmdbuf); err != nil {
		return err
	}
	return a.surface.Present(tex)
}

func buildScene(w, h float32) []instance {
	return []instance{
		// Panel with a large radius.
		{Rect: [4]float32{16, 16, w - 32, h - 32}, Radius: [4]float32{24, 24, 24, 24}, Color: [4]float32{0.18, 0.20, 0.25, 1}},
		// Nested rounded rects, exercising per-corner radii.
		{Rect: [4]float32{40, 40, 200, 120}, Radius: [4]float32{0, 32, 0, 32}, Color: [4]float32{0.23, 0.51, 0.96, 1}, Border: [4]float32{1, 1, 1, 3}},
		{Rect: [4]float32{70, 190, 200, 120}, Radius: [4]float32{32, 0, 32, 0}, Color: [4]float32{0.55, 0.36, 0.96, 1}, Border: [4]float32{0.9, 0.9, 0.9, 1}},
		{Rect: [4]float32{320, 60, 120, 60}, Radius: [4]float32{30, 30, 30, 30}, Color: [4]float32{0.13, 0.77, 0.37, 1}},
		// Thin border-only rect: verifies edge coverage on both sides.
		{Rect: [4]float32{320, 150, 160, 90}, Radius: [4]float32{12, 12, 12, 12}, Color: [4]float32{0.18, 0.20, 0.25, 1}, Border: [4]float32{1, 1, 1, 2}},
	}
}

func (a *app) release() {
	if a.pipeline != nil {
		a.pipeline.Release()
	}
	if a.bindGroup != nil {
		a.bindGroup.Release()
	}
	if a.storage != nil {
		a.storage.Release()
	}
	if a.uniform != nil {
		a.uniform.Release()
	}
	if a.surface != nil {
		a.surface.Release()
	}
	if a.device != nil {
		a.device.Release()
	}
	if a.adapter != nil {
		a.adapter.Release()
	}
	if a.instance != nil {
		a.instance.Release()
	}
}
