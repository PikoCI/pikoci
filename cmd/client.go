package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/davecgh/go-spew/spew"
	"github.com/spf13/cobra"
	"github.com/xescugc/pikoci/pikoci"
	"github.com/xescugc/pikoci/pikoci/team"
	"github.com/xescugc/pikoci/pikoci/transport/http/client"
	"github.com/xescugc/pikoci/pikoci/user"
)

var (
	configAuthenticationPath = "pikoci/authentication"
)

var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "Interacts with the PikoCI server",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		jwt, _ := cmd.Flags().GetString("jwt")
		if jwt == "" {
			configFilePath, err := xdg.SearchConfigFile(configAuthenticationPath)
			if err != nil {
				return nil
			}

			data, err := os.ReadFile(configFilePath)
			if err != nil {
				return nil
			}
			cmd.Flags().Set("jwt", string(data))
		}
		return nil
	},
}

func init() {
	clientCmd.PersistentFlags().StringP("url", "u", "localhost:4000", "URL to the PikoCI server")
	clientCmd.MarkPersistentFlagRequired("url")
	clientCmd.PersistentFlags().String("jwt", "", "Provide the JWT to authenticate on the API, if not provided will read it from the FS")

	clientCmd.AddCommand(loginCmd)
	clientCmd.AddCommand(pipelinesCmd)
	clientCmd.AddCommand(jobsCmd)
	clientCmd.AddCommand(usersCmd)
	clientCmd.AddCommand(teamsCmd)
	clientCmd.AddCommand(buildsCmd)
	clientCmd.AddCommand(resourcesCmd)
	clientCmd.AddCommand(triggersCmd)
}

// login
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: fmt.Sprintf("Logs the User in and stores the JWT locally at %q", filepath.Join(xdg.ConfigHome, configAuthenticationPath)),
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")

		c, err := client.New(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		_, jwtToken, err := c.UserLogin(cmd.Context(), username, password)
		if err != nil {
			return fmt.Errorf("failed to log in: %w", err)
		}

		configFilePath, err := xdg.ConfigFile(configAuthenticationPath)
		if err != nil {
			return fmt.Errorf("failed to check $XDG_CONFIG_HOME: %w", err)
		}

		err = os.WriteFile(configFilePath, []byte(jwtToken), 0600)
		if err != nil {
			return fmt.Errorf("failed to write the authentication file: %w", err)
		}

		fmt.Println("Login successfully")
		return nil
	},
}

func init() {
	loginCmd.Flags().String("username", "", "Username use to login")
	loginCmd.Flags().String("password", "", "Password use to login")
	loginCmd.MarkFlagRequired("username")
	loginCmd.MarkFlagRequired("password")
}

// pipelines
var pipelinesCmd = &cobra.Command{
	Use:   "pipelines",
	Short: "Interacts with the PikoCI Pipelines",
}

func init() {
	pipelinesCmd.PersistentFlags().String("team-canonical", "main", "Team Canonical to scope the action")
	pipelinesCmd.MarkPersistentFlagRequired("team-canonical")

	pipelinesCmd.AddCommand(pipelinesCreateCmd)
	pipelinesCmd.AddCommand(pipelinesUpdateCmd)
	pipelinesCmd.AddCommand(pipelinesListCmd)
	pipelinesCmd.AddCommand(pipelinesGetCmd)
	pipelinesCmd.AddCommand(pipelinesGraphCmd)
	pipelinesCmd.AddCommand(pipelinesDeleteCmd)
}

var pipelinesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Creates a new PikoCI Pipeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		name, _ := cmd.Flags().GetString("name")
		configPath, _ := cmd.Flags().GetString("config")
		varsPath, _ := cmd.Flags().GetString("vars")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		f, err := os.Open(configPath)
		if err != nil {
			return fmt.Errorf("failed to open config file at %q: %w", configPath, err)
		}
		defer f.Close()

		b, err := io.ReadAll(f)
		if err != nil {
			return fmt.Errorf("failed to read config file at %q: %w", configPath, err)
		}

		var vars map[string]interface{}
		if varsPath != "" {
			vf, err := os.Open(varsPath)
			if err != nil {
				return fmt.Errorf("failed to open vars file at %q: %w", varsPath, err)
			}
			defer vf.Close()

			err = json.NewDecoder(vf).Decode(&vars)
			if err != nil {
				return fmt.Errorf("failed to read decode vars file at %q: %w", varsPath, err)
			}
		}

		_, err = c.CreatePipeline(cmd.Context(), tc, name, b, vars)
		if err != nil {
			return fmt.Errorf("failed to create Pipeline %q: %w", name, err)
		}

		return nil
	},
}

func init() {
	pipelinesCreateCmd.Flags().StringP("name", "n", "", "Name of the Pipeline")
	pipelinesCreateCmd.Flags().StringP("config", "c", "", "Path to the Pipeline config file")
	pipelinesCreateCmd.Flags().StringP("vars", "v", "", "Path to the Pipeline var file (JSON)")
	pipelinesCreateCmd.MarkFlagRequired("name")
	pipelinesCreateCmd.MarkFlagRequired("config")
}

var pipelinesUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Updates a PikoCI Pipeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		name, _ := cmd.Flags().GetString("name")
		configPath, _ := cmd.Flags().GetString("config")
		varsPath, _ := cmd.Flags().GetString("vars")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}
		err = createPipeline(cmd.Context(), c, tc, name, configPath, varsPath)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("public") {
			public, _ := cmd.Flags().GetBool("public")
			err = c.SetPipelinePublic(cmd.Context(), tc, name, public)
			if err != nil {
				return fmt.Errorf("failed to set pipeline public: %w", err)
			}
		}
		return nil
	},
}

func init() {
	pipelinesUpdateCmd.Flags().StringP("name", "n", "", "Name of the Pipeline")
	pipelinesUpdateCmd.Flags().StringP("config", "c", "", "Path to the Pipeline config file")
	pipelinesUpdateCmd.Flags().StringP("vars", "v", "", "Path to the Pipeline var file (JSON)")
	pipelinesUpdateCmd.Flags().Bool("public", false, "Make the pipeline publicly visible")
	pipelinesUpdateCmd.MarkFlagRequired("name")
	pipelinesUpdateCmd.MarkFlagRequired("config")
}

var pipelinesListCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists the PikoCI Pipelines",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		pps, err := c.ListPipelines(cmd.Context(), tc)
		if err != nil {
			return fmt.Errorf("failed to list Pipelines: %w", err)
		}

		spew.Dump(pps)
		return nil
	},
}

var pipelinesGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Gets a PikoCI Pipeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		name, _ := cmd.Flags().GetString("name")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		pp, err := c.GetPipeline(cmd.Context(), tc, name)
		if err != nil {
			return fmt.Errorf("failed to get Pipeline %q: %w", name, err)
		}

		spew.Dump(pp)
		return nil
	},
}

func init() {
	pipelinesGetCmd.Flags().StringP("name", "n", "", "Name of the Pipeline")
	pipelinesGetCmd.MarkFlagRequired("name")
}

var pipelinesGraphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Outputs the pipeline graph in DOT format",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		name, _ := cmd.Flags().GetString("name")
		format, _ := cmd.Flags().GetString("format")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		image, err := c.GetPipelineImage(cmd.Context(), tc, name, format)
		if err != nil {
			return fmt.Errorf("failed to get pipeline graph for %q: %w", name, err)
		}

		fmt.Fprint(os.Stdout, string(image))
		return nil
	},
}

func init() {
	pipelinesGraphCmd.Flags().StringP("name", "n", "", "Name of the Pipeline")
	pipelinesGraphCmd.Flags().StringP("format", "f", "dot", "Output format (dot)")
	pipelinesGraphCmd.MarkFlagRequired("name")
}

var pipelinesDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Deletes a PikoCI Pipeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		name, _ := cmd.Flags().GetString("name")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		err = c.DeletePipeline(cmd.Context(), tc, name)
		if err != nil {
			return fmt.Errorf("failed to delete Pipeline %q: %w", name, err)
		}

		return nil
	},
}

func init() {
	pipelinesDeleteCmd.Flags().StringP("name", "n", "", "Name of the Pipeline")
	pipelinesDeleteCmd.MarkFlagRequired("name")
}

// jobs
var jobsCmd = &cobra.Command{
	Use:   "jobs",
	Short: "Interacts with the PikoCI Jobs",
}

func init() {
	jobsCmd.PersistentFlags().String("team-canonical", "", "Team Canonical to scope the action")
	jobsCmd.PersistentFlags().String("pipeline-name", "", "Name of the Pipeline")
	jobsCmd.MarkPersistentFlagRequired("team-canonical")
	jobsCmd.MarkPersistentFlagRequired("pipeline-name")

	jobsCmd.AddCommand(jobsGetCmd)
	jobsCmd.AddCommand(jobsTriggerCmd)
}

var jobsGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Gets a PikoCI Pipeline Job",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline-name")
		jn, _ := cmd.Flags().GetString("job-name")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		j, err := c.GetPipelineJob(cmd.Context(), tc, pn, jn)
		if err != nil {
			return fmt.Errorf("failed to get Job %q from Pipeline %q: %w", jn, pn, err)
		}

		spew.Dump(j)
		return nil
	},
}

func init() {
	jobsGetCmd.Flags().StringP("job-name", "n", "", "Name of the Job")
	jobsGetCmd.MarkFlagRequired("job-name")
}

var jobsTriggerCmd = &cobra.Command{
	Use:   "trigger",
	Short: "Triggers a new PikoCI Pipeline Job",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline-name")
		jn, _ := cmd.Flags().GetString("job-name")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		err = c.TriggerPipelineJob(cmd.Context(), tc, pn, jn)
		if err != nil {
			return fmt.Errorf("failed to trigger Job %q from Pipeline %q: %w", jn, pn, err)
		}

		return nil
	},
}

func init() {
	jobsTriggerCmd.Flags().StringP("job-name", "n", "", "Name of the Job")
	jobsTriggerCmd.MarkFlagRequired("job-name")
}

func newClientWithConfig(url, jwt string) (*client.Client, error) {
	c, err := client.New(url, jwt)
	if err != nil {
		return nil, err
	}
	configFilePath, err := xdg.ConfigFile(configAuthenticationPath)
	if err == nil {
		c.SetConfigPath(configFilePath)
	}
	return c, nil
}

func createPipeline(ctx context.Context, svc pikoci.Service, tc, name, config, vars string) error {
	f, err := os.Open(config)
	if err != nil {
		return fmt.Errorf("failed to open config file at %q: %w", config, err)
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("failed to read config file at %q: %w", config, err)
	}

	var vrs map[string]interface{}
	if vars != "" {
		vf, err := os.Open(vars)
		if err != nil {
			return fmt.Errorf("failed to open vars file at %q: %w", vars, err)
		}
		defer vf.Close()

		err = json.NewDecoder(vf).Decode(&vrs)
		if err != nil {
			return fmt.Errorf("failed to read decode vars file at %q: %w", vars, err)
		}
	}

	_, err = svc.CreatePipeline(ctx, tc, name, b, vrs)
	if err != nil {
		return fmt.Errorf("failed to create Pipeline %q: %w", name, err)
	}

	return nil
}

// users
var usersCmd = &cobra.Command{
	Use:   "users",
	Short: "Interacts with PikoCI Users",
}

func init() {
	usersCmd.AddCommand(usersCreateCmd)
	usersCmd.AddCommand(usersListCmd)
}

var usersCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Creates a new User",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		// isHash=false because the CLI receives a plain-text password
		u, err := c.CreateUser(cmd.Context(), user.User{
			Username: username,
			Password: password,
		}, false)
		if err != nil {
			return fmt.Errorf("failed to create User %q: %w", username, err)
		}

		spew.Dump(u)
		return nil
	},
}

func init() {
	usersCreateCmd.Flags().String("username", "", "Username for the new User")
	usersCreateCmd.Flags().String("password", "", "Password for the new User")
	usersCreateCmd.MarkFlagRequired("username")
	usersCreateCmd.MarkFlagRequired("password")
}

var usersListCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists all Users",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		users, err := c.ListUsers(cmd.Context())
		if err != nil {
			return fmt.Errorf("failed to list Users: %w", err)
		}

		spew.Dump(users)
		return nil
	},
}

// teams
var teamsCmd = &cobra.Command{
	Use:   "teams",
	Short: "Interacts with PikoCI Teams",
}

func init() {
	teamsCmd.AddCommand(teamsCreateCmd)
	teamsCmd.AddCommand(teamsListCmd)
	teamsCmd.AddCommand(teamsGetCmd)
	teamsCmd.AddCommand(teamsUpdateCmd)
	teamsCmd.AddCommand(teamsDeleteCmd)
	teamsCmd.AddCommand(teamsMembersCmd)
}

var teamsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Creates a new Team",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		name, _ := cmd.Flags().GetString("name")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		t, err := c.CreateTeam(cmd.Context(), "", team.Team{Name: name})
		if err != nil {
			return fmt.Errorf("failed to create Team %q: %w", name, err)
		}

		spew.Dump(t)
		return nil
	},
}

func init() {
	teamsCreateCmd.Flags().String("name", "", "Name of the Team")
	teamsCreateCmd.MarkFlagRequired("name")
}

var teamsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists all Teams",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		teams, err := c.ListTeams(cmd.Context(), "")
		if err != nil {
			return fmt.Errorf("failed to list Teams: %w", err)
		}

		spew.Dump(teams)
		return nil
	},
}

var teamsGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Gets a Team",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		canonical, _ := cmd.Flags().GetString("canonical")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		t, err := c.GetTeam(cmd.Context(), canonical)
		if err != nil {
			return fmt.Errorf("failed to get Team %q: %w", canonical, err)
		}

		spew.Dump(t)
		return nil
	},
}

func init() {
	teamsGetCmd.Flags().String("canonical", "", "Canonical of the Team")
	teamsGetCmd.MarkFlagRequired("canonical")
}

var teamsUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Updates a Team",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		canonical, _ := cmd.Flags().GetString("canonical")
		name, _ := cmd.Flags().GetString("name")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		t, err := c.UpdateTeam(cmd.Context(), canonical, team.Team{Name: name})
		if err != nil {
			return fmt.Errorf("failed to update Team %q: %w", canonical, err)
		}

		spew.Dump(t)
		return nil
	},
}

func init() {
	teamsUpdateCmd.Flags().String("canonical", "", "Canonical of the Team")
	teamsUpdateCmd.Flags().String("name", "", "New name for the Team")
	teamsUpdateCmd.MarkFlagRequired("canonical")
	teamsUpdateCmd.MarkFlagRequired("name")
}

var teamsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Deletes a Team",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		canonical, _ := cmd.Flags().GetString("canonical")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		err = c.DeleteTeam(cmd.Context(), canonical)
		if err != nil {
			return fmt.Errorf("failed to delete Team %q: %w", canonical, err)
		}

		fmt.Println("Team deleted successfully")
		return nil
	},
}

func init() {
	teamsDeleteCmd.Flags().String("canonical", "", "Canonical of the Team")
	teamsDeleteCmd.MarkFlagRequired("canonical")
}

// team members
var teamsMembersCmd = &cobra.Command{
	Use:   "members",
	Short: "Interacts with Team Members",
}

