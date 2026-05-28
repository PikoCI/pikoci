// Package cmd implements the CLI commands for the PikoCI application.
// It uses cobra to define the root command and all subcommands including
// server, worker, client, run, worker-token, and user-password.
package cmd

import (
	"github.com/spf13/cobra"
)

// AppName is the application name used for CLI identification and XDG paths.
var (
	AppName = "pikoci"
	Version = "dev"
	Commit  = "unknown"
)

// rootCmd is the top-level cobra command for the PikoCI CLI.
var rootCmd = &cobra.Command{
	Use:   AppName,
	Short: "PikoCI is a self-hosted, portable CI/CD system",
}

// Execute runs the root command and returns any error encountered.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.Version = Version + " (" + Commit + ")"
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(clientCmd)
	rootCmd.AddCommand(workerCmd)
	rootCmd.AddCommand(workerTokenCmd)
	rootCmd.AddCommand(userPasswordCmd)
	rootCmd.AddCommand(runCmd)
}
