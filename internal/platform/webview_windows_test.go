//go:build windows && !js && (webview || all)

package platform

import (
	"testing"
	"unsafe"
)

// TestWebviewWindowsVtableSlots pins the COM slot of every method the backend
// calls. COM dispatches by offset, so a deleted "unused" slot silently turns
// the methods after it into calls through the wrong function pointer.
func TestWebviewWindowsVtableSlots(t *testing.T) {
	slot := func(name string, off, want uintptr) {
		t.Helper()
		if got := off / unsafe.Sizeof(uintptr(0)); got != want {
			t.Errorf("%s is slot %d, want %d", name, got, want)
		}
	}
	p := unsafe.Sizeof(uintptr(0))
	slot("ICoreWebView2Environment.CreateCoreWebView2Controller", unsafe.Offsetof(envVtbl{}.CreateCoreWebView2Controller), 3)

	slot("ICoreWebView2Controller.PutBounds", unsafe.Offsetof(controllerVtbl{}.PutBounds), 6)
	slot("ICoreWebView2Controller.PutZoomFactor", unsafe.Offsetof(controllerVtbl{}.PutZoomFactor), 8)
	slot("ICoreWebView2Controller.MoveFocus", unsafe.Offsetof(controllerVtbl{}.MoveFocus), 12)
	slot("ICoreWebView2Controller.Close", unsafe.Offsetof(controllerVtbl{}.Close), 24)
	slot("ICoreWebView2Controller.GetCoreWebView2", unsafe.Offsetof(controllerVtbl{}.GetCoreWebView2), 25)

	slot("ICoreWebView2.GetSettings", unsafe.Offsetof(coreVtbl{}.GetSettings), 3)
	slot("ICoreWebView2.Navigate", unsafe.Offsetof(coreVtbl{}.Navigate), 5)
	slot("ICoreWebView2.NavigateToString", unsafe.Offsetof(coreVtbl{}.NavigateToString), 6)
	slot("ICoreWebView2.ExecuteScript", unsafe.Offsetof(coreVtbl{}.ExecuteScript), 29)
	slot("ICoreWebView2.Reload", unsafe.Offsetof(coreVtbl{}.Reload), 31)
	slot("ICoreWebView2.AddWebMessageReceived", unsafe.Offsetof(coreVtbl{}.AddWebMessageReceived), 34)
	slot("ICoreWebView2.GetCanGoBack", unsafe.Offsetof(coreVtbl{}.GetCanGoBack), 38)
	slot("ICoreWebView2.GoForward", unsafe.Offsetof(coreVtbl{}.GoForward), 41)

	slot("ICoreWebView2Settings.PutAreDevToolsEnabled", unsafe.Offsetof(settingsVtbl{}.PutAreDevToolsEnabled), 12)
	slot("ICoreWebView2Settings2.PutUserAgent", unsafe.Offsetof(settingsVtbl{}.PutUserAgent), 22)

	slot("ICoreWebView2WebMessageReceivedEventArgs.TryGetWebMessageAsString",
		unsafe.Offsetof(messageArgsVtbl{}.TryGetWebMessageAsString), 5)
	slot("handler Invoke", unsafe.Offsetof(handlerVtbl{}.Invoke), 3)

	if got := unsafe.Sizeof(coreVtbl{}) / p; got != 62 {
		t.Errorf("ICoreWebView2 has %d slots, want 62", got)
	}
}
