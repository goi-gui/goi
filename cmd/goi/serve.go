//go:build !js && !wasip1

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/goi-gui/goi/internal/i18n"
	"github.com/spf13/cobra"
)

// webHMRMarker is the global ui/web_hmr_js.go sets once its client is running.
// A build without that string in the wasm predates browser hot reload.
const webHMRMarker = "__GOI_HMR_READY__"

// hmrMessage is one event on the development stream. ui/web_hmr_js.go decodes
// the same shape in the browser; keep the two in step.
type hmrMessage struct {
	Type    string            `json:"type"`
	Files   map[string]string `json:"files,omitempty"`
	Message string            `json:"message,omitempty"`
}

func newServeCmd(goRun runFunc) *cobra.Command {
	var host string
	var port int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "build the browser app and serve it with hot reload",
		Long: "Build the entrypoint for the browser, then serve it on a local port. Editing a .goi file or an i18n catalog\n" +
			"updates the open page without reloading it, and the app state survives. A Go change rebuilds the wasm and\n" +
			"reloads the page.\n\n" +
			"The page is index.html at the project root, or a generated one when there is none.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serveProject(".", goRun, cmd.OutOrStdout(), cmd.ErrOrStderr(), host, port)
		},
	}
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "interface to listen on")
	cmd.Flags().IntVar(&port, "port", 8080, "port to listen on")
	return cmd
}

// serveProject builds the browser target and serves it until the process exits.
func serveProject(dir string, goRun runFunc, stdout, stderr io.Writer, host string, port int) error {
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	flags, err := resolveFlags(dir, cfg.Assets, cfg.BuildFlags)
	if err != nil {
		return err
	}
	out, err := buildProject(dir, "js", "wasm", goRun, stdout, stderr)
	if err != nil {
		return err
	}
	wasmDir := filepath.Dir(out)
	if err := copyWasmExec(wasmDir); err != nil {
		return err
	}
	assetsDir := resolvePath(dir, cfg.Assets)
	snapshot, err := designSnapshot(assetsDir)
	if err != nil {
		return err
	}
	hub := newSSEHub()
	hub.setSnapshot(snapshot)

	s := &devServer{
		projectDir: dir,
		assetsDir:  assetsDir,
		wasmDir:    wasmDir,
		wasmName:   filepath.Base(out),
		goRun:      goRun,
		stdout:     stdout,
		stderr:     stderr,
		hub:        hub,
		featureKey: flags.FeatureKey,
		builds:     make(chan struct{}, 1),
	}
	s.live.Store(wasmSupportsHMR(out))
	if !s.live.Load() {
		s.warnNoHMR()
	}
	go s.buildLoop()
	go func() {
		if err := s.watch(); err != nil {
			fmt.Fprintln(stderr, "goi: watcher:", err)
		}
	}()

	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		fmt.Fprintf(stderr, "goi: warning: listening on %s exposes this project to the network, and goi serve has no authentication\n", host)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "goi: serving http://%s\n", ln.Addr())
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second}
	return srv.Serve(ln)
}

// devServer is the HTTP handler and the watcher for one goi serve run.
type devServer struct {
	projectDir string
	assetsDir  string
	wasmDir    string
	wasmName   string
	goRun      runFunc
	stdout     io.Writer
	stderr     io.Writer
	hub        *sseHub

	mu         sync.Mutex
	featureKey string
	builds     chan struct{}
	// live is true when the built wasm carries the browser hot-reload client.
	// An older goi dependency has none, so design edits fall back to a rebuild.
	live atomic.Bool
}

func (s *devServer) warnNoHMR() {
	fmt.Fprintln(s.stderr, "goi: warning: this wasm was built with a goi version that predates browser hot reload")
	fmt.Fprintln(s.stderr, "goi: warning: update github.com/goi-gui/goi in the project, then run goi serve again")
	fmt.Fprintln(s.stderr, "goi: warning: design edits will rebuild the wasm and reload the page until then")
}

