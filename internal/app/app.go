package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime/debug"

	"github.com/snapp-incubator/S3-Panel/internal/logging"

	"github.com/urfave/cli/v3"

	"github.com/snapp-incubator/S3-Panel/internal/api"
	"github.com/snapp-incubator/S3-Panel/internal/config"
)

func Execute() {
	var configPath string
	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	cmd := &cli.Command{
		Name:        "s3-panel",
		Description: "S3 Panel — API for managing S3-compatible object storage",
		Commands: []*cli.Command{
			{
				Name:  "s3-panel",
				Usage: "run the s3-panel server",
				Action: func(_ context.Context, _ *cli.Command) error {
					cfg := config.Provide(configPath)
					logger := logging.Provide(cfg.Logger)

					return api.StartServer(cancelCtx, cancelFunc, cfg, logger)
				},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        "configPath",
						Value:       "./config.toml",
						Usage:       "Path to config file",
						Destination: &configPath,
						OnlyOnce:    false,
					},
				},
			},
		},
		Version: func() string {
			revision := ""
			timestamp := ""
			modified := ""

			if info, ok := debug.ReadBuildInfo(); ok {
				for _, setting := range info.Settings {
					switch setting.Key {
					case "vcs.revision":
						revision = setting.Value
					case "vcs.time":
						timestamp = setting.Value
					case "vcs.modified":
						modified = setting.Value
					}
				}
			}

			if revision == "" {
				return ""
			}

			if modified == "true" {
				return fmt.Sprintf("%s (%s) [dirty]", revision, timestamp)
			}

			return fmt.Sprintf("%s (%s)", revision, timestamp)
		}(),
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
