package cli

import (
	"fmt"

	"github.com/JamunaSoft/servervault/internal/version"
	"github.com/spf13/cobra"
)

// NewVersionCommand builds `servervault version`. Build metadata comes from
// internal/version, set at link time via -ldflags (see Makefile and
// .github/workflows/release.yml) rather than passed in here, so this
// package stays a thin Cobra wrapper with no state of its own.
func NewVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show ServerVault version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := version.Get()
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "ServerVault")
			fmt.Fprintln(out, "Version :", info.Version)
			fmt.Fprintln(out, "Commit  :", info.Commit)
			fmt.Fprintln(out, "Built   :", info.Date)
			fmt.Fprintln(out, "Go      :", info.GoVersion)
			return nil
		},
	}
}
