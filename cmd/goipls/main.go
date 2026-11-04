// Command goipls is the language server for .goi design files.
//
// It speaks the Language Server Protocol over stdio, so an editor starts it and
// talks JSON-RPC on the process' stdin and stdout:
//
//	goipls
//
// It provides diagnostics from the parser and the schema (including errors that
// live in an imported fragment), completion for elements, properties, enum
// values, colour roles, params, `bridge.` handlers and event calls, hover docs,
// a document outline, folding and go-to-definition for imports.
//
// The Zed extension (github.com/goi-gui/zed-goi) starts it automatically when
// it is on PATH:
//
//	go install github.com/ksckaan1/goi/cmd/goipls@latest
package main

import (
	"fmt"
	"os"

	"github.com/ksckaan1/goi/internal/lsp"
)

func main() {
	if err := lsp.New(os.Stdin, os.Stdout).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "goipls:", err)
		os.Exit(1)
	}
}
