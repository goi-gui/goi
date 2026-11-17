package mermaid

import (
	"fmt"

	"github.com/goi-gui/goi/internal/m3"
)

// ThemeKey names the palette a rendered diagram used, so a cache can tell a
// scheme change from a still frame without keeping the whole theme alive.
func ThemeKey(theme *m3.Theme) string {
	if theme == nil {
		return ""
	}
	c := theme.Colors
	return fmt.Sprintf("%08x/%08x/%08x/%08x/%08x/%08x/%08x/%08x",
		c.Surface, c.OnSurface, c.OnSurfaceVariant, c.Outline,
		c.SurfaceContainerHigh, c.SurfaceContainerHighest, c.OutlineVariant, c.Primary)
}
