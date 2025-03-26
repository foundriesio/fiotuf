package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/detsch/fiotuf/internal"
	"github.com/foundriesio/fioconfig/sotatoml"
	"github.com/urfave/cli/v2"
)

func NewApp(c *cli.Context) (*internal.App, error) {
	app, err := internal.NewApp(c.StringSlice("config"), c.String("secrets-dir"), c.Bool("unsafe-handlers"), false)
	if err != nil {
		return nil, err
	}
	return app, err
}

func tufAgent(c *cli.Context) error {
	app, err := NewApp(c)
	if err != nil {
		return err
	}
	log.Print("Starting TUF client agent")
	if err := app.StartTufAgent(); err != nil && !errors.Is(err, internal.NotModifiedError) {
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
