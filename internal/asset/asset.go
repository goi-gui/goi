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

	"github.com/goi-gui/goi/internal/componentdef"
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
	// read them, so a fragment's `bridge { ... }` and shapes merge into the one
	// file the tree sees. The entry file's own declarations are already there.
	// A name may repeat across files; it is one declaration as long as its type
	// is the same, so every page reads the value and handler SetData/SetHandler
	// writes.
	type bridgeDecl struct {
		v   syntax.DataVar
		src string
	}
	seen := make(map[string]bridgeDecl, len(expanded.Vars))
	for _, v := range expanded.Vars {
		if _, ok := seen[v.Name]; !ok {
			seen[v.Name] = bridgeDecl{v: v, src: entry}
		}
	}
	// Setups stay per file: their names are visible only to the elements
	// written in that file, and the tree resolves them by Element.Src.
	for _, name := range l.order {
		expanded.Setups = append(expanded.Setups, l.files[name].Setups...)
	}
	type shapeDecl struct {
		v   syntax.DataShape
		src string
	}
	seenShapes := make(map[string]shapeDecl, len(expanded.Shapes))
	for _, sh := range expanded.Shapes {
		if _, ok := seenShapes[sh.Name]; !ok {
			seenShapes[sh.Name] = shapeDecl{v: sh, src: entry}
		}
	}
	for _, name := range l.order {
		if name == entry {
			continue
		}
		for _, v := range l.files[name].Vars {
			prev, ok := seen[v.Name]
			if !ok {
				expanded.Vars = append(expanded.Vars, v)
				seen[v.Name] = bridgeDecl{v: v, src: name}
				continue
			}
			if sameBridgeType(prev.v.Type, v.Type) {
				continue
			}
			diag = append(diag, syntax.Diagnostic{
				Src: name, Pos: v.NameAt, End: v.NameAt + syntax.Pos(len(v.Name)),
				Message: fmt.Sprintf("bridge declaration %q is %s here, but %s in %s",
					v.Name, v.Type.String(), prev.v.Type.String(), prev.src),
			})
		}
		for _, sh := range l.files[name].Shapes {
			prev, ok := seenShapes[sh.Name]
			if !ok {
				expanded.Shapes = append(expanded.Shapes, sh)
				seenShapes[sh.Name] = shapeDecl{v: sh, src: name}
				continue
			}
			if sameShape(prev.v, sh) {
				continue
			}
			diag = append(diag, syntax.Diagnostic{
				Src: name, Pos: sh.NameAt, End: sh.NameAt + syntax.Pos(len(sh.Name)),
				Message: fmt.Sprintf("row type %q is declared differently here than in %s",
					sh.Name, prev.src),
			})
		}
	}
	b.File = expanded
	if !diag.Empty() {
		return b, diag
	}
	return b, nil
}

// sameShape reports whether two row-type declarations are the same shape: the
// same fields in the same order, with the same types.
func sameShape(a, b syntax.DataShape) bool {
	if len(a.Fields) != len(b.Fields) {
		return false
	}
	for i := range a.Fields {
		if a.Fields[i].Name != b.Fields[i].Name || !sameBridgeType(a.Fields[i].Type, b.Fields[i].Type) {
			return false
		}
	}
	return true
}

// sameBridgeType reports whether two bridge declarations have the same type.
// Parameter names on a handler do not matter; the arity and each parameter type
// do.
func sameBridgeType(a, b syntax.Type) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case syntax.TypeScalar:
		return a.Name == b.Name
	case syntax.TypeSlice:
		if a.Elem == nil || b.Elem == nil {
			return a.Elem == b.Elem
		}
		return sameBridgeType(*a.Elem, *b.Elem)
	case syntax.TypeMap:
		if a.Name != b.Name || a.Elem == nil || b.Elem == nil {
			return a.Name == b.Name && a.Elem == b.Elem
		}
		return sameBridgeType(*a.Elem, *b.Elem)
	case syntax.TypeFunc:
		if len(a.Args) != len(b.Args) {
			return false
		}
		for i := range a.Args {
			if !sameBridgeType(a.Args[i].Type, b.Args[i].Type) {
				return false
			}
		}
		if a.Result == nil || b.Result == nil {
			return a.Result == b.Result
		}
		return sameBridgeType(*a.Result, *b.Result)
	}
	return false
}

