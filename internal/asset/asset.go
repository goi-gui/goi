// Package asset loads .goi files from an fs.FS, resolving imports into one tree.
//
// A file may pull in fragments:
//
//	import Sidebar "./Sidebar.goi"
//
//	Window { Row { Sidebar { title: "Mail" } } }
//
// A fragment declares the typed values it expects and reads them by name:
//
//	props {
//		title string
//	}
//
//	Column {
//		Text { text: props.title }
//	}
//
// Expansion happens before validation, so the schema, validator and tree builder
// see one ordinary tree and need no notion of fragments.
package asset

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/goi-gui/goi/internal/syntax"
)

// Bundle is one loaded entry point together with every file it pulled in.
type Bundle struct {
	// File is the expanded tree: one root element, no imports left.
	File *syntax.File
	// Sources holds the text of every file that took part, keyed by path, so
	// diagnostics can show the right snippet.
	Sources map[string][]byte
	// Files lists the paths that were read, entry point first.
	Files []string
}

// Load reads name from fsys and expands its imports. Name is slash-separated
// and relative to the FS root, as fs.FS requires.
func Load(fsys fs.FS, name string) (*Bundle, syntax.Diagnostics) {
	l := &loader{fsys: fsys, sources: map[string][]byte{}, files: map[string]*syntax.File{}}
	// The bundle is returned even on failure: its Sources let the caller show a
	// snippet for each diagnostic.
	b := &Bundle{Sources: l.sources}
	entry := clean(name)
	root, diag := l.parse(entry, nil)
	if !diag.Empty() {
		b.Files = l.order
		return b, diag
	}
	b.Files = l.order
	expanded, diag := l.instantiateFile(root, entry, nil)
	if !diag.Empty() {
		return b, diag
	}
	// Data declarations are design-wide: Go sets them by name and any file may
	// read them, so a fragment's `data { ... }` and shapes merge into the one
	// file the tree sees. The entry file's own declarations are already there.
	for _, name := range l.order {
		if name == entry {
			continue
		}
		expanded.Vars = append(expanded.Vars, l.files[name].Vars...)
		expanded.Shapes = append(expanded.Shapes, l.files[name].Shapes...)
	}
	b.File = expanded
	return b, nil
}

type loader struct {
	fsys    fs.FS
	sources map[string][]byte
	// files holds every parsed file, keyed by its path from the FS root.
	files map[string]*syntax.File
	order []string
}

// parse reads and parses one file, recursing into its imports so a broken
// fragment is reported before anything is expanded.
func (l *loader) parse(name string, stack []string) (*syntax.File, syntax.Diagnostics) {
	for _, s := range stack {
		if s == name {
			return nil, syntax.Diagnostics{{
				Message: fmt.Sprintf("import cycle: %s", strings.Join(append(stack, name), " -> ")),
			}}
		}
	}
	if f, ok := l.files[name]; ok {
		return f, nil
	}
	src, err := fs.ReadFile(l.fsys, name)
	if err != nil {
		return nil, syntax.Diagnostics{{Message: describe(name, err)}}
	}
	l.sources[name] = src
	f, diag := syntax.ParseNamed(src, name)
	if !diag.Empty() {
		return nil, diag
	}

	chain := append(stack, name)
	taken := map[string]syntax.Pos{}
	for _, imp := range f.Imports {
		if _, dup := taken[imp.Alias]; dup {
			return nil, syntax.Diagnostics{{
				Pos:     imp.AliasAt,
				End:     imp.AliasAt + syntax.Pos(len(imp.Alias)),
				Src:     name,
				Message: fmt.Sprintf("duplicate import alias %q", imp.Alias),
			}}
		}
		taken[imp.Alias] = imp.AliasAt
		target, ok := resolve(name, imp.Path)
		if !ok {
			return nil, syntax.Diagnostics{{
				Pos:     imp.At,
				End:     imp.At + syntax.Pos(len("import")),
				Src:     name,
				Message: fmt.Sprintf("cannot import %q: it points outside the asset root", imp.Path),
			}}
		}
		if _, diag := l.parse(target, chain); !diag.Empty() {
			return nil, relabel(diag, imp, name)
		}
	}
	l.files[name] = f
	l.order = append(l.order, name)
	return f, nil
}

