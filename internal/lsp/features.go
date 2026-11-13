package lsp

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goi-gui/goi/internal/asset"
	"github.com/goi-gui/goi/internal/csscolor"
	"github.com/goi-gui/goi/internal/goifmt"
	"github.com/goi-gui/goi/internal/highlight"
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/schema"
	"github.com/goi-gui/goi/internal/syntax"
	"github.com/goi-gui/goi/internal/text"
)

// diagnostic is one published problem, in LSP shape.
type diagnostic struct {
	Range    rng    `json:"range"`
	Severity int    `json:"severity"` // 1 error, 2 warning, 3 info, 4 hint
	Source   string `json:"source"`
	Message  string `json:"message"`
}

// validate parses and validates one open document, publishing diagnostics to
// every file they point at (a broken import is reported in the fragment, not in
// the entry file).
func (s *Server) validate(d *document) {
	idx := newLineIndex(d.text)
	if _, diag := syntax.Parse(d.text); !diag.Empty() {
		s.publish(d.uri, convertDiags(idx, diag))
		return
	}

	fsys, name := s.dirOverlay(d)
	bundle, diag := asset.Load(fsys, name)
	if !diag.Empty() {
		// The bundle carries the sources it managed to read, so each diagnostic
		// can land in its own file.
		s.publishDiagSet(d, idx, diag, bundle)
		return
	}
	// The editor never requires a Window root: a design split into pages has
	// many roots, and the runtime is where the entry point is checked.
	_, vdiag := schema.Validate(bundle.File)
	s.publishDiagSet(d, idx, vdiag, bundle)
}

// publishDiagSet groups diagnostics by file and publishes each group, then
// clears any file in this design that no longer has one.
func (s *Server) publishDiagSet(d *document, idx lineIndex, diag syntax.Diagnostics, bundle *asset.Bundle) {
	dir := filepath.Dir(d.path)
	base := filepath.Base(d.path)

	byURI := map[string]syntax.Diagnostics{}
	for _, item := range diag {
		// An empty Src means "the file being validated".
		uri := d.uri
		if item.Src != "" && item.Src != base {
			uri = pathToURI(filepath.Join(dir, filepath.FromSlash(item.Src)))
		}
		byURI[uri] = append(byURI[uri], item)
	}

	for uri, items := range byURI {
		useIdx := idx
		if uri != d.uri {
			// Each file has its own offsets, so the index follows the file.
			if body, ok := bundle.Sources[baseOf(uri, dir)]; ok {
				useIdx = newLineIndex(body)
			}
		}
		s.publish(uri, convertDiags(useIdx, items))
	}
	// A fix removes the squiggle from files that had one.
	for uri := range s.published {
		if _, ok := byURI[uri]; ok {
			continue
		}
		if filepath.Dir(uriToPath(uri)) == dir {
			s.publish(uri, nil)
		}
	}
}

// convertDiags turns parser diagnostics into LSP diagnostics for one file.
func convertDiags(idx lineIndex, diag syntax.Diagnostics) []diagnostic {
	if len(diag) == 0 {
		return nil
	}
	out := make([]diagnostic, 0, len(diag))
	for _, item := range diag {
		msg := item.Message
		if item.Hint != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", item.Hint)
		}
		out = append(out, diagnostic{
			Range:    idx.at(int(item.Pos), int(item.End)),
			Severity: 1,
			Source:   "goipls",
			Message:  msg,
		})
	}
	return out
}

// baseOf maps a file URI back to the bundle-relative name it was loaded under.
func baseOf(uri, dir string) string {
	p := uriToPath(uri)
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return filepath.Base(p)
	}
	return filepath.ToSlash(rel)
}

// ---------------------------------------------------------------- completion

type completionParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Position position `json:"position"`
}

// completionList is the LSP result for a completion request.
type completionList struct {
	IsIncomplete bool               `json:"isIncomplete"`
	Items        []completionResult `json:"items"`
}

type completionResult struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind"`
	Detail        string `json:"detail,omitempty"`
	Documentation *doc   `json:"documentation,omitempty"`
	// InsertText differs from Label only when the label cannot be typed bare,
	// such as an icon name that starts with a digit (`"10k"`).
	InsertText string `json:"insertText,omitempty"`
	// TextEdit replaces the text the client would otherwise substitute. A member
	// of a typed reference needs it: after `theme.` the word under the cursor is
	// empty, so a bare insert of `theme.primary` would leave
	// `theme.theme.primary`.
	TextEdit *completionTextEdit `json:"textEdit,omitempty"`
}

// completionTextEdit is a single replacement range, the shape LSP completion
// items use for `textEdit`.
type completionTextEdit struct {
	Range   rng    `json:"range"`
	NewText string `json:"newText"`
}

type doc struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// CompletionItemKind values the server uses.
const (
	kindVariable   = 6
	kindClass      = 7
	kindProperty   = 10
	kindKeyword    = 14
	kindColor      = 16
	kindValue      = 12
	kindEnumMember = 20
	kindConstant   = 21
	kindEvent      = 23
	kindFunction   = 3
)

func (s *Server) completion(raw json.RawMessage) any {
	var p completionParams
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	d, ok := s.docs[p.TextDocument.URI]
	if !ok {
		return nil
	}
	idx := newLineIndex(d.text)
	off := idx.offset(p.Position)

	wordStart := off
	for wordStart > 0 && isWordByte(d.text[wordStart-1]) {
		wordStart--
	}
	prefix := string(d.text[wordStart:off])
	before := d.text[:wordStart]
	line := before
	if i := lastIndexByte(line, '\n'); i >= 0 {
		line = line[i+1:]
	}

	items := []completionResult{}
	incomplete := false
	file, _ := syntax.Parse(d.text)
	prop, inMap := enclosingProperty(file, d.text, off)
	ref := enclosingRef(before)
	// refStart is where the whole reference begins, so an item can replace
	// `theme.pr` instead of inserting after it.
	refStart := off - len(prefix)
	if ref != "" {
		refStart -= len(ref) + 1
	}

	// A responsive map key, a prop declaration inside the `props` block, the
	// value of a property, or a member of a reserved namespace (`app.`,
	// `window.`).
	switch {
	case inMap:
		items = append(items, breakpointItems()...)
	case inDeclBlock(d.text, off):
		items = append(items, declTypeItems(file, d.text, off)...)
	case prop != "":
		values, cut := valueItems(file, prop, d.text, off, prefix, insideString(d.text, off), s.fragmentProp(d, file, d.text, off, prop))
		items = append(items, values...)
		incomplete = cut
	}
	if ref != "" {
		items = append(items, refItems(file, d.text, off, ref)...)
	}

	// Member position: the line so far is only whitespace, or we are right
	// after an opening brace.
	trimmed := strings.TrimSpace(string(line))
	switch {
	case prop == "" && !inMap && strings.HasPrefix(prefix, "{"):
		// The Svelte spelling is gone; point at the Go one.
		items = append(items, completionResult{
			Label: "if", Kind: kindKeyword, Detail: "control",
			Documentation: &doc{Kind: "markdown", Value: "The `{#if}` form is gone: write `if cond { … } else { … }`."},
		})
	case prop == "" && !inMap && strings.HasPrefix(prefix, "$"):
		// `$` starts a slot here: a slot is an element, a prop is a value.
		items = append(items, slotItems(file, d.text, off)...)
	case prop == "" && !inMap && (trimmed == "" || strings.HasSuffix(trimmed, "{")):
		if imp, ok := s.fragmentAt(d, file, d.text, off); ok {
			// A fragment instance body: its props, the root's overridable
			// properties and the slots it fills.
			items = append(items, s.fragmentMemberItems(d, imp)...)
			items = append(items, elementItems(file)...)
		} else {
			items = append(items, memberItems(file, d.text, off)...)
			items = append(items, blockItems(file)...)
		}
	case prop == "" && !inMap:
		items = append(items, elementItems(file)...)
	}
	// A member item that carries the whole reference as its text needs an edit
	// covering the reference the design already typed: the client replaces the
	// word under the cursor, which is empty right after the dot.
	if ref != "" {
		for i := range items {
			it := &items[i]
			text := it.InsertText
			if text == "" {
				text = it.Label
			}
			// `theme` typed, then `theme` again: the item inserts itself only by
			// replacing what is there.
			if !strings.HasPrefix(text, ref+".") {
				continue
			}
			it.TextEdit = &completionTextEdit{Range: idx.at(refStart, off), NewText: text}
		}
	}
	return completionList{Items: dedupe(items, prefix, ref), IsIncomplete: incomplete}
}

// inDeclBlock reports whether off sits inside a top-level declaration block
// (`props`, `bridge`, `data` or `type Name`), tracked by braces so an element
// body that happens to contain the word does not count.
func inDeclBlock(src []byte, off int) bool {
	depth := 0
	for i := off - 1; i >= 0; i-- {
		switch src[i] {
		case '}':
			depth++
		case '{':
			if depth > 0 {
				depth--
				continue
			}
			j := i - 1
			for j >= 0 && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
				j--
			}
			end := j + 1
			for j >= 0 && isWordByte(src[j]) {
				j--
			}
			switch string(src[j+1 : end]) {
			case "props", "bridge", "data":
				return true
			}
			// `type Name {` — the word before the brace is the type's name.
			k := j
			for k >= 0 && (src[k] == ' ' || src[k] == '\t') {
				k--
			}
			wordEnd := k + 1
			for k >= 0 && isWordByte(src[k]) {
				k--
			}
			return string(src[k+1:wordEnd]) == "type"
		}
	}
	return false
}

// declTypeItems offers a type after a declaration's name. Nothing else in a
// declaration is a choice: the name belongs to the design.
func declTypeItems(file *syntax.File, src []byte, off int) []completionResult {
	line := off
	for line > 0 && src[line-1] != '\n' {
		line--
	}
	// The type follows the name, so a word on the line means the cursor is in
	// type position.
	if !strings.ContainsAny(string(src[line:off]), "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_") {
		return nil
	}
	out := make([]completionResult, 0, len(propTypes)+6)
	for _, t := range []string{"[]", "map[string]", "func("} {
		out = append(out, completionResult{
			Label: t, Kind: kindKeyword, Detail: "composite type",
			Documentation: &doc{Kind: "markdown", Value: "**" + t + "…**\n\nA slice (`[]Ticket`), a map (`map[string]int`) or a handler (`func(id string)`)."},
		})
	}
	for _, t := range propTypes {
		out = append(out, completionResult{
			Label: t, Kind: kindKeyword, Detail: "type",
			Documentation: &doc{Kind: "markdown", Value: "**" + t + "**\n\nA value of this type reads as a `" + typeNameOf(t) +
				"` in the design. Every primitive Go has is accepted, plus `color`."},
		})
	}
	if file != nil {
		for _, sh := range file.Shapes {
			out = append(out, completionResult{
				Label: sh.Name, Kind: kindClass, Detail: "declared type",
				Documentation: &doc{Kind: "markdown", Value: "**" + sh.Name + "**\n\nA declared row type. Use it as `[]" + sh.Name +
					"` to hold a list and `for i, row := range` to walk one."},
			})
		}
	}
	return out
}

// typeNameOf names the value type a declaration reads as.
func typeNameOf(t string) string {
	switch t {
	case "bool":
		return "bool"
	case "string", "byte", "rune":
		return "string"
	case "color":
		return "color"
	}
	return "number"
}

// propTypes are the primitive names a declaration may use: Go's primitives plus
// `color`, which the design needs and Go spells as a uint32.
var propTypes = []string{
	"bool", "string", "color",
	"int", "int8", "int16", "int32", "int64",
	"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
	"float32", "float64", "byte", "rune",
}

