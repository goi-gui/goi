// Package platform owns the SDL3 window, the native handle it exposes for
// WebGPU, and the translation of input into framework events. Only this package
// and render may import a windowing library or a graphics API.
package platform

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"unsafe"
)

// Input receives raw input translated from SDL3, in logical pixels.
type Input interface {
	MouseMove(x, y float64)
	MouseDown(x, y float64, button int, shift bool)
	MouseUp(x, y float64, button int)
	// Scroll carries the wheel delta, in logical steps, plus the Shift state:
	// a notched wheel is vertical unless Shift asks for horizontal.
	Scroll(dx, dy float64, shift bool)
	Key(key Key, mods Mods)
	// Preedit carries the not-yet-committed IME composition and the caret
	// position inside it; an empty string ends the composition.
	Preedit(text string, caret int)
	// CommitText replaces any composition with finished text. Plain typing
	// arrives here too, because SDL reports both as committed text.
	CommitText(text string)
	Focus(focused bool)
	Resize(width, height int, scale float32)
	// Expose asks for a frame without changing anything. Wayland sends it
	// during an interactive resize, where the compositor waits for a buffer
	// before it applies the new size.
	Expose()
	// ThemeChanged reports that the desktop switched between light and dark.
	// A design may follow it with `theme: app.systemMode`.
	ThemeChanged()
	Close()
}

// Key mirrors tree.Key without importing it, so platform stays leaf-level.
type Key int

// Keys.
const (
	KeyNone Key = iota
	KeyBackspace
	KeyDelete
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyEnter
	KeyTab
	KeySpace
	KeyEscape
	KeyUp
	KeyDown
	KeyA
	KeyC
	KeyV
	KeyX
)

// Mods is modifier state.
type Mods struct {
	Shift, Ctrl, Alt, Super bool
}

// HitTest is what the pointer over a window position means: a normal client
// area, a title-bar drag, or one of the edges that resizes the window.
//
// The values are SDL_HitTestResult's, so the callback is a cast.
type HitTest int

// Hit test results.
const (
	HitTestNormal            HitTest = 0
	HitTestDrag              HitTest = 1
	HitTestResizeTopLeft     HitTest = 2
	HitTestResizeTop         HitTest = 3
	HitTestResizeTopRight    HitTest = 4
	HitTestResizeRight       HitTest = 5
	HitTestResizeBottomRight HitTest = 6
	HitTestResizeBottom      HitTest = 7
	HitTestResizeBottomLeft  HitTest = 8
	HitTestResizeLeft        HitTest = 9
)

// Window is a native window with no graphics context of its own: WebGPU draws
// into it through the handles below.
type Window struct {
	win    unsafe.Pointer
	input  Input
	width  int
	height int
	scale  float32

	// closing is set when the window manager asks for a close, so the loop can
	// stop without waiting for the next event.
	closing bool

	// imeActive tracks SDL_StartTextInput, which must be paired with a stop.
	imeActive bool

	// cursor/cursorSet skip re-setting the same shape on every mouse move.
	cursor    Cursor
	cursorSet bool

	// resizable mirrors the design's `disableResize`: a non-resizable window
	// also refuses to maximize. minW/minH/maxW/maxH are the design's size
	// limits in logical points; a zero maximum means unbounded.
	resizable  bool
	minW, minH int
	maxW, maxH int
	fullscreen bool
}

// Cursor is the shape the pointer takes over the client area.
type Cursor int

// Cursor kinds the framework asks for.
const (
	CursorArrow Cursor = iota
	CursorText
	CursorPointer
)

// SetCursor changes the pointer shape. It is a no-op when the shape is already
// set, so the input layer can call it on every move.
func (p *Window) SetCursor(c Cursor) {
	if p.win == nil || (p.cursorSet && p.cursor == c) {
		return
	}
	sdlSetCursor(int(c))
	p.cursor, p.cursorSet = c, true
}

// SetTextInput tells the input method whether a text field is focused and where
// its caret is, so composed text and the candidate window land in the right
// place. SDL only delivers text events while text input is on.
func (p *Window) SetTextInput(active bool, x, y, w, h float64) {
	if p.win == nil {
		return
	}
	if !active {
		if p.imeActive {
			sdlStopTextInput(p.win)
			p.imeActive = false
		}
		return
	}
	if !p.imeActive {
		// A compositor without text-input support ignores this; plain typing
		// still works because it arrives through the same text path.
		_ = sdlStartTextInput(p.win)
		p.imeActive = true
	}
	sdlSetTextInputArea(p.win, int(x), int(y), int(w), int(h))
}

