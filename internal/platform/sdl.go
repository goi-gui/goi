//go:build !js

package platform

// The SDL3 backend. It loads libSDL3 through purego, so the framework needs no
// C toolchain or SDL development files: the runtime library is the only
// requirement. Every port uses this one implementation.

import (
	"os"
	"sync"
	"unsafe"

	sdl "github.com/Zyko0/go-sdl3/sdl"
)

var sdlOnce sync.Once

func loadSDL() error {
	var err error
	sdlOnce.Do(func() {
		err = sdl.LoadLibrary(sdl.Path())
	})
	return err
}

func sdlInit() error {
	if err := loadSDL(); err != nil {
		return err
	}
	if wantWaylandDriver(os.Getenv) {
		_ = sdl.SetHint(sdl.HINT_VIDEO_DRIVER, "wayland")
	}
	return sdl.Init(sdl.INIT_VIDEO)
}

func sdlQuit() { sdl.Quit() }

func sdlCreateWindow(title string, w, h int, decorated bool) (unsafe.Pointer, error) {
	flags := sdl.WINDOW_RESIZABLE | sdl.WINDOW_HIGH_PIXEL_DENSITY | sdl.WindowFlags(sdlExtraWindowFlags())
	if !decorated {
		flags |= sdl.WINDOW_BORDERLESS
	}
	win, err := sdl.CreateWindow(title, w, h, flags)
	if err != nil {
		return nil, err
	}
	return unsafe.Pointer(win), nil
}

func sdlDestroyWindow(p unsafe.Pointer) {
	if p == nil {
		return
	}
	sdlFreeCursors()
	sdlFreeMetalView()
	(*sdl.Window)(p).Destroy()
}

func sdlVideoDriver() string { return sdl.GetCurrentVideoDriver() }

func sdlPollEvent() (rawEvent, bool) {
	var e sdl.Event
	for sdl.PollEvent(&e) {
		if ev, ok := translateSDLEvent(&e); ok {
			return ev, true
		}
	}
	return rawEvent{}, false
}

func sdlWaitEvent() (rawEvent, bool) {
	for {
		var e sdl.Event
		if err := sdl.WaitEvent(&e); err != nil {
			return rawEvent{}, false
		}
		if ev, ok := translateSDLEvent(&e); ok {
			return ev, true
		}
	}
}

func sdlWaitEventTimeout(ms int) (rawEvent, bool) {
	for {
		var e sdl.Event
		if !sdl.WaitEventTimeout(&e, int32(ms)) {
			return rawEvent{}, false
		}
		if ev, ok := translateSDLEvent(&e); ok {
			return ev, true
		}
	}
}

// translateSDLEvent turns one SDL event into the framework's rawEvent.
func translateSDLEvent(e *sdl.Event) (rawEvent, bool) {
	switch e.Type {
	case sdl.EVENT_QUIT:
		return rawEvent{kind: evQuit}, true
	case sdl.EVENT_WINDOW_CLOSE_REQUESTED:
		return rawEvent{kind: evClose}, true
	case sdl.EVENT_WINDOW_PIXEL_SIZE_CHANGED:
		w := e.WindowEvent()
		return rawEvent{kind: evResize, width: int(w.Data1), height: int(w.Data2)}, true
	case sdl.EVENT_WINDOW_EXPOSED:
		return rawEvent{kind: evExpose}, true
	case sdl.EVENT_WINDOW_FOCUS_GAINED:
		return rawEvent{kind: evFocusIn}, true
	case sdl.EVENT_WINDOW_FOCUS_LOST:
		return rawEvent{kind: evFocusOut}, true
	case sdl.EVENT_WINDOW_MOUSE_LEAVE:
		return rawEvent{kind: evMouseLeave}, true
	case sdl.EVENT_MOUSE_MOTION:
		m := e.MouseMotionEvent()
		return rawEvent{kind: evMotion, x: float64(m.X), y: float64(m.Y)}, true
	case sdl.EVENT_MOUSE_BUTTON_DOWN, sdl.EVENT_MOUSE_BUTTON_UP:
		b := e.MouseButtonEvent()
		return rawEvent{kind: evButton, x: float64(b.X), y: float64(b.Y), button: int(b.Button) - 1, down: b.Down}, true
	case sdl.EVENT_MOUSE_WHEEL:
		w := e.MouseWheelEvent()
		return rawEvent{kind: evWheel, wheelX: float64(w.X), wheelY: float64(w.Y)}, true
	case sdl.EVENT_KEY_DOWN:
		k := e.KeyboardEvent()
		return rawEvent{kind: evKey, scancode: int(k.Scancode), mod: int(k.Mod)}, true
	case sdl.EVENT_TEXT_INPUT:
		return rawEvent{kind: evText, text: e.TextInputEvent().Text}, true
	case sdl.EVENT_TEXT_EDITING:
		t := e.TextEditingEvent()
		return rawEvent{kind: evEditing, text: t.Text, caret: int(t.Start)}, true
	case sdl.EVENT_USER:
		return rawEvent{kind: evUser}, true
	case sdl.EVENT_SYSTEM_THEME_CHANGED:
		return rawEvent{kind: evTheme}, true
	case sdl.EVENT_LOCALE_CHANGED:
		return rawEvent{kind: evLocale}, true
	case sdl.EVENT_PINCH_UPDATE:
		return rawEvent{kind: evPinch, scale: float64(pinchScale(e))}, true
	}
	return rawEvent{}, false
}

