package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/spf13/cobra"
)

var apiTokensCmd = &cobra.Command{
	Use:   "api-tokens",
	Short: "Manage API tokens for authenticated access",
}

func init() {
	apiTokensCmd.AddCommand(apiTokensCreateCmd)
	apiTokensCmd.AddCommand(apiTokensListCmd)
	apiTokensCmd.AddCommand(apiTokensDeleteCmd)
	apiTokensCmd.AddCommand(apiTokensUseCmd)
}

var apiTokensCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Creates a new API token",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		name, _ := cmd.Flags().GetString("name")
		personal, _ := cmd.Flags().GetBool("personal")
		teamCanonical, _ := cmd.Flags().GetString("team-canonical")
		roleStr, _ := cmd.Flags().GetString("role")
		expiresAtStr, _ := cmd.Flags().GetString("expires-at")

		if !personal && teamCanonical == "" {
			return fmt.Errorf("either --personal or --team-canonical/--role must be specified")
		}
		if personal && (teamCanonical != "" || roleStr != "") {
			return fmt.Errorf("--personal and --team-canonical/--role are mutually exclusive")
		}
		if !personal && roleStr == "" {
			return fmt.Errorf("--role is required when --team-canonical is specified")
		}

		var expiresAt *time.Time
		if expiresAtStr != "" {
			t, err := time.Parse(time.RFC3339, expiresAtStr)
			if err != nil {
				return fmt.Errorf("invalid expires-at format, use RFC3339: %w", err)
			}
			expiresAt = &t
		}

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		// username is empty for client calls - the server extracts it from JWT
		token, err := c.CreateApiToken(cmd.Context(), "", name, personal, teamCanonical, role.Role(roleStr), expiresAt)
		if err != nil {
			return fmt.Errorf("failed to create API token: %w", err)
		}

		printJSON(token)
		return nil
	},
}

func init() {
	apiTokensCreateCmd.Flags().String("name", "", "Name for the token (unique per user)")
	apiTokensCreateCmd.Flags().Bool("personal", false, "Create a personal token with full user access")
	apiTokensCreateCmd.Flags().String("team-canonical", "", "Team canonical for a team-scoped token")
	apiTokensCreateCmd.Flags().String("role", "", "Role cap for a team-scoped token (read, write, maintain, admin)")
	apiTokensCreateCmd.Flags().String("expires-at", "", "Optional expiration in RFC3339 format (e.g. 2026-12-31T23:59:59Z)")
	apiTokensCreateCmd.MarkFlagRequired("name")
}

var apiTokensListCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists all API tokens for the authenticated user",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		tokens, err := c.ListApiTokens(cmd.Context(), "")
		if err != nil {
			return fmt.Errorf("failed to list API tokens: %w", err)
		}

		printJSON(tokens)
		return nil
	},
}

var apiTokensDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Deletes an API token",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		id, _ := cmd.Flags().GetUint32("id")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		err = c.DeleteApiToken(cmd.Context(), "", id)
		if err != nil {
			return fmt.Errorf("failed to delete API token: %w", err)
		}

		fmt.Println("API token deleted successfully")
		return nil
	},
}

func init() {
	apiTokensDeleteCmd.Flags().Uint32("id", 0, "ID of the token to delete")
	apiTokensDeleteCmd.MarkFlagRequired("id")
}

var apiTokensUseCmd = &cobra.Command{
	Use:   "use",
	Short: fmt.Sprintf("Store an API token locally at %q for subsequent commands (replaces login JWT)", filepath.Join(xdg.ConfigHome, configAuthenticationPath)),
	RunE: func(cmd *cobra.Command, args []string) error {
		token, _ := cmd.Flags().GetString("token")

		configFilePath, err := xdg.ConfigFile(configAuthenticationPath)
		if err != nil {
			return fmt.Errorf("failed to check $XDG_CONFIG_HOME: %w", err)
		}

		err = os.WriteFile(configFilePath, []byte(token), 0600)
		if err != nil {
			return fmt.Errorf("failed to write the authentication file: %w", err)
		}

		fmt.Println("API token stored. All subsequent commands will authenticate with this token.")
		return nil
	},
}

func init() {
	apiTokensUseCmd.Flags().String("token", "", "The API token (pko_...)")
	apiTokensUseCmd.MarkFlagRequired("token")
}