// builtinItems lists the Go builtins a value can call, with the signature and
// one line that explains it.
func builtinItems() []completionResult {
	type entry struct{ name, insert, doc string }
	entries := []entry{
		{"formatf", `formatf("", )`, `formatf("%d items", bridge.Count) — build a string with Go's fmt verbs: ` + "`%d`" + ` counts, ` + "`%s`" + ` text, ` + "`%v`" + ` any value, ` + "`%0.2f`" + ` money.`},
		{"t", `t("")`, `t("settings.theme") — a translation from the design's i18n folder. A nested key uses dots, and later arguments fill the entry's verbs: ` + "`t(\"welcome_with_name\", \"Kaan\")`" + `.`},
		{"len", "len()", "len(x) — the length of a string, a list or a map. A list is a value here, unlike anywhere else."},
		{"cap", "cap()", "cap(x) — a design list has no spare capacity, so this is its length."},
		{"min", "min()", "min(a, b, …) — the smallest number or string."},
		{"max", "max()", "max(a, b, …) — the largest number or string."},
	}
	out := make([]completionResult, 0, len(entries))
	for _, e := range entries {
		out = append(out, completionResult{
			Label:         e.name,
			InsertText:    e.insert,
			Kind:          kindFunction,
			Detail:        "builtin",
			Documentation: &doc{Kind: "markdown", Value: e.doc},
		})
	}
	return out
}

// blockItems offers the control statements a body can contain, written the way
// Go writes them.
func blockItems(file *syntax.File) []completionResult {
	list := "bridge.rows"
	if lists := listVars(file); len(lists) > 0 {
		list = lists[0]
	}
	return []completionResult{
		{
			Label: "if", Kind: kindKeyword, Detail: "control",
			InsertText: "if ",
			Documentation: &doc{Kind: "markdown", Value: "**if cond { … } else if cond { … } else { … }**\n\n" +
				"Shows the first arm whose condition holds. The arms are ordinary children of the " +
				"enclosing element, so a block adds no box of its own."},
		},
		{
			Label: "for", Kind: kindKeyword, Detail: "control",
			InsertText: "for i, row := range " + list,
			Documentation: &doc{Kind: "markdown", Value: "**for i, row := range " + list + " { … }**\n\n" +
				"A range loop, with Go's rules: a slice gives index and element, a map gives key and " +
				"value, and a number counts from zero. `_` ignores a name you do not need."},
		},
		{
			Label: "switch", Kind: kindKeyword, Detail: "control",
			InsertText: "switch ",
			Documentation: &doc{Kind: "markdown", Value: "**switch value { case a, b: … default: … }**\n\n" +
				"Shows the first arm holding a value equal to the subject. Arms take a comma-separated " +
				"list of values; `default` shows when none matches. A block adds no box of its own."},
		},
		{
			Label: "t", Kind: kindFunction, Detail: "builtin",
			InsertText: `t("")`,
			Documentation: &doc{Kind: "markdown", Value: "**t(\"settings.theme\")**\n\n" +
				"Reads a translation from the design's `i18n/<language>.json` files. A nested " +
				"key uses dots, and later arguments fill the entry's verbs: " +
				"`t(\"welcome_with_name\", \"Kaan\")` with `\"Hoşgeldin %s\"`. A missing key " +
				"falls back to the fallback language, then shows the key itself."},
		},
		{
			Label: "formatf", Kind: kindFunction, Detail: "builtin",
			InsertText: `formatf("", )`,
			Documentation: &doc{Kind: "markdown", Value: "**formatf(\"Count is %d\", bridge.count)**\n\n" +
				"Builds a string with Go's `fmt` verbs: `%d` counts, `%s` text, `%v` any value, " +
				"`%0.2f` money. The format string comes first; every later argument fills one verb."},
		},
	}
}

// slotDoc explains a slot marker. It takes no properties; the instance supplies
// the elements, at `$children` for its plain children or `$name` for a named
// slot it fills with `$name { ... }`.
const slotDoc = "**$children** — slot\n\n" +
	"Where the children passed by an instance appear. A fragment that writes " +
	"this takes children; passing children to one that does not is an error. " +
	"With no children the slot draws nothing, so the fragment still previews on " +
	"its own. A file that writes a slot is a fragment.\n\n" +
	"A named slot works the same way: write `$header` in the fragment, and an " +
	"instance fills it with `$header { ... }`."

// namedSlotDoc explains `$name` at a hover position.
func namedSlotDoc(name string) string {
	return "**" + name + "** — named slot\n\n" +
		"The instance passes elements here with `" + name + " { ... }`. A fragment " +
		"that writes this takes that slot; passing one it does not declare is an " +
		"error. With nothing passed the slot draws nothing, so the fragment still " +
		"previews on its own."
}

// listVars lists every collection a design can range over: bridge variables and
// props whose type is a slice, written as the reference a loop takes.
func listVars(file *syntax.File) []string {
	if file == nil {
		return nil
	}
	var out []string
	for _, dv := range file.Vars {
		if dv.Type.Kind == syntax.TypeSlice {
			out = append(out, "bridge."+dv.Name)
		}
	}
	for _, pr := range file.Props {
		if pr.Type.Kind == syntax.TypeSlice {
			out = append(out, "props."+pr.Name)
		}
	}
	return out
}

// slotItems offers `$children` inside an element body. Only a container can
// hold it, and only once, since a second slot is almost always a typo.
func slotItems(file *syntax.File, src []byte, off int) []completionResult {
	if el, ok := schema.Lookup(enclosingElementName(file, src, off)); ok {
		if !el.Children || hasSlot(elementAt(file, off)) {
			return nil
		}
	}
	return []completionResult{{
		Label:         "$children",
		Kind:          kindKeyword,
		Detail:        "slot",
		Documentation: &doc{Kind: "markdown", Value: slotDoc},
	}}
}

// hasSlot reports whether an element already writes `$children`.
func hasSlot(el *syntax.Element) bool {
	if el == nil {
		return false
	}
	for _, k := range el.Children() {
		if k.Name == "$children" {
			return true
		}
	}
	return false
}

// globalItems lists the members of a reserved namespace. A design can read
// `app.theme`, `app.dark`, `app.os`, `app.systemLanguage` and the `window.*`
// state; nothing else is accepted, so completing the closed set is the whole
// story.
func globalItems(ref string) []completionResult {
	type member struct {
		name, typ, doc string
	}
	var names []member
	switch ref {
	case "app":
		names = []member{
			{"theme", "string", "the resolved scheme: light, dark or a registered name"},
			{"mode", "light | dark", "the resolved scheme's mood; feed an enum or compare it"},
			{"systemMode", "light | dark", "the desktop's preference; `theme: app.systemMode` follows it"},
			{"systemLanguage", "string", "the desktop's language as an ISO 639-1 code, e.g. tr or en"},
			{"dark", "bool", "true when the resolved scheme is dark"},
			{"os", "string", "runtime.GOOS: linux, darwin, windows, freebsd"},
		}
	case "window":
		names = []member{
			{"width", "number", "logical client width, updated on resize"},
			{"height", "number", "logical client height, updated on resize"},
			{"minimized", "bool", "whether the window is minimized, reported by the platform"},
			{"minimizable", "bool", "whether the framework may minimize the window"},
			{"maximized", "bool", "whether the window is maximized, reported by the platform"},
			{"maximizable", "bool", "whether the framework may maximize the window"},
			{"resizable", "bool", "whether the user may resize the window"},
			{"fullscreen", "bool", "whether the window fills the display"},
		}
	default:
		return nil
	}
	out := make([]completionResult, 0, len(names))
	for _, m := range names {
		out = append(out, completionResult{
			Label:  m.name,
			Kind:   kindProperty,
			Detail: ref + "." + m.name + " · " + m.typ,
			Documentation: &doc{
				Kind:  "markdown",
				Value: "**" + ref + "." + m.name + "**\n\nType: `" + m.typ + "`\n\n" + m.doc,
			},
		})
	}
	return out
}

// enclosingRef reports the reserved namespace the cursor is completing, given
// the word prefix being typed and the text before it. `app.o` yields "app", and
// so does `visible: app.` at the very start of a value.
func enclosingRef(before []byte) string {
	// Only the current line counts: a dot on an earlier line belongs to
	// another value.
	if i := lastIndexByte(before, '\n'); i >= 0 {
		before = before[i+1:]
	}
	if i := lastIndexByte(before, '.'); i >= 0 {
		start := i
		for start > 0 && isWordByte(before[start-1]) {
			start--
		}
		return string(before[start:i])
	}
	return ""
}

// refItems completes the member of a dotted reference: a reserved namespace
// (`app.`, `window.`), the `props` block, the `bridge` block (also spelled
// `data`), a loop variable or an element id.
func refItems(file *syntax.File, src []byte, off int, ref string) []completionResult {
	if file == nil {
		return nil
	}
	switch ref {
	case "theme":
		// The scheme's colour roles, which is the whole list a colour position
		// accepts after `theme.`.
		return colourRoleItems("theme.")
	case "customColor":
		return []completionResult{{
			Label:  "myColor",
			Kind:   kindColor,
			Detail: "customColor.name · the app defines it",
			Documentation: &doc{Kind: "markdown", Value: "**customColor.name**\n\n" +
				"A colour the app defined with `ui.SetCustomColor(\"name\", …)`. " +
				"The design cannot list these, because they are declared in Go."},
		}}
	case "app", "window":
		return globalItems(ref)
	case "props":
		return fieldItems(file.Props, "props", "An instance sets it as a property on the call.")
	case "data", "bridge":
		return fieldItems(file.Vars, ref, "Set it from Go with `ui.SetData(w, …)`. A handler is registered with `ui.SetHandler`.")
	}
	sh := loopShapeAt(file, src, off, ref)
	if sh == nil {
		return idMemberItems(file, ref)
	}
	return fieldItems(sh.Fields, ref, "Field of `"+sh.Name+"`.")
}

// fieldItems lists declarations as completion items, with their type and the
// one line that explains where the value comes from.
func fieldItems(fields []syntax.Field, prefix, source string) []completionResult {
	out := make([]completionResult, 0, len(fields))
	for _, f := range fields {
		typ := f.Type.String()
		kind := kindVariable
		if f.Type.Kind == syntax.TypeFunc {
			kind = kindFunction
		}
		out = append(out, completionResult{
			Label:  f.Name,
			Kind:   kind,
			Detail: prefix + "." + f.Name + " · " + typ,
			Documentation: &doc{Kind: "markdown", Value: "**" + prefix + "." + f.Name + "**\n\n" +
				declDoc(f, prefix) + "\n\n" + source},
		})
	}
	return out
}

// declDoc is the hover text of one declaration: its Go type and what it holds.
func declDoc(f syntax.Field, prefix string) string {
	var b strings.Builder
	b.WriteString("Type: `" + f.Type.String() + "`")
	switch f.Type.Kind {
	case syntax.TypeSlice:
		b.WriteString("\n\nA list. Walk it with `for i, row := range " + prefix + "." + f.Name + "`; " +
			"`_` ignores the index.")
	case syntax.TypeMap:
		b.WriteString("\n\nA map. Walk it with `for key, value := range " + prefix + "." + f.Name + "`.")
	case syntax.TypeFunc:
		b.WriteString("\n\nA handler. Call it in an event, as `onClick: " + prefix + "." + f.Name + "(…)`.")
	case syntax.TypeScalar:
		if f.Type.Name == "color" {
			b.WriteString("\n\nA colour: a theme role, a name the app defines with `ui.SetCustomColor`, or a `#rrggbb` literal.")
		} else if isNumericName(f.Type.Name) {
			b.WriteString("\n\nA number: arithmetic, comparisons and `formatf` verbs all apply.")
		} else if f.Type.Name == "bool" {
			b.WriteString("\n\nA bool: use it as an `if` condition, directly or in a comparison.")
		} else {
			b.WriteString("\n\nA string: `+` joins it with any value, and `formatf` formats it.")
		}
	}
	return b.String()
}

