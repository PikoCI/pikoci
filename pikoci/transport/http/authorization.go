package http

import (
	"context"
	"fmt"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/apitoken"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/user"
)

type authorizationFn func(ctx context.Context, s pikoci.Service, un, tc string) error

var (
	routeAuthorization = map[RouteName]authorizationFn{
		// No auth required
		UserLogin:            nothing,
		RefreshToken:         nothing,
		WebhookTrigger:       nothing,
		WorkerHeartbeat:      nothing,
		GetVersion:           nothing,
		GetHealth:            nothing,
		GetAuthMethods:       nothing,
		OAuthStart:           nothing,
		OAuthCallback:        nothing,
		OAuthCompleteProfile: nothing,

		// Public-level routes: unauthenticated access to public pipelines
		GetPipeline:          requirePublicOrRole(role.Read),
		GetPipelineImage:     requirePublicOrRole(role.Read),
		ListPipelineJobs:     requirePublicOrRole(role.Read),
		GetPipelineJob:       requirePublicOrRole(role.Read),
		ListJobBuilds:        requirePublicOrRole(role.Read),
		ListPipelineResources: requirePublicOrRole(role.Read),
		GetPipelineResource:   requirePublicOrRole(role.Read),
		ListResourceVersions:    requirePublicOrRole(role.Read),
		GetResourceVersionPath:  requirePublicOrRole(role.Read),

		// Read routes
		ListPipelines:    requireRole(role.Read),
		ListTeams:        requireRole(role.Read),
		GetTeam:          requireRole(role.Read),
		GetJobBuild:      requireRole(role.Read),
		GetBuildReport:   requireRole(role.Read),
		ChangePassword:     requireRole(role.Read),
		UpdateProfile:      requireRole(role.Read),
		ListLinkedAccounts: requireRole(role.Read),
		UnlinkAccount:      requireRole(role.Read),
		ListTriggersAfter: requireRole(role.Read),

		ListAuditLog:     requireRole(role.Read),

		// Write routes
		TriggerPipelineJob:     requireRole(role.Write),
		CancelJobBuild:        requireRole(role.Write),
		RetryJobBuild:         requireRole(role.Write),
		PausePipeline:         requireRole(role.Write),
		UnpausePipeline:       requireRole(role.Write),
		PauseJob:              requireRole(role.Write),
		UnpauseJob:            requireRole(role.Write),
		PinResourceVersion:    requireRole(role.Write),
		UnpinResourceVersion:  requireRole(role.Write),
		TriggerResourceVersion: requireRole(role.Write),
		TriggerPipelineResource: requireRole(role.Write),

		// Maintain routes
		CreatePipeline:         requireRole(role.Maintain),
		UpdatePipeline:         requireRole(role.Maintain),
		DeletePipeline:         requireRole(role.Maintain),
		CreatePipelineImage:    requireRole(role.Maintain),
		UpdatePipelineResource: requireRole(role.Maintain),
		CreateResourceVersion:  requireRole(role.Maintain),
		RegenerateWebhookToken: requireRole(role.Maintain),
		CreateTrigger:          requireRole(role.Maintain),

		// Worker-internal routes (bypassed by isFromWorker JWT)
		FireTriggerNotifications:       requireRole(role.Maintain),
		CreateJobBuild:                 requireRole(role.Maintain),
		CreateRetryJobBuild:            requireRole(role.Maintain),
		UpdateJobBuild:                 requireRole(role.Maintain),
		DeleteJobBuild:                 requireRole(role.Maintain),
		StartPendingBuild:              requireRole(role.Maintain),
		FindOldestPendingBuild:         requireRole(role.Maintain),
		NotifySerialGroupPendingBuilds: requireRole(role.Maintain),
		EvaluateDownstreamJobs:         requireRole(role.Maintain),
		InsertBuildGetVersion:          requireRole(role.Maintain),
		FindBuildGetVersions:           requireRole(role.Maintain),

		// Approval routes (Maintain+)
		ApproveBuild:       requireRole(role.Maintain),
		RejectBuild:        requireRole(role.Maintain),
		MarkBuildAsWarning: requireRole(role.Maintain),

		// Admin routes
		CreateTeamMember:        requireRole(role.Admin),
		UpdateTeamMember:        requireRole(role.Admin),
		DeleteTeamMember:        requireRole(role.Admin),
		UpdateTeam:              requireRole(role.Admin),
		DeleteTeam:              requireRole(role.Admin),
		GenerateTeamWorkerToken: requireRole(role.Admin),
		GetTeamWorkerToken:      requireRole(role.Admin),

		// Global admin routes
		CreateUser:     globalAdmin,
		ListUsers:      globalAdmin,
		GetUser:        globalAdmin,
		UpdateUser:     globalAdmin,
		DeleteUser:     globalAdmin,
		CreateTeam:     globalAdmin,
		ListWorkers:    globalAdmin,
		WorkersHealth:  globalAdmin,
		DeleteWorker:   globalAdmin,
		ExportDatabase:         globalAdmin,
		ListOAuthProviders:     globalAdmin,
		CreateOAuthProvider:    globalAdmin,
		UpdateOAuthProvider:    globalAdmin,
		DeleteOAuthProvider:    globalAdmin,
		GetAdminAuthSettings:   globalAdmin,
		UpdateAdminAuthSettings: globalAdmin,

		// API token management routes (JWT only — tokens cannot manage tokens)
		CreateApiToken: jwtOnly(requireRole(role.Read)),
		ListApiTokens:  jwtOnly(requireRole(role.Read)),
		DeleteApiToken: jwtOnly(requireRole(role.Read)),

		// Secret store. Writes match the other pipeline-configuration routes at
		// Maintain. Listing is Read and returns plain values in the clear;
		// secret values are never returned at any role.
		SetTeamSecret:        requireRole(role.Maintain),
		ListTeamSecrets:      requireRole(role.Read),
		DeleteTeamSecret:     requireRole(role.Maintain),
		SetPipelineSecret:    requireRole(role.Maintain),
		ListPipelineSecrets:  requireRole(role.Read),
		DeletePipelineSecret: requireRole(role.Maintain),

		// Resolved values are for workers only. Reaching this as a user is
		// always a denial: there is deliberately no secret-reveal API.
		GetPipelineSecretValues: workerOnly,
	}

	// workerScopedRoutes are routes that a worker JWT must be explicitly
	// authorized for, instead of being waved through by the blanket
	// "is_from_worker implies admin" bypass in the auth middleware. A worker
	// reaching one of these must present a team-scoped token whose team
	// matches the team in the request path.
	//
	// Without this, any worker token — including an unscoped global one —
	// could read every team's secrets.
	workerScopedRoutes = map[RouteName]bool{
		GetPipelineSecretValues: true,
	}

	// workerDeniedRoutes are routes no worker may reach at all, whatever its
	// token is scoped to. They are the counterpart to workerScopedRoutes: that
	// map narrows the blanket bypass, this one removes it.
	//
	// A worker only ever needs the values resolved for the build it is
	// running, which is GetPipelineSecretValues and nothing else. Managing
	// entries is a human action, so a worker token — which lives on a build
	// agent, and in the global case is printed to the server log at startup
	// and never rotates — must not be able to read, write or delete them.
	// Scoping is not enough here: a team-scoped token would still be a
	// standing credential over that team's whole secret store.
	//
	// These cannot be expressed in routeAuthorization, because the middleware
	// never consults that table for a worker.
	workerDeniedRoutes = map[RouteName]bool{
		SetTeamSecret:        true,
		ListTeamSecrets:      true,
		DeleteTeamSecret:     true,
		SetPipelineSecret:    true,
		ListPipelineSecrets:  true,
		DeletePipelineSecret: true,
	}
)

