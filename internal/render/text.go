//go:build !nogpu

package render

import (
	"fmt"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/ksckaan1/goui/internal/paint"
	"github.com/ksckaan1/goui/internal/text"
)

const textWGSL = `
struct GlyphInstance {
	rect: vec4f,  // x, y, w, h in logical pixels
	uv: vec4f,    // u0, v0, u1, v1
	color: vec4f,
	clip: vec4f,  // x0, y0, x1, y1
}

struct Viewport {
	size: vec4f, // logical w, logical h, scale, unused
}

@group(0) @binding(0) var<uniform> viewport: Viewport;
@group(0) @binding(1) var atlas: texture_2d<f32>;
@group(0) @binding(2) var samp: sampler;
@group(0) @binding(3) var<storage, read> glyphs: array<GlyphInstance>;

struct VSOut {
	@builtin(position) pos: vec4f,
	@location(0) uv: vec2f,
	@location(1) color: vec4f,
	@location(2) clip: vec4f,
	@location(3) pos2: vec2f,
}

@vertex
fn vs_main(@builtin(vertex_index) vi: u32, @builtin(instance_index) ii: u32) -> VSOut {
	var corners = array<vec2f, 6>(
		vec2f(0.0, 0.0), vec2f(1.0, 0.0), vec2f(0.0, 1.0),
		vec2f(0.0, 1.0), vec2f(1.0, 0.0), vec2f(1.0, 1.0),
	);
	let inst = glyphs[ii];
	let c = corners[vi];
	let p = inst.rect.xy + c * inst.rect.zw;
	var out: VSOut;
	out.pos = vec4f(p.x / viewport.size.x * 2.0 - 1.0, 1.0 - p.y / viewport.size.y * 2.0, 0.0, 1.0);
	out.uv = mix(inst.uv.xy, inst.uv.zw, c);
	out.color = inst.color;
	out.clip = inst.clip;
	out.pos2 = p;
	return out;
}

@fragment
fn fs_main(in: VSOut) -> @location(0) vec4f {
	if (in.pos2.x < in.clip.x || in.pos2.y < in.clip.y ||
	    in.pos2.x > in.clip.z || in.pos2.y > in.clip.w) { discard; }
	let a = textureSample(atlas, samp, in.uv).r;
	return vec4f(in.color.rgb, in.color.a * a);
}
`

// glyphInstance is one textured quad. 48 bytes.
type glyphInstance struct {
	Rect  [4]float32
	UV    [4]float32
	Color [4]float32
	Clip  [4]float32
}

// textRenderer owns the atlas texture, its pipeline and instance buffer.
type textRenderer struct {
	pipeline *wgpu.RenderPipeline
	bindG    *wgpu.BindGroup
	texture  *wgpu.Texture
	view     *wgpu.TextureView
	sampler  *wgpu.Sampler
	storage  *wgpu.Buffer
	capacity int
	inst     []glyphInstance

	atlasW, atlasH int
	atlasDirty     bool

	gpu *GPU // back-reference for buffers and the atlas library
}

func (g *GPU) initText(lib *text.Library) error {
	if lib == nil || lib.Atlas() == nil {
		return nil // no font: text is skipped, the app still runs
	}
	atlas := lib.Atlas()
	t := &textRenderer{atlasW: atlas.W, atlasH: atlas.H, atlasDirty: true, capacity: 1024}

	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "glyph-atlas",
		Size:          wgpu.Extent3D{Width: uint32(atlas.W), Height: uint32(atlas.H), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        gputypes.TextureFormatR8Unorm,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("atlas texture: %w", err)
	}
	t.texture = tex
	t.view, err = g.device.CreateTextureView(tex, nil)
	if err != nil {
		return fmt.Errorf("atlas view: %w", err)
	}
	t.sampler, err = g.device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        "atlas-sampler",
		AddressModeU: gputypes.AddressModeClampToEdge,
		AddressModeV: gputypes.AddressModeClampToEdge,
		AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
	})
	if err != nil {
		return fmt.Errorf("atlas sampler: %w", err)
	}

	shader, err := g.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "text", WGSL: textWGSL})
	if err != nil {
		return fmt.Errorf("text shader: %w", err)
	}
	defer shader.Release()

	layout, err := g.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "text-layout",
		Entries: []gputypes.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gputypes.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}},
			{Binding: 1, Visibility: gputypes.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{
				SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D, Multisampled: false}},
			{Binding: 2, Visibility: gputypes.ShaderStageFragment, Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}},
			{Binding: 3, Visibility: gputypes.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}},
		},
	})
	if err != nil {
		return fmt.Errorf("text bind group layout: %w", err)
	}
	// The layout is retained for the renderer's lifetime: the glyph buffer's
	// bind group is rebuilt whenever the buffer grows.
	g.textLayout = layout

	t.storage, err = g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "glyphs",
		Usage: gputypes.BufferUsageStorage | gputypes.BufferUsageCopyDst,
		Size:  uint64(t.capacity) * uint64(unsafe.Sizeof(glyphInstance{})),
	})
	if err != nil {
		return fmt.Errorf("glyph buffer: %w", err)
	}

	if err := t.rebuildBindGroup(g, layout); err != nil {
		return err
	}

	pl, err := g.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "text-pipeline-layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{layout},
	})
	if err != nil {
		return fmt.Errorf("text pipeline layout: %w", err)
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
	t.pipeline, err = g.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "text",
		Layout: pl,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []gputypes.VertexBufferLayout{{
				ArrayStride: uint64(unsafe.Sizeof(glyphInstance{})),
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
		return fmt.Errorf("text pipeline: %w", err)
	}
	t.gpu = g
	g.text = t
	g.textLib = lib
	return nil
}

func (t *textRenderer) rebuildBindGroup(g *GPU, layout *wgpu.BindGroupLayout) error {
	bg, err := g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "text-bindings",
		Layout: layout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: g.uniform, Size: 16},
			{Binding: 1, TextureView: t.view},
			{Binding: 2, Sampler: t.sampler},
			{Binding: 3, Buffer: t.storage, Size: uint64(t.capacity) * uint64(unsafe.Sizeof(glyphInstance{}))},
		},
	})
	if err != nil {
		return fmt.Errorf("text bind group: %w", err)
	}
	if t.bindG != nil {
		t.bindG.Release()
	}
	t.bindG = bg
	return nil
}

