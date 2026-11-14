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
  offset) and Go-set property values whose `.goi` literal did not change.
- **Widgets in v1:** Text, Button, TextInput. `Image` is still deferred.
  (Superseded: the whole component set and `Image` exist now; this records what
  the first milestone shipped.)
- **`opacity` deferred** — it needs offscreen layers to be correct.
- **Numbers are `float64`**, colors are `uint32` (0xRRGGBBAA).
- **`visible: false`** removes the node from layout and hit-testing entirely.

## Phase 1–3 — parser, schema, tree

- **No newline tokens.** The parser distinguishes `name: value` from
  `Name { ... }` with two-token lookahead. Fewer token kinds, and one-line
  elements work.
- **Validation returns diagnostics only**, plus a map of element → id. No
  "validated AST": instantiation reads the syntax AST and asks the schema for
  defaults, so there is one model, not two.
- **`Prop[T comparable]`** so `Set` can skip no-ops; numbers are all `float64`,
  colors are `uint32` (0xRRGGBBAA).
- **Per-node concrete fields, not a `map[string]any`.** A `[]NamedProp` table
  (name, dirty kind, boxed getter, literal setter, CopyFrom) is built once and
  only when something asks for it — that is what hot reload and the builder use.
- **`ui.Window` is a handle, not a node**; the root node is `ui.WindowNode`. The
  handle is what `Bind` receives, so `ByID` takes the handle and the node type is
  a separate generic parameter.
- **`Container` is shared** by Column/Row/Stack, with thin named types so
  `ByID[*ui.Column]` is type-checked.

## Phase 4 — layout

- **Own flexbox-lite.** No cgo Yoga, no dependency. Padding and border are
  applied once per node in `children()`, which fixed a class of double-inset bugs.
- **Auto-sized containers measure their children**; only `Window` fills its
  parent. Everything else with no declared size is "hug content".
- **A child of the root fills the client area** when it declares no size, which
  is what makes `flex: 1` on a top-level Column behave as expected.
- Layout is pure and takes a `Measurer`; tests use `FakeMeasurer`, the app passes
  a real one backed by the text library.

## Phase 5–8 — paint, render, text

- **One rectangle pipeline, one text pipeline, one draw call each.** Fills are
  instanced quads with an SDF fragment shader (per-corner radius, border, AA
  widened by `0.5/scale`). Text is textured quads over one `R8Unorm` atlas.
- **Colors are converted to 0..1 floats at upload time.** (A first cut passed
  0..255 floats and every fill rendered white — worth remembering.)
- **Non-sRGB surface format**, so authored hex colors are exact.
- **Grayscale atlas, not SDF.** Rasterizer is a small scanline fill with 4x
  vertical supersampling.
- **The glyph atlas never drops a glyph.** It starts at 2048². When the shelves
  run out it compacts to the current frame's working set (a per-frame stamp
  decides what is live), and if that set alone does not fit it grows, up to
  4096², telling the renderer to recreate the texture and rebuild its bind
  group. A frame therefore runs in two passes: `prepare` shapes every text op
  and resolves slots, then `emit` appends the quads in display-list order. That
  ordering is what makes a rebuild safe — nothing already emitted can point at a
  moved glyph. The image atlas follows the same rule (2048², grows to 4096²,
  downscale only at the cap).
- **Clipping is recorded per op and applied in the shader.** Each instance
  carries its clip rectangle and the fragment shader discards outside it (see
  the later note that replaced a batched `SetScissorRect` attempt).
- **Text is measured by the real shaper** in the app and by `FakeMeasurer` in
  layout tests, so layout tests stay headless and font-free.

## Phase 6–7 — input and dirty rendering

- The loop is: poll, frame, then block in `WaitEvents` only when nothing is
  pending. That is what keeps an idle window at ~0% CPU (measured 0 frames while
  idle).
- Hit-testing walks children last-first with the inherited clip rect, and press
  and release must land on the same node.
- Layout dirt propagates to ancestors; a layout-dirty frame repaints everything.

## Phase 9 — widgets

- **Text, Button, TextInput only in v1.** Checkbox, Scroll and Image are
  deferred; the schema and paint layer are ready for them.
- `TextInput` caret arithmetic is byte-based on rune boundaries, so Turkish
  characters do not split and shift the caret correctly.
- Text editing (insert, backspace, delete, arrows, home/end, Enter) is
  implemented; selection, clipboard and IME commits were added later (see the
  text-field notes near the end of this file).
- `hoverBackground` is a property; the input layer only sets the hover flag.

## Phase 10 — hot reload

- The watcher watches the **directory**, not the file, so editors that save by
  rename keep working. 80ms debounce.
- Parsing and validation run on the watcher goroutine; only the swap is posted.
- On errors the last good tree stays and `App.OnError` (or stderr) gets a
  `file:line:col` diagnostic.
- **`Bind` runs again after every reload**; app state lives outside it. The
  framework carries by id: focus, `TextInput` text and caret, and every property
  Go wrote whose `.goi` literal did not change. This is covered by
  `TestCounterSurvivesReload`.

## Phase 11 — docs and CI

- CI is a native build matrix (ubuntu/macos/windows), not a cross-compile check:
  GLFW is cgo, so "compiles for" is not "works on".
- Known unverified: no macOS or Windows machine was available while building
  this, so those two are exercised only by CI.

## Phase 11 — post-review fixes

- **Clipping moved into the fragment shader, not the scissor.** The first attempt
  batched ops by clip and used `SetScissorRect` with `FirstInstance`, which
  requires the `IndirectFirstInstance` feature and failed validation on Vulkan.
  Each instance now carries its clip rectangle and the shader discards fragments
  outside it: one draw call per pipeline, no per-run state, same visual result.
  A WGSL `any(vec < vec)` comparison also failed in naga's SPIR-V backend, so the
  test is written as four scalar comparisons. Both are worth remembering.
- **Colors are uploaded as 0..1 floats.** An earlier version passed 0..255 and
  every fill drew white; `rgba()` divides by 255 in one place.

## Phase 12 — Material 3 components

- **Components are core elements, not a package.** The 40 component kinds are
  built-in .goi elements (`Button`, `Chip`, `Dialog`, …) so a file needs no
  imports and `ByID` stays type-checked (`*ui.Button`). `internal/componentdef`
  is the single list of kinds and their property subsets; `schema` validates
  against it and `tree` builds prop tables from it, so they cannot drift.
  `internal/schema/components_test.go` writes every kind into a file and fails if
  one does not validate.
- **One shared `Component` state, per-kind props.** All components embed
  `tree.Component` (label, icon, variant, checked, value, events, interaction
  state). Each kind exposes only its own subset through `propsFor`, keeping the
  "unknown property" diagnostics honest.
- **Bundled Roboto 400/500/700 and Material Symbols Outlined, not system fonts.**
  M3's metrics and icon names depend on them. Both are fetched from the Google
  Fonts CDN as `woff` (which go-text parses) and embedded; Roboto covers all
  Turkish glyphs, Material Symbols is looked up by ligature (`home`, `add`, …),
  so no icon-name table is needed. Atlas keys now include the font tag, because
  three Roboto weights and the icon font share one atlas.
- **Theme tokens live in `internal/m3`.** Baseline light and dark schemes, the
  full type scale, the shape scale and elevation constants. Every painter reads
  those, so a window's `theme: dark` swaps the whole scheme. Custom/dynamic
  palettes are not exposed.
- **Display list grew `Shadow`, `Circle` and `Arc`.** The shader kind now
  distinguishes filled circle, ring/arc and shadow, instead of treating every
  non-rect as a ring. Shadows grow the quad by the blur radius and evaluate the
  SDF against the original shape, so the falloff reaches zero at the quad edge
  and the shape drawn on top hides the solid middle.
- **Container components reuse the Row/Column layout.** `ButtonGroup`, `Toolbar`,
  `Carousel`, `Tabs`, `NavigationBar` run their children horizontally;
  `NavigationRail` and `NavigationDrawer` vertically; `Tabs` and `NavigationBar`
  split the width equally. This kept the layout engine at one algorithm instead
  of a second widget layout system. `internal/layout` decides horizontal flow
  from the kind, and intrinsic size sums children for a row and maximises for a
  column.
- **Text gets an M3 role.** `Text.role` selects a type-scale step; `fontSize` and
  `weight` still override it, so existing files keep working. Buttons and other
  components measure their labels with the same style they paint.
- **Vertical centering uses font metrics, not a guessed baseline.** A line box is
  centred when its baseline sits at `box centre + (ascent - descent) / 2`; for
  the bundled fonts that is 0.342em for Roboto and 0.5em for Material Symbols.
  Every label and icon goes through `textIn`/`iconIn`, so the same rule applies
  to buttons, fields, list items, tabs and pickers. Icons are glyphs of an icon
  font, so their ink lands in the same centred line box as text.
- **A navigation destination's arrangement comes from its parent kind, not its
  width.** Bar and rail items stack the icon over the label; drawer items put
  them in a row. Width was a proxy that broke as soon as a bar was wide enough
  for its equal-width items to look like a drawer.
- **A border is its own anti-aliased band.** The fill and the border are
  disjoint coverages blended in premultiplied space and converted back to
  straight alpha, so the shared edge is not darkened twice. The band uses its
  own signed distance (`abs(d + bw/2) - bw/2`) rather than being the leftover of
  the fill coverage: with the leftover, a 1dp border loses half its coverage
  along curves and turns into a dotted line at some radii.
- **Shape edges snap to whole device pixels in the renderer.** A 1dp border on
  a box at `y = 113.2` covers two rows at 90% and 10%, which reads as a broken,
  ragged line, and a 1dp divider turns into a two-row grey smear. `snapRect`
  rounds both edges to the device grid (half a logical pixel at 2x), so a border,
  divider or outline lands on whole rows. Neighbouring boxes snap from their
  absolute edges, so they stay flush.
- **Text rasterizes at device resolution and each glyph snaps to that grid.**
  Covering a fractional box centres a run at a fractional baseline; the linear
  sampler then smears every stem over two columns, and small or light text goes
  grey (measured: darkest pixel 73 instead of 28 on the same label). The face is
  created at `size * scale`, and the pen origin, each glyph's x and the baseline
  are rounded in device pixels, so a glyph covers exactly the texels the atlas
  holds. Advance widths keep the shaper's spacing; each glyph moves by at most
  half a device pixel.
- **Interaction stays in the input layer.** Hover/pressed/focused are plain
  fields on `Component`; painters draw the state layer (8/10/10% of the
  foreground), and `disabled` applies M3's 38%/12% treatment.
- **Scrolling is a common `scroll: true` property, not a Scroll element.** A
  `Row` scrolls horizontally and every other element vertically, so the axis
  follows the container's flow instead of a second `axis` property; the offset
  lives on `NodeBase` (`SetScrollOffset`/`ScrollOffset`) and paint/hit-testing
  shift children by the same amount. The content clips to the content box,
  which is what makes the container look scrollable. The layout does not grow a
  scroll container: it still asks for a bounded main-axis size (`flex: 1` or
  `height`), like Flutter, and a flex child now gives space back when its
  content overflows, so `flex: 1` bounds the page instead of growing to fit it. `ScrollOffset` clamps on read rather than on write,
  so an offset set before a relayout comes back when the content grows again.
  Wheel and drag both scroll the nearest scrollable ancestor of the pointer;
  a drag past 4dp cancels the click it started on.
- **Ripples are driven by the frame clock, not by the painter.** `startRipple`
  records the press point, `tick(dt)` advances a 0.45s life (expand to 60%,
  then fade) on the UI goroutine, and the state layer maps the progress to a
  disc. The disc is clipped to the component's own box, not only to its
  ancestors (the page), and carries `ClipRadius` so it is rounded to the pill or
  card it sits in; without either, the disc spills past the button while it
  expands or shows a square edge at the corners.
  The clock is wall time clamped to 0.1s per frame, so an idle app does not
  burn frames and a stall does not skip the animation.
- **Text selection is a pair of byte offsets on `Component`.** `Caret` is the
  moving end and `SelAnchor` the fixed one (-1 when nothing is selected); both
  snap to rune boundaries, so Turkish characters cannot be split. The painter
  draws the band in the value box, the caret from the same shaped advances, and
  input maps pointer x by measuring prefix widths with the same measurer, so
  clicks land between the glyphs they follow. Click places the caret,
  shift-click and shift+arrows extend, drag extends from the press, double click
  selects a rune class (word, number, punctuation) and triple click selects all.
- **Clipboard goes through the platform, not a framework cache.** Ctrl+C/X/V
  copy, cut and paste the selected range (paste replaces it); the platform layer
  already wraps GLFW's clipboard. Paste flattens newlines to spaces because a
  TextField is one line. Copy with no selection is a no-op, and the interactor
  takes get/put hooks so tests do not touch the real clipboard.
- **IME: commits are supported, composition preview is not.** GLFW on Linux
  delivers only committed text (through the char callback), so the framework
  treats every committed rune as insertion at the caret over the selection;
  dead keys and composed characters therefore arrive as normal text. Preedit,
  the underlined composing run and the candidate window need a native
  text-input bridge (Wayland `zwp_text_input_v3`, XIM on X11) that GLFW does not
  expose; a long value also does not scroll horizontally inside its field yet.
- **Wrap and Grid are layout containers, not new components.** `Wrap` uses the
  existing `spacing` for the gap inside a run and between runs, so there is no
  second knob; `align` places shorter children inside their run (`stretch`
  fills it). `Grid` takes a
  `columns` count (default 2) and makes each cell tight in width like
  `GridView`, while a row is as tall as its tallest child (no fixed aspect
  ratio). Unlike Material components they have no intrinsic width in a Row
  beyond the sum of their cells. Their height depends on the width they actually
  receive, so the layout recomputes it after the cross size is known rather than
  trusting the parent's available width.
- **Positioned is a Stack child, not a layout of its own.** Like Flutter, the
  box anchors to the Stack's content edges (`left`/`top`/`right`/`bottom`, `-1`
  meaning auto), stretches between two opposite edges only when it has no
  declared size, and otherwise hugs its child or follows the Stack's `align` on
  an unanchored axis. It is a real element rather than parent data on the child,
  because `.goi` has no slot for parent-owned attributes; the validator enforces
  that it sits directly in a Stack. Positioned children still contribute their
  anchored extent to a Stack's auto size, unlike Flutter, so a stack made only
  of positioned children does not collapse to zero inside a Column. It does not
  clip, so shadows inside a positioned box paint.
- **Responsive properties are maps, applied by the frame loop.** A property may
  be `{ base: ..., sm: ..., md: ..., lg: ..., xl: ... }` on Tailwind's min-width
  scale (640/768/1024/1280 logical px). The builder records the map and applies
  `base` immediately, so a headless tree and the initial window size are
  sensible; `Tree.ApplyBreakpoints(width)` then picks the largest tier at or
  below the width and marks the node dirty only when the value actually changes.
  `id` stays scalar, maps do not nest, and the node's pre-map value (its
  constructor or schema default) is the fallback when `base` is absent. The file
  owns a responsive property: `Prop.Set` cannot win, because the next frame
  rewrites it. Maps are syntax, not a new property type, so every existing
  element accepts them without schema changes.
- **Expressions are re-evaluated on each layout, not wired as observers.** A
  numeric property may be a reference (`id.prop`), arithmetic (`+ - * /`,
  parentheses) or a percentage. A reference reads the target's property cell,
  so `abc.width / 2` follows a Go `Set` or a resize on the next frame without a
  binding registry or subscription lifetime to manage; a cycle simply fails to
  evaluate and leaves the previous value. Percentages are parsed as their own
  literal and resolved during layout, because the parent's content box is only
  known then; the validator limits them to `width`/`height` and rejects a
  reference to a percentage-valued property, which would depend on a different
  parent. Expressions and responsive maps share one write path: the selected
  value is stored and applied only when it changes, so a breakpoint can switch
  between a number and an expression without leaving both active.
