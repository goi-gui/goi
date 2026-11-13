//go:build !nogpu

package render

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"github.com/goi-gui/goi/internal/csscolor"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/text"
	"github.com/goi-gui/goi/internal/tree"
)

// One pipeline draws every solid shape. The fragment shader branches on the
// shape kind, which keeps a whole window at one draw call.
//
// Every texture sample is textureSampleLevel(…, 0.0) rather than textureSample:
// a kind branch is not uniform control flow, and only the *Level form is legal
// there. Native drivers accept both, Chrome rejects the plain one.
//
//	kind 0: rounded rect (per-corner radius, optional border)
//	kind 1: filled circle
//	kind 2: ring or arc (thickness in misc, sweep in params.zw)
//	kind 3: drop shadow (rounded rect with a soft falloff)
//	kind 4: glyph quad sampling the atlas (text and icons), optionally blurred
//	kind 5: bitmap
//	kind 6: inset shadow (a soft band inside a rounded rect)
//	kind 11: backdrop composite, sampling the scene texture by position
//	kind 12: one separable blur pass of that scene
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
	rot: vec4f,     // angle in radians, pivot x, pivot y, unused
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
@group(0) @binding(6) var<storage, read> gradients: array<vec4f>;
// segment is the first instance this pass draws. The backdrop splits a frame
// into several passes over one instance buffer, so each pass names its start.
@group(0) @binding(7) var<uniform> segment: vec4f;

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
	let inst = instances[u32(segment.x) + ii];
	let c = corners[vi];
	// A turned quad grows by a pixel on every side: the coverage fade at its
	// edge needs somewhere to land, and a fragment outside the quad never runs.
	// So every segment of a curve keeps its soft edge instead of a staircase.
	var pad = 0.0;
	if (inst.rot.x != 0.0) { pad = 1.0; }
	let grow = inst.rect.zw + vec2f(2.0 * pad);
	let origin = inst.rect.xy - vec2f(pad);
	let p = origin + c * grow;
	var out: VSOut;
	out.pos = vec4f(p.x / viewport.size.x * 2.0 - 1.0, 1.0 - p.y / viewport.size.y * 2.0, 0.0, 1.0);
	out.local = c * grow - vec2f(pad);
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
	// A rotated op turns its quad about the pivot in window space. Local
	// coordinates stay unrotated, so every distance field is evaluated in the
	// element's own frame.
	if (inst.rot.x != 0.0) {
		let ca = cos(inst.rot.x);
		let sa = sin(inst.rot.x);
		let d = p - inst.rot.yz;
		let q = vec2f(d.x * ca - d.y * sa, d.x * sa + d.y * ca);
		out.pos = vec4f((inst.rot.yz.x + q.x) / viewport.size.x * 2.0 - 1.0,
			1.0 - (inst.rot.yz.y + q.y) / viewport.size.y * 2.0, 0.0, 1.0);
		out.pos2 = inst.rot.yz + q;
	}
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

// radius_of is the corner radius a fragment sees, clamped to half the shape:
// CSS treats a border-radius of 999px on a 12px box as a circle, and without the
// clamp the distance field stays positive everywhere and the box vanishes.
fn radius_of(radius: vec4f, p: vec2f, s: vec2f) -> f32 {
	return clamp(corner_radius(radius, p, s), 0.0, min(s.x, s.y) * 0.5);
}

// Signed distance to a rounded rectangle centred on s/2.
fn sd_round_rect(p: vec2f, s: vec2f, r: f32) -> f32 {
	let h = s * 0.5;
	let q = abs(p - h) - (h - vec2f(r));
	return length(max(q, vec2f(0.0))) + min(max(q.x, q.y), 0.0) - r;
}

const PI = 3.14159265359;