// isNumericName reports whether a declared type is a Go number.
func isNumericName(name string) bool {
	switch name {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"float32", "float64", "byte", "rune":
		return true
	}
	return false
}

// idMemberItems completes `abc.width`, where `abc` is an id declared in the
// file. It offers the element's properties (the schema already merges the
// common ones into Attrs), so an id reference reads like the node it names.
func idMemberItems(file *syntax.File, ref string) []completionResult {
	kind, ok := idElementAt(file, ref)
	if !ok {
		return nil
	}
	el, ok := schema.Lookup(kind)
	if !ok {
		return nil
	}
	out := make([]completionResult, 0, len(el.Attrs)+3)
	for _, p := range el.Attrs {
		out = append(out, completionResult{
			Label:         p.Name,
			Kind:          kindProperty,
			Detail:        ref + "." + p.Name + " · " + p.Type.String(),
			Documentation: &doc{Kind: "markdown", Value: propDoc(el.Name, p)},
		})
	}
	if animatedKind(el.Name) {
		for _, m := range schema.NodeMethods {
			out = append(out, completionResult{
				Label:         m + "(…)",
				InsertText:    m + "(",
				Kind:          kindFunction,
				Detail:        ref + "." + m + "() · playback",
				Documentation: &doc{Kind: "markdown", Value: nodeMethodDoc(m)},
			})
		}
	}
	return out
}

// animatedKind reports whether an element plays an animation, so an id of that
// kind offers the playback methods.
func animatedKind(kind string) bool {
	switch kind {
	case "Image", "SVG", "Audio", "Video":
		return true
	}
	return false
}

// nodeMethodDoc explains one playback method, for completion and hover.
func nodeMethodDoc(name string) string {
	switch name {
	case "play":
		return "**play()**\n\nResumes the animation, or replays it after `stop()` or a finished one-shot."
	case "pause":
		return "**pause()**\n\nHolds the current frame; `play()` carries on from it."
	case "stop":
		return "**stop()**\n\nRewinds to the first frame and pauses there; `play()` starts it again."
	case "toggle":
		return "**toggle()**\n\nStarts a paused player and pauses a playing one."
	case "seek":
		return "**seek(seconds)**\n\nJumps to a position in seconds; the decoder restarts there."
	case "volume":
		return "**volume(0..1)**\n\nSets the player's volume."
	case "fullscreen":
		return "**fullscreen()**\n\nFills the window with a video and puts the window in fullscreen; Escape leaves."
	case "windowed":
		return "**windowed()**\n\nLeaves fullscreen and puts the video back in its box."
	case "audioTrack":
		return "**audioTrack(0)**\n\nPicks the audio stream, 0 being the first."
	case "toggleFullscreen":
		return "**toggleFullscreen()**\n\nFlips between fullscreen and windowed."
	}
	return ""
}

// loopShapeAt resolves a loop variable at off: the innermost loop whose body
// contains the cursor and whose value name is ref. A loop over a slice of a
// declared type completes that type's fields.
func loopShapeAt(file *syntax.File, src []byte, off int, ref string) *syntax.DataShape {
	if file == nil || file.Root == nil {
		return nil
	}
	var found *syntax.DataShape
	var walk func(blk *syntax.Block)
	walk = func(blk *syntax.Block) {
		if off <= int(blk.At) || off > int(blk.Close) {
			return
		}
		if blk.Kind == syntax.BlockFor && blk.LoopVal() == ref {
			found = rangeElemShape(file, blk)
		}
		for _, br := range blk.Branches {
			for _, m := range br.Members {
				switch x := m.(type) {
				case *syntax.Block:
					walk(x)
				case *syntax.Element:
					walkElementsIn(x, walk)
				}
			}
		}
	}
	syntax.WalkElements(file.Root, func(e *syntax.Element) {
		for _, m := range e.Members {
			if blk, ok := m.(*syntax.Block); ok {
				walk(blk)
			}
		}
	})
	return found
}

// rangeElemShape resolves the declared shape a loop's value holds.
func rangeElemShape(file *syntax.File, blk *syntax.Block) *syntax.DataShape {
	ref, ok := blk.Range.(*syntax.Ref)
	if !ok {
		return nil
	}
	switch ref.ID {
	case "bridge", "data":
		if dv := file.Var(ref.Prop); dv != nil && dv.Type.Kind == syntax.TypeSlice {
			return shapeOf(file, dv.Type.Elem)
		}
	case "props":
		if pr := file.Prop(ref.Prop); pr != nil && pr.Type.Kind == syntax.TypeSlice {
			return shapeOf(file, pr.Type.Elem)
		}
	}
	return nil
}

// shapeOf resolves a type to the declared shape it names.
func shapeOf(file *syntax.File, t *syntax.Type) *syntax.DataShape {
	if t == nil || t.Kind != syntax.TypeScalar {
		return nil
	}
	return file.Shape(t.Name)
}

// walkElementsIn visits the blocks directly inside an element.
func walkElementsIn(e *syntax.Element, fn func(*syntax.Block)) {
	for _, m := range e.Members {
		if blk, ok := m.(*syntax.Block); ok {
			fn(blk)
		}
	}
}

// valueItems offers the values a property accepts. The bool reports that the
// list was cut short, so the client asks again as the user types.
func valueItems(file *syntax.File, prop string, src []byte, off int, prefix string, inString bool, frag *syntax.Prop) ([]completionResult, bool) {
	elName := enclosingElementName(file, src, off)
	el, ok := schema.Lookup(elName)
	var out []completionResult
	var truncated bool
	if !ok {
		// A fragment instance: its props are not in the schema, so the values
		// offered are the ones any value position has.
		return fragmentValueItems(file, prop, frag, prefix), false
	}
	if pd := el.Prop(prop); pd != nil {
		switch {
		case isIconProp(prop) && pd.Type == schema.TypeString:
			out, truncated = iconItems(prefix, inString)
		case pd.Type == schema.TypeBool:
			for _, v := range []string{"true", "false"} {
				out = append(out, completionResult{Label: v, Kind: kindKeyword, Detail: "bool"})
			}
		case pd.Type == schema.TypeEnum:
			for _, v := range pd.Enum {
				out = append(out, completionResult{
					Label: v, Kind: kindEnumMember,
					Detail: prop + ": " + v,
				})
			}
		case pd.Type == schema.TypeColor:
			out = append(out, colourItems(elName)...)
		case prop == "rotate":
			for _, v := range []string{"none", "90deg", "180deg", "0.25turn", "x 90deg", "y 90deg", "z 45deg", "1 1 1 90deg"} {
				out = append(out, completionResult{
					Label: v, Kind: kindKeyword, Detail: "rotate",
					Documentation: &doc{Kind: "markdown", Value: "**" + v + "**"},
				})
			}
		case prop == "language" && elName == "Code":
			// Code's language is a plain string in the schema, because an
			// unknown one must still draw; the lexers the highlighter ships are
			// what a reader wants to pick from here.
			out, truncated = languageItems(prefix, inString)
		case pd.Type == schema.TypeID:
			for _, id := range fileIDElements(file) {
				out = append(out, completionResult{Label: id.name, Kind: kindConstant, Detail: "id · " + id.kind})
			}
		}
	}
	// An expression may read a node by its id: `width: abc.width` reads
	// property `width` of the element whose `id: abc`. Offer the ids
	// wherever a value goes, next to the property's own values.
	for _, id := range fileIDElements(file) {
		out = append(out, completionResult{
			Label:         id.name,
			Kind:          kindVariable,
			Detail:        "id · " + id.kind,
			Documentation: &doc{Kind: "markdown", Value: "**" + id.name + "**\n\n`" + id.kind + "` element declared with `id: " + id.name + "`.\n\nRead its properties with `" + id.name + ".width`, `" + id.name + ".visible`, and so on."},
		})
	}
	if pd := el.Prop(prop); !isIconProp(prop) && (pd == nil || pd.Type == schema.TypeString || pd.Type == schema.TypeEnum) {
		out = append(out, builtinItems()...)
	}
	if _, ok := schema.Event(prop); ok && el.HasEvent(prop) {
		for _, f := range file.Props {
			if f.Type.Kind != syntax.TypeFunc {
				continue
			}
			out = append(out, completionResult{
				Label:         "props." + f.Name + "(…)",
				InsertText:    "props." + f.Name + "(",
				Kind:          kindFunction,
				Detail:        "handler call · " + f.Type.String(),
				Documentation: &doc{Kind: "markdown", Value: "**props." + f.Name + "**\n\n" + f.Type.String() + "\n\nThe instance passes the handler this fragment calls."},
			})
		}
		for _, f := range file.Vars {
			if f.Type.Kind != syntax.TypeFunc {
				continue
			}
			out = append(out, completionResult{
				Label:         "bridge." + f.Name + "(…)",
				InsertText:    "bridge." + f.Name + "(",
				Kind:          kindFunction,
				Detail:        "handler call · " + f.Type.String(),
				Documentation: &doc{Kind: "markdown", Value: "**bridge." + f.Name + "**\n\n" + f.Type.String() + "\n\nRuns the Go handler registered with `ui.SetHandler(w, \"" + f.Name + "\", …)` when this event fires."},
			})
		}
		// An event can drive an animation by id without a Go handler.
		for _, id := range fileIDElements(file) {
			if !animatedKind(id.kind) {
				continue
			}
			for _, m := range schema.NodeMethods {
				out = append(out, completionResult{
					Label:         id.name + "." + m + "(…)",
					InsertText:    id.name + "." + m + "(",
					Kind:          kindFunction,
					Detail:        "playback · " + id.kind,
					Documentation: &doc{Kind: "markdown", Value: nodeMethodDoc(m)},
				})
			}
		}
	}
	return out, truncated
}

// maxLanguageItems caps the code-language list. There are about three hundred
// lexers; sending them all on every keystroke is a lot of JSON, so the list is
// filtered by prefix and IsIncomplete asks the client for a longer one.
const maxLanguageItems = 60

// languageItems lists the languages a Code block can highlight, plus the short
// spellings people reach for. The aliases come first because `go`, `ts` and `py`
// are what a design actually writes.
func languageItems(prefix string, inString bool) ([]completionResult, bool) {
	prefix = strings.ToLower(prefix)
	out := make([]completionResult, 0, maxLanguageItems)
	add := func(name, detail string) bool {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			return true
		}
		if len(out) == maxLanguageItems {
			return false
		}
		item := completionResult{Label: name, Kind: kindEnumMember, Detail: detail}
		if !inString && !isIdentifier(name) {
			// `c++` and `emacs-lisp` are not bare .goi identifiers.
			item.InsertText = `"` + name + `"`
		}
		out = append(out, item)
		return true
	}
	names := make([]string, 0, len(highlight.Aliases()))
	for name := range highlight.Aliases() {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if !add(name, "alias of "+highlight.Aliases()[name]) {
			return out, true
		}
	}
	for _, name := range highlight.Languages() {
		if !add(strings.ToLower(name), "lexer") {
			return out, true
		}
	}
	return out, false
}

// isIdentifier reports whether s can be written as a bare .goi identifier.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// maxIconItems caps one icon list. The font names ~4300 icons; sending them all
// on every keystroke is a lot of JSON, and IsIncomplete makes the client ask
// again with a longer prefix.
const maxIconItems = 200

// isIconProp reports whether a property takes a Material Symbols ligature. The
// name is the only signal the schema carries: every icon property is spelled
// `icon`, `iconOn`, `iconOff`, `trailingIcon` and so on.
func isIconProp(name string) bool {
	return strings.Contains(strings.ToLower(name), "icon")
}

