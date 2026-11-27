package ui

import "github.com/goi-gui/goi/internal/tree"

// setupBuild is what loadSetups gives the loader in either build. hook resolves
// one file's setup, and bind attaches the live tree after it exists.
type setupBuild struct {
	hook func(string) tree.SetupScope
	bind func(*tree.Tree)
}
