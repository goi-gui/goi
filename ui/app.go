package ui

import (
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/goi-gui/goi/internal/hotreload"
	"github.com/goi-gui/goi/internal/i18n"
	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/mermaid"
	"github.com/goi-gui/goi/internal/svg"
	"github.com/goi-gui/goi/internal/tree"
)

// Options configure LoadWindow.
type Options struct {
	// Assets loads the design from fsys instead of the ui directory in
	// goi.yaml. Use it for a design embedded in the binary, such as a wasm
	// build; hot reload needs a directory and is skipped with it.
	Assets    fs.FS
	HotReload bool
	Width     int // used only when the .goi file has no width
	Height    int
	// Canvas names the DOM element a browser build draws into, by id. A page
	// with several canvases runs one app per canvas, and each app names its
	// own. An empty id takes the page's first canvas, creating one if the page
	// has none; a desktop build ignores it.
	Canvas string
}

// Option mutates Options.
type Option func(*Options)

// WithHotReload reloads the .goi file when it changes on disk. It is on by
// default for a directory design; a build registered with RegisterAssets has no
// directory to watch, so it is skipped. This option overrides either default.
func WithHotReload(on bool) Option { return func(o *Options) { o.HotReload = on } }

// WithAssets loads the design from fsys instead of the ui directory in
// goi.yaml. It is for an embedded design, usually a wasm build where there is
// no disk to read. Hot reload needs a directory, so it is skipped.
func WithAssets(fsys fs.FS) Option { return func(o *Options) { o.Assets = fsys } }

// WithSize sets the fallback client size for files that do not declare one.
func WithSize(w, h int) Option { return func(o *Options) { o.Width, o.Height = w, h } }

// WithCanvas names the element a browser build draws into, by id:
//
//	app.LoadWindow("app.goi", ui.WithCanvas("hero"))
//
// A page can hold several canvases and run one app per canvas. Without it the
// app takes the page's first canvas, which is what a single-canvas page wants.
func WithCanvas(id string) Option { return func(o *Options) { o.Canvas = id } }

