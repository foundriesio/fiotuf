package main

import (
	"fmt"
	"log"
	"os"

	"github.com/detsch/fiotuf/internal"
	"github.com/foundriesio/fioconfig/sotatoml"
	"github.com/urfave/cli/v2"
)


func tufAgent(c *cli.Context) error {
	configPaths := c.StringSlice("config")
	if len(configPaths) == 0 {
		configPaths = sotatoml.DEF_CONFIG_ORDER
	}

	config, err := sotatoml.NewAppConfig(configPaths)
	if err != nil {
		fmt.Println("ERROR - unable to decode sota.toml:", err)
		os.Exit(1)
	}
	log.Print("Starting TUF client agent")
	err = internal.StartTufAgent(config)
	if err != nil {
		return err
	}
	return nil
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
		},
		Commands: []*cli.Command{
			{
				Name:  "start-agent",
				Usage: "Start TUF client agent",
				Action: func(c *cli.Context) error {
					return tufAgent(c)
				},
			},
			{
				Name:  "version",
				Usage: "Display version of this command",
				Action: func(c *cli.Context) error {
					fmt.Println(internal.Commit)
					return nil
				},
			},
		},
		DefaultCommand: "start-agent",
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
