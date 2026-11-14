package tree

import (
	"fmt"
	"math"
	"strings"

	"github.com/goi-gui/goi/internal/csscolor"
)

// HSVColor packs an HSV colour into opaque 0xRRGGBBAA. Hue is in degrees, with
// 0 and 360 both red.
func HSVColor(h, s, v float64) uint32 {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	s, v = clamp01(s), clamp01(v)
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return uint32((r+m)*255+0.5)<<24 | uint32((g+m)*255+0.5)<<16 | uint32((b+m)*255+0.5)<<8 | 0xFF
}

// hsvHex formats an HSV colour the way the picker shows it.
func hsvHex(h, s, v float64) string {
	c := HSVColor(h, s, v)
	return fmt.Sprintf("#%02x%02x%02x", c>>24, (c>>16)&0xFF, (c>>8)&0xFF)
}

// hexHSV reads any CSS colour into HSV. Alpha is ignored.
func hexHSV(text string) (h, s, v float64, ok bool) {
	c, ok := csscolor.Parse(strings.TrimSpace(text))
	if !ok {
		return 0, 0, 0, false
	}
	h, s, v = rgbToHSV(c>>24, (c>>16)&0xFF, (c>>8)&0xFF)
	return h, s, v, true
}

// rgbToHSV converts 8-bit sRGB to hue, saturation and value.
func rgbToHSV(r, g, b uint32) (h, s, v float64) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	hi := math.Max(rf, math.Max(gf, bf))
	lo := math.Min(rf, math.Min(gf, bf))
	v = hi
	if hi == 0 {
		return 0, 0, 0
	}
	s = (hi - lo) / hi
	if hi == lo {
		return 0, s, v
	}
	switch hi {
	case rf:
		h = (gf - bf) / (hi - lo)
		if gf < bf {
			h += 6
		}
	case gf:
		h = (bf-rf)/(hi-lo) + 2
	default:
		h = (rf-gf)/(hi-lo) + 4
	}
	return h * 60, s, v
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// isHexRun reports whether s is a bare hex colour: 3, 4, 6 or 8 hex digits
// with no leading #.
func isHexRun(s string) bool {
	switch len(s) {
	case 3, 4, 6, 8:
	default:
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}
