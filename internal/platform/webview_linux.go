//go:build linux && !js && (webview || all)

package platform

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/Zyko0/go-sdl3/sdl"
	"github.com/ebitengine/purego"
)

// This is the Linux Wayland backend for WebView. GTK creates a custom
// (roleless) wl_surface, and the view is attached as a wl_subsurface of the
// SDL window's surface. Both toolkits share one wl_display: SDL imports the
// display GTK created, so a subsurface is legal (Wayland surfaces cannot cross
// client connections).
//
// X11 is deliberately not used here: the user's scope is Wayland, and GtkPlug
// is X11-only. A Wayland session with SDL on XWayland would be a different
// backend.

var (
	gtkLib, gdkLib, gobjectLib, glibLib uintptr
	webkitLib, jscLib, waylandLib       uintptr

	gtkInitCheck                   func(*int32, ***byte) int32
	gtkWindowNew                   func(int32) uintptr
	gtkWindowSetDecorated          func(uintptr, int32)
	gtkWindowSetDefaultSize        func(uintptr, int32, int32)
	gtkWindowResize                func(uintptr, int32, int32)
	gtkWindowSetFocus              func(uintptr, uintptr)
	gtkWidgetShowAll               func(uintptr)
	gtkWidgetRealize               func(uintptr)
	gtkWidgetGetWindow             func(uintptr) unsafe.Pointer
	gtkWidgetDestroy               func(uintptr)
	gtkContainerAdd                func(uintptr, uintptr)
	gtkWidgetAddEvents             func(uintptr, int32)
	gtkEventsPending               func() int32
	gtkMainIteration               func() int32
	gdkDisplayGetDefault           func() uintptr
	gdkSeatGetPointer              func(uintptr) uintptr
	gdkDisplayGetDefaultSeat       func(uintptr) uintptr
	gdkWaylandDisplayGetWlDisplay  func(uintptr) unsafe.Pointer
	gdkWaylandDisplayGetCompositor func(uintptr) uintptr
	gdkWaylandWindowSetCustom      func(unsafe.Pointer)
	gdkWaylandWindowGetWlSurface   func(unsafe.Pointer) uintptr
	gdkEventNew                    func(int32) unsafe.Pointer
	gdkEventFree                   func(unsafe.Pointer)
	gdkEventSetDevice              func(unsafe.Pointer, uintptr)
	gdkUnicodeToKeyval             func(uint32) uint32
	gtkMainDoEvent                 func(unsafe.Pointer)
	gObjectRef                     func(unsafe.Pointer) unsafe.Pointer
	gSignalConnectData             func(uintptr, *byte, uintptr, uintptr, uintptr, uint32) uint64
	gFree                          func(unsafe.Pointer)
	gdkDisplay                     uintptr
	webkitWebViewNew               func() uintptr
	webkitWebViewNewWithManager    func(uintptr) uintptr
	webkitManagerNew               func() uintptr
	webkitManagerRegisterHandler   func(uintptr, *byte) int32
	webkitWebViewLoadURI           func(uintptr, *byte)
	webkitWebViewLoadHTML          func(uintptr, *byte, *byte)
	webkitWebViewReload            func(uintptr)
	webkitWebViewCanGoBack         func(uintptr) int32
	webkitWebViewGoBack            func(uintptr)
	webkitWebViewCanGoForward      func(uintptr) int32
	webkitWebViewGoForward         func(uintptr)
	webkitWebViewEval              func(uintptr, *byte, int64, uintptr, uintptr, uintptr, uintptr, uintptr)
	webkitWebViewSetZoom           func(uintptr, float64)
	webkitWebViewGetSettings       func(uintptr) uintptr
	webkitSettingsSetUserAgent     func(uintptr, *byte)
	webkitSettingsSetDeveloper     func(uintptr, int32)
	webkitJavascriptResultGetValue func(uintptr) uintptr
	jscValueToString               func(uintptr) unsafe.Pointer

	wlMarshalArrayFlags   func(uintptr, uint32, uintptr, uint32, uint32, uintptr) uintptr
	wlProxyAddListener    func(uintptr, uintptr, uintptr) int32
	wlDisplayRoundtrip    func(uintptr) int32
	wlRegistryInterface   uintptr
	wlCompositorInterface uintptr
	wlSubcompositorIface  uintptr
	wlSubsurfaceInterface uintptr
	wlSurfaceInterface    uintptr
	wlRegionInterface     uintptr

	wlDisplay       uintptr
	wlCompositor    uintptr
	wlSubcompositor uintptr

	webviewOnce sync.Once
	webviewErr  error
	webviewOK   bool
)

