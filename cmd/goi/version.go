//go:build !js && !wasip1

package main

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "print the goi version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "goi", programVersion())
		},
	}
}

// programVersion is the main module version Go recorded at build time. A local
// checkout built with go build has no release version, so it reports devel.
func programVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	version := info.Main.Version
	if version == "" || version == "(devel)" {
		return "devel"
	}
	return version
}
