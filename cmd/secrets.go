package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Manage encrypted secrets stored by PikoCI",
}

func init() {
	secretsCmd.AddCommand(secretsSetCmd)
	secretsCmd.AddCommand(secretsListCmd)
	secretsCmd.AddCommand(secretsDeleteCmd)

	// --team is required for every secret operation: ownership and
	// authorization must be explicit rather than inferred from local context.
	secretsCmd.PersistentFlags().String("team-canonical", "", "Team the secret belongs to")
	secretsCmd.MarkPersistentFlagRequired("team-canonical")
	secretsCmd.PersistentFlags().String("pipeline", "", "Scope the secret to a single pipeline instead of the whole team")

	secretsSetCmd.Flags().String("value", "", "Secret value. Prefer omitting this and entering the value at the prompt so it does not land in shell history")
	secretsSetCmd.Flags().Bool("stdin", false, "Read the secret value from stdin")
}

var secretsSetCmd = &cobra.Command{
	Use:          "set <NAME>",
	Short:        "Stores or replaces a secret value",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline")
		value, _ := cmd.Flags().GetString("value")
		fromStdin, _ := cmd.Flags().GetBool("stdin")
		name := args[0]

		value, err := readSecretValue(cmd, name, value, fromStdin)
		if err != nil {
			return err
		}

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		if pn != "" {
			err = c.SetPipelineSecret(cmd.Context(), tc, pn, name, value)
		} else {
			err = c.SetTeamSecret(cmd.Context(), tc, name, value)
		}
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "secret %q stored\n", name)
		return nil
	},
}

// readSecretValue resolves the value from the flag, stdin, or an interactive
// prompt. The prompt is the default because a value passed as --value is
// visible in shell history and in the process list.
func readSecretValue(cmd *cobra.Command, name, value string, fromStdin bool) (string, error) {
	if value != "" && fromStdin {
		return "", fmt.Errorf("--value and --stdin are mutually exclusive")
	}

	if value != "" {
		return value, nil
	}

	if fromStdin {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("failed to read the value from stdin: %w", err)
		}
		v := strings.TrimRight(string(b), "\r\n")
		if v == "" {
			return "", fmt.Errorf("no value provided on stdin")
		}
		return v, nil
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no value provided: pass --value, use --stdin, or run interactively")
	}

	fmt.Fprintf(os.Stderr, "%s: ", name)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("failed to read the value: %w", err)
	}
	if len(b) == 0 {
		return "", fmt.Errorf("no value provided")
	}

	return string(b), nil
}

var secretsListCmd = &cobra.Command{
	Use:          "list",
	Short:        "Lists stored secrets. Values are never displayed",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		if pn != "" {
			secrets, err := c.ListPipelineSecrets(cmd.Context(), tc, pn)
			if err != nil {
				return err
			}
			printJSON(secrets)
			return nil
		}

		secrets, err := c.ListTeamSecrets(cmd.Context(), tc)
		if err != nil {
			return err
		}
		printJSON(secrets)

		return nil
	},
}

var secretsDeleteCmd = &cobra.Command{
	Use:          "delete <NAME>",
	Short:        "Removes a stored secret",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline")
		name := args[0]

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		if pn != "" {
			err = c.DeletePipelineSecret(cmd.Context(), tc, pn, name)
		} else {
			err = c.DeleteTeamSecret(cmd.Context(), tc, name)
		}
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "secret %q deleted\n", name)
		return nil
	},
}
