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
  import stays `ui` (`import "github.com/ksckaan1/goi/ui"`): it names the
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
