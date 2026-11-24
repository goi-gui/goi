//go:build darwin && !js && (webview || all)

package platform

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/Zyko0/go-sdl3/sdl"
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// This is the macOS backend: WKWebView as a subview of SDL's content view. The
// view draws and takes input through AppKit, so no synthetic forwarding is
// needed; goi moves it and loads pages.
//
// The Objective-C objects are called through objc_msgSend with one typed Go
// signature per argument shape, which is how an ObjC call site is compiled.
//
// shortcut: this code is cross-compiled but has not run on macOS yet; the
// first run on real hardware should exercise load, bounds and the bridge.

// nsRect is NSRect/CGRect: x, y, width and height in points. The origin is the
// bottom-left corner, unlike a goi rect.
type nsRect struct {
	X, Y, W, H float64
}

var (
	cocoaOnce sync.Once
	cocoaErr  error

	selAlloc           objc.SEL
	selNew             objc.SEL
	selRelease         objc.SEL
	selContentView     objc.SEL
	selAddSubview      objc.SEL
	selRemoveFrom      objc.SEL
	selSetFrame        objc.SEL
	selSetHidden       objc.SEL
	selString          objc.SEL
	selUTF8            objc.SEL
	selURLWithString   objc.SEL
	selRequestWith     objc.SEL
	selLoadRequest     objc.SEL
	selLoadHTML        objc.SEL
	selEval            objc.SEL
	selReload          objc.SEL
	selCanGoBack       objc.SEL
	selCanGoForward    objc.SEL
	selGoBack          objc.SEL
	selGoForward       objc.SEL
	selSetPageZoom     objc.SEL
	selSetUserAgent    objc.SEL
	selResponds        objc.SEL
	selUserContent     objc.SEL
	selAddHandler      objc.SEL
	selRemoveHandler   objc.SEL
	selPreferences     objc.SEL
	selSetValueKey     objc.SEL
	selNumberBool      objc.SEL
	selFirstResponder  objc.SEL
	selWebView         objc.SEL
	selSetInspectable  objc.SEL
	selDidReceive      objc.SEL
	selBody            objc.SEL
	selInitFrameConfig objc.SEL

	sendID0       func(objc.ID, objc.SEL) objc.ID
	sendID1ID     func(objc.ID, objc.SEL, objc.ID) objc.ID
	sendID1Ptr    func(objc.ID, objc.SEL, *byte) objc.ID
	sendID1Rect   func(objc.ID, objc.SEL, nsRect) objc.ID
	sendID2Rect   func(objc.ID, objc.SEL, nsRect, objc.ID) objc.ID
	sendBool1SEL  func(objc.ID, objc.SEL, objc.SEL) bool
	sendID1Bool   func(objc.ID, objc.SEL, bool) objc.ID
	sendPtr0      func(objc.ID, objc.SEL) *byte
	sendBool0     func(objc.ID, objc.SEL) bool
	sendBool1ID   func(objc.ID, objc.SEL, objc.ID) bool
	sendVoid0     func(objc.ID, objc.SEL)
	sendVoid1ID   func(objc.ID, objc.SEL, objc.ID)
	sendVoid2ID   func(objc.ID, objc.SEL, objc.ID, objc.ID)
	sendVoid1Bool func(objc.ID, objc.SEL, bool)
	sendVoid1Rect func(objc.ID, objc.SEL, nsRect)
	sendVoid1F64  func(objc.ID, objc.SEL, float64)
)

