package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/event"
	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/logger"
	"github.com/JamunaSoft/servervault/internal/postgres"
	"github.com/JamunaSoft/servervault/internal/restic"
	"github.com/JamunaSoft/servervault/internal/restore"
	"github.com/spf13/cobra"
)

// NewRestoreCommand builds `servervault restore`. It is a thin wrapper:
// all planning and execution logic lives in internal/restore, which
// knows nothing about Cobra. See docs/restore-flow.md.
//
// Exit codes: 0 success (including a completed --dry-run), 1 the restore
// itself failed (lock busy, plan/execute error), 2 config or usage
// error.
func NewRestoreCommand() *cobra.Command {
	var configPath string
	var snapshotID string
	var target string
	var repoPath string
	var database string
	var dryRun bool
	var yes bool
	var output string

	cmd := &cobra.Command{
		Use:           "restore",
		Short:         "Restore a snapshot into staging or a temporary database",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if output != "text" && output != "json" {
				fmt.Fprintf(cmd.ErrOrStderr(), "servervault: restore: --output must be \"text\" or \"json\", got %q\n", output)
				return &ExitError{Code: 2}
			}
			var restoreTarget restore.Target
			switch target {
			case "files":
				restoreTarget = restore.TargetFiles
			case "temp-db":
				restoreTarget = restore.TargetTempDB
			default:
				fmt.Fprintf(cmd.ErrOrStderr(), "servervault: restore: --target must be \"files\" or \"temp-db\", got %q\n", target)
				return &ExitError{Code: 2}
			}
			if snapshotID == "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: restore: --snapshot is required")
				return &ExitError{Code: 2}
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: restore:", err)
				return &ExitError{Code: 2}
			}
			if errs := config.Validate(cfg); len(errs) > 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: restore: invalid configuration:")
				for _, e := range errs {
					fmt.Fprintf(cmd.ErrOrStderr(), "  - %s: %s\n", e.Field, e.Message)
				}
				return &ExitError{Code: 2}
			}

			repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
			planner, err := restore.NewPlanner(repo, cfg)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: restore:", err)
				return &ExitError{Code: 2}
			}

			plan, err := planner.Plan(cmd.Context(), restore.PlanOptions{
				SnapshotID: snapshotID,
				Target:     restoreTarget,
				Path:       repoPath,
				Database:   database,
			})
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: restore: plan:", err)
				return &ExitError{Code: 1}
			}

			if dryRun {
				return printPlan(cmd, plan, output)
			}

			if !yes {
				confirmed, err := confirm(cmd, plan)
				if err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), "servervault: restore:", err)
					return &ExitError{Code: 2}
				}
				if !confirmed {
					fmt.Fprintln(cmd.OutOrStdout(), "restore cancelled: not confirmed")
					return &ExitError{Code: 1}
				}
			}

			log, closeLog, err := logger.New(logger.DefaultOptions())
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: restore:", err)
				return &ExitError{Code: 2}
			}
			defer closeLog()

			jobsPath := filepath.Join(cfg.StateDir, "jobs.db")
			jobStore, err := job.Open(jobsPath)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: restore: open job store:", err)
				return &ExitError{Code: 2}
			}
			defer jobStore.Close()

			eventsPath := filepath.Join(cfg.StateDir, "events.db")
			eventStore, err := event.Open(eventsPath)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: restore: open event store:", err)
				return &ExitError{Code: 2}
			}
			defer eventStore.Close()

			var pgClient restore.PostgresClient
			if cfg.Postgres.Enabled {
				pgClient = postgres.New(execx.DefaultRunner{}, cfg.Postgres)
			}

			executor, err := restore.NewExecutor(repo, pgClient, cfg, jobStore, eventStore, log)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: restore:", err)
				return &ExitError{Code: 2}
			}

			result, err := executor.Execute(cmd.Context(), plan)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: restore:", err)
				return &ExitError{Code: 1}
			}

			return printResult(cmd, result, output)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to servervault.yaml (default: "+config.DefaultPath+")")
	cmd.Flags().StringVar(&snapshotID, "snapshot", "", "snapshot ID to restore from (required)")
	cmd.Flags().StringVar(&target, "target", "", `restore target: "files" or "temp-db" (required)`)
	cmd.Flags().StringVar(&repoPath, "path", "", "restrict a files restore to this path within the snapshot")
	cmd.Flags().StringVar(&database, "database", "", "configured database name to restore (temp-db only; defaults to the configured database)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "plan the restore and print it, without performing any writes")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the interactive confirmation prompt (required for non-interactive use)")
	cmd.Flags().StringVar(&output, "output", "text", `output format: "text" or "json"`)
	return cmd
}

// confirm prints a summary of what will happen and reads a line from
// stdin, requiring the exact text "yes" -- matching CLAUDE.md's
// "destructive-sounding execution requires explicit confirmation" rule.
// A non-interactive caller with no stdin to read (or one that doesn't
// type exactly "yes") is treated as not confirmed, never as confirmed by
// default -- see docs/restore-flow.md.
func confirm(cmd *cobra.Command, plan restore.Plan) (bool, error) {
	fmt.Fprintf(cmd.OutOrStdout(), "About to restore snapshot %s (%s) into %s.\n", plan.SnapshotID, plan.Target, plan.Destination)
	if plan.Target == restore.TargetTempDB {
		fmt.Fprintf(cmd.OutOrStdout(), "A new temporary database will be created: %s\n", plan.TempDatabaseName)
	}
	fmt.Fprint(cmd.OutOrStdout(), `Type "yes" to continue: `)

	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		// EOF (no stdin available, e.g. running non-interactively without
		// --yes) is not an error condition for confirm itself -- it's
		// simply "not confirmed."
		return false, nil
	}
	return strings.TrimSpace(line) == "yes", nil
}

func printPlan(cmd *cobra.Command, plan restore.Plan, output string) error {
	if output == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(plan)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "dry run: snapshot %s -> %s\n", plan.SnapshotID, plan.Target)
	fmt.Fprintf(out, "  destination:       %s\n", plan.Destination)
	if plan.Target == restore.TargetTempDB {
		fmt.Fprintf(out, "  temporary database: %s\n", plan.TempDatabaseName)
	}
	if plan.RepositoryPath != "" {
		fmt.Fprintf(out, "  repository path:   %s\n", plan.RepositoryPath)
	}
	fmt.Fprintf(out, "  expected files:    %d\n", plan.ExpectedFiles)
	fmt.Fprintf(out, "  expected bytes:    %d\n", plan.ExpectedBytes)
	fmt.Fprintf(out, "  required commands: %s\n", strings.Join(plan.RequiredCommands, ", "))
	fmt.Fprintln(out, "  safety checks:")
	for _, c := range plan.SafetyChecks {
		fmt.Fprintf(out, "    - %s\n", c)
	}
	fmt.Fprintln(out, "no writes were performed (--dry-run)")
	return nil
}

func printResult(cmd *cobra.Command, result restore.Result, output string) error {
	if output == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "restore completed: job %s, %d file(s), %d byte(s), landed in %s (%s)\n",
		result.JobID, result.FilesRestored, result.BytesRestored, result.Destination, result.Duration.Round(1e6))
	return nil
}
