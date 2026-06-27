package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/pikoci/pikoci/pikoci/auditlog"
	"github.com/pikoci/pikoci/pikoci/transport/http/client"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit log commands",
}

var auditListCmd = &cobra.Command{
	Use:   "list",
	Short: "List audit log entries for a team",
	RunE: func(cmd *cobra.Command, args []string) error {
		jwt, _ := cmd.Flags().GetString("jwt")
		url, _ := cmd.Flags().GetString("url")
		tc, _ := cmd.Flags().GetString("team-canonical")

		c, err := client.New(url, jwt)
		if err != nil {
			return err
		}

		opts := auditlog.FilterOpts{}

		if v, _ := cmd.Flags().GetStringSlice("user"); len(v) > 0 {
			opts.Actors = v
		}
		if v, _ := cmd.Flags().GetStringSlice("exclude-user"); len(v) > 0 {
			opts.ExcludeActors = v
		}
		if v, _ := cmd.Flags().GetStringSlice("action"); len(v) > 0 {
			actions := make([]auditlog.Action, len(v))
			for i, a := range v {
				actions[i] = auditlog.Action(a)
			}
			opts.Actions = actions
		}
		if v, _ := cmd.Flags().GetStringSlice("exclude-action"); len(v) > 0 {
			actions := make([]auditlog.Action, len(v))
			for i, a := range v {
				actions[i] = auditlog.Action(a)
			}
			opts.ExcludeActions = actions
		}
		if v, _ := cmd.Flags().GetStringSlice("pipeline"); len(v) > 0 {
			opts.Pipelines = v
		}
		if v, _ := cmd.Flags().GetString("since"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return fmt.Errorf("invalid --since format: %w", err)
			}
			opts.Since = &t
		}
		if v, _ := cmd.Flags().GetString("until"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return fmt.Errorf("invalid --until format: %w", err)
			}
			opts.Until = &t
		}
		if v, _ := cmd.Flags().GetUint32("limit"); v > 0 {
			opts.Limit = v
		}

		entries, _, err := c.ListAuditLog(cmd.Context(), tc, opts)
		if err != nil {
			return err
		}

		printJSON(entries)
		return nil
	},
}

func init() {
	auditCmd.AddCommand(auditListCmd)

	auditListCmd.Flags().String("team-canonical", mainTeamCanonical, "Team canonical to scope the audit log")
	auditListCmd.Flags().StringSlice("user", nil, "Filter by actor username (repeatable, OR logic)")
	auditListCmd.Flags().StringSlice("exclude-user", nil, "Exclude entries by actor (repeatable)")
	auditListCmd.Flags().StringSlice("action", nil, "Filter by action type (repeatable, OR logic)")
	auditListCmd.Flags().StringSlice("exclude-action", nil, "Exclude entries with action type (repeatable)")
	auditListCmd.Flags().StringSlice("pipeline", nil, "Filter by pipeline canonical prefix (repeatable, OR logic)")
	auditListCmd.Flags().String("since", "", "Show entries after this time (RFC3339)")
	auditListCmd.Flags().String("until", "", "Show entries before this time (RFC3339)")
	auditListCmd.Flags().Uint32("limit", 50, "Maximum number of entries to return")
}