// pinchScale reads SDL_PinchFingerEvent.scale. go-sdl3 declares the struct but
// has no accessor, so the field is read at its ABI offset: type, reserved,
// timestamp, then the float at byte 16.
func pinchScale(e *sdl.Event) float32 {
	return *(*float32)(unsafe.Add(unsafe.Pointer(e), 16))
}

func sdlPushUserEvent() {
	var e sdl.Event
	e.Type = sdl.EVENT_USER
	_ = sdl.PushEvent(&e)
}

var puregoCursors [int(sdl.SYSTEM_CURSOR_COUNT)]*sdl.Cursor
var puregoCursorHidden bool

func sdlSetCursor(kind int) {
	if kind == int(sdl.SYSTEM_CURSOR_COUNT) {
		if !puregoCursorHidden {
			sdl.HideCursor()
			puregoCursorHidden = true
		}
		return
	}
	if puregoCursorHidden {
		sdl.ShowCursor()
		puregoCursorHidden = false
	}
	if kind < 0 || kind >= len(puregoCursors) {
		return
	}
	if puregoCursors[kind] == nil {
		c, err := sdl.CreateSystemCursor(sdl.SystemCursor(kind))
		if err != nil {
			return
		}
		puregoCursors[kind] = c
	}
	sdl.SetCursor(puregoCursors[kind])
}

func sdlFreeCursors() {
	for i, c := range puregoCursors {
		if c != nil {
			c.Destroy()
			puregoCursors[i] = nil
		}
	}
	if puregoCursorHidden {
		sdl.ShowCursor()
		puregoCursorHidden = false
	}
}

func sdlWindowSizePixels(p unsafe.Pointer) (int, int) {
	w, h, err := (*sdl.Window)(p).SizeInPixels()
	if err != nil {
		return 0, 0
	}
	return int(w), int(h)
}

func sdlWindowSize(p unsafe.Pointer) (int, int) {
	w, h, err := (*sdl.Window)(p).Size()
	if err != nil {
		return 0, 0
	}
	return int(w), int(h)
}

func sdlWindowScale(p unsafe.Pointer) float32 {
	s, err := (*sdl.Window)(p).PixelDensity()
	if err != nil || s <= 0 {
		return 1
	}
	return s
}

func sdlWindowFlags(p unsafe.Pointer) uint64 { return uint64((*sdl.Window)(p).Flags()) }

func sdlSetTitle(p unsafe.Pointer, title string) { _ = (*sdl.Window)(p).SetTitle(title) }

func sdlMinimize(p unsafe.Pointer) { _ = (*sdl.Window)(p).Minimize() }
func sdlMaximize(p unsafe.Pointer) { _ = (*sdl.Window)(p).Maximize() }
func sdlRestore(p unsafe.Pointer)  { _ = (*sdl.Window)(p).Restore() }

func sdlSetResizable(p unsafe.Pointer, on bool) { _ = (*sdl.Window)(p).SetResizable(on) }

func sdlSetWindowSize(p unsafe.Pointer, w, h int) {
	_ = (*sdl.Window)(p).SetSize(int32(w), int32(h))
}

func sdlSetSizeLimits(p unsafe.Pointer, minW, minH, maxW, maxH int) {
	win := (*sdl.Window)(p)
	_ = win.SetMinimumSize(int32(minW), int32(minH))
	_ = win.SetMaximumSize(int32(maxW), int32(maxH))
}

func sdlSetFullscreen(p unsafe.Pointer, on bool) { _ = (*sdl.Window)(p).SetFullscreen(on) }

func sdlSystemLanguage() string {
	locales, err := sdl.GetPreferredLocales()
	if err != nil || len(locales) == 0 {
		return ""
	}
	return locales[0].Language
}

var puregoHitTest sdl.HitTest

func sdlInstallHitTest(p unsafe.Pointer) {
	win := (*sdl.Window)(p)
	puregoHitTest = sdl.NewHitTest(func(_ *sdl.Window, area *sdl.Point) sdl.HitTestResult {
		if currentHitTest == nil {
			return sdl.HITTEST_NORMAL
		}
		return sdl.HitTestResult(currentHitTest(float64(area.X), float64(area.Y)))
	})
	_ = win.SetHitTest(puregoHitTest)
}

