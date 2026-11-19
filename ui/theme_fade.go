package ui

import (
	"reflect"
	"time"

	"github.com/goi-gui/goi/internal/m3"
)

// displayTheme resolves the window's scheme and blends from the previous one
// while `crossfadeTheme` runs. Role colours are resolved with the blend and the
// painter gets it as its scheme, so the whole window moves together.
func (a *App) displayTheme(w *Window) *m3.Theme {
	target := themeFor(w)
	name := ""
	if w != nil && w.tree != nil && w.tree.Root != nil {
		name = w.tree.Root.Theme.Get()
	}
	if a.themeTo == nil {
		a.themeName, a.themeFrom, a.themeTo, a.themeDisplay = name, target, target, target
		return target
	}
	if name != a.themeName {
		dur := time.Duration(0)
		if w != nil && w.tree != nil && w.tree.Root != nil {
			dur = w.tree.Root.CrossfadeTheme.Get()
		}
		from := a.themeDisplay
		if from == nil {
			from = a.themeTo
		}
		a.themeName, a.themeFrom, a.themeTo = name, from, target
		if dur > 0 {
			a.themeStart, a.themeDur, a.themeFading = time.Now(), dur, true
			a.themeDisplay = blendTheme(from, target, 0)
			a.needsFrame = true
			return a.themeDisplay
		}
		a.themeFading, a.themeDisplay = false, target
		return target
	}
	if !a.themeFading {
		return a.themeDisplay
	}
	p := float64(time.Since(a.themeStart)) / float64(a.themeDur)
	if p >= 1 {
		a.themeFading, a.themeDisplay = false, a.themeTo
		a.needsFrame = true
		return a.themeTo
	}
	a.themeDisplay = blendTheme(a.themeFrom, a.themeTo, p)
	// A design with no role colours still needs frames while the scheme fades.
	a.needsFrame = true
	return a.themeDisplay
}

// blendTheme mixes every colour role between two schemes. The type scale and
// the mood follow the target, so text metrics and conditionals do not wobble
// during the fade.
func blendTheme(from, to *m3.Theme, p float64) *m3.Theme {
	if from == nil || to == nil || p <= 0 {
		return from
	}
	if p >= 1 {
		return to
	}
	out := *to
	va, vb := reflect.ValueOf(from.Colors), reflect.ValueOf(to.Colors)
	vo := reflect.ValueOf(&out.Colors).Elem()
	for i := 0; i < va.NumField(); i++ {
		x, y := uint32(va.Field(i).Uint()), uint32(vb.Field(i).Uint())
		vo.Field(i).SetUint(uint64(blendColor(x, y, p)))
	}
	return &out
}

// blendColor mixes two 0xRRGGBBAA roles channel by channel. The alpha is kept
// exact at the ends, so a fully transparent role stays transparent.
func blendColor(a, b uint32, p float64) uint32 {
	mix := func(x, y uint32) uint32 {
		return uint32(float64(x) + (float64(y)-float64(x))*p + 0.5)
	}
	return mix((a>>24)&0xFF, (b>>24)&0xFF)<<24 |
		mix((a>>16)&0xFF, (b>>16)&0xFF)<<16 |
		mix((a>>8)&0xFF, (b>>8)&0xFF)<<8 |
		mix(a&0xFF, b&0xFF)
}
