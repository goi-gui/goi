//go:build linux && !js && (webview || all)

package platform

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
// The subsurface owns the pointer inside its box, so WebKit sees real pointer
// events. The keyboard is forwarded: Wayland gives it to a toplevel, and a
// subsurface is not one.
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
	gtkOverlayNew                  func() uintptr
	gtkOverlayAddOverlay           func(uintptr, uintptr)
	gtkBoxNew                      func(int32, int32) uintptr
	gtkButtonNewWithLabel          func(*byte) uintptr
	gtkWidgetSetSensitive          func(uintptr, int32)
	gtkWidgetActivate              func(uintptr)
	gtkButtonSetAlignment          func(uintptr, float32, float32)
	gtkSeparatorNew                func(int32) uintptr
	gtkWidgetGetStyleContext       func(uintptr) uintptr
	gtkStyleContextAddClass        func(uintptr, *byte)
	gtkCssProviderNew              func() uintptr
	gtkCssProviderLoadData         func(uintptr, *byte, int64, uintptr) int32
	gtkStyleContextAddProvider     func(uintptr, uintptr, uint32)
	gdkScreenGetDefault            func() uintptr
	webkitOptionMenuItemSelected   func(uintptr) int32
	gtkWidgetSetHalign             func(uintptr, int32)
	gtkWidgetSetValign             func(uintptr, int32)
	gtkWidgetSetMarginStart        func(uintptr, int32)
	gtkWidgetSetMarginTop          func(uintptr, int32)
	gtkWidgetSetSizeRequest        func(uintptr, int32, int32)
	gtkWidgetGetPreferredSize      func(uintptr, uintptr, uintptr)
	gtkWidgetEvent                 func(uintptr, unsafe.Pointer) int32
	gtkWidgetAddEvents             func(uintptr, int32)
	gtkEventsPending               func() int32
	gtkMainIteration               func() int32
	gtkMainIterationDo             func(int32) int32
	gdkDisplayGetDefault           func() uintptr
	gdkSeatGetPointer              func(uintptr) uintptr
	gdkEventPut                    func(unsafe.Pointer)
	gdkWindowGetOrigin             func(unsafe.Pointer, *int32, *int32) int32
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
	webkitWebViewIsLoading         func(uintptr) int32
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
	webkitOptionMenuGetNItems      func(uintptr) uint32
	webkitOptionMenuGetItem        func(uintptr, uint32) uintptr
	webkitOptionMenuActivateItem   func(uintptr, uint32)
	webkitOptionMenuClose          func(uintptr)
	webkitOptionMenuItemGetLabel   func(uintptr) *byte
	webkitOptionMenuItemGroupLabel func(uintptr) int32
	webkitOptionMenuItemEnabled    func(uintptr) int32
	webkitContextMenuGetItems      func(uintptr) unsafe.Pointer
	webkitContextMenuItemTitle     func(uintptr) unsafe.Pointer
	webkitContextMenuItemSeparator func(uintptr) int32
	webkitContextMenuItemSubmenu   func(uintptr) unsafe.Pointer
	webkitContextMenuItemStock     func(uintptr) int32
	webkitStopLoading              func(uintptr)
	webkitExecuteEditingCommand    func(uintptr, *byte)
	webkitHitTestLinkURI           func(uintptr) *byte
	webkitGetInspector             func(uintptr) uintptr
	webkitInspectorShow            func(uintptr)
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
	purego.RegisterLibFunc(&gtkOverlayNew, gtkLib, "gtk_overlay_new")
	purego.RegisterLibFunc(&gtkOverlayAddOverlay, gtkLib, "gtk_overlay_add_overlay")
	purego.RegisterLibFunc(&gtkBoxNew, gtkLib, "gtk_box_new")
	purego.RegisterLibFunc(&gtkButtonNewWithLabel, gtkLib, "gtk_button_new_with_label")
	purego.RegisterLibFunc(&gtkWidgetSetSensitive, gtkLib, "gtk_widget_set_sensitive")
	purego.RegisterLibFunc(&gtkWidgetActivate, gtkLib, "gtk_widget_activate")
	purego.RegisterLibFunc(&gtkButtonSetAlignment, gtkLib, "gtk_button_set_alignment")
	purego.RegisterLibFunc(&gtkSeparatorNew, gtkLib, "gtk_separator_new")
	purego.RegisterLibFunc(&gtkWidgetGetStyleContext, gtkLib, "gtk_widget_get_style_context")
	purego.RegisterLibFunc(&gtkStyleContextAddClass, gtkLib, "gtk_style_context_add_class")
	purego.RegisterLibFunc(&gtkCssProviderNew, gtkLib, "gtk_css_provider_new")
	purego.RegisterLibFunc(&gtkCssProviderLoadData, gtkLib, "gtk_css_provider_load_from_data")
	purego.RegisterLibFunc(&gtkStyleContextAddProvider, gtkLib, "gtk_style_context_add_provider_for_screen")
	purego.RegisterLibFunc(&gdkScreenGetDefault, gdkLib, "gdk_screen_get_default")
	purego.RegisterLibFunc(&gtkWidgetSetHalign, gtkLib, "gtk_widget_set_halign")
	purego.RegisterLibFunc(&gtkWidgetSetValign, gtkLib, "gtk_widget_set_valign")
	purego.RegisterLibFunc(&gtkWidgetSetMarginStart, gtkLib, "gtk_widget_set_margin_start")
	purego.RegisterLibFunc(&gtkWidgetSetMarginTop, gtkLib, "gtk_widget_set_margin_top")
	purego.RegisterLibFunc(&gtkWidgetSetSizeRequest, gtkLib, "gtk_widget_set_size_request")
	purego.RegisterLibFunc(&gtkWidgetGetPreferredSize, gtkLib, "gtk_widget_get_preferred_size")
	purego.RegisterLibFunc(&gtkWidgetEvent, gtkLib, "gtk_widget_event")
	purego.RegisterLibFunc(&gtkWidgetAddEvents, gtkLib, "gtk_widget_add_events")
	purego.RegisterLibFunc(&gtkEventsPending, gtkLib, "gtk_events_pending")
	purego.RegisterLibFunc(&gtkMainIteration, gtkLib, "gtk_main_iteration")
	purego.RegisterLibFunc(&gtkMainIterationDo, gtkLib, "gtk_main_iteration_do")
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
	purego.RegisterLibFunc(&gdkEventPut, gdkLib, "gdk_event_put")
	purego.RegisterLibFunc(&gdkWindowGetOrigin, gdkLib, "gdk_window_get_origin")
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
	purego.RegisterLibFunc(&webkitWebViewIsLoading, webkitLib, "webkit_web_view_is_loading")
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
	purego.RegisterLibFunc(&webkitOptionMenuGetNItems, webkitLib, "webkit_option_menu_get_n_items")
	purego.RegisterLibFunc(&webkitOptionMenuGetItem, webkitLib, "webkit_option_menu_get_item")
	purego.RegisterLibFunc(&webkitOptionMenuActivateItem, webkitLib, "webkit_option_menu_activate_item")
	purego.RegisterLibFunc(&webkitOptionMenuClose, webkitLib, "webkit_option_menu_close")
	purego.RegisterLibFunc(&webkitOptionMenuItemGetLabel, webkitLib, "webkit_option_menu_item_get_label")
	purego.RegisterLibFunc(&webkitOptionMenuItemGroupLabel, webkitLib, "webkit_option_menu_item_is_group_label")
	purego.RegisterLibFunc(&webkitOptionMenuItemEnabled, webkitLib, "webkit_option_menu_item_is_enabled")
	purego.RegisterLibFunc(&webkitOptionMenuItemSelected, webkitLib, "webkit_option_menu_item_is_selected")
	purego.RegisterLibFunc(&webkitContextMenuGetItems, webkitLib, "webkit_context_menu_get_items")
	purego.RegisterLibFunc(&webkitContextMenuItemTitle, webkitLib, "webkit_context_menu_item_get_title")
	purego.RegisterLibFunc(&webkitContextMenuItemSeparator, webkitLib, "webkit_context_menu_item_is_separator")
	purego.RegisterLibFunc(&webkitContextMenuItemSubmenu, webkitLib, "webkit_context_menu_item_get_submenu")
	purego.RegisterLibFunc(&webkitContextMenuItemStock, webkitLib, "webkit_context_menu_item_get_stock_action")
	purego.RegisterLibFunc(&webkitStopLoading, webkitLib, "webkit_web_view_stop_loading")
	purego.RegisterLibFunc(&webkitExecuteEditingCommand, webkitLib, "webkit_web_view_execute_editing_command")
	purego.RegisterLibFunc(&webkitHitTestLinkURI, webkitLib, "webkit_hit_test_result_get_link_uri")
	purego.RegisterLibFunc(&webkitGetInspector, webkitLib, "webkit_web_view_get_inspector")
	purego.RegisterLibFunc(&webkitInspectorShow, webkitLib, "webkit_web_inspector_show")
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

	// GTK's Wayland input method would take the text input focus from SDL on
	// the shared connection, which stops committed text. The simple context
	// keeps GTK off that protocol; goi forwards keys itself.
	if os.Getenv("GTK_IM_MODULE") == "" {
		os.Setenv("GTK_IM_MODULE", "gtk-im-context-simple")
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
	styleMenus()
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
	for i := 0; i < 1<<16; i++ {
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

// liveViews lets a window focus change reach every open view.
var liveViews []*linuxWebview

// SetWebviewFocus tells the GTK windows whether the app has focus. Wayland
// gives the keyboard to the SDL toplevel, so without this the engine would
// treat its window as inactive and draw no caret or focus ring.
func SetWebviewFocus(focused bool) {
	for _, w := range liveViews {
		w.focusIn(focused)
	}
}

// WebviewsLoading reports whether a page is still loading. The loop then wakes
// often; a settled page only needs a slow poll, which keeps an idle app cheap.
func WebviewsLoading() bool {
	for _, w := range liveViews {
		if w.view != 0 && webkitWebViewIsLoading(w.view) != 0 {
			return true
		}
	}
	return false
}

// PumpWebviews advances GTK's event loop. It runs on the UI goroutine, where
// GTK was initialized.
func PumpWebviews() bool {
	if !webviewOK {
		return false
	}
	for gtkEventsPending() != 0 {
		gtkMainIteration()
	}
	// GDK may keep input on its own Wayland queue, which gtk_events_pending
	// does not report; a few non-blocking iterations drain it.
	for i := 0; i < 8; i++ {
		if gtkMainIterationDo(0) == 0 {
			break
		}
	}
	// GTK renders on its own loop, so an idle app must still wake it.
	return true
}

// sendFocus tells the GTK window whether the app has focus. Wayland gives the
// keyboard to the SDL toplevel, so the garden's window would otherwise stay
// "inactive" and the engine would draw no caret and no focus rings.
func sendFocus(win unsafe.Pointer, in bool) {
	if win == nil {
		return
	}
	ev := gdkEventNew(12) // GDK_FOCUS_CHANGE
	if ev == nil {
		return
	}
	var v int16
	if in {
		v = 1
	}
	gObjectRef(win)
	*(*unsafe.Pointer)(unsafe.Add(ev, 8)) = win
	*(*byte)(unsafe.Add(ev, 16)) = 1
	*(*int16)(unsafe.Add(ev, 18)) = v
	if seat := gdkDisplayGetDefaultSeat(gdkDisplay); seat != 0 {
		if dev := gdkSeatGetPointer(seat); dev != 0 {
			gdkEventSetDevice(ev, dev)
		}
	}
	gdkEventPut(ev)
	gdkEventFree(ev)
}

type linuxWebview struct {
	win, view, manager uintptr
	surface, sub       uintptr
	x, y, w, h         float64
	iw, ih             float64
	rw, rh             float64
	focusedOnce        bool
	visible            bool
	message            func(string)
	focus              func()
	handlerID          uintptr
	focusID            uintptr
	focusCallback      uintptr

	overlay    uintptr
	menu       uintptr
	menuBox    uintptr
	menuItems  map[uintptr]uint32
	menuAt     int64
	ctxItems   map[uintptr]ctxAction
	ctxButtons []uintptr
}

// ctxAction is one context-menu row: the GAction to run and its parameter.
type ctxAction struct {
	view  uintptr
	stock int32
	link  string
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
	// A select's popup cannot be positioned on a roleless surface, so the menu
	// is drawn by an overlay inside this window instead.
	w.overlay = gtkOverlayNew()
	gtkContainerAdd(win, w.overlay)
	gtkContainerAdd(w.overlay, w.view)
	// Synthetic events reach GTK only for masks the widgets selected.
	const mask = (1 << 8) | (1 << 9) | (1 << 2) | (1 << 10) | (1 << 11) | (1 << 21) | (1 << 23)
	gtkWidgetAddEvents(win, mask)
	gtkWidgetAddEvents(w.view, mask)
	gtkWidgetRealize(win)
	gdkwin := gtkWidgetGetWindow(win)
	if gdkwin == nil {
		w.Destroy()
		return nil, fmt.Errorf("%w: gtk_widget_get_window", ErrWebviewUnavailable)
	}
	gdkWaylandWindowSetCustom(gdkwin)
	w.surface = gdkWaylandWindowGetWlSurface(gdkwin)
	if w.surface == 0 {
		w.Destroy()
		return nil, fmt.Errorf("%w: the GTK window has no wl_surface", ErrWebviewUnavailable)
	}
	w.sub = wlMarshal(wlSubcompositorIface, wlSubcompositor, 1, wlSubsurfaceInterface, 1, 0,
		0, uint64(w.surface), uint64(handle))
	if w.sub == 0 {
		w.Destroy()
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
	liveViews = append(liveViews, w)
	if onFocus != nil {
		w.focusID = registerFocus(onFocus)
		// A click anywhere in the view's window is a click in the page; the
		// view itself may not emit when GTK delivers the press to the frame.
		gSignalConnectData(win, cstr("button-press-event"), w.focusCallback, w.focusID, 0, 0)
		gSignalConnectData(w.view, cstr("button-press-event"), w.focusCallback, w.focusID, 0, 0)
	}
	initMenuCallbacks()
	viewByPtr[w.view] = w
	gSignalConnectData(w.view, cstr("show-option-menu"), optionMenuCB, w.view, 0, 0)
	gSignalConnectData(w.view, cstr("context-menu"), contextMenuCB, w.view, 0, 0)
	gSignalConnectData(w.view, cstr("button-press-event"), closeMenuCB, w.view, 0, 0)
	gtkWidgetShowAll(win)
	gtkWindowSetFocus(win, w.view)
	return w, nil
}

// setInput gives the view the pointer inside its box, so the compositor sends
// hover, clicks and the wheel to WebKit directly.
func (w *linuxWebview) setInput(width, height float64) {
	if w.surface == 0 {
		return
	}
	region := wlMarshal(wlRegionInterface, wlCompositor, 1, wlRegionInterface, 1, 0, 0)
	if region == 0 {
		return
	}
	wlMarshal(0, region, 1, 0, 0, 0,
		uint64(uint32(0)), uint64(uint32(0)), uint64(uint32(width)), uint64(uint32(height)))
	wlMarshal(0, w.surface, 5, 0, 0, 0, uint64(region))
	wlMarshal(0, region, 0, 0, 0, 1)
	wlMarshal(0, w.surface, 6, 0, 0, 0)
}

// clearInput makes the view input-transparent, for a hidden view and for the
// gap between the SDL surface and its subsurface.
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
		if w.iw != 0 {
			w.clearInput()
			w.iw, w.ih = 0, 0
		}
		if w.sub != 0 {
			wlMarshal(0, w.sub, 1, 0, 0, 0, uint64(0xffffe000), uint64(0xffffe000))
		}
		return
	}
	if w.sub != 0 {
		wlMarshal(0, w.sub, 1, 0, 0, 0, uint64(uint32(int32(x))), uint64(uint32(int32(y))))
	}
	if !w.focusedOnce {
		// The SDL toplevel holds the Wayland keyboard focus, so the engine's
		// window has to be told that the app is focused once it is mapped.
		w.focusedOnce = true
		w.focusIn(true)
	}
	if w.rw != width || w.rh != height {
		w.rw, w.rh = width, height
		gtkWindowResize(w.win, int32(width), int32(height))
	}
	if w.iw != width || w.ih != height {
		w.iw, w.ih = width, height
		w.setInput(width, height)
	}
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
	*(*uint16)(unsafe.Add(ev, 48)) = hwKeycode(keyval)
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
	if k >= KeyA && k <= KeyZ {
		return uint32('a' + (k - KeyA))
	}
	return 0
}

// hwKeycode is the X11 keycode (evdev + 8) for a keyval. The engine uses it to
// recognise editing keys; a synthetic event without one reaches the page's
// keydown but performs no edit.
func hwKeycode(keyval uint32) uint16 {
	switch keyval {
	case 0xff08:
		return 22 // BackSpace
	case 0xffff:
		return 119 // Delete
	case 0xff51:
		return 113 // Left
	case 0xff53:
		return 114 // Right
	case 0xff52:
		return 111 // Up
	case 0xff54:
		return 116 // Down
	case 0xff50:
		return 110 // Home
	case 0xff57:
		return 115 // End
	case 0xff0d:
		return 36 // Return
	case 0xff09:
		return 23 // Tab
	case 0xff1b:
		return 9 // Escape
	case 0x20:
		return 65 // space
	}
	if keyval >= 'a' && keyval <= 'z' {
		return letterKeycode[keyval-'a']
	}
	return 0
}

// letterKeycode is the X11 keycode of each Latin letter on a QWERTY layout.
var letterKeycode = [26]uint16{
	38, 56, 54, 40, 26, 41, 42, 43, 31, 44, 45, 46, 58,
	57, 32, 33, 24, 27, 39, 28, 30, 55, 25, 53, 29, 52,
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

// ForwardPointer queues a pointer event on the view's own GdkWindow. The
// subsurface is input-transparent, so SDL keeps the pointer; a GdkEvent handed
// to gtk_main_do_event would stop at the frame, because GTK does not hit-test
// down from there. gdk_event_put with the view's window has the engine's widget
// as its event widget, which is what a real event carries.
func (w *linuxWebview) ForwardPointer(kind PointerKind, x, y float64, button int, dx, dy float64, shift bool) {
	win := gtkWidgetGetWindow(w.view)
	if win == nil {
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
	var ox, oy int32
	gdkWindowGetOrigin(win, &ox, &oy)
	xr, yr := float64(ox)+x, float64(oy)+y
	gObjectRef(win)
	*(*unsafe.Pointer)(unsafe.Add(ev, 8)) = win // GdkWindow*
	*(*byte)(unsafe.Add(ev, 16)) = 1            // send_event
	*(*uint32)(unsafe.Add(ev, 20)) = 0          // time
	*(*float64)(unsafe.Add(ev, 24)) = x
	*(*float64)(unsafe.Add(ev, 32)) = y
	state := uint32(0)
	if shift {
		state |= 1 // GDK_SHIFT_MASK
	}
	switch kind {
	case PointerDown, PointerUp:
		*(*uint32)(unsafe.Add(ev, 48)) = state
		*(*uint32)(unsafe.Add(ev, 52)) = uint32(button)
		*(*float64)(unsafe.Add(ev, 64)) = xr
		*(*float64)(unsafe.Add(ev, 72)) = yr
	case PointerScroll:
		*(*uint32)(unsafe.Add(ev, 40)) = state
		*(*int32)(unsafe.Add(ev, 44)) = 4 // GDK_SCROLL_SMOOTH
		*(*float64)(unsafe.Add(ev, 56)) = xr
		*(*float64)(unsafe.Add(ev, 64)) = yr
		*(*float64)(unsafe.Add(ev, 72)) = dx * 50
		*(*float64)(unsafe.Add(ev, 80)) = dy * 50
	default:
		*(*uint32)(unsafe.Add(ev, 48)) = state
		*(*int16)(unsafe.Add(ev, 52)) = 0 // not a hint: the hint query would return the real pointer
		*(*float64)(unsafe.Add(ev, 64)) = xr
		*(*float64)(unsafe.Add(ev, 72)) = yr
	}
	if seat := gdkDisplayGetDefaultSeat(gdkDisplay); seat != 0 {
		if dev := gdkSeatGetPointer(seat); dev != 0 {
			gdkEventSetDevice(ev, dev)
		}
	}
	gdkEventPut(ev)
	gdkEventFree(ev)
}

// viewByPtr finds a view from a GTK widget, for the signals that carry one.
var viewByPtr = map[uintptr]*linuxWebview{}

var optionMenuCB, closeMenuCB, menuClickCB, contextMenuCB, contextClickCB uintptr

func initMenuCallbacks() {
	if optionMenuCB != 0 {
		return
	}
	optionMenuCB = purego.NewCallback(func(view, menu, _ uintptr, rect unsafe.Pointer) int32 {
		return showOptionMenu(view, menu, rect)
	})
	closeMenuCB = purego.NewCallback(func(view, _ uintptr, _ uintptr) int32 {
		if w := viewByPtr[view]; w != nil && w.menuBox != 0 {
			// A press that just opened this menu (a right-click) must not
			// close it; only a later press outside does.
			if time.Since(time.Unix(0, w.menuAt)) < 250*time.Millisecond {
				return 0
			}
			if w.menu != 0 {
				webkitOptionMenuClose(w.menu)
			}
			w.closeMenu()
		}
		return 0
	})
	menuClickCB = purego.NewCallback(func(btn, _ uintptr, _ uintptr) int32 {
		return menuClicked(btn)
	})
	contextMenuCB = purego.NewCallback(func(view, menu uintptr, event unsafe.Pointer, hit uintptr) int32 {
		return showContextMenu(view, menu, event, hit)
	})
	contextClickCB = purego.NewCallback(func(btn, _ uintptr, _ uintptr) int32 {
		return contextClicked(btn)
	})
}

// menuCSS styles the overlay menus: a rounded card with flat rows, so they read
// as a menu rather than a stack of buttons.
const menuCSS = `
.goi-menu {
	background-color: @theme_bg_color;
	border-radius: 12px;
	padding: 6px;
	border: 1px solid alpha(@theme_fg_color, 0.12);
	box-shadow: 0 6px 20px alpha(black, 0.35);
}
.goi-menu button {
	background: none;
	background-image: none;
	border: none;
	border-radius: 8px;
	box-shadow: none;
	padding: 7px 12px;
	min-height: 20px;
	color: @theme_fg_color;
	font-size: 13px;
}
.goi-menu button:hover { background-color: alpha(@theme_fg_color, 0.10); }
.goi-menu button:active { background-color: alpha(@theme_fg_color, 0.16); }
.goi-menu button:disabled { color: alpha(@theme_fg_color, 0.45); }
.goi-menu separator {
	background-color: alpha(@theme_fg_color, 0.12);
	min-height: 1px;
	margin: 4px 8px;
}
`

var menuStyled bool

func styleMenus() {
	if menuStyled {
		return
	}
	menuStyled = true
	provider := gtkCssProviderNew()
	if provider == 0 {
		return
	}
	gtkCssProviderLoadData(provider, cstr(menuCSS), int64(len(menuCSS)), 0)
	if screen := gdkScreenGetDefault(); screen != 0 {
		gtkStyleContextAddProvider(screen, provider, 600) // GTK_STYLE_PROVIDER_PRIORITY_APPLICATION
	}
}

// menuPlace puts a menu box at (x, y) inside the view, pulled back so it never
// runs past the right or bottom edge and gets cut off.
func menuPlace(w *linuxWebview, box uintptr, x, y, width int32) {
	var min, natural struct{ W, H int32 }
	gtkWidgetGetPreferredSize(box, uintptr(unsafe.Pointer(&min)), uintptr(unsafe.Pointer(&natural)))
	height := natural.H
	if height <= 0 {
		height = 120
	}
	if width <= 0 {
		width = natural.W
	}
	maxX := int32(w.w) - width
	maxY := int32(w.h) - height
	x = min32(x, maxX)
	y = min32(y, maxY)
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	gtkWidgetSetHalign(box, 1) // GTK_ALIGN_START
	gtkWidgetSetValign(box, 1)
	gtkWidgetSetMarginStart(box, x)
	gtkWidgetSetMarginTop(box, y)
	gtkWidgetSetSizeRequest(box, width, -1)
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// menuRow makes one flat, left-aligned row in a menu box.
func menuRow(box uintptr, label string) uintptr {
	btn := gtkButtonNewWithLabel(cstr(label))
	gtkButtonSetAlignment(btn, 0, 0.5)
	gtkStyleContextAddClass(gtkWidgetGetStyleContext(btn), cstr("flat"))
	gtkContainerAdd(box, btn)
	return btn
}

// showContextMenu draws the page's context menu at the click. WebKit's own
// menu is a separate toplevel, which cannot be positioned here.
func showContextMenu(view, menu uintptr, event unsafe.Pointer, hit uintptr) int32 {
	w := viewByPtr[view]
	if w == nil || w.overlay == 0 || menu == 0 {
		return 0
	}
	w.closeMenu()
	link := ""
	if hit != 0 {
		if uri := webkitHitTestLinkURI(hit); uri != nil {
			link = goString(unsafe.Pointer(uri))
		}
	}
	box := gtkBoxNew(1, 0)
	gtkStyleContextAddClass(gtkWidgetGetStyleContext(box), cstr("goi-menu"))
	w.ctxItems = map[uintptr]ctxAction{}
	list := webkitContextMenuGetItems(menu)
	for node := list; node != nil; {
		data := *(*unsafe.Pointer)(node)                                        // GList.data
		next := *(*unsafe.Pointer)(unsafe.Add(node, unsafe.Sizeof(uintptr(0)))) // GList.next
		node = next
		item := uintptr(data)
		if item == 0 || webkitContextMenuItemSubmenu(item) != nil {
			continue
		}
		if webkitContextMenuItemSeparator(item) != 0 {
			if len(w.ctxItems) > 0 {
				gtkContainerAdd(box, gtkSeparatorNew(0)) // GTK_ORIENTATION_HORIZONTAL
			}
			continue
		}
		stock := webkitContextMenuItemStock(item)
		if !contextActionRuns(stock) {
			continue
		}
		title := webkitContextMenuItemTitle(item)
		btn := menuRow(box, strings.ReplaceAll(goString(title), "_", ""))
		w.ctxItems[btn] = ctxAction{view: view, stock: stock, link: link}
		w.ctxButtons = append(w.ctxButtons, btn)
		gSignalConnectData(btn, cstr("clicked"), contextClickCB, btn, 0, 0)
	}
	if len(w.ctxItems) == 0 {
		gtkWidgetDestroy(box)
		return 0
	}
	var x, y int32
	if event != nil {
		x = int32(*(*float64)(unsafe.Add(event, 24)))
		y = int32(*(*float64)(unsafe.Add(event, 32)))
	}
	menuPlace(w, box, x, y, 220)
	gtkOverlayAddOverlay(w.overlay, box)
	gtkWidgetShowAll(box)
	w.menuBox, w.menuAt = box, time.Now().UnixNano()
	return 1
}

// contextActionRuns reports whether this backend can perform the action. The
// rows it cannot run are left out rather than shown dead.
//
// shortcut: spelling suggestions, font menus and media actions are not mapped;
// add their stock action here when a design needs them.
func contextActionRuns(stock int32) bool {
	switch stock {
	case 1, 2, 4, 10, 11, 12, 13, 14, 15, 16, 17, 18, 27, 28, 29, 31: // open/copy link, history, edit, style, inspect
		return true
	}
	return false
}

// runContextAction performs one stock context-menu action through WebKit.
func (a ctxAction) run() {
	switch a.stock {
	case 1, 2: // open link, open link in a new window: this view has no windows
		if a.link != "" {
			webkitWebViewLoadURI(a.view, cstr(a.link))
		}
	case 4:
		if a.link != "" {
			sdlSetClipboard(a.link)
		}
	case 10:
		webkitWebViewGoBack(a.view)
	case 11:
		webkitWebViewGoForward(a.view)
	case 12:
		webkitStopLoading(a.view)
	case 13:
		webkitWebViewReload(a.view)
	case 14:
		webkitExecuteEditingCommand(a.view, cstr("Copy"))
	case 15:
		webkitExecuteEditingCommand(a.view, cstr("Cut"))
	case 16:
		webkitExecuteEditingCommand(a.view, cstr("Paste"))
	case 17:
		webkitExecuteEditingCommand(a.view, cstr("Delete"))
	case 18:
		webkitExecuteEditingCommand(a.view, cstr("SelectAll"))
	case 27:
		webkitExecuteEditingCommand(a.view, cstr("Bold"))
	case 28:
		webkitExecuteEditingCommand(a.view, cstr("Italic"))
	case 29:
		webkitExecuteEditingCommand(a.view, cstr("Underline"))
	case 31:
		if i := webkitGetInspector(a.view); i != 0 {
			webkitInspectorShow(i)
		}
	}
}

func contextClicked(btn uintptr) int32 {
	for _, w := range liveViews {
		item, ok := w.ctxItems[btn]
		if !ok {
			continue
		}
		item.run()
		w.closeMenu()
		return 0
	}
	return 0
}

// showOptionMenu draws a select's options inside the window at the rectangle
// WebKit reports. The native popup would be a separate Wayland toplevel, which
// a roleless surface cannot position.
func showOptionMenu(view, menu uintptr, rect unsafe.Pointer) int32 {
	w := viewByPtr[view]
	if w == nil || w.overlay == 0 {
		return 0
	}
	w.closeMenu()
	n := webkitOptionMenuGetNItems(menu)
	if n == 0 {
		return 0
	}
	box := gtkBoxNew(1, 0) // GTK_ORIENTATION_VERTICAL
	gtkStyleContextAddClass(gtkWidgetGetStyleContext(box), cstr("goi-menu"))
	w.menuItems = map[uintptr]uint32{}
	for i := uint32(0); i < n; i++ {
		item := webkitOptionMenuGetItem(menu, i)
		if item == 0 || webkitOptionMenuItemGroupLabel(item) != 0 {
			continue
		}
		label := goString(unsafe.Pointer(webkitOptionMenuItemGetLabel(item)))
		if webkitOptionMenuItemSelected(item) != 0 {
			label = "✓  " + label
		} else {
			label = "    " + label
		}
		btn := menuRow(box, label)
		if webkitOptionMenuItemEnabled(item) == 0 {
			gtkWidgetSetSensitive(btn, 0)
		}
		w.menuItems[btn] = i
		gSignalConnectData(btn, cstr("clicked"), menuClickCB, btn, 0, 0)
	}
	var x, y, width int32
	if rect != nil {
		x = *(*int32)(unsafe.Add(rect, 0))
		y = *(*int32)(unsafe.Add(rect, 4))
		width = *(*int32)(unsafe.Add(rect, 8))
	}
	if width < 160 {
		width = 160
	}
	menuPlace(w, box, x, y, width)
	gtkOverlayAddOverlay(w.overlay, box)
	gtkWidgetShowAll(box)
	w.menu, w.menuBox, w.menuAt = menu, box, time.Now().UnixNano()
	return 1
}

// closeMenu removes the option list, if one is open.
func (w *linuxWebview) closeMenu() {
	if w.menuBox != 0 {
		// destroy unparents on its own; removing first can drop the last
		// reference and leave this pointer dangling.
		gtkWidgetDestroy(w.menuBox)
	}
	w.menu, w.menuBox, w.menuItems, w.ctxItems, w.ctxButtons = 0, 0, nil, nil, nil
}

func menuClicked(btn uintptr) int32 {
	for _, w := range liveViews {
		index, ok := w.menuItems[btn]
		if !ok {
			continue
		}
		if w.menu != 0 {
			webkitOptionMenuActivateItem(w.menu, index)
			webkitOptionMenuClose(w.menu)
		}
		w.closeMenu()
		return 0
	}
	return 0
}

// Focus gives the web view GTK's keyboard focus, so forwarded keys reach the
// page and its focused form control.
func (w *linuxWebview) Focus() {
	if w.win != 0 && w.view != 0 {
		gtkWindowSetFocus(w.win, w.view)
	}
}

// focusIn forwards the app's focus state to the GTK window.
func (w *linuxWebview) focusIn(in bool) {
	if w.win != 0 {
		sendFocus(gtkWidgetGetWindow(w.win), in)
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
	delete(viewByPtr, w.view)
	w.closeMenu()
	for i, v := range liveViews {
		if v == w {
			liveViews = append(liveViews[:i], liveViews[i+1:]...)
			break
		}
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
