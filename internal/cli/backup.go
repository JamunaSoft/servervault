package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/JamunaSoft/servervault/internal/backup"
	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
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

			engine, err := backup.New(cfg, log, execx.DefaultRunner{})
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
