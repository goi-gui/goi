//go:build !js && !wasip1

package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/goi-gui/goi/internal/syntax"
)

const (
	autoFlag = "auto"
	allFlag  = "all"
)

// autoFeatures are the optional build tags that a design can prove it needs.
// allFeatures is what `all` turns on. The dialog and notification APIs are
// always available now, so there is no tag to add for them.
var (
	autoFeatures = []string{"emoji", "svg", "mermaid", "markdown", "code", "net", "terminal", "webview", "setup"}
	allFeatures  = []string{"emoji", "svg", "mermaid", "markdown", "code", "net", "terminal", "webview", "setup"}
)

// resolvedFlags is the result of reading one flag list. Tags go to the go
// command; Display is the same set with `all` expanded and nofakecgo left out,
// for the run/build line. FeatureKey changes when auto's detected feature set
// changes, which is the signal for goi run to restart.
type resolvedFlags struct {
	Tags       []string
	Display    []string
	FeatureKey string
	Auto       bool
}

// resolveFlags turns goi.yaml's runFlags or buildFlags into Go tags. `auto`
// adds the features the .goi files use and drops stale feature tags; custom
// tags such as abc and myflag stay. `all` wins over auto because it already
// covers every feature.
func resolveFlags(projectDir, assets string, configured []string) (resolvedFlags, error) {
	flags := splitFlags(configured)
	// dialog used to gate the picker and notification APIs. Keep old configs
	// working by retiring the tag silently; dialogs are always available.
	flags = removeFlag(flags, "dialog")
	hasAll := containsFlag(flags, allFlag)
	hasAuto := containsFlag(flags, autoFlag) && !hasAll

	custom := make([]string, 0, len(flags))
	for _, flag := range flags {
		if flag == autoFlag || flag == allFlag {
			continue
		}
		if (hasAuto || hasAll) && containsFlag(autoFeatures, flag) {
			// Auto owns these tags: a stale one has to disappear when the
			// design stops using the feature. all already covers them.
			continue
		}
		custom = append(custom, flag)
	}

	if hasAll {
		return resolvedFlags{
			Tags:    append([]string{allFlag}, custom...),
			Display: append(append([]string{}, allFeatures...), custom...),
		}, nil
	}
	if hasAuto {
		features, err := scanFeatures(filepath.Join(projectDir, filepath.FromSlash(assets)))
		if err != nil {
			return resolvedFlags{}, err
		}
		tags := append(append([]string{}, features...), custom...)
		return resolvedFlags{
			Tags:       tags,
			Display:    append([]string{}, tags...),
			FeatureKey: strings.Join(features, ","),
			Auto:       true,
		}, nil
	}
	display := append([]string{}, flags...)
	if containsFlag(flags, allFlag) {
		display = append([]string{}, allFeatures...)
		display = append(display, custom...)
	}
	return resolvedFlags{Tags: flags, Display: display}, nil
}

func splitFlags(flags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, flag := range flags {
		for _, tag := range strings.FieldsFunc(flag, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		}) {
			if tag == "" || seen[tag] {
				continue
			}
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out
}

func removeFlag(flags []string, drop string) []string {
	out := flags[:0]
	for _, flag := range flags {
		if flag != drop {
			out = append(out, flag)
		}
	}
	return out
}

func containsFlag(flags []string, want string) bool {
	for _, flag := range flags {
		if flag == want {
			return true
		}
	}
	return false
}

// scanFeatures walks every .goi file under the asset container and reports the
// optional features they need. A missing container has no features; an
// unreadable one is a real error.
func scanFeatures(assetsDir string) ([]string, error) {
	found := map[string]bool{}
	err := filepath.WalkDir(assetsDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".goi") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hasEmoji(src) {
			found["emoji"] = true
		}
		if hasRemoteURL(src) {
			found["net"] = true
		}
		file, _ := syntax.Parse(src)
		if file != nil && len(file.Setups) > 0 {
			found["setup"] = true
		}
		if file == nil || file.Root == nil {
			return nil
		}
		syntax.WalkElements(file.Root, func(el *syntax.Element) {
			switch el.Name {
			case "Markdown":
				found["markdown"] = true
			case "Code":
				found["code"] = true
			case "Mermaid":
				found["mermaid"] = true
			case "SVG":
				found["svg"] = true
			case "Map":
				found["net"] = true
			case "Terminal":
				found["terminal"] = true
			case "WebView":
				found["webview"] = true
			}
		})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	out := make([]string, 0, len(found))
	for _, tag := range autoFeatures {
		if found[tag] {
			out = append(out, tag)
		}
	}
	return out, nil
}

// hasRemoteURL catches a URL in a string or an expression, where inspecting
// only a literal src would miss a fetch and leave net out of the build.
func hasRemoteURL(src []byte) bool {
	lower := strings.ToLower(string(src))
	return strings.Contains(lower, "http://") || strings.Contains(lower, "https://")
}

// hasEmoji reports whether text contains a rune from the emoji blocks the
// optional emoji font data is needed for.
func hasEmoji(src []byte) bool {
	for len(src) > 0 {
		r, size := utf8.DecodeRune(src)
		src = src[size:]
		switch {
		case r >= 0x1F000 && r <= 0x1FAFF: // pictographs and supplemental symbols
			return true
		case r >= 0x2600 && r <= 0x27BF: // misc symbols and dingbats
			return true
		case r >= 0x1F1E6 && r <= 0x1F1FF: // regional indicators
			return true
		case r == 0xFE0F: // emoji presentation selector
			return true
		}
	}
	return false
}

// tagsLine is what the run/build log prints: no `all`, and no nofakecgo.
func tagsLine(display []string) string {
	if len(display) == 0 {
		return "(none)"
	}
	return strings.Join(display, ", ")
}

// logTags prints the resolved flags with all expanded and nofakecgo left out,
// so an all project says what it actually turned on.
func logTags(w io.Writer, kind string, flags resolvedFlags) {
	fmt.Fprintf(w, "goi: %s tags: %s\n", kind, tagsLine(flags.Display))
}
