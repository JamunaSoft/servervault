package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/JamunaSoft/servervault/internal/backup"
	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/event"
	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/logger"
	"github.com/spf13/cobra"
)

// NewBackupCommand builds `servervault backup`. It is a thin wrapper: all
// orchestration logic lives in internal/backup.Engine.Run, which knows
// nothing about Cobra. Exit codes: 0 success, 1 the backup itself failed
// (lock busy, dump/verify/restic failure), 2 config or usage error.
func NewBackupCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:           "backup",
		Short:         "Run a backup",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: backup:", err)
				return &ExitError{Code: 2}
			}
			if errs := config.Validate(cfg); len(errs) > 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: backup: invalid configuration:")
				for _, e := range errs {
					fmt.Fprintf(cmd.ErrOrStderr(), "  - %s: %s\n", e.Field, e.Message)
				}
				return &ExitError{Code: 2}
			}

			log, closeLog, err := logger.New(logger.DefaultOptions())
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: backup:", err)
				return &ExitError{Code: 2}
			}
			defer closeLog()

			// Job/event tracking is best-effort from the CLI's
			// perspective too: a problem opening the state directory
			// logs a warning and the backup still runs untracked,
			// rather than blocking the one thing this tool exists to
			// do -- see internal/backup's package doc comment for the
			// same policy applied inside Engine.Run itself.
			var opts []backup.Option
			if jobStore, err := job.Open(filepath.Join(cfg.StateDir, "jobs.db")); err != nil {
				log.Warn("backup: failed to open job store; continuing without job tracking", "error", err)
			} else {
				defer jobStore.Close()
				if n, err := jobStore.Reconcile(cmd.Context()); err != nil {
					log.Warn("backup: failed to reconcile job store", "error", err)
				} else if n > 0 {
					log.Warn("backup: reconciled jobs left in progress by an unclean previous exit", "count", n)
				}
				opts = append(opts, backup.WithJobStore(jobStore))

				if eventStore, err := event.Open(filepath.Join(cfg.StateDir, "events.db")); err != nil {
					log.Warn("backup: failed to open event store; continuing without event tracking", "error", err)
				} else {
					defer eventStore.Close()
					opts = append(opts, backup.WithEventSink(eventStore))
				}
			}

			engine, err := backup.New(cfg, log, execx.DefaultRunner{}, opts...)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: backup:", err)
				return &ExitError{Code: 2}
			}

			result, err := engine.Run(cmd.Context())
			if err != nil {
				if errors.Is(err, lock.ErrLocked) {
					fmt.Fprintln(cmd.ErrOrStderr(), "servervault: backup: another backup is already running")
				} else {
					fmt.Fprintln(cmd.ErrOrStderr(), "servervault: backup:", err)
				}
				return &ExitError{Code: 1}
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "backup completed: snapshot %s (%s, %d file(s) new, %d changed)\n",
				result.SnapshotID, result.Duration.Round(time.Second), result.FilesNew, result.FilesChanged)
			for _, w := range result.Warnings {
				fmt.Fprintln(out, "  warning:", w)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to servervault.yaml (default: "+config.DefaultPath+")")
	return cmd
}
