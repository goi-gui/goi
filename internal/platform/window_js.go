//go:build js

package platform

// The browser build: one canvas is the window, DOM events are the input, and
// WebGPU draws into it through the canvas the renderer takes from surface_js.go.
//
// There is no window manager, so the properties only a desktop has (minimize,
// resize edges, size limits) keep their state and draw nothing.

import (
	"fmt"
	"sync"
	"syscall/js"
	"time"
)

// Window is the canvas the app draws into.
type Window struct {
	canvas js.Value
	input  Input

	// width and height are logical pixels; scale is the device pixel ratio
	// between them and the canvas backing store.
	width, height int
	scale         float32

	closing bool

	// queue holds the callbacks a DOM event produced, drained by PollEvents so
	// a frame never runs in the middle of a DOM callback. A DOM callback is a
	// goroutine of its own, so the slice needs the lock.
	mu    sync.Mutex
	queue []func()
	// wake carries "there is work" to a waiting loop. A buffered channel with a
	// non-blocking send never blocks a DOM callback.
	wake chan struct{}

	// resizable, minimizable, maximizable and the size limits mirror the
	// design's window properties. The browser has no title bar to apply them
	// to, but a design that reads them back sees what it set.
	resizable, minimizable, maximizable bool
	minW, minH, maxW, maxH              int
	fullscreen, maximized, minimized    bool

	raf     js.Func
	stop    js.Func
	layouts js.Func
	// ime is the invisible textarea the browser's input method composes into.
	// A canvas cannot host a candidate window, so this element is what gives the
	// IME something to edit while the app still draws its own caret.
	ime       js.Value
	imeFuncs  []js.Func
	composing bool
	// pending is the size the page asked for before the app installed its
	// input sink, delivered by SetInput. A canvas that starts at the page's
	// size rather than the design's would otherwise keep the wrong size.
	pendingW, pendingH int
}

// Clipboard cache. navigator.clipboard.readText is async, and the framework
// reads the clipboard synchronously on Ctrl+V, so the page's own copy and paste
// events keep this in step. Text copied in another tab is therefore not
// readable until the page has seen a paste once.
var clipboard string

// Init is a no-op: the browser has no subsystem to start.
func Init() error { return nil }

// Terminate is a no-op.
func Terminate() {}

// New takes over the page's canvas, or creates one when the page has none, and
// starts the frame pump.
func New(title string, width, height int, decorated bool) (*Window, error) {
	doc := js.Global().Get("document")
	canvas := doc.Call("querySelector", "canvas")
	if canvas.IsNull() || canvas.IsUndefined() {
		canvas = doc.Call("createElement", "canvas")
		doc.Get("body").Call("appendChild", canvas)
	}
	if gpu := js.Global().Get("navigator").Get("gpu"); gpu.IsUndefined() || gpu.IsNull() {
		// A page on a browser without WebGPU gets a sentence instead of a blank
		// canvas: the framework cannot draw, and saying so is what keeps the
		// failure from looking like the app's own bug.
		msg := doc.Call("createElement", "p")
		msg.Set("textContent", "This browser has no WebGPU, so the app cannot draw. Try a current Chrome, Edge or Firefox.")
		msg.Get("style").Set("font", "16px system-ui, sans-serif")
		msg.Get("style").Set("padding", "24px")
		msg.Get("style").Set("color", "#333")
		doc.Get("body").Call("appendChild", msg)
		return nil, fmt.Errorf("platform: this browser has no WebGPU")
	}

	p := &Window{
		canvas:      canvas,
		width:       maxInt(width, 1),
		height:      maxInt(height, 1),
		scale:       1,
		resizable:   true,
		minimizable: true,
		maximizable: true,
		wake:        make(chan struct{}, 1),
	}
	if title != "" {
		doc.Set("title", title)
	}
	if dpr := js.Global().Get("devicePixelRatio"); dpr.Type() == js.TypeNumber && dpr.Float() > 0 {
		p.scale = float32(dpr.Float())
	}
	// The page's stylesheet decides the window size, so a full-window app only
	// needs `canvas { width: 100%; height: 100% }`. A page that says nothing
	// gets the design's size.
	rect := canvas.Call("getBoundingClientRect")
	if rect.Get("width").Float() >= 1 && rect.Get("height").Float() >= 1 {
		p.width = int(rect.Get("width").Float())
		p.height = int(rect.Get("height").Float())
	} else {
		style := canvas.Get("style")
		style.Set("width", fmt.Sprintf("%dpx", p.width))
		style.Set("height", fmt.Sprintf("%dpx", p.height))
	}
	p.applySize(p.width, p.height)
	p.createIME()
	p.listen()
	p.watchTheme()
	p.startFrames()
	return p, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// applySize sets the backing store to logical size times the pixel ratio and
// tells the app the framebuffer changed.
func (p *Window) applySize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	p.width, p.height = w, h
	p.canvas.Set("width", int(float64(w)*float64(p.scale)))
	p.canvas.Set("height", int(float64(h)*float64(p.scale)))
	fw, fh := p.FramebufferSize()
	if p.input == nil {
		p.pendingW, p.pendingH = fw, fh
		return
	}
	p.post(func(in Input) { in.Resize(fw, fh, p.scale) })
}

