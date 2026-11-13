//go:build !js

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goi-gui/goi/internal/platform"
)

// TestLocalPathIsNotAURL is the guard on the one decision that matters before a
// click opens anything: a link with a scheme is handed to the system's handler,
// never asked of the filesystem, while a path that exists is.
func TestLocalPathIsNotAURL(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"https://goi.dev/guide",
		"http://localhost:8080/a",
		"mailto:tech@goi.dev",
		"myapp://open?id=1",
		filepath.Join(dir, "missing.txt"),
	} {
		if got, ok := localPath(target); ok {
			t.Fatalf("%q resolved to the path %q", target, got)
		}
	}
	for _, target := range []string{file, dir, "file://" + file, "~"} {
		if _, ok := localPath(target); !ok {
			t.Fatalf("%q was not taken as a path", target)
		}
	}
	// A Windows drive is a path, not a scheme.
	if !driveRe.MatchString(`C:\Users\me`) {
		t.Fatal("a drive letter does not match driveRe")
	}
	if schemeRe.MatchString(`C:\Users\me`) && !driveRe.MatchString(`C:\Users\me`) {
		t.Fatal("a drive letter was read as a scheme")
	}
}

// TestOpenerCommands pins what each desktop is asked to do, including the one
// difference between them: Finder and Explorer select the item, xdg-open can
// only show the folder that holds it.
func TestOpenerCommands(t *testing.T) {
	cases := []struct {
		goos   string
		reveal bool
		path   string
		isDir  bool
		want   string
		args   []string
	}{
		{"linux", false, "https://goi.dev", false, "xdg-open", []string{"https://goi.dev"}},
		{"darwin", false, "https://goi.dev", false, "open", []string{"https://goi.dev"}},
		{"windows", false, "https://goi.dev", false, "rundll32", []string{"url.dll,FileProtocolHandler", "https://goi.dev"}},
		{"linux", true, "/home/me/notes/a.md", false, "xdg-open", []string{"/home/me/notes"}},
		{"linux", true, "/home/me/notes", true, "xdg-open", []string{"/home/me/notes"}},
		{"darwin", true, "/Users/me/notes/a.md", false, "open", []string{"-R", "/Users/me/notes/a.md"}},
		{"windows", true, `C:\notes\a.md`, false, "explorer", []string{`/select,C:\notes\a.md`}},
	}
	for _, c := range cases {
		var name string
		var args []string
		if c.reveal {
			name, args = revealCommand(c.goos, c.path, c.isDir)
		} else {
			name, args = launchCommand(c.goos, c.path)
		}
		if name != c.want || strings.Join(args, " ") != strings.Join(c.args, " ") {
			t.Fatalf("%s (reveal %v) %q: %s %v, want %s %v", c.goos, c.reveal, c.path, name, args, c.want, c.args)
		}
	}
}

const linkUI = `Window {
	Column {
		padding: 10
		RichText {
			id:    doc
			wrap:  true
			width: 320
			Text { text: "Read the " }
			Text { text: "guide" link: "https://goi.dev/guide" }
			Text { text: " today." }
		}
		Text { id: site text: "goi.dev" link: "https://goi.dev" }
	}
}`

// TestRichTextLinkClick covers the click on a run: the pointer over a link is a
// pointer, a click on it opens that run's target, and a click on the plain text
// around it opens nothing.
func TestRichTextLinkClick(t *testing.T) {
	_, win, in := inputFixture(t, linkUI, 480, 320)
	doc := MustByID[*RichText](win, "doc")
	var opened []string
	in.open = func(link string) error {
		opened = append(opened, link)
		return nil
	}
	var cursor platform.Cursor
	in.setCursor = func(c platform.Cursor) { cursor = c }
	r := absoluteRect(doc)
	cy := r.Y + r.H/4
	// The first run is plain, so a click at the start of the line is not a link.
	in.MouseDown(r.X+1, cy, 0, false)
	in.MouseUp(r.X+1, cy, 0)
	if len(opened) != 0 {
		t.Fatalf("a click on the plain prefix opened %v", opened)
	}
	// Sweep the line: every click that lands on the link opens the same target.
	linkX := -1.0
	for x := r.X; x < r.X+r.W; x += 2 {
		before := len(opened)
		in.MouseDown(x, cy, 0, false)
		in.MouseUp(x, cy, 0)
		if len(opened) > before && linkX < 0 {
			linkX = x
		}
	}
	if len(opened) == 0 {
		t.Fatal("no click on the line opened the link")
	}
	for _, link := range opened {
		if link != "https://goi.dev/guide" {
			t.Fatalf("opened %q", link)
		}
	}
	// Over a link the pointer is the link shape, over the paragraph's plain
	// text it stays the arrow.
	in.MouseMove(linkX, cy)
	if cursor != platform.CursorPointer {
		t.Fatalf("cursor over a paragraph of links = %v", cursor)
	}
	in.MouseMove(r.X+1, cy)
	if cursor == platform.CursorPointer {
		t.Fatalf("the plain prefix reports a link cursor")
	}
}

// TestLinkHandlerRoutesTheClick gives an app the choice: with an OnLink handler
// goi opens nothing, so `goi://` targets stay inside the app.
func TestLinkHandlerRoutesTheClick(t *testing.T) {
	_, win, in := inputFixture(t, linkUI, 480, 320)
	var routed string
	site := MustByID[*Text](win, "site")
	site.OnLink(func(link string) { routed = link })
	opened := 0
	in.open = func(string) error {
		opened++
		return nil
	}
	r := absoluteRect(site)
	in.MouseDown(r.X+2, r.Y+r.H/2, 0, false)
	in.MouseUp(r.X+2, r.Y+r.H/2, 0)
	if routed != "https://goi.dev" {
		t.Fatalf("handler got %q", routed)
	}
	if opened != 0 {
		t.Fatalf("the app's handler did not stop the desktop open (%d)", opened)
	}

	// A RichText keeps one slot for the paragraph, so a run's click lands in the
	// handler the app registered on the node.
	doc := MustByID[*RichText](win, "doc")
	routed = ""
	doc.OnLink(func(link string) { routed = link })
	docRect := absoluteRect(doc)
	for x := docRect.X; x < docRect.X+docRect.W; x += 2 {
		in.MouseDown(x, docRect.Y+docRect.H/4, 0, false)
		in.MouseUp(x, docRect.Y+docRect.H/4, 0)
	}
	if routed != "https://goi.dev/guide" {
		t.Fatalf("RichText handler got %q", routed)
	}
	if opened != 0 {
		t.Fatalf("the RichText handler did not stop the desktop open (%d)", opened)
	}
}
