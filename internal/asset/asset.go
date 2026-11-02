// Package asset loads .goi files from an fs.FS, resolving imports into one tree.
//
// A file may pull in fragments:
//
//	import "./Sidebar.goi" as Sidebar
//
//	Window { Row { Sidebar { title: "Mail" } } }
//
// A fragment declares the values it expects and marks where they go:
//
//	param title: ""
//	Column { Text { text: $title } }
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

	"github.com/ksckaan1/goi/internal/syntax"
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
	// Params, the data declarations and the slot flag are kept so callers can
	// tell a fragment from an entry point; the imports are consumed and
	// deliberately dropped.
	return &syntax.File{
		Src: name, Params: f.Params, Vars: f.Vars, Shapes: f.Shapes,
		Fragment: f.Fragment, Root: root,
	}, nil
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
			if child.Name == "$children" {
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
// substituting params, applying the instance's own properties and putting the
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

	// Params start at their defaults and take whatever the instance passed.
	params := make(map[string]syntax.Value, len(f.Params))
	declared := make(map[string]bool, len(f.Params))
	for _, pr := range f.Params {
		params[pr.Name] = pr.Default
		declared[pr.Name] = true
	}
	var overrides []*syntax.Property
	for _, p := range call.Props() {
		if declared[p.Name] {
			params[p.Name] = p.Value
			continue
		}
		overrides = append(overrides, p)
	}

	body := copyElement(f.Root)
	if diag := substitute(body, params, target); !diag.Empty() {
		return nil, diag
	}
	applyOverrides(body, overrides)

	kids := call.Children()
	filled, diag := l.fillSlot(body, kids, callerAliases, append(stack, target))
	if !diag.Empty() {
		return nil, diag
	}
	if !filled && len(kids) > 0 {
		first := kids[0]
		return nil, syntax.Diagnostics{{
			Pos:     first.NameAt,
			End:     first.NameAt + syntax.Pos(len(first.Name)),
			Src:     first.Src,
			Message: fmt.Sprintf("%s does not take children: add $children to %s where they should appear", call.Name, target),
		}}
	}

	expanded, diag := l.expand(body, l.aliases(f), append(stack, target))
	if !diag.Empty() {
		return nil, diag
	}
	return expanded, nil
}

// fillSlot replaces every `$children` in el with the instance's child elements,
// expanded in the caller's file so their own imports resolve there. A slot
// anywhere in the fragment body counts, inside a block arm too, which is how a
// fragment forwards its children. It reports whether a slot was found at all.
func (l *loader) fillSlot(el *syntax.Element, kids []*syntax.Element, callerAliases map[string]string, stack []string) (bool, syntax.Diagnostics) {
	members, found, diag := l.fillMembers(el.Members, kids, callerAliases, stack)
	if !diag.Empty() {
		return found, diag
	}
	el.Members = members
	return found, nil
}

// fillMembers fills the slots in one member list, recursing into blocks and
// nested elements.
func (l *loader) fillMembers(members []syntax.Member, kids []*syntax.Element, callerAliases map[string]string, stack []string) ([]syntax.Member, bool, syntax.Diagnostics) {
	out := make([]syntax.Member, 0, len(members))
	found := false
	for _, m := range members {
		switch x := m.(type) {
		case *syntax.Block:
			for i := range x.Branches {
				arm, nested, diag := l.fillMembers(x.Branches[i].Members, kids, callerAliases, stack)
				if !diag.Empty() {
					return nil, found, diag
				}
				x.Branches[i].Members = arm
				found = found || nested
			}
			out = append(out, x)
		case *syntax.Element:
			if x.Name == "$children" {
				found = true
				for _, k := range kids {
					expanded, diag := l.expand(copyElement(k), callerAliases, stack)
					if !diag.Empty() {
						return nil, found, diag
					}
					out = append(out, expanded)
				}
				continue
			}
			nested, below, diag := l.fillMembers(x.Members, kids, callerAliases, stack)
			if !diag.Empty() {
				return nil, found, diag
			}
			x.Members = nested
			found = found || below
			out = append(out, x)
		default:
			out = append(out, m)
		}
	}
	return out, found, nil
}

// substitute replaces `$name` values in place and reports a param the fragment
// never declared.
func substitute(el *syntax.Element, params map[string]syntax.Value, from string) syntax.Diagnostics {
	var diag syntax.Diagnostics
	var walk func(*syntax.Element)
	walk = func(e *syntax.Element) {
		walkMembers(e.Members, params, from, &diag, walk)
	}
	walk(el)
	return diag
}

// walkMembers substitutes params in a member list, descending into block arms.
func walkMembers(members []syntax.Member, params map[string]syntax.Value, from string, diag *syntax.Diagnostics, walk func(*syntax.Element)) {
	for _, m := range members {
		switch v := m.(type) {
		case *syntax.Property:
			v.Value, *diag = substValue(v.Value, params, v.Src, from, *diag)
		case *syntax.Element:
			walk(v)
		case *syntax.Block:
			for i := range v.Branches {
				br := &v.Branches[i]
				if br.Cond != nil {
					br.Cond, *diag = substValue(br.Cond, params, v.Src, from, *diag)
				}
				walkMembers(br.Members, params, from, diag, walk)
			}
		}
	}
}

// substValue returns a value with every param reference replaced.
func substValue(v syntax.Value, params map[string]syntax.Value, src, from string, diag syntax.Diagnostics) (syntax.Value, syntax.Diagnostics) {
	switch x := v.(type) {
	case *syntax.ParamRef:
		got, ok := params[x.Name]
		if !ok {
			diag = append(diag, syntax.Diagnostic{
				Pos:     x.At,
				End:     x.At + syntax.Pos(len(x.Name)+1),
				Src:     src,
				Message: fmt.Sprintf("%s has no param %q", from, x.Name),
			})
			return x, diag
		}
		return got, diag
	case *syntax.Map:
		out := &syntax.Map{At: x.At, Entries: make([]syntax.MapEntry, len(x.Entries))}
		for i, entry := range x.Entries {
			entry.Value, diag = substValue(entry.Value, params, src, from, diag)
			out.Entries[i] = entry
		}
		return out, diag
	case *syntax.Binary:
		left, diag := substValue(x.Left, params, src, from, diag)
		right, diag := substValue(x.Right, params, src, from, diag)
		return &syntax.Binary{At: x.At, Op: x.Op, Left: left, Right: right}, diag
	case *syntax.Unary:
		xv, diag := substValue(x.X, params, src, from, diag)
		return &syntax.Unary{At: x.At, Op: x.Op, X: xv}, diag
	case *syntax.Ternary:
		cond, diag := substValue(x.Cond, params, src, from, diag)
		then, diag := substValue(x.Then, params, src, from, diag)
		els, diag := substValue(x.Else, params, src, from, diag)
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
		Source: b.Source, SourceAt: b.SourceAt, Var: b.Var, VarAt: b.VarAt,
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

// copyValue clones the composite values; the leaves are immutable.
func copyValue(v syntax.Value) syntax.Value {
	switch x := v.(type) {
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
	}
	return v
}