// post queues a callback for the next PollEvents, which is what keeps a frame
// from running inside a DOM handler.
func (p *Window) post(fn func(Input)) {
	p.mu.Lock()
	p.queue = append(p.queue, func() {
		if p.input != nil {
			fn(p.input)
		}
	})
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// listen registers the DOM handlers.
func (p *Window) listen() {
	c := p.canvas
	mouse := js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		x, y := p.local(e)
		p.post(func(in Input) { in.MouseMove(x, y) })
		return nil
	})
	c.Call("addEventListener", "mousemove", mouse)

	down := js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		x, y := p.local(e)
		button := e.Get("button").Int()
		shift := e.Get("shiftKey").Bool()
		p.post(func(in Input) { in.MouseDown(x, y, button, shift) })
		return nil
	})
	c.Call("addEventListener", "mousedown", down)

	up := js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		x, y := p.local(e)
		button := e.Get("button").Int()
		p.post(func(in Input) { in.MouseUp(x, y, button) })
		return nil
	})
	// A release outside the canvas still ends the drag, which is what a slider
	// pulled past the edge needs.
	js.Global().Call("addEventListener", "mouseup", up)

	leave := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		p.post(func(in Input) { in.MouseLeave() })
		return nil
	})
	c.Call("addEventListener", "mouseleave", leave)

	// The input method composes into the hidden textarea; what it produces is
	// the app's preedit and committed text, on the same path SDL's IME uses.
	if p.ime.Truthy() {
		start := js.FuncOf(func(_ js.Value, _ []js.Value) any {
			p.composing = true
			p.ime.Set("value", "")
			return nil
		})
		update := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			text := args[0].Get("data").String()
			p.post(func(in Input) { in.Preedit(text, len([]rune(text))) })
			return nil
		})
		end := js.FuncOf(func(_ js.Value, args []js.Value) any {
			p.composing = false
			p.ime.Set("value", "")
			if len(args) == 0 {
				return nil
			}
			text := args[0].Get("data").String()
			if text != "" {
				p.post(func(in Input) { in.CommitText(text) })
			}
			return nil
		})
		typed := js.FuncOf(func(_ js.Value, _ []js.Value) any {
			if p.composing {
				return nil
			}
			text := p.ime.Get("value").String()
			p.ime.Set("value", "")
			if text != "" {
				p.post(func(in Input) { in.CommitText(text) })
			}
			return nil
		})
		p.ime.Call("addEventListener", "compositionstart", start)
		p.ime.Call("addEventListener", "compositionupdate", update)
		p.ime.Call("addEventListener", "compositionend", end)
		p.ime.Call("addEventListener", "input", typed)
		p.imeFuncs = []js.Func{start, update, end, typed}
	}

	wheel := js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		// A wheel notch is 100 delta units in every browser; the framework
		// measures its scrolling in notches, so the sign is enough here.
		dy := -e.Get("deltaY").Float() / 100
		dx := e.Get("deltaX").Float() / 100
		shift := e.Get("shiftKey").Bool()
		p.post(func(in Input) { in.Scroll(dx, dy, shift) })
		e.Call("preventDefault")
		return nil
	})
	c.Call("addEventListener", "wheel", wheel, map[string]any{"passive": false})

	keydown := js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		// An IME owns the keyboard while it composes, and the composition
		// events above already carry the text.
		if e.Get("isComposing").Bool() {
			return nil
		}
		if e.Get("ctrlKey").Bool() || e.Get("metaKey").Bool() {
			e.Call("preventDefault")
		}
		key, text, handled := decodeKey(e)
		mods := Mods{
			Shift: e.Get("shiftKey").Bool(),
			Ctrl:  e.Get("ctrlKey").Bool() || e.Get("metaKey").Bool(),
			Alt:   e.Get("altKey").Bool(),
		}
		if key != KeyNone {
			p.post(func(in Input) { in.Key(key, mods) })
		}
		if text != "" {
			p.post(func(in Input) { in.CommitText(text) })
		}
		if handled {
			e.Call("preventDefault")
		}
		return nil
	})
	js.Global().Call("addEventListener", "keydown", keydown)

	focus := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		p.post(func(in Input) { in.Focus(true) })
		return nil
	})
	blur := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		p.post(func(in Input) { in.Focus(false) })
		return nil
	})
	js.Global().Call("addEventListener", "focus", focus)
	js.Global().Call("addEventListener", "blur", blur)

	// A copy or cut in the page is the one clipboard read a browser allows
	// synchronously, so it is what keeps Ctrl+V inside the app working.
	copyFn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		if sel := js.Global().Call("getSelection").String(); sel != "" {
			clipboard = sel
		}
		_ = e
		return nil
	})
	pasteFn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		if data := e.Get("clipboardData"); !data.IsUndefined() && !data.IsNull() {
			if t := data.Call("getData", "text/plain"); t.Type() == js.TypeString {
				clipboard = t.String()
			}
		}
		return nil
	})
	js.Global().Get("document").Call("addEventListener", "copy", copyFn)
	js.Global().Get("document").Call("addEventListener", "cut", copyFn)
	js.Global().Get("document").Call("addEventListener", "paste", pasteFn)
	p.stop = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		c.Call("removeEventListener", "mousemove", mouse)
		c.Call("removeEventListener", "mousedown", down)
		c.Call("removeEventListener", "mouseleave", leave)
		c.Call("removeEventListener", "wheel", wheel)
		js.Global().Call("removeEventListener", "mouseup", up)
		js.Global().Call("removeEventListener", "keydown", keydown)
		js.Global().Call("removeEventListener", "focus", focus)
		js.Global().Call("removeEventListener", "blur", blur)
		js.Global().Get("document").Call("removeEventListener", "copy", copyFn)
		js.Global().Get("document").Call("removeEventListener", "cut", copyFn)
		js.Global().Get("document").Call("removeEventListener", "paste", pasteFn)
		mouse.Release()
		down.Release()
		up.Release()
		leave.Release()
		wheel.Release()
		keydown.Release()
		focus.Release()
		blur.Release()
		copyFn.Release()
		pasteFn.Release()
		for _, f := range p.imeFuncs {
			f.Release()
		}
		return nil
	})

	// A stylesheet can make the canvas follow the page, which is how a
	// full-window app is written; the observer turns that into a resize.
	if ctor := js.Global().Get("ResizeObserver"); ctor.Type() == js.TypeFunction {
		cb := js.FuncOf(func(_ js.Value, args []js.Value) any {
			entries := args[0]
			if entries.Length() == 0 {
				return nil
			}
			rect := entries.Index(0).Get("contentRect")
			w, h := int(rect.Get("width").Float()), int(rect.Get("height").Float())
			if w != p.width || h != p.height {
				p.applySize(w, h)
				p.post(func(in Input) { in.Expose() })
			}
			return nil
		})
		js.Global().Get("ResizeObserver").New(cb).Call("observe", c)
		p.layouts = cb
	}
}

