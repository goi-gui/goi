# Decisions

One entry per phase: what was decided, what else was considered, why.

## Phase 0 — tech freeze

**WebGPU bindings: `github.com/gogpu/wgpu` v0.34.5 (pure Go).**

Evaluated:
- `cogentcore/webgpu` — unmaintained, its README points elsewhere. Rejected.
- `go-webgpu/webgpu` — works, but crashes: `wgpu-native` panics with
  `invalid callback` in `wgpuInstanceRequestAdapter`. The callback-passing path
  (goffi callbacks reconstructed from the wrong ABI) is broken in v0.5.5 +
  goffi v0.6.7 on linux/amd64. Reproduced with `RequestAdapter(nil)` alone, so
  it is not our surface code.
- `gogpu/wgpu` — same author's successor: pure Go HAL (Vulkan/Metal/DX12/GLES)
  with WebGPU-shaped API, no `wgpu-native` .so needed, no callback FFI.
  Instances, adapters and device creation are direct calls. Chosen.

Consequence: `wgpu.Init()` does not exist; `CreateInstance(nil)` is enough.

**Linking: `-ldflags=-linkmode=internal` on Linux when any cgo package is
present.** GLFW is cgo, which forces the external linker by default, and the
external linker rejects goffi's `//go:cgo_import_dynamic` relocations
(`unhandled relocation ... SDYNIMPORT`, upstream goffi#72). Internal linking is
the documented workaround. Keep it in the example build commands and later in
`make` targets; the framework itself must not require it once the GLFW package
is separated. Also `CGO_ENABLED=0` + `-tags nofakecgo` is the pure-Go path if it
is ever needed.

**Surface formats: pick a non-sRGB unorm format** (`BGRA8Unorm`, else
`RGBA8Unorm`). sRGB formats darken/mute authored hex colors unless the shader
converts, and a UI toolkit wants raw literal fidelity. Verified visually at 1x
and simulated 2x.

**Surface creation:** Linux uses `CreateSurfaceUnsafe` with an explicit
`SurfaceTargetFromWaylandSurface` / `SurfaceTargetFromXlibWindow` built from
GLFW's native handles. Wayland is the default build; X11 is `-tags x11`. Do not
rely on `CreateSurface`'s environment sniffing — it guesses from `XDG_SESSION_TYPE`.

**HiDPI:** framebuffer size drives the surface, `GetContentScale` compensates.
The whole layout is in logical pixels; the viewport uniform carries the scale so
the fragment shader can widen AA by `0.5/scale`.

**Rendering approach:** instanced quads (6 vertices, one instance per rect) with
an SDF fragment shader doing per-corner radii, borders and AA. One draw call per
batch. This is what the display list will feed.

**Known issues / unverified:**
- darwin/arm64 and windows/amd64 were not built here (no machine). The pure-Go
  HAL supports them per its CI, but treat as unverified until someone builds.
- Wayland surface must be created *after* the window is shown; creating it on a
  hidden window yields a nil `wl_surface` under GLFW.
- `wgpu-native`-style `SurfaceTexture.Release` does not exist; the surface owns
  it (nothing to release per frame).

## Design decisions locked before Phase 1

- **Single source of truth for validation output:** semantic validation returns
  diagnostics only. There is no second "validated AST" type; instantiation reads
  the syntax AST and asks the schema for default values.
- **Hot reload keeps app state in the app, not the framework.** `Bind` re-runs
  after every reload, so app state must be declared outside it. The framework
  only carries widget-internal state (focus, TextInput value/caret, scroll
  offset) and Go-set property values whose `.ui` literal did not change.
- **Widgets in v1:** Text, Button, TextInput. Checkbox/Scroll/Image deferred.
- **`opacity` deferred** — it needs offscreen layers to be correct.
- **Numbers are `float64`**, colors are `uint32` (0xRRGGBBAA).
- **`visible: false`** removes the node from layout and hit-testing entirely.