- **Visibility is a common property, not a per-component flag.** `visible` is a
  `Prop[bool]` on `NodeBase`, so every element and Material component accepts
  `visible: false` and breakpoint maps, and Go can call `SetVisible` on any node.
  Layout, paint, hit-testing and scroll extent read the same cell, so a hidden
  component is fully absent rather than merely painted transparent.
- **Only surfaces clip their children, not layout flow.** `NodeBase.ClipsChildren`
  is true for scroll viewports, `Window`, `Stack` and painted components; `Column`,
  `Row`, `Toolbar` and `ButtonGroup` let content spill. Clipping every node to its
  content box cut the shadow of any elevated component in a content-sized row
  and the focus ring at the edge of the column it sat in. A scrolling column
  still clips its viewport, so content that needs shadow room keeps a padding
  (the gallery columns use 16dp).
- **Split button is two segments, not one pill with a divider.** M3 Expressive
  uses a 2dp gap, a 48dp menu segment and different corner radii per segment:
  full at the outer ends and 4dp at the gap. Each segment gets its own fill,
  shadow and state layer; the trailing segment shows `arrow_drop_down`. Hover
  and press only move the state layer, never the radii. Both segments share one
  `Click`, so pressing either runs the same handler.
- **Slider follows the Expressive XS geometry.** The 16dp track is split by a
  6dp gap on each side of the handle, with full radius at the outer ends and 2dp
  at the gap. The handle is a 4x44dp pill that narrows to 2dp while pressed or
  focused, and the inactive end carries the 4dp stop dot. Pointer values map
  across the handle-centre range, not the old 10dp inset.
- **Segmented buttons are one group per contiguous run of siblings.** The
  default is multi-select: each segment toggles, so a second click clears it.
  `variant: single` gives radio behaviour: picking a segment clears the others,
  and the selected segment cannot be cleared by clicking it again. The group
  rounds only the run's outer ends, so a label next to a group does not break
  the shape.
- **Overlays are painted in display-list order, not by pipeline.** Shapes and
  glyphs used to go to two separate pipelines and were drawn shapes-then-text, so
  every label in the window landed on top of a later drawer or dialog. Glyph
  quads now share the shape instance buffer as kind 4 with the atlas bound once,
  which keeps the window at one draw call and makes tree order the paint order.
- **A modal navigation drawer is app state plus three plain elements.** The
  `.goi` file holds a `Positioned` drawer and a scrim `Rectangle` with
  `visible: false`; Go toggles `SetVisible`. Any node now carries `OnClick`, so
  the scrim and the app-bar icon are hit areas without becoming components, and
  `win.OnKey` closes the drawer on Escape even when nothing is focused.
  `NavigationDrawer` itself stays a painted panel: the framework does not own
  open/closed state.
- **SearchBar edits like a TextField.** It shares the field hit-testing, focus,
  caret mapping and selection, but starts its text 52dp in (after the search
  icon) and paints the selection band on the same 20dp line box as the caret.
  The mapping lives once in `fieldTextStart`, so pointer and paint cannot
  disagree. Search has no floating label band, so the caret is vertically
  centred instead of sitting in the value row of a TextField.
- **Text fields scroll their value instead of overflowing.** `Component.TextScroll`
  is the horizontal shift paint computes each frame so the caret stays inside
  the text box; glyphs and the selection band are clipped to that box, and
  `fieldTextStart` subtracts the same shift, so a click maps to the character
  actually under the pointer. Unfocused fields show the start of the value.
  SearchBar uses the same rule with its own 52dp inset.
- **Dragging a selection past the edge auto-scrolls.** While a text selection
  drag is live, `tick` keeps moving the x used for caret mapping at 600dp/s
  when the pointer is outside the field's text box, so text that started
  off-screen can be selected without releasing. Only that mapping x moves, not
  the pointer, so releasing still selects what it points at. The field's own
  text box comes from `fieldTextBox`, shared with the caret mapping.
- **Word editing follows the Linux convention.** Ctrl+Left/Right move the caret
  by word; Ctrl+Backspace and Ctrl+Delete remove one word plus its separator
  per press, and Alt works as the second word-delete modifier. A live selection
  still wins over the modifier, so Ctrl+A then delete behaves like any editor.
- **IME composition is a separate field, not part of the value.** `Component.Preedit`
  and `PreeditCaret` hold text the input method has not committed; paint draws
  it at the caret with an underline, the caret sits inside it, and the
  caret-follow scroll counts its width. It only enters `Text` on commit, so a
  cancelled composition leaves no trace and nothing downstream sees half-chosen
  characters. `DeleteSurrounding` is implemented because an IME corrects text it
  already committed.
- **The IME connection is bound on GLFW's own Wayland display.** `zwp_text_input_v3`
  is a second client on the display GLFW already owns, so there is no extra
  connection or event loop. The caret rectangle is pushed on focus and after
  every edit so the candidate window sits next to the text. `internal/platform/ti3.c`
  is wayland-scanner output with one change: the core `wl_surface`/`wl_seat`
  interface pointers are resolved with `dlsym` at runtime, because Go's internal
  linker cannot resolve those data relocations and this project needs internal
  linking for WebGPU. Other platforms get a no-op.
- **Text wrap is greedy word breaking, shared by layout and paint.** `wrap: true`
  breaks the value at spaces against the element's width (or the parent's when
  no width is declared), and a single word wider than the column is broken
  between runes. `text.WrapLines` is the one implementation: layout asks it for
  the line count to reserve height, paint asks it for the lines to draw, so the
  block never clips its own last line. Runs of spaces collapse to one, which is
  right for UI copy and not for preformatted text.
- **Weights snap to a loaded face.** The library carries Roboto 300, 400, 500,
  700 and 900; `text.NearestWeight` picks the closest for any declared value, and
  both the measurer and the painter call it, so a `weight: 600` measures and
  draws with bold instead of measuring regular and drawing bold.
- **Glyph fill is nonzero winding, not even-odd.** TrueType outlines overlap
  themselves: the stem and crossbar of a "t" are separate contours that cross,
  and a bowl meets its stem. An even-odd fill counts the overlap as a hole, so
  every place two strokes meet showed a white gap. The rasterizer now tracks the
  winding direction of each scanline crossing and fills where the winding is
  nonzero; counters still carve because they are wound the opposite way. Two
  tests cover it: an artificial overlap must be solid, and a rasterized "t" must
  have no transparent pixel enclosed by ink.
- **A design is a set of files loaded from an `io/fs.FS`.** `App.LoadWindow(fsys,
  path)` replaces the old path-only loader: `ui.DirFS` gives hot-reloadable files,
  an `embed.FS` gives a single executable. Expansion happens in `internal/asset`
  *before* the schema, validator and tree builder run, so none of them knows what
  a fragment is — they see one ordinary tree.
- **Imports are structural, props are values.** `import X "./x.goi"` replaces
  `X { ... }` with the fragment's root element, so the fragment owns its layout and
  the instance places it like any child. A prop always has a default or the type's
  zero value, so an instance never has to pass one; `props.name` is replaced
  before validation. (This entry predates the rename: it used `param`/`$name`; see
  the props entry at the end for why the spelling changed.)
- **An instance property is a prop or an override.** Props win by name; every
  other property is written onto the fragment root, replacing its own value
  instead of duplicating it, which is what makes `Sidebar { width: 300 }` work.
- **Fragments may share nothing.** Two instances of one fragment get separate AST
  copies, so ids collide exactly as they would if the markup were written twice,
  and the error names the duplicate. Namespacing by instance id was considered and
  left out: it needs every `ByID` to learn a new path syntax.
- **Only an entry point must be a Window.** `schema.Validate` accepts any root;
  `ValidateEntry` adds the Window rule and rejects an unbound `props.name` (nothing
  can fill it in the file a program opens). A file with props is a fragment, which
  is how `goicheck` reports it.
- **Hot reload watches directories, not the entry file.** A design is several
  files, so any `.goi` write in any directory the design used reloads the whole
  tree; the import graph is recomputed, and the watch set is rebuilt in case a
  reload added a directory.
- **Navigation destinations are a radio group keyed by their own `selected`.**
  `NavigationItem` is now interactive: a click selects it, clears the contiguous
  run of destinations and emits the container's `Select` with the index. Clicking
  the active destination does nothing, so a page cannot reload itself. The
  navigation bar and rail draw their indicator from the destination's flag rather
  than a shared index, so a rail, a bar and a drawer showing the same selection
  stay in sync no matter which one the user touched.
- **The gallery is one design, one page at a time.** Each category is its own
  scrolling column; Go flips `visible` and rewrites the app-bar label. The
  navigation surface follows the M3 size classes: a bottom bar below `sm`, a rail
  from `sm` up, and the app-bar menu opens a modal drawer with the same
  categories. The drawer's hit area is measured from the app bar, not the window,
  because the rail moves the bar's left edge.
- **The page behind the components is the theme surface, not a literal.** The
  renderer's clear colour now comes from the list's theme, so switching the theme
  recolours the whole window without the window painting a full-size fill every
  frame. A `.goi` root that declares `background` still wins, which is why the
  gallery dropped its hard-coded `#FEF7FF`: a fixed light literal cannot follow a
  dark scheme. Content colours follow the same rule, so the positioned example uses
  a `Card` as its plate instead of a lavender `Rectangle`.
- **Keyboard focus is a first-class input path.** `Tab` and `Shift+Tab` walk the
  visible, enabled controls in document order, `Enter`/`Space` press the focused
  one through the same `activate` path as a click, and focus is drawn as a 2dp
  Primary outline offset by 3dp. Text fields still own character focus; landing
  on one with Tab also gives it the caret, so typing works immediately. Focus
  follows a click too, so Enter presses the last-touched control, but the click
  path does not set `FocusRing`: the ring is an M3 keyboard affordance and a
  mouse user does not need to see it. Slider arrow keys are not wired yet.
- **Selectable text is opt-in per node.** `Text { selectable: true }` lets the
  pointer drag a range, paints the M3 selection highlight under the glyphs, and
  Ctrl+C copies it; Escape clears it. Only Ctrl+C and Escape are wired because
  the text is read-only: selection is a reading aid, not a second TextField. The
  highlight maps a line back to byte offsets through the same wrap pass the
  painter uses, so a wrapped paragraph selects per line and copy gets the
  original source range; a drag across lines keeps one range from the anchor to
  the pointer. The anchor is measured with `absoluteRect`, so a scrolled page
  does not shift the selection by its offset. Plain labels stay
  pointer-transparent, so a card's caption does not steal a click from the card.
- **The pointer shape follows the target.** Over a text field or a selectable
  `Text` it is an I-beam, over anything clickable the pointing hand, and the
  arrow everywhere else. GLFW standard cursors are cached by kind and set only
  when the shape changes, so a mouse move costs no cursor churn. A disabled
  field keeps the arrow, since it takes no input.
- **Edge insets are a fallback chain, not four props.** `padding`/`margin` set
  all sides; `paddingLeft` and friends override one edge. An unset side stores
  `-1` and reads the base value, so a declaration can change one edge without
  repeating the other three, and `NodeBase.ContentBox` is the one place that
  turns the pair into a rectangle. Layout, paint, hit-testing and scroll extent
  all go through it now; before, four copies of `padding + border` disagreed the
  moment a side differed. `gap` is a second name for `spacing` on the same cell,
  because both spellings are what people reach for.
- **A drawer slides with `translateX`.** `Positioned.left` reserves `-1` for
  "auto", so it cannot carry a negative slide. `translateX`/`translateY` shift
  the anchored box and exist for enter/exit motion: the gallery's drawer tweens
  the translate from `-width` to 0 on open and back on close, flipping
  visibility only at the ends so a half-open sheet keeps drawing. A second press
  mid-slide continues from the current offset.
- **Themes are registered by name, and `app.dark` is a mood.** `theme` used to be
  an enum of `light`/`dark` and the driver compared the string to pick a scheme.
  Now it is a plain string resolved against `App.themes`: the two baseline
  schemes are always present, `ui.WithTheme(name, t)` adds more, and a name the
  app did not register keeps the window drawing in light and reports through
  `App.OnError` instead of silently ignoring the design. `Tree.SetDark` stores the
  resolved scheme's `Dark` flag, because a custom "midnight" theme is still a
  dark theme for `app.dark`; before the driver resolves one, the tree falls back
  to the old name convention so a bare `tree.BuildSource` test still works.
  `ui.Theme`/`ui.Colors` are aliases and `ui.LightTheme()`/`ui.DarkTheme()` are
  the public entry points, so the `internal/m3` package stays internal.
- **A theme is defined on the window, after it loads.** `ui.WithTheme` had to run
  before `LoadWindow`, which put the scheme's name in Go and its value in options
  while the design had a third spelling. `Window.DefineTheme` reads the name from
  `Theme.Name`, so registration happens after the window exists (inside `Bind`)
  and redefining the same name replaces the scheme in place. That moves the
  unknown-name check from load time to the first drawn frame: warning at load
  would fire for a scheme the app was about to define. A name that is still
  unknown when the frame draws logs once and falls back to light.
- **A seed palette is CIELCh, not HCT.** `NewThemeFromSeed` needs a full scheme
  from one colour, and CAM16/HCT would be a large amount of code for a result a
  designer tunes anyway. Tones are L*, the seed's hue drives primary, tertiary is
  +60°, and chroma is clamped per palette and lowered until the colour fits sRGB,
  so every role stays in gamut and the text pairs keep their contrast. The fixed
  names `lightFromSeed`/`darkFromSeed` mean a design can select a seed scheme
  without the app passing a string through.
- **One name per enum, one cell per property.** `variant` and `size` were
  spelled twice: the schema validated against `componentdef.EnumValues`, while
  `Component.one` had its own shorter lists, so `Divider { variant: vertical }`
  passed validation and then failed to build. Both now read componentdef, and
  `TestEveryComponentPropHasItsOwnCell` sets each declared property and fails if
  any other property moved with it, which is how a name that fell through to the
  `label` fallback would be caught. `TestComponentDocsExamplesValidate` validates
  the `.goi` example on every component page, so a doc cannot drift from the
  language.
