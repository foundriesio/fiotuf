package version

import (
	"fmt"

	"github.com/spf13/cobra"
)

var Commit string

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Display version of this command",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(Commit)
		},
	}
	return cmd
}
