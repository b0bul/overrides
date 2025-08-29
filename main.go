package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/b0bul/override/config"
	"github.com/b0bul/override/overrides"
	"github.com/b0bul/override/provider"
	"github.com/urfave/cli/v2"
)

var Version = fmt.Sprintf("v1.7.0-rc.8 complied with %v on %v", runtime.Version(), runtime.GOOS)
var app overrides.Override

func main() {

	co := config.GetConfig()
	app = overrides.InitializeOverrideApp(co)

	help := &cli.App{
		Name:                 "Overrides",
		Usage:                "Enable local terraform plans",
		EnableBashCompletion: true,
		Commands: []*cli.Command{
			{
				Name:    "version",
				Aliases: []string{"v"},
				Usage:   "Show application version",
				Action: func(cCtx *cli.Context) error {
					fmt.Println(Version)
					return nil
				},
			},
			{
				Name:    "apply",
				Aliases: []string{"a"},
				Usage:   "Create an overrides.tf file backing up the provders.tf file",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "alias",
						Required: false,
						Usage:    "If an environment alias is passed, replace the unaliased mapping.hcl profile environment with the new alias",
						Action: func(cCtx *cli.Context, alias string) error {
							app.SetAlias(alias)
							return nil
						},
					},
					&cli.BoolFlag{
						Name:  "verbose",
						Value: false,
						Usage: "increased logging verbosity",
						Action: func(cCtx *cli.Context, verbose bool) error {
							app.VerboseLogging(verbose)
							return nil
						},
					},
				},
				Action: func(cCtx *cli.Context) error {
					if app.Verbose {
						log.Println("--- Restoring working directory")
					}

					currentProviderFile, backedupProviderFile := provider.RestoreProvider(app.Verbose, app.OverrideProviderFile)
					// detected based on filenames on disk, passed as app state for use later
					app.ProviderFile = currentProviderFile
					app.ProviderFileBackup = backedupProviderFile

					provider.BackupProvider(app.Verbose, app.OverrideProviderFile, app.ProviderFile, app.ProviderFileBackup)
					if app.Verbose {
						log.Println("--- Starting Overrides")
					}

					if app.Verbose {
						log.Println("--- Writing overrides")
					}
					app.WriteOverrideProvidersFileDynamic()
					log.Println("Overrides applied")
					return nil
				},
			},
			{
				Name:    "refresh",
				Aliases: []string{"r"},
				Usage:   "Manages ~/.aws/config and credentials files, allowing estate wide read access for tf plans",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "chunks",
						Value: 4,
						Usage: "Set a new batchsize with which to process account roles",
						Action: func(cCtx *cli.Context, bsize int) error {
							app.BatchSize(bsize)
							return nil
						},
					},
					&cli.IntFlag{
						Name:  "threads",
						Value: 12,
						Usage: "Number of workers to start by default",
						Action: func(cCtx *cli.Context, wcount int) error {
							app.SetWorkers(wcount)
							return nil
						},
					},
					&cli.BoolFlag{
						Name:  "verbose",
						Value: false,
						Usage: "increased logging verbosity",
						Action: func(cCtx *cli.Context, verbose bool) error {
							app.VerboseLogging(verbose)
							return nil
						},
					},
					&cli.BoolFlag{
						Name:  "use-credentials-file",
						Value: false,
						Usage: "the default is sso, however if you use this flag your ~/.aws/credentials will be updated and take precedence (default on windows)",
						Action: func(cCtx *cli.Context, cf bool) error {
							app.UseAwsCredentialsFile(cf)
							return nil
						},
					},
				},
				Action: func(cCtx *cli.Context) error {
					if app.Verbose {
						log.Println("--- Starting refresh")
					}

					// if refresh, rewrite aws config file and potentially aws credentials file if enabled,  populates app state based account structure
					app.Overrides()

					if app.UseCredentialsFile {
						app.WriteAwsCredentialsFile()
					}

					app.WriteSsoProfiles()
					// if windows need written sso profile to take a back seat
					return nil
				},
			},
			{
				Name:    "show",
				Aliases: []string{"p"},
				Usage:   "List all known ReadOnly and Contributor profiles for all aws accounts",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "batchsize",
						Value: 4,
						Usage: "Set a new batchsize with which to process account roles",
						Action: func(cCtx *cli.Context, bsize int) error {
							app.BatchSize(bsize)
							return nil
						},
					},
					&cli.BoolFlag{
						Name:  "verbose",
						Value: false,
						Usage: "Increased logging verbosity",
						Action: func(cCtx *cli.Context, verbose bool) error {
							app.VerboseLogging(verbose)
							return nil
						},
					},
				},
				Action: func(cCtx *cli.Context) error {
					if app.Verbose {
						log.Println("Known profiles:")
					}
					app.ListProfiles()
					return nil
				},
			},
			{
				Name:    "restore",
				Aliases: []string{"r"},
				Usage:   "Resotre a providers.tf file and remove the overrides.tf file",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "verbose",
						Value: false,
						Usage: "Increased logging verbosity",
						Action: func(cCtx *cli.Context, verbose bool) error {
							app.VerboseLogging(verbose)
							return nil
						},
					},
				},
				Action: func(cCtx *cli.Context) error {
					if app.Verbose {
						log.Println("Restoring providers file")
					}
					provider.RestoreProvider(app.Verbose, app.OverrideProviderFile)
					log.Println("Done")
					return nil
				},
			},
			{
				Name:    "validate",
				Aliases: []string{"x"},
				Usage:   "check the validity of the contents of ~/.aws/sso/",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "refresh",
						Value: false,
						Usage: "refresh the contents of ~/.aws/sso/",
						Action: func(cCtx *cli.Context, verbose bool) error {
							fmt.Println("to be implemeneted")
							return nil
						},
					},
				},
				Action: func(cCtx *cli.Context) error {
					fmt.Println("to be implemeneted")
					return nil
				},
			},
			{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "interface for getting, setting and listing override internal config",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "show",
						Value: false,
						Usage: "display internal config state of the app",
						Action: func(cCtx *cli.Context, verbose bool) error {
							co.Show()
							return nil
						},
					},
					&cli.BoolFlag{
						Name:  "set",
						Value: false,
						Usage: "set opotion to the internal config state of the app",
						Action: func(cCtx *cli.Context, verbose bool) error {
							args := cCtx.Args().Slice()
							co.Set(args[0], args[1])
							return nil
						},
					},
					&cli.BoolFlag{
						Name:  "reset-to-default-override-config",
						Value: false,
						Usage: "reset all opotions to the internal defaults of the app back to default",
						Action: func(cCtx *cli.Context, reset bool) error {
							now := time.Now()
							stamp := fmt.Sprintf("%d", now.Unix())
							backedUpDefaultConfig := co.DefaultConfigFile + "-" + stamp
							err := os.Rename(co.DefaultConfigFile, backedUpDefaultConfig)
							if err != nil {
								fmt.Println("error resetting config file", err)
							}
							return nil
						},
					},
					&cli.BoolFlag{
						Name:  "reset-to-default-aws-sso-config",
						Value: false,
						Usage: "reset all opotions to the internal defaults of the app back to default",
						Action: func(cCtx *cli.Context, reset bool) error {
							co.ResetAwsSsoConfig(reset)
							return nil
						},
					},
				},
				Action: func(cCtx *cli.Context) error {
					return nil
				},
			},
		},
	}

	if err := help.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