func loadCocoa() error {
	cocoaOnce.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/Cocoa.framework/Cocoa", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			cocoaErr = fmt.Errorf("%w: %v", ErrWebviewUnavailable, err)
			return
		}
		if _, err := purego.Dlopen("/System/Library/Frameworks/WebKit.framework/WebKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			cocoaErr = fmt.Errorf("%w: %v", ErrWebviewUnavailable, err)
			return
		}
		if objc.GetClass("WKWebView") == 0 {
			cocoaErr = fmt.Errorf("%w: WKWebView is missing", ErrWebviewUnavailable)
			return
		}
		lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			cocoaErr = fmt.Errorf("%w: %v", ErrWebviewUnavailable, err)
			return
		}
		msgSend, err := purego.Dlsym(lib, "objc_msgSend")
		if err != nil {
			cocoaErr = fmt.Errorf("%w: %v", ErrWebviewUnavailable, err)
			return
		}
		purego.RegisterFunc(&sendID0, msgSend)
		purego.RegisterFunc(&sendID1ID, msgSend)
		purego.RegisterFunc(&sendID1Ptr, msgSend)
		purego.RegisterFunc(&sendID1Rect, msgSend)
		purego.RegisterFunc(&sendID2Rect, msgSend)
		purego.RegisterFunc(&sendBool1SEL, msgSend)
		purego.RegisterFunc(&sendID1Bool, msgSend)
		purego.RegisterFunc(&sendPtr0, msgSend)
		purego.RegisterFunc(&sendBool0, msgSend)
		purego.RegisterFunc(&sendBool1ID, msgSend)
		purego.RegisterFunc(&sendVoid0, msgSend)
		purego.RegisterFunc(&sendVoid1ID, msgSend)
		purego.RegisterFunc(&sendVoid2ID, msgSend)
		purego.RegisterFunc(&sendVoid1Bool, msgSend)
		purego.RegisterFunc(&sendVoid1Rect, msgSend)
		purego.RegisterFunc(&sendVoid1F64, msgSend)

		selAlloc = objc.RegisterName("alloc")
		selNew = objc.RegisterName("new")
		selRelease = objc.RegisterName("release")
		selContentView = objc.RegisterName("contentView")
		selAddSubview = objc.RegisterName("addSubview:")
		selRemoveFrom = objc.RegisterName("removeFromSuperview")
		selSetFrame = objc.RegisterName("setFrame:")
		selSetHidden = objc.RegisterName("setHidden:")
		selString = objc.RegisterName("stringWithUTF8String:")
		selUTF8 = objc.RegisterName("UTF8String")
		selURLWithString = objc.RegisterName("URLWithString:")
		selRequestWith = objc.RegisterName("requestWithURL:")
		selLoadRequest = objc.RegisterName("loadRequest:")
		selLoadHTML = objc.RegisterName("loadHTMLString:baseURL:")
		selEval = objc.RegisterName("evaluateJavaScript:completionHandler:")
		selReload = objc.RegisterName("reload")
		selCanGoBack = objc.RegisterName("canGoBack")
		selCanGoForward = objc.RegisterName("canGoForward")
		selGoBack = objc.RegisterName("goBack")
		selGoForward = objc.RegisterName("goForward")
		selSetPageZoom = objc.RegisterName("setPageZoom:")
		selSetUserAgent = objc.RegisterName("setCustomUserAgent:")
		selResponds = objc.RegisterName("respondsToSelector:")
		selUserContent = objc.RegisterName("userContentController")
		selAddHandler = objc.RegisterName("addScriptMessageHandler:name:")
		selRemoveHandler = objc.RegisterName("removeScriptMessageHandlerForName:")
		selPreferences = objc.RegisterName("preferences")
		selSetValueKey = objc.RegisterName("setValue:forKey:")
		selNumberBool = objc.RegisterName("numberWithBool:")
		selFirstResponder = objc.RegisterName("makeFirstResponder:")
		selWebView = objc.RegisterName("webView")
		selSetInspectable = objc.RegisterName("setInspectable:")
		selDidReceive = objc.RegisterName("userContentController:didReceiveScriptMessage:")
		selInitFrameConfig = objc.RegisterName("initWithFrame:configuration:")
	})
	return cocoaErr
}

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

// nsString copies a Go string into an autoreleased NSString.
func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	id := sendID1Ptr(class("NSString"), selString, &b[0])
	runtime.KeepAlive(b)
	return id
}

