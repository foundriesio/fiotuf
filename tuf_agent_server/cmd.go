package tuf_agent_server

import (
	"fmt"
	"log"
	"os"

	"github.com/foundriesio/fioconfig/sotatoml"
	"github.com/spf13/cobra"
)

var (
	srcDir      string
	configPaths []string
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start-http-agent",
		Short: "Start TUF client HTTP agent",
		Run:   doTufHttpAgent,
	}

	cmd.Flags().StringVarP(&srcDir, "src-dir", "s", "", "Directory that contains an offline update bundle.")
	cmd.Flags().StringSliceVarP(&configPaths, "config", "c", []string{}, "Aktualizr config paths.")

	return cmd
}

func doTufHttpAgent(cmd *cobra.Command, args []string) {
	err := tufHttpAgent()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting TUF HTTP agent: %v\n", err)
		os.Exit(1)
	}
}

func tufHttpAgent() error {
	if len(configPaths) == 0 {
		configPaths = sotatoml.DEF_CONFIG_ORDER
	}

	config, err := sotatoml.NewAppConfig(configPaths)
	if err != nil {
		log.Println("ERROR - unable to decode sota.toml:", err)
		os.Exit(1)
	}
	log.Print("Starting TUF client HTTP agent")
	err = StartTufAgent(config)
	if err != nil {
		return err
	}
	return nil
}
