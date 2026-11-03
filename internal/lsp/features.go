package lsp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ksckaan1/goi/internal/asset"
	"github.com/ksckaan1/goi/internal/highlight"
	"github.com/ksckaan1/goi/internal/m3"
	"github.com/ksckaan1/goi/internal/schema"
	"github.com/ksckaan1/goi/internal/syntax"
	"github.com/ksckaan1/goi/internal/text"
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
	var vdiag syntax.Diagnostics
	if bundle.File.IsFragment() {
		_, vdiag = schema.Validate(bundle.File)
	} else {
		_, vdiag = schema.ValidateEntry(bundle.File)
	}
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

	// A responsive map key, the value of a property, or a member of one of the
	// reserved global namespaces (`app.`, `window.`).
	switch {
	case inMap:
		items = append(items, breakpointItems()...)
	case prop != "":
		values, cut := valueItems(file, prop, d.text, off, prefix, insideString(d.text, off))
		items = append(items, values...)
		incomplete = cut
	}
	if ref := enclosingRef(before); ref != "" {
		items = append(items, refItems(file, d.text, off, ref)...)
	}

	// Member position: the line so far is only whitespace, or we are right
	// after an opening brace.
	trimmed := strings.TrimSpace(string(line))
	switch {
	case prop == "" && !inMap && strings.HasPrefix(prefix, "{"):
		// A block tag: `{#if`, `{:else}`, `{/if}`. The prefix word includes the
		// brace because isWordByte does not, so it is cut to the tag itself.
		items = append(items, blockItemsFor(openBlockAt(file, off) != nil, listVars(file))...)
	case prop == "" && !inMap && strings.HasPrefix(prefix, "$"):
		// `$` starts a slot here, not a param: a param is a value.
		items = append(items, slotItems(file, d.text, off)...)
	case prop == "" && !inMap && (trimmed == "" || strings.HasSuffix(trimmed, "{")):
		items = append(items, memberItems(file, d.text, off)...)
	case prop == "" && !inMap:
		items = append(items, elementItems(file)...)
	}
	return completionList{Items: dedupe(items, prefix), IsIncomplete: incomplete}
}

// blockItems offers the block tags. Only `{#if}` exists, so the list is short:
// a fresh block at member level, and the two tags that continue one already
// open at the cursor.
func blockItems(inside bool) []completionResult {
	return blockItemsFor(inside, nil)
}

// blockItemsFor adds the design's list variables to the {#each} snippet, so the
// tag completes into something that already names a real list.
func blockItemsFor(inside bool, lists []string) []completionResult {
	list := "rows"
	if len(lists) > 0 {
		list = lists[0]
	}
	out := []completionResult{{
		Label:  "{#if}",
		Kind:   kindKeyword,
		Detail: "block",
		Documentation: &doc{Kind: "markdown", Value: "**{#if cond} … {/if}**\n\n" +
			"Shows the first arm whose condition holds. The arms are ordinary children " +
			"of the enclosing element, so a block adds no box of its own."},
	}}
	out = append(out, completionResult{
		Label:  "{#each}",
		Kind:   kindKeyword,
		Detail: "block",
		Documentation: &doc{Kind: "markdown", Value: "**{#each data." + list + " as row} … {/each}**\n\n" +
			"Repeats its body once per item of a list variable. Rows are rebuilt when " +
			"`ui.SetData` changes the list."},
	})
	if !inside {
		return out
	}
	return append(out,
		completionResult{
			Label: "{:else}", Kind: kindKeyword, Detail: "block arm",
			Documentation: &doc{Kind: "markdown", Value: "**{:else}** — the arm shown when no earlier condition holds."},
		},
		completionResult{
			Label: "{:else if}", Kind: kindKeyword, Detail: "block arm",
			Documentation: &doc{Kind: "markdown", Value: "**{:else if cond}** — another condition, tried in order."},
		},
		completionResult{
			Label: "{/if}", Kind: kindKeyword, Detail: "block end",
			Documentation: &doc{Kind: "markdown", Value: "**{/if}** — closes the `{#if}` block."},
		},
	)
}

// openBlockAt returns the innermost block whose body contains off.
func openBlockAt(file *syntax.File, off int) *syntax.Block {
	if file == nil || file.Root == nil {
		return nil
	}
	var best *syntax.Block
	walk := func(blk *syntax.Block) {
		if off > int(blk.At) && off <= int(blk.Close) {
			best = blk
		}
	}
	syntax.WalkElements(file.Root, func(e *syntax.Element) {
		for _, m := range e.Members {
			if blk, ok := m.(*syntax.Block); ok {
				blocksIn(blk, walk)
			}
		}
	})
	return best
}

