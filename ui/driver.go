package ui

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/paint"
	"github.com/goi-gui/goi/internal/platform"
	"github.com/goi-gui/goi/internal/render"
	"github.com/goi-gui/goi/internal/tree"
)

// gpuDriver is the SDL3 + WebGPU event loop. It runs on the main goroutine.
type gpuDriver struct {
	app    *App
	win    *platform.Window
	gpu    render.Renderer
	list   paint.List
	engine *layout.Engine
	input  *interactor

	stopped atomic.Bool
	closing bool

	pendingW, pendingH int
	pendingScale       float32
	resized            bool

	// lastTick drives the animations (ripples), which run on wall-clock time.
	lastTick time.Time

	// Frame rate for the overlay: only drawn frames count, so the number is the
	// paint rate and not the loop's wakeups.
	fps    int
	frames int
	fpsAt  time.Time
	// frameAt is the deadline of the next paced frame, from App.MaxFPS.
	frameAt time.Time
}

func (a *App) initPlatform() error {
	if err := platform.Init(); err != nil {
		return fmt.Errorf("ui: %w", err)
	}
	return nil
}

// initWindow creates the native window, the renderer and the input handler.
func (a *App) initWindow(w *Window) error {
	// A custom title bar means the platform draws no decorations of its own.
	pw, err := platform.New(w.title, w.width, w.height, w.tree.Root.TitleBar.Get() != "custom")
	if err != nil {
		return fmt.Errorf("ui: create window: %w", err)
	}
	display, handle, kind, err := pw.SurfaceHandles()
	if err != nil {
		pw.Destroy()
		return fmt.Errorf("ui: %w", err)
	}
	gpu, err := render.NewGPU(display, handle, render.TargetKind(kind))
	if err != nil {
		pw.Destroy()
		return fmt.Errorf("ui: %w", err)
	}
	if !a.vsync {
		// The surface is configured on the first frame, so this only has to set
		// the mode before that happens.
		if err := gpu.SetVSync(false); err != nil {
			a.reportError(err)
		}
	}
	// Families the app loaded before this window existed.
	if lib, ok := render.FontLibrary(gpu); ok {
		for name, data := range a.fonts {
			if err := lib.RegisterFamily(name, data); err != nil {
				a.reportError(err)
			}
		}
	}
	meas := textMeasurer{lib: gpu.Text()}
	d := &gpuDriver{
		app:    a,
		win:    pw,
		gpu:    gpu,
		engine: layout.New(meas),
	}
	d.list.Measure = func(s string, size float64, weight int) float64 {
		w, _ := meas.MeasureWeight(s, size, weight)
		return w
	}
	d.list.MeasureFamily = func(s string, size float64, weight int, family string) float64 {
		w, _ := meas.MeasureFamily(s, size, weight, family)
		return w
	}
	d.list.Theme = themeFor(w)
	d.lastTick = time.Now()
	d.fpsAt = time.Now()
	d.input = &interactor{app: a, tree: w.tree, measure: d.list.Measure, setCursor: pw.SetCursor}
	// A design that draws its own title bar also owns the window edges, so the
	// platform asks us what each press means: drag, resize or a normal click.
	if w.tree.Root.TitleBar.Get() == "custom" {
		d.input.resizeEdges = true
		pw.SetHitTest(d.input.chromeHitTest)
	}
	pw.SetInput(d.input)
	a.driver = d
	// The window properties must hold from the first frame: a design that asks
	// for a minimum larger than its start size is resized here, before layout.
	a.chrome = chromeOf(w)
	a.applyChrome(pw, a.chrome)
	// The language is a startup read; a locale change event updates it.
	w.tree.SetSystemLanguage(systemLanguage())

	fw, fh := pw.FramebufferSize()
	d.pendingW, d.pendingH, d.pendingScale = fw, fh, pw.Scale()
	d.resized = true
	// The first frame must lay out even though nothing has changed yet.
	w.tree.Root.Base().MarkDirty(tree.DirtyLayout)
	a.needsFrame = true
	return nil
}

func (d *gpuDriver) run(frame func()) error {
	defer d.close()
	for {
		if d.stopped.Load() || d.win.ShouldClose() {
			return nil
		}
		d.win.PollEvents()
		start := time.Now()
		frame()
		// Only block when there is nothing left to draw: that is what keeps an
		// idle app at ~0% CPU. A pending animation sleep is bounded by its next
		// frame, so the loop wakes once per frame the clip actually shows
		// rather than once per display refresh.
		if !d.app.needsFrame {
			if w := d.app.animWait; w > 0 {
				d.win.WaitEventsTimeout(w)
			} else {
				d.win.WaitEvents()
			}
			d.frameAt = time.Time{}
			continue
		}
		d.pace(start)
	}
}