func load(lib *uintptr, name string) error {
	h, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrWebviewUnavailable, name)
	}
	*lib = h
	return nil
}

func ptrOf(lib uintptr, name string) uintptr {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		return 0
	}
	return p
}

// InitWebview starts GTK before SDL video, imports GTK's wl_display into SDL,
// and binds the compositor objects the views need. It is safe to call more than
// once; the first result is kept.
func InitWebview() error {
	webviewOnce.Do(func() {
		webviewErr = initLinuxWebview()
		webviewOK = webviewErr == nil
	})
	return webviewErr
}

func initLinuxWebview() error {
	// GTK's wl_display must be imported before SDL connects, so a view that
	// appears after video started (a hot reload that adds one) cannot work in
	// this process; the placeholder reports it and an app restart fixes it.
	if videoStarted {
		return fmt.Errorf("%w: SDL video already started; restart the app to use a WebView", ErrWebviewUnsupported)
	}
	if err := load(&gtkLib, "libgtk-3.so.0"); err != nil {
		return err
	}
	if err := load(&gdkLib, "libgdk-3.so.0"); err != nil {
		return err
	}
	if err := load(&gobjectLib, "libgobject-2.0.so.0"); err != nil {
		return err
	}
	if err := load(&glibLib, "libglib-2.0.so.0"); err != nil {
		return err
	}
	if err := load(&webkitLib, "libwebkit2gtk-4.1.so.0"); err != nil {
		return err
	}
	if err := load(&jscLib, "libjavascriptcoregtk-4.1.so.0"); err != nil {
		return err
	}
	if err := load(&waylandLib, "libwayland-client.so.0"); err != nil {
		return err
	}

	purego.RegisterLibFunc(&gtkInitCheck, gtkLib, "gtk_init_check")
	purego.RegisterLibFunc(&gtkWindowNew, gtkLib, "gtk_window_new")
	purego.RegisterLibFunc(&gtkWindowSetDecorated, gtkLib, "gtk_window_set_decorated")
	purego.RegisterLibFunc(&gtkWindowSetDefaultSize, gtkLib, "gtk_window_set_default_size")
	purego.RegisterLibFunc(&gtkWindowResize, gtkLib, "gtk_window_resize")
	purego.RegisterLibFunc(&gtkWindowSetFocus, gtkLib, "gtk_window_set_focus")
	purego.RegisterLibFunc(&gtkWidgetShowAll, gtkLib, "gtk_widget_show_all")
	purego.RegisterLibFunc(&gtkWidgetRealize, gtkLib, "gtk_widget_realize")
	purego.RegisterLibFunc(&gtkWidgetGetWindow, gtkLib, "gtk_widget_get_window")
	purego.RegisterLibFunc(&gtkWidgetDestroy, gtkLib, "gtk_widget_destroy")
	purego.RegisterLibFunc(&gtkContainerAdd, gtkLib, "gtk_container_add")
	purego.RegisterLibFunc(&gtkWidgetAddEvents, gtkLib, "gtk_widget_add_events")
	purego.RegisterLibFunc(&gtkEventsPending, gtkLib, "gtk_events_pending")
	purego.RegisterLibFunc(&gtkMainIteration, gtkLib, "gtk_main_iteration")
	purego.RegisterLibFunc(&gdkDisplayGetDefault, gdkLib, "gdk_display_get_default")
	purego.RegisterLibFunc(&gdkWaylandDisplayGetWlDisplay, gdkLib, "gdk_wayland_display_get_wl_display")
	purego.RegisterLibFunc(&gdkWaylandDisplayGetCompositor, gdkLib, "gdk_wayland_display_get_wl_compositor")
	purego.RegisterLibFunc(&gdkWaylandWindowSetCustom, gdkLib, "gdk_wayland_window_set_use_custom_surface")
	purego.RegisterLibFunc(&gdkWaylandWindowGetWlSurface, gdkLib, "gdk_wayland_window_get_wl_surface")
	purego.RegisterLibFunc(&gdkEventNew, gdkLib, "gdk_event_new")
	purego.RegisterLibFunc(&gdkEventFree, gdkLib, "gdk_event_free")
	purego.RegisterLibFunc(&gdkEventSetDevice, gdkLib, "gdk_event_set_device")
	purego.RegisterLibFunc(&gdkUnicodeToKeyval, gdkLib, "gdk_unicode_to_keyval")
	purego.RegisterLibFunc(&gdkDisplayGetDefaultSeat, gdkLib, "gdk_display_get_default_seat")
	purego.RegisterLibFunc(&gdkSeatGetPointer, gdkLib, "gdk_seat_get_pointer")
	purego.RegisterLibFunc(&gtkMainDoEvent, gtkLib, "gtk_main_do_event")
	purego.RegisterLibFunc(&gSignalConnectData, gobjectLib, "g_signal_connect_data")
	purego.RegisterLibFunc(&gObjectRef, gobjectLib, "g_object_ref")
	purego.RegisterLibFunc(&gFree, glibLib, "g_free")
	purego.RegisterLibFunc(&webkitWebViewNew, webkitLib, "webkit_web_view_new")
	purego.RegisterLibFunc(&webkitWebViewNewWithManager, webkitLib, "webkit_web_view_new_with_user_content_manager")
	purego.RegisterLibFunc(&webkitManagerNew, webkitLib, "webkit_user_content_manager_new")
	purego.RegisterLibFunc(&webkitManagerRegisterHandler, webkitLib, "webkit_user_content_manager_register_script_message_handler")
	purego.RegisterLibFunc(&webkitWebViewLoadURI, webkitLib, "webkit_web_view_load_uri")
	purego.RegisterLibFunc(&webkitWebViewLoadHTML, webkitLib, "webkit_web_view_load_html")
	purego.RegisterLibFunc(&webkitWebViewReload, webkitLib, "webkit_web_view_reload")
	purego.RegisterLibFunc(&webkitWebViewCanGoBack, webkitLib, "webkit_web_view_can_go_back")
	purego.RegisterLibFunc(&webkitWebViewGoBack, webkitLib, "webkit_web_view_go_back")
	purego.RegisterLibFunc(&webkitWebViewCanGoForward, webkitLib, "webkit_web_view_can_go_forward")
	purego.RegisterLibFunc(&webkitWebViewGoForward, webkitLib, "webkit_web_view_go_forward")
	purego.RegisterLibFunc(&webkitWebViewEval, webkitLib, "webkit_web_view_evaluate_javascript")
	purego.RegisterLibFunc(&webkitWebViewSetZoom, webkitLib, "webkit_web_view_set_zoom_level")
	purego.RegisterLibFunc(&webkitWebViewGetSettings, webkitLib, "webkit_web_view_get_settings")
	purego.RegisterLibFunc(&webkitSettingsSetUserAgent, webkitLib, "webkit_settings_set_user_agent")
	purego.RegisterLibFunc(&webkitSettingsSetDeveloper, webkitLib, "webkit_settings_set_enable_developer_extras")
	purego.RegisterLibFunc(&webkitJavascriptResultGetValue, webkitLib, "webkit_javascript_result_get_js_value")
	purego.RegisterLibFunc(&jscValueToString, jscLib, "jsc_value_to_string")

	purego.RegisterLibFunc(&wlMarshalArrayFlags, waylandLib, "wl_proxy_marshal_array_flags")
	purego.RegisterLibFunc(&wlProxyAddListener, waylandLib, "wl_proxy_add_listener")
	purego.RegisterLibFunc(&wlDisplayRoundtrip, waylandLib, "wl_display_roundtrip")

	wlRegistryInterface = ptrOf(waylandLib, "wl_registry_interface")
	wlCompositorInterface = ptrOf(waylandLib, "wl_compositor_interface")
	wlSubcompositorIface = ptrOf(waylandLib, "wl_subcompositor_interface")
	wlSubsurfaceInterface = ptrOf(waylandLib, "wl_subsurface_interface")
	wlSurfaceInterface = ptrOf(waylandLib, "wl_surface_interface")
	wlRegionInterface = ptrOf(waylandLib, "wl_region_interface")
	if wlRegistryInterface == 0 || wlCompositorInterface == 0 || wlSubcompositorIface == 0 ||
		wlSubsurfaceInterface == 0 || wlSurfaceInterface == 0 || wlRegionInterface == 0 {
		return fmt.Errorf("%w: Wayland protocol interfaces", ErrWebviewUnavailable)
	}

	argc := int32(1)
	arg0 := []byte("goi\x00")
	argv := []*byte{&arg0[0], nil}
	argvp := &argv[0]
	if gtkInitCheck(&argc, &argvp) == 0 {
		return fmt.Errorf("%w: gtk_init_check failed (is a Wayland session running?)", ErrWebviewUnavailable)
	}
	gdisp := gdkDisplayGetDefault()
	if gdisp == 0 {
		return fmt.Errorf("%w: no GDK display", ErrWebviewUnavailable)
	}
	gdkDisplay = gdisp
	wlDisplayPtr := gdkWaylandDisplayGetWlDisplay(gdisp)
	wlDisplay = uintptr(wlDisplayPtr)
	wlCompositor = gdkWaylandDisplayGetCompositor(gdisp)
	if wlDisplay == 0 || wlCompositor == 0 {
		return fmt.Errorf("%w: GTK is not using the Wayland backend", ErrWebviewUnavailable)
	}
	if err := bindSubcompositor(); err != nil {
		return err
	}
	// SDL must import this display before its video subsystem starts.
	props, err := sdl.GetGlobalProperties()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWebviewUnavailable, err)
	}
	if err := props.SetPointerProperty("SDL.video.wayland.wl_display", sdl.Pointer(wlDisplayPtr)); err != nil {
		return fmt.Errorf("%w: import wl_display: %v", ErrWebviewUnavailable, err)
	}
	return nil
}

