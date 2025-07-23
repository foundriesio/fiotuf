package http_get

import (
	"fmt"
	"log"
	"os"

	"github.com/foundriesio/fioconfig/sotatoml"
	"github.com/foundriesio/fioconfig/transport"
	"github.com/spf13/cobra"
)

var (
	url      string
	logLevel int
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "http-get",
		Short: "Perform an HTTP GET request using Foundries.io credentials",
		Run:   doDockerGet,
	}

	cmd.Flags().StringVarP(&url, "url", "u", "", "url to get, mandatory")
	cmd.Flags().IntVar(&logLevel, "loglevel", 2, "set log level 0-5 (trace, debug, info, warning, error, fatal)")

	// viper.BindEnv("config", "SOTA_DIR")
	// viper.BindPFlag("src-dir", cmd.Flags().Lookup("src-dir"))
	// viper.BindPFlag("config", cmd.Flags().Lookup("config"))

	return cmd
}

func doDockerGet(cmd *cobra.Command, args []string) {
	if url == "" {
		cmd.Help()
		return
	}

	configPaths := sotatoml.DEF_CONFIG_ORDER
	config, err := sotatoml.NewAppConfig(configPaths)
	if err != nil {
		log.Println("ERROR - unable to decode sota.toml:", err)
		os.Exit(1)
	}

	client := transport.CreateClient(config)
	resp, err := transport.HttpGet(client, url, map[string]string{})
	if err != nil || resp.StatusCode != 200 {
		log.Printf("Error fetching URL %s: %v\n", url, err)
		os.Exit(1)
	}

	fmt.Print(string(resp.Body))
}
