package main

import (
	"github.com/JamunaSoft/servervault/internal/cli"
	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "servervault",
		Short: "Server backup and disaster recovery toolkit",
	}

	rootCmd.AddCommand(cli.NewVersionCommand(Version, Commit, Date))

	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}
