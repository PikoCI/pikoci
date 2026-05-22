package cmd

import (
	"github.com/spf13/cobra"
)

var (
	AppName = "pikoci"
)

var rootCmd = &cobra.Command{
	Use:   AppName,
	Short: "PikoCI is a self-hosted, portable CI/CD system",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(clientCmd)
	rootCmd.AddCommand(workerCmd)
	rootCmd.AddCommand(workerTokenCmd)
	rootCmd.AddCommand(userPasswordCmd)
	rootCmd.AddCommand(runCmd)
}