func init() {
	teamsMembersCmd.PersistentFlags().String("team-canonical", "", "Team Canonical to scope the action")
	teamsMembersCmd.MarkPersistentFlagRequired("team-canonical")

	teamsMembersCmd.AddCommand(teamsMembersCreateCmd)
	teamsMembersCmd.AddCommand(teamsMembersUpdateCmd)
	teamsMembersCmd.AddCommand(teamsMembersDeleteCmd)
}

var teamsMembersCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Adds a Member to a Team",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		username, _ := cmd.Flags().GetString("username")
		admin, _ := cmd.Flags().GetBool("admin")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		m, err := c.CreateTeamMember(cmd.Context(), tc, team.Member{
			Admin: admin,
			User:  user.User{Username: username},
		})
		if err != nil {
			return fmt.Errorf("failed to add member %q to team %q: %w", username, tc, err)
		}

		spew.Dump(m)
		return nil
	},
}

func init() {
	teamsMembersCreateCmd.Flags().String("username", "", "Username of the member to add")
	teamsMembersCreateCmd.Flags().Bool("admin", false, "Whether the member is a team admin")
	teamsMembersCreateCmd.MarkFlagRequired("username")
}

var teamsMembersUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Updates a Team Member",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		username, _ := cmd.Flags().GetString("username")
		admin, _ := cmd.Flags().GetBool("admin")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		m, err := c.UpdateTeamMember(cmd.Context(), tc, username, team.Member{
			Admin: admin,
		})
		if err != nil {
			return fmt.Errorf("failed to update member %q in team %q: %w", username, tc, err)
		}

		spew.Dump(m)
		return nil
	},
}

func init() {
	teamsMembersUpdateCmd.Flags().String("username", "", "Username of the member to update")
	teamsMembersUpdateCmd.Flags().Bool("admin", false, "Whether the member is a team admin")
	teamsMembersUpdateCmd.MarkFlagRequired("username")
}

var teamsMembersDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Removes a Member from a Team",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		username, _ := cmd.Flags().GetString("username")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		err = c.DeleteTeamMember(cmd.Context(), tc, username)
		if err != nil {
			return fmt.Errorf("failed to remove member %q from team %q: %w", username, tc, err)
		}

		fmt.Println("Team member removed successfully")
		return nil
	},
}

func init() {
	teamsMembersDeleteCmd.Flags().String("username", "", "Username of the member to remove")
	teamsMembersDeleteCmd.MarkFlagRequired("username")
}

// builds
var buildsCmd = &cobra.Command{
	Use:   "builds",
	Short: "Interacts with PikoCI Builds",
}

func init() {
	buildsCmd.PersistentFlags().String("team-canonical", "", "Team Canonical to scope the action")
	buildsCmd.PersistentFlags().String("pipeline-name", "", "Name of the Pipeline")
	buildsCmd.PersistentFlags().String("job-name", "", "Name of the Job")
	buildsCmd.MarkPersistentFlagRequired("team-canonical")
	buildsCmd.MarkPersistentFlagRequired("pipeline-name")
	buildsCmd.MarkPersistentFlagRequired("job-name")

	buildsCmd.AddCommand(buildsListCmd)
	buildsCmd.AddCommand(buildsGetCmd)
	buildsCmd.AddCommand(buildsDeleteCmd)
	buildsCmd.AddCommand(buildsCancelCmd)
	buildsCmd.AddCommand(buildsRetryCmd)
}

var buildsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists all Builds for a Job",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline-name")
		jn, _ := cmd.Flags().GetString("job-name")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		builds, err := c.ListJobBuilds(cmd.Context(), tc, pn, jn)
		if err != nil {
			return fmt.Errorf("failed to list builds for job %q: %w", jn, err)
		}

		spew.Dump(builds)
		return nil
	},
}

var buildsGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Gets a Build",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline-name")
		jn, _ := cmd.Flags().GetString("job-name")
		bn, _ := cmd.Flags().GetString("build-number")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		b, err := c.GetJobBuild(cmd.Context(), tc, pn, jn, bn)
		if err != nil {
			return fmt.Errorf("failed to get build %q: %w", bn, err)
		}

		spew.Dump(b)
		return nil
	},
}