// pace holds the loop to App.MaxFPS. The deadline is absolute, so a late frame
// is caught up on the next one instead of adding its lateness to every frame.
// With vsync on, a period shorter than the refresh never sleeps: acquire is
// already the pacer.
func (d *gpuDriver) pace(start time.Time) {
	limit := d.app.maxFPS
	if limit <= 0 {
		return
	}
	period := time.Second / time.Duration(limit)
	if d.frameAt.IsZero() || start.Sub(d.frameAt) > 4*period {
		d.frameAt = start
	}
	d.frameAt = d.frameAt.Add(period)
	if wait := time.Until(d.frameAt); wait > 0 {
		time.Sleep(wait)
	}
}

func (d *gpuDriver) wake() { platform.PostEmptyEvent() }

func (d *gpuDriver) quit() {
	d.stopped.Store(true)
	platform.PostEmptyEvent()
}

func (d *gpuDriver) close() {
	if d.closing {
		return
	}
	d.closing = true
	if d.app.watcher != nil {
		d.app.watcher.Close()
		d.app.watcher = nil
	}
	if d.gpu != nil {
		d.gpu.Release()
		d.gpu = nil
	}
	if d.win != nil {
		d.win.Destroy()
		d.win = nil
	}
	platform.Terminate()
}

// frame is one loop iteration: posted work, then layout and paint only when
// something is dirty, then a single draw.
func (a *App) frame() {
	a.drainPosted()
	d, ok := a.driver.(*gpuDriver)
	if !ok || d == nil || a.win == nil {
		return
	}

	if d.resized {
		d.resized = false
		if err := d.gpu.Resize(d.pendingW, d.pendingH, d.pendingScale); err != nil {
			a.reportError(err)
			return
		}
		a.win.width = d.pendingW
		a.win.height = d.pendingH
	}

	// Advance animations before the frame check so they can ask for a frame.
	now := time.Now()
	dt := now.Sub(d.lastTick).Seconds()
	d.lastTick = now
	if dt > 0.1 {
		dt = 0.1 // don't jump after a long idle
	}
	if d.input != nil && d.input.tick(dt) {
		a.needsFrame = true
	}
	if a.tickImageLoads(dt) {
		a.needsFrame = true
	}
	// One walk advances both kinds of clip; a design with none pays a flag read.
	// Only a frame that actually changed asks for a paint, and the wait tells
	// the loop how long it may sleep before the next one is due.
	changed, animWait := a.tickAnimations(dt)
	a.animWait = animWait
	if changed {
		a.needsFrame = true
	}
	// A playing video advances its clock, takes the newest decoded frame and
	// asks for a paint at its own rate, which is what keeps 30 fps video from
	// driving the window at 30 fps when nothing else changed.
	mediaChanged, mediaWait := a.tickMedia(dt)
	if mediaChanged {
		a.needsFrame = true
	}
	if mediaWait > 0 && (a.animWait == 0 || mediaWait < a.animWait) {
		a.animWait = mediaWait
	}
	if a.tickTweens() {
		a.needsFrame = true
	}

	// Responsive properties follow the window width. Applying them marks the
	// tree dirty when a value changes, so this runs before the frame check.
	logicalW := float64(d.pendingW) / float64(maxScale(d.pendingScale))
	logicalH := float64(d.pendingH) / float64(maxScale(d.pendingScale))
	a.win.tree.SetViewport(logicalW, logicalH)
	a.win.tree.ApplyBreakpoints(logicalW)
	// The desktop preference is read before anything resolves, so a design that
	// follows it with `theme: app.systemMode` switches on this frame.
	a.win.tree.SetSystemMode(systemMode())
	// `app.mode` and `app.dark` have to be settled before expressions run,
	// because a design's conditional may read them. They come from the resolved
	// scheme only, so they need no expression pass first.
	a.win.tree.SetMode(modeFor(themeFor(a.win)))
	// Expressions run before the theme resolver: a conditional can pick a role,
	// and the role then resolves in the same frame. They also run before the
	// frame check, because a Go property change may be what an expression reads.
	a.win.tree.ApplyExpressions()
	// Role colors follow the scheme and custom colours follow SetCustomColor, so
	// a theme change needs no per-component work and a name declared in .goi
	// never goes stale. The theme is resolved again: an expression may have just
	// changed it.
	if a.resolveColors() {
		a.needsFrame = true
	}
	// A role may just have given an SVG its colour, and Go may have changed it
	// between frames. This runs before layout, so a changed src can still size.
	a.syncSVGs()
	// An Image src written as an expression, or a row built by a loop, only has
	// its value once the expression pass above has run.
	a.syncImages()
	// A Markdown's children are built from its source, so a text change from Go
	// or an expression has to land before layout measures them.
	if a.syncMarkdown() {
		a.needsFrame = true
	}
	// A player follows its design: a changed src restarts the decoder, a muted
	// switch silences it, and autoplay starts on the frame the node appears.
	if a.syncMedia() {
		a.needsFrame = true
	}
	// A fullscreen video takes the window with it, whether the design wrote the
	// property or a button called the method.
	a.syncVideoFullscreen()
	// The window properties can change from Go or an expression, and the
	// platform has to hear about them before the frame is measured.
	a.syncChrome()

	if !a.needsFrame {
		return
	}
	a.needsFrame = false

	root := a.win.tree.Root
	kind, dirty := root.Base().Dirty()
	if dirty && kind == tree.DirtyLayout {
		d.engine.Layout(root, float64(d.pendingW)/float64(maxScale(d.pendingScale)), float64(d.pendingH)/float64(maxScale(d.pendingScale)))
		// A relayout moves children, so everything must be repainted.
		markAll(root.Base(), tree.DirtyPaint)
	}
	// Half a second is long enough to smooth frame-time jitter and short enough
	// that a stutter shows up while it is still happening.
	now = time.Now()
	d.frames++
	if span := now.Sub(d.fpsAt); span >= 500*time.Millisecond {
		d.fps = int(float64(d.frames)/span.Seconds() + 0.5)
		d.frames, d.fpsAt = 0, now
	}
	d.list.ShowFPS = a.showFPS
	d.list.FPS = d.fps
	if a.showFPS {
		// The overlay measures paint rate, so it keeps painting to have
		// something to measure. Only a debug build pays this.
		a.needsFrame = true
	}
	d.list.Theme = themeFor(a.win)
	d.list.Build(a.win.tree, float64(d.pendingW)/float64(maxScale(d.pendingScale)), float64(d.pendingH)/float64(maxScale(d.pendingScale)))
	if err := d.gpu.Render(&d.list, d.pendingW, d.pendingH); err != nil {
		a.reportError(err)
	}
	clearAll(root.Base())
}

