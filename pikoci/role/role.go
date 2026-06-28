// Package role defines the hierarchical RBAC roles for PikoCI teams.
//
// # Role Hierarchy
//
// Each role inherits all permissions of roles below it:
//
//	public   (level 0) — unauthenticated access to public pipelines (not assignable to users)
//	read     (level 1) — read-only: view pipelines, jobs, builds, resources, team info
//	write    (level 2) — trigger/cancel/retry builds, pause/unpause pipelines and jobs, pin/unpin resources
//	maintain (level 3) — pipeline CRUD: create/update/delete pipelines, manage resources, regenerate webhooks
//	admin    (level 4) — team management: add/remove/change members, update team settings, delete team
//	                      (at least one admin required per team)
//
// Global admin (user.Admin flag) is a superuser that transcends all team-level roles.
//
// # Route Mapping
//
//	nothing:     UserLogin, RefreshToken, WebhookTrigger, WorkerHeartbeat, GetVersion
//	public:      GetPipeline, GetPipelineImage, ListPipelineJobs, GetPipelineJob, ListJobBuilds,
//	             ListPipelineResources, GetPipelineResource, ListResourceVersions, GetResourceVersionPath
//	read:        ListPipelines, ListTeams, GetTeam, GetJobBuild, ChangePassword, UpdateProfile, ListTriggersAfter
//	write:       TriggerPipelineJob, CancelJobBuild, RetryJobBuild, PausePipeline, UnpausePipeline,
//	             PauseJob, UnpauseJob, PinResourceVersion, UnpinResourceVersion, TriggerResourceVersion,
//	             TriggerPipelineResource
//	maintain:    CreatePipeline, UpdatePipeline, DeletePipeline, CreatePipelineImage,
//	             UpdatePipelineResource, CreateResourceVersion, RegenerateWebhookToken, CreateTrigger
//	admin:       CreateTeamMember, UpdateTeamMember, DeleteTeamMember, UpdateTeam, DeleteTeam
//	globalAdmin: CreateUser, ListUsers, GetUser, UpdateUser, DeleteUser, CreateTeam,
//	             ListWorkers, WorkersHealth, DeleteWorker, ExportDatabase
package role

// Role represents a user's role within a team.
type Role string

const (
	Public   Role = "public"
	Read     Role = "read"
	Write    Role = "write"
	Maintain Role = "maintain"
	Admin    Role = "admin"
)

var roleLevel = map[Role]int{
	Public:   0,
	Read:     1,
	Write:    2,
	Maintain: 3,
	Admin:    4,
}

// Level returns the numeric level of the role. Invalid roles return -1.
func (r Role) Level() int {
	l, ok := roleLevel[r]
	if !ok {
		return -1
	}
	return l
}

// AtLeast reports whether this role is at or above the required role level.
func (r Role) AtLeast(required Role) bool {
	return r.Level() >= required.Level() && r.Level() >= 0
}

// Valid reports whether this role is one of the 5 defined roles.
func (r Role) Valid() bool {
	_, ok := roleLevel[r]
	return ok
}

// Assignable reports whether this role can be assigned to a user.
// Public is a valid role but not assignable to users.
func (r Role) Assignable() bool {
	return r.Valid() && r != Public
}
