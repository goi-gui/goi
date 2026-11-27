package ui

import "github.com/goi-gui/goi/internal/tree"

// setupBuild is what loadSetups gives the loader in either build. hook resolves
// one file's setup, bind attaches the live tree after it exists, and err reports
// a file whose setup failed to compile while the tree was built.
type setupBuild struct {
	hook func(string, string) tree.SetupScope
	bind func(*tree.Tree)
	err  func() error
}
