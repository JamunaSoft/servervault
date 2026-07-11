package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/doctor"
	"github.com/spf13/cobra"
)

// NewDoctorCommand builds `servervault doctor`. It is strictly
// non-destructive: every check reads state and never writes or deletes
// anything. Exit codes follow CLAUDE.md's doctor contract: 0 all required
// checks pass, 1 one or more required checks fail, 2 config or usage error.
func NewDoctorCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:           "doctor",
		Short:         "Run non-destructive environment checks",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "servervault: doctor:", err)
				return &ExitError{Code: 2}
			}

			report := doctor.Run(cmd.Context(), doctor.Options{Config: cfg})
			printReport(cmd.OutOrStdout(), report)

			if report.Failed() {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to servervault.yaml (default: "+config.DefaultPath+")")
	return cmd
}

func printReport(w io.Writer, report doctor.Report) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CHECK\tSTATUS\tDETAIL")
	for _, c := range report.Checks {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Name, c.Status, c.Detail)
	}
	tw.Flush()
}
