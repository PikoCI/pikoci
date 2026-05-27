package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// workerTokenCmd is the cobra command that generates a worker authentication token
// from a JWT secret, for use by standalone workers connecting to the server.
var workerTokenCmd = &cobra.Command{
	Use:   "worker-token",
	Short: "Generate a worker authentication token",
	RunE: func(cmd *cobra.Command, args []string) error {
		js, _ := cmd.Flags().GetString("jwt-secret")
		if js == "" {
			return fmt.Errorf("--jwt-secret is required")
		}
		fmt.Println(generateWorkerJWT([]byte(js)))
		return nil
	},
}

func init() {
	workerTokenCmd.Flags().String("jwt-secret", "", "JWT secret used by the server")
}
