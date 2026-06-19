package http

import (
	"context"
	"fmt"

	"github.com/pikoci/pikoci/pikoci"
)

type authorizationFn func(ctx context.Context, s pikoci.Service, un, tc string) error

var (
	routeAuthorization = map[RouteName]authorizationFn{
		UserLogin:    nothing,
		RefreshToken: nothing,

		CreateUser: admin,
		ListUsers:  admin,
		GetUser:    admin,
		UpdateUser: admin,
		DeleteUser: admin,
		ChangePassword: member,
		UpdateProfile:  member,

		CreateTeam: admin,
		ListTeams:  member,
		GetTeam:    member,
		UpdateTeam: admin,
		DeleteTeam: admin,

		CreateTeamMember: admin,
		UpdateTeamMember: admin,
		DeleteTeamMember: admin,

		CreatePipeline: admin,
		UpdatePipeline: admin,
		GetPipeline:    member,
		DeletePipeline: admin,
		ListPipelines:  member,

		GetPipelineImage:    member,
		CreatePipelineImage: admin,

		ListPipelineJobs:   member,
		TriggerPipelineJob: member,
		GetPipelineJob:     member,

		CreateJobBuild:       admin,
		CreateRetryJobBuild:  admin,
		UpdateJobBuild:       admin,
		DeleteJobBuild:       admin,
		ListJobBuilds:        member,
		GetJobBuild:          member,
		CancelJobBuild:         member,
		RetryJobBuild:          member,
		StartPendingBuild:              admin,
		FindOldestPendingBuild:         admin,
		NotifySerialGroupPendingBuilds: admin,
		EvaluateDownstreamJobs:         admin,
		InsertBuildGetVersion:          admin,
		FindBuildGetVersions:   admin,

		ListPipelineResources:   member,
		GetPipelineResource:     member,
		UpdatePipelineResource:  admin,
		TriggerPipelineResource: member,
		CreateResourceVersion:   admin,
		ListResourceVersions:    member,

		PinResourceVersion:     member,
		UnpinResourceVersion:   member,
		TriggerResourceVersion: member,
		GetResourceVersionPath: member,

		WebhookTrigger:         nothing,
		RegenerateWebhookToken: admin,

		CreateTrigger:     admin,
		ListTriggersAfter: member,

		ExportDatabase: admin,

		PausePipeline:   member,
		UnpausePipeline: member,
		PauseJob:        member,
		UnpauseJob:      member,

		WorkerHeartbeat: nothing,
		ListWorkers:     admin,
		WorkersHealth:   admin,
		DeleteWorker:    admin,
	}
)

func nothing(ctx context.Context, s pikoci.Service, un, tc string) error { return nil }

func admin(ctx context.Context, s pikoci.Service, un, tc string) error {
	um, err := s.GetUser(ctx, un)
	if err != nil {
		return fmt.Errorf("failed to GetUser: %w", err)
	}
	if !um.IsAdmin(tc) {
		return fmt.Errorf("needs to be admin")
	}
	return nil
}

func member(ctx context.Context, s pikoci.Service, un, tc string) error {
	um, err := s.GetUser(ctx, un)
	if err != nil {
		return fmt.Errorf("failed to GetUser: %w", err)
	}
	if !um.IsMember(tc) {
		return fmt.Errorf("needs to be member")
	}
	return nil
}
