// Package platform owns the SDL3 window, the native handle it exposes for
// WebGPU, and the translation of input into framework events. Only this package
// and render may import a windowing library or a graphics API.
package platform

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// Input receives raw input translated from SDL3, in logical pixels.
type Input interface {
	MouseMove(x, y float64)
	MouseDown(x, y float64, button int, shift bool)
	MouseUp(x, y float64, button int)
	Scroll(dx, dy float64)
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
	p := &Window{win: win, width: width, height: height, scale: 1}
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
		if ev.width <= 0 || ev.height <= 0 {
			return
		}
		p.width, p.height = ev.width, ev.height
		if p.input != nil {
			p.input.Resize(p.width, p.height, p.contentScale())
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
			// is the same convention the scroll handler expects.
			p.input.Scroll(ev.wheelX, ev.wheelY)
		}
	case evKey:
		println("DEBUG key scancode", ev.scancode, "key", translateKey(ev.scancode))
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
func (p *Window) resizeToFramebuffer() {
	w, h := p.FramebufferSize()
	if w <= 0 || h <= 0 || (w == p.width && h == p.height) {
		return
	}
	p.width, p.height = w, h
	if p.input != nil {
		p.input.Resize(w, h, p.contentScale())
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

// SetTitle updates the window title.
func (p *Window) SetTitle(title string) { sdlSetTitle(p.win, title) }

// Minimize hides the window in the platform's task bar.
func (p *Window) Minimize() { sdlMinimize(p.win) }

// ToggleMaximize maximizes the window, or restores it when already maximized.
func (p *Window) ToggleMaximize() {
	if p.Maximized() {
		sdlRestore(p.win)
		return
	}
	sdlMaximize(p.win)
}

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
