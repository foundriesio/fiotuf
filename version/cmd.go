package version

import (
	"log"

	"github.com/spf13/cobra"
)

var Commit string

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Display version of this command",
		Run: func(cmd *cobra.Command, args []string) {
			log.Println(Commit)
		},
	}
	return cmd
}