// relabel points an error that has no position of its own at the import that
// pulled the broken file in.
func relabel(diag syntax.Diagnostics, imp syntax.Import, from string) syntax.Diagnostics {
	for i := range diag {
		if diag[i].Src != "" {
			continue
		}
		diag[i].Src = from
		diag[i].Pos = imp.At
		diag[i].End = imp.At + syntax.Pos(len("import"))
	}
	return diag
}

// describe turns an fs error into a message that names the file.
// describe turns an fs error into a message. It carries no source file: the
// caller points the diagnostic at the import that requested the file.
func describe(name string, err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Sprintf("%s: file not found", name)
	}
	return fmt.Sprintf("%s: %v", name, err)
}

// resolve turns an import path into a slash path from the FS root, refusing
// anything that escapes it.
func resolve(from, target string) (string, bool) {
	if strings.HasPrefix(target, "/") {
		return "", false
	}
	joined := path.Clean(path.Join(path.Dir(from), target))
	if joined == ".." || strings.HasPrefix(joined, "../") || path.IsAbs(joined) {
		return "", false
	}
	return joined, true
}

func clean(name string) string {
	return strings.TrimPrefix(path.Clean("/"+name), "/")
}

// aliases returns the fragment paths a file imports, keyed by alias.
func (l *loader) aliases(f *syntax.File) map[string]string {
	out := make(map[string]string, len(f.Imports))
	for _, imp := range f.Imports {
		if target, ok := resolve(f.Src, imp.Path); ok {
			out[imp.Alias] = target
		}
	}
	return out
}

// instantiateFile expands a whole file's tree.
func (l *loader) instantiateFile(f *syntax.File, name string, stack []string) (*syntax.File, syntax.Diagnostics) {
	root, diag := l.expand(f.Root, l.aliases(f), stack)
	if !diag.Empty() {
		return nil, diag
	}
	// Props, the data declarations and the slot flag are kept so callers can
	// tell a fragment from an entry point; the imports are consumed and
	// deliberately dropped.
	return &syntax.File{
		Src: name, Props: f.Props, Vars: f.Vars, Shapes: f.Shapes,
		Fragment: f.Fragment, Root: root,
	}, nil
}

// Preview returns a copy of a fragment with its props replaced by the type's
// zero value, so the file can be laid out on its own: goicheck wraps the result
// in a Window. A file that is not a fragment comes back unchanged.
//
// A slice, a map and a handler have no zero literal, so a preview stands them in
// with a bridge variable of the same type. Nothing is registered at runtime, so
// clicking a previewed button reports "no handler" instead of panicking.
func Preview(f *syntax.File) (*syntax.File, syntax.Diagnostics) {
	if f == nil || f.Root == nil || len(f.Props) == 0 {
		return f, nil
	}
	props := propValues(f, nil)
	out := *f
	for _, pr := range f.Props {
		if pr.Type.Kind == syntax.TypeScalar {
			continue
		}
		props[pr.Name] = &syntax.Ref{ID: "bridge", Prop: pr.Name}
		if out.Var(pr.Name) == nil {
			out.Vars = append(out.Vars, syntax.Field{Name: pr.Name, NameAt: pr.NameAt, Type: pr.Type, At: pr.At})
		}
	}
	root := copyElement(f.Root)
	if diag := substitute(root, props, f.Src, &out); !diag.Empty() {
		return nil, diag
	}
	out.Root = root
	return &out, nil
}

// propValues is what each declared prop holds: the instance's value when it
// passed one, else the type's zero value.
func propValues(f *syntax.File, passed map[string]syntax.Value) map[string]syntax.Value {
	out := make(map[string]syntax.Value, len(f.Props))
	for _, pr := range f.Props {
		if v, ok := passed[pr.Name]; ok {
			out[pr.Name] = v
			continue
		}
		out[pr.Name] = zeroValue(pr.Type)
	}
	return out
}

