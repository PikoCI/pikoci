package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/xescugc/pikoci/pikoci"
	"github.com/xescugc/pikoci/pikoci/build"
	"github.com/xescugc/pikoci/pikoci/job"
	"github.com/xescugc/pikoci/pikoci/pipeline"
	"github.com/xescugc/pikoci/pikoci/resource"
	"github.com/xescugc/pikoci/pikoci/team"
	thttp "github.com/xescugc/pikoci/pikoci/transport/http"
	"github.com/xescugc/pikoci/pikoci/user"
)

type Client struct {
	url        string
	jwt        string
	configPath string
}

// New returns a new HTTP Client for QID
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

func (cl *Client) GetPublicPipeline(ctx context.Context, tc, pn string) (*pipeline.Pipeline, error) {
	return cl.GetPipeline(ctx, tc, pn)
}

func (cl *Client) GetPublicPipelineImage(ctx context.Context, tc, pn, format string) ([]byte, error) {
	return cl.GetPipelineImage(ctx, tc, pn, format)
}

func (cl *Client) GetPublicPipelineJob(ctx context.Context, tc, pn, jn string) (*job.Job, error) {
	return cl.GetPipelineJob(ctx, tc, pn, jn)
}

func (cl *Client) ListPublicJobBuilds(ctx context.Context, tc, pn, jn string, before *uint32, after *uint32, limit uint32) ([]*build.Build, bool, error) {
	return cl.ListJobBuilds(ctx, tc, pn, jn, before, after, limit)
}

func (cl *Client) GetPublicPipelineResource(ctx context.Context, tc, pn, rCan string) (*resource.Resource, error) {
	return cl.GetPipelineResource(ctx, tc, pn, rCan)
}

func (cl *Client) ListPublicResourceVersions(ctx context.Context, tc, pn, rCan string, before *uint32, after *uint32, limit uint32) ([]*resource.Version, bool, error) {
	return cl.ListResourceVersions(ctx, tc, pn, rCan, before, after, limit)
}

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

func (cl *Client) GetPipelineImage(ctx context.Context, tc, pn, format string) ([]byte, error) {
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
