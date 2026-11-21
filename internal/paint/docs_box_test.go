package paint

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/goi-gui/goi/internal/layout"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/tree"
)

// TestEveryComponentTakesTheBoxProps is the full audit: every documented
// component is built, given a background, a border and a radius, painted, and
// must show them on its own box. It reads the docs checkout next to the repo, so
// CI without it skips.
func TestEveryComponentTakesTheBoxProps(t *testing.T) {
	dir := "../../../docs/src/content/docs/components"
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			t.Skip("docs site is not checked out next to the repo")
		}
		t.Fatal(err)
	}
	pages, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile("(?s)```goi\n(.*?)```")
	heading := regexp.MustCompile(`(?m)^title:\s*"?([^"\n]+)"?$`)
	for _, page := range pages {
		body, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		m := block.FindSubmatch(body)
		h := heading.FindSubmatch(body)
		if m == nil || h == nil {
			continue
		}
		title := strings.TrimSpace(string(h[1]))
		src := strings.TrimSpace(string(m[1]))
		// A fragment needs a window; a page that already has one keeps it.
		first := firstLine(src)
		if first != "Window" {
			src = "Window { width: 500 height: 500 " + src + " }"
		}
		tr, diag := tree.BuildSource([]byte(src), nil)
		if !diag.Empty() {
			t.Errorf("%s: %v", filepath.Base(page), diag)
			continue
		}
		kind := title
		if base := strings.TrimSuffix(filepath.Base(page), ".md"); base == "geom" || base == "path" || base == "point" {
			kind = "Map"
		}
		n := findKind(tr.Root, kind)
		if n == nil {
			t.Errorf("%s: no %s", filepath.Base(page), kind)
			continue
		}
		r := absRect(n)
		if r.W < 2 || r.H < 2 {
			continue // a Positioned or a bare Tab has no box of its own
		}
		if !n.Base().SetPropLiteral("background", &syntax.Color{RGBA: 0x112233FF}) {
			t.Errorf("%s: background rejected", filepath.Base(page))
			continue
		}
		n.Base().SetPropLiteral("borderColor", &syntax.Color{RGBA: 0x445566FF})
		n.Base().SetPropLiteral("borderThickness", &syntax.Number{Val: 2})
		n.Base().SetPropLiteral("radius", &syntax.Number{Val: 6})
		layout.New(nil).Layout(tr.Root, 500, 500)
		r = absRect(n)
		l := &List{}
		l.Build(tr, 500, 500)
		var bg, border, radius bool
		for _, op := range l.Ops {
			if op.Kind != Fill || !rectEqual(op.Rect, r) {
				continue
			}
			if op.Color == 0x112233FF {
				bg = true
			}
			if op.BorderColor == 0x445566FF && op.BorderThickness == 2 {
				border = true
			}
			if op.Radius[0] > 0 {
				radius = true
			}
		}
		if !bg || !border || !radius {
			t.Errorf("%s (%s): background=%v border=%v radius=%v", filepath.Base(page), kind, bg, border, radius)
		}
	}
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if i := strings.IndexAny(line, " \t{"); i > 0 {
			line = line[:i]
		}
		return line
	}
	return ""
}

func findKind(n tree.Node, kind string) tree.Node {
	if n.Kind() == kind {
		return n
	}
	for _, k := range n.Base().Kids() {
		if got := findKind(k.Node(), kind); got != nil {
			return got
		}
	}
	return nil
}
