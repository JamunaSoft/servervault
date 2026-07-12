package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/restic"
	"github.com/spf13/cobra"
)

// NewSnapshotsCommand builds `servervault snapshots`. It is strictly
// read-only: it wraps `restic snapshots --json` and prints the result,
// never modifying the repository. Exit codes: 0 success, 1 the query
// itself failed (repository unreachable, wrong password, etc.), 2
// config or usage error.
func NewSnapshotsCommand() *cobra.Command {
	var configPath string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:           "snapshots",
		Short:         "List Restic snapshots",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: snapshots:", err)
				return &ExitError{Code: 2}
			}
			if errs := config.Validate(cfg); len(errs) > 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: snapshots: invalid configuration:")
				for _, e := range errs {
					fmt.Fprintf(cmd.ErrOrStderr(), "  - %s: %s\n", e.Field, e.Message)
				}
				return &ExitError{Code: 2}
			}

			repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
			snapshots, err := repo.Snapshots(cmd.Context(), restic.SnapshotsOptions{})
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: snapshots:", err)
				return &ExitError{Code: 1}
			}

			if jsonOutput {
				if err := printSnapshotsJSON(cmd.OutOrStdout(), snapshots); err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), "servervault: snapshots:", err)
					return &ExitError{Code: 1}
				}
			} else {
				printSnapshotsTable(cmd.OutOrStdout(), snapshots)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to servervault.yaml (default: "+config.DefaultPath+")")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print snapshots as JSON instead of a table")
	return cmd
}

func printSnapshotsTable(w io.Writer, snapshots []restic.Snapshot) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTIME\tHOST\tTAGS\tPATHS")
	for _, s := range snapshots {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%v\n", s.ID, s.Time.Format("2006-01-02 15:04:05"), s.Hostname, s.Tags, s.Paths)
	}
	tw.Flush()
}

func printSnapshotsJSON(w io.Writer, snapshots []restic.Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(snapshots)
}