func init() {
	buildsGetCmd.Flags().String("build-number", "", "Number of the Build")
	buildsGetCmd.MarkFlagRequired("build-number")
}

var buildsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Deletes a Build",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline-name")
		jn, _ := cmd.Flags().GetString("job-name")
		bn, _ := cmd.Flags().GetString("build-number")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		err = c.DeleteJobBuild(cmd.Context(), tc, pn, jn, bn)
		if err != nil {
			return fmt.Errorf("failed to delete build %q: %w", bn, err)
		}

		fmt.Println("Build deleted successfully")
		return nil
	},
}

func init() {
	buildsDeleteCmd.Flags().String("build-number", "", "Number of the Build")
	buildsDeleteCmd.MarkFlagRequired("build-number")
}

var buildsCancelCmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancels a Build",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline-name")
		jn, _ := cmd.Flags().GetString("job-name")
		bn, _ := cmd.Flags().GetString("build-number")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		err = c.CancelJobBuild(cmd.Context(), tc, pn, jn, bn)
		if err != nil {
			return fmt.Errorf("failed to cancel build %q: %w", bn, err)
		}

		fmt.Println("Build cancelled successfully")
		return nil
	},
}

func init() {
	buildsCancelCmd.Flags().String("build-number", "", "Number of the Build")
	buildsCancelCmd.MarkFlagRequired("build-number")
}

var buildsRetryCmd = &cobra.Command{
	Use:   "retry",
	Short: "Retries a Build",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline-name")
		jn, _ := cmd.Flags().GetString("job-name")
		bn, _ := cmd.Flags().GetString("build-number")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		err = c.RetryJobBuild(cmd.Context(), tc, pn, jn, bn)
		if err != nil {
			return fmt.Errorf("failed to retry build %q: %w", bn, err)
		}

		fmt.Println("Build retried successfully")
		return nil
	},
}

func init() {
	buildsRetryCmd.Flags().String("build-number", "", "Number of the Build")
	buildsRetryCmd.MarkFlagRequired("build-number")
}

// resources
var resourcesCmd = &cobra.Command{
	Use:   "resources",
	Short: "Interacts with PikoCI Resources",
}

func init() {
	resourcesCmd.PersistentFlags().String("team-canonical", "", "Team Canonical to scope the action")
	resourcesCmd.PersistentFlags().String("pipeline-name", "", "Name of the Pipeline")
	resourcesCmd.MarkPersistentFlagRequired("team-canonical")
	resourcesCmd.MarkPersistentFlagRequired("pipeline-name")

	resourcesCmd.AddCommand(resourcesGetCmd)
	resourcesCmd.AddCommand(resourcesTriggerCmd)
	resourcesCmd.AddCommand(resourcesVersionsCmd)
	resourcesCmd.AddCommand(resourcesWebhookRegenerateCmd)
}

var resourcesGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Gets a Pipeline Resource",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline-name")
		rCan, _ := cmd.Flags().GetString("resource-canonical")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		r, err := c.GetPipelineResource(cmd.Context(), tc, pn, rCan)
		if err != nil {
			return fmt.Errorf("failed to get resource %q: %w", rCan, err)
		}

		spew.Dump(r)
		return nil
	},
}

func init() {
	resourcesGetCmd.Flags().String("resource-canonical", "", "Canonical of the Resource")
	resourcesGetCmd.MarkFlagRequired("resource-canonical")
}

var resourcesTriggerCmd = &cobra.Command{
	Use:   "trigger",
	Short: "Triggers a Pipeline Resource check",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline-name")
		rCan, _ := cmd.Flags().GetString("resource-canonical")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		err = c.TriggerPipelineResource(cmd.Context(), tc, pn, rCan)
		if err != nil {
			return fmt.Errorf("failed to trigger resource %q: %w", rCan, err)
		}

		fmt.Println("Resource triggered successfully")
		return nil
	},
}