// local turns a DOM event position into logical canvas pixels.
func (p *Window) local(e js.Value) (float64, float64) {
	rect := p.canvas.Call("getBoundingClientRect")
	x := e.Get("clientX").Float() - rect.Get("left").Float()
	y := e.Get("clientY").Float() - rect.Get("top").Float()
	return x, y
}

// decodeKey maps a DOM keydown to a framework key. Text is not produced here;
// an `input` event on the hidden textarea commits it.
func decodeKey(e js.Value) (Key, string, bool) {
	key := e.Get("key")
	if key.Type() != js.TypeString {
		return KeyNone, "", false
	}
	return keyFor(key.String(), e.Get("ctrlKey").Bool() || e.Get("metaKey").Bool())
}

// keyFor is the pure half of decodeKey: a DOM key name plus the Ctrl/Meta
// state, in framework terms.
func keyFor(s string, ctrl bool) (Key, string, bool) {
	switch s {
	case "Backspace":
		return KeyBackspace, "", true
	case "Delete":
		return KeyDelete, "", true
	case "ArrowLeft":
		return KeyLeft, "", true
	case "ArrowRight":
		return KeyRight, "", true
	case "Home":
		return KeyHome, "", true
	case "End":
		return KeyEnd, "", true
	case "Enter":
		return KeyEnter, "", true
	case "Tab":
		return KeyTab, "", true
	case " ":
		return KeySpace, "", true
	case "Escape":
		return KeyEscape, "", true
	case "ArrowUp":
		return KeyUp, "", true
	case "ArrowDown":
		return KeyDown, "", true
	}
	if ctrl {
		switch s {
		case "a", "A":
			return KeyA, "", true
		case "c", "C":
			return KeyC, "", true
		case "v", "V":
			return KeyV, "", true
		case "x", "X":
			return KeyX, "", true
		}
		return KeyNone, "", false
	}
	// A printable key is left to the browser: it types into the hidden textarea
	// and the resulting `input` event commits the text, which is what makes
	// dead keys, AltGr and an input method all take the same path. Consuming it
	// here would type everything twice.
	return KeyNone, "", false
}