- **A navigation index counts destinations, not children.** A rail can hold a
  header, a spacer and an action of its own (the gallery's theme switch), so
  `Select` reports the position among `NavigationItem`s. Counting raw children
  made the switch shift every page by one.
- **The project is `goi`, and designs are `.goi`.** The module path, the
  command and the file extension all read `goi` (go + gui). The Go package you
  import stays `ui` (`import "github.com/goi-gui/goi/ui"`): it names the
  layer, not the project, and every example in this repo and in the docs uses
  `ui.MustByID`, so renaming the import path would churn every snippet without
  making anything clearer. The design-language pages and the component pages
  use the new extension.
- **Components have one page each.** `docs/components.md` is the grouped index
  and `docs/components/*.md` is one page per element: a `.goi` example, the
  property table and a Go example. It is generated from a script outside the
  repo (the tables mirror `internal/componentdef`), so the check that keeps it
  honest is a compile-only test of every Go snippet, run while writing the
  pages rather than committed. The index is the entry point a reader reaches
  from the README; the syntax reference still owns the language itself.
- **The frame-rate overlay is opt-in.** `app.ShowFPS(true)` draws the paint rate
  in the top-right corner; it is off by default and meant for development, so it
  lives on `App`, not in the `.goi` syntax. Only painted frames count, over a
  500ms window, so the number reports real paint cost rather than event-loop
  wakeups and it reacts while a stutter is still visible. While it is on the app
  keeps painting, because a rate can only be measured from painted frames; that
  cost is why the overlay is off by default. The gallery exposes it as `-fps`.
- **Text advances are memoized per face.** `Face.Shape` is the expensive step
  (HarfBuzz), and a wrapped paragraph measures a growing prefix for every
  candidate break, so a full relayout re-shaped the same prefixes every frame:
  about 27ms per animated frame, which read as a stutter in the Motion demo. The
  advance of a run is now cached on the face (reset past 8k entries to bound
  memory), which brings the same layout to well under a millisecond. Glyph
  drawing still shapes once per text op, so output is unchanged.
- **The rail owns the theme switch, and it is its own surface.** The gallery's
  theme control moved out of the app bar into the bottom of the navigation rail,
  where M3 puts a rail's own actions: a spacer takes the free height and the
  switch sits above the bottom edge. The app bar now has one action (the compact
  drawer). The rail paints `SurfaceContainer` like the bottom bar plus a 1dp
  `OutlineVariant` hairline on its right edge; the container alone is only a few
  units off `Surface`, so the hairline is what makes the rail read as a separate
  surface in both the light and dark schemes. `selectChildren` counts only
  `NavigationItem`s, so the switch below the destinations does not shift the
  selected index.
- **The hamburger is a compact-only affordance.** The top app bar drops the
  leading icon when `icon` is empty, and the title then starts at the 16dp M3
  inset instead of leaving a gap. The gallery expresses that as
  `icon: { base: menu, sm: "" }`, because at sm and up the navigation rail is
  already on screen and a second way into the drawer is noise; the app-bar click
  handler checks `Icon` before opening the drawer.
- **Expressions are values, evaluated every frame.** The old numeric-only
  expression path grew comparisons, `&& || !`, a conditional, string concat and
  two reserved globals (`app.theme`/`app.dark`, `window.width`/`height`). The
  evaluator returns `any` and writes back through the property's literal path, so
  a conditional can pick a role, an enum, a bool or a string. It runs once per
  tick before the frame check and marks a node only when the result changed; the
  alternative, dependency tracking on every `Prop.Set`, needs a graph and a
  generation counter for no visible gain at this tree size. `tonumber`-style
  coercion is deliberately absent: a mismatch leaves the old value, and the
  validator type-checks a bare reference at parse time.
- **A design names colors by role, not by hex.** `background: surfaceContainerHigh`
  stores the role name on the node and a resolver rewrites the cell from the
  current scheme once per tick, so a role follows a theme switch with no per
  component work and no stale paint. Hex still works and wins: a literal is a
  deliberate fixed color, which is what a scrim wants. The role list lives in
  `m3.Colors.Role`, a test reflects over `Colors` so a new field cannot be
  forgotten, and the validator suggests the nearest role on a typo.
- **Theme has two entry points and one owner.** The gallery exposes it as the
  app-bar trailing icon and as a `Switch` on the Layout page; both call one
  function that writes `Window.theme`, so they cannot disagree, and the state
  lives outside `Bind` so a hot reload keeps it.
- **What is intentionally shallow for now:** DatePicker and TimePicker render a
  month grid and a clock dial but do not edit a date; FABMenu renders the FAB and
  leaves its expanded item list to app content; Slider drags but has no keyboard
  handling. Slider arrow keys are still missing.
- **Images are packed, not bound per image.** `Image` decodes once per path in
  the ui layer (which owns the asset fs) and the renderer packs every bitmap into
  one 2048x2048 RGBA atlas with a shelf packer, drawing it as kind 5 in the same
  instance buffer as glyphs. One draw call survives, paint order holds, and
  `cover` crops by UV while `contain` shrinks the quad. The atlas has no
  eviction: past its budget images are halved and then skipped, which is the
  shortcut to revisit before an app shows many large photos.
- **Text is split into bidirectional runs.** The shaper used to force Latin LTR,
  so Arabic and Hebrew drew unjoined and mirrored. A line is now cut with
  `x/text/unicode/bidi`, each run is shaped with its own direction and script
  (from the first strong rune), and a right-to-left paragraph reverses the run
  order. A pure RTL line is one run and fully correct; caret mapping inside RTL
  text still assumes LTR prefixes. Fonts are Roboto + Material Symbols, so CJK or
  Arabic still needs `RegisterFont`; the layout now knows the difference.
- **A hidden template plus `Instantiate` is the list story.** `.goi` has no loops
  by design, and building a row by hand in Go means duplicating the design. A
  template declared `visible: false` is copied per item; ids stay on the copy for
  `Descendant` but are not registered, so copies cannot collide. `Remove` is the
  inverse. Expressions inside a template that reference a sibling by id still
  point at the template's node, which is the documented limit.
- **A reload re-runs Bind, it does not restore app state.** Node state the
  framework owns (text, caret, Go-set properties including the root's theme) is
  carried over by id, and copies made with `Instantiate` are remade when Bind
  runs again; anything else an app holds lives in a closure outside Bind and is
  re-applied there. There is deliberately no second mechanism: a generic state
  snapshot would have to know which properties an app considers state, and Bind
  already runs at exactly the right moment.
- **Motion is a tween list, not a property system.** `App.Tween`/`App.Animate`
  run an eased function on the UI goroutine; they tick before the frame check and
  keep the loop awake only while running. A full animation DSL was not needed:
  the tick loop already existed for ripples, and a tween that writes a `Prop` is
  enough for a page fade or a sheet slide. Page transitions stay out until a
  design asks for one.
- **Scrollbars are drawn, not overlays.** A scrolling node paints the M3 thumb
  last, over its content, 4dp wide with a 32dp minimum and no track. That is also
  what made wheel scrolling verifiable: the offset was always right, the demo had
  no cue. A wheel step also sets a velocity and the offset coasts with
  exponential friction, stopping at an edge or below a threshold, so the loop can
  sleep again. Dragging still does not fling.
- **`goicheck -layout` catches what syntax cannot.** It lays the design out with
  the real font library and reports a visible container with children and no size,
  or a leaf with a surface or label and no area — the class of bug behind a Stack
  that silently collapsed to nothing.
- **Not in v1 still:** full accessibility. IME
  composition now shows as underlined preedit on Wayland; per-clause styling of
  the composition (the protocol's cursor range) is drawn as one run. The previous
  phase notes that say "Checkbox deferred" are superseded: Checkbox,
  RadioButton, Switch and the rest of the component set now exist.
- **`goipls` is the runtime's own parser and schema behind a stdio server.** The
  language server (`cmd/goipls`, features in `internal/lsp`) imports `syntax`,
  `asset` and `schema` instead of re-deriving the vocabulary, so a design that
  draws also type-checks; there is no second list to keep in sync. Validation
  goes through `asset.Load` over an overlay FS: the buffer being edited is
  served from memory, its imports still resolve from disk, and each diagnostic
  keeps the `Src` it was produced with, so a typo in a fragment is published on
  the fragment. The protocol loop is single-threaded, syncs whole documents
  (a design file is small) and converts byte offsets to UTF-16 columns through
  one line index, because a Turkish label or an emoji makes the two disagree.
  Deliberately absent: code actions, rename, formatting and semantic tokens.
  Formatting would need to rewrite a file the parser keeps verbatim, and the
  editor already has the grammar.
- **The icon list is read from the font, not vendored.** Material Symbols stores
  each icon name as a ligature, so `text.IconNames` walks the GSUB table of the
  embedded woff, maps every component glyph back through the cmap and spells the
  name (4300+ of them, ~20 ms once). A checked-in list would drift the moment the
  font is updated and would need a human to re-copy it; the walk keeps
  completion honest by construction. Names are lowercase ASCII, so they insert
  as bare identifiers; the handful that start with a digit (`10k`) get quotes
  because `icon: 10k` would lex as a number followed by a word. The list is
  capped per response (200, `isIncomplete`) so one keystroke does not ship every
  name to the editor.
- **A custom title bar is opt-in per design, and it is just a Row plus a drag
  region.** `Window { titleBar: custom }` hides the platform decorations when
  the window is created; `system` stays the default so existing designs do not
  move. The design draws its own bar with `TitleBar`, which behaves exactly like
  `Row` in layout, paint and scrolling — no second layout path — but answers
  `DragRegion()`. The input layer starts a window drag only when the press is
  not on a clickable node or a text field, so the minimize/maximize/close
  `IconButton`s keep their clicks while the empty bar drags. The OS split is
  plain `app.os` (`runtime.GOOS`), not build tags, so one design file can hold
  both bars and `visible: app.os == "darwin"` picks one at runtime.
- **Window dragging moves the window by the pointer delta.** GLFW has no
  client-side drag request, so `platform.Window` remembers the window position
  and the pointer's screen position at press and calls `SetPos` with the delta on
  every cursor move. Wayland forbids client window positioning (`xdg-shell`
  exposes no request for it), so `StartDrag` returns false there and the press
  falls through to normal input; X11, macOS and Windows work. The controls
  (`Minimize`, `ToggleMaximize`, `Maximized`, `Close`) mirror the platform
  buttons through `Iconify`, `Maximize`/`Restore`, `GetAttrib` and
  `SetShouldClose`, so a design never needs platform code of its own.
- **The window layer is SDL3, not GLFW.** GLFW has no way to ask the compositor
  to move a window: on Wayland `SetWindowPos` is a no-op and `xdg_toplevel` is
  private to GLFW, so a design-drawn title bar could not drag. SDL3 exposes
  `SDL_SetWindowHitTest`, which it turns into `xdg_toplevel_move`/`resize` with
  the implicit-grab serial it already owns, and swallows the press so the drag
  never reaches the app; the same callback gives frameless resize on every
  platform. It also replaces ~1400 lines of our own `zwp_text_input_v3` binding
  (`ti3.c`, `textinput_wayland.go`) with `SDL_EVENT_TEXT_EDITING`, and picks
  Wayland or X11 at runtime, so `-tags x11` is gone.
- **`internal/platform` is the tree's only cgo, and it is deliberately small.**
  SDL3 has a Go binding (`Zyko0/go-sdl3`), but it uses purego's
  `cgo_import_dynamic` loader, which collides with the renderer's goffi FFI
  (`_cgo_init` defined twice with `CGO_ENABLED=0`, `SDYNIMPORT` relocations
  unhandled with cgo + internal linker). A shim over `sdl3` via pkg-config keeps
  the cgo confined to one file, keeps the SDL version the system's, and leaves
  the renderer's FFI path untouched. The event union is flattened to a small C
  struct in `sdl_shim.c`, so Go never mirrors `SDL_Event`'s layout.
- **The window is moved by the compositor, so the drag has no client state.**
  The input layer answers a hit test (`platform.HitTest`) for every pointer
  position: a `TitleBar` region reports drag, the outer 6dp reports a resize
  edge, everything else is a normal press. SDL calls that from its pointer
  handler and keeps the press out of the event queue, which means no
  `pressed` node, no mouse-up pairing, and no delta arithmetic to get lost.
  `titleBar: custom` creates a `WINDOW_BORDERLESS` window, which is what makes
  the design own both the drag region and the resize edges.
- **An expose during an interactive resize must schedule a frame.** SDL defers
  the wayland configure while a resize grab is active: it holds the ack until
  the next frame callback and sends `SDL_EVENT_WINDOW_EXPOSED` only to ask the
  client for a buffer. A client that renders only on its own dirt never commits
  one, so the compositor keeps the old size until the button is released and the
  window appears to jump at the end of the drag. `platform` therefore forwards
  the expose to `Input.Expose`, which sets `needsFrame` without touching the
  tree: the same display list is repainted, that frame completes the ack, and
  the resize follows the pointer.
- **One window, four native surfaces, no build tags.** `platform.SurfaceHandles`
  asks SDL which video driver it got and returns the handle pair the renderer
  needs: `wl_display`/`wl_surface`, X11 `Display*`/`Window`, Win32
  `HINSTANCE`/`HWND` or a `CAMetalLayer`. `render.NewGPU` then picks
  `SurfaceTargetFromWaylandSurface`, `FromXlibWindow`, `FromWindowsHWND` or
  `FromMetalLayer`. The alternative — build tags per platform — would mean four
  copies of the driver loop and a binary that cannot be cross-compiled at all;
  everything above `platform` is pure Go, so only this one package needs a
  compiler. The Metal view is created once and destroyed before its window,
  because SDL attaches it to the window and does not own its lifetime.
- **A Wayland session gets SDL's Wayland driver, not its XWayland fallback.**
  SDL 3.4 keeps the Wayland driver only when the compositor offers `fifo-v1`;
  without it (niri does not) it prefers X11 and connects through XWayland. There
  the window is redrawn by the X server between buffers, so an interactive
  resize shows the window black for a frame at a time and only settles when the
  drag ends; a borderless window also loses its hit-test resize and move edges,
  because XWayland has no client-side drag. `platform.Init` therefore sets
  `SDL_HINT_VIDEO_DRIVER=wayland` before `SDL_Init` when `WAYLAND_DISPLAY` is
  set, as SDL's own fallback message suggests. An explicit `SDL_VIDEODRIVER`
  still wins. The frame pacing the hint trades away is what the renderer's FIFO
  present already gives, so nothing is lost.
- **The language server completes the reserved globals too.** `app.os` was
  added to the expression layer with the custom title bar, and discovering a
  global by reading the docs is not the same as getting it from the editor. The
  set is closed and tiny (`app.theme`, `app.dark`, `app.os`, `window.width`,
  `window.height`), so `globalItems` lists it directly rather than deriving it
  from the validator: the validator knows how to reject an unknown name, not
  what it means. Everything else in the server already came from the shared
  schema, which is why `TitleBar` and `Window.titleBar` needed no new code —
  only a test to prove they reached the editor, since the stale installed binary
  was the real reason Zed flagged them.
- **A chip's label box is its padding minus its icons, and selecting one is a
  layout change.** The width said 32 + 18 for a leading icon, but the paint path
  only reserved 8dp for that icon and then asked for a label box that subtracted
  a further 8dp on the trailing side, so a selected or icon-bearing chip gave the
  text less room than it needs and the run spilled past the pill. Both sides now
  agree on the same numbers (16dp clear, or 8 + 18 + 8 with an icon) and the
  `selected` property is `DirtyLayout` because it swaps the leading icon for a
  check, which changes the width to a size that was never laid out.
- **A switch toggle is animated by the ui layer, not by a property.** Adding a
  numeric "position" property would put a frame-rate concern into the design
  language, so `Component` carries `TogglePos`/`TogglePosed` as UI state next to
  the hover and ripple fields; `ui`'s per-frame tick advances it and paint reads
  it. The motion eases toward the target every tick instead of running from a
  start time (`switchTau` 45 ms, about 150 ms to settle at 60 Hz), so flipping
  the switch again mid-travel reverses it rather than restarting, and the handle
  snaps onto the exact end state so an idle window stops asking for frames. The
  first tick adopts the design's `checked` as the settled position, so a page
  load or a hot reload does not slide every switch on. Paint falls back to
  reading `checked` directly when `TogglePosed` is false, which keeps the
  headless paint tests (and anything that builds a tree without `ui`) drawing the
  end state.
- **A remote image is fetched off the UI goroutine and painted before it
  arrives.** Blocking the tree build on the network would freeze the window on a
  slow host — the very case the feature is for. So `loadImages` reads local files
  as before but only marks a remote `src` as loading and starts a request keyed
  by URL; the node lays out at its declared size and paints a faint breathing
  wash (`Image.Phase`, advanced by the frame tick like the ripple). The request
  is capped (32 MiB, 20 s) and the decode runs on the fetch goroutine, with the
  result posted back; the arriving bitmap is matched to nodes by `src` on the
  current tree, so a fetch that outlives a hot reload still lands. Loading state
  and phase live on the node as UI state, not as properties, for the same reason
  the switch handle does: the design language stays free of frame-rate concerns.
- **The theme's type scale is a package-level value, not something `Light()`
  rebuilds.** `RoleStyle` is called for every text measurement, and it used to
read the scale off a freshly allocated `Theme`; on the gallery page that was
  ~21 MB of garbage per relayout and 620 allocations per frame. The scale does
  not vary between light and dark, so `typeScale` is one value and both schemes
  point at it.
- **`layout.Engine` keeps one scratch `m3.ComponentView`.** The view exists to
  keep `m3` free of a `tree` import, so it cannot be replaced by reading the
  component directly; filling it in place instead of allocating one per call
  removed the other 75% of the layout garbage. The rule is that the pointer is
  only valid until the next `componentOf`, which the four call sites already
  satisfy because they use it for one `MetricsFor` call.
- **`Tree.Walk` exists because `Tree.All` allocates.** A hover update, the switch
  tick and the image-load tick all scan the whole tree, and doing that with a
  fresh slice every pointer move or frame is pure garbage. `All` stays for tests
  and one-off work; anything on the frame path uses `Walk`.
- **`text.Library` remembers its last face lookup.** A page uses a handful of
  sizes and asks for each hundreds of times per layout, so one cached key in
  front of the map lookup removes most of the lock-and-hash cost of measuring.
- **Shaping is cached per face and string.** Every repaint walks the display list
  and calls `Shape` for each text run, and a short line costs ~20 µs to shape;
  a page with a few dozen runs therefore paid a millisecond per repaint just to
  re-shape strings that had not changed. `Face.shaped` keeps the whole `shaping.Output`
  (glyphs included) for up to 512 distinct runs per face, dropping the map when
  it fills. The outputs are read-only to every caller — `shapeOp` reads `Advance`
  and the glyph slice — so the cache is safe to share. A distinct run is still
  ~30 µs, which is why the map is per face (size and weight) rather than global:
  the key is the string alone.
- **Layout memoizes per pass, and the pass boundary is the only invalidation.**
  A parent asks a child for its natural size while measuring itself and again
  while placing it, and each ask recursed into the whole subtree, so a deep tree
  was measured once per level; a component's metrics were recomputed for the same
  reason. `Engine.memo`, `Engine.metrics` are cleared at the top of every
  `Layout`, which is where a property change lands, so nothing stale survives.
  Only subtrees are cached: a leaf is cheaper to measure than to look up, and a
  wide row of 10k children made the map lookup a net loss. `TestPassCacheIsPerPass`
  grows and shrinks a text and a chip across two calls to prove the boundary.
- **`containerOf` returns a shared zero `Container` for nodes that have none.**
  It used to allocate one per call, which happened for every component in the
  tree. Nothing writes through it; `Spacing.Get()` on the zero cell is 0 and
  `Align.Get()` is "", which is exactly what a non-container should answer.
- **`text.Library` keeps a four-slot direct-mapped face cache.** The single
  "last face" slot missed almost every time because a page alternates between
  sizes (11, 12, 14, 16, 22), so the map hash and the lock were paid per
  measurement. Four slots keyed by the size absorb that pattern.
- **A FAB menu is laid out by the layout pass, not by the painter.** It was
  drawn as a single 56dp box with its actions placed at the parent's origin,
  which is why `expanded` did nothing visible. Now a closed menu is just the FAB
  and its actions get zero bounds (so neither paint nor hit testing sees them),
  and an open one is the FAB at the bottom with the actions stacked above it,
  right-aligned, the last declared action nearest the FAB. `expanded` inherited
  `DirtyLayout` from the component definition, so the switch already relaid out.
- **A node with no area draws nothing of its own.** A collapsed FAB menu left its
  action glyphs on the parent's origin: the icon of a zero-size node was still
  emitted at that point. `walk` and `walkOffset` now skip a node's own visuals
  when its box has no width or height, while still walking its children, so a
  zero-size Stack can keep holding anchored content.
- **A floating Toolbar hugs its actions.** `isContainer` made every container
  stretch on the cross axis, and a Toolbar was in that list, so a pill meant to
  wrap five icon buttons spanned the whole page. It no longer stretches; a design
  that wants a full-width bar can set `width: 100%` or put it in a Row.
- **The viewport uniform is only written when it changes.** It was uploaded every
  frame, and wgpu's Vulkan backend drains the queue (`waitForGPU`) on every
  `Queue.WriteBuffer`. That drain was invisible in the median but showed up as
  3–21 ms spikes whenever it landed near the end of a frame, which is what made
  the FPS counter dip to ~50. `GPU.uniformWritten` now guards the write. The
  remaining per-frame instance upload drains the same way and is an upstream
  limitation; measured at ~0.5% dropped frames on the gallery, so it stays.
- **Vsync can be turned off, and `MaxFPS` caps what that exposes.** The paint
  rate was pinned to the panel: Fifo present mode blocks in `acquire`, so a
  60 Hz display always reads 60 and the overlay could not show headroom. With
  `VSync(false)` the surface switches to mailbox and a frame is presented as
  soon as it is ready (measured: ~1500 fps on the gallery, same panel), which is
  useful for benchmarking and useless for a shipped app. `MaxFPS(120)` is the
  companion: the loop sleeps to an absolute deadline, one period per frame, so a
  late frame is not made up by a burst. `presentModeFor` rejects an unsupported
  mode, so a surface without mailbox falls back to Fifo and reports the error;
  the default stays vsync-on, and idle apps still block in `WaitEvents`.
- **The pickers draw their cells themselves, and input reads the same
  geometry back.** A month grid and a clock dial have no nodes per cell, so
  `paint.DateLayoutOf`/`TimeLayoutOf` are pure functions of the box and the
  input layer calls them to map a click; the painter and the hit test cannot
  drift. The value is the component's `text` (`YYYY-MM-DD`, `HH:MM`) and a pick
  emits `Submit`, so an app stores the result without a new event type. The
  month arrows move `PickerMonth` and leave the value alone, and the time dial
  hands over from hours to minutes after the first click, which is how M3 asks
  for it. Before this the pickers were fixed pictures: a click did nothing and
  the date was hard-coded to the 12th.
- **The time picker is 24-hour, with two rings.** A 12-hour dial cannot express
  13-23, and the value format was already `HH:MM`. The outer ring is 1-12 and
  the inner one 13-00, so a click's distance from the centre picks the half of
  the day and its angle picks the hour; `DialInner` decides which ring gets the
  filled circle. Midnight lives on the inner ring (00, at the top), which is why
  the test pins `00:30` there.
- **`Code` is a tokenizer, not a parser.** A block needs to read as code without
  a language server: line and block comments, quoted strings, numbers and a
  keyword set per language are enough, and every byte of the source is covered
  by exactly one span so no run is dropped. Lines break only on `\n` because a
  code block that reflows is unreadable; a long line is clipped. Tabs expand to
  four spaces here rather than in the font, since a tab has no advance.
- **`TextArea` wraps with its own `text.WrapEdit`, not the label `WrapRanges`.**
  A label may collapse whitespace and drop offsets; a caret cannot, so the edit
  wrapper keeps every byte and gives exact ranges. It is a separate exported
  function for that reason, and paint and input both call it. Enter inserts a
  newline in a TextArea and still submits in a TextField, which is the split
  users expect from every toolkit.
- **Selectable text and fields share the click-count logic, separately.** A
  double click selects the word and a triple click the paragraph between
  newlines; after a multi-click the drag is disabled, so the selection does not
  collapse the moment the pointer moves. That is the desktop convention, and it
  lives in `pressTextNode` because a static Text is not a field.
- **A row's intrinsic width includes its children's margins.** Placement already
  laid a child out inside its margins, so a Toolbar measured without them came
  out 52dp narrower than what it drew and the last action hung past the pill. A
  Toolbar also gained 12dp of content inset, since its 40dp actions were touching
  a 64dp rounded box.
- **`m3.ComponentView` carries `Spacing` and `Align`.** A component keeps them in
  its own cells while `Container` keeps separate ones, so the layout engine was
  reading 0 spacing and "start" alignment for every Toolbar, List and Drawer.
- **Code highlighting is Chroma's, not ours.** The hand-written tokenizer only
  knew six languages and only the ones someone remembered to add. Chroma ships
  ~300 lexers behind a stable `Tokenise` API, and this package only maps its
  token types onto the theme's six color roles. Cost: `go build` grows by about
  4MB, because the lexer registry embeds every grammar; that is the price of
  "any language works" and it is paid once per binary. `internal/highlight` is a
  new leaf package so `paint` never sees a Chroma type.
- **A Code block's selection indexes the drawn text, tabs expanded.** The font
  gives a tab no advance, so the block draws tabs as four spaces; the selection
  and the copy button use that same string, which makes "what you see is what
  you copy" true. `Code.Spans()` caches the lexer run and re-runs only when the
  text or the language changes, because a still block is repainted every frame.
- **Selection helpers take a `tree.Node`, not a `*tree.Text`.** Code needed the
  same drag, double-click-word and Ctrl+C path as selectable prose, and the
  alternative was a second copy of the geometry. `selectableAt`, `selText`,
  `selRange`, `selIndexAt` are the seam; a new selectable kind only adds a case.
- **ClipRRect clips by rounding the ops its subtree emitted.** The clip rect was
  already narrowed by `ClipsChildren`; the corner radius is applied to the ops in
  the subtree's range rather than threading a second rectangle through every
  painter. A nested ClipRRect has already rounded its own range to its own
  radius, so the outer pass overwrites it with the outer radius — which is what
  a real clip stack would produce, since the outer cut is the smaller one.
- **A wheel notch scrolls one step; only a touchpad coasts.** `Scroll` used to
  start a fling for every wheel event, so one click of the wheel travelled 48px
  and then coasted: with the old 8000px/s cap and a friction of 6 that is another
  1300px, which is why scrolling felt like it ran away. A touchpad and a notched
  wheel are told apart by the delta itself (libinput reports a notch as a whole
  step and a touchpad as a fraction of one), the coast is capped at
  `flingMaxSpeed/flingFriction` ≈ 430px, and a notch cancels a coast in progress.
  `TestWheelNotchDoesNotFling`, `TestWheelSpinStaysLinear` and
  `TestTouchpadFlingIsCapped` pin all three.
- **A rounded clip follows the box, not the visible intersection.** The ops a
  `ClipRRect` covers carry `ClipBox`, the box the component declared, next to
  `Clip`, the intersection of every clip above it. Rounding `Clip` put a corner
  wherever the viewport happened to cut the card: a `ClipRRect` scrolled half out
  of its page still drew rounded top corners at the cut. The shader rounds
  `ClipBox` and lets the hard clip cut the rest, so the only rounded corners are
  the ones the component actually has. `TestClipRRectRoundsItsOwnBoxWhenScrolled`
  pins it.
- **The clip radius travels in `misc.z`, because `misc.y` is a UV.** Glyph and
  bitmap instances store their atlas far corner in `misc.xy`, so reading the
  radius from `misc.y` rounded every clipped glyph and bitmap by a sub-pixel
  fraction of the atlas — harmless-looking, but it meant an image inside a
  `ClipRRect` was clipped square. Shapes, glyphs and `image.go` now put the clip
  radius in `misc.z`.
- **`language` gets its own completion list.** It is a plain string in the schema
  (an unknown lexer must still draw), so the editor had no values at all for it.
  The list comes from `internal/highlight.Languages` plus the short aliases, is
  filtered by prefix and capped, and quotes names such as `c++` that are not bare
  `.goi` identifiers.
- **A scroll gesture continues into the page when a child is at its end.**
  `Scroll` used to move only the nearest scrollable ancestor, so a `TextArea`
  that fits, or a list already at its end, swallowed the wheel and the page
  behind it looked frozen under the pointer. The gesture is now applied in
  pixels from the nearest scrollable node upwards: each node takes what it can
  and passes the rest on. A touchpad coast that reaches an edge is handed on the
  same way, so a flick does not die at the inner end. `scrollChain` is the one
  implementation, shared by the wheel and `tickFling`;
  `TestWheelChainsFromInnerToPage`,
  `TestWheelPassesThroughAnInnerThatCannotScroll` and
  `TestTouchpadFlingChainsToThePage` pin it.
- **A scroll gesture only moves the axis it was made on.** A vertical wheel used
  to be pressed into service for a horizontal area under the pointer, so
  scrolling a page down through a sideways row dragged that row along. The
  wheel is now axial: a notched wheel is vertical, **Shift swaps the axes** (so
  it reaches a sideways area, and a sideways wheel becomes vertical), and a
  touchpad's real dx/dy are kept apart, so a two-finger sideways swipe scrolls
  a row without Shift. `scrollChain` takes both pixel amounts and each node
  consumes the one for its own axis, which also means a diagonal swipe scrolls
  the row sideways and the page vertically at once. Drag-to-scroll follows the
  same rule and the same chain, moving by the step between two pointer
  positions instead of an absolute offset.
  `TestWheelKeepsItsAxisOverAHorizontalRow`, `TestShiftWheelScrollsTheHorizontalRow`,
  `TestTouchpadKeepsBothAxes`, `TestDragScrollChainsAndKeepsItsAxis`,
  `TestDragKeepsItsAxisOverAHorizontalRow` and `TestDragScrollsTheHorizontalRow`
  pin it.

- **Fragment children are a `$children` slot, expanded in the caller's file.**
  A fragment that takes children writes `$children` where they should appear;
  an instance passes ordinary child elements. Expansion substitutes them before
  validation, so the schema and the tree builder still see one plain tree. The
  children are expanded with the *caller's* aliases, because their imports
  belong to the file they were written in; passing children to a fragment with
  no slot is an error reported in the caller. A file that writes a slot is a
  fragment even without params (`File.IsFragment`), so opening it in the editor
  does not demand a `Window` root and previewing it simply leaves the slot
  empty. `TestChildrenLandAtTheSlot`, `TestChildrenWithoutASlotIsAnError`,
  `TestSlotWithoutChildrenDrawsNothing` and `TestChildrenSlotIsAFragment` pin
  it.
- **Touchpad x is not the wheel's x.** SDL's wheel y is positive away from the
  user while its x is already a scroll direction (positive to the right), so
  the vertical delta flips and the horizontal does not: `panX = +dx`. A
  notched wheel is vertical, and Shift swaps the axes as `dx, dy = -dy, dx`, so
  Shift + wheel-up scrolls left, matching a browser. With the old `-dx`, a
  two-finger horizontal swipe moved the content against the fingers, which is
  what natural scrolling must not do. `TestTouchpadKeepsBothAxes` pins it.

- **A row prototype is `template: true`, and `ui.Fill` rebuilds the list.** The
  old recipe (hidden template + a clear loop + `Instantiate` per item + a `Set`
  per field) had two traps: forget the clear and a refresh stacked duplicates,
  and the template id was repeated in Go and in the file. `template: true` hides
  the marked row when the tree is built and lets `Fill` find it; `Fill` removes
  the other children, instantiates one copy per item and hands each copy to the
  caller. Copies drop the marker, because a copy is a row and not a prototype,
  so a second `Fill` starts from the same prototype. `Instantiate`/`Remove`
  remain for a row added or removed one at a time. A `repeat` in `.goi` was
  rejected: it needs field access and an untyped `$item` scope, which neither
  the schema nor the LSP can check, trading the design file's compile-time
  validation for a little less Go. `TestFillRebuildsFromTheTemplate` and
  `TestFillNeedsATemplateChild` pin the behaviour.

- **The language server documents both new pieces.** `$children` is offered
  after `$` in a container body (not in a leaf, and not twice), hovers as a slot,
  and `template` hovers with the `ui.Fill` explanation instead of the generic
  common-property line. Since `$children` only survives parsing and is gone by
  the time the schema sees the tree, none of this needed a new diagnostic path:
  the editor reads the AST. A fragment instance marked `template: true` works as
  a template row, so a row's design can live in its own `.goi` file.
  `TestCompletionOffersChildrenSlot`, `TestHoverExplainsTheSlotAndTemplate` and
  `TestFragmentInstanceCanBeATemplateRow` pin it.

- **`{#if}` is a visibility switch on flat arms, not a node.** `.goi` needed a
  way to switch part of a design on the state the app already has. Two shapes
  were possible: a container node wrapping each arm, or builds every arm as an
  ordinary child of the enclosing element and toggles `visible`. The second
  wins: layout already skips an invisible subtree, so a hidden arm costs nothing
  after the build, and a `Row` keeps laying the shown arm out as a row item
  instead of wrapping it in a foreign box. The chain semantics are Svelte's: an
  arm shows when it is the first condition that holds, and the `{:else}` arm
  shows when none does; nested blocks multiply with their outer arm. The
  conditions ride the existing expression engine (`app.dark`,
  `window.width > 900`, `id.checked`) and are re-evaluated in
  `ApplyExpressions`, so a Go `Set` reaches them with no new mechanism. Cost of
  the flat shape: every arm is built even when hidden, and the arms cannot
  declare the same id. `{#each}` waits until the data story is designed; the
  parser says so instead of failing to parse.
  `TestIfBlockTogglesItsArm`, `TestNestedIfBlockNeedsItsOuterArm`,
  `TestIfBlockAddsNoContainerNode`, `TestParseIfBlockErrors` and
  `TestIfBlockValidation` pin it.

- **`{#each}` repeats a design body from a declared list, and Go sets the
  list.** A design could already borrow Go for a row (`template: true` plus
  `ui.Fill`), but the row's markup lived in Go's head and the template id was a
  string in two places. `{#each data.rows as row}` keeps the row in the design
  and needs one call to fill it. The data is *declared* (`data ticket { … }`)
  rather than inferred, because only a declaration keeps the design
  self-checking: every `row.field` is verified against the shape, and every field
  is verified against the property it feeds, at build time and in the editor.
  Values reach the tree by `ui.SetData`, whose slice is read with reflection
  (`json` tag, then field name) so an app passes its own structs unchanged; that
  is the one place the framework guesses, and a mismatch shows up as an empty
  value, not a crash. A list rebuild replaces the block's nodes only, and
  registrations (`exprNodes`, `conds`, `eachs`, ids) are pruned with the nodes, so
  a refresh cannot grow the tree or leave a loop pointing at a dead parent.
  Row ids stay on the nodes but are not registered, like an `Instantiate` copy,
  because every row would collide; the `{:else}` arm is built once and registers
  normally. `{#each}` is deliberately not a general for-loop: no index range, no
  filtering, no sorting — those belong in Go, which supplies the slice already in
  the order the design shows. `TestEachBuildsOneRowPerItem`,
  `TestEachRowsGetTheirOwnScope`, `TestEachOnEachHooksEveryRebuiltRow`,
  `TestEachPrunesStaleRegistrations`, `TestNestedEachWalksTheInnerList`,
  `TestDataValidation`, `TestParseDataAndEach` and
  `TestDataAndEachAreKnownToTheEditor` pin it.

- **Alignment is two axes, named after Flutter's.** One `align` on a container
  was ambiguous: on a `Column` it meant "where across", on a `Row` it meant the
  same thing but read as if it were vertical, and there was no way to say "push
  the run to the end" or "spread the children apart" at all. `crossAxisAlign`
  (across the flow: sideways in a `Column`, up and down in a `Row`) and
  `mainAxisAlign` (along it, on the leftover space) replace it, with Flutter's
  six main values: `start`, `center`, `end`, `spaceBetween`, `spaceAround`,
  `spaceEvenly`. One `distribute` helper splits the leftover into a leading
  offset and an extra gap, so a `Row`, a `Column`, each `Wrap` run and a `Grid`'s
  rows all read the same six names. A `Stack` and a `ClipRRect` overlay a single
  box, so they accept the four geometric values on the main axis instead and
  borrow the `Column` convention (main runs down). `align` stays only on `Text`,
  where it means what it says: how the glyphs sit inside their own box.
  `alignSelf` is unchanged and still overrides the parent's `crossAxisAlign` for
  one child. `TestMainAxisAlignment`, `TestMainAxisAlignmentIgnoresAFullRun`,
  `TestColumnAxisNames`, `TestWrapMainAxisAlignPerRun` and
  `TestAlignmentAxesAreSeparate` pin it.
- **The Zed extension ships the current grammar.** `{#if}`, `{#each}`, `data`
  and `$children` were in the language but not in the tree-sitter grammar, so the
  highlighter parsed blocks as errors. The grammar now has `data_block`,
  `data_shape`, `data_field`, `if_block`, `each_block` and `children_slot`,
  generated with the `tree-sitter` CLI at ABI 14 (the version the shipped
  `parser.c` already used), and `highlights.scm` colours the block tags as
  keywords, the row variable as a variable and a declared shape as a type.
  `outline.scm` nests blocks and shapes, `indents.scm` indents a block body.
  Generating the parser needs the CLI, which is not on this machine's PATH:
  `npx tree-sitter-cli` downloads its binary on install, and its `install.js`
  has to be run once (`node install.js`) before `tree-sitter generate` works.

- **The theme mood is a `light`/`dark` value, not only a bool.** `app.dark` could
  answer a yes/no question, but a design that wanted to *feed* the mood to a
  property (an icon, a label, a scheme name) had no value to pass, and
  `app.theme` reports the scheme's name, which for a registered `brand` theme
  says nothing about light or dark. `app.mode` is that value, backed by
  `tree.Mode` on the Go side (`ui.Mode`, `Window.Mode()`), and `app.dark` stays as
  the bool shorthand so existing files keep working. The desktop preference is
  `app.systemMode`, read through `SDL_GetSystemTheme` and refreshed on
  `SDL_EVENT_SYSTEM_THEME_CHANGED`, which means `theme: app.systemMode` gives an
  app that follows the OS with no Go code and no polling. `ModeUnknown` exists
  only for that desktop value, because a platform may not report one; a window
  always resolves to light or dark. The language server completes both globals
  with the `light | dark` detail. `TestModeFollowsTheResolvedScheme`,
  `TestModeFallsBackToTheThemeName`, `TestSystemModeIsWhatTheDesktopSaid`,
  `TestWindowModeReachesGo`, `TestAppModeGlobals` and
  `TestCompletionOffersModeGlobals` pin it.

- **A Go write takes the property back from its expression.** `theme: app.systemMode`
  made the window follow the desktop, but the gallery's switch then did nothing:
  `Prop.Set` changed the value and `ApplyExpressions` put `app.systemMode` back on
  the next frame, because the expression owned the cell forever. The alternative
  was a gallery-only workaround (drop the expression, copy `SystemMode` in Bind),
  which leaves the trap for every app that documents the same pattern. Instead
  `applyExpressions` skips a property with `GoSet` set, so the precedence is
  "Go wins until reload", the same rule hot reload already carried Go-set values
  by (`LiteralChanged`), with `CopyFrom` marking a carried value as Go's so a
  reload without a file edit does not hand it back. It applies to every expression, which is what the rest
  of the framework wants anyway: typing in a `TextField` whose `text` came from
  an expression, or dragging a `Slider` whose `value` did, must not be reverted
  either. Costs: the expression stays recorded but idle, and Go cannot hand the
  property back without a reload (a changed .goi literal reclaims it). The gallery
  tracks `themeChosen`, so a fresh run keeps following the desktop until the
  switch is used; `TestGoSetTakesThePropertyFromItsExpression` pins both halves.

## Phase 13 — fragments as components

- **An instance's id namespaces the ids its fragment declares.** A fragment with
  an id inside it used to be usable once: two instances collided, so a design
  with behaviour could not reuse its own part. `Card { id: first }` now makes
  `Card.goi`'s `title` id `first.title`; the pattern allows dotted segments, the
  tree registers the full name, and `ui.ByID` needs no new syntax. References
  inside the fragment are rewritten with the same prefix, so `side.width` points
  at that copy's `side`. Scoping runs before the caller's children land in a slot
  and before the caller's properties are written onto the root, because those
  belong to the caller's file. An instance without an `id` scopes nothing, so the
  old behaviour (and the old duplicate-id error) stands. Two side effects:
  `copyValue` now clones leaves, since rewriting an `*Ident` or `*Ref` in place
  would edit the cached fragment file and double a prefix on the second
  instance; and `ui.Descendant` inside a scoped instance needs the prefixed name.

- **Any `$name` in member position is a slot.** `$children` was hard-coded in the
  parser and the expansion, so a card could not have a header and a body. A bare
  `$header` in a fragment body is a slot marker and still makes the file a
  fragment; the same name with a body is how an instance fills it:
  `Card { $header { Text { ... } } }`. Plain children keep going to `$children`.
  Passing a slot the fragment does not declare is a diagnostic in the caller, and
  an empty `$header { }` counts as filled, which is how a conditional header is
  written. The cost is that the parser can no longer reject `$params`, so a typo
  in a slot name now shows up as "unknown slot" from the expansion instead.

- **`data` declarations merge across the design.** A fragment's `data { ... }`
  block and its shapes used to be parsed and dropped: `instantiateFile` copied
  only the entry file's declarations, so a fragment that drew its own `{#each}`
  failed validation and could not be fed. `Load` now appends every imported
  file's declarations after the entry's, which matches how `SetData` works (one
  name, app-wide) and lets a fragment own the shape it repeats. A name declared
  in two files is a duplicate-variable error from the validator, not silent
  shadowing.

- **`goicheck -layout` previews a fragment with its param defaults.** A fragment
  has no Window root, so the checker used to validate it and stop, which left
  the one class of bug it exists for — a component that collapses to zero size —
  untested in exactly the files a design reuses. `asset.Preview` fills the
  params with their defaults (the same replacement an import does) and the
  checker wraps the root in a synthetic Window. It is deliberately not part of
  `Load`: the validator still sees `$param` and can point a wrong param type at
  the param line.

- **`bridge` is `data` plus handlers.** A design can now declare handlers next
  to its values (`bridge { onOpen: func(id: string) }`) and call them from event
  properties (`onClick: bridge.onOpen(row.id)`). `bridge` and `type` parse into
  the same structures as `data <name>`, so every existing file keeps working and
  the validator, tree and language server share one path. The call is deliberately
  not an expression: it is only legal as the whole value of an event property,
  because its arguments must be evaluated when the event fires, not every frame;
  that is what makes `row.id` read the row that exists at click time. Handlers
  live on `tree.Tree` rather than on each node, so `Bind` can register them again
  after a hot reload without leaking subscriptions. The event list and payload
  types live in `schema` so the validator, the builder and the LSP cannot drift;
  a missing handler or a bad argument surfaces through the app's error hook
  rather than panicking inside an input callback.

- **The editor treats an id like a small namespace.** `abc.width` was already
  readable at runtime (`tree.evalRef` falls back to `ByID`), but the language
  server only listed ids for `id:` values, so reading one meant remembering the
  name. Now every value position offers the file's ids, `abc.` completes the
  element's schema properties, hover explains the id or the property, and
  go-to-definition jumps to the declaring `id:`. The element kind comes from the
  element that carries the `id:`, not a guess, so the member list cannot offer a
  property that node does not have. Ids inside a block arm are offered too: the
  design still writes the bare name there.

- **`props` replaces `param`, and a prop is typed.** A fragment used to declare
  `param title: ""` and write `$title`; both are gone. One `props { title: string
  = "Mail" }` block declares the inputs and the fragment reads `props.title`,
  which is an ordinary reference like `bridge.count`. Three things follow: the
  type is stated once and checked against every property the prop feeds (instead
  of being inferred from a default literal); a prop the instance leaves out is
  its default or, with none, the type's zero value (`""`, `0`, `false`,
  transparent black), so a fragment never guards for a missing value; and the
  reference reuses the path the parser, validator, expander and editor already
  handle, so completion and hover come for free and `$` stops meaning two
  different things in value and member position. Props stay scalars, because a
  prop is substituted as a literal and a list has no literal to substitute. The
  lexer now emits a lone `=` for the default; only the props block accepts it, so
  a stray `=` elsewhere still fails at the same edit.

- **A value can interpolate, and `+` means what the reader expects.** `` `Total:
  ${price * count}` `` parses into a `Template` whose parts alternate literal
  strings and ordinary expressions, so a hole gets the full expression language
  and re-reads its references every frame. The parser parses each hole with the
  same value parser by padding the hole's text with spaces to its byte offset:
  every position it reports is then already an offset into the real file, so a
  diagnostic inside `${...}` lands on the right column with no position fix-up.
  `+` adds two numbers and joins anything else as text, which is what makes
  `"n: " + count` work; `-`, `*` and `/` stay numeric because there is no
  sensible concatenation for them.

- **A bridge variable starts at a default, then its zero value.** `count: number
  = 0` is declared in the design and seeded into the tree when it is built, so a
  design reads a real value before Go says anything; `ui.SetData` still replaces
  it. Without a default the seed is the type's zero (`""`, `0`, `false`,
  transparent black). The alternative — leaving the variable unset so the
  property keeps its own default — was rejected because it makes `{#if
  bridge.flag}` depend on whether Go wrote first.

- **A list prop is a bridge reference, not a list literal.** `.goi` has no
  list literal, so `props { rows: [ticket] }` is filled by naming a bridge list
  (`List { rows: bridge.tickets }`) and the fragment walks it with `{#each
  props.rows as row}`; expansion rewrites the block's source to the bridge name
  before validation, so the runtime only ever sees `{#each bridge.tickets}`. A
  handler prop works the same way: `props { onOpen: func(id: string) }` plus
  `onClick: props.onOpen(row.id)` becomes `bridge.onOpen(...)` once the instance
  names the handler. Both keep one execution path instead of teaching the tree
  about fragment inputs.

- **The design language reads like Go, because that is where behaviour lives.**
  Declarations are `Name Type` (`bridge { Count int }`, `type Row { Title
  string }`, `props { Rows []Row }`), blocks are `if cond { … } else { … }` and
  `for i, row := range bridge.Rows { … }`, and a string is formatted with
  `formatf`, Go's `fmt` in a value. The JavaScript-flavoured spellings
  (`{#if}`, `{#each … as}`, `bridge.Count: number = 0`, backtick `${…}`) are
  gone; nothing is left that a Go reader has to translate. Every primitive Go
  has is accepted, plus `[]T`, `map[string]T` and `func(...)`, so a declared
  type maps to the Go field the app actually holds.
- **No declaration takes a default; the zero value is the default.** A variable
  the app never sets reads its type's zero — `""`, `0`, `false`, transparent
  black, an empty slice or map. That removes the second source of truth the old
  `= 0` introduced (was the value the design's, Go's, or both?) and makes the
  first frame identical whether or not Go wrote before it. `ui.SetData` and
  `ui.SetHandler` stay the only place a value starts.
- **A struct reaches the design through its `goi` tag.** `SetData` matches a
  field by `goi:"name"`, then by the field name with a lower first letter; an
  unexported field and a field tagged `goi:"-"` are invisible. The tag is a
  separate key from `json` on purpose: a design name is not an API name, and a
  struct already carrying JSON tags should not be forced to leak them into the
  layout.
- **`formatf` replaced template strings.** A backtick literal needed its own
  lexer token, its own AST node, a nested parse for every hole and a formatter
  per value type; `formatf` is one builtin call, gets `%d`/`%s`/`%0.2f` for free
  from `fmt`, and needs one small helper that hands `%d` an integer because a
  design number is a `float64`. The `+` rule survives: two numbers add, anything
  else joins.
- **A range loop is the only loop, and it follows Go exactly.** A slice gives
  index and element, a map gives key and value (sorted, so a frame is stable), a
  number counts. `_` and a missing name both mean "not bound". There is no
  `{:else}` arm: an empty list draws nothing, which is what a Go `for` does, so a
  design that wants an empty state switches on a bool the app sets
  (`if bridge.Empty { … }`).

- **An import reads like Go's.** `import Sidebar "./Sidebar.goi"` names the alias
  first, which is the order a Go import uses and the order the reader already
  knows. `as` is gone from the grammar, so the token no longer means two things
  (an import keyword and a value word), and the parser has one shape to check.

- **Only the pure Go builtins are callable.** `formatf` is ours; `len`, `cap`,
  `min` and `max` are Go's. `len` is also the one place a list is a value,
  because counting a list is a common need and the alternative (`bridge.N`) means
  the app has to keep a second variable in sync. The rest of Go's builtins
  allocate, mutate, or talk to the process (`append`, `make`, `new`, `copy`,
  `delete`, `clear`, `close`, `panic`, `recover`, `print`, `println`, `complex`,
  `real`, `imag`). A design value is re-read every frame, so those have no
  meaning; the validator rejects them by name and lists what a value may call.

- **`goifmt` rewrites the layout, never the meaning.** It prints from the AST and
  the comment list, so a file formats to bytes that re-parse to the same tree —
  the test asserts a second run changes nothing. A file that does not parse is
  returned untouched, which is the difference between a formatter and a data
  loss bug on a half-typed save.
- **The layout mirrors Go's, because that is the shape a reader expects.** A
  declaration's names and types align in one column like a struct; an element
  body puts one property per line with the values in one column; a property run
  and the children after it are one blank line apart, and so are two children;
  an element with no members collapses to `Text {}`. Comments are the one thing
  the file still owns: a comment above a member stays cuddled to it, and one
  written after a member stays on that line. The formatter records comments in
  the lexer rather than threading them through the AST, so the parser stays
  ignorant of them and every other consumer is unaffected.
- **Formatting is an LSP method, not an editor setting.** `goipls` answers
  `textDocument/formatting` with one whole-document edit, so any editor with
  format-on-save gets it with no per-editor configuration, and the rules live in
  one place (`internal/goifmt`) shared with the `goifmt` command.
- **Dialogs and notifications are wrapped in `ui`, not left to the caller.**
  File dialogs use `ncruces/zenity`: pure Go, no cgo, native on Windows and
  macOS, and on Linux it drives `zenity`, `matedialog` or `qarma`. Notifications
  use `gen2brain/beeep`, which sends over D-Bus on Linux (falling back to
  `notify-send`), through the notification center on macOS and the toast API on
  Windows. Zenity's own `Notify` blocks until the notification is dismissed,
  which is why notifications do not use it. Both wrappers keep the third-party
  types private: a caller imports `ui`, passes `ui.FileFilter`, and tests
  `errors.Is(err, ui.ErrDialogCanceled)`. No cgo is added, so the Linux
  `-linkmode=internal` rule is unchanged.
- **SVG is rasterized by `srwiley/oksvg` + `rasterx`, inside `ui`.** The two are
  pure Go, so nothing above `internal/platform` gains cgo and the Linux
  `-linkmode=internal` rule is unchanged. `oksvg` exposes
  `ReadReplacingCurrentColor`, which is exactly the design's `color` property:
  one parsed file, a text substitution, then a raster. The rasterizer lives in
  `internal/svg` so `internal/tree` keeps importing only the pixel type from
  `internal/img`; the node itself no longer needs to know how SVG is drawn.
  Rasters are cached by path and colour, so a reload or two nodes sharing an
  icon repeat neither the parse nor the raster. The frame loop compares a
  node's `Color`/`Src` with the values its bitmap was made from and re-rasters
  when Go changes them; that is cheaper than giving `Prop[T]` a change hook.
  A remote `src` reuses the Image fetch rather than growing a second one: the
  download is shared per URL, the node keeps a `Loading`/`Phase` placeholder,
  and the bytes are parsed and rasterized when they land. The fetch function
  was split into "download bytes" and "decode", because SVG needs the bytes
  before it has a pixel type to decode into.
  A colour written from Go drops the role the design named for that cell
  (`Prop.Set` clears it), otherwise `ResolveColors` would rewrite the scheme's
  colour on the next frame and `Color.Set` would look like a no-op.
- **Colour names are resolved in one lookup, so custom colours need no new
  syntax.** `ResolveColors` already rewrote role-valued cells each frame; it now
  asks the scheme first and the window's `SetCustomColor` map second. That keeps
  the design language unchanged (`color: myBrand` is just a bare word), makes a
  later `SetCustomColor` repaint on the next frame without a reload, and puts the
  "reserved name" rule in one place: the setter rejects a name the scheme owns.
  The schema no longer rejects an unknown colour identifier, because it cannot
  see the app's map; the runtime warns once and falls back to `onSurface`, which
  is a theme colour rather than transparent.
- **`ksckaan1/logger` is the one logging dependency, and `ui` owns its default.**
  `ui.SetLogger` lets an app supply its own; otherwise the library's `LOGGER_*`
  environment variables configure it, with `service=goi` as the default name.
  Logging is a side path: a bad config falls back to a disabled logger rather
  than failing the window. `DEBUG_UI` logs every input event except pointer
  motion, which fires at the display rate.
  The library pulls in `mattn/go-isatty` and `rs/zerolog`, and with cgo enabled
  that is enough to push the test binary onto the external linker, where
  goffi's dynamic imports fail. Test commands therefore carry
  `-ldflags=-linkmode=internal` on Linux, the same as builds; Make, CI and
  `AGENTS.md` were updated together so the rule has one shape everywhere.
- **Window chrome is a per-frame comparison, not a startup read.** `titleBar`
  is applied once because changing it means recreating the frame, but
  `minWidth`/`minHeight`/`maxWidth`/`maxHeight`, `disableResize` and `fullscreen`
  are cheap to compare against the last applied state, so the frame loop does
  exactly that. A `Set` from Go or an expression therefore reaches the platform
  without a reload, with no change hook on `Prop[T]`. The size limits are also
  enforced in `platform` rather than trusted to SDL's hints, because a tiling
  compositor may ignore them; the clamped size is written back so the client
  area and the tree never disagree.
- **`minHeight`/`maxHeight` are common properties, matching `minWidth`/`maxWidth`.**
  A height pair that only `Window` accepted would be the one asymmetry in the
  common set; adding it costs two cells and the same `minSize`/`maxSize` branch
  layout already had for width.
- **Every size change goes through one clamp, because Wayland hides the resize.**
  During an interactive resize SDL defers the configure and sends only an
  expose, so the framebuffer read is the path a drag actually takes; it used to
  bypass the limits, which is why `maxWidth` looked ignored. `applyClientSize`
  now clamps both the resize event and the framebuffer read, asks the platform
  for the clamped size when the request was outside it, and only then tells the
  tree. A declared maximum also undoes a maximize, since the compositor owns
  that button and an xdg `max_size` hint is only a hint; fullscreen stays
  exempt so it can fill the display.
- **A `src` says where it lives, so `goi://` marks the asset container.** A bare
  path used to mean the container, which left no way to point at a file the user
  picked or one next to the binary. Now `goi://path` reads the `fs.FS` given to
  `LoadWindow`, `./path` and `path` read the working directory, an absolute path
  reads itself, and `http(s)://` still fetches in the background. The scheme is
  explicit rather than inferred, because `assets/logo.svg` and `./logo.svg` are
  both plausible and a heuristic would silently read the wrong file. The cache
  is keyed by the whole string, so one name under two schemes stays two bitmaps.
- **`Image.SetImage` and `SVG.SetDataString`/`SetDataBytes` are node methods, not
  options on the loader.** A picture that exists in Go (a chart, a screenshot, an
  `//go:embed`) never passes through `src`, so the loader would need a parallel
  path for something the node already stores. `SetImage` converts through the
  same `img.FromImage` the decoder uses; the SVG setters keep the document on
  the node and bump a generation counter, so the frame loop notices a new
  document the way it notices a new colour, and the rasterizer stays in `ui`
  instead of `tree` learning how SVG is drawn. Neither survives a hot reload,
  which is why both are documented as "call it from Bind".
- **`switch` is a block like `if`, not a value expression.** Its arms are built
  with the window and only the chosen one is shown, so a switch costs no layout
  for the arms it hides, and it nests in an `if` or a `for` the way a reader
  expects. The subject and the cases are compared at runtime with the same loose
  equality `==` uses, so `switch bridge.job` follows `SetData` a frame later
  instead of being read once at build. A case may therefore hold an expression
  where Go would require a constant; nothing folds duplicate cases, and only
  `default` is checked for uniqueness because a second one is unreachable.
- **A builtin call is an expression value.** `width: max(120, min(window.width
  / 3, 480))` was documented and evaluated at runtime, but the validator judged a
  call by its syntax kind and called it a handler call, so a legitimately
  responsive design did not pass `goicheck`. A call is now an expression like
  any other, and in a number position only the builtins that return a number
  (`len`, `cap`, `min`, `max`) pass, so `width: formatf(…)` stays an error
  instead of failing silently at runtime.
- **`shadow` is one property with CSS's two models, chosen by the node.** On
  `Text` the layers copy the glyphs, because a box behind a line of text is not
  what anyone means by a text shadow; everywhere else they follow the box. The
  syntax is CSS's — `x y [blur [spread]] [color] [inset]`, comma separated — so
  nothing has to be relearned, and the property rides the common set so every
  element (including a future one) gets it without an entry in `componentdef`.
- **A shadow is the one literal written as juxtaposed tokens, so it ends at the
  line break.** The lexer drops newlines, which is what lets a property and a
  child share a line; a shadow literal has no closing delimiter of its own, so
  the parser reads it until a token starts on a new line (or the arm closes).
  Every other value is one literal or an expression with brackets, so this is the
  only place that needs line info.
- **The box shadow reuses the existing soft-shadow shader; only `inset` is new.**
  The shader already rebuilds a shape from a quad grown by the blur, so spread is
  baked into the quad and the radius and the blur stays the falloff width. Inset
  needed a kind of its own: the band is the distance from a hole shrunk by spread
  and moved by the offset, masked by the shape. A shadow colour resolves through
  the same lookup as a colour property, so a role follows the theme and a
  `ui.SetCustomColor` name works; with no colour it falls back to the scheme's
  shadow role instead of CSS's black, which is why a dark theme does not get a
  black smudge.
- **A text shadow blurs the glyph's coverage on the CPU and caches it in the
  atlas.** Two earlier shapes failed visibly: offset copies of the glyph read as
  a stack of ghost letters (frosted glass), and a fragment-shader blur sampled
  the neighbouring glyph because the atlas packs glyphs edge to edge. A blurred
  glyph is a second atlas entry keyed by its blur radius, with the bitmap grown
  by 1.5×blur so the soft edge is not clipped; a separable Gaussian costs two
  passes and is paid once per (glyph, blur), not per frame. `spread` and `inset`
  have no glyph meaning and are ignored on `Text`.
- **An explicit `shadow` replaces a component's M3 elevation rather than stacking
  on it.** A `Button` already has an elevation, and drawing the design's shadow
  in addition made it look smudged. `shadow: none` counts as explicit, so it is
  how a design removes the elevation, and the painter suppresses the component's
  own shadow for that node only.
- **A box shadow's corner radius comes from the node, or from its component's M3
  shape.** A `Button` is a pill and a `Card` keeps its medium corners, so a shadow
  matches the box it is behind without the design repeating the radius. The small
  table in `paint/shadow.go` has to be kept in step with the painters; a component
  that grows a new default shape belongs there.
- **The window state is a request and a report, so they are kept apart.** Writing
  `minimized`, `maximized` or `fullscreen` asks the window to move; what the
  window actually is comes from `SDL_GetWindowFlags` every frame. Echoing the
  report back into the request prop would fight the compositor — a maximize it
  applies one frame after the request would be reverted by the next frame — so
  the report lives in separate fields, and both `window.*` and the Go getters
  prefer it once the platform has reported. A headless window has no report, so
  the request stands in.
- **`resizable` is the readable spelling of `disableResize`, not a replacement.**
  A design may write either; when they disagree, "off" wins, because asking for
  a resizable window while the old flag says no is the mistake worth forgiving.
  Keeping the old name means no design or Go call needs an edit.
- **`minimizable`/`maximizable` gate the framework's requests only.** SDL has no
  flag to disable one title bar button, and no portable way to ask a compositor
  to grey it out, so claiming the platform button follows the switch would be a
  lie. What they do control — `Window.Minimize`, `Window.ToggleMaximize` and the
  equivalent setters, which is what a custom title bar calls — is the part the
  framework owns.
- **`app.systemLanguage` is the language, not the locale.** `SDL_GetPreferredLocales`
  returns a language and a country; the design question is "which language",
  so only the ISO 639-1 subtag is exposed (`tr-TR` reads `tr`) and it is
  re-read on `SDL_EVENT_LOCALE_CHANGED`.
- **`fontWeight` shares `weight`'s cell, and relative weights are resolved against
  the role.** CSS `bolder`/`lighter` mean nothing without a base, and the base
  here is the type role's weight, so the parser keeps them as sentinels (-1/-2)
  and `Text.WeightFor` steps to the next weight the embedded font actually has.
  One cell keeps layout and paint in agreement and avoids a second property that
  could contradict the first; `TestEveryComponentPropHasItsOwnCell` does not
  apply to a core element, and the pairing is the intentional `spacing`/`gap`
  kind of alias. The formatters print the keyword back (`bold`, not 700) so a
  file survives a round trip unchanged.
- **Italic is synthesized by shearing the outline at raster time.** The embedded
  Roboto has no italic file, and adding one would grow the binary for a style a
  design uses sparingly. The slant is baked into the glyph bitmap and its atlas
  key includes the italic flag, so an italic and an upright glyph of the same
  code point never share coverage. A real italic face, if it is ever embedded,
  replaces this in `text.Raster` and needs no API change.
- **Text decoration is drawn as ops, not baked into the glyph atlas.** The
  strokes sit at fixed offsets from the baseline for the whole run, so they are
  cheaper and crisper as shapes. `wavy` is one `Shape` value (`Wave`) the
  fragment shader draws;
  the other styles are plain fills, with `double` recursing into two thin solid
  lines. The CSS rule that the strokes combine is enforced in the parser (one
  space-separated value) and the schema, not in each painter.
- **`opacity` multiplies alpha per op, there is no offscreen group.** A real group
  opacity needs an offscreen pass and a second read of every pixel, which the
  single-draw-call renderer deliberately avoids. Multiplying each op's alpha is
  exact for a single box and for disjoint children, and only differs where two
  translucent children overlap inside the same faded subtree; the design is
  still honest, and no app has to opt in to a slower path.
- **Scrollbar geometry lives in tree, not paint.** `ScrollbarThumb` is used by
  paint and by hit-testing, so the bar a reader grabs is exactly the bar drawn.
  `ScrollbarVisible` is the one place auto-hide is decided: pointer inside, or a
  scroll within the last `ScrollbarHideDelay`. Input owns the timer that asks for
  the final repaint, because paint may not schedule work.
- **The scrollbar drag maps the thumb to the track 1:1, not the pointer delta.**
  Grabbing a thumb and pulling it to the end of the track is what readers expect,
  and it makes a short thumb usable in a long document; a delta would move the
  content too slowly by the ratio `max/view`.
- **A scroll shadow is per edge, so the top one appears only after scrolling.**
  CSS scroll shadows use the same rule: an edge with nothing hidden behind it
  paints no band. At rest only the bottom band shows; in the middle both do; at
  the end only the top one. `scrollShadowColor` defaults to the scheme shadow at
  45%, and the band is rounded with the node's radius so a card keeps its shape.
- **A percentage on `opacity` resolves against 1, not a parent size.** The
  percentage syntax is shared with width/height, so `ResolveSize` handles the
  third name with a scale of 1; the schema's error message names all three.
- **Scroll chrome uses a border-only viewport, not the content box.** Padding is
  part of what a reader perceives as the moving content's gutter, so the bar
  and the scroll shadows attach to `ScrollViewport` (border box minus border).
  Using `ContentBox` inset them by the padding, which made a padded scroller look
  like it had a floating bar and a shadow band in the middle of the gutter.
  Hit-testing uses the same rect, so the drawn bar and the grab target agree.
- **A scroller clips to its frame, never to its padding.** Reserving any edge of
  the content box in the viewport leaves a permanent empty band there while the
  content scrolls under it: first the bottom, and the same is true of the top,
  left and right. So `ScrollClip` is `ScrollViewport` for a scrolling node and
  `ContentBox` only for one that does not scroll (a Card's padding is a real
  inset for what it draws). Padding still spaces layout and `ScrollMax` still
  counts it, so the gap at the start shows at rest and the gap at the end is
  reached at the end of the scroll, without either becoming a fixed gutter.
- **Only the runtime requires a Window root.** A design split into pages has
  many roots, and a tool that opens one file cannot tell a page from a mistake:
  guessing by looking at the folder's imports was brittle and still wrong for
  files nobody imported yet. So `ui.loadBundle` keeps `ValidateEntry`, and
  `goipls` and `goicheck` validate every file as a fragment and never name the
  root rule. `goicheck` wraps a non-Window root in a Window before laying it out,
  which is what the preview already did for declared fragments.
- **`zIndex` orders siblings only; there are no stacking contexts.** A full CSS
  model needs a context per positioned ancestor and an ordered tree walk with
  negative phases. The common need (a popover over a card, a label over an
  image) is sibling order, so `PaintOrder` sorts the direct children stably by
  `zIndex` and hit-testing walks the same slice backwards. A child can never
  paint over an uncle; the ancestor order still decides that, which keeps the
  walk single-pass and keeps clipping correct. The sort is skipped entirely
  when no sibling sets the property, so the usual frame pays nothing.
- **i18n is a catalog folder, not a binding generator.** A design that needs
  translations drops `i18n/<code>.json` next to its `.goi` files; the folder is
  optional and its absence is not an error, which keeps `t(...)` legal in every
  design and makes the key itself the visible fallback while a translation is
  still missing. Nested objects flatten to dotted keys, the same shape the
  design writes, so no code generation step has to be run or kept in sync.
- **Fallback is resolved per key, and defaults to the desktop language.**
  `SetLanguage` and `SetFallbackLanguage` both start empty and read
  `app.systemLanguage`, so a design follows the desktop until Go overrides it; a
  locale change therefore needs no extra work. Resolving each key separately
  lets a language be complete for one screen and borrow the fallback for another,
  which one global "is this catalog complete" switch could not express.
- **A catalog edit reloads only the strings.** `t(...)` is an expression, so the
  frame loop re-resolves it after `SetI18n`; rebuilding the tree for a text
  change would cost a layout and re-run Bind for no benefit. The watcher now
  accepts `.json` in the design's `i18n` folder, and a malformed catalog is
  reported while the previous strings stay live.
- **The property table is shared per kind, and its entries hold cells, not
  closures.** Every node used to build a `NamedProp` per property out of five
  closures plus a copied name map. A 5000-node design spent most of its heap on
  those: five allocations and ~120 bytes per property, times thirty-odd
  properties per node, plus a string-keyed map per node. The entry now stores
  one type-erased cell pointer and the kind decides how to read it, and the
  name-to-index map is cached per element kind, because a kind's property list
  is identical for every node of that kind. Live memory for a rectangle fell
  from ~7.5 KB to ~4.3 KB and allocations per node from 231 to 23.
- **Wrapped text is cached, not re-wrapped per frame.** The display list is
  rebuilt every frame and layout re-measures paragraphs on a resize; both call
  the same greedy wrap. `paint.List` and `layout.Engine` keep a bounded map from
  (text, column, size, weight) to the break points, so a repaint that changed
  nothing pays nothing. Paint on the gallery went from 11.3µs and 3 KB per frame
  to 6.5µs and zero allocations; a relayout went from 34.5µs to 14.8µs.
- **The glyph atlas starts at 1024².** It grows on demand, and 2048² — 4 MB per
  app — was the worst case for a window that fits comfortably in a quarter of
  it. The per-face advance cache is capped at 2048 entries for the same reason:
  the wrap caches upstream mean a prefix is rarely measured twice, so a larger
  map only held dead strings.
- **The expression cycle check is a slice, not a map.** Expressions re-run every
  frame; the reference chain is a handful deep, so scanning a tree-owned stack
  costs nothing and removed an allocation per evaluated expression. The
  unchanged-value check now compares through the cell instead of boxing the
  current value into `any`.
- **`cursor` takes every CSS name and maps what the platform lacks.** SDL's
  system cursors cover the common shapes plus the eight resize directions, but
  not `zoom-in`, `help`, `copy`, `alias` or `context-menu`. The schema accepts
  the full CSS list anyway, because a design should not have to know which
  shapes the current OS happens to ship, and `cursorFor` maps the missing ones
  to the arrow — the same fallback a browser uses. `none` hides the pointer
  instead of picking a shape, and an explicit `auto` stops inheritance so a
  control can return to the framework's own affordance inside a draggable area.
- **A table sorts by reading its rows, not by reordering them.** `TableRows`
  returns the row nodes sorted by the current column and is the one order
  layout, paint and hit-testing use. Reordering `kids` in place would have been
  one line, but the `for` loop that builds rows replaces those children whenever
  the data changes, so the sort would silently drop on a refresh; deriving the
  order on read cannot go stale. Sorting is O(n log n) per pass over a few
  dozen rows, which is cheaper than the cache invalidation it avoids.
- **Table cells are laid out by the table, not by a generic flow.** A column is
  a declaration, not a box, so `Table` intercepts layout before the component
  path and places the header band and each row itself. Cells are positioned
  inside their row, which keeps their bounds parent-relative like every other
  child; placing them with table coordinates instead was the bug the first test
  caught.
- **Column defaults live in componentdef, not in the node.** The schema seeds
  every component property from the zero value, so `showHeader`, `rowHeight`,
  `headerHeight`, `sortColumn`, `sortAscending` and `selectedRow` declare their
  real defaults in `componentdef.Default`. Without that the builder would seed
  `true` and the defaults pass would immediately write `false`.
- **A table part is clickable without being focusable.** Sortable headers and
  cells are handled by `tableTarget` in the input layer rather than added to
  `interactive`, so they react to the pointer but never enter the Tab order: a
  hundred-row grid must not add a hundred stops.
- **A percentage column width is read before it is resolved.** `ResolveSize`
  only runs for nodes the generic flow places, and a `Table` places its own
  columns, so `width: 25%` would have stayed at `-1` and turned into a flexible
  column. `NodeBase.PercentWidth` exposes the literal fraction, and the table
  takes that share of its content width. Only a direct literal is read; an
  expression has no fraction to report before it runs.
- **A component may narrow the shared variant union.** A `Table` paints
  `outlined`, `filled` and `plain`; accepting every word the union lists made
  `variant: tonal` pass the checker and paint the outlined style, which reads
  as a dead option. `EnumValuesFor` lets the component declare the styles it
  implements, so completion, the checker and hover show only those.
- **An animation is decoded to full frames and swaps pixels in place.** A GIF or
  APNG is composited once, when the file loads, into one RGBA picture per frame,
  so playing a frame costs a pointer swap rather than a decode or a per-frame
  blit. The renderer watches `img.Data.Gen` and rewrites that image's atlas
  region instead of packing each frame beside the last, which would grow GPU
  memory every frame. Each node owns its `img.Data`, because two elements with
  the same `src` must be able to sit on different frames.
- **An animation keeps the frame loop awake.** `needsFrame` is cleared for the
  frame just drawn, so animation alone would have parked the loop after one
  frame; `App.animating` re-arms it while a clock is actually advancing, the same
  way the FPS overlay does.
- **SMIL is replayed by rewriting the document, not by a transform engine.** An
  animated SVG is rasterized by oksvg, which has no animation support, so the
  frame's attribute values are interpolated into a copy of the XML and that copy
  is rasterized. It costs a parse per frame (quantized to 30 Hz) but needs no
  new renderer, and a file with an unsupported construct draws its first frame
  rather than failing.
- **`paused` is a property, not UI state.** As a Go field it could not be set
  from a design; as a Prop both the design and Go drive it, and a finite
  animation that played its passes uses a separate `AnimDone` flag so `Paused`
  keeps meaning "the app asked for this".
- **An event may call an element by id.** A design that wants a play button for
  a spinner would otherwise need a bridge handler and Go glue for three one-line
  methods. `onClick: spin.pause()` is parsed as a call whose callee is the id,
  validated against `schema.NodeMethods`, and dispatched through a `Transport`
  interface on the node, so the checker and the tree agree on the same three
  names. It stays a closed set: only `play`, `pause` and `stop` are allowed,
  and only on an `Image` or `SVG`.
- **An animation frame rewrites its atlas rect; it never packs a new one.** The
  first version assigned a fresh `*img.Data` per SVG frame, and the renderer keys
  packed regions by that pointer: every frame added a region, the atlas grew to
  its cap, then the downscale fallback shrank images until nothing was left —
  the spinner appeared to fade out of existence. A frame now copies its pixels
  into the node's existing bitmap and bumps `Data.Gen`, which the renderer reads
  as "refresh this rect". `TestAnimatedSVGKeepsOneBitmap` pins it.
- **The loop sleeps until the next animation frame.** Painting at the display
  rate for an 8 fps GIF wastes a full display list and a full 16 MiB atlas
  upload per refresh. The animation tick now reports whether a frame actually
  changed and how long until the next one is due; the run loop waits that long
  with `WaitEventsTimeout`, so a 8 fps clip costs 8 frames and a 30 Hz SMIL clip
  30, while input still wakes the loop immediately. Writing just the changed
  atlas rect instead of the whole texture would be the next saving, but the
  gogpu 0.34 staging path does not land a sub-rect copy — it froze the picture.
- **One walk advances every clip, and hidden subtrees are skipped.** GIF/APNG
  and SVG advance in the same pass over `Tree.WalkVisible`, so a design with
  five clips pays one walk, not two; a clip on a `visible: false` page neither
  advances nor keeps the frame loop awake.
- **A table cell holds components; `value` is only the sort key.** `TableCell`
  used to draw a single `text` string, which made anything richer (a chip, a
  badge, an icon row) impossible without a second table. The cell is now a
  container: its children are what the row shows, laid out as one aligned run
  inside the 16dp inset, and `value` is the string sorting and `CellValue` read.
  Keeping them separate is the point — a cell can draw "8 Nov 2026" and sort by
  `2026-11-08`. `text` is gone rather than kept as a fallback, so a stale design
  fails the checker instead of silently losing its content.
- **A `value` means two types, so the schema asks per component.** `TypeOf` was
  a name-keyed table and `value` is a number on a Slider. `TypeOfFor(kind, name)`
  lets a TableCell's `value` be a string without turning the slider's into one.
- **Emoji fall back per rune inside the shaper, not per text node.** A line that
  mixes "Hi 😀 there" must wrap, centre and place a caret as one string, so the
  shape step splits the line at font boundaries and returns runs with the face
  that drew each. `Face.Shape` folds those runs back into one advance, so layout
  never sees the split; `ShapeRuns` is what the renderer uses to know which
  glyphs are colour bitmaps. Noto Color Emoji is embedded (11 MB) because a UI
  framework that cannot draw an emoji is missing a basic glyph.
- **A colour glyph draws from the image atlas.** The coverage atlas is 8-bit
  alpha, and an emoji is a full-colour bitmap (CBDT/PNG in the font). Emoji quads
  are emitted as ordinary bitmap instances instead, keyed by size and glyph, so
  no second pipeline or shader branch is needed.
- **A cell's `value` is a scalar, not a string.** A design writes `value: 42` for
  a count; forcing quotes made the sort key a formatting question. `TypeScalar`
  takes a string, number or bool, and the tree stores a number's exact decimal
  text so `7.5` does not become `7.500000`. Two numeric values compare as
  numbers even in a text column, which is what a reader expects of a count.
- **An exported SVG keeps its colours in a stylesheet.** `loading.svg` had
  `.shape { fill: var(--indicator) }`, which oksvg ignores, so the shape drew
  nothing; its `d` animation also morphs a path, which the first blend could not
  express. The SVG package now folds class rules and `:root` custom properties
  into presentation attributes before rasterizing, and interpolates path data
  when both keyframes share a command sequence.
- **One colour parser, shared by the checker and the tree.** CSS syntax is not
  worth implementing twice: `internal/csscolor` reads hex, the 148 names,
  `rgb()`/`rgba()` in both spellings, `hsl()`/`hsla()` and gradients, and both
  the validator and the property setter call it. A bare identifier is tried as a
  CSS keyword first and only then as a theme role, so `white` is a literal while
  `onSurface` still follows the scheme.
- **A gradient is a value of its own, not a colour.** The tree keeps it beside
  the colour cell, because a gradient has no single RGBA and the expression layer
  reads a `uint32`. Paint emits a `CSSGradient` op and the renderer puts the stops
  in a storage buffer: a stop list does not fit an instance, and a second texture
  would need an upload per gradient. The shader walks four stops, so a longer
  gradient is resampled on the CPU — invisible at the size a box fill is read at.
- **A gradient only fills `background`.** `color` and `borderColor` are strokes
  and glyph colours, where a box gradient has no meaning; the checker reports it
  rather than storing a value paint would ignore.
- **A colour reference is a literal for its cell, not an expression.** `theme.x`
  and `customColor.y` parse as references, which the expression pass would
  evaluate to nil and write as zero. They are stored in the colour-role list
  instead, tagged with their namespace, so the resolver knows which side to ask
  and neither can shadow the other. The bare spellings still work, and the
  prefixed form is what completion offers.
- **`rotate` rides the instance, not a separate pass.** A turn is one angle and
  one pivot per op; the vertex shader rotates the quad about the pivot and leaves
  local coordinates unrotated, so every distance field still measures in the
  element's own frame and a rounded, bordered, gradient-filled box turns
  correctly. Nested turns compose on the CPU: the inner pivot moves into the
  outer frame and the angles add, which is what a rigid transform does.
- **`rotate` takes a dedicated parser.** The CSS grammar (`none`, an angle with
  any unit, `x|y|z` plus angle, or three numbers plus angle) is not an
  expression, and squeezing it into one would have made `90deg` a lexer special
  case. `parseRotate` runs before the generic value reader and stores degrees, so
  the tree, formatter and renderer agree on one unit.
- **A clicked link goes through the OS opener, not the app.** A link target is a
  URL, a deep link or a path, and the system already knows what to do with all
  three: `xdg-open`/`open`/`rundll32` reach the registered handler for a scheme
  and the file manager for a folder. goi does not parse URLs to guess. The
  decision that stays in the framework is "URL or path": a target with a scheme
  is never `stat`ed, so `https://goi.dev/a` cannot be mistaken for a file, and a
  path that exists is revealed rather than opened by its owning program. Finder
  and Explorer select the item; the freedesktop opener has no such call without
  the `org.freedesktop.FileManager1` interface, so Linux shows the folder.
  `Text.OnLink` is the escape hatch for a target the app owns, and goi opens
  nothing when it is set.
- **A link is a run, so its hit test recomputes the wrap.** Paint does not write
  geometry back onto nodes, and a paragraph's click needs the same lines the
  painter drew. `RichText.Lines` is the one wrap function layout, paint and
  `richLinkAt` call, each with its own measure callback, so the box a click lands
  in is the box that was drawn. The cost is one wrap per pointer move over a
  paragraph that has a link; `HasLink` makes that cost zero for the ones that do
  not.
- **Chart data is a child element, so a chart needs no Go.** A design had no way
  to hold a list of numbers, so every chart was filled from code. `ChartSeries`
  is the list: `categories` carries the labels and one child per series carries
  `values`, a comma-separated string, because the language has no list literal
  and inventing one for charts alone would be a parser feature with a narrower
  use than the element. The child is data, not a node — the builder turns it into
  `Series` and detaches it, exactly as a RichText's runs are turned into spans —
  so layout, paint and id lookup never see it. A number that does not parse is a
  diagnostic from the checker, not a bar of zero: the string type would otherwise
  hide the mistake.
- **A chart's colour role is resolved in paint, not copied at build.** `Series`
  gains `ColorRole` for the same reason `Span` did: the child is gone when the
  series is drawn, and a colour copied then would not follow a theme change. The
  painter asks the scheme, so `color: tertiary` on a series reads like any other
  role.
- **A chart's colours are two properties and a list, not five.** Every label a
  chart writes shares one role, so `textColor` covers the ticks, categories,
  values, legend and the gauge's number; the lines and rings share `gridColor`.
  Splitting them per part would be five cells a design almost never sets
  differently. Both are ordinary colour cells, so a role follows the scheme
  through the tree's own colour pass. A per-slice list is a string of colours and
  roles (`colors: "primary, #c62828"`), parsed by the checker and stored as a
  literal slice beside a role slice, because the colour pass resolves one cell
  and a list has no cell of its own; the painter asks the scheme per entry.
- **A list is a value, not a string of commas.** Chart data first took
  `values: "12, 18, 9"`, which made the checker parse a string to find a typo and
  made a colour list the one place a role could not be written the way the rest
  of the language writes it. `[42, 27, 18]` gives the parser the items: the
  printer keeps them on one line, the checker reads each one by the property's
  own element type, and a colour list takes a `theme.` reference like any other
  colour. The parser has no other list to spend, so it is one more value kind and
  nothing else changes.
- **A role in a list keeps its namespace.** A single colour property accepts
  `primary` and `theme.primary`, because one value leaves no doubt. In a list a
  bare word is a CSS colour name (`red`, `rebeccapurple`), so `[primary]` would
  be ambiguous; the checker rejects it with the form to write instead of guessing
  between two meanings.
- **A list item may be an expression, so the cell is re-read rather than
  written once.** `colors: [bridge.brand, #c62828]` names a value the app sets,
  and a list is only as static as its items: one item that is an expression makes
  the whole property an expression, which the frame loop resolves and the chart
  rebuilds its data from. A list of literals never enters that path, so the
  common case costs what it did. An unchanged rebuild is compared and dropped, so
  a per-frame pass does not relayout a chart that did not move.
- **A chart keeps its series children after the build.** They were detached,
  which is what makes them data rather than nodes, but a cell that reads a bridge
  variable has to be readable later. `Chart.sources` holds them, and
  `syncData` is the one reader, so the build path and the frame path produce the
  same data. `SetData` still replaces the copy for an app whose numbers come from
  Go.
- **A role in a list is resolved by the colour pass, not by paint.** Paint has
  the scheme and can resolve `theme.`, but a custom colour lives on the window, so
  the pass that already resolves every other role writes the list's roles into
  the series as well. That is what makes `customColor.brand` in a slice list
  follow `SetCustomColor` like a colour property does.
- **A video's bar defaults to white, an audio's to the scheme.** The bar is drawn
  over the picture with a dark scrim in both schemes, and the light theme's
  `onSurface` is near-black: white is the only default that reads on any frame.
  An audio player's bar is a surface of its own, so it keeps the scheme's ink.
- **A multi-line string is `!"…"!`, dedented by the closing line's indent.** A
  document in a `.goi` file needs real line breaks, and escapes make a paragraph
  unreadable. The delimiters cannot collide with a one-line string, and the
  dedent rule uses the whitespace the closing delimiter's line starts with,
  rather than the smallest indent of the block: the first line follows `!"` on
  the property's own line, so it has no indent to take part in a minimum, and a
  line indented deeper keeps its extra indent, which is what a nested markdown
  list is. Nothing is unescaped in the form, because a document is not source
  code; goifmt writes any value with a line break back this way and puts its own
  indentation in front of each line, so a re-read strips exactly that.
- **A block string lines up under its own first character.** Writing the
  continuation lines one level under the property put them left of the first line
  whenever the property name is short (`text: !"#` starts eight columns after the
  indentation, a tab-indented body at four), which reads as a hanging block rather
  than one document. goifmt pads each continuation line with the indentation plus
  the value column plus the two characters of `!"`, so every line starts where
  the first line's text does. The alignment is plain spaces after the tabs, which
  matters because the reader strips the closing line's indentation and only
  whitespace can be stripped.
- **A wavy progress is a ribbon, and the layout knows it.** `variant: wavy` draws
  the active part as a sine sampled into the same rounded segments the charts use,
  because the renderer has no path op: a ribbon is a polyline with round caps, and
  a ring's ribbon is the same polyline bent around the centre, its radius pulsing
  with the angle. The bar needs height the flat 4dp line did not, so
  `m3.MetricsFor` reads the variant and reserves 16dp; a design that pins its own
  height keeps it, and the wave scales down to fit rather than drawing outside the
  box. The stop indicator dot is drawn in both variants: it is what says where a
  determinate bar's scale ends.
- **A progress stroke is a property, and a wavy one moves with the window's
  clock.** `thickness` sizes the stroke and the layout reads it, because a
  thicker line — and a ribbon, which is 1.75 times its stroke — is a taller box,
  not just a different paint. A box the design pins smaller wins, so the wave
  scales down instead of drawing outside the space layout gave it.
  The wave's phase comes from `List.Clock`, which the driver advances with the
  frames; paint reports that it drew something that moves, and the loop asks for
  the next frame from that rather than from a walk over the tree. A hidden wavy
  bar paints nothing, so an idle page still sleeps — the animation costs what it
  shows.
- **The track is the part that is left, not a line under the progress.** A
  progress bar that painted a full track first and the value over it showed the
  light colour through the gap between the rounded ends of the filled part, and a
  ring showed it under the arc. Both now draw the track only where the value is
  not, with a gap the width of the stroke at each end, so the two never touch and
  nothing runs under the arc. An arc's sweep is a straight radial cut in the
  shader, so its soft ends are round caps drawn at the band's centreline; without
  them a ring end read as a corner. The stop indicator keeps its own small gap
  from the track's end.
- **Multisampling is a frame target, not a shader trick.** Every shape is a quad
  with a signed distance, so its own edge is already faded over a pixel; MSAA
  takes the last stair-step off the corners the distance cannot round, such as
  the join of two quads. Four samples is the default because WebGPU guarantees it
  for a renderable format, and a count the device refuses falls back to four and
  then to none. A blurred backdrop's offscreen scene stays single-sample: it is
  blurred before it is sampled, and a multi-pass target that resolves between
  passes is where the backend proved fragile.
- **A curve is sampled for the pixels it lands on.** The step is a device-pixel
  length, not a count: two device pixels of chord for a spline and two of arc for
  a ribbon, with a floor so a short span still bends smoothly and a cap so one
  huge chart cannot flood the display list. paint learns the device scale from
  the driver, the way it learns the clock, so the same design is smooth at 1x and
  2x instead of being twice as coarse on the sharper panel.
- **An over-large radius is clamped where the shape is built.** CSS treats a
  radius past half the box as a circle; the distance field does not, and returned
  "outside" for every fragment. Both the op and the shader clamp, so a design's
  `radius: 999` circle draws and a tool reading the list sees the real radius.
- **An upright shape snaps to the pixel grid; a turned one does not.** Snapping
  is what keeps a border crisp, but a chart draws a curve as dozens of small
  turned quads whose joins live at subpixels. Snapping each one rounded its
  rectangle back to whole pixels and the spline read as a staircase, so a
  rotated fill keeps its exact box and the distance field fades its own edge.
- **`size` is a shorthand, not a cell.** One number writes width and height,
  because most boxes are square and two lines for one decision reads worse. It
  owns no cell: writing it writes both axes, reading it gives the width, and a
  later `width` or `height` moves only the axis it names. It replaced an enum of
  the same name (sm/md/lg) that only a few components' metrics read and that
  everybody else ignored, so the language has one `size` and it means a number.
- **The loading indicator is a polar signed distance, not a bitmap.** The M3
  shape turns while morphing from its twelve-lobed star to a rounded square,
  squeezes into a pill and comes back; drawing it from a distance field keeps it
  sharp at any size and display scale, needs one quad a frame and no atlas, and
  lets the design tint it with `color`. The gallery's SVG was the reference for
  the motion, not the implementation: morphing 200 lines of path per frame would
  have cost a rasterization a frame and blurred at large sizes. The radius is a
  blend of a cosine star and a superellipse, because a star blended with a circle
  cannot give the square's flat sides, and a four-lobed cosine reads as a clover
  rather than a square.
- **A colour variable always names its namespace.** A shadow colour and a
  gradient stop have no property type to tell a role from a literal, so
  `theme.primary` and `customColor.brand` are the only spellings for a variable;
  a bare word is a CSS colour name. That is also why a custom colour may reuse a
  scheme role's name: `theme.primary` and `customColor.primary` are two
  different values in different namespaces, so nothing has to be reserved.
- **A gradient keeps its unresolved stops on the node.** A stop that names a
  colour stores the namespace and name beside the pixel value, and the same
  resolver that rewrites colour cells rewrites those stops, so a theme switch or
  a changed custom colour recolours a gradient without re-parsing its string.
- **A slot is written once, and its fill carries elements only.** The same passed
  children cannot land in two places in one tree, and a passed child with an `id`
  would be declared twice, so a repeated `$name` is an error. A fill is a wrapper
  with children; a property on it, or a bare marker at a call with no fragment
  above it, would pass nothing, so both are diagnostics instead of silent holes.
  A bare slot inside a nested fragment call forwards the enclosing fragment's
  slot under the same name; `$name { … }` there is the inner call's own argument
  and is left alone.
- **A resolved element is never resolved again.** Expansion used to walk the
  whole body with the fragment's import aliases, including the children the
  caller passed in, so a child named like one of those aliases (a `Card`
  component beside `import Card "./Card.goi"`) was read as a second fragment
  call. Elements now carry a `Resolved` mark once the loader has expanded them,
  and later passes copy them untouched, which is what "a child resolves its
  imports in the file it was written in" needs.
- **A component's box can be the design's.** Button and TextField draw their
  fill, border and radius from the variant by default, but when the design sets
  `background`, a border, a radius or a `color`, those win: the variant is a
  starting point, not a wall. A design border also replaces the focus colour,
  because the design chose the look; a filled field with a design border or
  radius drops the M3 bottom indicator, which would otherwise cut across the
  rounded corners.
- **A gesture is a press, not a click.** GestureDetector arms on the press and
  turns it into a long press (500ms inside an 8dp slop), a pan (leaving the
  slop), or a tap; a pan carries only deltas so a handler adds them, and a
  long press or a double tap suppresses the click that would otherwise run the
  same action twice. A pan owns the gesture, so a scroll area under the
  detector does not also scroll.
- **A progress ring centres its children.** The ring is a container now, and a
  child keeps its own size in the middle instead of filling the box, because the
  usual child is a percentage or an icon that would look wrong stretched to the
  stroke. A child that declares 100% still fills.
- **A player's chrome is the design's to paint.** `surfaceColor` is the bar's
  background and, on a video, the loading area, used exactly as written so an
  `#rrggbbaa` value keeps its own translucency; unset keeps the theme's scrim or
  container. The spinner's small plate stays the scheme scrim rather than the
  surface, because its job is to keep the ring readable on any picture.
- **One `variant` styles a player's scrubber and its loading ring.** They are the
  same design decision — plain or a ribbon — so one property carries both, and
  the same for `color`: the played part, its handle and the ring are one accent.
  Splitting them would double the vocabulary for no gain.
- **A wavy seekbar waves only what has played, and only while it plays.** The
  ribbon is the part already heard or watched; the part still ahead stays a dim
  flat track, and a paused or stopped player falls back to the normal bar. The
  wave travels while playing, because the bar is showing a process that is still
  running. The ribbon stands alone — no dim fill runs under it — and in every
  state, playing or not, the dark part stops at the handle and the track starts
  after a 6dp gap, which is the slider's own rule and the wavy progress's. The
  handle is the slider's slim pill rather than a dot, and dragging it updates the
  position on the pointer with the decoder seeked once on release, so a drag does
  not reopen the file at every step. Its hit strip grows to the wave's height so
  the crests are as pressable as the flat track, and the flat bar inside that
  strip stays 4dp tall.
- **A loading video turns a ring in the middle.** The placeholder wash alone read
  as a still frame, especially on a live stream that stalls between segments, so
  the video draws an indeterminate circular-progress arc (270°, one turn per
  1.1s) over the picture while the probe or the decoder is waiting. It is white,
  like the rest of the video chrome, and asks for frames so it keeps turning
  when no media frame arrives.
- **A time picker is 12-hour with AM/PM, and its two variants share one model.**
  The old 24-hour two-ring dial could not show AM/PM, which is what the M3
  picker's header is for, and a "dial" and an "input" that behave differently
  would need two sets of state. Both variants read and write one 24-hour
  `HH:MM`; the dial shows a single 12-hour ring and the switch moves the value
  by twelve hours. The corner icon flips the variant, so a design can start on
  either. Cancel and OK are events (`onCancel`, `onSubmit`) rather than
  behaviour, because whether a picker is a dialog is the app's decision.
- **A picker's variant is a layout change, and its typing needs the platform's
  text input.** The two variants have different sizes, so a shared variant cell
  marked for repaint left the card at its old height when the icon switched it;
  a TimePicker's cell is built as layout dirt. The input variant also takes
  digits from the platform's text path, so a focused picker turns the text input
  on anchored to its active field, the same way a text field does — otherwise a
  browser types into nothing and SDL never starts text input.
- **A time picker's hour format is a display choice, not a value.** The stored
  time stays 24-hour `HH:MM`, so `hourFormat: 24` only hides the AM/PM switch,
  changes the spelling and gives the dial its second ring. The chip on the action
  row is both the label of the current clock and its switch, which keeps the
  visual toggle out of the header the design is using for the time itself.
