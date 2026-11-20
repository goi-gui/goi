package lsp

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// designRootFor finds the asset container a document belongs to: the `assets`
// directory in the nearest goi.yaml, or the document's own directory when there
// is no project. The name is the document's path inside that container, so
// `goi://ui/Main.goi` and a relative import resolve the way the runtime does.
func (s *Server) designRootFor(d *document) (dir, name string) {
	if d == nil {
		return "", ""
	}
	for at := filepath.Dir(d.path); ; {
		if b, err := os.ReadFile(filepath.Join(at, "goi.yaml")); err == nil {
			var cfg struct {
				Assets string `yaml:"assets"`
			}
			if yaml.Unmarshal(b, &cfg) == nil {
				assets := cfg.Assets
				if assets == "" {
					assets = "assets"
				}
				root := assets
				if !filepath.IsAbs(root) {
					root = filepath.Join(at, filepath.FromSlash(assets))
				}
				if rel, ok := relUnder(root, d.path); ok {
					return root, rel
				}
			}
		}
		parent := filepath.Dir(at)
		if parent == at {
			break
		}
		at = parent
	}
	return filepath.Dir(d.path), filepath.Base(d.path)
}

// designOverlay roots the loader at the asset container and serves every open
// document under it from memory, so an unsaved fragment is what validation
// sees.
func (s *Server) designOverlay(d *document) (overlayFS, string) {
	dir, name := s.designRootFor(d)
	overlay := map[string][]byte{}
	for _, open := range s.docs {
		if rel, ok := relUnder(dir, open.path); ok {
			overlay[rel] = open.text
		}
	}
	if _, ok := overlay[name]; !ok {
		overlay[name] = d.text
	}
	return overlayFS{dir: dir, overlay: overlay}, name
}

// resolveImport turns an import path into an absolute file. `goi://path` starts
// at the container root; a relative path starts at the importing file.
func (s *Server) resolveImport(d *document, target string) (string, bool) {
	root, from := s.designRootFor(d)
	return resolveDesignPath(root, from, target)
}

// resolveDesignPath is resolveImport for a known root and importing path.
func resolveDesignPath(root, from, target string) (string, bool) {
	if strings.HasPrefix(target, "goi://") {
		clean := path.Clean(strings.TrimPrefix(target, "goi://"))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			return "", false
		}
		return filepath.Join(root, filepath.FromSlash(clean)), true
	}
	if strings.HasPrefix(target, "/") {
		return "", false
	}
	clean := path.Clean(path.Join(path.Dir(from), target))
	if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", false
	}
	return filepath.Join(root, filepath.FromSlash(clean)), true
}

// relUnder returns slash-form rel when path is inside root.
func relUnder(root, target string) (string, bool) {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