type loader struct {
	fsys fs.FS
	// nextScope numbers fragment instances, so each gets its own setup state.
	nextScope int
	sources   map[string][]byte
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
// anything that escapes it. `goi://ui/Sidebar.goi` is a path from the container
// root; anything else is relative to the importing file.
func resolve(from, target string) (string, bool) {
	if strings.HasPrefix(target, "goi://") {
		joined := path.Clean(strings.TrimPrefix(target, "goi://"))
		if joined == "." || joined == ".." || strings.HasPrefix(joined, "../") || path.IsAbs(joined) {
			return "", false
		}
		return joined, true
	}
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
	// A slot in a file the app loads has no caller, so it is a mistake; a
	// fragment opened on its own is allowed to keep one as an empty hole. A
	// Window is never a fragment: its root is the window itself.
	slots := f.IsFragment() && f.Root.Name != "Window"
	// A fragment opened on its own has no caller, so every slot it writes is
	// empty and `len($slot)` reads zero.
	body := copyElement(f.Root)
	if diag := slotLens(body, nil, name); !diag.Empty() {
		return nil, diag
	}
	root, diag := l.expand(body, l.aliases(f), stack, slots)
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
			out.Vars = append(out.Vars, syntax.Field(pr))
		}
	}
	root := copyElement(f.Root)
	if diag := substitute(root, props, f.Src, &out, f.Src, ""); !diag.Empty() {
		return nil, diag
	}
	if len(props) > 0 {
		root.InstanceProps = make(map[string]syntax.InstanceProp, len(props))
		for name, v := range props {
			root.InstanceProps[name] = syntax.InstanceProp{Value: copyValue(v), Src: f.Src}
		}
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
func (l *loader) expand(el *syntax.Element, aliases map[string]string, stack []string, slots bool) (*syntax.Element, syntax.Diagnostics) {
	// An element another pass already resolved is concrete: its imports belong
	// to the file it was written in, and that file may not be this one.
	if el.Resolved {
		return copyElement(el), nil
	}
	// A fragment can stand at the root of the expansion too: filling a slot
	// expands each child on its own, and a fragment may wrap another one.
	if target, ok := aliases[el.Name]; ok {
		return l.instance(target, el, aliases, stack)
	}
	out := copyElement(el)
	kids, diag := l.expandMembers(out.Members, aliases, stack, slots, out.Name)
	if !diag.Empty() {
		return nil, diag
	}
	out.Members = kids
	out.Resolved = true
	return out, nil
}

// expandMembers expands one member list, including the arms of a control block.
// owner is the element the members belong to, so a built-in component's own
// `$name` slot is kept while a fragment slot is dropped or reported.
func (l *loader) expandMembers(members []syntax.Member, aliases map[string]string, stack []string, slots bool, owner string) ([]syntax.Member, syntax.Diagnostics) {
	kids := make([]syntax.Member, 0, len(members))
	for _, m := range members {
		switch child := m.(type) {
		case *syntax.Block:
			for i := range child.Branches {
				expanded, diag := l.expandMembers(child.Branches[i].Members, aliases, stack, slots, owner)
				if !diag.Empty() {
					return nil, diag
				}
				child.Branches[i].Members = expanded
			}
			kids = append(kids, child)
		case *syntax.Element:
			if child.Resolved {
				kids = append(kids, copyElement(child))
				continue
			}
			if strings.HasPrefix(child.Name, "$") {
				if componentdef.Slot(owner, child.Name) {
					// A built-in's own slot stays in the tree; the tree builder
					// turns it into the component's panel.
					expanded, diag := l.expand(child, aliases, stack, slots)
					if !diag.Empty() {
						return nil, diag
					}
					kids = append(kids, expanded)
					continue
				}
				if !slots {
					// A slot is filled by whoever imports the file, and the file
					// a program loads has nobody above it. Saying so beats
					// silently dropping what the design meant as a hole.
					msg := fmt.Sprintf("%s is a slot: only a fragment can declare one, because nothing above the loaded file fills it", child.Name)
					hint := ""
					if child.Open != child.NameAt {
						msg = fmt.Sprintf("%s { … } fills a slot, and nothing above this file fills one", child.Name)
						hint = fmt.Sprintf("a fragment declares a slot bare (%s); the instance fills it with %s { … }", child.Name, child.Name)
					}
					return nil, syntax.Diagnostics{{
						Pos:     child.NameAt,
						End:     child.NameAt + syntax.Pos(len(child.Name)),
						Src:     child.Src,
						Message: msg,
						Hint:    hint,
					}}
				}
				// A fragment previewed on its own has no caller, so nothing
				// goes there.
				continue
			}
			expanded, diag := l.expand(child, aliases, stack, slots)
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

	// The caller's children split into fills, `$name { ... }` for a named slot
	// and everything else for `$children`. Do it before the body is rewritten:
	// `len($slot)` needs the counts.
	slots := map[string][]*syntax.Element{}
	namedAt := map[string]syntax.Pos{}
	var names []string
	var unnamed []*syntax.Element
	for _, k := range call.Children() {
		if strings.HasPrefix(k.Name, "$") {
			if k.Open == k.NameAt {
				// A bare slot is a marker in a fragment body; by the time a call
				// is expanded, a fragment's own marker has already been filled,
				// so one here would pass nothing at all.
				return nil, syntax.Diagnostics{{
					Pos:     k.NameAt,
					End:     k.NameAt + syntax.Pos(len(k.Name)),
					Src:     call.Src,
					Message: fmt.Sprintf("%s at a call passes nothing: write %s { … } to fill the slot", k.Name, k.Name),
				}}
			}
			if props := k.Props(); len(props) > 0 {
				return nil, syntax.Diagnostics{{
					Pos:     props[0].NameAt,
					End:     props[0].NameAt + syntax.Pos(len(props[0].Name)),
					Src:     call.Src,
					Message: fmt.Sprintf("%s takes elements only: a slot is filled with children, not properties", k.Name),
				}}
			}
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
	counts := make(map[string]int, len(slots))
	for name, els := range slots {
		counts[name] = len(els)
	}

	body := copyElement(f.Root)
	if len(props) > 0 {
		body.InstanceProps = make(map[string]syntax.InstanceProp, len(props))
		for name, v := range props {
			src := f.Src
			if _, ok := passed[name]; ok {
				src = call.Src
			}
			body.InstanceProps[name] = syntax.InstanceProp{Value: copyValue(v), Src: src}
		}
	}
	l.nextScope++
	setScope(body, fmt.Sprintf("%s#%d", target, l.nextScope))
	if diag := slotLens(body, counts, target); !diag.Empty() {
		return nil, diag
	}
	if diag := substitute(body, props, target, f, call.Src, call.Scope); !diag.Empty() {
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
	applyOverrides(body, overrides, call.Scope)

	found, diag := l.fillSlot(body, slots, callerAliases, l.aliases(f), append(stack, target))
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

	expanded, diag := l.expand(body, l.aliases(f), append(stack, target), true)
	if !diag.Empty() {
		return nil, diag
	}
	return expanded, nil
}

// fillSlot replaces every `$name` in el with the instance's elements for that
// slot, expanded in the caller's file so their own imports resolve there. A slot
// anywhere in the fragment body counts, inside a block arm too, which is how a
// fragment forwards its children. It reports the slots it found.
func (l *loader) fillSlot(el *syntax.Element, slots map[string][]*syntax.Element, callerAliases, fragAliases map[string]string, stack []string) (map[string]bool, syntax.Diagnostics) {
	found := map[string]bool{}
	members, diag := l.fillMembers(el.Members, slots, callerAliases, fragAliases, stack, false, found, el.Name)
	if !diag.Empty() {
		return found, diag
	}
	el.Members = members
	return found, nil
}

// fillMembers fills the slots in one member list, recursing into blocks and
// nested elements.
func (l *loader) fillMembers(members []syntax.Member, slots map[string][]*syntax.Element, callerAliases, fragAliases map[string]string, stack []string, inCall bool, found map[string]bool, owner string) ([]syntax.Member, syntax.Diagnostics) {
	out := make([]syntax.Member, 0, len(members))
	for _, m := range members {
		switch x := m.(type) {
		case *syntax.Block:
			for i := range x.Branches {
				arm, diag := l.fillMembers(x.Branches[i].Members, slots, callerAliases, fragAliases, stack, inCall, found, owner)
				if !diag.Empty() {
					return nil, diag
				}
				x.Branches[i].Members = arm
			}
			out = append(out, x)
		case *syntax.Element:
			if strings.HasPrefix(x.Name, "$") {
				if componentdef.Slot(owner, x.Name) {
					// A built-in's own slot is ordinary content: recurse so a
					// fragment slot inside it still fills.
					nested, diag := l.fillMembers(x.Members, slots, callerAliases, fragAliases, stack, inCall, found, x.Name)
					if !diag.Empty() {
						return nil, diag
					}
					x.Members = nested
					out = append(out, x)
					continue
				}
				if inCall && x.Open != x.NameAt {
					// Inside a nested fragment call this is that call's argument,
					// not this fragment's slot: leave it for the inner instance.
					out = append(out, x)
					continue
				}
				if x.Open != x.NameAt {
					// Braces fill a slot; a fragment declares one bare, so the
					// loader can tell the two apart without a caller in sight.
					at := x.NameAt
					if pr := x.Props(); len(pr) > 0 {
						at = pr[0].NameAt
					}
					return nil, syntax.Diagnostics{{
						Pos:     at,
						End:     at + syntax.Pos(len(x.Name)),
						Src:     x.Src,
						Message: fmt.Sprintf("%s in a fragment takes no elements: declare it bare, and let the instance fill it", x.Name),
						Hint:    fmt.Sprintf("write %s, and pass %s { … } from the call", x.Name, x.Name),
					}}
				}
				if found[x.Name] {
					return nil, syntax.Diagnostics{{
						Pos:     x.NameAt,
						End:     x.NameAt + syntax.Pos(len(x.Name)),
						Src:     x.Src,
						Message: fmt.Sprintf("%s appears more than once in this fragment: write a slot once, so the elements it receives have one home", x.Name),
					}}
				}
				found[x.Name] = true
				if inCall {
					// Forwarding keeps the slot's name: the elements become the
					// inner call's `$name`, not its plain children, or a named
					// slot would land in the unnamed one.
					fill := &syntax.Element{
						Name: x.Name, NameAt: x.NameAt, Src: x.Src,
						// A body, even when empty: this is a fill at a call.
						Open: x.NameAt + 1, Close: x.NameAt + 1,
					}
					for _, k := range slots[x.Name] {
						expanded, diag := l.expand(copyElement(k), callerAliases, stack, true)
						if !diag.Empty() {
							return nil, diag
						}
						fill.Members = append(fill.Members, expanded)
					}
					if len(fill.Members) > 0 {
						out = append(out, fill)
					}
					continue
				}
				for _, k := range slots[x.Name] {
					expanded, diag := l.expand(copyElement(k), callerAliases, stack, true)
					if !diag.Empty() {
						return nil, diag
					}
					out = append(out, expanded)
				}
				continue
			}
			// A bare slot inside a nested fragment call is how a fragment
			// forwards its children: fill it here. A slot with content is that
			// call's own argument, which the rule above leaves alone.
			_, isCall := fragAliases[x.Name]
			nested, diag := l.fillMembers(x.Members, slots, callerAliases, fragAliases, stack, inCall || isCall, found, x.Name)
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
func substitute(el *syntax.Element, props map[string]syntax.Value, from string, f *syntax.File, callerSrc, callerScope string) syntax.Diagnostics {
	var diag syntax.Diagnostics
	var walk func(*syntax.Element)
	walk = func(e *syntax.Element) {
		walkMembers(e.Members, props, from, f, callerSrc, callerScope, &diag, walk)
	}
	walk(el)
	return diag
}

// walkMembers substitutes props in a member list, descending into block arms.
func walkMembers(members []syntax.Member, props map[string]syntax.Value, from string, f *syntax.File, callerSrc, callerScope string, diag *syntax.Diagnostics, walk func(*syntax.Element)) {
	for _, m := range members {
		switch v := m.(type) {
		case *syntax.Property:
			var changed bool
			v.Value, changed, *diag = substValue(v.Value, props, v.Src, from, callerSrc, callerScope, *diag)
			if changed {
				v.CallerSrc, v.CallerScope = callerSrc, callerScope
			}
		case *syntax.Element:
			walk(v)
		case *syntax.Block:
			blockChanged := false
			if v.Kind == syntax.BlockFor && v.Range != nil {
				var changed bool
				v.Range, changed, *diag = substValue(v.Range, props, v.Src, from, callerSrc, callerScope, *diag)
				blockChanged = blockChanged || changed
			}
			if v.Kind == syntax.BlockSwitch && v.Subject != nil {
				var changed bool
				v.Subject, changed, *diag = substValue(v.Subject, props, v.Src, from, callerSrc, callerScope, *diag)
				blockChanged = blockChanged || changed
			}
			for i := range v.Branches {
				br := &v.Branches[i]
				if br.Cond != nil {
					var changed bool
					br.Cond, changed, *diag = substValue(br.Cond, props, v.Src, from, callerSrc, callerScope, *diag)
					blockChanged = blockChanged || changed
				}
				for j, val := range br.Vals {
					var changed bool
					br.Vals[j], changed, *diag = substValue(val, props, v.Src, from, callerSrc, callerScope, *diag)
					blockChanged = blockChanged || changed
				}
				walkMembers(br.Members, props, from, f, callerSrc, callerScope, diag, walk)
			}
			if blockChanged {
				v.CallerSrc, v.CallerScope = callerSrc, callerScope
			}
		}
	}
}

// substValue returns a value with every `props.name` reference replaced. changed
// reports that a caller value now sits in the tree, so the enclosing property or
// block resolves setup names in the caller's file.
func substValue(v syntax.Value, props map[string]syntax.Value, src, from, callerSrc, callerScope string, diag syntax.Diagnostics) (syntax.Value, bool, syntax.Diagnostics) {
	switch x := v.(type) {
	case *syntax.Ref:
		if x.ID != "props" {
			return x, false, diag
		}
		if len(x.Path) > 0 {
			diag = append(diag, syntax.Diagnostic{
				Pos:     x.At,
				End:     x.End(),
				Src:     src,
				Message: fmt.Sprintf("props.%s cannot be walked field by field: pass %s as its own prop", x.Prop, x.Path[len(x.Path)-1]),
			})
			return x, false, diag
		}
		got, ok := props[x.Prop]
		if !ok {
			diag = append(diag, syntax.Diagnostic{
				Pos:     x.At,
				End:     x.End(),
				Src:     src,
				Message: fmt.Sprintf("%s has no prop %q", from, x.Prop),
			})
			return x, false, diag
		}
		if got == nil {
			diag = append(diag, syntax.Diagnostic{
				Pos:     x.At,
				End:     x.End(),
				Src:     src,
				Message: fmt.Sprintf("props.%s is not set: a slice or a map has no literal default, so %s must pass one", x.Prop, from),
			})
			return x, false, diag
		}
		// Copy: the value belongs to the caller (or to the fragment's default),
		// and the expansion rewrites ids and references in place.
		return copyValue(got), true, diag
	case *syntax.Call:
		callee := x.Callee
		changed := false
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
				return x, false, diag
			}
			ref, ok := got.(*syntax.Ref)
			if !ok || (ref.ID != "bridge" && ref.ID != "data") {
				diag = append(diag, syntax.Diagnostic{
					Pos:     x.At,
					End:     x.At + syntax.Pos(len("props.")+len(callee.Prop)),
					Src:     src,
					Message: fmt.Sprintf("props.%s is a handler; the instance must pass one, as %s: bridge.onSomething", callee.Prop, callee.Prop),
				})
				return x, false, diag
			}
			callee = ref
			changed = true
		}
		out := &syntax.Call{At: x.At, Callee: callee, Name: x.Name, Args: make([]syntax.Value, len(x.Args))}
		for i, a := range x.Args {
			var argChanged bool
			out.Args[i], argChanged, diag = substValue(a, props, src, from, callerSrc, callerScope, diag)
			changed = changed || argChanged
		}
		return out, changed, diag
	case *syntax.Map:
		out := &syntax.Map{At: x.At, Entries: make([]syntax.MapEntry, len(x.Entries))}
		changed := false
		for i, entry := range x.Entries {
			var entryChanged bool
			entry.Value, entryChanged, diag = substValue(entry.Value, props, src, from, callerSrc, callerScope, diag)
			changed = changed || entryChanged
			out.Entries[i] = entry
		}
		return out, changed, diag
	case *syntax.Binary:
		left, lc, diag := substValue(x.Left, props, src, from, callerSrc, callerScope, diag)
		right, rc, diag := substValue(x.Right, props, src, from, callerSrc, callerScope, diag)
		return &syntax.Binary{At: x.At, Op: x.Op, Left: left, Right: right}, lc || rc, diag
	case *syntax.Unary:
		xv, changed, diag := substValue(x.X, props, src, from, callerSrc, callerScope, diag)
		return &syntax.Unary{At: x.At, Op: x.Op, X: xv}, changed, diag
	case *syntax.Ternary:
		cond, cc, diag := substValue(x.Cond, props, src, from, callerSrc, callerScope, diag)
		then, tc, diag := substValue(x.Then, props, src, from, callerSrc, callerScope, diag)
		els, ec, diag := substValue(x.Else, props, src, from, callerSrc, callerScope, diag)
		return &syntax.Ternary{At: x.At, Cond: cond, Then: then, Else: els}, cc || tc || ec, diag
	}
	return v, false, diag
}

// applyOverrides writes the instance's properties onto the fragment root,
// replacing a property the fragment already set instead of duplicating it.
func applyOverrides(el *syntax.Element, props []*syntax.Property, scope string) {
	for _, p := range props {
		p.CallerSrc, p.CallerScope = p.Src, scope
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
		Name:          el.Name,
		NameAt:        el.NameAt,
		Open:          el.Open,
		Close:         el.Close,
		Src:           el.Src,
		Scope:         el.Scope,
		Members:       copyMembers(el.Members),
		Resolved:      el.Resolved,
		InstanceProps: copyInstanceProps(el.InstanceProps),
	}
	return out
}

func copyInstanceProps(in map[string]syntax.InstanceProp) map[string]syntax.InstanceProp {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]syntax.InstanceProp, len(in))
	for name, ip := range in {
		out[name] = ip
	}
	return out
}

// setScope marks a fragment body and its descendants with the instance id.
func setScope(el *syntax.Element, id string) {
	el.Scope = id
	for _, m := range el.Members {
		switch x := m.(type) {
		case *syntax.Element:
			setScope(x, id)
		case *syntax.Block:
			for i := range x.Branches {
				setScopeMembers(x.Branches[i].Members, id)
			}
		}
	}
}

func setScopeMembers(members []syntax.Member, id string) {
	for _, m := range members {
		switch x := m.(type) {
		case *syntax.Element:
			setScope(x, id)
		case *syntax.Block:
			for i := range x.Branches {
				setScopeMembers(x.Branches[i].Members, id)
			}
		}
	}
}

// copyBlock deep-copies a control block, so two instances of one fragment do not
// share arm slices.
func copyBlock(b *syntax.Block) *syntax.Block {
	out := &syntax.Block{
		Kind: b.Kind, At: b.At, Close: b.Close, Src: b.Src,
		CallerSrc: b.CallerSrc, CallerScope: b.CallerScope,
		KeyVar: b.KeyVar, KeyAt: b.KeyAt, ValVar: b.ValVar, ValAt: b.ValAt,
		Range: copyValue(b.Range), RangeAt: b.RangeAt,
		Subject: copyValue(b.Subject), SubjectAt: b.SubjectAt,
	}
	out.Branches = make([]syntax.Branch, len(b.Branches))
	for i, br := range b.Branches {
		vals := make([]syntax.Value, len(br.Vals))
		for j, v := range br.Vals {
			vals[j] = copyValue(v)
		}
		out.Branches[i] = syntax.Branch{
			Cond:    copyValue(br.Cond),
			Vals:    vals,
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
				CallerSrc: v.CallerSrc, CallerScope: v.CallerScope,
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
		return &syntax.Ref{At: x.At, ID: x.ID, Prop: x.Prop, Path: append([]string(nil), x.Path...), Alpha: x.Alpha, HasAlpha: x.HasAlpha}
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
			callee = &syntax.Ref{At: x.Callee.At, ID: x.Callee.ID, Prop: x.Callee.Prop, Path: append([]string(nil), x.Callee.Path...), Alpha: x.Callee.Alpha, HasAlpha: x.Callee.HasAlpha}
		}
		return &syntax.Call{At: x.At, Callee: callee, Name: x.Name, Args: args}
	}
	return v
}
