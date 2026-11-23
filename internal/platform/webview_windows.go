//go:build windows && !js && (webview || all)

package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego"
)

// This is the Windows backend: Microsoft Edge WebView2 as a child window of
// the SDL window. WebView2 draws and takes input in-process, so the view needs
// no synthetic pointer or key forwarding; goi only moves it and loads pages.
//
// The COM interfaces are called through their vtables, whose slot order is the
// IDL order from WebView2.h. The runtime is located without shipping
// WebView2Loader.dll: the installed Edge runtime's EmbeddedBrowserWebView.dll
// exports the same environment constructor. A loader DLL next to the exe is
// preferred when present, because that export is the documented one.
//
// shortcut: this code is cross-compiled but has not run on Windows yet; the
// first run on real hardware should exercise load, bounds and the bridge.

// --- Win32 and COM plumbing ------------------------------------------------

type comProc uintptr

func (p comProc) call(self unsafe.Pointer, args ...uintptr) uintptr {
	r, _, _ := purego.SyscallN(uintptr(p), append([]uintptr{uintptr(self)}, args...)...)
	return r
}

type unknownVtbl struct {
	QueryInterface comProc
	AddRef         comProc
	Release        comProc
}

type envVtbl struct {
	unknownVtbl
	CreateCoreWebView2Controller     comProc
	CreateWebResourceResponse        comProc
	GetBrowserVersionString          comProc
	AddNewBrowserVersionAvailable    comProc
	RemoveNewBrowserVersionAvailable comProc
}

type controllerVtbl struct {
	unknownVtbl
	GetIsVisible                      comProc
	PutIsVisible                      comProc
	GetBounds                         comProc
	PutBounds                         comProc
	GetZoomFactor                     comProc
	PutZoomFactor                     comProc
	AddZoomFactorChanged              comProc
	RemoveZoomFactorChanged           comProc
	SetBoundsAndZoomFactor            comProc
	MoveFocus                         comProc
	AddMoveFocusRequested             comProc
	RemoveMoveFocusRequested          comProc
	AddGotFocus                       comProc
	RemoveGotFocus                    comProc
	AddLostFocus                      comProc
	RemoveLostFocus                   comProc
	AddAcceleratorKeyPressed          comProc
	RemoveAcceleratorKeyPressed       comProc
	GetParentWindow                   comProc
	PutParentWindow                   comProc
	NotifyParentWindowPositionChanged comProc
	Close                             comProc
	GetCoreWebView2                   comProc
}

// coreVtbl lists every ICoreWebView2 slot in IDL order: a missing slot would
// silently shift every method after it onto the wrong function.
type coreVtbl struct {
	unknownVtbl
	GetSettings                            comProc
	GetSource                              comProc
	Navigate                               comProc
	NavigateToString                       comProc
	AddNavigationStarting                  comProc
	RemoveNavigationStarting               comProc
	AddContentLoading                      comProc
	RemoveContentLoading                   comProc
	AddSourceChanged                       comProc
	RemoveSourceChanged                    comProc
	AddHistoryChanged                      comProc
	RemoveHistoryChanged                   comProc
	AddNavigationCompleted                 comProc
	RemoveNavigationCompleted              comProc
	AddFrameNavigationStarting             comProc
	RemoveFrameNavigationStarting          comProc
	AddFrameNavigationCompleted            comProc
	RemoveFrameNavigationCompleted         comProc
	AddScriptDialogOpening                 comProc
	RemoveScriptDialogOpening              comProc
	AddPermissionRequested                 comProc
	RemovePermissionRequested              comProc
	AddProcessFailed                       comProc
	RemoveProcessFailed                    comProc
	AddScriptToExecuteOnDocumentCreated    comProc
	RemoveScriptToExecuteOnDocumentCreated comProc
	ExecuteScript                          comProc
	CapturePreview                         comProc
	Reload                                 comProc
	PostWebMessageAsJSON                   comProc
	PostWebMessageAsString                 comProc
	AddWebMessageReceived                  comProc
	RemoveWebMessageReceived               comProc
	CallDevToolsProtocolMethod             comProc
	GetBrowserProcessID                    comProc
	GetCanGoBack                           comProc
	GetCanGoForward                        comProc
	GoBack                                 comProc
	GoForward                              comProc
	GetDevToolsProtocolEventReceiver       comProc
	Stop                                   comProc
	AddNewWindowRequested                  comProc
	RemoveNewWindowRequested               comProc
	AddDocumentTitleChanged                comProc
	RemoveDocumentTitleChanged             comProc
	GetDocumentTitle                       comProc
	AddHostObjectToScript                  comProc
	RemoveHostObjectFromScript             comProc
	OpenDevToolsWindow                     comProc
	AddContainsFullScreenElementChanged    comProc
	RemoveContainsFullScreenElementChanged comProc
	GetContainsFullScreenElement           comProc
	AddWebResourceRequested                comProc
	RemoveWebResourceRequested             comProc
	AddWebResourceRequestedFilter          comProc
	RemoveWebResourceRequestedFilter       comProc
	AddWindowCloseRequested                comProc
	RemoveWindowCloseRequested             comProc
}