// gradient_color samples a CSS gradient. Each record is 6 vec4: header
// (kind, angle, stop count, unused), the four stop positions, then four stop
// colours. t is the position along the gradient line, 0..1.
fn gradient_color(idx: f32, t: f32) -> vec4f {
	let base = u32(idx) * 6u;
	let count = u32(gradients[base].z);
	if (count == 0u) { return vec4f(0.0); }
	let p = gradients[base + 1u];
	let ps = array<f32, 4>(p.x, p.y, p.z, p.w);
	let cs = array<vec4f, 4>(
		gradients[base + 2u], gradients[base + 3u],
		gradients[base + 4u], gradients[base + 5u],
	);
	if (t <= ps[0]) { return cs[0]; }
	for (var i = 1u; i < count; i = i + 1u) {
		if (t <= ps[i]) {
			let span = max(ps[i] - ps[i - 1u], 0.0001);
			let f = clamp((t - ps[i - 1u]) / span, 0.0, 1.0);
			return mix(cs[i - 1u], cs[i], f);
		}
	}
	return cs[count - 1u];
}

// gradient_t is where a fragment falls on its gradient line.
fn gradient_t(head: vec4f, idx: f32, local: vec2f, size: vec2f) -> f32 {
	if (head.x > 2.5) {
		// 3 = radial: out from the centre to the farthest corner.
		let d = (local - size * 0.5) / max(length(size) * 0.5, 0.001);
		return length(d);
	}
	// 2 = linear, angle in radians measured from "to top", clockwise the way CSS
	// does, and the gradient line's length is the box projected on it.
	let a = head.y;
	let dir = vec2f(sin(a), -cos(a));
	let len = abs(size.x * sin(a)) + abs(size.y * cos(a));
	let c = local - size * 0.5;
	return dot(c, dir) / max(len, 0.001) + 0.5;
}

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

	if (kind > 11.5) {
		// One separable blur pass of the backdrop. params.y is the radius in
		// logical pixels and misc.x the axis: 1 horizontal, 2 vertical. The
		// two passes run before the panel samples the result, so the blur is
		// a quadratic kernel at the cost of two 9-tap draws.
		let radius = max(in.params.y, 0.001);
		let uv0 = in.pos2 / viewport.size.xy;
		var sum = vec4f(0.0);
		for (var i = -4; i <= 4; i = i + 1) {
			let o = f32(i) * 0.25 * radius;
			var uv = uv0;
			if (in.misc.x < 1.5) { uv.x = uv.x + o / viewport.size.x; }
			else { uv.y = uv.y + o / viewport.size.y; }
			sum = sum + textureSampleLevel(images, imgSamp, uv, 0.0);
		}
		return sum * 0.1111111;
	}

	if (kind > 10.5) {
		// Backdrop composite: the scene texture blurred by the passes above,
		// masked to the panel's rounded box. The element's own background then
		// paints over it, which is what makes a translucent background read as
		// tinted glass.
		let r = radius_of(in.radius, in.local, in.size);
		let d = sd_round_rect(in.local, in.size, r);
		let cov = 1.0 - smoothstep(-aa, aa, d);
		let c = textureSampleLevel(images, imgSamp, in.pos2 / viewport.size.xy, 0.0);
		if (cov <= 0.0 || c.a <= 0.0) { discard; }
		return vec4f(c.rgb, c.a * cov * in.color.a);
	}

	if (kind > 8.5) {
		// CSS gradient fill, linear or radial, with the shape's own rounded
		// corners and border drawn on top.
		let head = gradients[u32(in.params.y) * 6u];
		let g = gradient_color(in.params.y, gradient_t(head, in.params.y, in.local, in.size));
		let r = radius_of(in.radius, in.local, in.size);
		let d = sd_round_rect(in.local, in.size, r);
		let cov = 1.0 - smoothstep(-aa, aa, d);
		if (cov <= 0.0) { discard; }
		let bw = in.border.w;
		if (bw > 0.0) {
			let inner = 1.0 - smoothstep(-aa, aa, d + bw);
			let band = abs(d + bw * 0.5) - bw * 0.5;
			let fa = g.a * inner;
			let ba = in.border.a * (1.0 - smoothstep(-aa, aa, band));
			let a = fa + ba;
			if (a <= 0.0) { discard; }
			return vec4f((g.rgb * fa + in.border.rgb * ba) / a, a);
		}
		return vec4f(g.rgb, g.a * cov);
	}

	if (kind > 7.5) {
		// Edge gradient: opaque at the edge params.z names, transparent at the
		// far side. This is the scroll shadow, so it never needs a rounded box.
		var t = in.local.y / max(in.size.y, 0.001);
		let dir = in.params.z;
		if (dir > 0.5 && dir < 1.5) { t = 1.0 - t; }
		else if (dir > 1.5 && dir < 2.5) { t = in.local.x / max(in.size.x, 0.001); }
		else if (dir > 2.5) { t = 1.0 - in.local.x / max(in.size.x, 0.001); }
		let a = in.color.a * (1.0 - t);
		if (a <= 0.0) { discard; }
		let r = clamp(in.misc.x, 0.0, min(in.size.x, in.size.y) * 0.5);
		if (r > 0.0) {
			let d = sd_round_rect(in.local, in.size, r);
			let cov = 1.0 - smoothstep(-aa, aa, d);
			if (cov <= 0.0) { discard; }
			return vec4f(in.color.rgb, a * cov);
		}
		return vec4f(in.color.rgb, a);
	}

	if (kind > 6.5) {
		// Wavy text decoration: a sine stroke through the box. Thickness is the
		// stroke width and the box height is twice the amplitude.
		let t = max(in.misc.x, 0.5);
		let amp = max((in.size.y - t) * 0.5, 0.0);
		let wavelength = max(t * 4.0, 4.0);
		let cy = in.size.y * 0.5 + amp * sin(in.local.x / wavelength * 2.0 * PI);
		let d = abs(in.local.y - cy) - t * 0.5;
		let cov = 1.0 - smoothstep(-aa, aa, d);
		if (cov <= 0.0) { discard; }
		return vec4f(in.color.rgb, in.color.a * cov);
	}

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
		let hr = max(radius_of(in.radius, in.local, in.size) - spread, 0.0);
		let dh = sd_round_rect(in.local - holeOrigin, hole, hr);
		// Outside the hole is where the shadow is: dh >= 0, and the edge of the
		// hole is the darkest point before the fade inward.
		let cov = smoothstep(-blur, 0.0, dh);
		if (cov <= 0.0) { discard; }
		// Keep the band inside the shape itself.
		let dr = sd_round_rect(in.local, in.size, radius_of(in.radius, in.local, in.size));
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
		let c = textureSampleLevel(images, imgSamp, in.uv, 0.0);
		if (c.a <= 0.0) { discard; }
		return vec4f(c.rgb, c.a * cov);
	}

	if (kind > 3.5) {
		// Glyph: the atlas holds one coverage value per texel. A text shadow is
		// a separately blurred entry in the same atlas, so this stays one sample.
		let a = textureSampleLevel(atlas, samp, in.uv, 0.0).r;
		return vec4f(in.color.rgb, in.color.a * a);
	}

	if (kind > 2.5) {
		// Drop shadow: the quad is the shape grown by the blur radius, so the
		// SDF is evaluated against the original shape and fades to zero at the
		// quad edge. The shape itself is drawn on top of the solid middle.
		let blur = max(in.params.y, 0.001);
		let innerSize = in.size - vec2f(2.0 * blur);
		let p = in.local - vec2f(blur);
		let r = max(radius_of(in.radius, in.local, in.size) - blur, 0.0);
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
		var thick = 0.5;
		if (kind > 1.5) {
			// A ring: keep only the band of the given thickness.
			thick = max(in.misc.x, 0.5);
			d = abs(length(in.local - c) - (r - thick * 0.5)) - thick * 0.5;
		}
		var cov = 1.0 - smoothstep(-aa, aa, d);
		if (cov <= 0.0) { discard; }

		// Sweep mask, measured from 12 o'clock clockwise. A filled circle has no
		// sweep, so only rings and arcs pay for it.
		let v = in.local - c;
		var ang = atan2(v.x, -v.y);
		if (ang < 0.0) { ang = ang + 2.0 * PI; }
		let start = in.params.z;
		let end = in.params.w;
		if (kind > 1.5 && end - start < 2.0 * PI - 0.01) {
			// Where the fragment falls in the sweep. Measuring the offset from
			// the start rather than comparing absolute angles keeps a slice
			// that begins before 12 o'clock (a gauge's left shoulder) whole.
			let span = end - start;
			let rel = fract((ang - start) / (2.0 * PI)) * 2.0 * PI;
			// The sweep is a straight cut, so it needs its own fade: the arc
			// length to the nearest cut, over one physical pixel, is what keeps
			// a wedge's radial edge from reading as a staircase. A fragment past
			// the sweep has a negative distance and fades to nothing.
			let edge = min(rel, span - rel) * max(r - thick * 0.5, 1.0);
			let px = 1.0 / max(viewport.size.z, 1.0);
			cov = cov * smoothstep(0.0, px, edge);
			if (cov <= 0.0) { discard; }
		}
		return vec4f(in.color.rgb, in.color.a * cov);
	}

	// Rounded rectangle with an optional border.
	let r = radius_of(in.radius, in.local, in.size);
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
	Rot     [4]float32 // angle (radians), pivot x, pivot y
}