// iconItems lists icon names the font can draw, filtered by what has been
// typed. A name that starts with a digit is quoted, because `icon: 10k` would
// lex as a number followed by a word.
func iconItems(prefix string, inString bool) ([]completionResult, bool) {
	prefix = strings.ToLower(prefix)
	out := make([]completionResult, 0, maxIconItems)
	for _, name := range text.IconNames() {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		if len(out) == maxIconItems {
			return out, true
		}
		item := completionResult{
			Label:  name,
			Kind:   kindValue,
			Detail: "Material Symbols icon",
		}
		if !inString && name[0] >= '0' && name[0] <= '9' {
			item.InsertText = `"` + name + `"`
		}
		out = append(out, item)
	}
	return out, false
}

// insideString reports whether the cursor sits in a string literal on its line,
// so an icon name is inserted without adding quotes.
func insideString(src []byte, off int) bool {
	if off > len(src) {
		off = len(src)
	}
	start := 0
	if i := lastIndexByte(src[:off], '\n'); i >= 0 {
		start = i + 1
	}
	quotes := 0
	for i := start; i < off; i++ {
		switch src[i] {
		case '\\':
			i++
		case '"':
			quotes++
		}
	}
	return quotes%2 == 1
}

// memberItems offers properties and child elements inside an element body.
func memberItems(file *syntax.File, src []byte, off int) []completionResult {
	name := enclosingElementName(file, src, off)
	el, ok := schema.Lookup(name)
	if !ok {
		return elementItems(file)
	}
	out := make([]completionResult, 0, len(el.Attrs)+len(el.Events)+8)
	for _, p := range el.Attrs {
		out = append(out, completionResult{
			Label:         p.Name,
			Kind:          kindProperty,
			Detail:        propDetail(el.Name, p),
			Documentation: &doc{Kind: "markdown", Value: propDoc(el.Name, p)},
		})
	}
	for _, name := range el.Events {
		out = append(out, completionResult{
			Label:         name,
			Kind:          kindEvent,
			Detail:        name + " · handler call",
			Documentation: &doc{Kind: "markdown", Value: "**" + name + "** on `" + el.Name + "`\n\nBridge handler call, for example `" + name + ": bridge.onOpen(...)`. It runs only when the event fires."},
		})
	}
	if el.Children {
		items := elementItems(file)
		// A RichText's children are its runs, so only Text is worth offering
		// there; every other element would be rejected by the checker.
		if el.Name == "RichText" {
			runs := items[:0]
			for _, it := range items {
				if it.Label == "Text" {
					runs = append(runs, it)
				}
			}
			items = runs
		}
		out = append(out, items...)
		out = append(out, blockItems(file)...)
	}
	return out
}

// elementItems lists every element that can be written, plus the fragment
// aliases this file imports, so a call site completes like any other element.
func elementItems(file *syntax.File) []completionResult {
	names := schema.ElementNames()
	out := make([]completionResult, 0, len(names))
	if file != nil {
		for _, imp := range file.Imports {
			out = append(out, completionResult{
				Label:         imp.Alias,
				Kind:          kindClass,
				Detail:        "fragment",
				Documentation: &doc{Kind: "markdown", Value: "**" + imp.Alias + "** — fragment\n\nImported from `" + imp.Path + "`. Its props are properties on the call; `$name { ... }` fills a named slot and plain children fill `$children`."},
			})
		}
	}
	for _, n := range names {
		el, ok := schema.Lookup(n)
		if !ok {
			continue
		}
		detail := "leaf"
		if el.Children {
			detail = "container"
		}
		out = append(out, completionResult{
			Label:         n,
			Kind:          kindClass,
			Detail:        detail,
			Documentation: &doc{Kind: "markdown", Value: elementDoc(el)},
		})
	}
	return out
}

// breakpointItems lists the responsive map keys.
func breakpointItems() []completionResult {
	var out []completionResult
	for _, b := range []string{"base", "sm", "md", "lg", "xl"} {
		min, _ := schema.BreakpointMin(b)
		out = append(out, completionResult{
			Label:  b,
			Kind:   kindEnumMember,
			Detail: fmt.Sprintf("from %.0fdp", min),
		})
	}
	return out
}

