package cli

import (
	"fmt"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/spf13/cobra"
)

// NewConfigCommand builds the `servervault config` command group.
func NewConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate ServerVault configuration",
	}
	cmd.AddCommand(NewConfigValidateCommand())
	return cmd
}

// NewConfigValidateCommand builds `servervault config validate`. It only
// checks the configuration's shape (see internal/config.Validate) — it
// never touches the filesystem beyond reading the YAML file itself.
// Environment-reality checks (does the password file exist, are backup
// paths readable) belong to `servervault doctor`.
func NewConfigValidateCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:           "validate",
		Short:         "Validate the ServerVault configuration",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: config validate:", err)
				return &ExitError{Code: 2}
			}

			errs := config.Validate(cfg)
			if len(errs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "configuration is valid")
				return nil
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "configuration is invalid: %d issue(s) found\n", len(errs))
			for _, e := range errs {
				fmt.Fprintf(out, "  - %s: %s\n", e.Field, e.Message)
			}
			return &ExitError{Code: 1}
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to servervault.yaml (default: "+config.DefaultPath+")")
	return cmd
}