// startFrames drives the app from requestAnimationFrame: each display refresh
// posts an expose, which is what makes an animation run at the panel's rate.
func (p *Window) startFrames() {
	p.raf = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		if p.closing {
			return nil
		}
		// A hidden tab gets no animation frames; when it comes back the first
		// one repaints, so nothing is lost by stopping here.
		if doc := js.Global().Get("document"); !doc.Get("hidden").Bool() {
			p.post(func(in Input) { in.Expose() })
		}
		js.Global().Call("requestAnimationFrame", p.raf)
		return nil
	})
	js.Global().Call("requestAnimationFrame", p.raf)
}

// watchTheme follows the platform's light/dark preference.
func (p *Window) watchTheme() {
	query := js.Global().Call("matchMedia", "(prefers-color-scheme: dark)")
	query.Call("addEventListener", "change", js.FuncOf(func(_ js.Value, _ []js.Value) any {
		p.post(func(in Input) { in.ThemeChanged() })
		return nil
	}))
}

// SetInput installs the input sink and delivers the size the page settled on
// before the sink existed.
func (p *Window) SetInput(in Input) {
	p.input = in
	if p.pendingW > 0 && p.pendingH > 0 {
		w, h := p.pendingW, p.pendingH
		p.pendingW, p.pendingH = 0, 0
		in.Resize(w, h, p.scale)
	}
}

// SetHitTest is a no-op: the page, not the app, owns the window edges.
func (p *Window) SetHitTest(fn func(x, y float64) HitTest) {}

// PollEvents runs every callback the DOM produced since the last call.
func (p *Window) PollEvents() {
	for {
		p.mu.Lock()
		if len(p.queue) == 0 {
			p.mu.Unlock()
			return
		}
		fn := p.queue[0]
		p.queue = p.queue[1:]
		p.mu.Unlock()
		fn()
	}
}

// WaitEvents blocks until a DOM event or the next animation frame. Blocking on
// a channel is what lets the browser's event loop run while the app is idle.
func (p *Window) WaitEvents() {
	<-p.wake
	p.PollEvents()
}