// GPU is the WebGPU renderer.
type GPU struct {
	instance *wgpu.Instance
	adapter  *wgpu.Adapter
	device   *wgpu.Device
	queue    *wgpu.Queue
	surface  *wgpu.Surface
	pipeline *wgpu.RenderPipeline
	// pipeline1 draws into a single-sample target. The blur passes of a blurred
	// backdrop render into offscreen textures that have no multisampling, so
	// they need a pipeline whose count matches.
	pipeline1 *wgpu.RenderPipeline
	// samples is the multisample count the surface target is drawn with; 1
	// turns it off. shapeLayout and shader are kept so a new count can rebuild
	// the pipelines without recreating the device.
	samples int
	shader  *wgpu.ShaderModule
	// msaa is the multisampled colour target a frame resolves from.
	msaa           *wgpu.Texture
	msaaView       *wgpu.TextureView
	msaaW          int
	msaaH          int
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

	text    *textRenderer
	textLib *text.Library

	// grads holds the CSS gradients the frame draws, one 6-vec4 record each;
	// gradSeen maps a gradient pointer to its index so two nodes with the same
	// value share a record.
	gradBuf   *wgpu.Buffer
	gradData  []float32
	gradSeen  map[*tree.Gradient]int
	gradCount int

	textLayout *wgpu.BindGroupLayout

	// backdrop holds the offscreen scene the backdrop passes render into. It is
	// nil until a design asks for a blur, so a frame without one never pays for
	// an extra render target.
	backdrop *backdropTarget
	// segZero is the "draw from instance 0" uniform a normal frame binds.
	segZero *wgpu.Buffer
	// shapeLayout is shared by the pipeline and every per-pass bind group.
	shapeLayout *wgpu.BindGroupLayout
	// blend and attrs are what a pipeline rebuild needs, kept so a multisample
	// count change does not have to recreate them.
	blend *gputypes.BlendState
	attrs []gputypes.VertexAttribute
	// passBinds are a blurred frame's per-pass bind groups, freed once the
	// command buffer that uses them is submitted.
	passBinds []*wgpu.BindGroup

	closed bool
}

