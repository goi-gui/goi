//go:build linux && !js

package platform

import (
	"sync/atomic"

	"github.com/godbus/dbus/v5"
)

// portalThemeValue is the freedesktop appearance preference as last read or
// signalled: 0 unknown, 1 light, 2 dark.
var portalThemeValue atomic.Int32

// portalTheme is the value the portal watcher last saw.
func portalTheme() Theme {
	switch portalThemeValue.Load() {
	case 1:
		return ThemeLight
	case 2:
		return ThemeDark
	}
	return ThemeUnknown
}

// startThemeWatcher follows the XDG desktop portal's color-scheme setting.
// SDL reports the same preference on some desktops, but its system-theme event
// is not reliable when the window is unfocused, and a settings app is usually
// focused when the user switches. The portal signal is global, so the tree can
// follow the desktop even while another window has focus.
func startThemeWatcher() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	if v, ok := readPortalTheme(obj); ok {
		setPortalTheme(v)
	}
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/portal/desktop"),
		dbus.WithMatchInterface("org.freedesktop.portal.Settings"),
		dbus.WithMatchMember("SettingChanged"),
	); err != nil {
		_ = conn.Close()
		return
	}
	ch := make(chan *dbus.Signal, 16)
	conn.Signal(ch)
	portalConn = conn
	go func() {
		for {
			select {
			case <-portalStop:
				return
			case sig, ok := <-ch:
				if !ok {
					return
				}
				if sig.Name != "org.freedesktop.portal.Settings.SettingChanged" || len(sig.Body) < 3 {
					continue
				}
				ns, _ := sig.Body[0].(string)
				key, _ := sig.Body[1].(string)
				if ns != "org.freedesktop.appearance" || key != "color-scheme" {
					continue
				}
				if v, ok := portalUint(sig.Body[2]); ok {
					setPortalTheme(v)
					PostEmptyEvent()
				}
			}
		}
	}()
}

// portalConn and portalStop let Terminate close the watcher; the app has one
// session bus connection.
var (
	portalConn *dbus.Conn
	portalStop = make(chan struct{})
)

func stopThemeWatcher() {
	select {
	case <-portalStop:
	default:
		close(portalStop)
	}
	if portalConn != nil {
		_ = portalConn.Close()
		portalConn = nil
	}
}

func readPortalTheme(obj dbus.BusObject) (uint32, bool) {
	var v dbus.Variant
	if err := obj.Call("org.freedesktop.portal.Settings.Read", 0,
		"org.freedesktop.appearance", "color-scheme").Store(&v); err != nil {
		return 0, false
	}
	return portalUint(v)
}

func portalUint(v any) (uint32, bool) {
	switch x := v.(type) {
	case dbus.Variant:
		return portalUint(x.Value())
	case uint32:
		return x, true
	case int32:
		return uint32(x), true
	}
	return 0, false
}

func setPortalTheme(v uint32) {
	switch v {
	case 1:
		portalThemeValue.Store(2) // prefer-dark
	case 2:
		portalThemeValue.Store(1) // prefer-light
	default:
		portalThemeValue.Store(0)
	}
}