type registryListener struct{ global, remove uintptr }

var regListener registryListener
var regCallbackGlobal, regCallbackRemove uintptr

func bindSubcompositor() error {
	if regCallbackGlobal == 0 {
		regCallbackGlobal = purego.NewCallback(func(_ uintptr, registry uintptr, name uint32, iface unsafe.Pointer, _ uint32) {
			if iface == nil || goString(iface) != "wl_subcompositor" {
				return
			}
			wlSubcompositor = wlMarshal(wlRegistryInterface, registry, 0, wlSubcompositorIface, 1, 0,
				uint64(name), uint64(uintptr(iface)), 1, 0)
		})
		regCallbackRemove = purego.NewCallback(func(uintptr, uintptr, uint32) {})
		regListener = registryListener{global: regCallbackGlobal, remove: regCallbackRemove}
	}
	reg := wlMarshal(wlRegistryInterface, wlDisplay, 1, wlRegistryInterface, 1, 0, 0)
	if reg == 0 {
		return fmt.Errorf("%w: wl_display_get_registry", ErrWebviewUnavailable)
	}
	wlProxyAddListener(reg, uintptr(unsafe.Pointer(&regListener)), 0)
	wlDisplayRoundtrip(wlDisplay)
	if wlSubcompositor == 0 {
		return fmt.Errorf("%w: the compositor has no wl_subcompositor", ErrWebviewUnavailable)
	}
	return nil
}

