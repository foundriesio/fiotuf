package main

import (
	"log"
	"os"

	"github.com/detsch/fiotuf/internal"
	"github.com/detsch/fiotuf/updateclient"
	"github.com/foundriesio/fioconfig/sotatoml"
	"github.com/urfave/cli/v2"
)

func tufHttpAgent(c *cli.Context) error {
	configPaths := c.StringSlice("config")
	if len(configPaths) == 0 {
		configPaths = sotatoml.DEF_CONFIG_ORDER
	}

	config, err := sotatoml.NewAppConfig(configPaths)
	if err != nil {
		log.Println("ERROR - unable to decode sota.toml:", err)
		os.Exit(1)
	}
	log.Print("Starting TUF client HTTP agent")
	err = internal.StartTufAgent(config)
	if err != nil {
		return err
	}
	return nil
}

func updateClient(c *cli.Context) error {
	srcDir := c.String("src-dir")

	return updateclient.RunUpdateClient(srcDir)
}

func main() {
	app := &cli.App{
		Name:  "fiotuf",
		Usage: "A TUF client agent",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Value:   cli.NewStringSlice(sotatoml.DEF_CONFIG_ORDER...),
				Usage:   "Aktualizr config paths",
				EnvVars: []string{"SOTA_DIR"},
			},
			&cli.StringFlag{
				Name:    "src-dir",
				Aliases: []string{"s"},
				Value:   "",
				Usage:   "Directory that contains an offline update bundle",
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "start-http-agent",
				Usage: "Start TUF client HTTP agent",
				Action: func(c *cli.Context) error {
					return tufHttpAgent(c)
				},
			},
			{
				Name:  "update-client",
				Usage: "Start update client",
				Action: func(c *cli.Context) error {
					return updateClient(c)
				},
			},
			{
				Name:  "version",
				Usage: "Display version of this command",
				Action: func(c *cli.Context) error {
					log.Println(internal.Commit)
					return nil
				},
			},
		},
		DefaultCommand: "start-http-agent",
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
