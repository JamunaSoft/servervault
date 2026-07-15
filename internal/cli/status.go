package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/health"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/restic"
	"github.com/spf13/cobra"
)

// NewStatusCommand builds `servervault status`. Like doctor, it is
// strictly non-destructive and does not call config.Validate before
// running -- internal/health degrades to StatusUnknown for anything
// unconfigured rather than needing a valid config to run at all, the
// same design doctor.Run itself already relies on. Exit codes mirror
// doctor's contract: 0 all checks pass, 1 one or more checks fail
// (StatusFail; StatusWarn/StatusUnknown do not fail the exit code),
// 2 config or usage error.
func NewStatusCommand() *cobra.Command {
	var configPath string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:           "status",
		Short:         "Show current operational health: repository reachability, lock state, and recent job history",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: status:", err)
				return &ExitError{Code: 2}
			}

			var resticChecker health.ResticAccessChecker
			if cfg.Restic.Repository != "" {
				resticChecker = restic.New(execx.DefaultRunner{}, cfg.Restic)
			}

			// Best-effort, matching internal/doctor.Run's identical
			// nil-fallback: a job store this command can't open (e.g.
			// an unwritable state_dir) leaves Jobs nil, which
			// internal/health reports as StatusUnknown per job-history
			// check, not a reason to refuse running status entirely.
			var jobs health.JobLister
			if cfg.StateDir != "" {
				if jobStore, err := job.Open(filepath.Join(cfg.StateDir, "jobs.db")); err == nil {
					defer jobStore.Close()
					jobs = jobStore
				}
			}

			report := health.Run(cmd.Context(), health.Options{
				Config: cfg,
				Restic: resticChecker,
				Jobs:   jobs,
			})

			if jsonOutput {
				if err := printHealthReportJSON(cmd.OutOrStdout(), report); err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), "servervault: status:", err)
					return &ExitError{Code: 2}
				}
			} else {
				printHealthReport(cmd.OutOrStdout(), report)
			}

			if report.Failed() {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to servervault.yaml (default: "+config.DefaultPath+")")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print the report as JSON instead of a table")
	return cmd
}

func printHealthReport(w io.Writer, report health.Report) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CHECK\tSTATUS\tDETAIL")
	for _, c := range report.Checks {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Name, c.Status, c.Detail)
	}
	tw.Flush()
}

func printHealthReportJSON(w io.Writer, report health.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
