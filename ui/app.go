package ui

import (
	"fmt"
	"os"
	"sync"

	"github.com/ksckaan1/goui/internal/tree"
)

// Options configure LoadWindow.
type Options struct {
	HotReload bool
	Width     int // used only when the .ui file has no width
	Height    int
}

// Option mutates Options.
type Option func(*Options)

// WithHotReload reloads the .ui file when it changes on disk.
func WithHotReload(on bool) Option { return func(o *Options) { o.HotReload = on } }

// WithSize sets the fallback client size for files that do not declare one.
func WithSize(w, h int) Option { return func(o *Options) { o.Width, o.Height = w, h } }

// App owns the UI goroutine, the posted-function queue and every window's
// state. It is safe to call Post from any goroutine.
type App struct {
	mu     sync.Mutex
	posted []func()
	wake   func() // pushes the platform loop out of WaitEvents

	win        *Window
	path       string
	opts       Options
	driver     driver
	quitOnce   sync.Once
	needsFrame bool

	// OnError receives rendering errors (a lost surface, a validation failure).
	// When nil they are printed and the app keeps running.
	OnError func(error)
}

// driver is the platform event loop.
type driver interface {
	// run blocks until the app quits, calling frame for each loop iteration.
	run(frame func()) error
	// wake interrupts a blocking wait (thread-safe).
	wake()
	// quit asks the loop to stop (thread-safe).
	quit()
	// close releases the platform window and GPU resources.
	close()
}

// NewApp creates an app with the platform driver for this OS.
func NewApp() (*App, error) {
	a := &App{}
	if err := a.initPlatform(); err != nil {
		return nil, err
	}
	return a, nil
}

// newHeadlessApp creates an app without a window, for tests.
func newHeadlessApp() *App { return &App{} }

// LoadWindow parses the .ui file at path, builds the tree and gives the app its
// window. Call Bind afterwards.
func (a *App) LoadWindow(path string, opts ...Option) (*Window, error) {
	o := Options{Width: 480, Height: 320}
	for _, fn := range opts {
		fn(&o)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tr, diag := tree.BuildSource(src, &tree.Hooks{Marked: a.markDirty})
	if !diag.Empty() {
		return nil, fmt.Errorf("%s", diag.Format(path, src))
	}

	w := &Window{tree: tr, app: a, title: tr.Root.Title.Get()}
	w.width, w.height = o.Width, o.Height
	if v, ok := tr.Root.PropValue("width"); ok {
		if f, ok := v.(float64); ok && f > 0 {
			w.width = int(f)
		}
	}
	if v, ok := tr.Root.PropValue("height"); ok {
		if f, ok := v.(float64); ok && f > 0 {
			w.height = int(f)
		}
	}
	a.win, a.path, a.opts = w, path, o
	if err := a.initWindow(w); err != nil {
		return nil, err
	}
	return w, nil
}

// Bind registers the handler-attachment function and runs it immediately.
// It runs again after every hot reload, so app state must live outside it.
func (w *Window) Bind(fn func(*Window) error) error {
	w.binding = fn
	if fn == nil {
		return nil
	}
	return fn(w)
}

// Post runs fn on the UI goroutine, in FIFO order with other posted functions.
func (a *App) Post(fn func()) {
	if fn == nil {
		return
	}
	a.mu.Lock()
	a.posted = append(a.posted, fn)
	a.mu.Unlock()
	if a.driver != nil {
		a.driver.wake()
	}
}

// drainPosted runs every queued function. Called by the frame loop only.
func (a *App) drainPosted() {
	a.mu.Lock()
	fns := a.posted
	a.posted = nil
	a.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Run starts the event loop and blocks until Quit or the window closes.
func (a *App) Run() error {
	if a.driver == nil {
		return fmt.Errorf("ui: Run needs a platform driver (use NewApp)")
	}
	return a.driver.run(a.frame)
}

// Quit stops the event loop.
func (a *App) Quit() {
	a.quitOnce.Do(func() {
		if a.driver != nil {
			a.driver.wake()
			a.driver.quit()
		}
	})
}

// markDirty is the tree's hook: a change means the next frame must run.
func (a *App) markDirty(tree.DirtyKind) {
	a.needsFrame = true
	if a.driver != nil {
		a.driver.wake()
	}
}
