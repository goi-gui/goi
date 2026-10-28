package tree

import (
	"strconv"
	"strings"
)

// ParseHexColor parses #rgb, #rrggbb or #rrggbbaa into 0xRRGGBBAA.
func ParseHexColor(s string) (uint32, bool) {
	if !strings.HasPrefix(s, "#") {
		return 0, false
	}
	h := s[1:]
	if len(h) != 3 && len(h) != 6 && len(h) != 8 {
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
	case 6:
		return uint32(n)<<8 | 0xFF, true
	default:
		return uint32(n), true
	}
}