// blocksIn visits blk and every block nested in its arms.
func blocksIn(blk *syntax.Block, fn func(*syntax.Block)) {
	fn(blk)
	for _, br := range blk.Branches {
		for _, m := range br.Members {
			switch x := m.(type) {
			case *syntax.Block:
				blocksIn(x, fn)
			case *syntax.Element:
				for _, sm := range x.Members {
					if nested, ok := sm.(*syntax.Block); ok {
						blocksIn(nested, fn)
					}
				}
			}
		}
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

// listVars lists the declared data variables that hold a list.
func listVars(file *syntax.File) []string {
	var out []string
	for _, dv := range file.Vars {
		if dv.List {
			out = append(out, dv.Name)
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
// `app.theme`, `app.dark`, `app.os` and `window.width`/`window.height`; nothing
// else is accepted, so completing the closed set is the whole story.
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
			{"dark", "bool", "true when the resolved scheme is dark"},
			{"os", "string", "runtime.GOOS: linux, darwin, windows, freebsd"},
		}
	case "window":
		names = []member{
			{"width", "number", "logical client width, updated on resize"},
			{"height", "number", "logical client height, updated on resize"},
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
			Documentation: &doc{Kind: "markdown",
				Value: "**" + ref + "." + m.name + "**\n\nType: `" + m.typ + "`\n\n" + m.doc},
		})
	}
	return out
}

// enclosingRef reports the reserved namespace the cursor is completing, given
// the word prefix being typed and the text before it. `app.o` yields "app", and
// so does `visible: app.` at the very start of a value.
func enclosingRef(before []byte) string {
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
// (`app.`, `window.`), the `data` block, or an {#each} loop variable.
func refItems(file *syntax.File, src []byte, off int, ref string) []completionResult {
	switch ref {
	case "app", "window":
		return globalItems(ref)
	case "data":
		out := make([]completionResult, 0, len(file.Vars))
		for _, dv := range file.Vars {
			typ := dv.Type
			if dv.List {
				typ = dv.Elem + "[]"
			}
			out = append(out, completionResult{
				Label:  dv.Name,
				Kind:   kindVariable,
				Detail: "data." + dv.Name + " · " + typ,
				Documentation: &doc{Kind: "markdown", Value: "**data." + dv.Name + "**\n\nType: `" +
					typ + "`\n\nSet it from Go with `ui.SetData(w, \"" + dv.Name + "\", …)`."},
			})
		}
		return out
	}
	sh := loopShapeAt(file, src, off, ref)
	if sh == nil {
		return nil
	}
	out := make([]completionResult, 0, len(sh.Fields))
	for _, f := range sh.Fields {
		typ := f.Type
		if f.List {
			typ = f.Elem + "[]"
		}
		out = append(out, completionResult{
			Label:  f.Name,
			Kind:   kindProperty,
			Detail: ref + "." + f.Name + " · " + typ,
			Documentation: &doc{Kind: "markdown", Value: "**" + ref + "." + f.Name + "**\n\nField of `" +
				sh.Name + "`, type `" + typ + "`."},
		})
	}
	return out
}

// loopShapeAt resolves a loop variable at off: the innermost {#each} whose arm
// contains the cursor and whose variable is named ref.
func loopShapeAt(file *syntax.File, src []byte, off int, ref string) *syntax.DataShape {
	var found *syntax.DataShape
	var walk func(blk *syntax.Block)
	walk = func(blk *syntax.Block) {
		if off <= int(blk.At) || off > int(blk.Close) {
			return
		}
		if blk.Kind == syntax.BlockEach {
			if dv := file.Var(blk.Source); dv != nil && dv.List && blk.Var == ref {
				found = file.Shape(dv.Elem)
			}
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
func valueItems(file *syntax.File, prop string, src []byte, off int, prefix string, inString bool) ([]completionResult, bool) {
	elName := enclosingElementName(file, src, off)
	el, ok := schema.Lookup(elName)
	if !ok {
		return nil, false
	}
	var out []completionResult
	var truncated bool
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
			for _, v := range m3.ColorRoles() {
				out = append(out, completionResult{
					Label: v, Kind: kindColor,
					Detail: "colour role",
					Documentation: &doc{Kind: "markdown",
						Value: "Theme role: follows the scheme. A `#rrggbb` literal stays fixed."},
				})
			}
		case prop == "language" && elName == "Code":
			// Code's language is a plain string in the schema, because an
			// unknown one must still draw; the lexers the highlighter ships are
			// what a reader wants to pick from here.
			out, truncated = languageItems(prefix, inString)
		case pd.Type == schema.TypeID:
			for _, id := range fileIDs(file) {
				out = append(out, completionResult{Label: id, Kind: kindConstant, Detail: "id"})
			}
		}
	}
	// A fragment may only use the params it declared.
	for _, p := range file.Params {
		out = append(out, completionResult{
			Label: "$" + p.Name, Kind: kindVariable, Detail: "param",
		})
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
	out := make([]completionResult, 0, len(el.Attrs)+8)
	for _, p := range el.Attrs {
		out = append(out, completionResult{
			Label:         p.Name,
			Kind:          kindProperty,
			Detail:        propDetail(el.Name, p),
			Documentation: &doc{Kind: "markdown", Value: propDoc(el.Name, p)},
		})
	}
	if el.Children {
		out = append(out, elementItems(file)...)
		out = append(out, blockItemsFor(openBlockAt(file, off) != nil, listVars(file))...)
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
				Documentation: &doc{Kind: "markdown", Value: "**" + imp.Alias + "** — fragment\n\nImported from `" + imp.Path + "`. Its params are properties on the call; `$name { ... }` fills a named slot and plain children fill `$children`."},
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
func dedupe(items []completionResult, prefix string) []completionResult {
	seen := map[string]bool{}
	out := make([]completionResult, 0, len(items))
	for _, it := range items {
		if seen[it.Label] {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(it.Label), strings.ToLower(prefix)) {
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
		return map[string]any{
			"contents": doc{Kind: "markdown", Value: "**" + el.Name + "**"},
			"range":    idx.at(start, end),
		}
	}
	if start, end, el, prop := propertyAt(file, off); prop != nil {
		return map[string]any{
			"contents": doc{Kind: "markdown", Value: propDoc(el.Name, *prop)},
			"range":    idx.at(start, end),
		}
	}
	// A value in an enum, icon or colour position explains itself.
	if start, end, prop, value := valueAt(file, off); value != "" {
		summary := fmt.Sprintf("`%s` for `%s`", value, prop.Name)
		if isIconProp(prop.Name) {
			summary = "Material Symbols icon `" + value + "`"
		}
		return map[string]any{
			"contents": doc{Kind: "markdown", Value: summary},
			"range":    idx.at(start, end),
		}
	}
	return nil
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
	name := "{#if}"
	switch {
	case blk.Kind == syntax.BlockEach:
		name = "{#each data." + blk.Source + " as " + blk.Var + "}"
	case len(blk.Branches) > 0 && blk.Branches[0].Cond != nil:
		name = "{#if " + sourceSpan(src, int(blk.Branches[0].Cond.Pos())) + "}"
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
	if el := elementAt(file, off); el != nil {
		for _, p := range el.Props() {
			start, end := valueExtent(src, p)
			if off < start || off > end {
				continue
			}
			if _, ok := p.Value.(*syntax.Map); ok {
				// Inside the braces of a map the keys are breakpoints.
				return "", mapKeyPosition(src, start, off)
			}
			return p.Name, false
		}
	}
	name, ok := braceProperty(src, off)
	return name, ok
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
	// A value that opens a brace is a responsive map: keys, not values.
	for i := colon + 1; i < off && i < len(src); i++ {
		switch src[i] {
		case ' ', '\t':
			continue
		case '{':
			return "", mapKeyPosition(src, i, off)
		default:
			i = off
		}
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
		id, isIdent := p.Value.(*syntax.Ident)
		if !isIdent {
			continue
		}
		start, end := int(id.At), int(id.At)+len(id.Val)
		if off < start || off > end {
			continue
		}
		if pd := elDef.Prop(p.Name); pd != nil {
			return start, end, pd, id.Val
		}
	}
	return 0, 0, nil, ""
}

// fileIDs lists the ids a design declares, for `id:` completion targets.
func fileIDs(file *syntax.File) []string {
	var out []string
	var walk func(e *syntax.Element)
	walk = func(e *syntax.Element) {
		if p := e.Prop("id"); p != nil {
			if id, ok := p.Value.(*syntax.Ident); ok {
				out = append(out, id.Val)
			}
		}
		for _, kid := range e.Children() {
			walk(kid)
		}
	}
	if file != nil && file.Root != nil {
		walk(file.Root)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------- docs

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

func propDoc(el string, p schema.Prop) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** on `%s`\n\nType: `%s`", p.Name, el, p.Type)
	if len(p.Enum) > 0 {
		fmt.Fprintf(&b, "\n\nValues: %s", codeList(p.Enum))
	}
	if p.Default != nil {
		fmt.Fprintf(&b, "\n\nDefault: `%v`", p.Default)
	}
	if p.Common {
		b.WriteString("\n\nAccepted by every element.")
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