func init() {
	resourcesTriggerCmd.Flags().String("resource-canonical", "", "Canonical of the Resource")
	resourcesTriggerCmd.MarkFlagRequired("resource-canonical")
}

var resourcesVersionsCmd = &cobra.Command{
	Use:   "versions",
	Short: "Lists versions of a Pipeline Resource",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline-name")
		rCan, _ := cmd.Flags().GetString("resource-canonical")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		versions, err := c.ListResourceVersions(cmd.Context(), tc, pn, rCan)
		if err != nil {
			return fmt.Errorf("failed to list versions for resource %q: %w", rCan, err)
		}

		spew.Dump(versions)
		return nil
	},
}

func init() {
	resourcesVersionsCmd.Flags().String("resource-canonical", "", "Canonical of the Resource")
	resourcesVersionsCmd.MarkFlagRequired("resource-canonical")
}

var resourcesWebhookRegenerateCmd = &cobra.Command{
	Use:   "webhook-regenerate",
	Short: "Regenerates the webhook token for a Pipeline Resource",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		pn, _ := cmd.Flags().GetString("pipeline-name")
		rCan, _ := cmd.Flags().GetString("resource-canonical")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		token, err := c.RegenerateWebhookToken(cmd.Context(), tc, pn, rCan)
		if err != nil {
			return fmt.Errorf("failed to regenerate webhook token for resource %q: %w", rCan, err)
		}

		fmt.Println(token)
		return nil
	},
}

func init() {
	resourcesWebhookRegenerateCmd.Flags().String("resource-canonical", "", "Canonical of the Resource")
	resourcesWebhookRegenerateCmd.MarkFlagRequired("resource-canonical")
}

// triggers
var triggersCmd = &cobra.Command{
	Use:   "triggers",
	Short: "Interacts with PikoCI Triggers",
}

func init() {
	triggersCmd.PersistentFlags().String("team-canonical", "", "Team Canonical to scope the action")
	triggersCmd.MarkPersistentFlagRequired("team-canonical")

	triggersCmd.AddCommand(triggersCreateCmd)
	triggersCmd.AddCommand(triggersListCmd)
}

var triggersCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Creates a new Trigger",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		name, _ := cmd.Flags().GetString("name")
		versionStr, _ := cmd.Flags().GetString("version")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		var version map[string]interface{}
		if err := json.Unmarshal([]byte(versionStr), &version); err != nil {
			return fmt.Errorf("failed to parse version JSON: %w", err)
		}

		t, err := c.CreateTrigger(cmd.Context(), tc, name, version)
		if err != nil {
			return fmt.Errorf("failed to create trigger %q: %w", name, err)
		}

		spew.Dump(t)
		return nil
	},
}

func init() {
	triggersCreateCmd.Flags().String("name", "", "Name of the Trigger")
	triggersCreateCmd.Flags().String("version", "", "Version data as a JSON string")
	triggersCreateCmd.MarkFlagRequired("name")
	triggersCreateCmd.MarkFlagRequired("version")
}

var triggersListCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists Triggers",
	RunE: func(cmd *cobra.Command, args []string) error {
		url, _ := cmd.Flags().GetString("url")
		jwt, _ := cmd.Flags().GetString("jwt")
		tc, _ := cmd.Flags().GetString("team-canonical")
		name, _ := cmd.Flags().GetString("name")
		after, _ := cmd.Flags().GetUint32("after")

		c, err := newClientWithConfig(url, jwt)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", url, err)
		}

		triggers, err := c.ListTriggersAfter(cmd.Context(), tc, name, after)
		if err != nil {
			return fmt.Errorf("failed to list triggers %q: %w", name, err)
		}

		spew.Dump(triggers)
		return nil
	},
}

func init() {
	triggersListCmd.Flags().String("name", "", "Name of the Trigger")
	triggersListCmd.Flags().Uint32("after", 0, "List triggers after this ID")
	triggersListCmd.MarkFlagRequired("name")
}
