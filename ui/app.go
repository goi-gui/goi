package ui

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"time"

	"github.com/goi-gui/goi/internal/hotreload"
	"github.com/goi-gui/goi/internal/i18n"
	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/media"
	"github.com/goi-gui/goi/internal/svg"
	"github.com/goi-gui/goi/internal/tree"
)

// Options configure LoadWindow.
type Options struct {
	HotReload bool
	Width     int // used only when the .goi file has no width
	Height    int
}

// Option mutates Options.
type Option func(*Options)

// WithHotReload reloads the .goi file when it changes on disk.
func WithHotReload(on bool) Option { return func(o *Options) { o.HotReload = on } }

// WithSize sets the fallback client size for files that do not declare one.
func WithSize(w, h int) Option { return func(o *Options) { o.Width, o.Height = w, h } }

// App owns the UI goroutine, the posted-function queue and every window's
// state. It is safe to call Post from any goroutine.
type App struct {
	mu     sync.Mutex
	posted []func()

	win        *Window
	assets     fs.FS
	path       string
	opts       Options
	driver     driver
	quitOnce   sync.Once
	needsFrame bool
	// showFPS is the debug overlay switch; off unless the app asks for it.
	showFPS bool
	// vsync waits for the display refresh; maxFPS caps the drawn frames per
	// second when it does not. Both default to "tied to the display".
	vsync  bool
	maxFPS int
	// debugUI logs every input event except pointer motion; DEBUG_UI turns it
	// on for a run.
	debugUI bool
	// chrome is the window state last applied to the platform, so a frame that
	// did not change it costs one comparison.
	chrome chromeState

	watcher *hotreload.Reloader

	// tweens are the running app animations, touched only on the UI goroutine.
	tweens []*tween
	// images caches decoded Image assets by path or URL, so a reload is cheap;
	// anims holds the animations among them.
	images map[string]*img.Data
	anims  map[string]*img.Anim
	// hasAnim is true while the tree holds an animated Image, so the per-frame
	// walk only runs when there is one.
	hasAnim bool
	// animWait is how long until the next animation frame is due; zero means
	// nothing is playing, so the loop may block until input.
	animWait time.Duration
	// svgs caches parsed SVG files by src, and svgData the rasterized bitmaps
	// by src and colour, so a reload or a second node reuses the work. svgRemote
	// holds the bytes of a fetched remote src until it is parsed.
	svgs      map[string]*svg.SVG
	svgData   map[svgKey]*img.Data
	svgRemote map[string][]byte
	// svgPending holds the SVG URLs with a fetch in flight, for the same reason
	// as pending: one request per URL.
	svgPending map[string]bool
	hasSVG     bool
	// hasImages is true while the tree holds an Image, so the per-frame src
	// check only walks a design that can use one.
	hasImages bool
	// hasSVGAnim is true while the tree holds an animated SVG document.
	hasSVGAnim bool
	// pending holds the URLs with a fetch in flight, so two nodes with the same
	// src share one request and a reload does not start a second.
	pending map[string]bool
	// pulsing is true while at least one remote image is on the way; loadClock is
	// the phase clock for the loading placeholder.
	pulsing   bool
	loadClock float64
	// themes are the schemes a `.goi` file may name, keyed by name.
	themes map[string]*Theme
	// hasMedia is true while the tree holds an Audio or Video, and media holds
	// the running players, one per playing node. mediaInfo caches the probe of
	// each src, so a reload does not run ffmpeg again.
	hasMedia  bool
	media     []*mediaPlayer
	mediaInfo map[string]media.Info
	// mediaFiles maps a src inside the design's container to the temp file
	// ffmpeg reads, so one copy serves every player and a reload.
	mediaFiles map[string]string
	// fonts holds the families LoadFont registered, so a window created after
	// the call still finds them.
	fonts map[string][]byte
	// catalog is the design's i18n folder, nil when it has none. language and
	// fallbackLanguage are what SetLanguage chose; empty means "follow the
	// desktop", which is why neither needs a startup write.
	catalog          *i18n.Bundle
	language         string
	fallbackLanguage string

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
	a := &App{vsync: true, debugUI: debugUI()}
	logg().Info(logCtx, "ui: starting")
	if err := a.initPlatform(); err != nil {
		logg().Error(logCtx, "ui: platform init failed", "error", err)
		return nil, err
	}
	return a, nil
}

// newHeadlessApp creates an app without a window, for tests.
func newHeadlessApp() *App { return &App{vsync: true, debugUI: debugUI()} }

