// Package client provides an HTTP client for interacting with the PikoCI server API.
// It implements the pikoci.Service interface by making authenticated HTTP requests
// to the server endpoints for managing users, teams, pipelines, jobs, builds, and resources.
package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/queue"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/team"
	thttp "github.com/pikoci/pikoci/pikoci/transport/http"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/wkr"
)

// Client is an HTTP client for the PikoCI API. It holds the server URL,
// a JWT for authentication, and an optional config path for persisting
// refreshed tokens.
type Client struct {
	url        string
	jwt        string
	configPath string
}

// New returns a new Client configured with the given host URL and JWT token.
// The host must be a valid URL; if no scheme is provided, "http://" is prepended.
func New(host, jwt string) (*Client, error) {
	if host == "" {
		return nil, fmt.Errorf("can't initialize the %q with an empty host", "qid")
	}
	if !strings.HasPrefix(host, "http") {
		host = fmt.Sprintf("http://%s", host)
	}
	_, err := url.Parse(host)
	if err != nil {
		return nil, err
	}

	cl := &Client{
		url: host,
		jwt: jwt,
	}

	return cl, nil
}

// SetConfigPath sets the path where the JWT will be persisted on refresh.
// When empty (default), JWT refresh will not be written to disk.
func (cl *Client) SetConfigPath(path string) {
	cl.configPath = path
}

// UserLogin authenticates a user with the given username and password,
// returning the user details and a JWT token on success.
func (cl *Client) UserLogin(ctx context.Context, un, pass string) (*user.WithMemberships, string, error) {
	var resp thttp.UserLoginResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/login", cl.url), thttp.UserLoginRequest{
		Username: un,
		Password: pass,
	}, &resp)
	if err != nil {
		return nil, "", fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, "", fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Data.User, resp.Data.JWT, nil
}

// RefreshToken requests a new JWT token for the given user, returning
// updated user details and the new token.
func (cl *Client) RefreshToken(ctx context.Context, un string) (*user.WithMemberships, string, error) {
	var resp thttp.RefreshTokenResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/refresh-token", cl.url), nil, &resp)
	if err != nil {
		return nil, "", fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, "", fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Data.User, resp.Data.JWT, nil
}

// GetUser retrieves a user by username.
func (cl *Client) GetUser(ctx context.Context, un string) (*user.WithMemberships, error) {
	var resp thttp.GetUserResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/users/%s", cl.url, un), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	if resp.User == nil {
		return nil, fmt.Errorf("user not found")
	}

	return &user.WithMemberships{User: *resp.User}, nil
}

// CreateUser creates a new user. If isHash is true, the password is treated
// as already hashed.
func (cl *Client) CreateUser(ctx context.Context, u user.User, isHash bool) (*user.User, error) {
	var resp thttp.CreateUserResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/users", cl.url), thttp.CreateUserRequest{
		Username: u.Username,
		Password: u.Password,
		IsHash:   isHash,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.User, nil
}

// ListUsers retrieves all users from the server.
func (cl *Client) ListUsers(ctx context.Context) ([]*user.User, error) {
	var resp thttp.ListUsersResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/users", cl.url), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Users, nil
}

