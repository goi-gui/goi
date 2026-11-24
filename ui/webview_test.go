package ui

import (
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// TestWebviewURLExtractsEmbeddedPage: a goi:// page keeps its sibling CSS and
// scripts because the whole container is written to one temp directory, and
// the directory goes away with the views.
func TestWebviewURLExtractsEmbeddedPage(t *testing.T) {
	a := newHeadlessApp()
	a.assets = fstest.MapFS{
		"ui/docs/index.html": &fstest.MapFile{Data: []byte("<link rel=stylesheet href=style.css>")},
		"ui/docs/style.css":  &fstest.MapFile{Data: []byte("body{margin:0}")},
		"odd/a#b.html":       &fstest.MapFile{Data: []byte("x")},
	}
	url, err := a.webviewURL("goi://ui/docs/index.html")
	if err != nil {
		t.Fatalf("goi://: %v", err)
	}
	if !strings.HasPrefix(url, "file:///") {
		t.Fatalf("url = %q, want a three-slash file URL", url)
	}
	parsed, err := neturl.Parse(url)
	if err != nil || parsed.Path == "" {
		t.Fatalf("url = %q does not parse: %v", url, err)
	}
	index := parsed.Path
	got, err := os.ReadFile(index)
	if err != nil || string(got) != "<link rel=stylesheet href=style.css>" {
		t.Fatalf("index content = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(index), "style.css")); err != nil {
		t.Fatalf("the sibling asset did not land next to the page: %v", err)
	}

	// A second asset reuses the same directory rather than copying twice.
	url2, err := a.webviewURL("goi://ui/docs/style.css")
	if err != nil {
		t.Fatalf("second goi://: %v", err)
	}
	parsed2, err := neturl.Parse(url2)
	if err != nil {
		t.Fatalf("second url = %q does not parse: %v", url2, err)
	}
	if filepath.Dir(parsed2.Path) != filepath.Dir(index) {
		t.Fatal("the second asset was extracted to a different directory")
	}

	// A plain URL is passed straight through.
	if got, err := a.webviewURL("https://example.com/x"); err != nil || got != "https://example.com/x" {
		t.Fatalf("https url = %q, %v", got, err)
	}

	// A missing entry is an error, not an empty page.
	if _, err := a.webviewURL("goi://ui/docs/missing.html"); err == nil {
		t.Fatal("a missing embedded page loaded without an error")
	}

	// A path that walks out of the design is refused: goi:// is the design
	// container, not the file system.
	if _, err := a.webviewURL("goi://../../../../etc/hostname"); err == nil {
		t.Fatal("goi://.. escaped the design container")
	}

	// A name with URL syntax characters survives the trip to a file URL.
	url3, err := a.webviewURL("goi://odd/a#b.html")
	if err != nil {
		t.Fatalf("odd name: %v", err)
	}
	if !strings.Contains(url3, "%23") {
		t.Fatalf("url = %q does not escape #", url3)
	}

	a.closeWebviews()
	if a.webviewDir != "" {
		t.Fatalf("webviewDir = %q after close", a.webviewDir)
	}
	if _, err := os.Stat(filepath.Dir(index)); !os.IsNotExist(err) {
		t.Fatalf("the extraction directory survived closeWebviews: %v", err)
	}
}

// TestWebviewFrameHidesUnderNativeLimits: a native view cannot follow a clip,
// a rotation, a scale or a fade, so the frame reports it as not placeable.
func TestWebviewFrameHidesUnderNativeLimits(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"visible", `Window { Stack { width: 300 height: 200
			WebView { id: view width: 100 height: 100 } } }`},
		{"clipped", `Window { Stack { width: 300 height: 200
			WebView { id: view width: 400 height: 100 } } }`},
		{"rotated", `Window { Stack { width: 300 height: 200 rotate: 45deg
			WebView { id: view width: 100 height: 100 } } }`},
		{"scaled", `Window { Stack { width: 300 height: 200 scale: 2
			WebView { id: view width: 100 height: 100 } } }`},
		{"half faded", `Window { Stack { width: 300 height: 200 opacity: 0.5
			WebView { id: view width: 100 height: 100 } } }`},
		{"faded", `Window { Stack { width: 300 height: 200 opacity: 0
			WebView { id: view width: 100 height: 100 } } }`},
		{"hidden", `Window { Stack { width: 300 height: 200
			WebView { id: view width: 100 height: 100 visible: false } } }`},
	}
	want := map[string]bool{"visible": true}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, win, _ := inputFixture(t, tc.src, 500, 400)
			wv := MustByID[*WebView](win, "view")
			if _, ok := a.webviewFrame(wv); ok != want[tc.name] {
				t.Fatalf("webviewFrame ok = %v, want %v", ok, want[tc.name])
			}
		})
	}
}

// TestTreeHasWebview: only a design with the element pays for the runtime.
func TestTreeHasWebview(t *testing.T) {
	_, win, _ := inputFixture(t, `Window { Column { WebView { width: 10 height: 10 } } }`, 100, 100)
	if !treeHasWebview(win.tree) {
		t.Fatal("a nested WebView was not found")
	}
	_, win, _ = inputFixture(t, `Window { Column { Text { text: "hi" } } }`, 100, 100)
	if treeHasWebview(win.tree) {
		t.Fatal("a design without a WebView reported one")
	}
}

func TestWebviewMessageReachesTheNode(t *testing.T) {
	_, win, _ := inputFixture(t, `Window { WebView { id: view width: 10 height: 10 } }`, 100, 100)
	wv := MustByID[*WebView](win, "view")
	var got []string
	wv.OnMessage(func(msg string) { got = append(got, msg) })
	wv.DeliverMessage("hello")
	wv.DeliverMessage("world")
	if len(got) != 2 || got[0] != "hello" || got[1] != "world" {
		t.Fatalf("messages = %v", got)
	}
}