// reportError prints a rendering problem instead of killing the app: a lost
// surface or a transient validation error should not end the session.
func (a *App) reportError(err error) {
	logg().Error(logCtx, "ui: runtime error", "error", err)
	if a.OnError != nil {
		a.OnError(err)
	}
}

func markAll(b *tree.NodeBase, kind tree.DirtyKind) {
	for _, k := range b.Kids() {
		k.MarkDirty(kind)
		markAll(k, kind)
	}
}

func clearAll(b *tree.NodeBase) {
	b.ClearDirty()
	for _, k := range b.Kids() {
		clearAll(k)
	}
}

func maxScale(s float32) float32 {
	if s <= 0 {
		return 1
	}
	return s
}

func (d *gpuDriver) windowLogicalW() float64 {
	return float64(d.pendingW) / float64(maxScale(d.pendingScale))
}

func (d *gpuDriver) windowLogicalH() float64 {
	return float64(d.pendingH) / float64(maxScale(d.pendingScale))
}

// modeFor maps a resolved scheme to its light/dark mood.
func modeFor(t *m3.Theme) tree.Mode {
	if t.Dark {
		return tree.ModeDark
	}
	return tree.ModeLight
}

// systemLanguage reads the desktop's preferred language code. It runs on the UI
// goroutine, which is where SDL may be called.
func systemLanguage() string {
	return platform.SystemLanguage()
}

// systemMode reads the desktop's preference. It runs on the UI goroutine, which
// is where SDL may be called.
func systemMode() tree.Mode {
	switch platform.SystemTheme() {
	case platform.ThemeDark:
		return tree.ModeDark
	case platform.ThemeLight:
		return tree.ModeLight
	}
	return tree.ModeUnknown
}

// themeFor resolves the window's theme name against the app's registry.
func themeFor(w *Window) *m3.Theme {
	if w == nil || w.tree == nil || w.tree.Root == nil {
		return m3.Light()
	}
	name := w.tree.Root.Theme.Get()
	if w.app != nil {
		if t, ok := w.app.themes[name]; ok {
			return t
		}
	}
	if name == "dark" {
		return m3.Dark()
	}
	return m3.Light()
}