type settingsVtbl struct {
	unknownVtbl
	GetIsScriptEnabled                comProc
	PutIsScriptEnabled                comProc
	GetIsWebMessageEnabled            comProc
	PutIsWebMessageEnabled            comProc
	GetAreDefaultScriptDialogsEnabled comProc
	PutAreDefaultScriptDialogsEnabled comProc
	GetIsStatusBarEnabled             comProc
	PutIsStatusBarEnabled             comProc
	GetAreDevToolsEnabled             comProc
	PutAreDevToolsEnabled             comProc
	GetAreDefaultContextMenusEnabled  comProc
	PutAreDefaultContextMenusEnabled  comProc
	GetAreHostObjectsAllowed          comProc
	PutAreHostObjectsAllowed          comProc
	GetIsZoomControlEnabled           comProc
	PutIsZoomControlEnabled           comProc
	GetIsBuiltInErrorPageEnabled      comProc
	PutIsBuiltInErrorPageEnabled      comProc
	GetUserAgent                      comProc
	PutUserAgent                      comProc
}

type messageArgsVtbl struct {
	unknownVtbl
	GetSource                comProc
	GetWebMessageAsJSON      comProc
	TryGetWebMessageAsString comProc
}

type envObj struct{ vtbl *envVtbl }
type controllerObj struct{ vtbl *controllerVtbl }
type coreObj struct{ vtbl *coreVtbl }
type settingsObj struct{ vtbl *settingsVtbl }
type messageArgsObj struct{ vtbl *messageArgsVtbl }

// --- COM handler objects Go implements -------------------------------------

// comHandler is one of the handler objects WebView2 calls back into. Handlers
// are kept for the life of the process: a view is created rarely, and a vtable
// that disappears under the runtime would crash the app.
type comHandler struct {
	vtbl   handlerVtbl
	invoke func(a uintptr, b unsafe.Pointer) uintptr
}

type handlerVtbl struct {
	unknownVtbl
	Invoke comProc
}

var (
	handlerOnce    sync.Once
	handlerProcs   handlerVtbl
	handlerObjects []*comHandler
)

func newHandler(invoke func(a uintptr, b unsafe.Pointer) uintptr) uintptr {
	handlerOnce.Do(func() {
		handlerProcs = handlerVtbl{
			unknownVtbl: unknownVtbl{
				// shortcut: every QueryInterface succeeds; WebView2 only asks
				// for the vtable it was handed.
				QueryInterface: comProc(purego.NewCallback(func(this, _ /*riid*/, out unsafe.Pointer) uintptr {
					if out != nil {
						*(*unsafe.Pointer)(out) = this
					}
					return 0
				})),
				AddRef:  comProc(purego.NewCallback(func(unsafe.Pointer) uintptr { return 1 })),
				Release: comProc(purego.NewCallback(func(unsafe.Pointer) uintptr { return 1 })),
			},
			Invoke: comProc(purego.NewCallback(func(this, a, b unsafe.Pointer) uintptr {
				h := (*comHandler)(this)
				return h.invoke(uintptr(a), b)
			})),
		}
	})
	h := &comHandler{vtbl: handlerProcs, invoke: invoke}
	handlerObjects = append(handlerObjects, h)
	return uintptr(unsafe.Pointer(h))
}

// --- Win32 calls ------------------------------------------------------------

var (
	winOnce          sync.Once
	winErr           error
	peekMessageW     func(*msg, uintptr, uint32, uint32, uint32) int32
	translateMessage func(*msg) int32
	dispatchMessageW func(*msg) uintptr
	sleepMS          func(uint32)
	coInitializeEx   func(uintptr, uint32) int32
	coTaskMemFree    func(unsafe.Pointer)
)