// New creates the window. Call Init once on the main goroutine first.
// decorated=false hides the platform title bar, so the design draws its own.
func New(title string, width, height int, decorated bool) (*Window, error) {
	win, err := sdlCreateWindow(title, width, height, decorated)
	if err != nil {
		return nil, err
	}
	p := &Window{win: win, width: width, height: height, scale: 1, resizable: true}
	if s := sdlWindowScale(win); s > 0 {
		p.scale = s
	}
	// Adopt the real pixel size, so the first expose does not look like a resize.
	p.resizeToFramebuffer()
	return p, nil
}

var (
	initOnce sync.Once
	initErr  error
)

// Init initializes SDL's video subsystem and locks the goroutine, as macOS
// requires. A missing SDL3 install is reported here instead of crashing later.
func Init() error {
	runtime.LockOSThread()
	initOnce.Do(func() {
		if err := sdlInit(); err != nil {
			initErr = fmt.Errorf("platform: %w", err)
		}
	})
	return initErr
}

// Terminate releases SDL. Call after every window is destroyed.
func Terminate() { sdlQuit() }

// SetInput installs the input sink. SDL is poll based, so input arrives through
// PollEvents and WaitEvents rather than callbacks.
func (p *Window) SetInput(in Input) { p.input = in }

// currentHitTest is read by SDL's hit-test callback. There is one window per
// process today and the callback runs on the UI goroutine, so no lock is needed.
var currentHitTest func(x, y float64) HitTest

// SetHitTest installs the region callback a borderless window needs: a press in
// a title bar asks the compositor to move the window, and the outer pixels ask
// it to resize. The callback runs on the UI goroutine.
func (p *Window) SetHitTest(fn func(x, y float64) HitTest) {
	if p.win == nil || fn == nil {
		return
	}
	currentHitTest = fn
	sdlInstallHitTest(p.win)
}

// PollEvents dispatches every pending event without blocking.
func (p *Window) PollEvents() {
	for {
		ev, ok := sdlPollEvent()
		if !ok {
			return
		}
		p.dispatch(ev)
	}
}

// WaitEvents blocks until an event arrives, then dispatches it and any others
// already queued. PostEmptyEvent wakes it from another goroutine.
func (p *Window) WaitEvents() {
	ev, ok := sdlWaitEvent()
	if ok {
		p.dispatch(ev)
	}
	p.PollEvents()
}

func (p *Window) dispatch(ev rawEvent) {
	switch ev.kind {
	case evQuit, evClose:
		p.closing = true
		if p.input != nil {
			p.input.Close()
		}
	case evResize:
		p.applyClientSize(ev.width, ev.height)
	case evTheme:
		if p.input != nil {
			p.input.ThemeChanged()
		}
	case evExpose:
		// During an interactive resize Wayland holds an implicit grab: SDL
		// defers the configure and its resize event to the next frame, and
		// sends this event to ask for one. Ignoring it stalls the drag until
		// the button is released.
		p.resizeToFramebuffer()
		if p.input != nil {
			p.input.Expose()
		}
	case evFocusIn:
		if p.input != nil {
			p.input.Focus(true)
		}
	case evFocusOut:
		if p.input != nil {
			p.input.Focus(false)
		}
	case evMotion:
		if p.input != nil {
			p.input.MouseMove(ev.x, ev.y)
		}
	case evButton:
		if p.input == nil {
			return
		}
		if ev.down {
			// SDL's button event carries no modifier state, so the current one
			// is read instead; it is updated before the event is queued.
			p.input.MouseDown(ev.x, ev.y, ev.button, translateMods(sdlModState()).Shift)
		} else {
			p.input.MouseUp(ev.x, ev.y, ev.button)
		}
	case evWheel:
		if p.input != nil {
			// SDL reports whole wheel steps, positive away from the user, which
			// is the same convention the scroll handler expects. The wheel event
			// carries no modifiers, so the current Shift state is read instead.
			p.input.Scroll(ev.wheelX, ev.wheelY, translateMods(sdlModState()).Shift)
		}
	case evKey:
		if p.input != nil {
			p.input.Key(translateKey(ev.scancode), translateMods(ev.mod))
		}
	case evText:
		if p.input != nil {
			// Typing and an IME commit are both committed text in SDL; the text
			// handler clears any composition and inserts the run.
			p.input.CommitText(ev.text)
		}
	case evEditing:
		if p.input != nil {
			p.input.Preedit(ev.text, ev.caret)
		}
	}
}

// resizeToFramebuffer re-reads the client size and reports it when it changed.
// During an interactive Wayland resize SDL defers the configure and sends only
// an expose, so this is the path a drag actually takes: it must clamp as well,
// or the client would follow the pointer past the design's limits.
func (p *Window) resizeToFramebuffer() {
	w, h := p.FramebufferSize()
	if w <= 0 || h <= 0 {
		return
	}
	p.applyClientSize(w, h)
}