func sdlStartTextInput(p unsafe.Pointer) error { return (*sdl.Window)(p).StartTextInput() }
func sdlStopTextInput(p unsafe.Pointer)        { _ = (*sdl.Window)(p).StopTextInput() }

func sdlSetTextInputArea(p unsafe.Pointer, x, y, w, h int) {
	_ = (*sdl.Window)(p).SetTextInputArea(&sdl.Rect{X: int32(x), Y: int32(y), W: int32(w), H: int32(h)}, 0)
}

func sdlModState() int { return int(sdl.GetModState()) }

func sdlSystemTheme() int {
	switch sdl.GetSystemTheme() {
	case sdl.SYSTEM_THEME_LIGHT:
		return 1
	case sdl.SYSTEM_THEME_DARK:
		return 2
	}
	return 0
}

func sdlClipboard() string {
	// The clipboard can be reached without a window (a test, or a copy before
	// the first frame), so the library may not be loaded yet. A machine without
	// SDL gets an empty clipboard instead of a nil function call.
	if loadSDL() != nil {
		return ""
	}
	s, err := sdl.GetClipboardText()
	if err != nil {
		return ""
	}
	return s
}

func sdlSetClipboard(s string) {
	if loadSDL() != nil {
		return
	}
	_ = sdl.SetClipboardText(s)
}

func sdlWaylandHandles(p unsafe.Pointer) (display, surface unsafe.Pointer) {
	props, err := (*sdl.Window)(p).Properties()
	if err != nil {
		return nil, nil
	}
	return unsafe.Pointer(props.PointerProperty(sdl.PROP_WINDOW_WAYLAND_DISPLAY_POINTER)),
		unsafe.Pointer(props.PointerProperty(sdl.PROP_WINDOW_WAYLAND_SURFACE_POINTER))
}

func sdlX11Handles(p unsafe.Pointer) (display unsafe.Pointer, window uintptr) {
	props, err := (*sdl.Window)(p).Properties()
	if err != nil {
		return nil, 0
	}
	return unsafe.Pointer(props.PointerProperty(sdl.PROP_WINDOW_X11_DISPLAY_POINTER)),
		uintptr(props.NumberProperty(sdl.PROP_WINDOW_X11_WINDOW_NUMBER, 0))
}

func sdlWin32Handles(p unsafe.Pointer) (instance, hwnd unsafe.Pointer) {
	props, err := (*sdl.Window)(p).Properties()
	if err != nil {
		return nil, nil
	}
	return unsafe.Pointer(props.PointerProperty(sdl.PROP_WINDOW_WIN32_INSTANCE_POINTER)),
		unsafe.Pointer(props.PointerProperty(sdl.PROP_WINDOW_WIN32_HWND_POINTER))
}

// The window flags the rest of the platform compares against.
const (
	flagHidden     = uint64(sdl.WINDOW_HIDDEN)
	flagMinimized  = uint64(sdl.WINDOW_MINIMIZED)
	flagMaximized  = uint64(sdl.WINDOW_MAXIMIZED)
	flagFullscreen = uint64(sdl.WINDOW_FULLSCREEN)
)

// Key modifiers and scancodes, named the way the input layer expects.
const (
	sdlKmodShift = int(sdl.KMOD_SHIFT)
	sdlKmodCtrl  = int(sdl.KMOD_CTRL)
	sdlKmodAlt   = int(sdl.KMOD_ALT)
	sdlKmodGui   = int(sdl.KMOD_GUI)

	scancodeA         = int(sdl.SCANCODE_A)
	scancodeC         = int(sdl.SCANCODE_C)
	scancodeV         = int(sdl.SCANCODE_V)
	scancodeX         = int(sdl.SCANCODE_X)
	scancodeBackspace = int(sdl.SCANCODE_BACKSPACE)
	scancodeDelete    = int(sdl.SCANCODE_DELETE)
	scancodeDown      = int(sdl.SCANCODE_DOWN)
	scancodeEnd       = int(sdl.SCANCODE_END)
	scancodeEscape    = int(sdl.SCANCODE_ESCAPE)
	scancodeHome      = int(sdl.SCANCODE_HOME)
	scancodeKPEnter   = int(sdl.SCANCODE_KP_ENTER)
	scancodeLeft      = int(sdl.SCANCODE_LEFT)
	scancodeReturn    = int(sdl.SCANCODE_RETURN)
	scancodeRight     = int(sdl.SCANCODE_RIGHT)
	scancodeSpace     = int(sdl.SCANCODE_SPACE)
	scancodeTab       = int(sdl.SCANCODE_TAB)
	scancodeUp        = int(sdl.SCANCODE_UP)
)