// LoadWindow loads a design from an asset container: fsys is where the .goi files
// live (an embed.FS in production, ui.DirFS in development) and path is the
// entry file inside it, slash-separated.
//
// A file may import fragments, so the design is usually a directory rather than
// one file. Call Bind afterwards.
func (a *App) LoadWindow(fsys fs.FS, path string, opts ...Option) (*Window, error) {
	start := time.Now()
	o := Options{Width: 480, Height: 320}
	for _, fn := range opts {
		fn(&o)
	}
	name := cleanName(path)
	b, err := loadBundle(fsys, name)
	if err != nil {
		logg().Error(logCtx, "ui: design failed to load", "file", name, "error", err)
		return nil, err
	}
	tr, err := tree.Build(b.File, &tree.Hooks{Marked: a.markDirty, Error: a.reportError, Media: a.mediaRequest})
	if err != nil {
		return nil, fmt.Errorf("goi: %s: %w", name, err)
	}
	// i18n is optional: no folder means every t(...) renders its own key. A
	// broken catalog is reported but does not stop the window from loading.
	catalog, cerr := i18n.Load(fsys, i18n.Dir)
	if cerr != nil {
		a.reportError(cerr)
	}
	a.catalog = catalog
	tr.SetI18n(catalog)
	tr.SetLanguage(a.language)
	tr.SetFallbackLanguage(a.fallbackLanguage)

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
	a.themes = map[string]*Theme{"light": m3.Light(), "dark": m3.Dark()}
	// Images resolve against the container above, so this runs after it is set.
	a.loadImages(tr)
	a.loadMedia(tr)
	if err := a.initWindow(w); err != nil {
		return nil, err
	}
	logg().Info(logCtx, "ui: window loaded", "file", name, "took", time.Since(start).String())
	if o.HotReload {
		if err := a.enableWatcher(); err != nil {
			logg().Error(logCtx, "ui: hot reload unavailable", "error", err)
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

// VSync ties the frame rate to the display refresh; it is on by default.
// Turning it off presents each frame as soon as it is ready, so the app can
// draw faster than the panel and ShowFPS reports the renderer's rate rather
// than the display's. MaxFPS is the usual companion: uncapped alone goes as
// fast as the GPU allows and burns the battery. Call it from the UI goroutine,
// such as inside Window.Bind.
func (a *App) VSync(on bool) {
	a.vsync = on
	a.needsFrame = true
	if d, ok := a.driver.(*gpuDriver); ok && d.gpu != nil {
		if err := d.gpu.SetVSync(on); err != nil {
			a.reportError(err)
		}
	}
	if a.driver != nil {
		a.driver.wake()
	}
}

// MaxFPS caps the drawn frames per second; 0 (the default) means no cap. A cap
// below the display refresh only takes effect together with VSync(false), so a
// 60 Hz panel still shows 60 unless vsync is off.
func (a *App) MaxFPS(fps int) {
	if fps < 0 {
		fps = 0
	}
	a.maxFPS = fps
	a.needsFrame = true
	if a.driver != nil {
		a.driver.wake()
	}
}

// SetLanguage chooses the language `t(...)` reads first, as an ISO 639-1 code
// ("tr", "en"). An empty code follows the desktop again. A code the design does
// not ship falls through to the fallback language entry by entry.
//
// From Go: a language menu calls this on selection. A design reads the result
// as `app.language`.
func (a *App) SetLanguage(code string) {
	a.language = code
	if a.win != nil {
		a.win.tree.SetLanguage(code)
	}
	a.requestFrame()
}

// SetFallbackLanguage chooses the language a missing translation falls back to.
// An empty code follows the desktop, which is the default.
func (a *App) SetFallbackLanguage(code string) {
	a.fallbackLanguage = code
	if a.win != nil {
		a.win.tree.SetFallbackLanguage(code)
	}
	a.requestFrame()
}

// Language reports the active language: the code SetLanguage chose, or the
// desktop's when none was set. The design reads the same value as
// `app.language`.
func (a *App) Language() string {
	if a.win != nil && a.win.tree != nil {
		return a.win.tree.Language()
	}
	return a.language
}

// SystemLanguage reports the desktop's preferred language as an ISO 639-1 code,
// or "" when the platform does not report one. A design reads the same value as
// `app.systemLanguage`.
func (a *App) SystemLanguage() string {
	if a.win != nil && a.win.tree != nil {
		return a.win.tree.SystemLanguage()
	}
	return systemLanguage()
}

// requestFrame schedules one frame, for state that changes what expressions
// resolve rather than a property cell.
func (a *App) requestFrame() {
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
	// The decoders are child processes: leaving them running would keep the
	// sound playing after the window is gone.
	a.closeMedia()
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