// applyClientSize is the single place a new client size enters the window: it
// clamps to the design's limits, asks the platform for the clamped size when
// the request was outside them, and tells the input sink. Both the resize event
// and the framebuffer read go through here.
func (p *Window) applyClientSize(pw, ph int) {
	if pw <= 0 || ph <= 0 {
		return
	}
	// A design that caps the size is a window that is not meant to fill the
	// screen, so a maximize from the title bar is undone: the compositor owns
	// that button, and this is the only way to keep the cap.
	if p.hasMax() && p.Maximized() && !p.fullscreen {
		sdlRestore(p.win)
	}
	cw, ch := p.clamp(pw, ph)
	if cw != pw || ch != ph {
		p.setWindowPixels(cw, ch)
	}
	if cw == p.width && ch == p.height {
		return
	}
	p.width, p.height = cw, ch
	if p.input != nil {
		p.input.Resize(cw, ch, p.contentScale())
	}
}

func (p *Window) contentScale() float32 {
	if s := sdlWindowScale(p.win); s > 0 {
		p.scale = s
	} else if p.scale <= 0 {
		p.scale = 1
	}
	return p.scale
}

// FramebufferSize returns the device-pixel size of the client area.
func (p *Window) FramebufferSize() (int, int) {
	w, h := sdlWindowSizePixels(p.win)
	if w <= 0 || h <= 0 {
		return p.width, p.height
	}
	return w, h
}

// LogicalSize returns the client size in logical pixels.
func (p *Window) LogicalSize() (float64, float64) {
	w, h := sdlWindowSize(p.win)
	if w <= 0 || h <= 0 {
		return float64(p.width), float64(p.height)
	}
	return float64(w), float64(h)
}

// Scale returns the content scale.
func (p *Window) Scale() float32 { return p.contentScale() }

// SurfaceKind says which native handle pair SurfaceHandles returned.
type SurfaceKind int

// Surface kinds.
const (
	SurfaceWayland SurfaceKind = iota
	SurfaceX11
)

// Visible reports whether the window is shown (some compositors report a nil
// surface while minimized).
func (p *Window) Visible() bool {
	flags := sdlWindowFlags(p.win)
	return flags&(flagHidden|flagMinimized) == 0
}

// ShouldClose reports whether the user asked to close the window.
func (p *Window) ShouldClose() bool { return p.closing }

// Destroy tears the window down.
func (p *Window) Destroy() {
	if p.win == nil {
		return
	}
	currentHitTest = nil
	sdlDestroyWindow(p.win)
	p.win = nil
	p.cursorSet = false
}

// PostEmptyEvent wakes WaitEvents from another goroutine.
func PostEmptyEvent() { sdlPushUserEvent() }

// Theme is the desktop's light/dark preference.
type Theme uint8

// Theme values. ThemeUnknown means the platform does not report one.
const (
	ThemeUnknown Theme = iota
	ThemeLight
	ThemeDark
)

// SystemTheme returns the desktop's theme as last reported by the platform. It
// is the value SDL caches and updates on SDL_EVENT_SYSTEM_THEME_CHANGED, so it
// is cheap to read on every frame.
func SystemTheme() Theme {
	switch sdlSystemTheme() {
	case 1:
		return ThemeLight
	case 2:
		return ThemeDark
	}
	return ThemeUnknown
}

// SetTitle updates the window title.
func (p *Window) SetTitle(title string) { sdlSetTitle(p.win, title) }

// Minimize hides the window in the platform's task bar.
func (p *Window) Minimize() { sdlMinimize(p.win) }

// ToggleMaximize maximizes the window, or restores it when already maximized.
// A window the design made non-resizable does not maximize, which is the same
// rule the platform title bar's button follows.
func (p *Window) ToggleMaximize() {
	if p.Maximized() {
		sdlRestore(p.win)
		return
	}
	if !p.resizable || p.hasMax() {
		return
	}
	sdlMaximize(p.win)
}

// hasMax reports whether the design capped the window size.
func (p *Window) hasMax() bool { return p.maxW > 0 || p.maxH > 0 }

// SetResizable turns the user's resize and maximize off. A screen-filling
// window is unaffected: fullscreen is a separate switch.
func (p *Window) SetResizable(on bool) {
	if p.win == nil || p.resizable == on {
		return
	}
	p.resizable = on
	sdlSetResizable(p.win, on)
	if !on && p.Maximized() {
		sdlRestore(p.win)
	}
}

