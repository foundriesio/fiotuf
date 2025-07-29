package updateclient

import (
	"github.com/spf13/cobra"
)

var (
	srcDir      string
	configPaths []string
	noTuf       bool
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-client",
		Short: "Start update client",
		Run:   doUpdateClient,
	}

	cmd.Flags().StringVarP(&srcDir, "src-dir", "s", "", "Directory that contains an offline update bundle.")
	cmd.Flags().StringSliceVarP(&configPaths, "config", "c", []string{}, "Aktualizr config paths.")
	cmd.Flags().BoolVar(&noTuf, "no-tuf", false, "Disable TUF checking, read targets.json directly.")

	// viper.BindEnv("config", "SOTA_DIR")
	// viper.BindPFlag("src-dir", cmd.Flags().Lookup("src-dir"))
	// viper.BindPFlag("config", cmd.Flags().Lookup("config"))

	return cmd
}