// TargetKind names the native handle pair passed to NewGPU.
type TargetKind int

// Target kinds.
const (
	TargetWayland TargetKind = iota
	TargetX11
	TargetWin32
	TargetMetal
	// TargetWeb is a browser canvas: the page provides it, so both handles are
	// unused.
	TargetWeb
)

// NewGPU creates a renderer for an existing native window.
// display and window are the platform handles: X11 Display*/Window, Wayland
// wl_display*/wl_surface*, Win32 HINSTANCE/HWND, or a Metal layer in window
// with display unused. The kind must match, because the legacy CreateSurface
// sniffs the environment and picks the wrong backend when a Wayland session
// also has DISPLAY set.
func NewGPU(display, window uintptr, kind TargetKind) (*GPU, error) {
	inst, err := wgpu.CreateInstance(nil)
	if err != nil {
		return nil, fmt.Errorf("wgpu instance: %w", err)
	}
	g := &GPU{instance: inst, scale: 1, vsync: true, capacity: 256, samples: DefaultSamples}

	surface, err := createSurface(inst, display, window, kind)
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

// DefaultSamples is the multisample count a renderer starts with. Four is the
// count WebGPU guarantees for every renderable format, and it is enough to take
// the last stair-step off a curve that the signed-distance fade rounds.
const DefaultSamples = 4

// samplesTryOrder is the order a requested count falls back through: the ask
// itself, then four, then off. A device that refuses 8 still draws at 4 rather
// than failing the frame.
func samplesTryOrder(n int) []int {
	out := []int{n}
	if n > 4 {
		out = append(out, 4)
	}
	if n > 1 {
		out = append(out, 1)
	}
	return out
}

// Samples reports the multisample count the renderer is drawing with.
func (g *GPU) Samples() int { return g.samples }

// SetSamples chooses the multisample count: 1, 2, 4 or 8. A device that cannot
// draw the surface at that count falls back to 4 and then to 1, so the call
// reports what it settled on in Samples rather than failing the app.
func (g *GPU) SetSamples(n int) error {
	if g.closed {
		return nil
	}
	if n != 1 && n != 2 && n != 4 && n != 8 {
		return fmt.Errorf("render: %d samples is not 1, 2, 4 or 8", n)
	}
	if n == g.samples && g.pipeline != nil {
		return nil
	}
	prev := g.samples
	for _, want := range samplesTryOrder(n) {
		if err := g.probeSamples(want); err != nil {
			continue
		}
		g.samples = want
		if err := g.rebuildPipelines(); err != nil {
			g.samples = prev
			return err
		}
		g.releaseMSAA()
		if g.backdrop != nil {
			g.backdrop.release()
			g.backdrop = nil
		}
		return nil
	}
	g.samples = prev
	if prev != 1 {
		g.samples = 1
		if err := g.rebuildPipelines(); err != nil {
			return err
		}
	}
	return nil
}

// probeSamples asks the device for a texture at that count without keeping it,
// which is how an unsupported count is found before the pipelines are rebuilt.
func (g *GPU) probeSamples(n int) error {
	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "msaa-probe",
		Size:          wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   uint32(n),
		Dimension:     gputypes.TextureDimension2D,
		Format:        g.format,
		Usage:         gputypes.TextureUsageRenderAttachment,
	})
	if err != nil {
		return err
	}
	tex.Release()
	return nil
}

