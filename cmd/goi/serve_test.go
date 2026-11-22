//go:build !js && !wasip1

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeServeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDesignSnapshotCarriesDesignAndCatalogs(t *testing.T) {
	dir := t.TempDir()
	writeServeFile(t, dir, "ui/Main.goi", "Window {}")
	writeServeFile(t, dir, "ui/parts/Part.goi", "Text {}")
	writeServeFile(t, dir, "i18n/en.json", `{"hello":"Hello"}`)
	writeServeFile(t, dir, "i18n/notes.txt", "not a catalog")
	writeServeFile(t, dir, "images/logo.svg", "<svg/>")

	snap, err := designSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"ui/Main.goi":       "Window {}",
		"ui/parts/Part.goi": "Text {}",
		"i18n/en.json":      `{"hello":"Hello"}`,
	}
	if !sameSnapshot(snap, want) {
		t.Fatalf("snapshot = %v, want %v", snap, want)
	}
}

func TestWasmSupportsHMR(t *testing.T) {
	with := filepath.Join(t.TempDir(), "with.wasm")
	if err := os.WriteFile(with, []byte("prefix "+webHMRMarker+" suffix"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !wasmSupportsHMR(with) {
		t.Fatal("a wasm carrying the marker was not recognised")
	}
	without := filepath.Join(t.TempDir(), "without.wasm")
	if err := os.WriteFile(without, []byte("old build"), 0o644); err != nil {
		t.Fatal(err)
	}
	if wasmSupportsHMR(without) {
		t.Fatal("a wasm without the marker was accepted")
	}
}

func TestInjectHMR(t *testing.T) {
	page := injectHMR([]byte("<html><body>hi</body></html>"))
	if !strings.Contains(string(page), hmrMarker) {
		t.Fatalf("page lacks the marker: %s", page)
	}
	if !strings.Contains(string(page), "hi") {
		t.Fatal("page body was dropped")
	}
	already := []byte(`<script>window.__GOI_HMR__="/mine"</script>`)
	if got := injectHMR(already); string(got) != string(already) {
		t.Fatal("a page with its own marker was changed")
	}
}

func TestDevServerServesPageAndWasm(t *testing.T) {
	dir := t.TempDir()
	writeServeFile(t, dir, "index.html", "<html><body>hi</body></html>")
	writeServeFile(t, dir, "app.wasm", "wasm")
	s := &devServer{projectDir: dir, wasmDir: dir, wasmName: "app.wasm", hub: newSSEHub()}
	srv := httptest.NewServer(s)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "__GOI_HMR__") {
		t.Fatalf("served page has no marker: %s", body)
	}

	res, err = http.Get(srv.URL + "/app.wasm")
	if err != nil {
		t.Fatal(err)
	}
	wasm, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(wasm) != "wasm" {
		t.Fatalf("wasm body = %q", wasm)
	}
	if got := res.Header.Get("Content-Type"); got != "application/wasm" {
		t.Fatalf("wasm content type = %q", got)
	}

	res, err = http.Get(srv.URL + "/missing.js")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file status = %d", res.StatusCode)
	}
}
