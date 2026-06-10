// Package pikoci implements the core orchestration layer for the PikoCI continuous
// integration system. It provides the Service interface that defines all operations
// for managing users, teams, pipelines, jobs, builds, resources, and triggers. The
// PikoCI struct is the primary implementation of this interface, coordinating between
// repository backends, message queues, and the background scheduler.
package pikoci

import (
	"context"

	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/queue"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/scheduler"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/trigger"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/wkr"

	"log/slog"
)

//go:generate go tool mockgen -destination=mock/service.go -mock_names=Service=Service -package mock github.com/pikoci/pikoci/pikoci Service

// Service defines the complete set of operations exposed by the PikoCI orchestration
// layer. It covers user authentication, team management, pipeline CRUD, job triggering,
// build lifecycle, resource version tracking, webhook handling, and trigger management.
type Service interface {
	// UserLogin authenticates a user by username and password, returning the user
	// with team memberships and a signed JWT token.
	UserLogin(ctx context.Context, un, pass string) (*user.WithMemberships, string, error)
	// RefreshToken generates a new JWT token for the given username without
	// requiring the password.
	RefreshToken(ctx context.Context, un string) (*user.WithMemberships, string, error)

	// GetUser retrieves a user and their team memberships by username.
	GetUser(ctx context.Context, un string) (*user.WithMemberships, error)
	// CreateUser creates a new user. If isHash is true, the password is already
	// hashed; otherwise it will be hashed before storage.
	CreateUser(ctx context.Context, u user.User, isHash bool) (*user.User, error)
	// ListUsers returns all registered users.
	ListUsers(ctx context.Context) ([]*user.User, error)
	// UpdateUser updates an existing user identified by username. If isHash is
	// true, the password is treated as already hashed.
	UpdateUser(ctx context.Context, un string, u user.User, isHash bool) (*user.User, error)
	// DeleteUser removes a user by username.
	DeleteUser(ctx context.Context, un string) error
	// ChangePassword updates the password for the given user after verifying the
	// old password.
	ChangePassword(ctx context.Context, un, oldPassword, newPassword string) error
	// UpdateProfile updates a user's display name and optionally their username.
	UpdateProfile(ctx context.Context, un string, fullName, newUsername string) (*user.User, error)

	// CreateTeam creates a new team and adds the specified user as its admin member.
	CreateTeam(ctx context.Context, un string, t team.Team) (*team.WithMembers, error)
	// ListTeams returns all teams that the given user is a member of.
	ListTeams(ctx context.Context, un string) ([]*team.WithMembers, error)
	// GetTeam retrieves a team by its canonical name, including its members.
	GetTeam(ctx context.Context, tc string) (*team.WithMembers, error)
	// UpdateTeam updates a team's properties identified by its canonical name.
	UpdateTeam(ctx context.Context, tc string, t team.Team) (*team.WithMembers, error)
	// DeleteTeam removes a team by its canonical name.
	DeleteTeam(ctx context.Context, tc string) error

	// CreateTeamMember adds a new member to the specified team.
	CreateTeamMember(ctx context.Context, tc string, tm team.Member) (*team.Member, error)
	// UpdateTeamMember updates an existing team member's role within the team.
	UpdateTeamMember(ctx context.Context, tc, mc string, tm team.Member) (*team.Member, error)
	// DeleteTeamMember removes a member from the specified team.
	DeleteTeamMember(ctx context.Context, tc, mc string) error

	// CreatePipeline parses the raw HCL pipeline configuration and persists the
	// pipeline along with its jobs, resources, resource types, runners, and secret types.
	CreatePipeline(ctx context.Context, tc, pn string, pp []byte, vars map[string]interface{}) (*pipeline.Pipeline, error)
	// UpdatePipeline replaces an existing pipeline's configuration, performing a
	// diff-based reconciliation of jobs, resources, resource types, runners, and
	// secret types. An optional newName renames the pipeline.
	UpdatePipeline(ctx context.Context, tc, pCan string, pp []byte, vars map[string]interface{}, newName ...string) (*pipeline.Pipeline, error)
	// GetPipeline retrieves a pipeline by team canonical and pipeline canonical.
	GetPipeline(ctx context.Context, tc, pn string) (*pipeline.Pipeline, error)
	// DeletePipeline removes a pipeline and all its associated entities.
	DeletePipeline(ctx context.Context, tc, pn string) error
	// ListPipelines returns all pipelines for the given team, enriched with last
	// build timestamps.
	ListPipelines(ctx context.Context, tc string) ([]*pipeline.Pipeline, error)

	// SetPipelinePublic toggles the public visibility of a pipeline.
	SetPipelinePublic(ctx context.Context, tc, pn string, public bool) error

	// PausePipeline pauses a pipeline, preventing all its jobs from being triggered.
	PausePipeline(ctx context.Context, tc, pCan string) error
	// UnpausePipeline unpauses a pipeline and unpauses all its jobs.
	UnpausePipeline(ctx context.Context, tc, pCan string) error
	// PauseJob pauses a specific job within a pipeline.
	PauseJob(ctx context.Context, tc, pCan, jn string) error
	// UnpauseJob unpauses a specific job within a pipeline.
	UnpauseJob(ctx context.Context, tc, pCan, jn string) error

	// GetPublicPipeline retrieves a public pipeline with sensitive fields sanitized.
	GetPublicPipeline(ctx context.Context, tc, pn string) (*pipeline.Pipeline, error)
	// GetPublicPipelineImage generates a DOT graph image for a public pipeline.
	GetPublicPipelineImage(ctx context.Context, tc, pn, format string) ([]byte, error)
	// GetPublicPipelineJob retrieves a job from a public pipeline.
	GetPublicPipelineJob(ctx context.Context, tc, pn, jn string) (*job.Job, error)
	// ListPublicJobBuilds returns paginated builds for a job on a public pipeline,
	// with secret step logs redacted.
	ListPublicJobBuilds(ctx context.Context, tc, pn, jn string, before *uint32, after *uint32, limit uint32) ([]*build.Build, bool, error)
	// GetPublicPipelineResource retrieves a resource from a public pipeline with
	// sensitive fields sanitized.
	GetPublicPipelineResource(ctx context.Context, tc, pn, rCan string) (*resource.Resource, error)
	// ListPublicResourceVersions returns paginated resource versions for a public pipeline.
	ListPublicResourceVersions(ctx context.Context, tc, pn, rCan string, before *uint32, after *uint32, limit uint32) ([]*resource.Version, bool, error)

	// GetPipelineImage generates a DOT graph representation of a pipeline's jobs
	// and resources.
	GetPipelineImage(ctx context.Context, tc, pn, format string) ([]byte, error)
	// CreatePipelineImage generates a DOT graph image from raw pipeline
	// configuration bytes without persisting the pipeline.
	CreatePipelineImage(ctx context.Context, tc string, pp []byte, vars map[string]interface{}, format string) ([]byte, error)

	// TriggerPipelineJob creates a pending build for the specified job and enqueues
	// it for execution.
	TriggerPipelineJob(ctx context.Context, tc, pn, jn string) error
	// GetPipelineJob retrieves a job by its name within a pipeline.
	GetPipelineJob(ctx context.Context, tc, pn, jn string) (*job.Job, error)

	// CreateJobBuild creates a new pending build for the specified job.
	CreateJobBuild(ctx context.Context, tc, pn, jn string, b build.Build) (*build.Build, error)
	// CreateRetryJobBuild creates a retry build under the given parent build number.
	CreateRetryJobBuild(ctx context.Context, tc, pn, jn, parentBuildNumber string, b build.Build) (*build.Build, error)
	// UpdateJobBuild updates an existing build's state and metadata.
	UpdateJobBuild(ctx context.Context, tc, pn, jn string, buildNumber string, b build.Build) error
	// DeleteJobBuild removes a build by its build number.
	DeleteJobBuild(ctx context.Context, tc, pn, jn string, buildNumber string) error
	// ListJobBuilds returns paginated builds for a job, supporting cursor-based
	// pagination with before and after parameters. The boolean return value
	// indicates whether more results exist.
	ListJobBuilds(ctx context.Context, tc, pn, jn string, before *uint32, after *uint32, limit uint32) ([]*build.Build, bool, error)
	// GetJobBuild retrieves a single build by its build number.
	GetJobBuild(ctx context.Context, tc, pn, jn string, buildNumber string) (*build.Build, error)
	// CancelJobBuild cancels a running or pending build and notifies the next
	// pending build in the queue.
	CancelJobBuild(ctx context.Context, tc, pn, jn string, buildNumber string) error
	// RetryJobBuild creates a retry of a completed build and enqueues it for execution.
	RetryJobBuild(ctx context.Context, tc, pn, jn, buildNumber string) error
	// FindBuildGetVersions returns the resource version IDs fetched during get
	// steps of the specified build.
	FindBuildGetVersions(ctx context.Context, tc, pn, jn string, buildID uint32) (map[string]uint32, error)

	// StartPendingBuild transitions a pending build to started status, respecting
	// the job's concurrency limit.
	StartPendingBuild(ctx context.Context, tc, pn, jn string, buildID uint32) (*build.Build, error)
	// FindOldestPendingBuild returns the oldest pending build for the specified job.
	FindOldestPendingBuild(ctx context.Context, tc, pn, jn string) (*build.Build, error)
	// NotifySerialGroupPendingBuilds finds all jobs sharing serial groups with the
	// given job and enqueues their oldest pending builds for execution.
	NotifySerialGroupPendingBuilds(ctx context.Context, tc, pn, jn string)

	// GetPipelineResource retrieves a resource by its canonical name within a pipeline.
	GetPipelineResource(ctx context.Context, tc, pn, rCan string) (*resource.Resource, error)
	// UpdatePipelineResource updates a resource's metadata within a pipeline.
	UpdatePipelineResource(ctx context.Context, tc, pn, rCan string, r resource.Resource) error
	// TriggerPipelineResource enqueues a resource check and updates the resource's
	// next check time.
	TriggerPipelineResource(ctx context.Context, tc, pn, rCan string) error
	// CreateResourceVersion creates a new version for the specified resource.
	CreateResourceVersion(ctx context.Context, tc, pn, rCan string, v resource.Version) (*resource.Version, error)
	// ListResourceVersions returns paginated versions for a resource, supporting
	// cursor-based pagination.
	ListResourceVersions(ctx context.Context, tc, pn, rCan string, before *uint32, after *uint32, limit uint32) ([]*resource.Version, bool, error)

	// InsertBuildGetVersion records the resource version fetched by a get step
	// during a build.
	InsertBuildGetVersion(ctx context.Context, tc, pn, jn string, buildID uint32, stepName string, versionID uint32) error

	// PinResourceVersion pins a resource to a specific version, preventing the scheduler from advancing.
	PinResourceVersion(ctx context.Context, tc, pn, rCan string, versionID uint32) error
	// UnpinResourceVersion removes the version pin from a resource.
	UnpinResourceVersion(ctx context.Context, tc, pn, rCan string) error
	// TriggerResourceVersion triggers immediate downstream jobs with a specific resource version.
	TriggerResourceVersion(ctx context.Context, tc, pn, rCan string, versionID uint32) error

	// WebhookTrigger triggers a resource check using the resource's unique
	// webhook token.
	WebhookTrigger(ctx context.Context, token string) error
	// RegenerateWebhookToken generates a new webhook token for the specified
	// resource, invalidating the old one.
	RegenerateWebhookToken(ctx context.Context, tc, pn, rCan string) (string, error)

	// CreateTrigger creates a new trigger event with the given name and version
	// data within a team.
	CreateTrigger(ctx context.Context, tc, name string, version map[string]interface{}) (*trigger.Trigger, error)
	// ListTriggersAfter returns all trigger events with IDs greater than afterID
	// for the given trigger name.
	ListTriggersAfter(ctx context.Context, tc, name string, afterID uint32) ([]*trigger.Trigger, error)

	// WorkerHeartbeat registers or updates a worker's heartbeat.
	WorkerHeartbeat(ctx context.Context, w wkr.Worker) error
	// ListWorkers returns all registered workers.
	ListWorkers(ctx context.Context) ([]*wkr.Worker, error)
	// WorkersHealth returns true if at least one worker is healthy.
	WorkersHealth(ctx context.Context) (bool, error)
	// DeleteWorker removes a worker by name.
	DeleteWorker(ctx context.Context, name string) error

	// NextWork finds the next available work item (pending build or due resource check).
	NextWork(ctx context.Context) (*queue.WorkItem, error)
	// PollNextWork blocks until work is available or timeout (30s).
	PollNextWork(ctx context.Context) (*queue.WorkItem, error)
}

