package gosetup

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Import is one import declaration after setup's imports are normalized. Name
// is the local alias when the source had one, and "" for the default.
type Import struct {
	Name string
	Path string
}

// StdPackages maps a standard library package name to its import path, for the
// packages the interpreter can run. Setup code may use `fmt.Println` without an
// import; this table is what makes that an import of "fmt".
func StdPackages() map[string]string { return stdPackages }

// PrepareImports adds the standard packages a setup body refers to and drops
// the imports it does not use. body keeps the original byte layout, with the
// import declarations blanked, so a diagnostic offset still points into code.
func PrepareImports(code string) (body string, imports []Import) {
	const header = "package setup;"
	f, err := parser.ParseFile(token.NewFileSet(), "_.go", header+code, parser.ParseComments)
	if err != nil {
		return code, nil
	}
	existing := map[string]Import{}
	var order []string
	for _, d := range f.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gen.Specs {
			im := spec.(*ast.ImportSpec)
			p, err := strconv.Unquote(im.Path.Value)
			if err != nil {
				continue
			}
			name := ""
			if im.Name != nil {
				name = im.Name.Name
			}
			local := name
			if local == "" {
				local = path.Base(p)
			}
			existing[local] = Import{Name: name, Path: p}
			order = append(order, local)
		}
	}

	used := map[string]bool{}
	var added []Import
	seen := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if _, ok := existing[id.Name]; ok {
			used[id.Name] = true
			return true
		}
		// A declared name (variable, parameter, type) is not a package.
		if id.Obj != nil {
			return true
		}
		p, ok := stdPackages[id.Name]
		if !ok || seen[p] {
			return true
		}
		seen[p] = true
		added = append(added, Import{Path: p})
		return true
	})

	for _, local := range order {
		im := existing[local]
		if im.Name == "." || im.Name == "_" || used[local] {
			imports = append(imports, im)
		}
	}
	imports = append(imports, added...)
	sort.Slice(imports, func(i, j int) bool { return imports[i].Path < imports[j].Path })

	// Blank the import declarations, keeping every newline so line numbers and
	// diagnostic offsets do not move.
	body = code
	for _, d := range f.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		start := int(gen.Pos()) - 1 - len(header)
		end := int(gen.End()) - 1 - len(header)
		if start < 0 || end > len(body) || start >= end {
			continue
		}
		blank := make([]byte, end-start)
		for i, c := range body[start:end] {
			if c == '\n' {
				blank[i] = '\n'
			} else {
				blank[i] = ' '
			}
		}
		body = body[:start] + string(blank) + body[end:]
	}
	return body, imports
}

// ImportBlock writes imports as one Go import declaration.
func ImportBlock(imports []Import) string {
	if len(imports) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("import (\n")
	for _, im := range imports {
		if im.Name != "" {
			fmt.Fprintf(&b, "%s %q\n", im.Name, im.Path)
		} else {
			fmt.Fprintf(&b, "%q\n", im.Path)
		}
	}
	b.WriteString(")\n\n")
	return b.String()
}