// releaseMSAA drops the multisampled target, so the next frame builds one for
// the count and size it needs.
func (g *GPU) releaseMSAA() {
	if g.msaaView != nil {
		g.msaaView.Release()
		g.msaaView = nil
	}
	if g.msaa != nil {
		g.msaa.Release()
		g.msaa = nil
	}
	g.msaaW, g.msaaH = 0, 0
}

// ensureMSAA returns the multisampled colour target for this frame, creating it
// on the first frame and after a resize.
func (g *GPU) ensureMSAA(w, h int) (*wgpu.TextureView, error) {
	if g.samples <= 1 {
		return nil, nil
	}
	if g.msaaView != nil && g.msaaW == w && g.msaaH == h {
		return g.msaaView, nil
	}
	g.releaseMSAA()
	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "msaa-colour",
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   uint32(g.samples),
		Dimension:     gputypes.TextureDimension2D,
		Format:        g.format,
		Usage:         gputypes.TextureUsageRenderAttachment,
	})
	if err != nil {
		// The count the device accepted for a probe can still be refused at
		// this size; drawing without multisampling beats not drawing.
		g.samples = 1
		if rerr := g.rebuildPipelines(); rerr != nil {
			return nil, rerr
		}
		return nil, nil
	}
	view, err := g.device.CreateTextureView(tex, nil)
	if err != nil {
		tex.Release()
		return nil, err
	}
	g.msaa, g.msaaView, g.msaaW, g.msaaH = tex, view, w, h
	return view, nil
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
	// The module outlives the first pipeline: a multisample count change builds
	// both pipelines again from it.
	g.shader = shader

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
		// The shader reads it as storage and the pass binds it as a vertex
		// buffer, and a browser validates that the usage covers both.
		Usage: gputypes.BufferUsageStorage | gputypes.BufferUsageCopyDst | gputypes.BufferUsageVertex,
		Size:  g.storageSize(g.capacity),
	})
	if err != nil {
		return fmt.Errorf("storage buffer: %w", err)
	}
	g.segZero, err = g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "segment-zero",
		Usage: gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst,
		Size:  segUniformSize,
	})
	if err != nil {
		return fmt.Errorf("segment buffer: %w", err)
	}
	g.gradData = make([]float32, maxGradients*gradRecordFloats)
	g.gradBuf, err = g.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "gradients",
		Usage: gputypes.BufferUsageStorage | gputypes.BufferUsageCopyDst,
		Size:  gradBufferSize(),
	})
	if err != nil {
		return fmt.Errorf("gradient buffer: %w", err)
	}

	g.shapeLayout, err = g.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "shapes-layout",
		Entries: shapeLayoutEntries(),
	})
	if err != nil {
		return fmt.Errorf("bind group layout: %w", err)
	}
	if err := g.rebind(); err != nil {
		return fmt.Errorf("bind group: %w", err)
	}

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
	g.blend = blend
	g.attrs = attrs
	return g.rebuildPipelines()
}