// PikoCI is the primary implementation of the Service interface. It coordinates
// between repository backends for persistence, a work notifier for async job and
// resource check dispatching, and a background scheduler for periodic resource
// checks.
type PikoCI struct {
	// Notifier broadcasts work availability to waiting workers.
	Notifier *notifier.WorkNotifier
	// Users is the repository for user persistence.
	Users user.Repository
	// Teams is the repository for team persistence.
	Teams         team.Repository
	// Pipelines is the repository for pipeline persistence.
	Pipelines     pipeline.Repository
	// Jobs is the repository for job persistence.
	Jobs          job.Repository
	// Resources is the repository for resource persistence.
	Resources     resource.Repository
	// ResourceTypes is the repository for resource type persistence.
	ResourceTypes restype.Repository
	// Builds is the repository for build persistence.
	Builds        build.Repository
	// Runners is the repository for runner persistence.
	Runners       runner.Repository
	// SecretTypes is the repository for secret type persistence.
	SecretTypes   sectype.Repository
	// Triggers is the repository for trigger persistence.
	Triggers      trigger.Repository
	// Workers is the repository for worker persistence.
	Workers       wkr.Repository
	// StartUoW begins a new unit of work for transactional operations.
	StartUoW      unitwork.StartUnitOfWork
	// Ctx is the root context for the service.
	Ctx           context.Context

	// JWTSecret is the signing key used for JWT token generation and validation.
	JWTSecret []byte

	scheduler *scheduler.Scheduler
	logger    *slog.Logger
}

// New creates a new PikoCI service instance with all required dependencies. It
// initializes the internal scheduler for periodic resource checks and returns
// the configured service ready for use.
func New(ctx context.Context, ur user.Repository, tr team.Repository, pr pipeline.Repository, jr job.Repository, rr resource.Repository, rt restype.Repository, br build.Repository, rur runner.Repository, str sectype.Repository, tgr trigger.Repository, wr wkr.Repository, suow unitwork.StartUnitOfWork, js []byte, wn *notifier.WorkNotifier, l *slog.Logger) *PikoCI {
	return &PikoCI{
		Ctx:           ctx,
		Notifier:      wn,
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
		Workers:       wr,
		StartUoW:      suow,
		JWTSecret:     js,
		logger:        l,
		scheduler:     scheduler.New(rr, pr, br, wn, l),
	}
}

// StartScheduler starts the background scheduler that polls for due resources.
func (q *PikoCI) StartScheduler(ctx context.Context) {
	q.scheduler.Start(ctx)
}
