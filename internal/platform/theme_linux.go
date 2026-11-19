//go:build linux && !js

package platform

import (
	"strings"
	"sync/atomic"
	"time"

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
	refreshPortalTheme(obj)
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
		// A theme manager may only change the GTK theme and not emit a
		// color-scheme signal; poll as a backstop so the window still follows.
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-portalStop:
				return
			case <-ticker.C:
				before := portalThemeValue.Load()
				refreshPortalTheme(obj)
				if portalThemeValue.Load() != before {
					PostEmptyEvent()
				}
			case sig, ok := <-ch:
				if !ok {
					return
				}
				if sig.Name != "org.freedesktop.portal.Settings.SettingChanged" || len(sig.Body) < 3 {
					continue
				}
				ns, _ := sig.Body[0].(string)
				key, _ := sig.Body[1].(string)
				if (ns == "org.freedesktop.appearance" && key == "color-scheme") ||
					(ns == "org.gnome.desktop.interface" && key == "gtk-theme") {
					// A desktop may express light mode as color-scheme 0 while
					// its GTK theme still says which side it is on.
					refreshPortalTheme(obj)
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

func refreshPortalTheme(obj dbus.BusObject) {
	if v, ok := readPortalTheme(obj); ok && (v == 1 || v == 2) {
		setPortalTheme(v)
		return
	}
	// color-scheme 0 is "no preference", which some desktops use for light.
	// The GTK theme name still carries the side, so use it as the tie-breaker.
	if name, ok := readGTKTheme(obj); ok {
		setPortalGTKTheme(name)
		return
	}
	portalThemeValue.Store(0)
}

// setPortalGTKTheme is the fallback when color-scheme has no preference:
// light unless the theme name says dark.
func setPortalGTKTheme(name string) {
	if strings.Contains(strings.ToLower(name), "dark") {
		setPortalTheme(1)
		return
	}
	setPortalTheme(2)
}

func readPortalTheme(obj dbus.BusObject) (uint32, bool) {
	var v dbus.Variant
	if err := obj.Call("org.freedesktop.portal.Settings.Read", 0,
		"org.freedesktop.appearance", "color-scheme").Store(&v); err != nil {
		return 0, false
	}
	return portalUint(v)
}

func readGTKTheme(obj dbus.BusObject) (string, bool) {
	var v dbus.Variant
	if err := obj.Call("org.freedesktop.portal.Settings.Read", 0,
		"org.gnome.desktop.interface", "gtk-theme").Store(&v); err != nil {
		return "", false
	}
	switch x := v.Value().(type) {
	case string:
		return x, x != ""
	case dbus.Variant:
		if s, ok := x.Value().(string); ok {
			return s, s != ""
		}
	}
	return "", false
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
