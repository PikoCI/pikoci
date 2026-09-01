package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/pikoci/pikoci/pikoci/secret"
)

var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Manage secrets and plain values stored by PikoCI",
	Long: "Manage the secrets PikoCI stores for a team or a pipeline.\n\n" +
		"Values are stored as secrets by default: encrypted at rest, masked in build\n" +
		"logs, and never displayed again. Values stored with --plain are kept as-is\n" +
		"and shown in build logs.",
}

// secretCommandLineRefusal is returned whenever a secret value would land on
// the command line — as a positional argument or via --value — where it is
// visible in shell history and the process list.
const secretCommandLineRefusal = "refusing to read a secret from the command line, where it is visible in shell history and the process list: omit the value to be prompted, use --stdin, or pass --plain if it is not sensitive"

func init() {
	secretsCmd.AddCommand(secretsSetCmd)
	secretsCmd.AddCommand(secretsListCmd)
	secretsCmd.AddCommand(secretsDeleteCmd)

	// --team-canonical is required for every operation: ownership and
	// authorization must be explicit rather than inferred from local context.
	secretsCmd.PersistentFlags().String("team-canonical", "", "Team the entry belongs to")
	secretsCmd.MarkPersistentFlagRequired("team-canonical")
	secretsCmd.PersistentFlags().String("pipeline", "", "Scope the entry to a single pipeline instead of the whole team")

	secretsSetCmd.Flags().Bool("plain", false, "Store the value as-is: readable back and shown in build logs. Without it the value is stored as a secret")
	secretsSetCmd.Flags().String("value", "", "Value to store. For secrets prefer omitting this and entering it at the prompt so it does not land in shell history")
	secretsSetCmd.Flags().Bool("stdin", false, "Read the value from stdin")
}

var secretsSetCmd = &cobra.Command{
	Use:          "set <NAME> [VALUE]",
	Short:        "Stores or replaces a secret or plain value",
	Args:         cobra.RangeArgs(1, 2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline")
		// Secret is the default: a forgotten flag then over-protects a plain
		// value rather than silently storing a credential in the clear.
		isPlain, _ := cmd.Flags().GetBool("plain")
		isSecret := !isPlain
		value, _ := cmd.Flags().GetString("value")
		fromStdin, _ := cmd.Flags().GetBool("stdin")
		name := args[0]

		if len(args) == 2 {
			if value != "" {
				return fmt.Errorf("value given both as an argument and as --value")
			}
			if isSecret {
				return fmt.Errorf("%s", secretCommandLineRefusal)
			}
			value = args[1]
		}

		kind := secret.KindPlain
		if isSecret {
			kind = secret.KindSecret
		}

		value, err := readSecretValue(cmd, name, value, fromStdin, isSecret)
		if err != nil {
			return err
		}

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		if pn != "" {
			err = c.SetPipelineSecret(cmd.Context(), tc, pn, name, value, kind)
		} else {
			err = c.SetTeamSecret(cmd.Context(), tc, name, value, kind)
		}
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "%s %q stored\n", kind, name)
		return nil
	},
}

// readSecretValue resolves the value from the flag, stdin, or an interactive
// prompt. Secrets are echoed off; plain values are read normally.
func readSecretValue(cmd *cobra.Command, name, value string, fromStdin, isSecret bool) (string, error) {
	if value != "" && fromStdin {
		return "", fmt.Errorf("--value and --stdin are mutually exclusive")
	}

	if value != "" {
		if isSecret {
			return "", fmt.Errorf("%s", secretCommandLineRefusal)
		}
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
		return "", fmt.Errorf("no value provided: pass it as an argument, use --stdin, or run interactively")
	}

	fmt.Fprintf(os.Stderr, "%s: ", name)

	var (
		b   []byte
		err error
	)
	if isSecret {
		b, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
	} else {
		var line string
		_, err = fmt.Scanln(&line)
		b = []byte(line)
	}
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
	Short:        "Lists stored entries. Plain values are shown; secret values never are",
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
			entries, err := c.ListPipelineSecrets(cmd.Context(), tc, pn)
			if err != nil {
				return err
			}
			printJSON(entries)
			return nil
		}

		entries, err := c.ListTeamSecrets(cmd.Context(), tc)
		if err != nil {
			return err
		}
		printJSON(entries)

		return nil
	},
}

var secretsDeleteCmd = &cobra.Command{
	Use:          "delete <NAME>",
	Short:        "Removes a stored entry",
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

		fmt.Fprintf(os.Stderr, "%q deleted\n", name)
		return nil
	},
}
