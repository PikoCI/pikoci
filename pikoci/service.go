package pikoci

import (
	"context"

	"github.com/xescugc/pikoci/pikoci/build"
	"github.com/xescugc/pikoci/pikoci/job"
	"github.com/xescugc/pikoci/pikoci/pipeline"
	"github.com/xescugc/pikoci/pikoci/queue"
	"github.com/xescugc/pikoci/pikoci/resource"
	"github.com/xescugc/pikoci/pikoci/restype"
	"github.com/xescugc/pikoci/pikoci/runner"
	"github.com/xescugc/pikoci/pikoci/scheduler"
	"github.com/xescugc/pikoci/pikoci/sectype"
	"github.com/xescugc/pikoci/pikoci/team"
	"github.com/xescugc/pikoci/pikoci/trigger"
	"github.com/xescugc/pikoci/pikoci/unitwork"
	"github.com/xescugc/pikoci/pikoci/user"

	"log/slog"
)

//go:generate go tool mockgen -destination=mock/service.go -mock_names=Service=Service -package mock github.com/xescugc/pikoci/pikoci Service

type Service interface {
	UserLogin(ctx context.Context, un, pass string) (*user.WithMemberships, string, error)
	RefreshToken(ctx context.Context, un string) (*user.WithMemberships, string, error)

	GetUser(ctx context.Context, un string) (*user.WithMemberships, error)
	CreateUser(ctx context.Context, u user.User, isHash bool) (*user.User, error)
	ListUsers(ctx context.Context) ([]*user.User, error)
	UpdateUser(ctx context.Context, un string, u user.User, isHash bool) (*user.User, error)
	DeleteUser(ctx context.Context, un string) error
	ChangePassword(ctx context.Context, un, oldPassword, newPassword string) error
	UpdateProfile(ctx context.Context, un string, fullName, newUsername string) (*user.User, error)

	CreateTeam(ctx context.Context, un string, t team.Team) (*team.WithMembers, error)
	ListTeams(ctx context.Context, un string) ([]*team.WithMembers, error)
	GetTeam(ctx context.Context, tc string) (*team.WithMembers, error)
	UpdateTeam(ctx context.Context, tc string, t team.Team) (*team.WithMembers, error)
	DeleteTeam(ctx context.Context, tc string) error

	CreateTeamMember(ctx context.Context, tc string, tm team.Member) (*team.Member, error)
	UpdateTeamMember(ctx context.Context, tc, mc string, tm team.Member) (*team.Member, error)
	DeleteTeamMember(ctx context.Context, tc, mc string) error

	CreatePipeline(ctx context.Context, tc, pn string, pp []byte, vars map[string]interface{}) (*pipeline.Pipeline, error)
	UpdatePipeline(ctx context.Context, tc, pCan string, pp []byte, vars map[string]interface{}, newName ...string) (*pipeline.Pipeline, error)
	GetPipeline(ctx context.Context, tc, pn string) (*pipeline.Pipeline, error)
	DeletePipeline(ctx context.Context, tc, pn string) error
	ListPipelines(ctx context.Context, tc string) ([]*pipeline.Pipeline, error)

	SetPipelinePublic(ctx context.Context, tc, pn string, public bool) error

	GetPublicPipeline(ctx context.Context, tc, pn string) (*pipeline.Pipeline, error)
	GetPublicPipelineImage(ctx context.Context, tc, pn, format string) ([]byte, error)
	GetPublicPipelineJob(ctx context.Context, tc, pn, jn string) (*job.Job, error)
	ListPublicJobBuilds(ctx context.Context, tc, pn, jn string, before *uint32, after *uint32, limit uint32) ([]*build.Build, bool, error)
	GetPublicPipelineResource(ctx context.Context, tc, pn, rCan string) (*resource.Resource, error)
	ListPublicResourceVersions(ctx context.Context, tc, pn, rCan string, before *uint32, after *uint32, limit uint32) ([]*resource.Version, bool, error)

	GetPipelineImage(ctx context.Context, tc, pn, format string) ([]byte, error)
	CreatePipelineImage(ctx context.Context, tc string, pp []byte, vars map[string]interface{}, format string) ([]byte, error)

	TriggerPipelineJob(ctx context.Context, tc, pn, jn string) error
	GetPipelineJob(ctx context.Context, tc, pn, jn string) (*job.Job, error)

	CreateJobBuild(ctx context.Context, tc, pn, jn string, b build.Build) (*build.Build, error)
	CreateRetryJobBuild(ctx context.Context, tc, pn, jn, parentBuildNumber string, b build.Build) (*build.Build, error)
	UpdateJobBuild(ctx context.Context, tc, pn, jn string, buildNumber string, b build.Build) error
	DeleteJobBuild(ctx context.Context, tc, pn, jn string, buildNumber string) error
	ListJobBuilds(ctx context.Context, tc, pn, jn string, before *uint32, after *uint32, limit uint32) ([]*build.Build, bool, error)
	GetJobBuild(ctx context.Context, tc, pn, jn string, buildNumber string) (*build.Build, error)
	CancelJobBuild(ctx context.Context, tc, pn, jn string, buildNumber string) error
	RetryJobBuild(ctx context.Context, tc, pn, jn, buildNumber string) error
	FindBuildGetVersions(ctx context.Context, tc, pn, jn string, buildID uint32) (map[string]uint32, error)

	StartPendingBuild(ctx context.Context, tc, pn, jn string, buildID uint32) (*build.Build, error)
	FindOldestPendingBuild(ctx context.Context, tc, pn, jn string) (*build.Build, error)

	GetPipelineResource(ctx context.Context, tc, pn, rCan string) (*resource.Resource, error)
	UpdatePipelineResource(ctx context.Context, tc, pn, rCan string, r resource.Resource) error
	TriggerPipelineResource(ctx context.Context, tc, pn, rCan string) error
	CreateResourceVersion(ctx context.Context, tc, pn, rCan string, v resource.Version) (*resource.Version, error)
	ListResourceVersions(ctx context.Context, tc, pn, rCan string, before *uint32, after *uint32, limit uint32) ([]*resource.Version, bool, error)

	InsertBuildGetVersion(ctx context.Context, tc, pn, jn string, buildID uint32, stepName string, versionID uint32) error

	WebhookTrigger(ctx context.Context, token string) error
	RegenerateWebhookToken(ctx context.Context, tc, pn, rCan string) (string, error)

	CreateTrigger(ctx context.Context, tc, name string, version map[string]interface{}) (*trigger.Trigger, error)
	ListTriggersAfter(ctx context.Context, tc, name string, afterID uint32) ([]*trigger.Trigger, error)
}

