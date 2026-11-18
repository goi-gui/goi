//go:build !js && !wasip1

package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// goi setup and goi update do the same work: install the latest CLI tools and,
// inside a Go module, bump the project's goi dependency too. Keeping two names
// matches what people type after installing or after pulling a release.
func newSetupCmd(goRun runFunc) *cobra.Command { return newToolsCmd("setup", goRun) }
func newUpdateCmd(goRun runFunc) *cobra.Command {
	return newToolsCmd("update", goRun)
}

func newToolsCmd(name string, goRun runFunc) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: "install the latest goi, goipls and goifmt",
		Long: "Install goi, goipls and goifmt at latest. Inside a Go module, " +
			"also run go get -u github.com/goi-gui/goi@latest to bump the dependency.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			for _, tool := range []string{"goi", "goipls", "goifmt"} {
				fmt.Fprintln(out, "goi: installing", tool)
				args := []string{"install", "github.com/goi-gui/goi/cmd/" + tool + "@latest"}
				if err := goRun(".", args, out, cmd.ErrOrStderr(), os.Environ()); err != nil {
					return fmt.Errorf("install %s: %w", tool, err)
				}
			}
			if _, err := os.Stat("go.mod"); err == nil {
				fmt.Fprintln(out, "goi: updating the project dependency")
				args := []string{"get", "-u", "github.com/goi-gui/goi@latest"}
				if err := goRun(".", args, out, cmd.ErrOrStderr(), os.Environ()); err != nil {
					return fmt.Errorf("update the goi dependency: %w", err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			fmt.Fprintln(out, "goi: goi, goipls and goifmt are up to date")
			return nil
		},
	}
}