// rebuildPipelines builds the multisampled pipeline and the single-sample one
// the offscreen blur passes need. Both come from one shader and one layout, so
// a shape looks the same whichever target it lands in.
func (g *GPU) rebuildPipelines() error {
	if g.shader == nil || g.shapeLayout == nil {
		return nil
	}
	pl, err := g.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "shapes-pipeline-layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{g.shapeLayout},
	})
	if err != nil {
		return fmt.Errorf("pipeline layout: %w", err)
	}
	defer pl.Release()

	build := func(count int) (*wgpu.RenderPipeline, error) {
		return g.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
			Label:  "shapes",
			Layout: pl,
			Vertex: wgpu.VertexState{
				Module:     g.shader,
				EntryPoint: "vs_main",
				Buffers: []gputypes.VertexBufferLayout{{
					ArrayStride: uint64(unsafe.Sizeof(gpuInstance{})),
					StepMode:    gputypes.VertexStepModeInstance,
					Attributes:  g.attrs,
				}},
			},
			Fragment: &wgpu.FragmentState{
				Module:     g.shader,
				EntryPoint: "fs_main",
				Targets:    []gputypes.ColorTargetState{{Format: g.format, Blend: g.blend, WriteMask: gputypes.ColorWriteMaskAll}},
			},
			Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, FrontFace: gputypes.FrontFaceCCW, CullMode: gputypes.CullModeNone},
			Multisample: gputypes.MultisampleState{Count: uint32(count), Mask: 0xFFFFFFFF},
		})
	}
	multi, err := build(g.samples)
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}
	single, err := build(1)
	if err != nil {
		multi.Release()
		return fmt.Errorf("pipeline: %w", err)
	}
	if g.pipeline != nil {
		g.pipeline.Release()
	}
	if g.pipeline1 != nil {
		g.pipeline1.Release()
	}
	g.pipeline, g.pipeline1 = multi, single
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
		{
			Binding: 6, Visibility: gputypes.ShaderStageFragment,
			Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage},
		},
		{
			Binding: 7, Visibility: gputypes.ShaderStageVertex,
			Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform},
		},
	}
}

func (g *GPU) storageSize(n int) uint64 {
	return uint64(n) * uint64(unsafe.Sizeof(gpuInstance{}))
}

// maxGradients bounds the gradient record buffer. A frame with more distinct
// gradients than this reuses the last slot, which is a design nobody ships.
const maxGradients = 512

// gradRecordFloats is one gradient record: a header, four positions and four
// colours, all as vec4.
const gradRecordFloats = 24

func gradBufferSize() uint64 { return maxGradients * gradRecordFloats * 4 }

