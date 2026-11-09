package ui

import (
	"strings"

	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
)

// SetCustomColors defines several design-side colour names at once. It is
// SetCustomColor for a map, so the same rules apply: a name the Material scheme
// already owns is reserved and rejected.
//
//	win.SetCustomColors(map[string]uint32{
//		"myBrand":   0x411F89FF,
//		"otherColor": 0x2C165CFF,
//	})
//
// A design reads them like a role:
//
//	Rectangle { color: myBrand }
func (w *Window) SetCustomColors(colors map[string]uint32) {
	for name, c := range colors {
		w.SetCustomColor(name, c)
	}
}

// SetCustomColor defines or updates one colour a design can name, so a `.goi`
// file can use `color: name` without the framework knowing the name first.
//
// Calling it again with the same name updates the value and repaints whatever
// uses it, which is how a theme switch or a runtime accent colour reaches the
// design. The name must be a `.goi` identifier and must not be a Material role
// (`primary`, `onSurface`, ...): those belong to the scheme, and the call logs an
// error and does nothing. A name the design uses but nobody defines logs a
// warning once and falls back to the scheme's onSurface colour.
//
// It may be called from any goroutine; the colour resolves on the next frame.
func (w *Window) SetCustomColor(name string, color uint32) {
	if w == nil {
		return
	}
	if !validColorName(name) {
		logg().Error(logCtx, "ui: invalid custom colour name",
			"name", name, "reason", "not a .goi identifier")
		return
	}
	if _, reserved := (m3.Colors{}).Role(name); reserved {
		logg().Error(logCtx, "ui: custom colour name is reserved",
			"name", name, "reason", "the Material scheme already has this role")
		return
	}
	w.colorMu.Lock()
	if w.customColors == nil {
		w.customColors = map[string]uint32{}
	}
	w.customColors[name] = color
	delete(w.missingColors, name)
	w.colorMu.Unlock()

	// The frame loop re-resolves every colour, so a wake is enough; without it
	// an idle window would keep the old colour until something else moved.
	if w.app != nil {
		w.app.markDirty(tree.DirtyPaint)
	}
}

// customColor returns the value defined for name.
func (w *Window) customColor(name string) (uint32, bool) {
	w.colorMu.RLock()
	c, ok := w.customColors[name]
	w.colorMu.RUnlock()
	return c, ok
}

// noteMissingColor records name as unresolved and reports whether this is the
// first time, so a design that uses an undefined colour warns once instead of
// once per node per frame.
func (w *Window) noteMissingColor(name string) bool {
	w.colorMu.Lock()
	defer w.colorMu.Unlock()
	if w.missingColors[name] {
		return false
	}
	if w.missingColors == nil {
		w.missingColors = map[string]bool{}
	}
	w.missingColors[name] = true
	return true
}

// resolveColors runs the role and custom-colour lookup for the current frame.
// A missing custom colour falls back to the scheme's onSurface so the element
// still draws a colour that belongs to the theme.
func (a *App) resolveColors() bool {
	if a.win == nil || a.win.tree == nil {
		return false
	}
	w := a.win
	theme := themeFor(w)
	// DefineTheme runs after LoadWindow — usually inside Bind — so a name is only
	// a typo once the first frame is drawing. Warning at load would fire before
	// the app had a chance to register its scheme.
	if name := w.tree.Root.Theme.Get(); name != "" && name != "light" && name != "dark" {
		if _, ok := a.themes[name]; !ok && w.noteMissingTheme(name) {
			logg().Warn(logCtx, "ui: unknown theme, using light", "name", name,
				"file", a.path, "hint", "define it with ui.Window.DefineTheme")
		}
	}
	lookup := func(name string) (uint32, bool) {
		// A reference carries its namespace, so `theme.primary` never falls back
		// to a custom name of the same spelling and `customColor.primary` never
		// silently becomes the scheme's role.
		switch {
		case strings.HasPrefix(name, "theme:"):
			role := strings.TrimPrefix(name, "theme:")
			if c, ok := theme.Colors.Role(role); ok {
				return c, true
			}
			if w.noteMissingColor(name) {
				logg().Warn(logCtx, "ui: unknown theme colour, using onSurface",
					"name", role, "file", a.path, "hint", syntax.Suggest(role, m3.ColorRoles()))
			}
			return theme.Colors.OnSurface, true
		case strings.HasPrefix(name, "custom:"):
			name = strings.TrimPrefix(name, "custom:")
			if c, ok := w.customColor(name); ok {
				return c, true
			}
			if w.noteMissingColor(name) {
				logg().Warn(logCtx, "ui: undefined custom colour, using onSurface",
					"name", name, "file", a.path, "hint", "define it with ui.SetCustomColor")
			}
			return theme.Colors.OnSurface, true
		}
		if c, ok := theme.Colors.Role(name); ok {
			return c, true
		}
		if c, ok := w.customColor(name); ok {
			return c, true
		}
		if w.noteMissingColor(name) {
			hint := syntax.Suggest(name, m3.ColorRoles())
			if hint == "" {
				hint = "define it with ui.SetCustomColor"
			}
			logg().Warn(logCtx, "ui: unknown colour name, using onSurface",
				"name", name, "file", a.path, "hint", hint)
		}
		return theme.Colors.OnSurface, true
	}
	return w.tree.ResolveColors(lookup)
}

// validColorName reports whether name can be written as a bare `.goi` value:
// a letter or underscore, then letters, digits or underscores.
func validColorName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return !strings.HasPrefix(name, "$")
}