// dedupe drops items that do not match the prefix being typed.
func dedupe(items []completionResult, prefix, ref string) []completionResult {
	seen := map[string]bool{}
	out := make([]completionResult, 0, len(items))
	for _, it := range items {
		if seen[it.Label] {
			continue
		}
		// A member item may label itself with the whole reference
		// (`theme.primary`), so the filter compares the member half once a
		// reference is already typed.
		label := it.Label
		if ref != "" {
			label = strings.TrimPrefix(label, ref+".")
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(label), strings.ToLower(prefix)) {
			continue
		}
		seen[it.Label] = true
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// ---------------------------------------------------------------- hover

func (s *Server) hover(raw json.RawMessage) any {
	d, pos, ok := s.atPos(raw)
	if !ok {
		return nil
	}
	idx := newLineIndex(d.text)
	off := idx.offset(pos)
	file, diag := syntax.Parse(d.text)
	if !diag.Empty() || file == nil {
		return nil
	}

	// A declaration explains itself: its Go type and what it holds.
	for _, pr := range file.Props {
		if start, end := int(pr.NameAt), int(pr.NameAt)+len(pr.Name); off >= start && off <= end {
			return map[string]any{
				"contents": doc{Kind: "markdown", Value: "**props." + pr.Name + "**\n\n" + declDoc(pr, "props")},
				"range":    idx.at(start, end),
			}
		}
	}
	for _, dv := range file.Vars {
		if start, end := int(dv.NameAt), int(dv.NameAt)+len(dv.Name); off >= start && off <= end {
			return map[string]any{
				"contents": doc{Kind: "markdown", Value: "**bridge." + dv.Name + "**\n\n" + declDoc(dv, "bridge")},
				"range":    idx.at(start, end),
			}
		}
	}
	// A shape's field explains itself too.
	for _, sh := range file.Shapes {
		for _, f := range sh.Fields {
			start, end := int(f.NameAt), int(f.NameAt)+len(f.Name)
			if off >= start && off <= end {
				return map[string]any{
					"contents": doc{Kind: "markdown", Value: "**" + sh.Name + "." + f.Name + "**\n\n" + declDoc(f, "row")},
					"range":    idx.at(start, end),
				}
			}
		}
	}
	// A bare loop variable is a value, and its fields are the shape it holds.
	if start, end, v := loopVarAt(file, off); v != "" {
		return map[string]any{
			"contents": doc{Kind: "markdown", Value: loopVarDoc(file, d.text, off, v)},
			"range":    idx.at(start, end),
		}
	}
	if start, end, el := elementNameAt(file, off); el != nil {
		if strings.HasPrefix(el.Name, "$") {
			value := slotDoc
			if el.Name != "$children" {
				value = namedSlotDoc(el.Name)
			}
			return map[string]any{
				"contents": doc{Kind: "markdown", Value: value},
				"range":    idx.at(start, end),
			}
		}
		if def, ok := schema.Lookup(el.Name); ok {
			return map[string]any{
				"contents": doc{Kind: "markdown", Value: elementDoc(def)},
				"range":    idx.at(start, end),
			}
		}
		// An instance of a fragment: say what it takes.
		if imp, ok := s.fragmentAt(d, file, d.text, off); ok && imp.Alias == el.Name {
			if frag := s.importFile(d, imp); frag != nil {
				return map[string]any{
					"contents": doc{Kind: "markdown", Value: fragmentDoc(imp, frag)},
					"range":    idx.at(start, end),
				}
			}
		}
		return map[string]any{
			"contents": doc{Kind: "markdown", Value: "**" + el.Name + "**"},
			"range":    idx.at(start, end),
		}
	}
	// A fragment instance's property is not in the schema, but the fragment
	// declares it.
	if start, end, pr, ok := s.fragmentPropertyAt(d, file, off); ok {
		return map[string]any{
			"contents": doc{Kind: "markdown", Value: "**" + pr.Name + "** on `" + fragmentAliasAt(file, off) + "`\n\n" + declDoc(*pr, "props")},
			"range":    idx.at(start, end),
		}
	}
	if start, end, el, prop := propertyAt(file, off); prop != nil {
		return map[string]any{
			"contents": doc{Kind: "markdown", Value: propDoc(el.Name, *prop)},
			"range":    idx.at(start, end),
		}
	}
	// A reference reads another element by id: `abc.width` explains either the
	// element it names or the property it reads.
	if r := refAt(file, off); r != nil {
		propStart := int(r.At) + len(r.ID) + 1
		// A loop variable and the field it reads explain themselves.
		if doc, ok := loopRefDoc(file, d.text, off, r); ok {
			return map[string]any{
				"contents": doc,
				"range":    idx.at(int(r.At), int(r.At)+len(r.ID)+1),
			}
		}
		if r.ID == "theme" || r.ID == "customColor" {
			// A colour reference: the first half names the namespace, the second
			// the colour.
			if off < propStart {
				return map[string]any{
					"contents": doc{Kind: "markdown", Value: namespaceDoc(r.ID)},
					"range":    idx.at(int(r.At), int(r.At)+len(r.ID)),
				}
			}
			body := colourRefDoc(r.ID, r.Prop)
			return map[string]any{
				"contents": doc{Kind: "markdown", Value: body},
				"range":    idx.at(propStart, propStart+len(r.Prop)),
			}
		}
		if r.ID == "props" {
			// Either half of props.title explains the prop.
			if pr := file.Prop(r.Prop); pr != nil {
				return map[string]any{
					"contents": doc{Kind: "markdown", Value: declaredPropDoc(*pr)},
					"range":    idx.at(propStart, propStart+len(r.Prop)),
				}
			}
			return nil
		}
		if off >= propStart {
			if kind, ok := idElementAt(file, r.ID); ok {
				if schema.NodeMethod(r.Prop) && animatedKind(kind) {
					return map[string]any{
						"contents": doc{Kind: "markdown", Value: nodeMethodDoc(r.Prop)},
						"range":    idx.at(propStart, propStart+len(r.Prop)),
					}
				}
				if el, ok := schema.Lookup(kind); ok {
					if pd := el.Prop(r.Prop); pd != nil {
						return map[string]any{
							"contents": doc{Kind: "markdown", Value: propDoc(el.Name, *pd)},
							"range":    idx.at(propStart, propStart+len(r.Prop)),
						}
					}
				}
			}
			return nil
		}
		if kind, ok := idElementAt(file, r.ID); ok {
			if el, ok := schema.Lookup(kind); ok {
				return map[string]any{
					"contents": doc{Kind: "markdown", Value: "**" + r.ID + "**\n\n" + elementDoc(el)},
					"range":    idx.at(int(r.At), int(r.At)+len(r.ID)),
				}
			}
		}
		return nil
	}
	// A value in an enum, icon or colour position explains itself.
	if start, end, prop, value := valueAt(file, off); value != "" {
		summary := fmt.Sprintf("`%s` for `%s`", value, prop.Name)
		if isIconProp(prop.Name) {
			summary = "Material Symbols icon `" + value + "`"
		}
		if prop.Type == schema.TypeColor {
			summary = colourDoc(value)
		}
		return map[string]any{
			"contents": doc{Kind: "markdown", Value: summary},
			"range":    idx.at(start, end),
		}
	}
	return nil
}

// colourDoc explains a colour value: a scheme role follows the theme, a custom
// name follows the app, and anything else is a typo the runtime warns about.
func colourDoc(name string) string {
	if _, ok := (m3.Colors{}).Role(name); ok {
		return "`" + name + "` is a theme role: it follows the light/dark scheme."
	}
	if strings.HasPrefix(name, "#") {
		return "`" + name + "` is a fixed hex literal: it does not follow the scheme."
	}
	if csscolor.IsName(name) {
		return "`" + name + "` is a CSS colour name, so its value is the same in every theme."
	}
	low := strings.ToLower(name)
	if strings.HasPrefix(low, "rgb(") || strings.HasPrefix(low, "rgba(") ||
		strings.HasPrefix(low, "hsl(") || strings.HasPrefix(low, "hsla(") {
		return "`" + name + "` is a CSS colour function. Its alpha is the fourth argument, " +
			"0..1 or a percentage."
	}
	if csscolor.IsGradient(name) {
		return "`" + name + "` is a CSS gradient. The stops may carry a position " +
			"(`0%`, `50%`, `100%`); a missing one is spread between its neighbours."
	}
	return "`" + name + "` is a custom colour: the app defines it with `ui.SetCustomColor`. " +
		"If nothing defines it, the runtime logs a warning and draws `onSurface`."
}

// namespaceDoc explains one of the two colour namespaces.
func namespaceDoc(id string) string {
	if id == "theme" {
		return "**theme**\n\nThe colour scheme: `theme.primary`, `theme.onSurface`. " +
			"A role follows the light/dark scheme, so one design serves both."
	}
	return "**customColor**\n\nA colour the app defined with `ui.SetCustomColor`. " +
		"The names live in Go, so completion cannot list them."
}

// colourRefDoc explains the second half of a colour reference.
func colourRefDoc(id, name string) string {
	if id != "theme" {
		return "**customColor." + name + "**\n\nA colour the app defined with " +
			"`ui.SetCustomColor(\"" + name + "\", …)`. If nothing defines it, the runtime " +
			"logs a warning and draws `onSurface`."
	}
	if _, ok := (m3.Colors{}).Role(name); !ok {
		hint := syntax.Suggest(name, m3.ColorRoles())
		return "**theme." + name + "** is not a theme colour." + hint
	}
	return "**theme." + name + "**\n\nA theme role: it follows the light/dark scheme."
}

// colourRoleItems lists the scheme's roles, prefixed so a member completion
// shows `theme.primary` as the whole reference.
func colourRoleItems(prefix string) []completionResult {
	out := make([]completionResult, 0, len(m3.ColorRoles()))
	for _, v := range m3.ColorRoles() {
		out = append(out, completionResult{
			Label: prefix + v, InsertText: prefix + v, Kind: kindColor,
			Detail: "theme colour",
			Documentation: &doc{Kind: "markdown", Value: "**" + prefix + v + "**\n\n" +
				"A theme role: it follows the light/dark scheme."},
		})
	}
	return out
}

// colourItems completes a colour position: the two references, the theme roles,
// the CSS names a design may write, then a gradient snippet.
func colourItems(elName string) []completionResult {
	out := make([]completionResult, 0, len(m3.ColorRoles())+16)
	out = append(out,
		completionResult{
			Label: "theme", InsertText: "theme.", Kind: kindVariable,
			Detail: "theme.<role> · follows the scheme",
			Documentation: &doc{Kind: "markdown", Value: "**theme.<role>**\n\n" +
				"The scheme's colour: `theme.primary`, `theme.surfaceContainerHigh`. " +
				"It follows the light/dark scheme."},
		},
		completionResult{
			Label: "customColor", InsertText: "customColor.", Kind: kindVariable,
			Detail: "customColor.<name> · app-defined",
			Documentation: &doc{Kind: "markdown", Value: "**customColor.<name>**\n\n" +
				"A colour the app defined with `ui.SetCustomColor`. The name is Go's, " +
				"so completion cannot list it."},
		},
	)
	for _, v := range m3.ColorRoles() {
		out = append(out, completionResult{
			Label: "theme." + v, InsertText: "theme." + v, Kind: kindColor,
			Detail: "colour role",
			Documentation: &doc{
				Kind: "markdown",
				Value: "Theme role: follows the scheme. A `#rrggbb` literal stays fixed, " +
					"and a name the app defines with `ui.SetCustomColor` follows the app.",
			},
		})
	}
	for _, v := range []string{"white", "black", "red", "green", "blue", "transparent"} {
		out = append(out, completionResult{
			Label: v, Kind: kindColor, Detail: "CSS colour name",
			Documentation: &doc{Kind: "markdown", Value: colourDoc(v)},
		})
	}
	out = append(out, completionResult{
		Label:      `"linear-gradient(…)"`,
		InsertText: `"linear-gradient(90deg, `,
		Kind:       kindColor, Detail: "CSS gradient",
		Documentation: &doc{Kind: "markdown", Value: "**linear-gradient(angle, stop, stop …)**\n\n" +
			"Fills the box along an angle: `90deg` points right, `0deg` up. Every colour position " +
			"accepts a gradient, so `background`, `borderColor` and a shadow colour all take one."},
	})
	out = append(out, completionResult{
		Label:      `"radial-gradient(…)"`,
		InsertText: `"radial-gradient(circle, `,
		Kind:       kindColor, Detail: "CSS gradient",
		Documentation: &doc{Kind: "markdown", Value: "**radial-gradient(size, stop, stop …)**\n\n" +
			"Fills the box out from its centre. `circle` and `closest-side` are accepted and the " +
			"element's own box is the circle."},
	})
	return out
}

// ---------------------------------------------------------------- formatting

// textEdit is one edit the client applies. goifmt rewrites the whole document,
// so the server answers with one edit covering every byte.
type textEdit struct {
	Range   rng    `json:"range"`
	NewText string `json:"newText"`
}

// formatting formats a whole document. A file that does not parse comes back
// untouched, which is what a save mid-edit needs.
func (s *Server) formatting(raw json.RawMessage) any {
	d := s.docFor(raw)
	if d == nil {
		return nil
	}
	out := goifmt.Source(d.text)
	if string(out) == string(d.text) {
		return []textEdit{}
	}
	idx := newLineIndex(d.text)
	return []textEdit{{
		Range:   idx.at(0, len(d.text)),
		NewText: string(out),
	}}
}

// ---------------------------------------------------------------- symbols

type symbol struct {
	Name           string   `json:"name"`
	Kind           int      `json:"kind"`
	Range          rng      `json:"range"`
	SelectionRange rng      `json:"selectionRange"`
	Children       []symbol `json:"children,omitempty"`
}

func (s *Server) symbols(raw json.RawMessage) any {
	d := s.docFor(raw)
	if d == nil {
		return nil
	}
	file, diag := syntax.Parse(d.text)
	if !diag.Empty() || file == nil || file.Root == nil {
		return []symbol{}
	}
	idx := newLineIndex(d.text)
	return []symbol{symbolOf(idx, file.Root, d.text)}
}

func symbolOf(idx lineIndex, e *syntax.Element, src []byte) symbol {
	sym := symbol{
		Name:           e.Name,
		Kind:           19, // Object: a design element, not a language symbol
		Range:          idx.at(int(e.NameAt), int(e.Close)+1),
		SelectionRange: idx.at(int(e.NameAt), int(e.NameAt)+len(e.Name)),
	}
	for _, kid := range e.Children() {
		sym.Children = append(sym.Children, symbolOf(idx, kid, src))
	}
	for _, m := range e.Members {
		if blk, ok := m.(*syntax.Block); ok {
			sym.Children = append(sym.Children, symbolOfBlock(idx, blk, src))
		}
	}
	return sym
}

// symbolOfBlock shows a control block in the outline. Its arms are not nodes, so
// they have no kind of their own; the block is named after its first condition.
func symbolOfBlock(idx lineIndex, blk *syntax.Block, src []byte) symbol {
	name := "if"
	switch {
	case blk.Kind == syntax.BlockFor:
		name = "for"
		if blk.Range != nil {
			name = "for " + sourceSpan(src, int(blk.RangeAt))
		}
	case blk.Kind == syntax.BlockSwitch:
		name = "switch"
		if blk.Subject != nil {
			name = "switch " + sourceSpan(src, int(blk.SubjectAt))
		}
	case len(blk.Branches) > 0 && blk.Branches[0].Cond != nil:
		name = "if " + sourceSpan(src, int(blk.Branches[0].Cond.Pos()))
	}
	sym := symbol{
		Name:           name,
		Kind:           19,
		Range:          idx.at(int(blk.At), int(blk.Close)+1),
		SelectionRange: idx.at(int(blk.At), int(blk.Close)+1),
	}
	for _, br := range blk.Branches {
		for _, m := range br.Members {
			switch x := m.(type) {
			case *syntax.Element:
				sym.Children = append(sym.Children, symbolOf(idx, x, src))
			case *syntax.Block:
				sym.Children = append(sym.Children, symbolOfBlock(idx, x, src))
			}
		}
	}
	return sym
}

// ---------------------------------------------------------------- folding

type foldingRange struct {
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Kind      string `json:"kind,omitempty"`
}

func (s *Server) foldingRanges(raw json.RawMessage) any {
	d := s.docFor(raw)
	if d == nil {
		return nil
	}
	file, diag := syntax.Parse(d.text)
	if !diag.Empty() || file == nil || file.Root == nil {
		return []foldingRange{}
	}
	idx := newLineIndex(d.text)
	var out []foldingRange
	var walk func(e *syntax.Element)
	walk = func(e *syntax.Element) {
		start := idx.position(int(e.Open)).Line
		end := idx.position(int(e.Close)).Line
		if end > start {
			out = append(out, foldingRange{StartLine: start, EndLine: end, Kind: "region"})
		}
		for _, kid := range e.Children() {
			walk(kid)
		}
		for _, m := range e.Members {
			if blk, ok := m.(*syntax.Block); ok {
				walkBlockFold(blk, idx, &out, walk)
			}
		}
	}
	walk(file.Root)
	return out
}

// walkBlockFold folds a block on its own and descends into its arms.
func walkBlockFold(blk *syntax.Block, idx lineIndex, out *[]foldingRange, walk func(*syntax.Element)) {
	start := idx.position(int(blk.At)).Line
	end := idx.position(int(blk.Close)).Line
	if end > start {
		*out = append(*out, foldingRange{StartLine: start, EndLine: end, Kind: "region"})
	}
	for _, br := range blk.Branches {
		for _, m := range br.Members {
			switch x := m.(type) {
			case *syntax.Element:
				walk(x)
			case *syntax.Block:
				walkBlockFold(x, idx, out, walk)
			}
		}
	}
}

// ---------------------------------------------------------------- definition

type location struct {
	URI   string `json:"uri"`
	Range rng    `json:"range"`
}

// definition jumps from an import path or alias to the fragment file.
func (s *Server) definition(raw json.RawMessage) any {
	d, pos, ok := s.atPos(raw)
	if !ok {
		return nil
	}
	file, diag := syntax.Parse(d.text)
	if !diag.Empty() || file == nil {
		return nil
	}
	off := newLineIndex(d.text).offset(pos)
	dir := filepath.Dir(d.path)
	for _, imp := range file.Imports {
		// The alias names the element that instantiates the fragment.
		if off >= int(imp.AliasAt) && off <= int(imp.AliasAt)+len(imp.Alias) {
			return []location{{URI: pathToURI(filepath.Join(dir, filepath.FromSlash(imp.Path)))}}
		}
		// The path itself: find the quoted token that follows `import`.
		if start, end, ok := importPathRange(d.text, int(imp.At)); ok && off >= start && off <= end {
			return []location{{URI: pathToURI(filepath.Join(dir, filepath.FromSlash(imp.Path)))}}
		}
	}
	// A reference jumps to the element that declares the id.
	if r := refAt(file, off); r != nil && off < int(r.At)+len(r.ID) {
		for _, id := range fileIDElements(file) {
			if id.name == r.ID {
				return []location{{URI: d.uri, Range: newLineIndex(d.text).at(id.at, id.at+len(id.name))}}
			}
		}
	}
	// A call site jumps to the fragment it instantiates.
	if _, _, el := elementNameAt(file, off); el != nil {
		for _, imp := range file.Imports {
			if imp.Alias == el.Name {
				return []location{{URI: pathToURI(filepath.Join(dir, filepath.FromSlash(imp.Path)))}}
			}
		}
	}
	return nil
}

// importPathRange returns the offsets of the quoted path after an `import`
// keyword at kw.
func importPathRange(src []byte, kw int) (start, end int, ok bool) {
	i := kw
	for i < len(src) && src[i] != '"' {
		if src[i] == '\n' {
			return 0, 0, false
		}
		i++
	}
	if i >= len(src) {
		return 0, 0, false
	}
	start = i
	for i++; i < len(src); i++ {
		if src[i] == '"' {
			return start, i + 1, true
		}
	}
	return 0, 0, false
}

// ---------------------------------------------------------------- AST helpers

// elementAt finds the innermost element containing off.
func elementAt(file *syntax.File, off int) *syntax.Element {
	if file == nil || file.Root == nil {
		return nil
	}
	var best *syntax.Element
	var walk func(e *syntax.Element)
	walk = func(e *syntax.Element) {
		if off >= int(e.NameAt) && off <= int(e.Close) {
			best = e
			for _, kid := range e.Children() {
				walk(kid)
			}
			for _, m := range e.Members {
				if blk, ok := m.(*syntax.Block); ok {
					walkBlockElements(blk, walk)
				}
			}
		}
	}
	walk(file.Root)
	return best
}

// sourceSpan reads a short expression from src up to the end of its line or the
// closing brace, for an outline label.
func sourceSpan(src []byte, from int) string {
	end := from
	for end < len(src) && src[end] != '}' && src[end] != '\n' {
		end++
	}
	return strings.TrimSpace(string(src[from:end]))
}

// walkBlockElements visits the elements inside a block's arms.
func walkBlockElements(blk *syntax.Block, walk func(*syntax.Element)) {
	for _, br := range blk.Branches {
		for _, m := range br.Members {
			switch x := m.(type) {
			case *syntax.Element:
				walk(x)
			case *syntax.Block:
				walkBlockElements(x, walk)
			}
		}
	}
}

// enclosingElementName is the element a completion request is inside. When the
// buffer does not parse yet (the usual case mid-typing) it falls back to a
// brace scan, so completion keeps working while a brace is missing.
func enclosingElementName(file *syntax.File, src []byte, off int) string {
	if el := elementAt(file, off); el != nil {
		return el.Name
	}
	return braceElementName(src, off)
}

// braceElementName reads the identifier before the `{` that encloses off.
func braceElementName(src []byte, off int) string {
	depth := 0
	for i := off - 1; i >= 0; i-- {
		switch src[i] {
		case '}':
			depth++
		case '{':
			if depth > 0 {
				depth--
				continue
			}
			j := i - 1
			for j >= 0 && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
				j--
			}
			end := j + 1
			for j >= 0 && isWordByte(src[j]) {
				j--
			}
			name := string(src[j+1 : end])
			if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
				return name
			}
			return ""
		}
	}
	return ""
}