func goString(p *byte) string {
	if p == nil {
		return ""
	}
	var b []byte
	for i := 0; ; i++ {
		c := *(*byte)(unsafe.Add(unsafe.Pointer(p), i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}

// InitWebview loads AppKit and WebKit. There is no ordering constraint with
// SDL: a subview can be added to a window at any time.
func InitWebview() error { return loadCocoa() }

// PumpWebviews needs no loop of its own: AppKit's run loop is the one SDL
// services, and WKWebView renders on it.
func PumpWebviews() bool { return false }

type darwinWebview struct {
	parent   *Window
	nsWindow objc.ID
	content  objc.ID
	view     objc.ID
	config   objc.ID
	uc       objc.ID
	handler  objc.ID
	message  func(string)

	x, y    float64
	w, h    float64
	visible bool
	closed  bool
	zoom    float64
}

var (
	scriptOnce  sync.Once
	scriptClass objc.Class
	scriptErr   error
)

// webViews maps a WKWebView to its Go side, so the one message handler class
// can find the view the message came from.
var webViews = map[objc.ID]*darwinWebview{}

// scriptHandlerClass registers the message handler class once. Registering a
// class per view would leak one class (and its closure) for every view and
// every hot reload, since ObjC classes cannot be unregistered.
func scriptHandlerClass() (objc.Class, error) {
	scriptOnce.Do(func() {
		scriptClass, scriptErr = objc.RegisterClass("GoiWebViewHandler",
			objc.GetClass("NSObject"), nil, nil,
			[]objc.MethodDef{{
				Cmd: selDidReceive,
				Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, message objc.ID) {
					view := sendID0(message, selWebView)
					w := webViews[view]
					if w == nil || w.message == nil {
						return
					}
					body := sendID0(message, selBody)
					if body == 0 {
						return
					}
					if cs := sendPtr0(body, selUTF8); cs != nil {
						w.message(goString(cs))
					}
				},
			}})
	})
	return scriptClass, scriptErr
}

// NewWebview creates a WKWebView and adds it above the Metal view in SDL's
// content view.
func NewWebview(parent *Window, onMessage func(string), _ func()) (Webview, error) {
	if err := loadCocoa(); err != nil {
		return nil, err
	}
	if parent == nil || parent.win == nil {
		return nil, ErrWebviewUnsupported
	}
	props, err := (*sdl.Window)(parent.win).Properties()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWebviewUnsupported, err)
	}
	nsWindow := objc.ID(uintptr(props.PointerProperty(sdl.PROP_WINDOW_COCOA_WINDOW_POINTER)))
	if nsWindow == 0 {
		return nil, fmt.Errorf("%w: SDL did not report an NSWindow", ErrWebviewUnsupported)
	}
	content := sendID0(nsWindow, selContentView)
	if content == 0 {
		return nil, fmt.Errorf("%w: the window has no content view", ErrWebviewUnsupported)
	}
	w := &darwinWebview{
		parent:   parent,
		nsWindow: nsWindow,
		content:  content,
		visible:  true,
		zoom:     1,
	}

	w.config = sendID0(class("WKWebViewConfiguration"), selNew)
	if w.config == 0 {
		return nil, fmt.Errorf("%w: WKWebViewConfiguration", ErrWebviewUnavailable)
	}
	w.message = onMessage
	if onMessage != nil {
		handler, err := scriptHandlerClass()
		if err != nil {
			w.Destroy()
			return nil, fmt.Errorf("%w: %v", ErrWebviewUnavailable, err)
		}
		w.handler = sendID0(objc.ID(handler), selNew)
		w.uc = sendID0(w.config, selUserContent)
		if w.handler != 0 && w.uc != 0 {
			sendVoid2ID(w.uc, selAddHandler, w.handler, nsString("goi"))
		}
	}
	w.view = sendID2Rect(sendID0(class("WKWebView"), selAlloc), selInitFrameConfig, nsRect{}, w.config)
	if w.view == 0 {
		w.Destroy()
		return nil, fmt.Errorf("%w: WKWebView", ErrWebviewUnavailable)
	}
	sendVoid1ID(content, selAddSubview, w.view)
	webViews[w.view] = w
	w.SetBounds(w.x, w.y, w.w, w.h)
	return w, nil
}

func (w *darwinWebview) SetBounds(x, y, width, height float64) {
	w.x, w.y, w.w, w.h = x, y, width, height
	if w.view == 0 {
		return
	}
	if !w.visible || width <= 0 || height <= 0 {
		sendVoid1Bool(w.view, selSetHidden, true)
		return
	}
	sendVoid1Bool(w.view, selSetHidden, false)
	// NSView is bottom-left origin; the design's rect is top-left.
	top := float64(w.parent.height) - (y + height)
	sendVoid1Rect(w.view, selSetFrame, nsRect{X: x, Y: top, W: width, H: height})
}

func (w *darwinWebview) SetVisible(v bool) {
	w.visible = v
	w.SetBounds(w.x, w.y, w.w, w.h)
}