// gradIndex queues a gradient for this frame and returns its record index.
func (g *GPU) gradIndex(gr *tree.Gradient) int {
	if g.gradSeen == nil {
		g.gradSeen = map[*tree.Gradient]int{}
	}
	if i, ok := g.gradSeen[gr]; ok {
		return i
	}
	if g.gradCount >= maxGradients {
		return maxGradients - 1
	}
	i := g.gradCount
	g.gradCount++
	g.gradSeen[gr] = i
	// Four stops: a longer gradient is resampled, which is invisible at this
	// size and keeps the shader free of loops over a dynamic count.
	stops := resample(gr.Stops, 4)
	head := float32(2) // a CSS linear gradient
	if gr.Kind == csscolor.Radial {
		head = 3
	}
	base := i * gradRecordFloats
	rec := make([]float32, gradRecordFloats)
	rec[0] = float32(head)
	rec[1] = float32(gr.Angle * 3.141592653589793 / 180)
	rec[2] = float32(len(stops))
	for k := 0; k < 4; k++ {
		rec[4+k] = float32(stops[k].At)
	}
	for k := 0; k < 4; k++ {
		c := stops[k].Color
		o := 8 + k*4
		rec[o] = float32((c>>24)&0xFF) / 255
		rec[o+1] = float32((c>>16)&0xFF) / 255
		rec[o+2] = float32((c>>8)&0xFF) / 255
		rec[o+3] = float32(c&0xFF) / 255
	}
	if cap(g.gradData) < maxGradients*gradRecordFloats {
		g.gradData = make([]float32, maxGradients*gradRecordFloats)
	}
	copy(g.gradData[base:], rec)
	return i
}

// resample turns any stop list into exactly n stops, evenly in index, keeping
// the first and last positions exact.
func resample(stops []tree.GradientStop, n int) []tree.GradientStop {
	out := make([]tree.GradientStop, n)
	if len(stops) == 0 {
		return out
	}
	for k := 0; k < n; k++ {
		t := float64(k) / float64(n-1)
		out[k] = sampleStops(stops, t)
	}
	out[0] = stops[0]
	out[n-1] = stops[len(stops)-1]
	return out
}

// sampleStops blends a stop list at position t.
func sampleStops(stops []tree.GradientStop, t float64) tree.GradientStop {
	if t <= stops[0].At {
		return stops[0]
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].At {
			span := stops[i].At - stops[i-1].At
			f := 0.0
			if span > 0 {
				f = (t - stops[i-1].At) / span
			}
			return tree.GradientStop{At: t, Color: mixColor(stops[i-1].Color, stops[i].Color, f)}
		}
	}
	return stops[len(stops)-1]
}