// enclosingProperty reports the property whose value the cursor sits in. A
// responsive map key reports inMap instead.
func enclosingProperty(file *syntax.File, src []byte, off int) (string, bool) {
	el := elementAt(file, off)
	// Mid-typing the AST drops the broken property, so the line is the more
	// reliable witness when it names a property this element really has. A
	// fragment instance is not in the schema, so for it the line is the only
	// witness: every `name:` there is a prop or a root override.
	if name, inMap := braceProperty(src, off); inMap || isElementMember(el, name) ||
		(name != "" && el != nil && !isSchemaElement(el.Name)) {
		return name, inMap
	}
	if el != nil {
		props := el.Props()
		for i, p := range props {
			start, end := valueExtent(src, p)
			// A value ends where the next property begins, or two properties
			// on one line would both own the cursor.
			if i+1 < len(props) && int(props[i+1].NameAt) < end {
				end = int(props[i+1].NameAt)
			}
			if off < start || off > end {
				continue
			}
			switch v := p.Value.(type) {
			case *syntax.Map:
				// Inside the braces of a map the keys are breakpoints.
				return "", mapKeyPosition(src, start, off)
			case *syntax.Modifier:
				if v.Form == syntax.ModifierResponsive {
					return "", mapKeyPosition(src, start, off)
				}
			}
			return p.Name, false
		}
	}
	return "", false
}

// isSchemaElement reports whether the element name is a core element.
func isSchemaElement(name string) bool {
	_, ok := schema.Lookup(name)
	return ok
}

// isElementMember reports whether name is a property or event the element
// accepts, which is what makes a line-based guess trustworthy.
func isElementMember(el *syntax.Element, name string) bool {
	if el == nil || name == "" {
		return false
	}
	def, ok := schema.Lookup(el.Name)
	if !ok {
		return false
	}
	return def.Prop(name) != nil || def.HasEvent(name)
}

// mapKeyPosition reports whether off sits in the key half of a map entry: past
// the opening brace or a comma, with no colon yet.
func mapKeyPosition(src []byte, open, off int) bool {
	depth := 0
	sawColon := false
	inString := false
	for i := open; i < off && i < len(src); i++ {
		c := src[i]
		if inString {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 1 {
				sawColon = false
			}
		case ':':
			if depth == 1 {
				sawColon = true
			}
		}
	}
	return !sawColon
}

// braceProperty is the fallback for an unparsable buffer: the property name on
// the current line, if the cursor is after its colon.
func braceProperty(src []byte, off int) (string, bool) {
	line := off
	for line > 0 && src[line-1] != '\n' {
		line--
	}
	colon := -1
	for i := line; i < off && i < len(src); i++ {
		if src[i] == ':' {
			colon = i
		}
	}
	if colon < 0 {
		return "", false
	}
	j := colon - 1
	for j >= line && (src[j] == ' ' || src[j] == '\t') {
		j--
	}
	end := j + 1
	for j >= line && isWordByte(src[j]) {
		j--
	}
	name := string(src[j+1 : end])
	if name == "" {
		return "", false
	}
	// A value that opens a brace is a responsive map, and the responsive
	// modifier spells the same keys after a word: both offer breakpoints.
	j = colon + 1
	for j < off && j < len(src) && (src[j] == ' ' || src[j] == '\t') {
		j++
	}
	if j < len(src) && src[j] == '{' {
		return "", mapKeyPosition(src, j, off)
	}
	if j+len("responsive") <= len(src) && string(src[j:j+len("responsive")]) == "responsive" {
		for j < off && j < len(src) && src[j] != '{' {
			j++
		}
		return "", mapKeyPosition(src, j, off)
	}
	return name, false
}

// valueExtent returns the byte span of a property's value.
func valueExtent(src []byte, p *syntax.Property) (int, int) {
	start := int(p.Value.Pos())
	end := valueEnd(src, start)
	return start, end
}

// valueEnd walks to the end of a value: the next newline that closes the value
// or the brace that closes the element.
func valueEnd(src []byte, start int) int {
	depth := 0
	inString := false
	for i := start; i < len(src); i++ {
		c := src[i]
		if inString {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return i
			}
			depth--
		case '\n':
			if depth == 0 {
				return i
			}
		case ';':
			if depth == 0 {
				return i
			}
		}
	}
	return len(src)
}

// elementNameAt returns the element name under off.
func elementNameAt(file *syntax.File, off int) (int, int, *syntax.Element) {
	if el := elementAt(file, off); el != nil {
		start, end := int(el.NameAt), int(el.NameAt)+len(el.Name)
		if off >= start && off <= end {
			return start, end, el
		}
	}
	return 0, 0, nil
}

// propertyAt returns the property name under off.
func propertyAt(file *syntax.File, off int) (int, int, *syntax.Element, *schema.Prop) {
	if el := elementAt(file, off); el != nil {
		elDef, ok := schema.Lookup(el.Name)
		for _, p := range el.Props() {
			start, end := int(p.NameAt), int(p.NameAt)+len(p.Name)
			if off < start || off > end {
				continue
			}
			if !ok {
				return start, end, el, nil
			}
			if pd := elDef.Prop(p.Name); pd != nil {
				return start, end, el, pd
			}
			return start, end, el, nil
		}
	}
	return 0, 0, nil, nil
}

// valueAt returns the identifier value under off.
func valueAt(file *syntax.File, off int) (int, int, *schema.Prop, string) {
	el := elementAt(file, off)
	if el == nil {
		return 0, 0, nil, ""
	}
	elDef, ok := schema.Lookup(el.Name)
	if !ok {
		return 0, 0, nil, ""
	}
	for _, p := range el.Props() {
		// A bare word (a role) and a quoted string (a CSS colour, a gradient)
		// are both values a reader points at.
		var start, end int
		var val string
		switch v := p.Value.(type) {
		case *syntax.Ident:
			start, end, val = int(v.At), int(v.At)+len(v.Val), v.Val
		case *syntax.String:
			// The lexer records the opening quote, so the value starts after it.
			start, end, val = int(v.At), int(v.At)+len(v.Val)+2, v.Val
		default:
			continue
		}
		if off < start || off > end {
			continue
		}
		if pd := elDef.Prop(p.Name); pd != nil {
			return start, end, pd, val
		}
	}
	return 0, 0, nil, ""
}

// idElement pairs a declared id with the kind of element that carries it,
// so an expression `abc.width` can complete from the schema alone.
type idElement struct {
	name string
	kind string
	// at is the byte offset of the id's value, for go-to-definition.
	at int
}