type msg [64]byte

type rect struct{ Left, Top, Right, Bottom int32 }

func initWin32() error {
	winOnce.Do(func() {
		user32, err := syscall.LoadLibrary("user32.dll")
		if err != nil {
			winErr = fmt.Errorf("%w: user32: %v", ErrWebviewUnavailable, err)
			return
		}
		kernel32, err := syscall.LoadLibrary("kernel32.dll")
		if err != nil {
			winErr = fmt.Errorf("%w: kernel32: %v", ErrWebviewUnavailable, err)
			return
		}
		ole32, err := syscall.LoadLibrary("ole32.dll")
		if err != nil {
			winErr = fmt.Errorf("%w: ole32: %v", ErrWebviewUnavailable, err)
			return
		}
		bind := func(fptr any, lib syscall.Handle, name string) {
			if addr, err := syscall.GetProcAddress(lib, name); err == nil {
				purego.RegisterFunc(fptr, addr)
			}
		}
		bind(&peekMessageW, user32, "PeekMessageW")
		bind(&translateMessage, user32, "TranslateMessage")
		bind(&dispatchMessageW, user32, "DispatchMessageW")
		bind(&sleepMS, kernel32, "Sleep")
		bind(&coInitializeEx, ole32, "CoInitializeEx")
		bind(&coTaskMemFree, ole32, "CoTaskMemFree")
	})
	return winErr
}

func pumpMessage() {
	var m msg
	if peekMessageW(&m, 0, 0, 0, 1) != 0 { // PM_REMOVE
		translateMessage(&m)
		dispatchMessageW(&m)
		return
	}
	sleepMS(1)
}

// InitWebview initializes COM on the UI thread before anything else uses it.
// WebView2 needs a single-threaded apartment, and SDL video may have started
// by the time a hot reload adds a view, so the order is not load-bearing here.
func InitWebview() error {
	if err := initWin32(); err != nil {
		return err
	}
	// S_OK, S_FALSE and RPC_E_CHANGED_MODE are all fine: the last means someone
	// already put this thread in a different apartment, which is not fatal for
	// a windowed child view.
	coInitializeEx(0, 2 /* COINIT_APARTMENTTHREADED */)
	return nil
}

// PumpWebviews needs no loop of its own: SDL polls the same Win32 queue that
// drives WebView2.
func PumpWebviews() bool { return false }

// --- Runtime discovery ------------------------------------------------------

// runtimeDLL finds the installed Edge WebView2 runtime's engine DLL. The
// registry is not consulted; the evergreen installer's default roots are.
//
// shortcut: a runtime installed to a custom path is not found; the registry
// key SOFTWARE\Microsoft\EdgeUpdate\ClientState is the upgrade path.
func runtimeDLL() (string, error) {
	arch := "x64"
	switch runtime.GOARCH {
	case "arm64":
		arch = "arm64"
	case "386":
		arch = "x86"
	}
	var roots []string
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
		if dir := os.Getenv(env); dir != "" {
			roots = append(roots, filepath.Join(dir, "Microsoft", "EdgeWebView", "Application"))
		}
	}
	for _, root := range roots {
		// Any installed version works: the base API has been stable since the
		// first evergreen release.
		matches, _ := filepath.Glob(filepath.Join(root, "*", "EBWebView", arch, "EmbeddedBrowserWebView.dll"))
		if len(matches) > 0 {
			return matches[0], nil
		}
	}
	return "", fmt.Errorf("%w: the Edge WebView2 runtime is not installed", ErrWebviewUnavailable)
}

// createEnvironment starts the runtime and reports its environment through
// handler. The documented loader DLL is used when the app ships one, because
// the runtime's own export is internal.
func createEnvironment(userData string, handler uintptr) error {
	dir := utf16Ptr(userData)
	defer runtime.KeepAlive(dir)
	arg := uintptr(unsafe.Pointer(&dir[0]))
	if loader, lerr := syscall.LoadLibrary("WebView2Loader.dll"); lerr == nil {
		if fn, ferr := syscall.GetProcAddress(loader, "CreateCoreWebView2EnvironmentWithOptions"); ferr == nil {
			hr, _, _ := purego.SyscallN(fn, 0, arg, 0, handler)
			return hresult("CreateCoreWebView2EnvironmentWithOptions", hr)
		}
	}
	dll, err := runtimeDLL()
	if err != nil {
		return err
	}
	mod, err := syscall.LoadLibrary(dll)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWebviewUnavailable, err)
	}
	fn, err := syscall.GetProcAddress(mod, "CreateWebViewEnvironmentWithOptionsInternal")
	if err != nil {
		return fmt.Errorf("%w: this WebView2 runtime has no environment constructor", ErrWebviewUnavailable)
	}
	// HRESULT(bool checkRunningInstance, int runtimeType, PCWSTR userDataFolder,
	// IUnknown *options, handler)
	hr, _, _ := purego.SyscallN(fn, 1, 0, arg, 0, handler)
	return hresult("CreateWebViewEnvironmentWithOptionsInternal", hr)
}

