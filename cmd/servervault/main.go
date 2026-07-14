package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/JamunaSoft/servervault/internal/cli"
	"github.com/spf13/cobra"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	rootCmd := &cobra.Command{
		Use:   "servervault",
		Short: "Server backup and disaster recovery toolkit",
	}

	rootCmd.AddCommand(cli.NewVersionCommand())
	rootCmd.AddCommand(cli.NewDoctorCommand())
	rootCmd.AddCommand(cli.NewConfigCommand())
	rootCmd.AddCommand(cli.NewBackupCommand())
	rootCmd.AddCommand(cli.NewSnapshotsCommand())
	rootCmd.AddCommand(cli.NewRestoreCommand())
	rootCmd.AddCommand(cli.NewPruneCommand())

	err := rootCmd.ExecuteContext(ctx)
	os.Exit(cli.ExitCode(err))
}
