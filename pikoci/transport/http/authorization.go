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
		UserLogin:       nothing,
		RefreshToken:    nothing,
		WebhookTrigger:  nothing,
		WorkerHeartbeat: nothing,
		GetVersion:      nothing,

		// Public-level routes: unauthenticated access to public pipelines
		GetPipeline:          requirePublicOrRole(role.Viewer),
		GetPipelineImage:     requirePublicOrRole(role.Viewer),
		ListPipelineJobs:     requirePublicOrRole(role.Viewer),
		GetPipelineJob:       requirePublicOrRole(role.Viewer),
		ListJobBuilds:        requirePublicOrRole(role.Viewer),
		ListPipelineResources: requirePublicOrRole(role.Viewer),
		GetPipelineResource:   requirePublicOrRole(role.Viewer),
		ListResourceVersions:    requirePublicOrRole(role.Viewer),
		GetResourceVersionPath:  requirePublicOrRole(role.Viewer),

		// Viewer routes
		ListPipelines:    requireRole(role.Viewer),
		ListTeams:        requireRole(role.Viewer),
		GetTeam:          requireRole(role.Viewer),
		GetJobBuild:      requireRole(role.Viewer),
		ChangePassword:   requireRole(role.Viewer),
		UpdateProfile:    requireRole(role.Viewer),
		ListTriggersAfter: requireRole(role.Viewer),

		// Operator routes
		TriggerPipelineJob:     requireRole(role.Operator),
		CancelJobBuild:        requireRole(role.Operator),
		RetryJobBuild:         requireRole(role.Operator),
		PausePipeline:         requireRole(role.Operator),
		UnpausePipeline:       requireRole(role.Operator),
		PauseJob:              requireRole(role.Operator),
		UnpauseJob:            requireRole(role.Operator),
		PinResourceVersion:    requireRole(role.Operator),
		UnpinResourceVersion:  requireRole(role.Operator),
		TriggerResourceVersion: requireRole(role.Operator),
		TriggerPipelineResource: requireRole(role.Operator),

		// Maintainer routes
		CreatePipeline:         requireRole(role.Maintainer),
		UpdatePipeline:         requireRole(role.Maintainer),
		DeletePipeline:         requireRole(role.Maintainer),
		CreatePipelineImage:    requireRole(role.Maintainer),
		UpdatePipelineResource: requireRole(role.Maintainer),
		CreateResourceVersion:  requireRole(role.Maintainer),
		RegenerateWebhookToken: requireRole(role.Maintainer),
		CreateTrigger:          requireRole(role.Maintainer),

		// Worker-internal routes (bypassed by isFromWorker JWT)
		CreateJobBuild:                 requireRole(role.Maintainer),
		CreateRetryJobBuild:            requireRole(role.Maintainer),
		UpdateJobBuild:                 requireRole(role.Maintainer),
		DeleteJobBuild:                 requireRole(role.Maintainer),
		StartPendingBuild:              requireRole(role.Maintainer),
		FindOldestPendingBuild:         requireRole(role.Maintainer),
		NotifySerialGroupPendingBuilds: requireRole(role.Maintainer),
		EvaluateDownstreamJobs:         requireRole(role.Maintainer),
		InsertBuildGetVersion:          requireRole(role.Maintainer),
		FindBuildGetVersions:           requireRole(role.Maintainer),

		// Admin routes
		CreateTeamMember: requireRole(role.Admin),
		UpdateTeamMember: requireRole(role.Admin),
		DeleteTeamMember: requireRole(role.Admin),
		UpdateTeam: requireRole(role.Admin),
		DeleteTeam: requireRole(role.Admin),

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
		ExportDatabase: globalAdmin,

		// API token management routes (JWT only — tokens cannot manage tokens)
		CreateApiToken: jwtOnly(requireRole(role.Viewer)),
		ListApiTokens:  jwtOnly(requireRole(role.Viewer)),
		DeleteApiToken: jwtOnly(requireRole(role.Viewer)),
	}
)

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
