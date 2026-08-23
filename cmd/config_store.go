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

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage stored configuration and secrets",
	Long: "Manage configuration values stored by PikoCI.\n\n" +
		"Plain values are stored as-is and shown in build logs. Values stored with\n" +
		"--secret are encrypted at rest, masked in build logs, and never displayed again.",
}

func init() {
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configDeleteCmd)

	// --team-canonical is required for every operation: ownership and
	// authorization must be explicit rather than inferred from local context.
	configCmd.PersistentFlags().String("team-canonical", "", "Team the entry belongs to")
	configCmd.MarkPersistentFlagRequired("team-canonical")
	configCmd.PersistentFlags().String("pipeline", "", "Scope the entry to a single pipeline instead of the whole team")

	configSetCmd.Flags().Bool("secret", false, "Store the value encrypted, masked in build logs and never displayed again")
	configSetCmd.Flags().String("value", "", "Value to store. For secrets prefer omitting this and entering it at the prompt so it does not land in shell history")
	configSetCmd.Flags().Bool("stdin", false, "Read the value from stdin")
}

var configSetCmd = &cobra.Command{
	Use:          "set <NAME> [VALUE]",
	Short:        "Stores or replaces a configuration value",
	Args:         cobra.RangeArgs(1, 2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline")
		isSecret, _ := cmd.Flags().GetBool("secret")
		value, _ := cmd.Flags().GetString("value")
		fromStdin, _ := cmd.Flags().GetBool("stdin")
		name := args[0]

		if len(args) == 2 {
			if value != "" {
				return fmt.Errorf("value given both as an argument and as --value")
			}
			if isSecret {
				return fmt.Errorf("refusing to read a secret from the command line, where it is visible in shell history and the process list: omit the value to be prompted, or use --stdin")
			}
			value = args[1]
		}

		kind := secret.KindPlain
		if isSecret {
			kind = secret.KindSecret
		}

		value, err := readConfigValue(cmd, name, value, fromStdin, isSecret)
		if err != nil {
			return err
		}

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		if pn != "" {
			err = c.SetPipelineConfig(cmd.Context(), tc, pn, name, value, kind)
		} else {
			err = c.SetTeamConfig(cmd.Context(), tc, name, value, kind)
		}
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "%s %q stored\n", kind, name)
		return nil
	},
}

// readConfigValue resolves the value from the flag, stdin, or an interactive
// prompt. Secrets are echoed off; plain values are read normally.
func readConfigValue(cmd *cobra.Command, name, value string, fromStdin, isSecret bool) (string, error) {
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

var configListCmd = &cobra.Command{
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
			entries, err := c.ListPipelineConfig(cmd.Context(), tc, pn)
			if err != nil {
				return err
			}
			printJSON(entries)
			return nil
		}

		entries, err := c.ListTeamConfig(cmd.Context(), tc)
		if err != nil {
			return err
		}
		printJSON(entries)

		return nil
	},
}

var configDeleteCmd = &cobra.Command{
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
			err = c.DeletePipelineConfig(cmd.Context(), tc, pn, name)
		} else {
			err = c.DeleteTeamConfig(cmd.Context(), tc, name)
		}
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "%q deleted\n", name)
		return nil
	},
}
