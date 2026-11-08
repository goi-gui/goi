package svg

import (
	"strings"
)

// css is the little of a stylesheet an animated icon uses: class rules with
// presentation properties, and custom properties on :root that a var() refers
// to. A full CSS engine is not the point; a file exported from a design tool
// keeps its colours in `.shape { fill: var(--indicator) }`, which oksvg ignores,
// so the shapes would draw nothing at all.
type css struct {
	classes map[string]map[string]string
	vars    map[string]string
}

// empty reports whether the stylesheet said nothing useful.
func (c *css) empty() bool { return len(c.classes) == 0 && len(c.vars) == 0 }

// parseCSS reads <style> blocks. Later blocks win, as in CSS; @media is dropped
// (the light :root values are used), which is the one deliberate approximation.
func parseCSS(blocks []string) *css {
	out := &css{classes: map[string]map[string]string{}, vars: map[string]string{}}
	for _, block := range blocks {
		block = stripCSSComments(block)
		block = stripAtRules(block)
		for _, rule := range splitRules(block) {
			sel, decls := rule.selector, parseDecls(rule.body)
			if len(decls) == 0 {
				continue
			}
			for _, one := range strings.Split(sel, ",") {
				one = strings.TrimSpace(one)
				if one == ":root" || one == "*" || one == "svg" {
					for k, v := range decls {
						if strings.HasPrefix(k, "--") {
							out.vars[k] = v
						}
					}
					continue
				}
				if !strings.HasPrefix(one, ".") {
					continue
				}
				name := strings.TrimPrefix(one, ".")
				if out.classes[name] == nil {
					out.classes[name] = map[string]string{}
				}
				for k, v := range decls {
					out.classes[name][k] = v
				}
			}
		}
	}
	return out
}

func stripCSSComments(s string) string {
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "*/")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+j+2:]
	}
}

// stripAtRules removes @media (and any other at-rule) blocks by matching braces.
func stripAtRules(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '@' {
			b.WriteByte(s[i])
			i++
			continue
		}
		// Skip to the opening brace, then to its matching close.
		depth := 0
		for i < len(s) {
			if s[i] == '{' {
				depth++
			} else if s[i] == '}' {
				depth--
				if depth == 0 {
					i++
					break
				}
			}
			i++
		}
	}
	return b.String()
}

type cssRule struct{ selector, body string }

func splitRules(s string) []cssRule {
	var out []cssRule
	for {
		open := strings.Index(s, "{")
		if open < 0 {
			return out
		}
		close := strings.Index(s[open:], "}")
		if close < 0 {
			return out
		}
		close += open
		out = append(out, cssRule{selector: strings.TrimSpace(s[:open]), body: s[open+1 : close]})
		s = s[close+1:]
	}
}

func parseDecls(body string) map[string]string {
	out := map[string]string{}
	for _, decl := range strings.Split(body, ";") {
		i := strings.Index(decl, ":")
		if i < 0 {
			continue
		}
		k := strings.TrimSpace(decl[:i])
		v := strings.TrimSpace(decl[i+1:])
		if k == "" || v == "" || strings.Contains(k, " ") {
			continue
		}
		out[k] = v
	}
	return out
}

// resolve replaces var(--name) and var(--name, fallback) with the :root value.
func (c *css) resolve(v string) string {
	for pass := 0; pass < 4; pass++ {
		i := strings.Index(v, "var(")
		if i < 0 {
			return v
		}
		rest := v[i+4:]
		close := strings.Index(rest, ")")
		if close < 0 {
			return v
		}
		inner := rest[:close]
		name, fallback := inner, ""
		if comma := strings.Index(inner, ","); comma >= 0 {
			name, fallback = inner[:comma], strings.TrimSpace(inner[comma+1:])
		}
		name = strings.TrimSpace(name)
		val, ok := c.vars[name]
		if !ok {
			val = fallback
		}
		v = v[:i] + val + rest[close+1:]
	}
	return v
}

// style returns the presentation attributes an element's classes ask for, with
// var() resolved. An attribute the element already writes wins, as in CSS only
// inline style would override a stylesheet but a presentation attribute is
// weaker — either way the file's own attribute is the more specific statement.
func (c *css) style(e *elem) map[string]string {
	if c.empty() {
		return nil
	}
	var class string
	for _, a := range e.attrs {
		if a.Name.Local == "class" {
			class = a.Value
			break
		}
	}
	if class == "" {
		return nil
	}
	out := map[string]string{}
	for _, name := range strings.Fields(class) {
		for k, v := range c.classes[name] {
			out[k] = c.resolve(v)
		}
	}
	return out
}

// collectCSS gathers every <style> block in the tree. A design tool writes one
// at the top level, but a hand-written file may put it in <defs>.
func collectCSS(root *elem) *css {
	var blocks []string
	walkPaths(root, nil, func(e *elem, _ []int) {
		if e.name.Local != "style" {
			return
		}
		blocks = append(blocks, e.text)
	})
	if len(blocks) == 0 {
		return &css{}
	}
	return parseCSS(blocks)
}