type PikoCI struct {
	JobTopic   queue.Topic
	CheckTopic queue.Topic
	Users      user.Repository
	Teams         team.Repository
	Pipelines     pipeline.Repository
	Jobs          job.Repository
	Resources     resource.Repository
	ResourceTypes restype.Repository
	Builds        build.Repository
	Runners       runner.Repository
	SecretTypes   sectype.Repository
	Triggers      trigger.Repository
	StartUoW      unitwork.StartUnitOfWork
	Ctx           context.Context

	JWTSecret []byte

	scheduler *scheduler.Scheduler
	logger    *slog.Logger
}

func New(ctx context.Context, jobTopic, checkTopic queue.Topic, ur user.Repository, tr team.Repository, pr pipeline.Repository, jr job.Repository, rr resource.Repository, rt restype.Repository, br build.Repository, rur runner.Repository, str sectype.Repository, tgr trigger.Repository, suow unitwork.StartUnitOfWork, js []byte, l *slog.Logger) *PikoCI {
	return &PikoCI{
		Ctx:           ctx,
		JobTopic:      jobTopic,
		CheckTopic:    checkTopic,
		Users:         ur,
		Teams:         tr,
		Pipelines:     pr,
		Jobs:          jr,
		Resources:     rr,
		ResourceTypes: rt,
		Builds:        br,
		Runners:       rur,
		SecretTypes:   str,
		Triggers:      tgr,
		StartUoW:      suow,
		JWTSecret:     js,
		logger:        l,
		scheduler:     scheduler.New(rr, pr, br, jobTopic, checkTopic, l),
	}
}

// StartScheduler starts the background scheduler that polls for due resources.
func (q *PikoCI) StartScheduler(ctx context.Context) {
	q.scheduler.Start(ctx)
}
