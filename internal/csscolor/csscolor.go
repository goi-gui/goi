// Package csscolor parses the colour syntax CSS uses: hex, the 148 named
// colours, rgb()/rgba() and hsl()/hsla(), plus linear and radial gradients.
// It is shared by the checker and the tree so both accept the same words.
package csscolor

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Parse reads one CSS colour and returns 0xRRGGBBAA. It accepts every spelling
// a stylesheet allows: `#3b82f6`, `#38f`, `red`, `rgba(42, 123, 155, 1)`,
// `rgb(42 123 155 / 50%)`, `hsl(200 40% 50%)` and `transparent`.
func Parse(s string) (uint32, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if s[0] == '#' {
		return parseHex(s)
	}
	lower := strings.ToLower(s)
	if c, ok := names[lower]; ok {
		return c, true
	}
	switch {
	case strings.HasPrefix(lower, "rgb("), strings.HasPrefix(lower, "rgba("):
		return parseFunc(s, func(f []float64, pct []bool) (uint32, bool) {
			if len(f) < 3 {
				return 0, false
			}
			for i := 0; i < 3; i++ {
				// `rgb(100% 0% 0%)` is white-red, not 100/255.
				if pct[i] {
					f[i] = f[i] * 255 / 100
				}
			}
			a := 1.0
			if len(f) > 3 {
				a = f[3]
			}
			return pack(f[0], f[1], f[2], a*255), true
		})
	case strings.HasPrefix(lower, "hsl("), strings.HasPrefix(lower, "hsla("):
		return parseFunc(s, func(f []float64, _ []bool) (uint32, bool) {
			if len(f) < 3 {
				return 0, false
			}
			a := 1.0
			if len(f) > 3 {
				a = f[3]
			}
			r, g, b := hslToRGB(f[0], f[1]/100, f[2]/100)
			return pack(r*255, g*255, b*255, a*255), true
		})
	}
	return 0, false
}

// IsName reports whether s is a CSS colour keyword.
func IsName(s string) bool {
	_, ok := names[strings.ToLower(strings.TrimSpace(s))]
	return ok
}

func parseHex(s string) (uint32, bool) {
	h := s[1:]
	if len(h) != 3 && len(h) != 4 && len(h) != 6 && len(h) != 8 {
		return 0, false
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, false
	}
	switch len(h) {
	case 3:
		r, g, b := (n>>8)&0xF, (n>>4)&0xF, n&0xF
		return uint32(r*17)<<24 | uint32(g*17)<<16 | uint32(b*17)<<8 | 0xFF, true
	case 4:
		r, g, b, a := (n>>12)&0xF, (n>>8)&0xF, (n>>4)&0xF, n&0xF
		return uint32(r*17)<<24 | uint32(g*17)<<16 | uint32(b*17)<<8 | uint32(a*17), true
	case 6:
		return uint32(n)<<8 | 0xFF, true
	default:
		return uint32(n), true
	}
}

// parseFunc reads the arguments of rgb()/hsl(), in either the legacy
// comma-separated form or the modern space-separated one with a `/ alpha`.
func parseFunc(s string, build func([]float64, []bool) (uint32, bool)) (uint32, bool) {
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return 0, false
	}
	inner := s[open+1 : len(s)-1]
	// `none` is a valid modern component meaning zero.
	inner = strings.ReplaceAll(strings.ToLower(inner), "none", "0")
	// A slash separates the alpha channel in the modern syntax.
	inner = strings.ReplaceAll(inner, "/", " ")
	fields := strings.FieldsFunc(inner, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	nums := make([]float64, 0, len(fields))
	pct := make([]bool, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		// An angle unit is only meaningful for hue, which is degrees anyway.
		f = strings.TrimSuffix(f, "deg")
		percent := strings.HasSuffix(f, "%")
		f = strings.TrimSuffix(f, "%")
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return 0, false
		}
		if percent && len(nums) == 3 {
			// A percentage alpha is 0..1; a channel percentage is scaled by the
			// caller, which knows whether the values are RGB or HSL.
			v /= 100
		}
		nums = append(nums, v)
		pct = append(pct, percent)
	}
	return build(nums, pct)
}

func pack(r, g, b, a float64) uint32 {
	return uint32(clamp8(r))<<24 | uint32(clamp8(g))<<16 | uint32(clamp8(b))<<8 | uint32(clamp8(a))
}

func clamp8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// hslToRGB converts HSL, with h in degrees and s, l in 0..1, to RGB in 0..1.
func hslToRGB(h, s, l float64) (float64, float64, float64) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	h /= 360
	if s <= 0 {
		return l, l, l
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	return hue(p, q, h+1.0/3), hue(p, q, h), hue(p, q, h-1.0/3)
}

func hue(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6:
		return p + (q-p)*6*t
	case t < 0.5:
		return q
	case t < 2.0/3:
		return p + (q-p)*(2.0/3-t)*6
	}
	return p
}

// Format writes 0xRRGGBBAA as the #rrggbb literal a design can read back.
func Format(c uint32) string {
	return fmt.Sprintf("#%02x%02x%02x", (c>>24)&0xFF, (c>>16)&0xFF, (c>>8)&0xFF)
}
