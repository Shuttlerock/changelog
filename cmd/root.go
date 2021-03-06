package cmd

import (
	log "github.com/sirupsen/logrus"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "changelog",
	Short: "A CLI for working with application changelogs",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

const defaultExitCode = 1

func handleError(err error) {
	if err != nil {
		log.Fatal(err)
		os.Exit(defaultExitCode)
	}
}