// wasmSupportsHMR searches the built module for the client's global. The
// compiler keeps the string literal in the binary. It streams the file instead
// of reading a 40 MB module into memory for a one-line answer.
func wasmSupportsHMR(name string) bool {
	f, err := os.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	marker := []byte(webHMRMarker)
	const chunk = 64 << 10
	buf := make([]byte, chunk+len(marker)-1)
	n := 0
	for {
		if n == len(buf) {
			if bytes.Contains(buf, marker) {
				return true
			}
			copy(buf[:len(marker)-1], buf[n-len(marker)+1:n])
			n = len(marker) - 1
		}
		read, err := f.Read(buf[n:])
		n += read
		if bytes.Contains(buf[:n], marker) {
			return true
		}
		if err != nil {
			return false
		}
	}
}

// ServeHTTP serves the page, the wasm, wasm_exec.js and any static file the
// project root holds, with the development stream on its own path.
func (s *devServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/__goi/hmr" {
		s.hub.serve(w, r)
		return
	}
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		s.serveIndex(w)
		return
	}
	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if serveDevFile(w, r, s.wasmDir, rel) || servePageAsset(w, r, s.projectDir, rel) {
		return
	}
	http.NotFound(w, r)
}

// pageAssetExts are the project-root files a served page may reference. The
// root is also where the sources live, so anything outside this set (go.mod,
// goi.yaml, the .go files) stays private even when --host opens the port.
var pageAssetExts = map[string]bool{
	".html": true, ".css": true, ".js": true, ".mjs": true, ".map": true,
	".json": true, ".wasm": true, ".webmanifest": true, ".txt": true,
	".svg": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".avif": true, ".jxl": true, ".ico": true, ".woff": true, ".woff2": true,
	".ttf": true, ".otf": true, ".mp3": true, ".mp4": true, ".webm": true,
	".wav": true, ".ogg": true,
}

// servePageAsset serves one file the page itself may reference.
func servePageAsset(w http.ResponseWriter, r *http.Request, base, rel string) bool {
	if !pageAssetExts[strings.ToLower(path.Ext(rel))] {
		return false
	}
	return serveDevFile(w, r, base, rel)
}