// UpdateUser updates an existing user identified by username.
func (cl *Client) UpdateUser(ctx context.Context, un string, u user.User, isHash bool) (*user.User, error) {
	var resp thttp.UpdateUserResponse

	err := cl.Request(ctx, http.MethodPut, fmt.Sprintf("%s/users/%s", cl.url, un), thttp.UpdateUserRequest{
		FullName: u.FullName,
		Username: u.Username,
		Password: u.Password,
		Admin:    u.Admin,
		IsHash:   isHash,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.User, nil
}

// DeleteUser deletes the user with the given username.
func (cl *Client) DeleteUser(ctx context.Context, un string) error {
	var resp thttp.DeleteUserResponse

	err := cl.Request(ctx, http.MethodDelete, fmt.Sprintf("%s/users/%s", cl.url, un), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// ChangePassword changes the password for the authenticated user.
func (cl *Client) ChangePassword(ctx context.Context, un, oldPassword, newPassword string) error {
	var resp thttp.ChangePasswordResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/users/change-password", cl.url), thttp.ChangePasswordRequest{
		OldPassword: oldPassword,
		NewPassword: newPassword,
	}, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// UpdateProfile updates the profile of the authenticated user.
func (cl *Client) UpdateProfile(ctx context.Context, un string, fullName, newUsername string) (*user.User, error) {
	var resp thttp.UpdateProfileResponse

	err := cl.Request(ctx, http.MethodPut, fmt.Sprintf("%s/profile", cl.url), thttp.UpdateProfileRequest{
		FullName: fullName,
		Username: newUsername,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.User, nil
}

// CreateTeam creates a new team with the given details.
func (cl *Client) CreateTeam(ctx context.Context, un string, t team.Team) (*team.WithMembers, error) {
	var resp thttp.CreateTeamResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams", cl.url), thttp.CreateTeamRequest{
		Name: t.Name,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Team, nil
}

// ListTeams retrieves all teams visible to the given user.
func (cl *Client) ListTeams(ctx context.Context, un string) ([]*team.WithMembers, error) {
	var resp thttp.ListTeamsResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams", cl.url), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Teams, nil
}

// GetTeam retrieves a team by its canonical name.
func (cl *Client) GetTeam(ctx context.Context, tc string) (*team.WithMembers, error) {
	var resp thttp.GetTeamResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s", cl.url, tc), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Team, nil
}

// UpdateTeam updates a team identified by its canonical name.
func (cl *Client) UpdateTeam(ctx context.Context, tc string, t team.Team) (*team.WithMembers, error) {
	var resp thttp.UpdateTeamResponse

	err := cl.Request(ctx, http.MethodPut, fmt.Sprintf("%s/teams/%s", cl.url, tc), thttp.UpdateTeamRequest{
		Name: t.Name,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Team, nil
}

// DeleteTeam deletes the team with the given canonical name.
func (cl *Client) DeleteTeam(ctx context.Context, tc string) error {
	var resp thttp.DeleteTeamResponse

	err := cl.Request(ctx, http.MethodDelete, fmt.Sprintf("%s/teams/%s", cl.url, tc), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// CreateTeamMember adds a new member to the specified team.
func (cl *Client) CreateTeamMember(ctx context.Context, tc string, tm team.Member) (*team.Member, error) {
	var resp thttp.CreateTeamMemberResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/members", cl.url, tc), tm, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Member, nil
}

// UpdateTeamMember updates a member's role within a team.
func (cl *Client) UpdateTeamMember(ctx context.Context, tc, mu string, tm team.Member) (*team.Member, error) {
	var resp thttp.UpdateTeamMemberResponse

	err := cl.Request(ctx, http.MethodPut, fmt.Sprintf("%s/teams/%s/members/%s", cl.url, tc, mu), thttp.UpdateTeamMemberRequest{
		Admin: tm.Admin,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Member, nil
}

// DeleteTeamMember removes a member from the specified team.
func (cl *Client) DeleteTeamMember(ctx context.Context, tc, mu string) error {
	var resp thttp.DeleteTeamMemberResponse

	err := cl.Request(ctx, http.MethodDelete, fmt.Sprintf("%s/teams/%s/members/%s", cl.url, tc, mu), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// CreatePipeline creates a new pipeline with the given name, HCL config, and variables.
func (cl *Client) CreatePipeline(ctx context.Context, tc, pn string, pp []byte, vars map[string]interface{}) (*pipeline.Pipeline, error) {
	var resp thttp.CreatePipelineResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines", cl.url, tc), thttp.CreatePipelineRequest{
		Name:   pn,
		Config: pp,
		Vars:   vars,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Pipeline, nil
}

// SetPipelinePublic sets the public visibility of a pipeline.
func (cl *Client) SetPipelinePublic(ctx context.Context, tc, pn string, public bool) error {
	var resp thttp.UpdatePipelineResponse

	err := cl.Request(ctx, http.MethodPut, fmt.Sprintf("%s/teams/%s/pipelines/%s", cl.url, tc, pn), thttp.UpdatePipelineRequest{
		Public: &public,
	}, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// GetPublicPipeline retrieves a public pipeline. It delegates to GetPipeline.
func (cl *Client) GetPublicPipeline(ctx context.Context, tc, pn string) (*pipeline.Pipeline, error) {
	return cl.GetPipeline(ctx, tc, pn)
}

// GetPublicPipelineImage retrieves a public pipeline's image. It delegates to GetPipelineImage.
func (cl *Client) GetPublicPipelineImage(ctx context.Context, tc, pn, format string) ([]byte, error) {
	return cl.GetPipelineImage(ctx, tc, pn, format)
}

// GetPublicPipelineJob retrieves a job from a public pipeline. It delegates to GetPipelineJob.
func (cl *Client) GetPublicPipelineJob(ctx context.Context, tc, pn, jn string) (*job.Job, error) {
	return cl.GetPipelineJob(ctx, tc, pn, jn)
}

// ListPublicJobBuilds lists builds for a job on a public pipeline. It delegates to ListJobBuilds.
func (cl *Client) ListPublicJobBuilds(ctx context.Context, tc, pn, jn string, before *uint32, after *uint32, limit uint32) ([]*build.Build, bool, error) {
	return cl.ListJobBuilds(ctx, tc, pn, jn, before, after, limit)
}

// GetPublicPipelineResource retrieves a resource from a public pipeline. It delegates to GetPipelineResource.
func (cl *Client) GetPublicPipelineResource(ctx context.Context, tc, pn, rCan string) (*resource.Resource, error) {
	return cl.GetPipelineResource(ctx, tc, pn, rCan)
}

// ListPublicResourceVersions lists versions for a resource on a public pipeline. It delegates to ListResourceVersions.
func (cl *Client) ListPublicResourceVersions(ctx context.Context, tc, pn, rCan string, before *uint32, after *uint32, limit uint32) ([]*resource.Version, bool, error) {
	return cl.ListResourceVersions(ctx, tc, pn, rCan, before, after, limit)
}

// UpdatePipeline updates an existing pipeline's configuration, variables, and optionally its name.
func (cl *Client) UpdatePipeline(ctx context.Context, tc, pn string, pp []byte, vars map[string]interface{}, newName ...string) (*pipeline.Pipeline, error) {
	var resp thttp.UpdatePipelineResponse

	req := thttp.UpdatePipelineRequest{
		Config: pp,
		Vars:   vars,
	}
	if len(newName) > 0 && newName[0] != "" {
		req.Name = newName[0]
	}
	err := cl.Request(ctx, http.MethodPut, fmt.Sprintf("%s/teams/%s/pipelines/%s", cl.url, tc, pn), req, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Pipeline, nil
}

// GetPipeline retrieves a pipeline by team canonical and pipeline name.
func (cl *Client) GetPipeline(ctx context.Context, tc, pn string) (*pipeline.Pipeline, error) {
	var resp thttp.GetPipelineResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s/pipelines/%s", cl.url, tc, pn), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Pipeline, nil
}

// GetPipelineImage retrieves the rendered image of a pipeline in the given format.
func (cl *Client) GetPipelineImage(ctx context.Context, tc, pn, format string) ([]byte, error) {
	if format == "svg" || format == "png" {
		return cl.RequestRaw(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s/pipelines/%s/image.%s", cl.url, tc, pn, format))
	}

	var resp thttp.GetPipelineImageResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s/pipelines/%s/image.%s", cl.url, tc, pn, format), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return []byte(resp.Image), nil
}

// CreatePipelineImage generates a pipeline image from the given config without persisting it.
func (cl *Client) CreatePipelineImage(ctx context.Context, tc string, pp []byte, vars map[string]interface{}, format string) ([]byte, error) {
	var resp thttp.CreatePipelineImageResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/image.%s", cl.url, tc, format), thttp.CreatePipelineRequest{
		Config: pp,
		Vars:   vars,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return []byte(resp.Image), nil
}

// ListPipelines retrieves all pipelines for a team.
func (cl *Client) ListPipelines(ctx context.Context, tc string) ([]*pipeline.Pipeline, error) {
	var resp thttp.ListPipelinesResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s/pipelines", cl.url, tc), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Pipelines, nil
}

// DeletePipeline deletes a pipeline by team canonical and pipeline name.
func (cl *Client) DeletePipeline(ctx context.Context, tc, pn string) error {
	var resp thttp.DeletePipelineResponse

	err := cl.Request(ctx, http.MethodDelete, fmt.Sprintf("%s/teams/%s/pipelines/%s", cl.url, tc, pn), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// TriggerPipelineJob triggers a manual run of the specified job.
func (cl *Client) TriggerPipelineJob(ctx context.Context, tc, pn, jn string) error {
	var resp thttp.TriggerPipelineJobResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/trigger", cl.url, tc, pn, jn), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

func (cl *Client) PausePipeline(ctx context.Context, tc, pn string) error {
	var resp thttp.PausePipelineResponse
	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/pause", cl.url, tc, pn), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}
	return nil
}

func (cl *Client) UnpausePipeline(ctx context.Context, tc, pn string) error {
	var resp thttp.UnpausePipelineResponse
	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/unpause", cl.url, tc, pn), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}
	return nil
}

func (cl *Client) PauseJob(ctx context.Context, tc, pn, jn string) error {
	var resp thttp.PauseJobResponse
	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/pause", cl.url, tc, pn, jn), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}
	return nil
}

func (cl *Client) UnpauseJob(ctx context.Context, tc, pn, jn string) error {
	var resp thttp.UnpauseJobResponse
	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/unpause", cl.url, tc, pn, jn), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}
	return nil
}

// GetPipelineJob retrieves a single job from a pipeline.
func (cl *Client) GetPipelineJob(ctx context.Context, tc, pn, jn string) (*job.Job, error) {
	var resp thttp.GetPipelineJobResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s", cl.url, tc, pn, jn), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Job, nil
}

// CreateJobBuild creates a new build for the specified job.
func (cl *Client) CreateJobBuild(ctx context.Context, tc, pn, jn string, b build.Build) (*build.Build, error) {
	var resp thttp.CreateJobBuildResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/builds", cl.url, tc, pn, jn), thttp.CreateJobBuildRequest{
		Build: b,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		if resp.Err == pikoci.ErrConcurrencyLimit.Error() {
			return nil, pikoci.ErrConcurrencyLimit
		}
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Build, nil
}

// UpdateJobBuild updates the status or details of an existing build.
func (cl *Client) UpdateJobBuild(ctx context.Context, tc, pn, jn string, buildNumber string, b build.Build) error {
	var resp thttp.UpdateJobBuildResponse

	err := cl.Request(ctx, http.MethodPut, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/builds/%s", cl.url, tc, pn, jn, buildNumber), thttp.UpdateJobBuildRequest{
		Build: b,
	}, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// DeleteJobBuild deletes a build by its number.
func (cl *Client) DeleteJobBuild(ctx context.Context, tc, pn, jn string, buildNumber string) error {
	var resp thttp.DeleteJobBuildResponse

	err := cl.Request(ctx, http.MethodDelete, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/builds/%s", cl.url, tc, pn, jn, buildNumber), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// GetJobBuild retrieves a single build by its number.
func (cl *Client) GetJobBuild(ctx context.Context, tc, pn, jn string, buildNumber string) (*build.Build, error) {
	var resp thttp.GetJobBuildResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/builds/%s", cl.url, tc, pn, jn, buildNumber), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Build, nil
}

// CancelJobBuild cancels a running build.
func (cl *Client) CancelJobBuild(ctx context.Context, tc, pn, jn string, buildNumber string) error {
	var resp thttp.CancelJobBuildResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/builds/%s/cancel", cl.url, tc, pn, jn, buildNumber), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// RetryJobBuild retries a completed build.
func (cl *Client) RetryJobBuild(ctx context.Context, tc, pn, jn, buildNumber string) error {
	var resp thttp.RetryJobBuildResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/builds/%s/retry", cl.url, tc, pn, jn, buildNumber), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// CreateRetryJobBuild creates a new build as a retry of a parent build.
func (cl *Client) CreateRetryJobBuild(ctx context.Context, tc, pn, jn, parentBuildNumber string, b build.Build) (*build.Build, error) {
	var resp thttp.CreateRetryJobBuildResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/retry-builds", cl.url, tc, pn, jn), thttp.CreateRetryJobBuildRequest{
		ParentBuildNumber: parentBuildNumber,
		Build:             b,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		if resp.Err == pikoci.ErrConcurrencyLimit.Error() {
			return nil, pikoci.ErrConcurrencyLimit
		}
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Build, nil
}

// FindBuildGetVersions retrieves the resource versions fetched by get steps in a build.
func (cl *Client) FindBuildGetVersions(ctx context.Context, tc, pn, jn string, buildID uint32) (map[string]uint32, error) {
	var resp thttp.FindBuildGetVersionsResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/builds-get-versions/%d", cl.url, tc, pn, jn, buildID), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Versions, nil
}

// InsertBuildGetVersion records which resource version a get step fetched during a build.
func (cl *Client) InsertBuildGetVersion(ctx context.Context, tc, pn, jn string, buildID uint32, stepName string, versionID uint32) error {
	var resp thttp.InsertBuildGetVersionResponse

	body := thttp.InsertBuildGetVersionRequest{
		StepName:  stepName,
		VersionID: versionID,
	}

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/builds/%d/get-versions", cl.url, tc, pn, jn, buildID), body, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// StartPendingBuild transitions a pending build to started status.
func (cl *Client) StartPendingBuild(ctx context.Context, tc, pn, jn string, buildID uint32) (*build.Build, error) {
	var resp thttp.StartPendingBuildResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/builds/start-pending", cl.url, tc, pn, jn), thttp.StartPendingBuildRequest{
		BuildID: buildID,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		if resp.Err == pikoci.ErrConcurrencyLimit.Error() {
			return nil, pikoci.ErrConcurrencyLimit
		}
		if strings.Contains(resp.Err, pikoci.ErrBuildNotPending.Error()) {
			return nil, pikoci.ErrBuildNotPending
		}
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Build, nil
}

// FindOldestPendingBuild retrieves the oldest pending build for a job.
func (cl *Client) FindOldestPendingBuild(ctx context.Context, tc, pn, jn string) (*build.Build, error) {
	var resp thttp.FindOldestPendingBuildResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/builds/oldest-pending", cl.url, tc, pn, jn), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Build, nil
}

// NotifySerialGroupPendingBuilds asks the server to notify pending builds for
// all jobs sharing serial groups with the given job.
func (cl *Client) NotifySerialGroupPendingBuilds(ctx context.Context, tc, pn, jn string) {
	_ = cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/notify-serial-groups", cl.url, tc, pn, jn), nil, nil)
}

// ListJobBuilds always fetches all builds (limit=0) for CLI backward compat.
// The before/after/limit params satisfy the Service interface but are not sent.
func (cl *Client) ListJobBuilds(ctx context.Context, tc, pn, jn string, before *uint32, after *uint32, limit uint32) ([]*build.Build, bool, error) {
	var resp thttp.ListJobBuildsResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s/pipelines/%s/jobs/%s/builds?limit=0", cl.url, tc, pn, jn), nil, &resp)
	if err != nil {
		return nil, false, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, false, fmt.Errorf("error from request: %s", resp.Err)
	}

	hasMore := false
	if resp.Meta != nil {
		hasMore = resp.Meta.HasMore
	}

	return resp.Builds, hasMore, nil
}

// CreateResourceVersion creates a new version for the specified resource.
func (cl *Client) CreateResourceVersion(ctx context.Context, tc, pn, rCan string, rv resource.Version) (*resource.Version, error) {
	var resp thttp.CreateResourceVersionResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/resources/%s/versions", cl.url, tc, pn, rCan), thttp.CreateResourceVersionRequest{
		Version: rv,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Version, nil
}

// ListResourceVersions always fetches all versions (limit=0) for CLI backward compat.
// The before/after/limit params satisfy the Service interface but are not sent.
func (cl *Client) ListResourceVersions(ctx context.Context, tc, pn, rCan string, before *uint32, after *uint32, limit uint32) ([]*resource.Version, bool, error) {
	var resp thttp.ListResourceVersionsResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s/pipelines/%s/resources/%s/versions?limit=0", cl.url, tc, pn, rCan), nil, &resp)
	if err != nil {
		return nil, false, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, false, fmt.Errorf("error from request: %s", resp.Err)
	}

	hasMore := false
	if resp.Meta != nil {
		hasMore = resp.Meta.HasMore
	}

	return resp.Versions, hasMore, nil
}

// GetPipelineResource retrieves a single resource from a pipeline.
func (cl *Client) GetPipelineResource(ctx context.Context, tc, pn, rCan string) (*resource.Resource, error) {
	var resp thttp.GetPipelineResourceResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s/pipelines/%s/resources/%s", cl.url, tc, pn, rCan), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Resource, nil
}

// UpdatePipelineResource updates the configuration of a pipeline resource.
func (cl *Client) UpdatePipelineResource(ctx context.Context, tc, pn, rCan string, r resource.Resource) error {
	var resp thttp.UpdatePipelineResourceResponse

	err := cl.Request(ctx, http.MethodPut, fmt.Sprintf("%s/teams/%s/pipelines/%s/resources/%s", cl.url, tc, pn, rCan), thttp.UpdatePipelineResourceRequest{
		Resource: r,
	}, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// TriggerPipelineResource triggers a manual check on the specified resource.
func (cl *Client) TriggerPipelineResource(ctx context.Context, tc, pn, rCan string) error {
	var resp thttp.TriggerPipelineResourceResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/resources/%s", cl.url, tc, pn, rCan), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// PinResourceVersion pins a resource to a specific version via the API.
func (cl *Client) PinResourceVersion(ctx context.Context, tc, pn, rCan string, versionID uint32) error {
	var resp thttp.PinResourceVersionResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/resources/%s/pin", cl.url, tc, pn, rCan), thttp.PinResourceVersionRequest{
		VersionID: versionID,
	}, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// UnpinResourceVersion removes a version pin from a resource via the API.
func (cl *Client) UnpinResourceVersion(ctx context.Context, tc, pn, rCan string) error {
	var resp thttp.UnpinResourceVersionResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/resources/%s/unpin", cl.url, tc, pn, rCan), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// TriggerResourceVersion triggers downstream jobs with a specific resource version via the API.
func (cl *Client) TriggerResourceVersion(ctx context.Context, tc, pn, rCan string, versionID uint32) error {
	var resp thttp.TriggerResourceVersionResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/resources/%s/versions/%d/trigger", cl.url, tc, pn, rCan, versionID), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// WebhookTrigger sends a webhook trigger request using the given token.
func (cl *Client) WebhookTrigger(ctx context.Context, token string) error {
	var resp thttp.WebhookTriggerResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/webhooks/%s", cl.url, token), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// RegenerateWebhookToken generates a new webhook token for the specified resource.
func (cl *Client) RegenerateWebhookToken(ctx context.Context, tc, pn, rCan string) (string, error) {
	var resp thttp.RegenerateWebhookTokenResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/pipelines/%s/resources/%s/webhook_token", cl.url, tc, pn, rCan), nil, &resp)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return "", fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Token, nil
}

// WorkerHeartbeat sends a heartbeat for the given worker.
func (cl *Client) WorkerHeartbeat(ctx context.Context, w wkr.Worker) error {
	var resp thttp.WorkerHeartbeatResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/workers/heartbeat", cl.url), thttp.WorkerHeartbeatRequest{
		Name:        w.Name,
		Hostname:    w.Hostname,
		OS:          w.OS,
		Arch:        w.Arch,
		GoVersion:   w.GoVersion,
		Version:     w.Version,
		Concurrency: w.Concurrency,
		Queues:      w.Queues,
		StartedAt:   w.StartedAt.Format(time.RFC3339),
	}, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// ListWorkers retrieves all registered workers.
func (cl *Client) ListWorkers(ctx context.Context) ([]*wkr.Worker, error) {
	var resp thttp.ListWorkersResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/workers", cl.url), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Workers, nil
}

// WorkersHealth checks if at least one worker is healthy.
func (cl *Client) WorkersHealth(ctx context.Context) (bool, error) {
	var resp thttp.WorkersHealthResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/workers/health", cl.url), nil, &resp)
	if err != nil {
		return false, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return false, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Healthy, nil
}

// DeleteWorker removes a worker by name.
func (cl *Client) DeleteWorker(ctx context.Context, name string) error {
	var resp thttp.DeleteWorkerResponse

	err := cl.Request(ctx, http.MethodDelete, fmt.Sprintf("%s/workers/%s", cl.url, name), nil, &resp)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return fmt.Errorf("error from request: %s", resp.Err)
	}

	return nil
}

// NextWork is not used by the client; it is a server-side method.
func (cl *Client) NextWork(ctx context.Context) (*queue.WorkItem, error) {
	return nil, fmt.Errorf("NextWork is not available via HTTP client")
}

// PollNextWork calls the server's long-poll endpoint to wait for available work.
func (cl *Client) PollNextWork(ctx context.Context) (*queue.WorkItem, error) {
	var resp thttp.PollNextWorkResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/work/poll", cl.url), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.WorkItem, nil
}
