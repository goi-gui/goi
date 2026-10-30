package ui

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"

	"github.com/ksckaan1/goui/internal/hotreload"
	"github.com/ksckaan1/goui/internal/img"
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
	assets     fs.FS
	path       string
	opts       Options
	driver     driver
	quitOnce   sync.Once
	needsFrame bool
	// showFPS is the debug overlay switch; off unless the app asks for it.
	showFPS bool

	watcher *hotreload.Reloader

	// tweens are the running app animations, touched only on the UI goroutine.
	tweens []*tween
	// images caches decoded Image assets by path, so a reload is cheap.
	images map[string]*img.Data

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

// LoadWindow loads a design from an asset container: fsys is where the .ui files
// live (an embed.FS in production, ui.DirFS in development) and path is the
// entry file inside it, slash-separated.
//
// A file may import fragments, so the design is usually a directory rather than
// one file. Call Bind afterwards.
func (a *App) LoadWindow(fsys fs.FS, path string, opts ...Option) (*Window, error) {
	o := Options{Width: 480, Height: 320}
	for _, fn := range opts {
		fn(&o)
	}
	name := cleanName(path)
	b, err := loadBundle(fsys, name)
	if err != nil {
		return nil, err
	}
	tr, err := tree.Build(b.File, &tree.Hooks{Marked: a.markDirty})
	if err != nil {
		return nil, fmt.Errorf("goui: %s: %w", name, err)
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
	a.win, a.assets, a.path, a.opts = w, fsys, name, o
	// Images resolve against the container above, so this runs after it is set.
	a.loadImages(tr)
	if err := a.initWindow(w); err != nil {
		return nil, err
	}
	if o.HotReload {
		if err := a.enableWatcher(); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// LoadWindowFile is LoadWindow for a single file on disk. The file's directory
// becomes the asset container, so imports resolve next to it. Hot reload watches
// that directory.
func (a *App) LoadWindowFile(file string, opts ...Option) (*Window, error) {
	dir := filepath.Dir(file)
	return a.LoadWindow(DirFS(dir), filepath.Base(file), opts...)
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

// ShowFPS draws the frame rate in the top-right corner. It is off by default and
// is a debugging aid, not a design element: call it from the UI goroutine, such
// as inside Window.Bind.
func (a *App) ShowFPS(show bool) {
	a.showFPS = show
	a.needsFrame = true
	if a.driver != nil {
		a.driver.wake()
	}
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

// Close releases the file watcher and platform resources. Run calls it on exit;
// call it yourself only when you never call Run.
func (a *App) Close() {
	if a.watcher != nil {
		a.watcher.Close()
		a.watcher = nil
	}
	if a.driver != nil {
		a.driver.close()
	}
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
