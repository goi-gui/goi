package platform

/*
#cgo pkg-config: sdl3
#include <stdlib.h>
#include "sdl_shim.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

// event kinds mirror the GOI_EV_* enum in sdl_shim.h.
const (
	_ = iota // evNone: an event the framework does not use
	evQuit
	evClose
	evResize
	evExpose
	evFocusIn
	evFocusOut
	evMotion
	evButton
	evWheel
	evKey
	evText
	evEditing
	evUser
	evTheme
)

// rawEvent is one translated SDL event.
type rawEvent struct {
	kind     int
	width    int
	height   int
	x, y     float64
	button   int
	down     bool
	wheelX   float64
	wheelY   float64
	scancode int
	mod      int
	text     string
	caret    int
}

// sdlError turns SDL's last error into a Go error.
func sdlError(what string) error {
	msg := C.GoString(C.SDL_GetError())
	if msg == "" {
		msg = "unknown SDL error"
	}
	return errors.New(what + ": " + msg)
}

func sdlInit() error {
	if !C.SDL_Init(C.SDL_INIT_VIDEO) {
		return sdlError("init video")
	}
	return nil
}

func sdlQuit() { C.SDL_Quit() }

func sdlCreateWindow(title string, w, h int, decorated bool) (unsafe.Pointer, error) {
	ctitle := C.CString(title)
	defer C.free(unsafe.Pointer(ctitle))
	flags := C.SDL_WindowFlags(C.SDL_WINDOW_RESIZABLE) | C.SDL_WindowFlags(C.SDL_WINDOW_HIGH_PIXEL_DENSITY)
	if !decorated {
		// A borderless window has no platform title bar; the design draws its
		// own and the hit test supplies drag and resize edges.
		flags |= C.SDL_WindowFlags(C.SDL_WINDOW_BORDERLESS)
	}
	win := C.SDL_CreateWindow(ctitle, C.int(w), C.int(h), flags)
	if win == nil {
		return nil, sdlError("create window")
	}
	return unsafe.Pointer(win), nil
}

func sdlDestroyWindow(p unsafe.Pointer) {
	if p == nil {
		return
	}
	C.goiFreeCursors()
	C.SDL_DestroyWindow((*C.SDL_Window)(p))
}

func sdlVideoDriver() string { return C.GoString(C.SDL_GetCurrentVideoDriver()) }

func sdlPollEvent() (rawEvent, bool) {
	var e C.goiEvent
	if C.goiPollEvent(&e) == 0 {
		return rawEvent{}, false
	}
	return translateEvent(&e), true
}

func sdlWaitEvent() (rawEvent, bool) {
	var e C.goiEvent
	if C.goiWaitEvent(&e) == 0 {
		return rawEvent{}, false
	}
	return translateEvent(&e), true
}

func translateEvent(e *C.goiEvent) rawEvent {
	return rawEvent{
		kind:     int(e._type),
		width:    int(e.width),
		height:   int(e.height),
		x:        float64(e.x),
		y:        float64(e.y),
		button:   int(e.button),
		down:     e.down != 0,
		wheelX:   float64(e.wheelX),
		wheelY:   float64(e.wheelY),
		scancode: int(e.scancode),
		mod:      int(e.mod),
		text:     C.GoString(e.text),
		caret:    int(e.caret),
	}
}

func sdlPushUserEvent() { C.goiPushUserEvent() }

func sdlSetCursor(kind int) { C.goiSetCursorKind(C.int(kind)) }

func sdlWindowSizePixels(p unsafe.Pointer) (int, int) {
	var w, h C.int
	C.goiWindowSizePixels((*C.SDL_Window)(p), &w, &h)
	return int(w), int(h)
}

func sdlWindowSize(p unsafe.Pointer) (int, int) {
	var w, h C.int
	if !C.SDL_GetWindowSize((*C.SDL_Window)(p), &w, &h) {
		return 0, 0
	}
	return int(w), int(h)
}

func sdlWindowScale(p unsafe.Pointer) float32 {
	return float32(C.goiWindowScale((*C.SDL_Window)(p)))
}

func sdlWindowFlags(p unsafe.Pointer) uint64 {
	return uint64(C.SDL_GetWindowFlags((*C.SDL_Window)(p)))
}

func sdlSetTitle(p unsafe.Pointer, title string) {
	ctitle := C.CString(title)
	defer C.free(unsafe.Pointer(ctitle))
	C.SDL_SetWindowTitle((*C.SDL_Window)(p), ctitle)
}

func sdlMinimize(p unsafe.Pointer) { C.SDL_MinimizeWindow((*C.SDL_Window)(p)) }
func sdlMaximize(p unsafe.Pointer) { C.SDL_MaximizeWindow((*C.SDL_Window)(p)) }
func sdlRestore(p unsafe.Pointer)  { C.SDL_RestoreWindow((*C.SDL_Window)(p)) }

func sdlInstallHitTest(p unsafe.Pointer) { C.goiInstallHitTest((*C.SDL_Window)(p)) }

func sdlStartTextInput(p unsafe.Pointer) error {
	if !C.SDL_StartTextInput((*C.SDL_Window)(p)) {
		return sdlError("start text input")
	}
	return nil
}

func sdlStopTextInput(p unsafe.Pointer) { C.SDL_StopTextInput((*C.SDL_Window)(p)) }

func sdlSetTextInputArea(p unsafe.Pointer, x, y, w, h int) {
	C.goiSetTextInputArea((*C.SDL_Window)(p), C.int(x), C.int(y), C.int(w), C.int(h))
}

func sdlModState() int { return int(C.SDL_GetModState()) }

// sdlSystemTheme reports the desktop preference: 0 unknown, 1 light, 2 dark.
func sdlSystemTheme() int { return int(C.goiSystemTheme()) }

func sdlClipboard() string {
	p := C.goiClipboardText()
	if p == nil {
		return ""
	}
	defer C.SDL_free(unsafe.Pointer(p))
	return C.GoString(p)
}

func sdlSetClipboard(s string) {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	C.SDL_SetClipboardText(cs)
}

// Wayland and X11 handles. SDL reports whichever pair belongs to its current
// video driver.
func sdlWaylandHandles(p unsafe.Pointer) (display, surface unsafe.Pointer) {
	w := (*C.SDL_Window)(p)
	return C.goiWaylandDisplay(w), C.goiWaylandSurface(w)
}

func sdlX11Handles(p unsafe.Pointer) (display unsafe.Pointer, window uintptr) {
	w := (*C.SDL_Window)(p)
	return C.goiX11Display(w), uintptr(C.goiX11Window(w))
}

// Window flag bits, mirrored so the rest of the package needs no cgo.
const (
	flagHidden    = uint64(C.SDL_WINDOW_HIDDEN)
	flagMinimized = uint64(C.SDL_WINDOW_MINIMIZED)
	flagMaximized = uint64(C.SDL_WINDOW_MAXIMIZED)
)

// Modifier and scancode values, mirrored so the event translation in window.go
// stays free of cgo.
const (
	sdlKmodShift = int(C.SDL_KMOD_SHIFT)
	sdlKmodCtrl  = int(C.SDL_KMOD_CTRL)
	sdlKmodAlt   = int(C.SDL_KMOD_ALT)
	sdlKmodGui   = int(C.SDL_KMOD_GUI)
)

const (
	scancodeA         = int(C.SDL_SCANCODE_A)
	scancodeC         = int(C.SDL_SCANCODE_C)
	scancodeV         = int(C.SDL_SCANCODE_V)
	scancodeX         = int(C.SDL_SCANCODE_X)
	scancodeBackspace = int(C.SDL_SCANCODE_BACKSPACE)
	scancodeDelete    = int(C.SDL_SCANCODE_DELETE)
	scancodeDown      = int(C.SDL_SCANCODE_DOWN)
	scancodeEnd       = int(C.SDL_SCANCODE_END)
	scancodeEscape    = int(C.SDL_SCANCODE_ESCAPE)
	scancodeHome      = int(C.SDL_SCANCODE_HOME)
	scancodeKPEnter   = int(C.SDL_SCANCODE_KP_ENTER)
	scancodeLeft      = int(C.SDL_SCANCODE_LEFT)
	scancodeReturn    = int(C.SDL_SCANCODE_RETURN)
	scancodeRight     = int(C.SDL_SCANCODE_RIGHT)
	scancodeSpace     = int(C.SDL_SCANCODE_SPACE)
	scancodeTab       = int(C.SDL_SCANCODE_TAB)
	scancodeUp        = int(C.SDL_SCANCODE_UP)
)

// goiHitTestXY is the C entry point for SDL's hit-test callback.
//
//export goiHitTestXY
func goiHitTestXY(x, y C.int) C.int {
	if currentHitTest == nil {
		return C.int(HitTestNormal)
	}
	return C.int(currentHitTest(float64(x), float64(y)))
}