// fileIDElements lists the ids a design declares, with their element kind,
// for `id:` values and for `abc.property` references.
func fileIDElements(file *syntax.File) []idElement {
	var out []idElement
	if file == nil || file.Root == nil {
		return nil
	}
	syntax.WalkElements(file.Root, func(e *syntax.Element) {
		if p := e.Prop("id"); p != nil {
			switch v := p.Value.(type) {
			case *syntax.Ident:
				out = append(out, idElement{name: v.Val, kind: e.Name, at: int(v.At)})
			case *syntax.String:
				out = append(out, idElement{name: v.Val, kind: e.Name, at: int(v.At) + 1})
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// idElementAt finds the id ref names in the file, for `abc.` completion.
func idElementAt(file *syntax.File, ref string) (string, bool) {
	for _, id := range fileIDElements(file) {
		if id.name == ref {
			return id.kind, true
		}
	}
	return "", false
}

// refAt finds the reference under off, for hover and go-to-definition.
func refAt(file *syntax.File, off int) *syntax.Ref {
	var found *syntax.Ref
	var val func(syntax.Value)
	val = func(v syntax.Value) {
		switch x := v.(type) {
		case *syntax.Ref:
			start := int(x.At)
			end := start + len(x.ID) + 1 + len(x.Prop)
			if off >= start && off <= end && found == nil {
				found = x
			}
		case *syntax.Call:
			// The callee is a ref too: `spin.pause` is what hover explains.
			if x.Callee != nil {
				start := int(x.Callee.At)
				end := start + len(x.Callee.ID) + 1 + len(x.Callee.Prop)
				if off >= start && off <= end && found == nil {
					found = x.Callee
				}
			}
			for _, a := range x.Args {
				val(a)
			}
		case *syntax.Map:
			for i := range x.Entries {
				val(x.Entries[i].Value)
			}
		case *syntax.Binary:
			val(x.Left)
			val(x.Right)
		case *syntax.Unary:
			val(x.X)
		case *syntax.Ternary:
			val(x.Cond)
			val(x.Then)
			val(x.Else)
		}
	}
	var members func([]syntax.Member)
	members = func(ms []syntax.Member) {
		for _, m := range ms {
			switch x := m.(type) {
			case *syntax.Property:
				val(x.Value)
			case *syntax.Element:
				members(x.Members)
			case *syntax.Block:
				for i := range x.Branches {
					if x.Branches[i].Cond != nil {
						val(x.Branches[i].Cond)
					}
					members(x.Branches[i].Members)
				}
			}
		}
	}
	if file != nil && file.Root != nil {
		members(file.Root.Members)
	}
	return found
}

// loopRefDoc explains `row` or `row.title` when row is a loop variable.
func loopRefDoc(file *syntax.File, src []byte, off int, r *syntax.Ref) (doc, bool) {
	sh := loopShapeAt(file, src, off, r.ID)
	if sh == nil {
		return doc{}, false
	}
	for _, f := range sh.Fields {
		if fieldMatchesName(f.Name, r.Prop) {
			return doc{Kind: "markdown", Value: "**" + r.ID + "." + r.Prop + "**\n\n" + declDoc(f, "row")}, true
		}
	}
	return doc{Kind: "markdown", Value: "**" + r.ID + "** — loop variable\n\nHolds one `" + sh.Name + "`: " + fieldList(sh)}, true
}

// fieldMatchesName compares a declared field name with the one a reference
// reads, ignoring the first letter's case.
func fieldMatchesName(got, want string) bool {
	return got != "" && strings.EqualFold(got, want)
}

// loopVarAt returns the bare loop variable under off, if there is one.
func loopVarAt(file *syntax.File, off int) (int, int, string) {
	var out struct {
		start, end int
		name       string
	}
	syntax.WalkElements(file.Root, func(e *syntax.Element) {
		for _, m := range e.Members {
			blk, ok := m.(*syntax.Block)
			if !ok {
				continue
			}
			walkBlocks(blk, func(b *syntax.Block) {
				if b.Kind != syntax.BlockFor {
					return
				}
				for _, v := range []struct {
					name string
					at   syntax.Pos
				}{{b.LoopKey(), b.KeyAt}, {b.LoopVal(), b.ValAt}} {
					if v.name == "" {
						continue
					}
					start, end := int(v.at), int(v.at)+len(v.name)
					if off >= start && off <= end {
						out.start, out.end, out.name = start, end, v.name
					}
				}
			})
		}
	})
	return out.start, out.end, out.name
}

// walkBlocks visits a block and every block nested in its arms.
func walkBlocks(blk *syntax.Block, fn func(*syntax.Block)) {
	fn(blk)
	for _, br := range blk.Branches {
		for _, m := range br.Members {
			switch x := m.(type) {
			case *syntax.Block:
				walkBlocks(x, fn)
			case *syntax.Element:
				for _, sm := range x.Members {
					if nested, ok := sm.(*syntax.Block); ok {
						walkBlocks(nested, fn)
					}
				}
			}
		}
	}
}

// loopVarDoc explains a loop variable, and the shape it holds when the loop
// walks a list of a declared type.
func loopVarDoc(file *syntax.File, src []byte, off int, name string) string {
	var blk *syntax.Block
	syntax.WalkElements(file.Root, func(e *syntax.Element) {
		for _, m := range e.Members {
			if b, ok := m.(*syntax.Block); ok {
				walkBlocks(b, func(x *syntax.Block) {
					if x.Kind == syntax.BlockFor && off > int(x.At) && off <= int(x.Close) &&
						(x.LoopKey() == name || x.LoopVal() == name) {
						blk = x
					}
				})
			}
		}
	})
	if blk == nil {
		return "**" + name + "**"
	}
	value := "**" + name + "** — loop variable\n\nFrom `for … := range " + sourceSpan(src, int(blk.RangeAt)) + "`."
	if sh := rangeElemShape(file, blk); sh != nil {
		value += "\n\nEach iteration holds one `" + sh.Name + "`: " + fieldList(sh)
	} else if blk.LoopKey() == name && blk.LoopVal() == "" {
		value += "\n\nThe index, or the key of a map."
	}
	return value
}

// fieldList writes a shape's fields as `Name type, …` for a hover line.
func fieldList(sh *syntax.DataShape) string {
	parts := make([]string, len(sh.Fields))
	for i, f := range sh.Fields {
		parts[i] = f.Name + " " + f.Type.String()
	}
	return strings.Join(parts, ", ")
}

// declaredPropDoc explains one `props` declaration.
func declaredPropDoc(f syntax.Field) string {
	return "**props." + f.Name + "**\n\n" + declDoc(f, "props")
}

func elementDoc(el *schema.Element) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s**", el.Name)
	if el.Children {
		b.WriteString(" — container")
	} else {
		b.WriteString(" — leaf")
	}
	if len(el.Attrs) > 0 {
		names := make([]string, 0, len(el.Attrs))
		for _, p := range el.Attrs {
			names = append(names, p.Name)
		}
		fmt.Fprintf(&b, "\n\nProperties: %s", strings.Join(names, ", "))
	}
	return b.String()
}

func propDetail(el string, p schema.Prop) string {
	s := fmt.Sprintf("%s · %s", el, p.Type)
	if len(p.Enum) > 0 {
		s += " (" + strings.Join(p.Enum, " | ") + ")"
	}
	if p.Default != nil {
		s += fmt.Sprintf(" · default %v", p.Default)
	}
	return s
}

// isNaN reports the unset value of a numeric property such as Slider.center,
// which would otherwise print as a bare NaN default.
func isNaN(v any) bool {
	f, ok := v.(float64)
	return ok && math.IsNaN(f)
}

func propDoc(el string, p schema.Prop) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** on `%s`\n\nType: `%s`", p.Name, el, p.Type)
	if len(p.Enum) > 0 {
		fmt.Fprintf(&b, "\n\nValues: %s", codeList(p.Enum))
	}
	if p.Default != nil && !isNaN(p.Default) {
		fmt.Fprintf(&b, "\n\nDefault: `%v`", p.Default)
	}
	if p.Common {
		b.WriteString("\n\nAccepted by every element.")
	}
	if p.Type == schema.TypeColor {
		b.WriteString("\n\nA theme role (`theme.primary`, `theme.onSurface`), an app colour " +
			"(`customColor.brand`, defined with `ui.SetCustomColor`), or CSS colour syntax: " +
			"`#rrggbb`, any of the 148 CSS names (`white`, `rebeccapurple`), " +
			"`rgb()`/`rgba()` and `hsl()`/`hsla()`. " +
			"Quote a function or a gradient: `background: \"linear-gradient(90deg, red, blue)\")`.")
	}
	if p.Name == "src" {
		b.WriteString("\n\n`goi://path` reads the design's asset container, `./path` or an absolute " +
			"path reads this machine, and `http(s)://` is fetched in the background.")
		switch el {
		case "Image":
			b.WriteString(" `SetImage` from Go skips the file entirely. An animated GIF or " +
				"APNG plays on its own; `paused` stops it.")
		case "SVG":
			b.WriteString(" `currentColor` in the file takes the element's `color`, so one icon " +
				"follows the theme; `SetDataString`/`SetDataBytes` supply the document from Go. " +
				"A SMIL `<animate>` or `<animateTransform>` plays on its own; `paused` stops it.")
		}
	}
	switch p.Name {
	case "opacity":
		b.WriteString("\n\n0 to 1, or a percentage such as `50%`. Fades the element and " +
			"everything under it. From Go: `node.SetOpacity(0.5)`, which a `Tween` can drive.")
	case "hideScrollbar":
		b.WriteString("\n\nRemoves the scrollbar of a scrolling element. The gesture still scrolls.")
	case "autoHideScrollbar":
		b.WriteString("\n\n`true` (default) shows the bar only while the pointer is over the " +
			"area or it scrolled in the last two seconds. `false` keeps it up.")
	case "scrollbarWidth":
		b.WriteString("\n\nThickness of the scrollbar in dp, 4 by default. The thumb keeps its " +
			"rounded pill shape at any width.")
	case "sortable":
		b.WriteString("\n\n`true` makes a table header clickable: it gets a pointer cursor and an " +
			"arrow that follows the direction.")
	case "sortColumn":
		b.WriteString("\n\nThe sorted column index, `-1` (default) for declaration order. A " +
			"header click sets it; `Table.Sort` and `SortBy` set it from Go.")
	case "sortAscending":
		b.WriteString("\n\nSort direction, `true` by default. Reversing the same header flips it.")
	case "selectable":
		b.WriteString("\n\nHow rows are selected: `none` (default) makes cells inert, " +
			"`single` holds one row, `multiple` adds or removes a row per click. " +
			"Clicking a selected row deselects it.")
	case "selectedRow":
		b.WriteString("\n\nThe selected row index, `-1` by default. It counts rows as they are " +
			"shown, so it stays meaningful after sorting. In `multiple` mode use " +
			"`Table.SelectedRows` and the rows' own `selected` flags.")
	case "striped":
		b.WriteString("\n\nTints every second row; off by default.")
	case "showHeader":
		b.WriteString("\n\n`true` by default. `false` hides the header band and starts the rows " +
			"at the table's top edge.")
	case "rowHeight":
		b.WriteString("\n\nRow height in dp; 48 by default. A row's own `height` wins.")
	case "headerHeight":
		b.WriteString("\n\nHeader height in dp; 40 by default.")
	case "theme":
		b.WriteString("\n\n`light` and `dark` are built in, and `app.systemMode` follows the " +
			"desktop. A scheme the app defines with `Window.DefineTheme` is selected by its " +
			"`Theme.Name`; `ui.NewThemeFromSeed(color, dark)` names its result " +
			"`lightFromSeed` or `darkFromSeed`. A name defined after `LoadWindow` still " +
			"applies on the next frame.")
	case "rotate":
		b.WriteString("\n\nCSS `rotate`: `none`, an angle (`90deg`, `0.25turn`, `1.57rad`), " +
			"an axis plus angle (`x 90deg`, `y 0.25turn`, `z 1.57rad`) or a vector plus " +
			"angle (`1 1 1 90deg`). It turns the element and its subtree about its own " +
			"centre; layout ignores it. The renderer is two-dimensional, so only a z-axis " +
			"turn shows.")
	case "paused":
		b.WriteString("\n\nHolds an animation where it is: a GIF or APNG frame, or an SVG's " +
			"SMIL clock. A design can start it paused with `paused: true`; from Go, " +
			"`Paused.Set(false)` resumes it, and the node's `play()`, `pause()` and " +
			"`stop()` do the same from an event: `onClick: spin.pause()`.")
	case "loop":
		b.WriteString("\n\n`true` (default) loops an animation forever. `false` plays one pass " +
			"and holds the last frame, whatever loop count the file declares.")
	case "center":
		b.WriteString("\n\nThe value a `centered` slider fills from. Left out, it is the " +
			"midpoint of `min` and `max`, so the fill marks above or below the middle.")
	case "end":
		b.WriteString("\n\nThe high value of a `range` slider; `value` is the low one. " +
			"From Go: `SetRange(lo, hi)` and `OnRange`.")
	case "text":
		if el == "Text" {
			b.WriteString("\n\nEmoji draw in colour from the embedded Noto Color Emoji font, " +
				"at the size of the run, so a line can mix text and emoji.")
		}
	case "numeric":
		b.WriteString("\n\nRight-aligns the column by default and sorts by the number inside " +
			"each cell's `value`, so `1,200` orders as 1200.")
	case "value":
		if el == "TableCell" {
			b.WriteString("\n\nThe sort key for this cell, which may differ from what the cell " +
				"draws: `value: \"2026-11-08\"` with a `Text` showing \"8 Nov 2026\". " +
				"A string, number or bool all work, and two numbers compare as numbers.")
		}
	case "key":
		b.WriteString("\n\nNames a table column for Go: `table.SortBy(\"age\", false)` needs no " +
			"index.")
	case "cursor":
		b.WriteString("\n\nThe pointer shape over this element, named the way CSS names it: " +
			"`pointer`, `text`, `move`, `not-allowed`, `grab`, the resize arrows, " +
			"`wait`, `crosshair`, `none` and every other CSS cursor value. A child " +
			"inherits it; `cursor: auto` asks for the framework's own shape again. " +
			"From Go: `node.SetCursor(\"grab\")`.")
	case "zIndex":
		b.WriteString("\n\nPaint order among siblings, like CSS: a higher value paints later, " +
			"so it lands on top, and equal values keep document order. Layout ignores " +
			"it, so a design can restack without moving anything. Negative values go " +
			"below siblings with the default 0. From Go: `node.SetZIndex(2)`.")
	case "scrollbarColor":
		b.WriteString("\n\nColour of the bar; defaults to the scheme's onSurfaceVariant at " +
			"40%. A hex literal is used as written, alpha included.")
	case "scrollShadow":
		b.WriteString("\n\nDraws a gradient at each edge the content has scrolled away from, " +
			"so it marks hidden content the way CSS scroll shadows do. Tint it with " +
			"`scrollShadowColor`.")
	case "scrollShadowColor":
		b.WriteString("\n\nColour of the `scrollShadow` gradient; defaults to the scheme's " +
			"shadow colour.")
	case "disableResize":
		b.WriteString("\n\n`true` turns off the user's resize and the title bar's maximize button. " +
			"`resizable` is the same switch read the other way round. `fullscreen` is a " +
			"separate switch and still works. Reactive: a `Set` from Go reaches the " +
			"platform on the next frame.")
	case "resizable":
		b.WriteString("\n\n`false` turns off the user's resize and the title bar's maximize " +
			"button. `disableResize: true` is the older spelling of the same thing. " +
			"Reactive from Go with `SetResizable`.")
	case "minimizable":
		b.WriteString("\n\n`false` makes the framework refuse minimize requests, so a " +
			"custom title bar can hide its button. SDL has no flag for the platform's " +
			"own title bar button. Reactive from Go with `SetMinimizable`.")
	case "maximizable":
		b.WriteString("\n\n`false` makes the framework refuse maximize requests, and restores " +
			"a window that is already maximized. Reactive from Go with `SetMaximizable`.")
	case "minimized":
		b.WriteString("\n\na request: `true` asks the window to minimize and `false` to " +
			"restore. Read the live state as `window.minimized` or with `Window.Minimized`, " +
			"which follow the user's title bar and task bar actions too.")
	case "maximized":
		b.WriteString("\n\na request: `true` asks the window to maximize and `false` to " +
			"restore. Read the live state as `window.maximized` or with `Window.Maximized`, " +
			"which follow the user's title bar actions too.")
	case "fullscreen":
		b.WriteString("\n\n`true` fills the display. A request when written; read the live " +
			"state as `window.fullscreen` or with `Window.Fullscreen`. Reactive from Go, " +
			"with no reload.")
	case "minWidth", "minHeight":
		b.WriteString("\n\nThe smallest the element may lay out, and on `Window` the smallest the user " +
			"may drag the native window.")
	case "maxWidth", "maxHeight":
		b.WriteString("\n\nThe largest the element may lay out, and on `Window` the largest the user " +
			"may drag the native window. `-1` means no limit.")
	case "shadow":
		where := "the element's box, like CSS `box-shadow`"
		if el == "Text" {
			where = "the glyphs, like CSS `text-shadow`"
		}
		b.WriteString("\n\nShadows " + where + ":\n\n" +
			"`x y [blur [spread]] [color] [inset]`, and several comma separated. " +
			"`x y` is the offset, `blur` softens the edge, `spread` grows the shape " +
			"(box only), and `inset` paints inside it. A colour is a `#rrggbb` " +
			"literal, a theme role, or left out for the scheme's shadow. " +
			"`shadow: none` removes it.")
	}
	if p.Name == "template" {
		b.WriteString("\n\nMarks a hidden row prototype: it is hidden when the tree is built, " +
			"takes no space, and `ui.Fill` builds the rows from it. Copies made from it " +
			"are ordinary rows, so a second `Fill` starts from the same prototype.")
	}
	return b.String()
}

func codeList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "`" + v + "`"
	}
	return strings.Join(quoted, ", ")
}

// ---------------------------------------------------------------- fragments

// fragmentAt reports the import whose alias is the element the cursor sits in,
// so a fragment instance body completes with that fragment's props.
func (s *Server) fragmentAt(d *document, file *syntax.File, src []byte, off int) (syntax.Import, bool) {
	if file == nil {
		return syntax.Import{}, false
	}
	name := enclosingElementName(file, src, off)
	if name == "" {
		return syntax.Import{}, false
	}
	for _, imp := range file.Imports {
		if imp.Alias == name {
			return imp, true
		}
	}
	return syntax.Import{}, false
}

// importFile parses the file an import points at: the editor's buffer when it
// has one open, the file on disk otherwise. A broken or missing fragment gives
// nil, and completion falls back to the schema only.
func (s *Server) importFile(d *document, imp syntax.Import) *syntax.File {
	target := filepath.Clean(filepath.Join(filepath.Dir(d.path), filepath.FromSlash(imp.Path)))
	if open, ok := s.docs[pathToURI(target)]; ok {
		f, diag := syntax.Parse(open.text)
		if diag.Empty() {
			return f
		}
		return nil
	}
	src, err := os.ReadFile(target)
	if err != nil {
		return nil
	}
	f, diag := syntax.ParseNamed(src, target)
	if !diag.Empty() {
		return nil
	}
	return f
}

// fragmentMemberItems lists what an instance can write inside a fragment: the
// props the fragment declares, the properties its root lets the instance
// override, and the slots it fills.
func (s *Server) fragmentMemberItems(d *document, imp syntax.Import) []completionResult {
	frag := s.importFile(d, imp)
	if frag == nil || frag.Root == nil {
		return nil
	}
	out := make([]completionResult, 0, len(frag.Props)+8)
	for _, pr := range frag.Props {
		out = append(out, completionResult{
			Label:         pr.Name,
			Kind:          kindProperty,
			Detail:        imp.Alias + "." + pr.Name + " · " + pr.Type.String(),
			Documentation: &doc{Kind: "markdown", Value: "**" + pr.Name + "** on `" + imp.Alias + "`\n\n" + declDoc(pr, "props")},
		})
	}
	// An instance property the fragment does not declare replaces the root's
	// own value, so those names work here too.
	if def, ok := schema.Lookup(frag.Root.Name); ok {
		for _, p := range def.Attrs {
			out = append(out, completionResult{
				Label:         p.Name,
				Kind:          kindProperty,
				Detail:        propDetail(frag.Root.Name+" root", p),
				Documentation: &doc{Kind: "markdown", Value: propDoc(frag.Root.Name, p)},
			})
		}
	}
	for _, slot := range fragmentSlots(frag) {
		out = append(out, completionResult{
			Label:         slot,
			Kind:          kindKeyword,
			Detail:        "slot",
			Documentation: &doc{Kind: "markdown", Value: "**" + slot + " { … }**\n\nElements written here appear where `" + imp.Alias + "` writes `" + slot + "`."},
		})
	}
	return out
}

// fragmentProp resolves the prop a fragment instance property names, so the
// value after it completes like the declared type.
func (s *Server) fragmentProp(d *document, file *syntax.File, src []byte, off int, prop string) *syntax.Prop {
	imp, ok := s.fragmentAt(d, file, src, off)
	if !ok {
		return nil
	}
	frag := s.importFile(d, imp)
	if frag == nil {
		return nil
	}
	return frag.Prop(prop)
}

// fragmentValueItems offers the values a fragment-instance property accepts: the
// declared type's own values when the prop exists, plus what any value position
// has.
func fragmentValueItems(file *syntax.File, prop string, frag *syntax.Prop, prefix string) []completionResult {
	var out []completionResult
	if frag != nil {
		switch frag.Type.Kind {
		case syntax.TypeFunc:
			for _, f := range file.Vars {
				if f.Type.Kind == syntax.TypeFunc {
					out = append(out, handlerCallItem("bridge", f.Name, f.Type.String()))
				}
			}
			return out
		case syntax.TypeSlice, syntax.TypeMap:
			for _, l := range listVars(file) {
				out = append(out, completionResult{Label: l, Kind: kindVariable, Detail: "list · " + frag.Type.String()})
			}
			return out
		case syntax.TypeScalar:
			switch typeNameOf(frag.Type.Name) {
			case "bool":
				return []completionResult{
					{Label: "true", Kind: kindKeyword, Detail: "bool"},
					{Label: "false", Kind: kindKeyword, Detail: "bool"},
				}
			case "color":
				return append(out, colourItems("")...)
			}
		}
	}
	for _, f := range file.Vars {
		out = append(out, completionResult{
			Label:         "bridge." + f.Name,
			InsertText:    "bridge." + f.Name,
			Kind:          kindVariable,
			Detail:        "bridge." + f.Name + " · " + f.Type.String(),
			Documentation: &doc{Kind: "markdown", Value: "**bridge." + f.Name + "**\n\n" + declDoc(f, "bridge")},
		})
	}
	if frag == nil || frag.Type.Kind == syntax.TypeScalar {
		out = append(out, builtinItems()...)
	}
	for _, id := range fileIDElements(file) {
		out = append(out, completionResult{Label: id.name, Kind: kindVariable, Detail: "id · " + id.kind})
	}
	return out
}

// fragmentSlots lists the slots a fragment takes, as the call writes them.
func fragmentSlots(frag *syntax.File) []string {
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		if strings.HasPrefix(name, "$") && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	if frag == nil || frag.Root == nil {
		return nil
	}
	syntax.WalkElements(frag.Root, func(e *syntax.Element) {
		for _, kid := range e.Children() {
			add(kid.Name)
		}
	})
	sort.Strings(out)
	return out
}

// handlerCallItem is the completion item for calling a bridge handler.
func handlerCallItem(ns, name, sig string) completionResult {
	detail := "handler call"
	if sig != "" {
		detail += " · " + sig
	}
	return completionResult{
		Label:         ns + "." + name + "(…)",
		InsertText:    ns + "." + name + "(",
		Kind:          kindFunction,
		Detail:        detail,
		Documentation: &doc{Kind: "markdown", Value: "**" + ns + "." + name + "**\n\nHandler call; its arguments are evaluated when the event fires."},
	}
}

// fragmentPropertyAt returns the property of a fragment instance under off and
// the prop the fragment declares for it.
func (s *Server) fragmentPropertyAt(d *document, file *syntax.File, off int) (int, int, *syntax.Prop, bool) {
	el := elementAt(file, off)
	if el == nil {
		return 0, 0, nil, false
	}
	imp, ok := s.fragmentAt(d, file, d.text, off)
	if !ok || imp.Alias != el.Name {
		return 0, 0, nil, false
	}
	for _, p := range el.Props() {
		start, end := int(p.NameAt), int(p.NameAt)+len(p.Name)
		if off < start || off > end {
			continue
		}
		frag := s.importFile(d, imp)
		if frag == nil {
			return 0, 0, nil, false
		}
		pr := frag.Prop(p.Name)
		if pr == nil {
			return 0, 0, nil, false
		}
		return start, end, pr, true
	}
	return 0, 0, nil, false
}

// fragmentAliasAt names the fragment instance the cursor is inside.
func fragmentAliasAt(file *syntax.File, off int) string {
	if el := elementAt(file, off); el != nil {
		return el.Name
	}
	return ""
}

// fragmentDoc explains a fragment alias: what the instance can pass and fill.
func fragmentDoc(imp syntax.Import, frag *syntax.File) string {
	var b strings.Builder
	b.WriteString("**" + imp.Alias + "** — fragment\n\nImported from `" + imp.Path + "`.")
	if len(frag.Props) > 0 {
		b.WriteString("\n\nProps: ")
		for i, pr := range frag.Props {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("`" + pr.Name + " " + pr.Type.String() + "`")
		}
	}
	if slots := fragmentSlots(frag); len(slots) > 0 {
		b.WriteString("\n\nSlots: " + strings.Join(slots, ", "))
	}
	return b.String()
}

// ---------------------------------------------------------------- small utils

func isWordByte(c byte) bool {
	return c == '_' || c == '$' || c == '#' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func lastIndexByte(b []byte, c byte) int {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == c {
			return i
		}
	}
	return -1
}
