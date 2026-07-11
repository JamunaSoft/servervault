package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func NewVersionCommand(version, commit, date string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show ServerVault version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("ServerVault")
			fmt.Println("Version :", version)
			fmt.Println("Commit  :", commit)
			fmt.Println("Built   :", date)
			fmt.Println("Go      :", runtime.Version())
		},
	}
}