func hresult(what string, hr uintptr) error {
	if int32(hr) >= 0 {
		return nil
	}
	return fmt.Errorf("web view: %s failed (HRESULT 0x%08X)", what, uint32(hr))
}

func utf16Ptr(s string) []uint16 {
	u := utf16.Encode([]rune(s))
	return append(u, 0)
}

func wideString(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	var u []uint16
	for i := 0; ; i++ {
		c := *(*uint16)(unsafe.Add(p, i*2))
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

// --- The view ---------------------------------------------------------------

type winWebview struct {
	hwnd    uintptr
	env     *envObj
	ctrl    *controllerObj
	core    *coreObj
	message func(string)
	scale   float32

	ready   bool
	initHR  int32
	x, y    float64
	w, h    float64
	visible bool
	closed  bool
}

var putZoom func(unsafe.Pointer, float64) uintptr

// userDataDir is one WebView2 profile for the process, not one per view: the
// profile is megabytes, and a hot reload would leave a copy behind each time.
var (
	userDataOnce sync.Once
	userDataPath string
	userDataErr  error
)

func userDataDir() (string, error) {
	userDataOnce.Do(func() {
		userDataPath, userDataErr = os.MkdirTemp("", "goi-webview-")
	})
	return userDataPath, userDataErr
}

// NewWebview creates the environment and a controller parented to the SDL
// window. WebView2 builds its object graph asynchronously, so this pumps the
// thread's Win32 queue until the controller arrives.
func NewWebview(parent *Window, onMessage func(string), _ func()) (Webview, error) {
	if err := InitWebview(); err != nil {
		return nil, err
	}
	_, hwnd, kind, err := parent.SurfaceHandles()
	if err != nil {
		return nil, err
	}
	if kind != SurfaceWin32 || hwnd == 0 {
		return nil, ErrWebviewUnsupported
	}
	w := &winWebview{
		hwnd:    hwnd,
		message: onMessage,
		scale:   parent.Scale(),
		visible: true,
	}
	if w.scale <= 0 {
		w.scale = 1
	}
	if err := w.create(); err != nil {
		w.Destroy()
		return nil, err
	}
	return w, nil
}

func (w *winWebview) create() error {
	userData, err := userDataDir()
	if err != nil {
		return err
	}
	envHandler := newHandler(func(hr uintptr, env unsafe.Pointer) uintptr {
		if int32(hr) < 0 || env == nil {
			w.initHR, w.ready = int32(hr), true
			return 0
		}
		w.env = (*envObj)(env)
		ctrlHandler := newHandler(func(hr uintptr, ctrl unsafe.Pointer) uintptr {
			if int32(hr) < 0 || ctrl == nil {
				w.initHR, w.ready = int32(hr), true
				return 0
			}
			w.ctrl = (*controllerObj)(ctrl)
			w.ready = true
			return 0
		})
		if hr := w.env.createController(w.hwnd, ctrlHandler); hr < 0 {
			w.initHR, w.ready = hr, true
		}
		return 0
	})
	if err := createEnvironment(userData, envHandler); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for !w.ready {
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: WebView2 did not start", ErrWebviewUnavailable)
		}
		pumpMessage()
	}
	if w.ctrl == nil || w.initHR < 0 {
		return fmt.Errorf("web view: WebView2 controller failed (HRESULT 0x%08X)", uint32(w.initHR))
	}
	if hr := w.ctrl.getCore(&w.core); hr < 0 || w.core == nil {
		return fmt.Errorf("web view: WebView2 core failed (HRESULT 0x%08X)", uint32(hr))
	}
	w.core.addRef()
	if w.message != nil {
		w.core.addWebMessageReceived(newHandler(func(_ uintptr, args unsafe.Pointer) uintptr {
			if args == nil {
				return 0
			}
			var text unsafe.Pointer
			a := (*messageArgsObj)(args)
			if a.tryMessageAsString(&text) >= 0 && text != nil {
				msg := wideString(text)
				coTaskMemFree(text)
				w.message(msg)
			}
			return 0
		}))
	}
	w.SetBounds(w.x, w.y, w.w, w.h)
	return nil
}

// --- Interface methods ------------------------------------------------------

func (o *envObj) createController(hwnd, handler uintptr) int32 {
	return int32(o.vtbl.CreateCoreWebView2Controller.call(unsafe.Pointer(o), hwnd, handler))
}

func (o *controllerObj) putVisible(on int32) {
	o.vtbl.PutIsVisible.call(unsafe.Pointer(o), uintptr(on))
}

func (o *controllerObj) putBounds(r *rect) {
	o.vtbl.PutBounds.call(unsafe.Pointer(o), uintptr(unsafe.Pointer(r)))
}

func (o *controllerObj) putZoom(z float64) {
	if putZoom == nil {
		purego.RegisterFunc(&putZoom, uintptr(o.vtbl.PutZoomFactor))
	}
	putZoom(unsafe.Pointer(o), z)
}

func (o *controllerObj) moveFocus() {
	o.vtbl.MoveFocus.call(unsafe.Pointer(o), 1) // programmatic
}

func (o *controllerObj) getCore(out **coreObj) int32 {
	var p unsafe.Pointer
	hr := o.vtbl.GetCoreWebView2.call(unsafe.Pointer(o), uintptr(unsafe.Pointer(&p)))
	*out = (*coreObj)(p)
	return int32(hr)
}

func (o *controllerObj) close() {
	o.vtbl.Close.call(unsafe.Pointer(o))
}

func (o *coreObj) addRef()    { o.vtbl.AddRef.call(unsafe.Pointer(o)) }
func (o *coreObj) release()   { o.vtbl.Release.call(unsafe.Pointer(o)) }
func (o *coreObj) reload()    { o.vtbl.Reload.call(unsafe.Pointer(o)) }
func (o *coreObj) goBack()    { o.vtbl.GoBack.call(unsafe.Pointer(o)) }
func (o *coreObj) goForward() { o.vtbl.GoForward.call(unsafe.Pointer(o)) }

func (o *coreObj) navigate(url string) {
	u := utf16Ptr(url)
	o.vtbl.Navigate.call(unsafe.Pointer(o), uintptr(unsafe.Pointer(&u[0])))
	runtime.KeepAlive(u)
}

func (o *coreObj) navigateToString(html string) {
	u := utf16Ptr(html)
	o.vtbl.NavigateToString.call(unsafe.Pointer(o), uintptr(unsafe.Pointer(&u[0])))
	runtime.KeepAlive(u)
}

func (o *coreObj) eval(js string) {
	u := utf16Ptr(js)
	o.vtbl.ExecuteScript.call(unsafe.Pointer(o), uintptr(unsafe.Pointer(&u[0])), 0)
	runtime.KeepAlive(u)
}

func (o *coreObj) can(forward bool) bool {
	var ok int32
	vtbl := o.vtbl.GetCanGoBack
	if forward {
		vtbl = o.vtbl.GetCanGoForward
	}
	vtbl.call(unsafe.Pointer(o), uintptr(unsafe.Pointer(&ok)))
	return ok != 0
}

func (o *coreObj) addWebMessageReceived(handler uintptr) {
	var token uint64
	o.vtbl.AddWebMessageReceived.call(unsafe.Pointer(o), handler, uintptr(unsafe.Pointer(&token)))
}

func (o *coreObj) settings() *settingsObj {
	var p unsafe.Pointer
	o.vtbl.GetSettings.call(unsafe.Pointer(o), uintptr(unsafe.Pointer(&p)))
	return (*settingsObj)(p)
}

// iidSettings2 is the interface version of the settings that carries the user
// agent; the base settings object does not have those slots.
var iidSettings2 = guid{
	0xEE9A0F68, 0xF46C, 0x4E32,
	[8]byte{0xAC, 0x23, 0xEF, 0x8C, 0xAC, 0x22, 0x4D, 0x2A},
}

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func (o *settingsObj) putDevtools(on bool) {
	var v int32
	if on {
		v = 1
	}
	o.vtbl.PutAreDevToolsEnabled.call(unsafe.Pointer(o), uintptr(v))
}

func (o *settingsObj) putUserAgent(ua string) {
	var p unsafe.Pointer
	hr := o.vtbl.QueryInterface.call(unsafe.Pointer(o), uintptr(unsafe.Pointer(&iidSettings2)), uintptr(unsafe.Pointer(&p)))
	if int32(hr) < 0 || p == nil {
		return // the runtime predates Settings2
	}
	s2 := (*settingsObj)(p)
	u := utf16Ptr(ua)
	s2.vtbl.PutUserAgent.call(unsafe.Pointer(s2), uintptr(unsafe.Pointer(&u[0])))
	runtime.KeepAlive(u)
	s2.vtbl.Release.call(unsafe.Pointer(s2))
}

func (o *messageArgsObj) tryMessageAsString(out *unsafe.Pointer) int32 {
	return int32(o.vtbl.TryGetWebMessageAsString.call(unsafe.Pointer(o), uintptr(unsafe.Pointer(out))))
}

// --- Webview interface ------------------------------------------------------

func (w *winWebview) SetBounds(x, y, width, height float64) {
	w.x, w.y, w.w, w.h = x, y, width, height
	if w.ctrl == nil {
		return
	}
	if !w.visible || width <= 0 || height <= 0 {
		w.ctrl.putVisible(0)
		return
	}
	w.ctrl.putVisible(1)
	r := rect{
		Left:   int32(x * float64(w.scale)),
		Top:    int32(y * float64(w.scale)),
		Right:  int32((x + width) * float64(w.scale)),
		Bottom: int32((y + height) * float64(w.scale)),
	}
	w.ctrl.putBounds(&r)
}

func (w *winWebview) SetVisible(v bool) {
	w.visible = v
	w.SetBounds(w.x, w.y, w.w, w.h)
}

func (w *winWebview) Load(src string) {
	if w.core == nil {
		return
	}
	if src == "" {
		src = "about:blank"
	}
	w.core.navigate(src)
}

func (w *winWebview) LoadHTML(html, base string) {
	if w.core == nil {
		return
	}
	// NavigateToString has no base URL, so a relative link needs a <base> tag.
	// shortcut: markup that puts its own <base> first wins, as it should.
	if base != "" {
		html = `<base href="` + base + `">` + html
	}
	w.core.navigateToString(html)
}

func (w *winWebview) Reload() {
	if w.core != nil {
		w.core.reload()
	}
}

func (w *winWebview) GoBack() bool {
	if w.core == nil || !w.core.can(false) {
		return false
	}
	w.core.goBack()
	return true
}

func (w *winWebview) GoForward() bool {
	if w.core == nil || !w.core.can(true) {
		return false
	}
	w.core.goForward()
	return true
}

func (w *winWebview) Eval(js string) {
	if w.core != nil {
		w.core.eval(js)
	}
}

func (w *winWebview) SetZoom(z float64) {
	if w.ctrl != nil && z > 0 {
		w.ctrl.putZoom(z)
	}
}

func (w *winWebview) SetUserAgent(ua string) {
	if w.core == nil {
		return
	}
	if s := w.core.settings(); s != nil {
		s.putUserAgent(ua)
	}
}

func (w *winWebview) SetDevtools(on bool) {
	if w.core == nil {
		return
	}
	if s := w.core.settings(); s != nil {
		s.putDevtools(on)
	}
}

// ForwardKey and ForwardText are not needed: the view is a child window with
// its own keyboard focus.
func (w *winWebview) ForwardKey(Key, Mods) {}

func (w *winWebview) ForwardText(rune) {}

// ForwardPointer is not needed for the same reason: WebView2 receives the
// pointer through the child window.
func (w *winWebview) ForwardPointer(PointerKind, float64, float64, int, float64, float64, bool) {}

func (w *winWebview) Focus() {
	if w.ctrl != nil {
		w.ctrl.moveFocus()
	}
}

func (w *winWebview) Destroy() {
	if w.closed {
		return
	}
	w.closed = true
	if w.ctrl != nil {
		w.ctrl.close()
	}
	if w.core != nil {
		w.core.release()
	}
	w.ctrl, w.core, w.env = nil, nil, nil
}

// SetWebviewFocus is a no-op: this platform lets the engine take focus itself.
func SetWebviewFocus(bool) {}