// addOp converts one text op into glyph instances, rasterizing into the atlas
// on first use.
func (t *textRenderer) addOp(op paint.Op) {
	g := t.gpu
	if op.Text == "" {
		return
	}
	lib := g.textLib
	face := lib.Face(float64(op.FontSize))
	if face == nil {
		return
	}
	out := face.Shape(op.Text)
	penX := float64(op.TextX)
	// Alignment is applied by shifting the whole run once its width is known.
	runW := float64(out.Advance) / 64
	switch op.Align {
	case paint.AlignCenter:
		penX += (float64(op.Rect[2]) - runW) / 2
	case paint.AlignEnd:
		penX += float64(op.Rect[2]) - runW
	}
	penY := float64(op.TextY)
	col := rgba(op.Color)
	clip := clipRect(op.Clip)

	for _, gl := range out.Glyphs {
		slot, ok := lib.Atlas().Slot(face, uint16(gl.GlyphID), int(op.FontSize*4))
		if ok {
			x := penX + float64(slot.Offset.X)
			y := penY + float64(slot.Offset.Y)
			w := float64(slot.Rect.Dx())
			h := float64(slot.Rect.Dy())
			t.inst = append(t.inst, glyphInstance{
				Rect: [4]float32{float32(x), float32(y), float32(w), float32(h)},
				UV: [4]float32{
					float32(slot.Rect.Min.X) / float32(t.atlasW),
					float32(slot.Rect.Min.Y) / float32(t.atlasH),
					float32(slot.Rect.Max.X) / float32(t.atlasW),
					float32(slot.Rect.Max.Y) / float32(t.atlasH),
				},
				Color: col,
				Clip:  clip,
			})
		}
		penX += float64(gl.Advance) / 64
	}
	if !lib.Atlas().Dirty.Empty() {
		t.atlasDirty = true
		lib.Atlas().ClearDirty()
	}
	if len(t.inst) > t.capacity {
		t.grow(g, len(t.inst))
	}
}

func (t *textRenderer) grow(g *GPU, n int) {
	capacity := t.capacity * 2
	for capacity < n {
		capacity *= 2
	}
	buf, err := g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "glyphs",
		Usage: gputypes.BufferUsageStorage | gputypes.BufferUsageCopyDst,
		Size:  uint64(capacity) * uint64(unsafe.Sizeof(glyphInstance{})),
	})
	if err != nil {
		return
	}
	old := t.storage
	t.storage = buf
	t.capacity = capacity
	if err := t.rebuildBindGroup(g, g.textLayout); err != nil {
		t.storage = old
		buf.Release()
		return
	}
	old.Release()
}

// upload pushes the atlas if it changed since the last frame.
func (t *textRenderer) upload(g *GPU) error {
	if !t.atlasDirty {
		return nil
	}
	t.atlasDirty = false
	atlas := g.textLib.Atlas()
	return g.queue.WriteTexture(
		&wgpu.ImageCopyTexture{Texture: t.texture},
		atlas.Img.Pix,
		&wgpu.ImageDataLayout{Offset: 0, BytesPerRow: uint32(atlas.Img.Stride), RowsPerImage: uint32(t.atlasH)},
		&wgpu.Extent3D{Width: uint32(t.atlasW), Height: uint32(t.atlasH), DepthOrArrayLayers: 1},
	)
}

func (t *textRenderer) release() {
	if t.pipeline != nil {
		t.pipeline.Release()
	}
	if t.bindG != nil {
		t.bindG.Release()
	}
	if t.storage != nil {
		t.storage.Release()
	}
	if t.sampler != nil {
		t.sampler.Release()
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.texture != nil {
		t.texture.Release()
	}
}