// zeroValue is Go's zero for a declared type, as a literal the tree can read. A
// slice or a map has no literal form and stays nil, which substitute reports.
func zeroValue(t syntax.Type) syntax.Value {
	if t.Kind != syntax.TypeScalar {
		return nil
	}
	switch t.Name {
	case "bool":
		return &syntax.Bool{Val: false}
	case "color":
		return &syntax.Color{RGBA: 0}
	case "string":
		return &syntax.String{Val: ""}
	}
	if isNumericName(t.Name) {
		return &syntax.Number{Val: 0}
	}
	return nil
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

// expand walks one element tree, replacing fragment instances with their
// content. aliases belongs to the file el came from.
func (l *loader) expand(el *syntax.Element, aliases map[string]string, stack []string) (*syntax.Element, syntax.Diagnostics) {
	// A fragment can stand at the root of the expansion too: filling a slot
	// expands each child on its own, and a fragment may wrap another one.
	if target, ok := aliases[el.Name]; ok {
		return l.instance(target, el, aliases, stack)
	}
	out := copyElement(el)
	kids, diag := l.expandMembers(out.Members, aliases, stack)
	if !diag.Empty() {
		return nil, diag
	}
	out.Members = kids
	return out, nil
}

// expandMembers expands one member list, including the arms of a control block.
func (l *loader) expandMembers(members []syntax.Member, aliases map[string]string, stack []string) ([]syntax.Member, syntax.Diagnostics) {
	kids := make([]syntax.Member, 0, len(members))
	for _, m := range members {
		switch child := m.(type) {
		case *syntax.Block:
			for i := range child.Branches {
				expanded, diag := l.expandMembers(child.Branches[i].Members, aliases, stack)
				if !diag.Empty() {
					return nil, diag
				}
				child.Branches[i].Members = expanded
			}
			kids = append(kids, child)
		case *syntax.Element:
			if strings.HasPrefix(child.Name, "$") {
				// A slot with no caller: the fragment is being previewed on
				// its own, so nothing goes there.
				continue
			}
			expanded, diag := l.expand(child, aliases, stack)
			if !diag.Empty() {
				return nil, diag
			}
			kids = append(kids, expanded)
		default:
			kids = append(kids, m)
		}
	}
	return kids, nil
}

// instance replaces one fragment instance with the fragment's root element,
// substituting props, applying the instance's own properties and putting the
// instance's children where the fragment wrote `$children`. callerAliases
// belongs to the file the call was written in, so a child element that is
// itself a fragment resolves against the right file.
func (l *loader) instance(target string, call *syntax.Element, callerAliases map[string]string, stack []string) (*syntax.Element, syntax.Diagnostics) {
	for _, s := range stack {
		if s == target {
			return nil, syntax.Diagnostics{{
				Pos:     call.NameAt,
				End:     call.NameAt + syntax.Pos(len(call.Name)),
				Src:     call.Src,
				Message: fmt.Sprintf("import cycle: %s", strings.Join(append(stack, target), " -> ")),
			}}
		}
	}
	f, ok := l.files[target]
	if !ok {
		return nil, syntax.Diagnostics{{
			Pos:     call.NameAt,
			End:     call.NameAt + syntax.Pos(len(call.Name)),
			Src:     call.Src,
			Message: fmt.Sprintf("unknown fragment %q", call.Name),
		}}
	}

	// Props take whatever the instance passed; a prop it leaves out is the
	// type's zero value.
	passed := make(map[string]syntax.Value, len(f.Props))
	declared := make(map[string]bool, len(f.Props))
	for _, pr := range f.Props {
		declared[pr.Name] = true
	}
	var overrides []*syntax.Property
	for _, p := range call.Props() {
		if declared[p.Name] {
			passed[p.Name] = p.Value
			continue
		}
		overrides = append(overrides, p)
	}
	props := propValues(f, passed)

	body := copyElement(f.Root)
	if diag := substitute(body, props, target, f); !diag.Empty() {
		return nil, diag
	}
	// Before the caller's children land in a slot, and before the caller's own
	// properties are applied to the root: their ids and references belong to
	// the caller, and only the fragment's own ids follow the instance id. A
	// fragment that declares an `id` prop owns that name, so the instance's
	// `id:` is its value and scopes nothing.
	if id, ok := literalID(call); ok && !declared["id"] {
		scopeIDs(body, id)
	}
	applyOverrides(body, overrides)

	// Split the call's children: `$name { ... }` goes to that slot, everything
	// else to the unnamed `$children` slot.
	slots := map[string][]*syntax.Element{}
	namedAt := map[string]syntax.Pos{}
	var names []string
	var unnamed []*syntax.Element
	for _, k := range call.Children() {
		if strings.HasPrefix(k.Name, "$") {
			if _, ok := slots[k.Name]; !ok {
				names = append(names, k.Name)
				namedAt[k.Name] = k.NameAt
			}
			slots[k.Name] = append(slots[k.Name], k.Children()...)
			continue
		}
		unnamed = append(unnamed, k)
	}
	slots["$children"] = append(slots["$children"], unnamed...)

	found, diag := l.fillSlot(body, slots, callerAliases, append(stack, target))
	if !diag.Empty() {
		return nil, diag
	}
	if !found["$children"] && len(unnamed) > 0 {
		first := unnamed[0]
		return nil, syntax.Diagnostics{{
			Pos:     first.NameAt,
			End:     first.NameAt + syntax.Pos(len(first.Name)),
			Src:     first.Src,
			Message: fmt.Sprintf("%s does not take children: add $children to %s where they should appear", call.Name, target),
		}}
	}
	for _, name := range names {
		if found[name] {
			continue
		}
		at := namedAt[name]
		return nil, syntax.Diagnostics{{
			Pos:     at,
			End:     at + syntax.Pos(len(name)),
			Src:     call.Src,
			Message: fmt.Sprintf("%s has no slot %s: write %s in %s where its children should appear", call.Name, name, name, target),
		}}
	}

	expanded, diag := l.expand(body, l.aliases(f), append(stack, target))
	if !diag.Empty() {
		return nil, diag
	}
	return expanded, nil
}

// fillSlot replaces every `$name` in el with the instance's elements for that
// slot, expanded in the caller's file so their own imports resolve there. A slot
// anywhere in the fragment body counts, inside a block arm too, which is how a
// fragment forwards its children. It reports the slots it found.
func (l *loader) fillSlot(el *syntax.Element, slots map[string][]*syntax.Element, callerAliases map[string]string, stack []string) (map[string]bool, syntax.Diagnostics) {
	found := map[string]bool{}
	members, diag := l.fillMembers(el.Members, slots, callerAliases, stack, found)
	if !diag.Empty() {
		return found, diag
	}
	el.Members = members
	return found, nil
}

// fillMembers fills the slots in one member list, recursing into blocks and
// nested elements.
func (l *loader) fillMembers(members []syntax.Member, slots map[string][]*syntax.Element, callerAliases map[string]string, stack []string, found map[string]bool) ([]syntax.Member, syntax.Diagnostics) {
	out := make([]syntax.Member, 0, len(members))
	for _, m := range members {
		switch x := m.(type) {
		case *syntax.Block:
			for i := range x.Branches {
				arm, diag := l.fillMembers(x.Branches[i].Members, slots, callerAliases, stack, found)
				if !diag.Empty() {
					return nil, diag
				}
				x.Branches[i].Members = arm
			}
			out = append(out, x)
		case *syntax.Element:
			if strings.HasPrefix(x.Name, "$") {
				found[x.Name] = true
				for _, k := range slots[x.Name] {
					expanded, diag := l.expand(copyElement(k), callerAliases, stack)
					if !diag.Empty() {
						return nil, diag
					}
					out = append(out, expanded)
				}
				continue
			}
			nested, diag := l.fillMembers(x.Members, slots, callerAliases, stack, found)
			if !diag.Empty() {
				return nil, diag
			}
			x.Members = nested
			out = append(out, x)
		default:
			out = append(out, m)
		}
	}
	return out, nil
}

// substitute replaces `props.name` values in place and reports a prop the
// fragment never declared.
func substitute(el *syntax.Element, props map[string]syntax.Value, from string, f *syntax.File) syntax.Diagnostics {
	var diag syntax.Diagnostics
	var walk func(*syntax.Element)
	walk = func(e *syntax.Element) {
		walkMembers(e.Members, props, from, f, &diag, walk)
	}
	walk(el)
	return diag
}

// walkMembers substitutes props in a member list, descending into block arms.
func walkMembers(members []syntax.Member, props map[string]syntax.Value, from string, f *syntax.File, diag *syntax.Diagnostics, walk func(*syntax.Element)) {
	for _, m := range members {
		switch v := m.(type) {
		case *syntax.Property:
			v.Value, *diag = substValue(v.Value, props, v.Src, from, *diag)
		case *syntax.Element:
			walk(v)
		case *syntax.Block:
			if v.Kind == syntax.BlockFor && v.Range != nil {
				v.Range, *diag = substValue(v.Range, props, v.Src, from, *diag)
			}
			for i := range v.Branches {
				br := &v.Branches[i]
				if br.Cond != nil {
					br.Cond, *diag = substValue(br.Cond, props, v.Src, from, *diag)
				}
				walkMembers(br.Members, props, from, f, diag, walk)
			}
		}
	}
}

// substValue returns a value with every `props.name` reference replaced.
func substValue(v syntax.Value, props map[string]syntax.Value, src, from string, diag syntax.Diagnostics) (syntax.Value, syntax.Diagnostics) {
	switch x := v.(type) {
	case *syntax.Ref:
		if x.ID != "props" {
			return x, diag
		}
		got, ok := props[x.Prop]
		if !ok {
			diag = append(diag, syntax.Diagnostic{
				Pos:     x.At,
				End:     x.At + syntax.Pos(len(x.ID)+1+len(x.Prop)),
				Src:     src,
				Message: fmt.Sprintf("%s has no prop %q", from, x.Prop),
			})
			return x, diag
		}
		if got == nil {
			diag = append(diag, syntax.Diagnostic{
				Pos:     x.At,
				End:     x.At + syntax.Pos(len(x.ID)+1+len(x.Prop)),
				Src:     src,
				Message: fmt.Sprintf("%s has no prop %q: a slice or a map has no zero literal, so the instance must pass one", from, x.Prop),
			})
			return x, diag
		}
		// Copy: the value belongs to the caller (or to the fragment's default),
		// and the expansion rewrites ids and references in place.
		return copyValue(got), diag
	case *syntax.Call:
		callee := x.Callee
		// A builtin (formatf) has no callee; only a handler call names a prop.
		if callee != nil && callee.ID == "props" {
			// `onClick: props.onOpen(item.id)`: the instance passed the real
			// handler, so the call points at it now.
			got, ok := props[callee.Prop]
			if !ok {
				diag = append(diag, syntax.Diagnostic{
					Pos:     x.At,
					End:     x.At + syntax.Pos(len("props.")+len(callee.Prop)),
					Src:     src,
					Message: fmt.Sprintf("%s has no prop %q", from, callee.Prop),
				})
				return x, diag
			}
			ref, ok := got.(*syntax.Ref)
			if !ok || (ref.ID != "bridge" && ref.ID != "data") {
				diag = append(diag, syntax.Diagnostic{
					Pos:     x.At,
					End:     x.At + syntax.Pos(len("props.")+len(callee.Prop)),
					Src:     src,
					Message: fmt.Sprintf("props.%s is a handler; the instance must pass one, as %s: bridge.onSomething", callee.Prop, callee.Prop),
				})
				return x, diag
			}
			callee = ref
		}
		out := &syntax.Call{At: x.At, Callee: callee, Name: x.Name, Args: make([]syntax.Value, len(x.Args))}
		for i, a := range x.Args {
			out.Args[i], diag = substValue(a, props, src, from, diag)
		}
		return out, diag
	case *syntax.Map:
		out := &syntax.Map{At: x.At, Entries: make([]syntax.MapEntry, len(x.Entries))}
		for i, entry := range x.Entries {
			entry.Value, diag = substValue(entry.Value, props, src, from, diag)
			out.Entries[i] = entry
		}
		return out, diag
	case *syntax.Binary:
		left, diag := substValue(x.Left, props, src, from, diag)
		right, diag := substValue(x.Right, props, src, from, diag)
		return &syntax.Binary{At: x.At, Op: x.Op, Left: left, Right: right}, diag
	case *syntax.Unary:
		xv, diag := substValue(x.X, props, src, from, diag)
		return &syntax.Unary{At: x.At, Op: x.Op, X: xv}, diag
	case *syntax.Ternary:
		cond, diag := substValue(x.Cond, props, src, from, diag)
		then, diag := substValue(x.Then, props, src, from, diag)
		els, diag := substValue(x.Else, props, src, from, diag)
		return &syntax.Ternary{At: x.At, Cond: cond, Then: then, Else: els}, diag

	}
	return v, diag
}

// applyOverrides writes the instance's properties onto the fragment root,
// replacing a property the fragment already set instead of duplicating it.
func applyOverrides(el *syntax.Element, props []*syntax.Property) {
	for _, p := range props {
		replaced := false
		for i, m := range el.Members {
			existing, ok := m.(*syntax.Property)
			if !ok || existing.Name != p.Name {
				continue
			}
			el.Members[i] = p
			replaced = true
		}
		if !replaced {
			el.Members = append(el.Members, p)
		}
	}
}

// copyElement deep-copies an element tree, so two instances of one fragment do
// not share AST pointers.
func copyElement(el *syntax.Element) *syntax.Element {
	out := &syntax.Element{
		Name:    el.Name,
		NameAt:  el.NameAt,
		Open:    el.Open,
		Close:   el.Close,
		Src:     el.Src,
		Members: copyMembers(el.Members),
	}
	return out
}

// copyBlock deep-copies a control block, so two instances of one fragment do not
// share arm slices.
func copyBlock(b *syntax.Block) *syntax.Block {
	out := &syntax.Block{
		Kind: b.Kind, At: b.At, Close: b.Close, Src: b.Src,
		KeyVar: b.KeyVar, KeyAt: b.KeyAt, ValVar: b.ValVar, ValAt: b.ValAt,
		Range: copyValue(b.Range), RangeAt: b.RangeAt,
	}
	out.Branches = make([]syntax.Branch, len(b.Branches))
	for i, br := range b.Branches {
		out.Branches[i] = syntax.Branch{
			Cond:    copyValue(br.Cond),
			At:      br.At,
			Members: copyMembers(br.Members),
		}
	}
	return out
}

func copyMembers(members []syntax.Member) []syntax.Member {
	out := make([]syntax.Member, 0, len(members))
	for _, m := range members {
		switch v := m.(type) {
		case *syntax.Property:
			out = append(out, &syntax.Property{
				Name: v.Name, NameAt: v.NameAt, Value: copyValue(v.Value), Src: v.Src,
			})
		case *syntax.Element:
			out = append(out, copyElement(v))
		case *syntax.Block:
			out = append(out, copyBlock(v))
		}
	}
	return out
}

// copyValue clones a value, leaves included: expansion rewrites ids and the
// references that name them, and two instances must not share those pointers.
func copyValue(v syntax.Value) syntax.Value {
	switch x := v.(type) {
	case *syntax.Ident:
		return &syntax.Ident{At: x.At, Val: x.Val}
	case *syntax.String:
		return &syntax.String{At: x.At, Val: x.Val}
	case *syntax.Ref:
		return &syntax.Ref{At: x.At, ID: x.ID, Prop: x.Prop}
	case *syntax.Map:
		out := &syntax.Map{At: x.At, Entries: make([]syntax.MapEntry, len(x.Entries))}
		for i, entry := range x.Entries {
			entry.Value = copyValue(entry.Value)
			out.Entries[i] = entry
		}
		return out
	case *syntax.Binary:
		return &syntax.Binary{At: x.At, Op: x.Op, Left: copyValue(x.Left), Right: copyValue(x.Right)}
	case *syntax.Unary:
		return &syntax.Unary{At: x.At, Op: x.Op, X: copyValue(x.X)}
	case *syntax.Ternary:
		return &syntax.Ternary{At: x.At, Cond: copyValue(x.Cond), Then: copyValue(x.Then), Else: copyValue(x.Else)}
	case *syntax.Call:
		args := make([]syntax.Value, len(x.Args))
		for i, a := range x.Args {
			args[i] = copyValue(a)
		}
		var callee *syntax.Ref
		if x.Callee != nil {
			callee = &syntax.Ref{At: x.Callee.At, ID: x.Callee.ID, Prop: x.Callee.Prop}
		}
		return &syntax.Call{At: x.At, Callee: callee, Name: x.Name, Args: args}
	}
	return v
}