// wlMarshal calls one generated Wayland request through the non-variadic entry
// point. iface is the new object's interface, or 0 when the request creates
// none; the union wl_argument array is passed as 64-bit slots.
func wlMarshal(_ uintptr, proxy uintptr, opcode uint32, newIface uintptr, version, flags uint32, args ...uint64) uintptr {
	var a [8]uint64
	copy(a[:], args)
	return wlMarshalArrayFlags(proxy, opcode, newIface, version, flags, uintptr(unsafe.Pointer(&a[0])))
}

func goString(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	var b []byte
	for i := 0; i < 1<<20; i++ {
		c := *(*byte)(unsafe.Add(p, i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}

func cstr(s string) *byte {
	b := append([]byte(s), 0)
	return &b[0]
}

// PumpWebviews advances GTK's event loop. It runs on the UI goroutine, where
// GTK was initialized.
func PumpWebviews() {
	if !webviewOK {
		return
	}
	for gtkEventsPending() != 0 {
		gtkMainIteration()
	}
}

type linuxWebview struct {
	win, view, manager uintptr
	surface, sub       uintptr
	x, y, w, h         float64
	visible            bool
	message            func(string)
	focus              func()
	handlerID          uintptr
	focusID            uintptr
	focusCallback      uintptr
}

var (
	messageMu sync.Mutex
	messageID uint64
	messages  = map[uintptr]func(string){}
)

func registerMessage(fn func(string)) uintptr {
	id := uintptr(atomic.AddUint64(&messageID, 1))
	messageMu.Lock()
	messages[id] = fn
	messageMu.Unlock()
	return id
}

func unregisterMessage(id uintptr) {
	messageMu.Lock()
	delete(messages, id)
	messageMu.Unlock()
}

var jsCallback uintptr

func scriptMessage(_ uintptr, result uintptr, id uintptr) {
	if result == 0 {
		return
	}
	value := webkitJavascriptResultGetValue(result)
	if value == 0 {
		return
	}
	text := jscValueToString(value)
	if text == nil {
		return
	}
	msg := goString(text)
	gFree(text)
	messageMu.Lock()
	fn := messages[id]
	messageMu.Unlock()
	if fn != nil {
		fn(msg)
	}
}

// NewWebview creates one GTK window with a WebKit view and attaches its
// roleless surface as a subsurface of the SDL window.
func NewWebview(parent *Window, onMessage func(string), onFocus func()) (Webview, error) {
	if err := InitWebview(); err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, ErrWebviewUnsupported
	}
	_, handle, kind, err := parent.SurfaceHandles()
	if err != nil {
		return nil, err
	}
	if kind != SurfaceWayland || handle == 0 {
		return nil, ErrWebviewUnsupported
	}
	if jsCallback == 0 {
		jsCallback = purego.NewCallback(scriptMessage)
	}
	w := &linuxWebview{visible: true, message: onMessage, focus: onFocus}

	win := gtkWindowNew(0)
	if win == 0 {
		return nil, fmt.Errorf("%w: gtk_window_new", ErrWebviewUnavailable)
	}
	w.win = win
	gtkWindowSetDecorated(win, 0)
	gtkWindowSetDefaultSize(win, 320, 200)

	if onMessage != nil {
		w.manager = webkitManagerNew()
		if w.manager != 0 {
			webkitManagerRegisterHandler(w.manager, cstr("goi"))
			w.view = webkitWebViewNewWithManager(w.manager)
		}
		if w.view == 0 {
			w.view = webkitWebViewNew()
		} else {
			w.handlerID = registerMessage(onMessage)
			gSignalConnectData(w.manager, cstr("script-message-received::goi"), jsCallback, w.handlerID, 0, 0)
		}
	} else {
		w.view = webkitWebViewNew()
	}
	if w.view == 0 {
		gtkWidgetDestroy(win)
		return nil, fmt.Errorf("%w: webkit_web_view_new", ErrWebviewUnavailable)
	}
	gtkContainerAdd(win, w.view)
	// Synthetic events reach GTK only for masks the widgets selected.
	const mask = (1 << 8) | (1 << 9) | (1 << 2) | (1 << 10) | (1 << 11) | (1 << 21) | (1 << 23)
	gtkWidgetAddEvents(win, mask)
	gtkWidgetAddEvents(w.view, mask)
	gtkWidgetRealize(win)
	gdkwin := gtkWidgetGetWindow(win)
	if gdkwin == nil {
		gtkWidgetDestroy(win)
		return nil, fmt.Errorf("%w: gtk_widget_get_window", ErrWebviewUnavailable)
	}
	gdkWaylandWindowSetCustom(gdkwin)
	w.surface = gdkWaylandWindowGetWlSurface(gdkwin)
	if w.surface == 0 {
		gtkWidgetDestroy(win)
		return nil, fmt.Errorf("%w: the GTK window has no wl_surface", ErrWebviewUnavailable)
	}
	w.sub = wlMarshal(wlSubcompositorIface, wlSubcompositor, 1, wlSubsurfaceInterface, 1, 0,
		0, uint64(w.surface), uint64(handle))
	if w.sub == 0 {
		gtkWidgetDestroy(win)
		return nil, fmt.Errorf("%w: wl_subcompositor_get_subsurface", ErrWebviewUnavailable)
	}
	wlMarshal(0, w.sub, 1, 0, 0, 0, uint64(uint32(0)), uint64(uint32(0)))
	wlMarshal(0, w.sub, 5, 0, 0, 0)
	if onFocus != nil && w.focusCallback == 0 {
		w.focusCallback = purego.NewCallback(func(_, _, id uintptr) int32 {
			focusMu.Lock()
			fn := focusHandlers[id]
			focusMu.Unlock()
			if fn != nil {
				fn()
			}
			return 0
		})
	}
	if onFocus != nil {
		w.focusID = registerFocus(onFocus)
		gSignalConnectData(w.view, cstr("button-press-event"), w.focusCallback, w.focusID, 0, 0)
	}
	gtkWidgetShowAll(win)
	gtkWindowSetFocus(win, w.view)
	w.clearInput()
	return w, nil
}

func (w *linuxWebview) clearInput() {
	if w.surface == 0 {
		return
	}
	region := wlMarshal(wlRegionInterface, wlCompositor, 1, wlRegionInterface, 1, 0, 0)
	if region == 0 {
		return
	}
	wlMarshal(0, w.surface, 5, 0, 0, 0, uint64(region))
	wlMarshal(0, region, 0, 0, 0, 1)
	wlMarshal(0, w.surface, 6, 0, 0, 0)
}

func (w *linuxWebview) SetBounds(x, y, width, height float64) {
	w.x, w.y, w.w, w.h = x, y, width, height
	if !w.visible || width <= 0 || height <= 0 {
		w.clearInput()
		if w.sub != 0 {
			wlMarshal(0, w.sub, 1, 0, 0, 0, uint64(0xffffe000), uint64(0xffffe000))
		}
		return
	}
	if w.sub != 0 {
		wlMarshal(0, w.sub, 1, 0, 0, 0, uint64(uint32(int32(x))), uint64(uint32(int32(y))))
	}
	gtkWindowResize(w.win, int32(width), int32(height))
	w.clearInput()
}

func (w *linuxWebview) SetVisible(v bool) {
	w.visible = v
	w.SetBounds(w.x, w.y, w.w, w.h)
}

func (w *linuxWebview) Load(src string) { webkitWebViewLoadURI(w.view, cstr(src)) }

func (w *linuxWebview) LoadHTML(html, base string) {
	var b *byte
	if base != "" {
		b = cstr(base)
	}
	webkitWebViewLoadHTML(w.view, cstr(html), b)
}

func (w *linuxWebview) Reload() { webkitWebViewReload(w.view) }

func (w *linuxWebview) GoBack() bool {
	if webkitWebViewCanGoBack(w.view) == 0 {
		return false
	}
	webkitWebViewGoBack(w.view)
	return true
}

func (w *linuxWebview) GoForward() bool {
	if webkitWebViewCanGoForward(w.view) == 0 {
		return false
	}
	webkitWebViewGoForward(w.view)
	return true
}

func (w *linuxWebview) Eval(js string) {
	webkitWebViewEval(w.view, cstr(js), -1, 0, 0, 0, 0, 0)
}

func (w *linuxWebview) SetZoom(z float64) {
	if z > 0 {
		webkitWebViewSetZoom(w.view, z)
	}
}

func (w *linuxWebview) SetUserAgent(ua string) {
	if s := webkitWebViewGetSettings(w.view); s != 0 {
		webkitSettingsSetUserAgent(s, cstr(ua))
	}
}

func (w *linuxWebview) SetDevtools(on bool) {
	if s := webkitWebViewGetSettings(w.view); s != 0 {
		var v int32
		if on {
			v = 1
		}
		webkitSettingsSetDeveloper(s, v)
	}
}

// ForwardKey sends a key press and release to the web view. The view owns GTK
// focus, so a GdkEventKey dispatched on its window reaches WebKit.
func (w *linuxWebview) ForwardKey(k Key, mods Mods) {
	keyval := gdkKeyval(k)
	if keyval == 0 {
		return
	}
	w.sendKey(keyval, true, mods)
	w.sendKey(keyval, false, mods)
}

// ForwardText sends a committed character as a key press, which is how a
// WebKit view inserts text without a real IME event.
func (w *linuxWebview) ForwardText(r rune) {
	keyval := gdkUnicodeToKeyval(uint32(r))
	if keyval == 0 {
		return
	}
	w.sendKey(keyval, true, Mods{})
	w.sendKey(keyval, false, Mods{})
}

func (w *linuxWebview) sendKey(keyval uint32, press bool, mods Mods) {
	if w.win == 0 {
		return
	}
	typ := int32(9) // GDK_KEY_RELEASE
	if press {
		typ = 8 // GDK_KEY_PRESS
	}
	ev := gdkEventNew(typ)
	if ev == nil {
		return
	}
	gdkwin := gtkWidgetGetWindow(w.win)
	if gdkwin == nil {
		gdkEventFree(ev)
		return
	}
	gObjectRef(gdkwin)
	*(*unsafe.Pointer)(unsafe.Add(ev, 8)) = gdkwin // GdkWindow*
	*(*uint32)(unsafe.Add(ev, 24)) = gdkMods(mods)
	*(*uint32)(unsafe.Add(ev, 28)) = keyval
	if seat := gdkDisplayGetDefaultSeat(gdkDisplay); seat != 0 {
		if dev := gdkSeatGetPointer(seat); dev != 0 {
			gdkEventSetDevice(ev, dev)
		}
	}
	gtkMainDoEvent(ev)
	gdkEventFree(ev)
}

func gdkKeyval(k Key) uint32 {
	switch k {
	case KeyEnter:
		return 0xff0d
	case KeyEscape:
		return 0xff1b
	case KeyTab:
		return 0xff09
	case KeyBackspace:
		return 0xff08
	case KeyDelete:
		return 0xffff
	case KeyLeft:
		return 0xff51
	case KeyRight:
		return 0xff53
	case KeyUp:
		return 0xff52
	case KeyDown:
		return 0xff54
	case KeyHome:
		return 0xff50
	case KeyEnd:
		return 0xff57
	case KeySpace:
		return 0x20
	}
	return 0
}

func gdkMods(m Mods) uint32 {
	var s uint32
	if m.Shift {
		s |= 1
	}
	if m.Ctrl {
		s |= 4
	}
	if m.Alt {
		s |= 8
	}
	if m.Super {
		s |= 64
	}
	return s
}

// ForwardPointer synthesizes a GDK pointer event. The subsurface is
// input-transparent, so SDL owns the pointer and the view follows the pointer
// over its box; this keeps goi's focus handling in one place.
func (w *linuxWebview) ForwardPointer(kind PointerKind, x, y float64, button int, dx, dy float64) {
	if w.win == 0 {
		return
	}
	gdkwin := gtkWidgetGetWindow(w.win)
	if gdkwin == nil {
		return
	}
	var typ int32
	switch kind {
	case PointerDown:
		typ = 4 // GDK_BUTTON_PRESS
	case PointerUp:
		typ = 7 // GDK_BUTTON_RELEASE
	case PointerScroll:
		typ = 31 // GDK_SCROLL
	default:
		typ = 3 // GDK_MOTION_NOTIFY
	}
	ev := gdkEventNew(typ)
	if ev == nil {
		return
	}
	gObjectRef(gdkwin)
	*(*unsafe.Pointer)(unsafe.Add(ev, 8)) = gdkwin // GdkWindow*
	*(*byte)(unsafe.Add(ev, 16)) = 1               // send_event
	*(*float64)(unsafe.Add(ev, 24)) = x
	*(*float64)(unsafe.Add(ev, 32)) = y
	switch kind {
	case PointerDown, PointerUp:
		*(*uint32)(unsafe.Add(ev, 48)) = 0 // state
		*(*uint32)(unsafe.Add(ev, 52)) = uint32(button)
	case PointerScroll:
		*(*uint32)(unsafe.Add(ev, 40)) = 0
		*(*int32)(unsafe.Add(ev, 44)) = 4 // GDK_SCROLL_SMOOTH
		*(*float64)(unsafe.Add(ev, 72)) = dx * 50
		*(*float64)(unsafe.Add(ev, 80)) = dy * 50
	default:
		*(*uint32)(unsafe.Add(ev, 48)) = 0
		*(*int16)(unsafe.Add(ev, 52)) = 1 // is_hint
	}
	if seat := gdkDisplayGetDefaultSeat(gdkDisplay); seat != 0 {
		if dev := gdkSeatGetPointer(seat); dev != 0 {
			gdkEventSetDevice(ev, dev)
		}
	}
	gtkMainDoEvent(ev)
	gdkEventFree(ev)
}

// Focus gives the web view GTK's keyboard focus, so forwarded keys reach the
// page and its focused form control.
func (w *linuxWebview) Focus() {
	if w.win != 0 && w.view != 0 {
		gtkWindowSetFocus(w.win, w.view)
	}
}

func (w *linuxWebview) Destroy() {
	if w.handlerID != 0 {
		unregisterMessage(w.handlerID)
		w.handlerID = 0
	}
	if w.focusID != 0 {
		unregisterFocus(w.focusID)
		w.focusID = 0
	}
	if w.sub != 0 {
		wlMarshal(0, w.sub, 0, 0, 0, 1)
		w.sub = 0
	}
	if w.win != 0 {
		gtkWidgetDestroy(w.win)
		w.win = 0
	}
	w.view, w.manager, w.surface = 0, 0, 0
}

var (
	focusMu       sync.Mutex
	focusNext     uint64
	focusHandlers = map[uintptr]func(){}
)

func registerFocus(fn func()) uintptr {
	id := uintptr(atomic.AddUint64(&focusNext, 1))
	focusMu.Lock()
	focusHandlers[id] = fn
	focusMu.Unlock()
	return id
}

func unregisterFocus(id uintptr) {
	focusMu.Lock()
	delete(focusHandlers, id)
	focusMu.Unlock()
}