// serveIndex reads the project's page on every request, so editing the HTML
// only needs a browser refresh. Without one it writes a minimal page for the
// built wasm.
func (s *devServer) serveIndex(w http.ResponseWriter) {
	page, err := os.ReadFile(filepath.Join(s.projectDir, "index.html"))
	if errors.Is(err, os.ErrNotExist) {
		page = []byte(generatedIndex(s.wasmName))
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(injectHMR(page))
}

func serveDevFile(w http.ResponseWriter, r *http.Request, base, rel string) bool {
	if rel == "" || rel == "." || strings.HasPrefix(rel, "../") ||
		strings.HasPrefix(rel, ".") || strings.Contains(rel, "/.") {
		return false
	}
	name := filepath.Join(base, filepath.FromSlash(rel))
	info, err := os.Stat(name)
	if err != nil || info.IsDir() {
		return false
	}
	http.ServeFile(w, r, name)
	return true
}

// hmrMarker tells the wasm to follow the stream. A static page has no marker,
// so a production build never opens a connection. The timeout catches a wasm
// whose goi dependency predates the feature: it never sets READY, and the
// developer gets a console line instead of a silent no-op.
const hmrMarker = `<script>
window.__GOI_HMR__="/__goi/hmr";
setTimeout(function () {
	if (!window.__GOI_HMR_READY__) {
		console.error("goi: this page has no hot-reload client; the wasm's github.com/goi-gui/goi dependency predates browser hot reload.");
	}
}, 8000);
</script>`

func injectHMR(page []byte) []byte {
	s := string(page)
	if strings.Contains(s, "__GOI_HMR__") {
		return page
	}
	if i := strings.LastIndex(strings.ToLower(s), "</body>"); i >= 0 {
		return []byte(s[:i] + hmrMarker + s[i:])
	}
	return []byte(s + hmrMarker)
}

func generatedIndex(wasmName string) string {
	title := html.EscapeString(strings.TrimSuffix(wasmName, ".wasm"))
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
	html, body { margin: 0; height: 100%%; }
	canvas { width: 100%%; height: 100%%; display: block; }
</style>
</head>
<body>
<canvas></canvas>
<script src="wasm_exec.js"></script>
<script>
	const go = new Go();
	WebAssembly.instantiateStreaming(fetch(%q), go.importObject)
		.then((result) => go.run(result.instance))
		.catch((err) => { document.body.textContent = "goi: " + err; });
</script>
</body>
</html>
`, title, wasmName)
}

// designSnapshot reads every file the state-preserving reload can carry: the
// design and the i18n catalogs. Paths are relative to the assets root.
func designSnapshot(assetsDir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(assetsDir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(assetsDir, name)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		ext := filepath.Ext(name)
		if ext != ".goi" && !(ext == ".json" && path.Dir(rel) == i18n.Dir) {
			return nil
		}
		body, err := os.ReadFile(name)
		if err != nil {
			// A save mid-rename: the next event brings the finished file.
			return nil
		}
		out[rel] = string(body)
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return out, nil
}

func sameSnapshot(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for name, body := range a {
		if b[name] != body {
			return false
		}
	}
	return true
}

// watch reports design edits, asset edits and Go edits to the server. Design
// edits go over the stream as a full snapshot; everything else needs a new wasm.
func (s *devServer) watch() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	addTree := func(root string) {
		_ = filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !entry.IsDir() {
				return nil
			}
			switch entry.Name() {
			case ".git", "dist", "node_modules":
				if name != root {
					return filepath.SkipDir
				}
			}
			_ = w.Add(name)
			return nil
		})
	}
	addTree(s.projectDir)

	assets := time.NewTimer(time.Hour)
	assets.Stop()
	build := time.NewTimer(time.Hour)
	build.Stop()
	reload := time.NewTimer(time.Hour)
	reload.Stop()
	reset := func(t *time.Timer, d time.Duration) {
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		t.Reset(d)
	}
	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if s.ignored(ev.Name) {
				continue
			}
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					addTree(ev.Name)
					continue
				}
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			switch {
			case s.isDesignFile(ev.Name):
				if s.live.Load() {
					reset(assets, 100*time.Millisecond)
					break
				}
				// No client in the wasm: keep the stream snapshot current for a
				// later dependency update, but show the edit by rebuilding.
				s.refreshSnapshot()
				reset(build, 250*time.Millisecond)
			case s.isAsset(ev.Name), s.isSource(ev.Name):
				reset(build, 250*time.Millisecond)
			case strings.EqualFold(filepath.Ext(ev.Name), ".html"):
				reset(reload, 100*time.Millisecond)
			}
		case <-assets.C:
			s.publishDesign()
		case <-build.C:
			select {
			case s.builds <- struct{}{}:
			default:
			}
		case <-reload.C:
			s.hub.broadcast(hmrMessage{Type: "reload"})
		case _, ok := <-w.Errors:
			if !ok {
				return nil
			}
		}
	}
}

// publishDesign sends the current design when only its text changed. A design
// edit that brought in a new optional feature needs a new wasm instead, because
// the tag set is chosen at build time.
func (s *devServer) publishDesign() {
	if flags, ok := s.resolvedFlags(); ok && flags.Auto && s.featureChanged(flags.FeatureKey) {
		select {
		case s.builds <- struct{}{}:
		default:
		}
		return
	}
	s.refreshSnapshot()
}

func (s *devServer) refreshSnapshot() {
	files, err := designSnapshot(s.assetsDir)
	if err != nil {
		fmt.Fprintln(s.stderr, "goi: design:", err)
		return
	}
	s.hub.publishAssets(files)
}

func (s *devServer) buildLoop() {
	for range s.builds {
		s.rebuild()
	}
}

func (s *devServer) rebuild() {
	out, err := buildProject(s.projectDir, "js", "wasm", s.goRun, s.stdout, s.stderr)
	if err != nil {
		fmt.Fprintln(s.stderr, "goi: build failed:", err)
		s.hub.broadcast(hmrMessage{Type: "error", Message: "build failed; see the goi serve terminal"})
		return
	}
	if flags, ok := s.resolvedFlags(); ok {
		s.setFeatureKey(flags.FeatureKey)
	}
	// A go.mod change can bring in a goi version that has the client, so a
	// rebuild may turn the stream on without a restart.
	if live := wasmSupportsHMR(out); live != s.live.Swap(live) {
		if live {
			fmt.Fprintln(s.stdout, "goi: hot reload on")
		} else {
			s.warnNoHMR()
		}
	}
	s.hub.broadcast(hmrMessage{Type: "reload"})
}

func (s *devServer) featureChanged(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == s.featureKey {
		return false
	}
	s.featureKey = key
	return true
}

func (s *devServer) setFeatureKey(key string) {
	s.mu.Lock()
	s.featureKey = key
	s.mu.Unlock()
}

// resolvedFlags reads goi.yaml again, so a buildFlags or build tag change is
// followed without restarting the server.
func (s *devServer) resolvedFlags() (resolvedFlags, bool) {
	cfg, err := loadConfig(s.projectDir)
	if err != nil {
		return resolvedFlags{}, false
	}
	flags, err := resolveFlags(s.projectDir, cfg.Assets, cfg.BuildFlags)
	if err != nil {
		return resolvedFlags{}, false
	}
	return flags, true
}

func (s *devServer) ignored(name string) bool {
	base := filepath.Base(name)
	if base == "goi_embed_gen.go" || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "#") || strings.HasSuffix(base, "~") {
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".tmp", ".swp", ".swo", ".bak", ".orig":
		return true
	}
	rel, err := filepath.Rel(s.projectDir, name)
	if err != nil {
		return true
	}
	rel = filepath.ToSlash(rel)
	return rel == "dist" || strings.HasPrefix(rel, "dist/") ||
		rel == ".git" || strings.HasPrefix(rel, ".git/") ||
		rel == "node_modules" || strings.HasPrefix(rel, "node_modules/")
}

func (s *devServer) assetsRel(name string) (string, bool) {
	rel, err := filepath.Rel(s.assetsDir, name)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func (s *devServer) isDesignFile(name string) bool {
	rel, ok := s.assetsRel(name)
	if !ok {
		return false
	}
	ext := filepath.Ext(name)
	return ext == ".goi" || (ext == ".json" && path.Dir(rel) == i18n.Dir)
}

func (s *devServer) isAsset(name string) bool {
	_, ok := s.assetsRel(name)
	return ok
}

func (s *devServer) isSource(name string) bool {
	if strings.EqualFold(filepath.Ext(name), ".go") {
		return true
	}
	switch filepath.Base(name) {
	case "go.mod", "go.sum", "goi.yaml":
		return true
	}
	return false
}

// copyWasmExec puts the runtime shim next to the wasm. Go moved it from
// misc/wasm to lib/wasm, so both locations are tried.
func copyWasmExec(dir string) error {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return fmt.Errorf("go env GOROOT: %w", err)
	}
	root := strings.TrimSpace(string(out))
	for _, rel := range []string{"lib/wasm/wasm_exec.js", "misc/wasm/wasm_exec.js"} {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		return os.WriteFile(filepath.Join(dir, "wasm_exec.js"), body, 0o644)
	}
	return errors.New("wasm_exec.js not found under GOROOT")
}

// sseHub fans one stream out to every open page.
type sseHub struct {
	mu       sync.Mutex
	clients  map[chan []byte]struct{}
	snapshot map[string]string
}

func newSSEHub() *sseHub {
	return &sseHub{clients: map[chan []byte]struct{}{}}
}

func (h *sseHub) setSnapshot(files map[string]string) {
	h.mu.Lock()
	h.snapshot = files
	h.mu.Unlock()
}

// publishAssets sends files when they differ from the last snapshot. It returns
// false for an edit that did not change any design text.
func (h *sseHub) publishAssets(files map[string]string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sameSnapshot(h.snapshot, files) {
		return false
	}
	h.snapshot = files
	body, err := json.Marshal(hmrMessage{Type: "assets", Files: files})
	if err != nil {
		return false
	}
	for ch := range h.clients {
		select {
		case ch <- body:
		default:
		}
	}
	return true
}

func (h *sseHub) broadcast(msg hmrMessage) {
	body, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- body:
		default:
		}
	}
}

func (h *sseHub) serve(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	h.mu.Lock()
	ch := make(chan []byte, 8)
	h.clients[ch] = struct{}{}
	snapshot := h.snapshot
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}()

	w.WriteHeader(http.StatusOK)
	fl.Flush()
	if snapshot != nil {
		if err := writeEvent(w, hmrMessage{Type: "assets", Files: snapshot}); err != nil {
			return
		}
		fl.Flush()
	}
	keep := time.NewTicker(20 * time.Second)
	defer keep.Stop()
	for {
		select {
		case body := <-ch:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", body); err != nil {
				return
			}
			fl.Flush()
		case <-keep.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeEvent(w io.Writer, msg hmrMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", body)
	return err
}
