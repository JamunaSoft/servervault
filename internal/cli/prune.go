package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/event"
	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/logger"
	"github.com/JamunaSoft/servervault/internal/restic"
	"github.com/JamunaSoft/servervault/internal/retention"
	"github.com/spf13/cobra"
)

// NewPruneCommand builds `servervault prune`. It is a thin wrapper: all
// planning and execution logic lives in internal/retention, which knows
// nothing about Cobra. See docs/retention-flow.md.
//
// Exit codes: 0 success (including a completed --dry-run), 1 the prune
// itself failed (lock busy, plan/execute error, a safety limit refused
// the run), 2 config or usage error.
func NewPruneCommand() *cobra.Command {
	var configPath string
	var dryRun bool
	var yes bool
	var output string

	cmd := &cobra.Command{
		Use:           "prune",
		Short:         "Remove old snapshots according to the configured retention policy",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if output != "text" && output != "json" {
				fmt.Fprintf(cmd.ErrOrStderr(), "servervault: prune: --output must be \"text\" or \"json\", got %q\n", output)
				return &ExitError{Code: 2}
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: prune:", err)
				return &ExitError{Code: 2}
			}
			if errs := config.Validate(cfg); len(errs) > 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: prune: invalid configuration:")
				for _, e := range errs {
					fmt.Fprintf(cmd.ErrOrStderr(), "  - %s: %s\n", e.Field, e.Message)
				}
				return &ExitError{Code: 2}
			}

			repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
			planner, err := retention.NewPlanner(repo, cfg)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: prune:", err)
				return &ExitError{Code: 2}
			}

			plan, err := planner.Plan(cmd.Context())
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: prune: plan:", err)
				return &ExitError{Code: 1}
			}

			if dryRun {
				return printPrunePlan(cmd, plan, output)
			}

			if plan.RemoveCount == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nothing to prune: no snapshots are eligible for removal")
				return nil
			}

			if !yes {
				confirmed, err := confirmPrune(cmd, plan)
				if err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), "servervault: prune:", err)
					return &ExitError{Code: 2}
				}
				if !confirmed {
					fmt.Fprintln(cmd.OutOrStdout(), "prune cancelled: not confirmed")
					return &ExitError{Code: 1}
				}
			}

			log, closeLog, err := logger.New(logger.DefaultOptions())
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: prune:", err)
				return &ExitError{Code: 2}
			}
			defer closeLog()

			jobsPath := filepath.Join(cfg.StateDir, "jobs.db")
			jobStore, err := job.Open(jobsPath)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: prune: open job store:", err)
				return &ExitError{Code: 2}
			}
			defer jobStore.Close()

			eventsPath := filepath.Join(cfg.StateDir, "events.db")
			eventStore, err := event.Open(eventsPath)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: prune: open event store:", err)
				return &ExitError{Code: 2}
			}
			defer eventStore.Close()

			executor, err := retention.NewExecutor(repo, cfg, jobStore, wrapEventSinkWithNotify(cfg, eventStore, log), log)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: prune:", err)
				return &ExitError{Code: 2}
			}

			result, err := executor.Execute(cmd.Context(), plan)
			if err != nil {
				if errors.Is(err, lock.ErrLocked) {
					fmt.Fprintln(cmd.ErrOrStderr(), "servervault: prune: another prune is already running")
				} else {
					fmt.Fprintln(cmd.ErrOrStderr(), "servervault: prune:", err)
				}
				return &ExitError{Code: 1}
			}

			return printPruneResult(cmd, result, output)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to servervault.yaml (default: "+config.DefaultPath+")")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "plan the prune and print it, without removing anything")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the interactive confirmation prompt (required for non-interactive use)")
	cmd.Flags().StringVar(&output, "output", "text", `output format: "text" or "json"`)
	return cmd
}

// confirmPrune prints a summary of what will be removed and reads a
// line from stdin, requiring the exact text "yes" -- matching
// CLAUDE.md's "destructive-sounding execution requires explicit
// confirmation" rule and internal/cli/restore.go's confirm helper
// exactly. A non-interactive caller with no stdin to read (or one that
// doesn't type exactly "yes") is treated as not confirmed, never as
// confirmed by default.
func confirmPrune(cmd *cobra.Command, plan retention.Plan) (bool, error) {
	fmt.Fprintf(cmd.OutOrStdout(), "About to remove %d snapshot(s), leaving %d.\n", plan.RemoveCount, plan.RemainingAfterPrune)
	fmt.Fprint(cmd.OutOrStdout(), `Type "yes" to continue: `)

	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}
	return strings.TrimSpace(line) == "yes", nil
}

func printPrunePlan(cmd *cobra.Command, plan retention.Plan, output string) error {
	if output == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(plan)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "dry run: retention plan")
	fmt.Fprintf(out, "  current snapshots:  %d\n", plan.CurrentSnapshotCount)
	fmt.Fprintf(out, "  to remove:          %d\n", plan.RemoveCount)
	fmt.Fprintf(out, "  remaining after:    %d\n", plan.RemainingAfterPrune)
	if len(plan.RemoveSnapshotIDs) > 0 {
		fmt.Fprintln(out, "  snapshots to remove:")
		for _, id := range plan.RemoveSnapshotIDs {
			fmt.Fprintf(out, "    - %s\n", id)
		}
	}
	fmt.Fprintln(out, "  safety checks:")
	for _, c := range plan.SafetyChecks {
		fmt.Fprintf(out, "    - %s\n", c)
	}
	fmt.Fprintln(out, "no writes were performed (--dry-run)")
	return nil
}

func printPruneResult(cmd *cobra.Command, result retention.Result, output string) error {
	if output == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "prune completed: %d snapshot(s) removed (%s)\n", result.RemovedCount, result.Duration.Round(time.Second))
	return nil
}
