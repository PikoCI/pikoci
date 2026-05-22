package http

type RouteName int

//go:generate go tool enumer -type=RouteName -transform=snake -output=route_names_string.go

const (
	UserLogin RouteName = iota
	RefreshToken

	CreateUser
	ListUsers

	CreateTeam
	ListTeams
	GetTeam
	UpdateTeam
	DeleteTeam

	CreateTeamMember
	UpdateTeamMember
	DeleteTeamMember

	CreatePipeline
	UpdatePipeline
	GetPipeline
	DeletePipeline
	ListPipelines

	GetPipelineImage
	CreatePipelineImage

	TriggerPipelineJob
	GetPipelineJob

	CreateJobBuild
	CreateRetryJobBuild
	UpdateJobBuild
	DeleteJobBuild
	ListJobBuilds
	InsertBuildGetVersion
	FindBuildGetVersions
	GetJobBuild
	CancelJobBuild
	RetryJobBuild

	GetPipelineResource
	UpdatePipelineResource
	TriggerPipelineResource
	CreateResourceVersion
	ListResourceVersions

	WebhookTrigger
	RegenerateWebhookToken

	CreateTrigger
	ListTriggersAfter
)
