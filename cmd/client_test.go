package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

func TestCommandTree(t *testing.T) {
	tests := []struct {
		parent      *cobra.Command
		parentName  string
		subcommands []string
	}{
		{clientCmd, "client", []string{"login", "pipelines", "jobs", "users", "teams", "builds", "resources", "triggers", "export", "config"}},
		{usersCmd, "users", []string{"create", "list", "update", "delete", "change-password"}},
		{teamsCmd, "teams", []string{"create", "list", "get", "update", "delete", "members"}},
		{teamsMembersCmd, "members", []string{"create", "update", "delete"}},
		{buildsCmd, "builds", []string{"list", "get", "delete", "cancel", "retry", "approve", "reject", "report"}},
		{resourcesCmd, "resources", []string{"get", "trigger", "versions", "webhook-regenerate"}},
		{triggersCmd, "triggers", []string{"create", "list"}},
		{configCmd, "config", []string{"set", "list", "delete"}},
	}

	for _, tt := range tests {
		t.Run(tt.parentName, func(t *testing.T) {
			for _, name := range tt.subcommands {
				sub := findSubcommand(tt.parent, name)
				assert.NotNilf(t, sub, "expected subcommand %q under %q", name, tt.parentName)
			}
		})
	}
}

func TestRequiredFlags(t *testing.T) {
	tests := []struct {
		name     string
		cmd      *cobra.Command
		required []string
	}{
		{"users create", usersCreateCmd, []string{"username", "password"}},
		{"users update", usersUpdateCmd, []string{"username"}},
		{"users delete", usersDeleteCmd, []string{"username"}},
		{"users change-password", usersChangePasswordCmd, []string{"old-password", "new-password"}},
		{"teams create", teamsCreateCmd, []string{"name"}},
		{"teams get", teamsGetCmd, []string{"canonical"}},
		{"teams update", teamsUpdateCmd, []string{"canonical", "name"}},
		{"teams delete", teamsDeleteCmd, []string{"canonical"}},
		{"members create", teamsMembersCreateCmd, []string{"username"}},
		{"members update", teamsMembersUpdateCmd, []string{"username"}},
		{"members delete", teamsMembersDeleteCmd, []string{"username"}},
		{"builds get", buildsGetCmd, []string{"build-number"}},
		{"builds delete", buildsDeleteCmd, []string{"build-number"}},
		{"builds cancel", buildsCancelCmd, []string{"build-number"}},
		{"builds retry", buildsRetryCmd, []string{"build-number"}},
		{"builds report", buildsReportCmd, []string{"build-number"}},
		{"resources get", resourcesGetCmd, []string{"resource-canonical"}},
		{"resources trigger", resourcesTriggerCmd, []string{"resource-canonical"}},
		{"resources versions", resourcesVersionsCmd, []string{"resource-canonical"}},
		{"resources webhook-regenerate", resourcesWebhookRegenerateCmd, []string{"resource-canonical"}},
		{"triggers create", triggersCreateCmd, []string{"name", "version"}},
		{"triggers list", triggersListCmd, []string{"name"}},
		{"export", exportCmd, []string{"output"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, flagName := range tt.required {
				f := tt.cmd.Flags().Lookup(flagName)
				require.NotNilf(t, f, "flag %q not found on %q", flagName, tt.name)
				ann := tt.cmd.Flags().Lookup(flagName).Annotations
				_, isRequired := ann[cobra.BashCompOneRequiredFlag]
				assert.Truef(t, isRequired, "flag %q on %q should be required", flagName, tt.name)
			}
		})
	}
}

func TestPersistentFlags(t *testing.T) {
	tests := []struct {
		name   string
		parent *cobra.Command
		child  *cobra.Command
		flags  []string
	}{
		{"builds persistent flags", buildsCmd, buildsListCmd, []string{"team-canonical", "pipeline-name", "job-name"}},
		{"resources persistent flags", resourcesCmd, resourcesGetCmd, []string{"team-canonical", "pipeline-name"}},
		{"triggers persistent flags", triggersCmd, triggersCreateCmd, []string{"team-canonical"}},
		{"members persistent flags", teamsMembersCmd, teamsMembersCreateCmd, []string{"team-canonical"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, flagName := range tt.flags {
				f := tt.parent.PersistentFlags().Lookup(flagName)
				require.NotNilf(t, f, "persistent flag %q not found on parent %q", flagName, tt.name)
				// Verify child inherits the flag
				inherited := tt.child.InheritedFlags().Lookup(flagName)
				assert.NotNilf(t, inherited, "child should inherit persistent flag %q from parent", flagName)
			}
		})
	}
}
