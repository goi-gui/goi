package lsp

import "runtime/debug"

// Version is the module version the binary was built at, or "dev" for a
// checkout. An editor shows it, so it says which design vocabulary the server
// speaks: a goipls installed at vX.Y.Z completes the properties of vX.Y.Z.
func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}
