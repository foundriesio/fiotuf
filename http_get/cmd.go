package http_get

import (
	"fmt"
	"github.com/foundriesio/fioconfig/sotatoml"
	"github.com/foundriesio/fioconfig/transport"
	"github.com/spf13/cobra"
	"log"
	"net/url"
	"os"
)

type (
	httpOptions struct {
		// TODO: make it global flag, it should be defined as a global flag in the root command
		configPaths []string
	}
)

func NewCommand() *cobra.Command {
	httpCmd := &cobra.Command{
		Use:   "http <get> <endpoint or full URL> [flags]",
		Short: "Perform an HTTP request to a server by using mTLS credentials",
		Long: `Perform an HTTP request to a server by using mTLS credentials.
The command supports HTTP requests to a specified endpoint or full URL.
By default, it uses the server base URL defined in the configuration file.`,
		Args: cobra.MinimumNArgs(1),
	}

	opts := httpOptions{}

	getCmd := &cobra.Command{
		Use:  "get <endpoint or URL>",
		Args: cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			doGet(cmd, args, &opts)
		},
	}

	// TODO: Add support for other HTTP methods like POST, PUT, PATCH, DELETE
	for _, cmd := range []*cobra.Command{getCmd} {
		// TODO: make it global flag, it should be defined as a global flag in the root command
		cmd.Flags().StringSliceVarP(&opts.configPaths, "config-paths", "c",
			sotatoml.DEF_CONFIG_ORDER, "A comma-separated list of paths to search for .toml configuration files")
		httpCmd.AddCommand(cmd)
	}

	return httpCmd
}

func doGet(cmd *cobra.Command, args []string, opts *httpOptions) {
	config, err := sotatoml.NewAppConfig(opts.configPaths)
	if err != nil {
		log.Println("ERROR - unable to parse the configuration files: ", err)
		os.Exit(1)
	}

	url, err := getUrl(config, args[0])

	client := transport.CreateClient(config)
	resp, err := transport.HttpGet(client, url, nil)
	if err != nil || resp.StatusCode != 200 {
		if err == nil {
			log.Printf("Error:  %s: %s\n", url, resp)
		} else {
			log.Printf("Error:  %s: %v\n", url, err)
		}
		os.Exit(1)
	}
	fmt.Print(resp)
}

func getUrl(cfg *sotatoml.AppConfig, endpointOrUrl string) (string, error) {
	url, err := url.Parse(endpointOrUrl)
	if err != nil {
		return "", fmt.Errorf("invalid URL or endpoint: %w", err)
	}
	if url.Scheme == "https" {
		return url.String(), nil
	}

	server := cfg.Get("tls.server")
	url, err = url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("invalid server base URL in config: %w", err)
	}
	url.Path += endpointOrUrl
	return url.String(), nil
}
