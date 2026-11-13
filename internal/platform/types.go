// Package platform owns the window the framework draws into, the native handle
// it exposes for WebGPU, and the translation of input into framework events.
// Only this package and render may import a windowing library or a graphics
// API.
//
// The native build wraps SDL3 (sdl.go, window.go). The browser build talks to a
// canvas directly (window_js.go), so a design compiles to wasm unchanged.
package platform

// This file holds the types both builds share: the input sink, the key and
// cursor names, and the surface kinds.

// Input receives raw input translated from the platform, in logical pixels.
type Input interface {
	MouseMove(x, y float64)
	MouseDown(x, y float64, button int, shift bool)
	MouseUp(x, y float64, button int)
	// MouseLeave reports that the pointer left the window, so hover state and
	// anything that follows it can be cleared.
	MouseLeave()
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
	// LocaleChanged reports that the desktop's language setting changed, so a
	// design reading `app.systemLanguage` can pick it up.
	LocaleChanged()
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

// Cursor is the shape the pointer takes over the client area. The values match
// SDL_SystemCursor's order, so the shim can pass one straight to
// SDL_CreateSystemCursor; None is the first value past that enum.
//
// Every CSS cursor name maps to one of these; see ui.CursorFor.
type Cursor int

// Cursor kinds.
const (
	CursorArrow Cursor = iota // default: the system arrow
	CursorText                // I-beam
	CursorWait                // busy
	CursorCrosshair
	CursorProgress   // busy but still interactive
	CursorResizeNWSE // ↘↖
	CursorResizeNESW // ↙↗
	CursorResizeEW   // ↔
	CursorResizeNS   // ↕
	CursorMove
	CursorNotAllowed
	CursorPointer // pointing hand, for links and buttons
	CursorResizeNW
	CursorResizeN
	CursorResizeNE
	CursorResizeE
	CursorResizeSE
	CursorResizeS
	CursorResizeSW
	CursorResizeW
	// CursorNone hides the pointer, for a design that draws its own.
	CursorNone
)

// SurfaceKind says which native handle pair SurfaceHandles returned. The order
// matches render.TargetKind, which is how the driver passes one straight
// through.
type SurfaceKind int

// Surface kinds.
const (
	SurfaceWayland SurfaceKind = iota
	SurfaceX11
	SurfaceWin32
	SurfaceMetal
	// SurfaceWeb is a browser canvas. The handle is unused: the renderer takes
	// the page's canvas.
	SurfaceWeb
)

// Theme is the desktop's light/dark preference.
type Theme uint8

// Theme values. ThemeUnknown means the platform does not report one.
const (
	ThemeUnknown Theme = iota
	ThemeLight
	ThemeDark
)
