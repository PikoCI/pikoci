package http

// RouteName is an enumeration of all named API routes.
// Each value corresponds to an HTTP endpoint registered in the handler.
type RouteName int

//go:generate go tool enumer -type=RouteName -transform=snake -output=route_names_string.go

const (
	// UserLogin is the route for user login requests.
	UserLogin RouteName = iota
	// RefreshToken is the route for refreshing an authentication token.
	RefreshToken

	// CreateUser is the route for creating a new user.
	CreateUser
	// ListUsers is the route for listing all users.
	ListUsers
	// GetUser is the route for retrieving a single user by username.
	GetUser
	// UpdateUser is the route for updating a user's details.
	UpdateUser
	// DeleteUser is the route for deleting a user.
	DeleteUser
	// ChangePassword is the route for changing the current user's password.
	ChangePassword
	// UpdateProfile is the route for updating the current user's profile.
	UpdateProfile

	// CreateTeam is the route for creating a new team.
	CreateTeam
	// ListTeams is the route for listing all teams.
	ListTeams
	// GetTeam is the route for retrieving a single team by canonical name.
	GetTeam
	// UpdateTeam is the route for updating a team's details.
	UpdateTeam
	// DeleteTeam is the route for deleting a team.
	DeleteTeam

	// CreateTeamMember is the route for adding a member to a team.
	CreateTeamMember
	// UpdateTeamMember is the route for updating a team member's role.
	UpdateTeamMember
	// DeleteTeamMember is the route for removing a member from a team.
	DeleteTeamMember

	// CreatePipeline is the route for creating a new pipeline.
	CreatePipeline
	// UpdatePipeline is the route for updating a pipeline's configuration.
	UpdatePipeline
	// GetPipeline is the route for retrieving a single pipeline.
	GetPipeline
	// DeletePipeline is the route for deleting a pipeline.
	DeletePipeline
	// ListPipelines is the route for listing all pipelines in a team.
	ListPipelines

	// GetPipelineImage is the route for retrieving a pipeline's image.
	GetPipelineImage
	// CreatePipelineImage is the route for generating a pipeline image from config.
	CreatePipelineImage

	// TriggerPipelineJob is the route for triggering a pipeline job.
	TriggerPipelineJob
	// ListPipelineJobs is the route for listing all jobs of a pipeline with status.
	ListPipelineJobs
	// GetPipelineJob is the route for retrieving a single pipeline job.
	GetPipelineJob

	// CreateJobBuild is the route for creating a new build for a job.
	CreateJobBuild
	// CreateRetryJobBuild is the route for creating a retry build for a job.
	CreateRetryJobBuild
	// UpdateJobBuild is the route for updating a build's status or details.
	UpdateJobBuild
	// DeleteJobBuild is the route for deleting a build.
	DeleteJobBuild
	// ListJobBuilds is the route for listing all builds of a job.
	ListJobBuilds
	// InsertBuildGetVersion is the route for recording a get-step version for a build.
	InsertBuildGetVersion
	// FindBuildGetVersions is the route for retrieving get-step versions of a build.
	FindBuildGetVersions
	// GetJobBuild is the route for retrieving a single build by number.
	GetJobBuild
	// CancelJobBuild is the route for cancelling a running build.
	CancelJobBuild
	// RetryJobBuild is the route for retrying a completed build.
	RetryJobBuild
	// StartPendingBuild is the route for transitioning a pending build to started.
	StartPendingBuild
	// FindOldestPendingBuild is the route for finding the oldest pending build for a job.
	FindOldestPendingBuild
	// NotifySerialGroupPendingBuilds is the route for notifying pending builds in serial groups.
	NotifySerialGroupPendingBuilds
	// EvaluateDownstreamJobs is the route for evaluating downstream jobs after a build succeeds.
	EvaluateDownstreamJobs

	// ListPipelineResources is the route for listing all resources of a pipeline.
	ListPipelineResources
	// GetPipelineResource is the route for retrieving a single pipeline resource.
	GetPipelineResource
	// UpdatePipelineResource is the route for updating a pipeline resource.
	UpdatePipelineResource
	// TriggerPipelineResource is the route for triggering a resource check.
	TriggerPipelineResource
	// CreateResourceVersion is the route for creating a new resource version.
	CreateResourceVersion
	// ListResourceVersions is the route for listing all versions of a resource.
	ListResourceVersions

	// PinResourceVersion is the route for pinning a resource to a specific version.
	PinResourceVersion
	// UnpinResourceVersion is the route for unpinning a resource version.
	UnpinResourceVersion
	// TriggerResourceVersion is the route for triggering downstream jobs with a specific version.
	TriggerResourceVersion

	// GetResourceVersionPath is the route for retrieving a version's path through the pipeline.
	GetResourceVersionPath

	// WebhookTrigger is the route for incoming webhook triggers.
	WebhookTrigger
	// RegenerateWebhookToken is the route for regenerating a resource's webhook token.
	RegenerateWebhookToken

	// CreateTrigger is the route for creating a trigger event.
	CreateTrigger
	// ListTriggersAfter is the route for listing triggers after a given point.
	ListTriggersAfter

	// ExportDatabase is the route for exporting the database as a binary dump.
	ExportDatabase

	// PausePipeline is the route for pausing a pipeline.
	PausePipeline
	// UnpausePipeline is the route for unpausing a pipeline.
	UnpausePipeline
	// PauseJob is the route for pausing a job.
	PauseJob
	// UnpauseJob is the route for unpausing a job.
	UnpauseJob

	// GetVersion is the route for retrieving the application version.
	GetVersion

	// WorkerHeartbeat is the route for worker heartbeat registration.
	WorkerHeartbeat
	// ListWorkers is the route for listing all registered workers.
	ListWorkers
	// WorkersHealth is the route for checking if any worker is healthy.
	WorkersHealth
	// DeleteWorker is the route for deleting a worker by name.
	DeleteWorker

	// CreateApiToken is the route for creating a new API token.
	CreateApiToken
	// ListApiTokens is the route for listing the current user's API tokens.
	ListApiTokens
	// DeleteApiToken is the route for deleting an API token.
	DeleteApiToken

	// ListAuditLog is the route for listing audit log entries for a team.
	ListAuditLog

	// GetBuildReport is the route for exporting a build report as JSON.
	GetBuildReport

	// ApproveBuild is the route for approving a build waiting for approval.
	ApproveBuild
	// RejectBuild is the route for rejecting a build waiting for approval.
	RejectBuild

	// MarkBuildAsWarning is the route for marking a failed build as warning.
	MarkBuildAsWarning

	// GenerateTeamWorkerToken is the route for generating a team worker token.
	GenerateTeamWorkerToken
	// GetTeamWorkerToken is the route for retrieving the current team worker token.
	GetTeamWorkerToken

	// GetAuthMethods is the route for listing available auth methods.
	GetAuthMethods
	// OAuthStart is the route for starting an OAuth flow.
	OAuthStart
	// OAuthCallback is the route for handling OAuth provider callbacks.
	OAuthCallback
	// OAuthCompleteProfile is the route for completing profile after OAuth login.
	OAuthCompleteProfile
	// ListOAuthProviders is the route for listing all OAuth providers (admin).
	ListOAuthProviders
	// CreateOAuthProvider is the route for creating an OAuth provider (admin).
	CreateOAuthProvider
	// UpdateOAuthProvider is the route for updating an OAuth provider (admin).
	UpdateOAuthProvider
	// DeleteOAuthProvider is the route for deleting an OAuth provider (admin).
	DeleteOAuthProvider
	// GetAdminAuthSettings is the route for getting auth settings (admin).
	GetAdminAuthSettings
	// UpdateAdminAuthSettings is the route for updating auth settings (admin).
	UpdateAdminAuthSettings
	// ListLinkedAccounts is the route for listing user's linked OAuth accounts.
	ListLinkedAccounts
	// UnlinkAccount is the route for unlinking an OAuth account.
	UnlinkAccount
)
