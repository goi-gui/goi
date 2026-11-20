package syntax

import (
	"path"
	"strings"
)

// DeriveAlias names an unnamed import from its path: "./Sidebar.goi" is
// Sidebar, so the design writes Sidebar { … }. A base name that is not an
// identifier is folded into one ("my-card.goi" becomes MyCard); a purely
// numeric one gets a File prefix.
func DeriveAlias(target string) (string, bool) {
	base := strings.TrimSuffix(path.Base(target), path.Ext(target))
	if base == "" {
		return "", false
	}
	if isAlias(base) {
		return base, true
	}
	var b strings.Builder
	upper := true
	for _, r := range base {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			if upper && r >= 'a' && r <= 'z' {
				r -= 'a' - 'A'
			}
			b.WriteRune(r)
			upper = false
			continue
		}
		upper = true
	}
	name := b.String()
	if name == "" {
		return "", false
	}
	if name[0] >= '0' && name[0] <= '9' {
		name = "File" + name
	}
	return name, true
}

// isAlias reports whether s can be an element name.
func isAlias(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}
