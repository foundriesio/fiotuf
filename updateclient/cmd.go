package updateclient

import (
	"github.com/spf13/cobra"
)

var (
	srcDir      string
	configPaths []string
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-client",
		Short: "Start update client",
		Run:   doUpdateClient,
	}

	cmd.Flags().StringVarP(&srcDir, "src-dir", "s", "", "Directory that contains an offline update bundle.")
	cmd.Flags().StringSliceVarP(&configPaths, "config", "c", []string{}, "Aktualizr config paths.")

	// viper.BindEnv("config", "SOTA_DIR")
	// viper.BindPFlag("src-dir", cmd.Flags().Lookup("src-dir"))
	// viper.BindPFlag("config", cmd.Flags().Lookup("config"))

	return cmd
}