// SetSizeLimits constrains the window to minW..maxW and minH..maxH logical
// points. A non-positive maximum means "no limit".
func (p *Window) SetSizeLimits(minW, minH, maxW, maxH int) {
	if p.win == nil {
		return
	}
	if minW < 0 {
		minW = 0
	}
	if minH < 0 {
		minH = 0
	}
	p.minW, p.minH, p.maxW, p.maxH = minW, minH, maxW, maxH
	sdlSetSizeLimits(p.win, minW, minH, maxW, maxH)
	p.resizeToClamped()
}

// resizeToClamped brings the current size inside the limits, for a design that
// declares a minimum larger than its start size (or a maximum smaller).
func (p *Window) resizeToClamped() {
	w, h := p.FramebufferSize()
	if w <= 0 || h <= 0 {
		return
	}
	p.applyClientSize(w, h)
}

// clamp returns a pixel size inside the design's limits. SDL talks in logical
// points, so the limits are scaled to pixels here.
func (p *Window) clamp(pw, ph int) (int, int) {
	// Fullscreen and maximize are the platform's own sizes: a fullscreen window
	// fills the display, and a maximized one must keep the exact geometry the
	// compositor assigned or the protocol is violated. The limits apply again
	// when the window returns to a normal one. This is why maximize is not a
	// way to test the limits, and why `disableResize` also refuses to maximize.
	if p.fullscreen || (p.Maximized() && !p.hasMax()) {
		return pw, ph
	}
	s := p.contentScale()
	if s <= 0 {
		s = 1
	}
	return clampSize(p.minW, p.minH, p.maxW, p.maxH, float64(s), pw, ph)
}

// clampSize bounds a pixel size by limits given in logical points. A zero limit
// means "none", which is also how the design's -1 arrives here.
func clampSize(minW, minH, maxW, maxH int, scale float64, pw, ph int) (int, int) {
	w, h := pw, ph
	if minW > 0 && w < int(math.Round(float64(minW)*scale)) {
		w = int(math.Round(float64(minW) * scale))
	}
	if minH > 0 && h < int(math.Round(float64(minH)*scale)) {
		h = int(math.Round(float64(minH) * scale))
	}
	if maxW > 0 && w > int(math.Round(float64(maxW)*scale)) {
		w = int(math.Round(float64(maxW) * scale))
	}
	if maxH > 0 && h > int(math.Round(float64(maxH)*scale)) {
		h = int(math.Round(float64(maxH) * scale))
	}
	return w, h
}

// setWindowPixels asks the platform for a client size given in pixels.
func (p *Window) setWindowPixels(pw, ph int) {
	s := float64(p.contentScale())
	if s <= 0 {
		s = 1
	}
	sdlSetWindowSize(p.win, int(math.Round(float64(pw)/s)), int(math.Round(float64(ph)/s)))
}

// SetFullscreen makes the window fill the display, or returns it to a normal
// window. Fullscreen does not change the resizable flag.
func (p *Window) SetFullscreen(on bool) {
	if p.win == nil || p.fullscreen == on {
		return
	}
	p.fullscreen = on
	sdlSetFullscreen(p.win, on)
}

// Fullscreen reports whether the window fills the display.
func (p *Window) Fullscreen() bool { return p.fullscreen }

// Maximized reports whether the window is maximized.
func (p *Window) Maximized() bool { return sdlWindowFlags(p.win)&flagMaximized != 0 }

// Close asks the window to close, as the platform close button would.
func (p *Window) Close() { p.closing = true }

// Clipboard returns the clipboard text.
func Clipboard() string { return sdlClipboard() }

// SetClipboard replaces the clipboard text.
func SetClipboard(s string) { sdlSetClipboard(s) }

func translateMods(m int) Mods {
	return Mods{
		Shift: m&sdlKmodShift != 0,
		Ctrl:  m&sdlKmodCtrl != 0,
		Alt:   m&sdlKmodAlt != 0,
		Super: m&sdlKmodGui != 0,
	}
}

// translateKey maps a physical key. Scancodes are used rather than keycodes so
// the shortcuts stay in the same place on a different keyboard layout.
func translateKey(s int) Key {
	switch s {
	case scancodeBackspace:
		return KeyBackspace
	case scancodeDelete:
		return KeyDelete
	case scancodeLeft:
		return KeyLeft
	case scancodeRight:
		return KeyRight
	case scancodeHome:
		return KeyHome
	case scancodeEnd:
		return KeyEnd
	case scancodeReturn, scancodeKPEnter:
		return KeyEnter
	case scancodeTab:
		return KeyTab
	case scancodeSpace:
		return KeySpace
	case scancodeEscape:
		return KeyEscape
	case scancodeUp:
		return KeyUp
	case scancodeDown:
		return KeyDown
	case scancodeA:
		return KeyA
	case scancodeC:
		return KeyC
	case scancodeV:
		return KeyV
	case scancodeX:
		return KeyX
	}
	return KeyNone
}
