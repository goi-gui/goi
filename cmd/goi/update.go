//go:build !js && !wasip1

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newUpdateCmd updates the project's goi dependency and the two tools that go
// with it. goi itself is a separate install: `go install .../cmd/goi@latest`.
func newUpdateCmd(goRun runFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "update the goi dependency, goipls and goifmt",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			steps := []struct {
				label string
				args  []string
			}{
				{"goi dependency", []string{"get", "github.com/goi-gui/goi@latest"}},
				{"goipls", []string{"install", "github.com/goi-gui/goi/cmd/goipls@latest"}},
				{"goifmt", []string{"install", "github.com/goi-gui/goi/cmd/goifmt@latest"}},
			}
			for _, step := range steps {
				fmt.Fprintln(cmd.OutOrStdout(), "goi: updating", step.label)
				if err := goRun(".", step.args, cmd.OutOrStdout(), cmd.ErrOrStderr(), os.Environ()); err != nil {
					return fmt.Errorf("update %s: %w", step.label, err)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "goi: updated goi, goipls and goifmt")
			return nil
		},
	}
}