// WaitEventsTimeout is WaitEvents with a deadline, so an idle app yields to the
// browser instead of spinning.
func (p *Window) WaitEventsTimeout(d time.Duration) bool {
	select {
	case <-p.wake:
		p.PollEvents()
		return true
	case <-time.After(d):
		p.PollEvents()
		return false
	}
}

// SetCursor changes the pointer shape.
func (p *Window) SetCursor(c Cursor) {
	p.canvas.Get("style").Set("cursor", cursorCSS(c))
}

// cursorCSS maps the framework's names to CSS cursors.
func cursorCSS(c Cursor) string {
	switch c {
	case CursorText:
		return "text"
	case CursorWait:
		return "wait"
	case CursorCrosshair:
		return "crosshair"
	case CursorProgress:
		return "progress"
	case CursorResizeNWSE:
		return "nwse-resize"
	case CursorResizeNESW:
		return "nesw-resize"
	case CursorResizeEW:
		return "ew-resize"
	case CursorResizeNS:
		return "ns-resize"
	case CursorMove:
		return "move"
	case CursorNotAllowed:
		return "not-allowed"
	case CursorPointer:
		return "pointer"
	case CursorResizeNW:
		return "nw-resize"
	case CursorResizeN:
		return "n-resize"
	case CursorResizeNE:
		return "ne-resize"
	case CursorResizeE:
		return "e-resize"
	case CursorResizeSE:
		return "se-resize"
	case CursorResizeS:
		return "s-resize"
	case CursorResizeSW:
		return "sw-resize"
	case CursorResizeW:
		return "w-resize"
	case CursorNone:
		return "none"
	}
	return "default"
}

// SetTextInput moves the hidden textarea to the caret and focuses it, which is
// what lets the browser's input method compose; without a focused editable
// element there is nothing for a candidate window to attach to.
func (p *Window) SetTextInput(active bool, x, y, w, h float64) {
	if !p.ime.Truthy() {
		return
	}
	if !active {
		if p.composing {
			return
		}
		p.ime.Set("value", "")
		p.ime.Call("blur")
		return
	}
	rect := p.canvas.Call("getBoundingClientRect")
	style := p.ime.Get("style")
	style.Set("left", fmt.Sprintf("%.1fpx", rect.Get("left").Float()+x))
	style.Set("top", fmt.Sprintf("%.1fpx", rect.Get("top").Float()+y+w*0.5))
	if doc := js.Global().Get("document"); doc.Get("activeElement").Equal(p.ime) {
		return
	}
	p.ime.Call("focus")
}

// createIME builds the invisible textarea. It is one pixel tall and fully
// transparent, so the candidate window follows the caret while the app's own
// caret is the one that shows.
func (p *Window) createIME() {
	doc := js.Global().Get("document")
	ta := doc.Call("createElement", "textarea")
	ta.Set("autocomplete", "off")
	ta.Set("autocorrect", "off")
	ta.Set("autocapitalize", "off")
	ta.Set("spellcheck", false)
	ta.Set("aria-hidden", "true")
	ta.Set("tabIndex", -1)
	style := ta.Get("style")
	style.Set("position", "fixed")
	style.Set("left", "0")
	style.Set("top", "0")
	style.Set("width", "1px")
	style.Set("height", "1px")
	style.Set("padding", "0")
	style.Set("border", "none")
	style.Set("outline", "none")
	style.Set("resize", "none")
	style.Set("opacity", "0")
	style.Set("overflow", "hidden")
	style.Set("zIndex", "-1")
	doc.Get("body").Call("appendChild", ta)
	p.ime = ta
}

// FramebufferSize returns the canvas backing store in device pixels.
func (p *Window) FramebufferSize() (int, int) {
	return maxInt(int(float64(p.width)*float64(p.scale)), 1),
		maxInt(int(float64(p.height)*float64(p.scale)), 1)
}

// LogicalSize returns the CSS size in logical pixels.
func (p *Window) LogicalSize() (float64, float64) {
	return float64(p.width), float64(p.height)
}

// Scale returns the device pixel ratio.
func (p *Window) Scale() float32 { return p.scale }