func mixColor(a, b uint32, f float64) uint32 {
	ch := func(shift uint) uint32 {
		x := float64((a >> shift) & 0xFF)
		y := float64((b >> shift) & 0xFF)
		return uint32(x + (y-x)*f + 0.5)
	}
	return ch(24)<<24 | ch(16)<<16 | ch(8)<<8 | ch(0)
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
	g.gradCount = 0
	if g.gradSeen == nil {
		g.gradSeen = map[*tree.Gradient]int{}
	} else {
		clear(g.gradSeen)
	}
	// Reserve every glyph this frame needs before emitting, so an atlas
	// compaction can only drop entries the frame does not reference.
	if g.text != nil {
		g.text.prepare(list)
	}
	if g.images != nil {
		g.images.prepare(list)
	}
	var panels []backdropPanel
	for i, op := range list.Ops {
		switch op.Kind {
		case paint.Backdrop:
			panel := backdropPanel{
				at:         len(g.instances),
				rect:       op.Rect,
				radius:     op.Radius,
				clip:       op.Clip,
				blur:       op.Blur,
				alpha:      float32(op.Color&0xFF) / 255,
				clipRadius: op.ClipRadius,
				clipBox:    op.ClipBox,
				rot:        op.Rotate,
				pivotX:     op.PivotX,
				pivotY:     op.PivotY,
			}
			panels = append(panels, panel)
			g.instances = append(g.instances, backdropInstances(panel, float32(width)/g.scale, float32(height)/g.scale)...)
		case paint.Fill:
			rect := instanceRect(op, g.scale)
			params := [4]float32{shapeKind(op.Shape), 0, op.StartAngle, op.EndAngle}
			if op.Gradient != nil {
				// A CSS gradient: kind 9 linear, 10 radial, params.y its record.
				kind := float32(9)
				if op.Gradient.Kind == csscolor.Radial {
					kind = 10
				}
				params = [4]float32{kind, float32(g.gradIndex(op.Gradient)), 0, 0}
			}
			g.instances = append(g.instances, gpuInstance{
				Rect:    rect,
				Radius:  op.Radius,
				Color:   rgba(op.Color),
				Border:  borderVec(op.BorderColor, op.BorderWidth),
				Clip:    clipVec(op.Clip),
				ClipBox: clipVec(op.ClipBox),
				Params:  params,
				Misc:    [4]float32{op.Thickness, 0, op.ClipRadius, 0},
				Rot:     [4]float32{op.Rotate, op.PivotX, op.PivotY, 0},
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
				Rot:     [4]float32{op.Rotate, op.PivotX, op.PivotY, 0},
			})
		case paint.Text:
			if g.text != nil {
				g.text.emit(i)
			}
		case paint.Bitmap:
			g.images.addOp(op)
		}
	}
	if len(panels) > 0 {
		// The last instance copies the finished scene onto the surface, so a
		// frame with a backdrop renders through the offscreen texture only.
		lw, lh := float32(width)/g.scale, float32(height)/g.scale
		g.instances = append(g.instances, gpuInstance{
			Rect:   [4]float32{0, 0, lw, lh},
			Color:  [4]float32{1, 1, 1, 1},
			Clip:   clipVec([4]float32{0, 0, lw, lh}),
			Params: [4]float32{11, 0, 0, 0},
		})
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
	if g.gradCount > 0 {
		n := g.gradCount * gradRecordFloats
		if err := g.queue.WriteBuffer(g.gradBuf, 0, floatsToBytes(g.gradData[:n])); err != nil {
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

	if len(panels) > 0 {
		return g.renderBackdrop(list, width, height, panels)
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
	// A multisampled frame draws into its own colour target and resolves into
	// the swapchain image at the end of the pass, which is where the samples
	// become the smooth edge the window shows.
	msaa, err := g.ensureMSAA(width, height)
	if err != nil {
		return err
	}
	att := wgpu.RenderPassColorAttachment{
		View:       view,
		LoadOp:     gputypes.LoadOpClear,
		StoreOp:    gputypes.StoreOpStore,
		ClearValue: clearColor(list),
	}
	if msaa != nil {
		att.View, att.ResolveTarget, att.StoreOp = msaa, view, gputypes.StoreOpDiscard
	}
	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{att},
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
		Usage: gputypes.BufferUsageStorage | gputypes.BufferUsageCopyDst | gputypes.BufferUsageVertex,
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
	bg, err := g.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "shapes-bindings",
		Layout: g.shapeLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: g.uniform, Size: 16},
			{Binding: 1, Buffer: g.storage, Size: g.storageSize(g.capacity)},
			{Binding: 2, TextureView: g.textView()},
			{Binding: 3, Sampler: g.textSampler()},
			{Binding: 4, TextureView: g.imageView()},
			{Binding: 5, Sampler: g.imageSampler()},
			{Binding: 6, Buffer: g.gradBuf, Size: gradBufferSize()},
			{Binding: 7, Buffer: g.segZero, Size: segUniformSize},
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
	if g.images != nil {
		g.images.release()
	}
	if g.textLayout != nil {
		g.textLayout.Release()
	}
	if g.backdrop != nil {
		g.backdrop.release()
		g.backdrop = nil
	}
	if g.pipeline != nil {
		g.pipeline.Release()
	}
	if g.pipeline1 != nil {
		g.pipeline1.Release()
	}
	g.releaseMSAA()
	if g.shader != nil {
		g.shader.Release()
	}
	if g.bindG != nil {
		g.bindG.Release()
	}
	if g.storage != nil {
		g.storage.Release()
	}
	if g.gradBuf != nil {
		g.gradBuf.Release()
	}
	for _, bg := range g.passBinds {
		bg.Release()
	}
	if g.segZero != nil {
		g.segZero.Release()
	}
	if g.shapeLayout != nil {
		g.shapeLayout.Release()
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
// instanceRect is the pixel rectangle a fill instance draws in. Only an upright
// shape snaps to the pixel grid: snapping is what keeps a border crisp, and a
// curve's joins live at subpixels, so snapping each small step of a spline
// turns the curve into a staircase.
func instanceRect(op paint.Op, scale float32) [4]float32 {
	if op.Rotate != 0 {
		return op.Rect
	}
	return snapRect(op.Rect, scale)
}

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
	case paint.Wave:
		return 7
	case paint.Gradient:
		return 8
	case paint.CSSGradient:
		return 9
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
