package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/foundriesio/fiotuf/http_get"
	"github.com/foundriesio/fiotuf/register"
	"github.com/foundriesio/fiotuf/tuf_agent_server"
	"github.com/foundriesio/fiotuf/updateclient"
	"github.com/foundriesio/fiotuf/version"
	"github.com/spf13/cobra"
)

func initConfig() {
}

var (
	rootCmd = &cobra.Command{
		Use:               "fiotuf",
		Short:             "Foundries.io device client",
		PersistentPreRunE: rootArgValidation,
	}
)

func init() {
	cobra.EnableTraverseRunHooks = true
	cobra.OnInitialize(initConfig)

	rootCmd.AddCommand(updateclient.NewCommand())
	rootCmd.AddCommand(tuf_agent_server.NewCommand())
	rootCmd.AddCommand(register.NewCommand())
	rootCmd.AddCommand(version.NewCommand())
	rootCmd.AddCommand(http_get.NewCommand())
}

func rootArgValidation(cmd *cobra.Command, args []string) error {
	for pos, val := range args {
		if len(strings.TrimSpace(val)) == 0 {
			return fmt.Errorf("empty values or values containing only white space are not allowed for positional argument at %d", pos)
		}
	}
	return nil
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
