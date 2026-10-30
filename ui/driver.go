package ui

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ksckaan1/goui/internal/layout"
	"github.com/ksckaan1/goui/internal/m3"
	"github.com/ksckaan1/goui/internal/paint"
	"github.com/ksckaan1/goui/internal/platform"
	"github.com/ksckaan1/goui/internal/render"
	"github.com/ksckaan1/goui/internal/tree"
)

// gpuDriver is the GLFW + WebGPU event loop. It runs on the main goroutine.
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
}

func (a *App) initPlatform() error {
	if err := platform.Init(); err != nil {
		return fmt.Errorf("ui: %w", err)
	}
	return nil
}

// initWindow creates the native window, the renderer and the input handler.
func (a *App) initWindow(w *Window) error {
	pw, err := platform.New(w.title, w.width, w.height)
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
	d.list.Theme = themeFor(w)
	d.lastTick = time.Now()
	d.fpsAt = time.Now()
	d.input = &interactor{app: a, tree: w.tree, measure: d.list.Measure, setCursor: pw.SetCursor}
	pw.SetInput(d.input)
	// IME composition needs the input sink above, so it is wired second.
	pw.InitTextInput()
	a.driver = d

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
		platform.PollEvents()
		frame()
		// Only block when there is nothing left to draw: that is what keeps an
		// idle app at ~0% CPU.
		if !d.app.needsFrame {
			platform.WaitEvents()
		}
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
	if a.tickTweens() {
		a.needsFrame = true
	}

	// Responsive properties follow the window width. Applying them marks the
	// tree dirty when a value changes, so this runs before the frame check.
	logicalW := float64(d.pendingW) / float64(maxScale(d.pendingScale))
	logicalH := float64(d.pendingH) / float64(maxScale(d.pendingScale))
	a.win.tree.SetViewport(logicalW, logicalH)
	a.win.tree.ApplyBreakpoints(logicalW)
	// Expressions run before the theme resolver: a conditional can pick a role,
	// and the role then resolves in the same frame. They also run before the
	// frame check, because a Go property change may be what an expression reads.
	a.win.tree.ApplyExpressions()
	// Role colors follow the scheme, so a theme change needs no per-component
	// work and a role declared in .ui never goes stale.
	if a.win.tree.ResolveColors(themeFor(a.win).Colors.Role) {
		a.needsFrame = true
	}

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
		// The label refreshes with the next painted frame; the overlay never
		// asks for one itself, so an idle app stays asleep.
	}
	d.list.ShowFPS = a.showFPS
	d.list.FPS = d.fps
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
	if a.OnError != nil {
		a.OnError(err)
		return
	}
	println("goui:", err.Error())
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

// themeFor picks the light or dark theme from the window's theme property.
func themeFor(w *Window) *m3.Theme {
	if w == nil || w.tree == nil || w.tree.Root == nil {
		return m3.Light()
	}
	if v, ok := w.tree.Root.PropValue("theme"); ok {
		if s, ok := v.(string); ok && s == "dark" {
			return m3.Dark()
		}
	}
	return m3.Light()
}