// App owns the UI goroutine, the posted-function queue and every window's
// state. It is safe to call Post from any goroutine.
type App struct {
	mu     sync.Mutex
	posted []func()

	win    *Window
	assets fs.FS
	// picked holds the files the user chose with PickFiles this session, keyed
	// by their `goi://picked/…` src, so the image, media and subtitle readers
	// all reach them the way they reach an asset.
	picked     map[string][]byte
	path       string
	opts       Options
	driver     driver
	quitOnce   sync.Once
	needsFrame bool
	// themeFrom/to/display keep a scheme change: a different target starts the
	// effect named by `themeTransition`, and the timestamps drive it.
	themeFrom    *m3.Theme
	themeTo      *m3.Theme
	themeDisplay *m3.Theme
	themeStart   time.Time
	themeDur     time.Duration
	themeFading  bool
	themeWipe    bool
	themeWipeP   float64
	// viewportWait is set when a lazy element or a diagram had no viewport to
	// test yet, so the frame after layout looks again.
	viewportWait bool
	// imageTries counts how many times a node's missing bitmap was retried, so
	// a file that cannot be read is not retried forever.
	imageTries map[*tree.Image]int
	// mermaidTheme is the palette the current frame asked for. A queued render
	// whose theme was superseded is dropped instead of blocking the ones the
	// page now needs; a run of theme switches would otherwise leave a long tail
	// of stale renders.
	mermaidThemeMu sync.RWMutex
	mermaidTheme   string
	// showFPS is the debug overlay switch; off unless the app asks for it.
	showFPS bool
	// imageBudget is how many decoded image bytes the cache may hold, 0 for
	// the default.
	imageBudget int
	// videoBuffer is the per-player buffered-frame budget, 0 for the defaults.
	videoBuffer int
	// msaa is the multisample count the renderer draws with; 0 keeps the
	// renderer's own default (4).
	msaa int
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
	tweens []*Tween
	// images caches decoded Image assets by path or URL, so a reload is cheap;
	// anims holds the animations among them.
	images     map[string]*img.Data
	anims      map[string]*img.Anim
	imageBytes int
	imageOrder []string
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
	svgBytes   int
	svgOrder   []svgKey
	hasSVG     bool
	// hasImages is true while the tree holds an Image, so the per-frame src
	// check only walks a design that can use one.
	hasImages bool
	// hasMaps is true while the tree holds a Map, so the tile scheduler only
	// walks a design that draws one.
	hasMaps bool
	// mapQueue is the tile URLs waiting for one of mapTileParallel downloads;
	// mapPending dedupes them, mapFailed remembers a URL that 404ed so the same
	// blank tile is not requested every frame, and mapKeys is the insertion
	// order the cache cap evicts by.
	mapQueue    []string
	mapPending  map[string]bool
	mapFailed   map[string]bool
	mapKeys     []string
	mapBytes    int
	mapInFlight int
	// hasSVGAnim is true while the tree holds an animated SVG document.
	hasSVGAnim bool
	// mermaid renders diagrams off the UI goroutine. mermaidMu guards the
	// engine and its rasterizer, which is not safe for concurrent use; the
	// cache keeps a diagram shared by several nodes or pages.
	mermaidMu      sync.Mutex
	mermaidEng     *mermaid.Engine
	mermaidErr     error
	mermaidClosed  bool
	mermaidCache   map[string]mermaidBitmap
	mermaidOrder   []string
	mermaidBytes   int
	mermaidPending map[string]bool
	// pending holds the URLs with a fetch in flight, so two nodes with the same
	// src share one request and a reload does not start a second.
	pending map[string]bool
	// pulsing is true while at least one remote image is on the way; loadClock is
	// the phase clock for the loading placeholder.
	pulsing   bool
	loadClock float64
	// clock is how long the window has been drawing, which is what a wave on a
	// wavy progress bar follows. It advances only while frames run.
	clock time.Duration
	// themes are the schemes a `.goi` file may name, keyed by name.
	themes map[string]*Theme
	// hasMedia is true while the tree holds an Audio or Video, and media holds
	// the running players, one per playing node.
	hasMedia bool
	media    []*mediaPlayer
	// decoders is the ffmpeg side of playback: the probe cache and the temp
	// files it reads. A browser has no decoder of its own to feed, so the struct
	// is empty there.
	decoders mediaCache
	// fullscreenRestore is the window's fullscreen state before a video took it
	// over, so leaving the video puts it back.
	fullscreenRestore *bool
	// fonts holds the families LoadFont registered, so a window created after
	// the call still finds them. fontFaces holds the weight/style files
	// LoadFontFamily found for a family; fontVariables holds the variable files
	// and the non-weight axes pinned for a family.
	fonts         map[string][]byte
	fontFaces     map[string][]fontFaceFile
	fontVariables map[string][]fontVariable
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

// LoadWindow loads entry from the design directory: the ui value in goi.yaml,
// or the working directory when there is no goi.yaml. The entry is a file
// inside that directory, slash-separated; a design may import fragments next to
// it, so it is usually a directory rather than one file.
//
// Hot reload is on by default and watches every directory in the design. Call
// Bind afterwards. WithAssets loads an embedded design instead.
func (a *App) LoadWindow(entry string, opts ...Option) (*Window, error) {
	start := time.Now()
	o := Options{Width: 480, Height: 320, HotReload: defaultHotReload}
	for _, fn := range opts {
		fn(&o)
	}
	assets := o.Assets
	if assets == nil {
		assets = registeredAssets()
	}
	if assets == nil {
		dir, err := designDir()
		if err != nil {
			return nil, err
		}
		assets = dirFS(dir)
	}
	name := cleanName(entry)
	b, err := loadBundle(assets, name)
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
	catalog, cerr := i18n.Load(assets, i18n.Dir)
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
	a.win, a.assets, a.path, a.opts = w, assets, name, o
	w.noteMissingFonts(tr)
	a.themes = map[string]*Theme{"light": m3.Light(), "dark": m3.Dark()}
	// Images resolve against the container above, so this runs after it is set.
	a.loadImages(tr)
	a.loadMedia(tr)
	if err := a.initWindow(w); err != nil {
		return nil, err
	}
	logg().Info(logCtx, "ui: window loaded", "file", name, "took", time.Since(start).String())
	// An embedded design has no directory to watch; reload needs the disk.
	if _, ok := watchRoot(assets); o.HotReload && ok {
		if err := a.enableWatcher(b); err != nil {
			logg().Error(logCtx, "ui: hot reload unavailable", "error", err)
			return nil, err
		}
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

// MSAA sets the multisample count the window draws with: 1 turns it off, and
// 2, 4 and 8 ask for that many samples per pixel. A device that cannot draw the
// surface at the requested count falls back to 4 and then to 1, so the call
// never fails the app; the renderer's Samples reports what it settled on.
//
// Four, the default, is the count WebGPU guarantees, and it is enough to take
// the last stair-step off a curve. Curves are drawn from signed distances, which
// already fade their own edges, so a higher count refines edges rather than
// making a shape that lacked them.
// ImageCache sets how many decoded image bytes the cache may hold; 0 keeps the
// default (64 MB). The oldest picture is dropped, and the element showing it
// reloads from disk when it comes back. A photo wall on a small device can lower
// this; a design that swaps many large pictures can raise it.
func (a *App) ImageCache(bytes int) {
	if bytes < 0 {
		return
	}
	a.imageBudget = bytes
}

// VideoBuffer sets how many bytes one media player may hold in buffered video
// frames; 0 keeps the defaults (16 MB, 32 MB for a live feed). A 1080p frame is
// about 8 MB, so a design's `buffer` seconds are clamped by this. A player
// that already opened keeps the budget it started with.
func (a *App) VideoBuffer(bytes int) {
	if bytes < 0 {
		return
	}
	a.videoBuffer = bytes
}

func (a *App) MSAA(samples int) {
	if samples < 0 {
		return
	}
	if samples != 0 && samples != 1 && samples != 2 && samples != 4 && samples != 8 {
		a.reportError(fmt.Errorf("ui: MSAA(%d): use 1, 2, 4 or 8", samples))
		return
	}
	a.msaa = samples
	a.needsFrame = true
	if d, ok := a.driver.(*gpuDriver); ok && d.gpu != nil && samples > 0 {
		if err := d.gpu.SetSamples(samples); err != nil {
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
// Use it from a background goroutine that has finished fetching or computing
// something and now needs to touch a property, SetData or a window method.
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
	a.closeMermaid()
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
