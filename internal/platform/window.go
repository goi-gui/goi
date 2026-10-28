// Package platform owns the GLFW window, the native handle it exposes for
// WebGPU, and the translation of input into framework events. Only this package
// and render may import GLFW or a graphics API.
package platform

import (
	"errors"
	"runtime"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// Input receives raw input translated from GLFW, in logical pixels.
type Input interface {
	MouseMove(x, y float64)
	MouseDown(x, y float64, button int, shift bool)
	MouseUp(x, y float64, button int)
	Scroll(dx, dy float64)
	Key(key Key, mods Mods)
	Char(r rune)
	Focus(focused bool)
	Resize(width, height int, scale float32)
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

// Window is a native window with no graphics context of its own: WebGPU draws
// into it through the handles below.
type Window struct {
	win    *glfw.Window
	input  Input
	width  int
	height int
	scale  float32

	// cursor position, in logical pixels, kept here so press and release can
	// report the same point.
	cx, cy float64
}

// New creates the window. Call ShimInit once on the main goroutine first.
func New(title string, width, height int) (*Window, error) {
	// The surface must exist before WebGPU asks for its handles, and some
	// compositors only create the wl_surface once the window is shown, so the
	// window is shown immediately.
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	glfw.WindowHint(glfw.ScaleToMonitor, glfw.True)
	glfw.WindowHint(glfw.Visible, glfw.True)
	w, err := glfw.CreateWindow(width, height, title, nil, nil)
	if err != nil {
		return nil, err
	}
	p := &Window{win: w, width: width, height: height, scale: 1}
	xs, _ := w.GetContentScale()
	if xs > 0 {
		p.scale = xs
	}
	w.SetUserPointer(nil)
	return p, nil
}

// Init initializes GLFW and locks the goroutine, as macOS requires.
func Init() error {
	runtime.LockOSThread()
	return glfw.Init()
}

// Terminate releases GLFW. Call after every window is destroyed.
func Terminate() { glfw.Terminate() }

// SetInput installs the input sink.
func (p *Window) SetInput(in Input) {
	p.input = in
	p.win.SetMouseButtonCallback(func(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
		if p.input == nil {
			return
		}
		x, y := p.cursorLogical()
		switch action {
		case glfw.Press:
			p.input.MouseDown(x, y, int(button), translateMods(mods).Shift)
		case glfw.Release:
			p.input.MouseUp(x, y, int(button))
		}
	})
	p.win.SetCursorPosCallback(func(w *glfw.Window, x, y float64) {
		if p.input != nil {
			p.input.MouseMove(x, y)
		}
	})
	p.win.SetScrollCallback(func(w *glfw.Window, dx, dy float64) {
		if p.input != nil {
			p.input.Scroll(dx, dy)
		}
	})
	p.win.SetKeyCallback(func(w *glfw.Window, key glfw.Key, _ int, action glfw.Action, mods glfw.ModifierKey) {
		if p.input == nil || action == glfw.Release {
			return
		}
		p.input.Key(translateKey(key), translateMods(mods))
	})
	p.win.SetCharCallback(func(w *glfw.Window, r rune) {
		if p.input != nil {
			p.input.Char(r)
		}
	})
	p.win.SetFramebufferSizeCallback(func(w *glfw.Window, fw, fh int) {
		if p.input == nil || fw == 0 || fh == 0 {
			return
		}
		scale := p.contentScale()
		p.input.Resize(fw, fh, scale)
	})
	p.win.SetFocusCallback(func(w *glfw.Window, focused bool) {
		if p.input != nil {
			p.input.Focus(focused)
		}
	})
	p.win.SetCloseCallback(func(w *glfw.Window) {
		if p.input != nil {
			p.input.Close()
		}
	})
}

func (p *Window) cursorLogical() (float64, float64) {
	x, y := p.win.GetCursorPos()
	p.cx, p.cy = x, y
	return x, y
}

func (p *Window) contentScale() float32 {
	xs, _ := p.win.GetContentScale()
	if xs <= 0 {
		return 1
	}
	p.scale = xs
	return xs
}

// FramebufferSize returns the device-pixel size of the client area.
func (p *Window) FramebufferSize() (int, int) {
	return p.win.GetFramebufferSize()
}

// LogicalSize returns the client size in logical pixels.
func (p *Window) LogicalSize() (float64, float64) {
	fw, fh := p.win.GetFramebufferSize()
	s := float64(p.contentScale())
	return float64(fw) / s, float64(fh) / s
}

// Scale returns the content scale.
func (p *Window) Scale() float32 { return p.contentScale() }

// SurfaceKind says which native handle pair SurfaceHandles returned.
type SurfaceKind int

// Surface kinds.
const (
	SurfaceWayland SurfaceKind = iota
	SurfaceX11
	SurfaceWin32
	SurfaceCocoa
)

// Visible reports whether the window is shown (some compositors report a nil
// surface while minimized).
func (p *Window) Visible() bool {
	return p.win.GetAttrib(glfw.Iconified) == glfw.False && p.win.GetAttrib(glfw.Visible) == glfw.True
}

// ShouldClose reports whether the user asked to close the window.
func (p *Window) ShouldClose() bool { return p.win.ShouldClose() }

// Destroy tears the window down.
func (p *Window) Destroy() { p.win.Destroy() }

// PollEvents processes pending input without blocking.
func PollEvents() { glfw.PollEvents() }

// WaitEvents blocks until input arrives. PostEmptyEvent wakes it.
func WaitEvents() { glfw.WaitEvents() }

// PostEmptyEvent wakes WaitEvents from another goroutine.
func PostEmptyEvent() { glfw.PostEmptyEvent() }

// SetTitle updates the window title.
func (p *Window) SetTitle(title string) { p.win.SetTitle(title) }

// ErrNoGLFW reports a window created before Init.
var ErrNoGLFW = errors.New("platform: GLFW is not initialized")

func translateMods(m glfw.ModifierKey) Mods {
	return Mods{
		Shift: m&glfw.ModShift != 0,
		Ctrl:  m&glfw.ModControl != 0,
		Alt:   m&glfw.ModAlt != 0,
		Super: m&glfw.ModSuper != 0,
	}
}

func translateKey(k glfw.Key) Key {
	switch k {
	case glfw.KeyBackspace:
		return KeyBackspace
	case glfw.KeyDelete:
		return KeyDelete
	case glfw.KeyLeft:
		return KeyLeft
	case glfw.KeyRight:
		return KeyRight
	case glfw.KeyHome:
		return KeyHome
	case glfw.KeyEnd:
		return KeyEnd
	case glfw.KeyEnter, glfw.KeyKPEnter:
		return KeyEnter
	case glfw.KeyTab:
		return KeyTab
	case glfw.KeyEscape:
		return KeyEscape
	case glfw.KeyUp:
		return KeyUp
	case glfw.KeyDown:
		return KeyDown
	case glfw.KeyA:
		return KeyA
	case glfw.KeyC:
		return KeyC
	case glfw.KeyV:
		return KeyV
	case glfw.KeyX:
		return KeyX
	}
	return KeyNone
}