// workerOnly rejects every non-worker caller. Workers never reach it: the auth
// middleware handles them via workerScopedRoutes before consulting this table.
func workerOnly(ctx context.Context, s pikoci.Service, un, tc string) error {
	return fmt.Errorf("this endpoint is only available to workers")
}

func nothing(ctx context.Context, s pikoci.Service, un, tc string) error { return nil }

func requireRole(r role.Role) authorizationFn {
	return func(ctx context.Context, s pikoci.Service, un, tc string) error {
		um, err := s.GetUser(ctx, un)
		if err != nil {
			return fmt.Errorf("failed to GetUser: %w", err)
		}

		// For routes without a team scope (e.g. ChangePassword, UpdateProfile),
		// any authenticated user is allowed
		if tc == "" {
			// Block team-scoped API tokens on non-team routes
			if ar, ok := ctx.Value(ApiTokenContextKey).(*apitoken.AuthResult); ok && !ar.Personal {
				return fmt.Errorf("team-scoped API tokens require a team-scoped route")
			}
			return nil
		}

		if !um.HasRole(r, tc) {
			return fmt.Errorf("requires %s role", r)
		}

		// If team-scoped API token, also check token scope and role cap
		if ar, ok := ctx.Value(ApiTokenContextKey).(*apitoken.AuthResult); ok && !ar.Personal {
			if ar.TeamCanonical != tc {
				return fmt.Errorf("API token not scoped to team %q", tc)
			}
			effectiveRole := minRole(roleOnTeam(um, tc), ar.TokenRole)
			if !effectiveRole.AtLeast(r) {
				return fmt.Errorf("API token effective role insufficient")
			}
		}

		return nil
	}
}

