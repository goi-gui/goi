package lsp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ksckaan1/goi/internal/asset"
	"github.com/ksckaan1/goi/internal/m3"
	"github.com/ksckaan1/goi/internal/schema"
	"github.com/ksckaan1/goi/internal/syntax"
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
	if len(bundle.File.Params) > 0 {
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
	file, _ := syntax.Parse(d.text)
	prop, inMap := enclosingProperty(file, d.text, off)

	// A responsive map key, or the value of a property.
	switch {
	case inMap:
		items = append(items, breakpointItems()...)
	case prop != "":
		items = append(items, valueItems(file, prop, d.text, off)...)
	}

	// Member position: the line so far is only whitespace, or we are right
	// after an opening brace.
	trimmed := strings.TrimSpace(string(line))
	if prop == "" && !inMap && (trimmed == "" || strings.HasSuffix(trimmed, "{")) {
		items = append(items, memberItems(file, d.text, off)...)
	} else if prop == "" && !inMap {
		items = append(items, elementItems()...)
	}
	return completionList{Items: dedupe(items, prefix)}
}

// valueItems offers the values a property accepts.
func valueItems(file *syntax.File, prop string, src []byte, off int) []completionResult {
	elName := enclosingElementName(file, src, off)
	el, ok := schema.Lookup(elName)
	if !ok {
		return nil
	}
	pd := el.Prop(prop)
	var out []completionResult
	if pd != nil {
		switch pd.Type {
		case schema.TypeBool:
			for _, v := range []string{"true", "false"} {
				out = append(out, completionResult{Label: v, Kind: kindKeyword, Detail: "bool"})
			}
		case schema.TypeEnum:
			for _, v := range pd.Enum {
				out = append(out, completionResult{
					Label: v, Kind: kindEnumMember,
					Detail: prop + ": " + v,
				})
			}
		case schema.TypeColor:
			for _, v := range m3.ColorRoles() {
				out = append(out, completionResult{
					Label: v, Kind: kindColor,
					Detail: "colour role",
					Documentation: &doc{Kind: "markdown",
						Value: "Theme role: follows the scheme. A `#rrggbb` literal stays fixed."},
				})
			}
		case schema.TypeID:
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
	return out
}

// memberItems offers properties and child elements inside an element body.
func memberItems(file *syntax.File, src []byte, off int) []completionResult {
	name := enclosingElementName(file, src, off)
	el, ok := schema.Lookup(name)
	if !ok {
		return elementItems()
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
		out = append(out, elementItems()...)
	}
	return out
}

// elementItems lists every element that can be written.
func elementItems() []completionResult {
	names := schema.ElementNames()
	out := make([]completionResult, 0, len(names))
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
	// A value in an enum or colour position explains itself.
	if start, end, prop, value := valueAt(file, off); value != "" {
		return map[string]any{
			"contents": doc{Kind: "markdown", Value: fmt.Sprintf("`%s` for `%s`", value, prop.Name)},
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
	return []symbol{symbolOf(idx, file.Root)}
}

func symbolOf(idx lineIndex, e *syntax.Element) symbol {
	sym := symbol{
		Name:           e.Name,
		Kind:           19, // Object: a design element, not a language symbol
		Range:          idx.at(int(e.NameAt), int(e.Close)+1),
		SelectionRange: idx.at(int(e.NameAt), int(e.NameAt)+len(e.Name)),
	}
	for _, kid := range e.Children() {
		sym.Children = append(sym.Children, symbolOf(idx, kid))
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
	}
	walk(file.Root)
	return out
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
		}
	}
	walk(file.Root)
	return best
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
