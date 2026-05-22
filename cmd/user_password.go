package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/pikoci/pikoci/pikoci/utils"
)

const (
	userPasswordSeparator = ":"
)

var userPasswordCmd = &cobra.Command{
	Use:   "user-password",
	Short: "This is a helper if you want to add users via configuration. It generates the right value to set",
	RunE: func(cmd *cobra.Command, args []string) error {
		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")

		hash, err := utils.HashPassword(password)
		if err != nil {
			return err
		}

		fmt.Printf("%s%s%s\n", username, userPasswordSeparator, hash)
		return nil
	},
}

func init() {
	userPasswordCmd.Flags().StringP("username", "u", "", "Username of the user")
	userPasswordCmd.Flags().StringP("password", "p", "", "Plain password of the user")
	userPasswordCmd.MarkFlagRequired("username")
	userPasswordCmd.MarkFlagRequired("password")
}