// Visible reports whether the tab is shown.
func (p *Window) Visible() bool { return !js.Global().Get("document").Get("hidden").Bool() }

// ShouldClose reports whether the app asked to stop.
func (p *Window) ShouldClose() bool { return p.closing }

// Destroy removes the listeners and the frame pump. The canvas stays, so a page
// can show it again.
func (p *Window) Destroy() {
	if p.closing {
		return
	}
	p.closing = true
	if p.stop.Truthy() {
		p.stop.Invoke()
		p.stop.Release()
	}
	if p.raf.Truthy() {
		js.Global().Call("cancelAnimationFrame", p.raf)
		p.raf.Release()
	}
	p.input = nil
}

// PostEmptyEvent wakes a waiting loop.
func PostEmptyEvent() {
	// There is one window per page; a package-level wake would need the window
	// pointer, so the frame pump is what wakes an idle app anyway.
}

// SystemTheme returns the platform's light/dark preference.
func SystemTheme() Theme {
	if js.Global().Call("matchMedia", "(prefers-color-scheme: dark)").Get("matches").Bool() {
		return ThemeDark
	}
	return ThemeLight
}

// SetTitle updates the page title.
func (p *Window) SetTitle(title string) { js.Global().Get("document").Set("title", title) }

// Minimize is a no-op: a tab has nothing to minimize to.
func (p *Window) Minimize() {}

// SetMinimized keeps the state a design reads back.
func (p *Window) SetMinimized(on bool) { p.minimized = on }

// Minimized reports the state last asked for.
func (p *Window) Minimized() bool { return p.minimized }

// SetMinimizable keeps the property.
func (p *Window) SetMinimizable(on bool) { p.minimizable = on }

// Minimizable reports the property.
func (p *Window) Minimizable() bool { return p.minimizable }

// SetMaximized goes fullscreen, which is the closest a page has to maximizing.
func (p *Window) SetMaximized(on bool) {
	p.maximized = on
	if on {
		p.SetFullscreen(true)
	}
}

// Maximized reports the state last asked for.
func (p *Window) Maximized() bool { return p.maximized }

// SetMaximizable keeps the property.
func (p *Window) SetMaximizable(on bool) { p.maximizable = on }

// Maximizable reports the property.
func (p *Window) Maximizable() bool { return p.maximizable }

// ToggleMaximize flips the maximized state.
func (p *Window) ToggleMaximize() { p.SetMaximized(!p.maximized) }

// SetResizable keeps the property: CSS owns the canvas size.
func (p *Window) SetResizable(on bool) { p.resizable = on }

// Resizable reports the property.
func (p *Window) Resizable() bool { return p.resizable }

// SetSizeLimits keeps the limits; a design reads them back and the layout uses
// them, but the page decides the real size.
func (p *Window) SetSizeLimits(minW, minH, maxW, maxH int) {
	p.minW, p.minH, p.maxW, p.maxH = minW, minH, maxW, maxH
}

// SetFullscreen asks the browser for fullscreen on the canvas.
func (p *Window) SetFullscreen(on bool) {
	p.fullscreen = on
	if on {
		p.canvas.Call("requestFullscreen")
		return
	}
	doc := js.Global().Get("document")
	if doc.Get("fullscreenElement").Truthy() {
		doc.Call("exitFullscreen")
	}
}

// Fullscreen reports the state last asked for.
func (p *Window) Fullscreen() bool { return p.fullscreen }

// SystemLanguage returns the browser's UI language.
func SystemLanguage() string {
	lang := js.Global().Get("navigator").Get("language")
	if lang.Type() != js.TypeString || lang.String() == "" {
		return "en"
	}
	return lang.String()
}

// Close asks the app to stop.
func (p *Window) Close() {
	p.closing = true
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Clipboard returns the text the page last copied or received.
func Clipboard() string { return clipboard }

// SetClipboard writes the text to the system clipboard and to the cache, so a
// paste right after a copy reads back even before the browser allows a read.
func SetClipboard(s string) {
	clipboard = s
	if c := js.Global().Get("navigator").Get("clipboard"); c.Truthy() {
		c.Call("writeText", s)
	}
}
