package ui

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/ksckaan1/goui/internal/layout"
	"github.com/ksckaan1/goui/internal/paint"
	"github.com/ksckaan1/goui/internal/platform"
	"github.com/ksckaan1/goui/internal/render"
	"github.com/ksckaan1/goui/internal/tree"
)

var debugFrames = os.Getenv("GOUI_DEBUG") != ""
var debugContinuous = os.Getenv("GOUI_CONTINUOUS") != ""

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
	d := &gpuDriver{
		app:    a,
		win:    pw,
		gpu:    gpu,
		engine: layout.New(textMeasurer{lib: gpu.Text()}),
	}
	d.input = &interactor{app: a, tree: w.tree}
	pw.SetInput(d.input)
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
		if !d.app.needsFrame && !debugContinuous {
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

	if !a.needsFrame && !debugContinuous {
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
	d.list.Build(a.win.tree, float64(d.pendingW)/float64(maxScale(d.pendingScale)), float64(d.pendingH)/float64(maxScale(d.pendingScale)))
	if err := d.gpu.Render(&d.list, d.pendingW, d.pendingH); err != nil {
		a.reportError(err)
	}
	if debugFrames {
		println("frame:", d.pendingW, d.pendingH, d.pendingScale, "ops:", len(d.list.Ops))
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