func (w *darwinWebview) Load(src string) {
	if w.view == 0 {
		return
	}
	if src == "" {
		src = "about:blank"
	}
	url := sendID1ID(class("NSURL"), selURLWithString, nsString(src))
	if url == 0 {
		return
	}
	if req := sendID1ID(class("NSURLRequest"), selRequestWith, url); req != 0 {
		sendVoid1ID(w.view, selLoadRequest, req)
	}
}

func (w *darwinWebview) LoadHTML(html, base string) {
	if w.view == 0 {
		return
	}
	var baseID objc.ID
	if base != "" {
		baseID = sendID1ID(class("NSURL"), selURLWithString, nsString(base))
	}
	sendVoid2ID(w.view, selLoadHTML, nsString(html), baseID)
}

func (w *darwinWebview) Reload() {
	if w.view != 0 {
		sendVoid0(w.view, selReload)
	}
}

func (w *darwinWebview) GoBack() bool {
	if w.view == 0 || !sendBool0(w.view, selCanGoBack) {
		return false
	}
	sendVoid0(w.view, selGoBack)
	return true
}

func (w *darwinWebview) GoForward() bool {
	if w.view == 0 || !sendBool0(w.view, selCanGoForward) {
		return false
	}
	sendVoid0(w.view, selGoForward)
	return true
}

func (w *darwinWebview) Eval(js string) {
	if w.view != 0 {
		sendVoid2ID(w.view, selEval, nsString(js), 0)
	}
}

func (w *darwinWebview) SetZoom(z float64) {
	if w.view == 0 || z <= 0 {
		return
	}
	w.zoom = z
	// pageZoom is macOS 11+; sending it on an older system would raise.
	if sendBool1SEL(w.view, selResponds, selSetPageZoom) {
		sendVoid1F64(w.view, selSetPageZoom, z)
	}
}

func (w *darwinWebview) SetUserAgent(ua string) {
	if w.view == 0 {
		return
	}
	if ua == "" {
		sendVoid1ID(w.view, selSetUserAgent, 0)
		return
	}
	sendVoid1ID(w.view, selSetUserAgent, nsString(ua))
}

func (w *darwinWebview) SetDevtools(on bool) {
	if !on || w.view == 0 {
		return
	}
	// macOS 13.3 has a public switch; the preference key below is private and
	// can raise an exception, so it is only the fallback.
	if sendBool1SEL(w.view, selResponds, selSetInspectable) {
		sendVoid1Bool(w.view, selSetInspectable, true)
		return
	}
	if w.config == 0 {
		return
	}
	prefs := sendID0(w.config, selPreferences)
	if prefs == 0 {
		return
	}
	sendVoid2ID(prefs, selSetValueKey, nsString("developerExtrasEnabled"),
		sendID1Bool(class("NSNumber"), selNumberBool, true))
}

func (w *darwinWebview) Focus() {
	if w.view != 0 {
		sendBool1ID(w.nsWindow, selFirstResponder, w.view)
	}
}

// ForwardKey, ForwardText and ForwardPointer are not needed: the view is a
// real subview and AppKit gives it the keyboard and pointer.
func (w *darwinWebview) ForwardKey(Key, Mods) {}

func (w *darwinWebview) ForwardText(rune) {}

func (w *darwinWebview) ForwardPointer(PointerKind, float64, float64, int, float64, float64, bool) {}

func (w *darwinWebview) Destroy() {
	if w.closed {
		return
	}
	w.closed = true
	if w.uc != 0 {
		sendVoid1ID(w.uc, selRemoveHandler, nsString("goi"))
	}
	if w.view != 0 {
		sendVoid0(w.view, selRemoveFrom)
	}
	if w.handler != 0 {
		sendVoid0(w.handler, selRelease)
	}
	if w.view != 0 {
		sendVoid0(w.view, selRelease)
	}
	if w.config != 0 {
		sendVoid0(w.config, selRelease)
	}
	delete(webViews, w.view)
	w.view, w.config, w.uc, w.handler = 0, 0, 0, 0
	w.message = nil
}

// SetWebviewFocus is a no-op: this platform lets the engine take focus itself.
func SetWebviewFocus(bool) {}

// WebviewsLoading is false: this platform does not poll its engine.
func WebviewsLoading() bool { return false }