// requirePublicOrRole returns an authorization function that allows authenticated
// users with at least the given role, or marks the request for public pipeline
// fallback (handled in the auth middleware).
func requirePublicOrRole(r role.Role) authorizationFn {
	return func(ctx context.Context, s pikoci.Service, un, tc string) error {
		if un == "" {
			// Unauthenticated: let the middleware handle public fallback
			return fmt.Errorf("requires authentication or public pipeline")
		}
		um, err := s.GetUser(ctx, un)
		if err != nil {
			return fmt.Errorf("failed to GetUser: %w", err)
		}
		if !um.HasRole(r, tc) {
			return fmt.Errorf("requires %s role", r)
		}

		// If team-scoped API token, also check token scope and role cap
		if ar, ok := ctx.Value(ApiTokenContextKey).(*apitoken.AuthResult); ok && !ar.Personal {
			if ar.TeamCanonical != tc {
				return fmt.Errorf("API token not scoped to team %q", tc)
			}
			effectiveRole := minRole(roleOnTeam(um, tc), ar.TokenRole)
			if !effectiveRole.AtLeast(r) {
				return fmt.Errorf("API token effective role insufficient")
			}
		}

		return nil
	}
}

func globalAdmin(ctx context.Context, s pikoci.Service, un, tc string) error {
	// Reject team-scoped API tokens on global admin routes.
	// Personal tokens of global admins are intentionally allowed.
	if ar, ok := ctx.Value(ApiTokenContextKey).(*apitoken.AuthResult); ok && !ar.Personal {
		return fmt.Errorf("team-scoped API tokens cannot access global admin routes")
	}
	um, err := s.GetUser(ctx, un)
	if err != nil {
		return fmt.Errorf("failed to GetUser: %w", err)
	}
	if !um.Admin {
		return fmt.Errorf("requires global admin")
	}
	return nil
}

// jwtOnly rejects all API token auth, requiring a JWT session.
func jwtOnly(inner authorizationFn) authorizationFn {
	return func(ctx context.Context, s pikoci.Service, un, tc string) error {
		if _, ok := ctx.Value(ApiTokenContextKey).(*apitoken.AuthResult); ok {
			return fmt.Errorf("API tokens cannot manage API tokens; use JWT authentication")
		}
		return inner(ctx, s, un, tc)
	}
}

// roleOnTeam returns the user's role on the given team.
func roleOnTeam(um *user.WithMemberships, tc string) role.Role {
	for _, m := range um.Memberships {
		if m.TeamCanonical == tc {
			return m.Role
		}
	}
	return ""
}

// minRole returns the lesser of two roles by level.
func minRole(a, b role.Role) role.Role {
	if a.Level() <= b.Level() {
		return a
	}
	return b
}
