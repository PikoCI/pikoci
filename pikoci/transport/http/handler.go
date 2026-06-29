// Package http provides the HTTP transport layer for the PikoCI server.
// It defines the main HTTP handler, route registration, authentication
// and authorization middleware, and JSON request/response encoding.
package http

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/transport/http/assets"
	"github.com/pikoci/pikoci/pikoci/transport/http/templates"
	"github.com/pikoci/pikoci/pikoci/user"
)

// contextKey is a custom type for context value keys to avoid collisions.
type contextKey string

const (
	// UsernameContextKey is the context key used to store the authenticated username.
	UsernameContextKey contextKey = "username_context_key"
	// IsPublicAccessKey is the context key used to indicate that the request
	// is accessing a public pipeline without authentication.
	IsPublicAccessKey contextKey = "is_public_access_key"
	// ApiTokenContextKey is the context key used to store the API token auth result.
	ApiTokenContextKey contextKey = "api_token_context_key"
)

// publicFallbackRoutes lists routes that can fall back to public pipeline access
// when authentication/authorization fails.
var publicFallbackRoutes = map[RouteName]bool{
	GetPipeline:            true,
	GetPipelineImage:       true,
	ListPipelineJobs:       true,
	GetPipelineJob:         true,
	ListJobBuilds:          true,
	ListPipelineResources:  true,
	GetPipelineResource:    true,
	ListResourceVersions:   true,
	GetResourceVersionPath: true,
}

// Handler creates and returns the main HTTP handler for the PikoCI API.
// It configures all API routes, authentication middleware, static asset serving,
// and HTML template rendering. The ts parameter is the JWT signing secret,
// and dbSystem identifies the database backend for the export endpoint.
func Handler(s pikoci.Service, ts []byte, l *slog.Logger, db *sql.DB, dbSystem, version, commit string) http.Handler {
	r := mux.NewRouter()

	auth := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, rr *http.Request) {
			// Determine route name early for public fallback check
			cr := mux.CurrentRoute(rr)
			var crn RouteName
			var hasRouteName bool
			if cr != nil {
				crns := cr.GetName()
				if crns != "" {
					var err error
					crn, err = RouteNameString(crns)
					if err == nil {
						hasRouteName = true
					}
				}
			}

			// Authentication
			reqToken := rr.Header.Get("Authorization")
			splitToken := strings.Split(reqToken, " ")
			authFailed := len(splitToken) != 2 || reqToken == ""

			var (
				un           string
				isFromWorker bool
				userClaim    map[string]interface{}
			)

			if !authFailed {
				tokenString := splitToken[1]
				if strings.HasPrefix(tokenString, "pko_") {
					// API token auth
					h := sha256.Sum256([]byte(tokenString))
					hash := hex.EncodeToString(h[:])
					authResult, err := s.FindApiTokenByHash(rr.Context(), hash)
					if err != nil {
						l.Error("API token authentication error", "error", err)
						authFailed = true
					} else if authResult.ExpiresAt != nil && authResult.ExpiresAt.Before(time.Now()) {
						l.Error("API token expired")
						authFailed = true
					} else {
						un = authResult.Username
						rr = rr.WithContext(context.WithValue(rr.Context(), UsernameContextKey, un))
						rr = rr.WithContext(context.WithValue(rr.Context(), pikoci.ActorContextKey, un))
						rr = rr.WithContext(context.WithValue(rr.Context(), ApiTokenContextKey, authResult))
						go s.UpdateApiTokenLastUsed(context.Background(), authResult.TokenID)
					}
				} else {
					// JWT auth
					token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
						return ts, nil
					}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
					if err != nil {
						l.Error("authentication error", "error", err)
						authFailed = true
					} else {
						claims, ok := token.Claims.(jwt.MapClaims)
						if !ok {
							l.Error("invalid token claims")
							authFailed = true
						} else {
							userClaim, ok = claims["user"].(map[string]interface{})
							if !ok {
								isFromWorker, _ = claims["is_from_worker"].(bool)
								if !isFromWorker {
									l.Error("missing user claim in token")
									authFailed = true
								}
							} else {
								un, ok = userClaim["username"].(string)
								if !ok {
									l.Error("missing username in token")
									authFailed = true
								} else {
									rr = rr.WithContext(context.WithValue(rr.Context(), UsernameContextKey, un))
									rr = rr.WithContext(context.WithValue(rr.Context(), pikoci.ActorContextKey, un))
									isFromWorker, _ = claims["is_from_worker"].(bool)
								}
							}
						}
					}
				}
			}

			// If authentication failed, check for public pipeline fallback
			if authFailed {
				if hasRouteName && publicFallbackRoutes[crn] {
					vars := mux.Vars(rr)
					tc := vars["team_canonical"]
					pc := vars["pipeline_canonical"]
					if tc != "" && pc != "" {
						_, err := s.GetPublicPipeline(rr.Context(), tc, pc)
						if err == nil {
							rr = rr.WithContext(context.WithValue(rr.Context(), IsPublicAccessKey, true))
							h.ServeHTTP(rw, rr)
							return
						}
					}
				}
				encodeError("Authentication required", rw)
				return
			}

			// Authorization
			if cr == nil {
				encodeError("Route not found", rw)
				return
			}
			if !hasRouteName {
				crns := cr.GetName()
				if crns == "" {
					pt, _ := cr.GetPathTemplate()
					encodeError(fmt.Sprintf("Route %s has no name", pt), rw)
					return
				}
				pt, _ := cr.GetPathTemplate()
				encodeError(fmt.Sprintf("Route %s has no name conversion(%s)", pt, crns), rw)
				return
			}

			afn, ok := routeAuthorization[crn]
			if !ok {
				pt, _ := cr.GetPathTemplate()
				encodeError(fmt.Sprintf("Route %s has no auth", pt), rw)
				return
			}

			// If the JWT has the 'is_from_worker' we assume admin
			// so we do not even have to Authorize anything
			if !isFromWorker {
				vars := mux.Vars(rr)
				tc := vars["team_canonical"]
				err := afn(rr.Context(), s, un, tc)
				if err != nil {
					// If authorization fails but route supports public fallback, try it
					if publicFallbackRoutes[crn] {
						pc := vars["pipeline_canonical"]
						if tc != "" && pc != "" {
							_, perr := s.GetPublicPipeline(rr.Context(), tc, pc)
							if perr == nil {
								rr = rr.WithContext(context.WithValue(rr.Context(), IsPublicAccessKey, true))
								h.ServeHTTP(rw, rr)
								return
							}
						}
					}
					l.Error("authorization error", "error", err)
					encodeError("Authentication required", rw)
					return
				}

				// Check if JWT claims are stale compared to DB (skip for API tokens)
				if un != "" && userClaim != nil {
					um, err := s.GetUser(rr.Context(), un)
					if err == nil && membershipsDiffer(userClaim, um) {
						rw.Header().Set("X-Refresh-Token", "true")
					}
				}
			}

			h.ServeHTTP(rw, rr)
		})
	}

	r.Methods(http.MethodPost).Path("/webhooks/{webhook_token}").Name(WebhookTrigger.String()).Handler(webhookTrigger(s))

	jsonr := r.Headers("Content-Type", "application/json").Subrouter()

	jsonr.Methods(http.MethodPost).Path("/login").Handler(userLogin(s))
	jsonr.Methods(http.MethodGet).Path("/version").Name(GetVersion.String()).HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]string{
			"version": version,
			"commit":  commit,
		})
	})

	api := jsonr.PathPrefix("/").Subrouter()

	api.Use(auth)

	api.Methods(http.MethodPost).Path("/refresh-token").Name(RefreshToken.String()).Handler(refreshToken(s))

	api.Methods(http.MethodGet).Path("/users").Name(ListUsers.String()).Handler(listUsers(s))
	api.Methods(http.MethodPost).Path("/users").Name(CreateUser.String()).Handler(createUser(s))
	api.Methods(http.MethodPost).Path("/users/change-password").Name(ChangePassword.String()).Handler(changePassword(s))
	api.Methods(http.MethodGet).Path("/users/{username}").Name(GetUser.String()).Handler(getUser(s))
	api.Methods(http.MethodPut).Path("/users/{username}").Name(UpdateUser.String()).Handler(updateUser(s))
	api.Methods(http.MethodDelete).Path("/users/{username}").Name(DeleteUser.String()).Handler(deleteUser(s))
	api.Methods(http.MethodPut).Path("/profile").Name(UpdateProfile.String()).Handler(updateProfile(s))

	api.Methods(http.MethodPost).Path("/api-tokens").Name(CreateApiToken.String()).Handler(createApiToken(s))
	api.Methods(http.MethodGet).Path("/api-tokens").Name(ListApiTokens.String()).Handler(listApiTokens(s))
	api.Methods(http.MethodDelete).Path("/api-tokens/{token_id}").Name(DeleteApiToken.String()).Handler(deleteApiToken(s))

	api.Methods(http.MethodPost).Path("/teams").Name(CreateTeam.String()).Handler(createTeam(s))

	api.Methods(http.MethodGet).Path("/teams").Name(ListTeams.String()).Handler(listTeams(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}").Name(GetTeam.String()).Handler(getTeam(s))
	api.Methods(http.MethodPut).Path("/teams/{team_canonical}").Name(UpdateTeam.String()).Handler(updateTeam(s))
	api.Methods(http.MethodDelete).Path("/teams/{team_canonical}").Name(DeleteTeam.String()).Handler(deleteTeam(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/members").Name(CreateTeamMember.String()).Handler(createTeamMember(s))
	api.Methods(http.MethodPut).Path("/teams/{team_canonical}/members/{member_username}").Name(UpdateTeamMember.String()).Handler(updateTeamMember(s))
	api.Methods(http.MethodDelete).Path("/teams/{team_canonical}/members/{member_username}").Name(DeleteTeamMember.String()).Handler(deleteTeamMember(s))

	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines").Name(CreatePipeline.String()).Handler(createPipeline(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines").Name(ListPipelines.String()).Handler(listPipelines(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/image{ext}").Name(CreatePipelineImage.String()).Handler(createPipelineImage(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}").Name(GetPipeline.String()).Handler(getPipeline(s))
	api.Methods(http.MethodPut).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}").Name(UpdatePipeline.String()).Handler(updatePipeline(s))
	api.Methods(http.MethodDelete).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}").Name(DeletePipeline.String()).Handler(deletePipeline(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/pause").Name(PausePipeline.String()).Handler(pausePipeline(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/unpause").Name(UnpausePipeline.String()).Handler(unpausePipeline(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/pause").Name(PauseJob.String()).Handler(pauseJob(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/unpause").Name(UnpauseJob.String()).Handler(unpauseJob(s))

	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs").Name(ListPipelineJobs.String()).Handler(listPipelineJobs(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/trigger").Name(TriggerPipelineJob.String()).Handler(triggerPipelineJob(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}").Name(GetPipelineJob.String()).Handler(getPipelineJob(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds").Name(CreateJobBuild.String()).Handler(createJobBuild(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds").Name(ListJobBuilds.String()).Handler(listJobBuilds(s))
	api.Methods(http.MethodPut).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_number}").Name(UpdateJobBuild.String()).Handler(updateJobBuild(s))
	api.Methods(http.MethodDelete).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_number}").Name(DeleteJobBuild.String()).Handler(deleteJobBuild(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_number}").Name(GetJobBuild.String()).Handler(getJobBuild(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_number}/cancel").Name(CancelJobBuild.String()).Handler(cancelJobBuild(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_number}/retry").Name(RetryJobBuild.String()).Handler(retryJobBuild(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_number}/approve").Name(ApproveBuild.String()).Handler(approveBuild(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_number}/reject").Name(RejectBuild.String()).Handler(rejectBuild(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/start-pending").Name(StartPendingBuild.String()).Handler(startPendingBuild(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/oldest-pending").Name(FindOldestPendingBuild.String()).Handler(findOldestPendingBuild(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/notify-serial-groups").Name(NotifySerialGroupPendingBuilds.String()).Handler(notifySerialGroupPendingBuilds(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/evaluate-downstream").Name(EvaluateDownstreamJobs.String()).Handler(evaluateDownstreamJobs(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/retry-builds").Name(CreateRetryJobBuild.String()).Handler(createRetryJobBuild(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds-get-versions/{build_id}").Name(FindBuildGetVersions.String()).Handler(findBuildGetVersions(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_id}/get-versions").Name(InsertBuildGetVersion.String()).Handler(insertBuildGetVersion(s))

	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources").Name(ListPipelineResources.String()).Handler(listPipelineResources(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}/versions").Name(CreateResourceVersion.String()).Handler(createResourceVersion(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}/versions").Name(ListResourceVersions.String()).Handler(listResourceVersions(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}").Name(GetPipelineResource.String()).Handler(getPipelineResource(s))
	api.Methods(http.MethodPut).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}").Name(UpdatePipelineResource.String()).Handler(updatePipelineResource(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}/trigger").Name(TriggerPipelineResource.String()).Handler(triggerPipelineResource(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}/pin").Name(PinResourceVersion.String()).Handler(pinResourceVersion(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}/unpin").Name(UnpinResourceVersion.String()).Handler(unpinResourceVersion(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}/versions/{version_id}/trigger").Name(TriggerResourceVersion.String()).Handler(triggerResourceVersion(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}/versions/{version_id}/path").Name(GetResourceVersionPath.String()).Handler(getResourceVersionPath(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}/webhook_token").Name(RegenerateWebhookToken.String()).Handler(regenerateWebhookToken(s))

	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/audit").Name(ListAuditLog.String()).Handler(listAuditLog(s))

	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/triggers/{trigger_name}").Name(CreateTrigger.String()).Handler(createTrigger(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/triggers/{trigger_name}").Name(ListTriggersAfter.String()).Handler(listTriggersAfter(s))

	api.Methods(http.MethodPost).Path("/workers/heartbeat").Name(WorkerHeartbeat.String()).Handler(workerHeartbeat(s))

	api.Methods(http.MethodGet).Path("/workers").Name(ListWorkers.String()).Handler(listWorkers(s))
	api.Methods(http.MethodGet).Path("/workers/health").Name(WorkersHealth.String()).Handler(workersHealth(s))
	api.Methods(http.MethodDelete).Path("/workers/{worker_name}").Name(DeleteWorker.String()).Handler(deleteWorker(s))

	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/image{ext}").Name(GetPipelineImage.String()).Handler(getPipelineImage(s))

	api.NotFoundHandler = http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error": "Path not found"}`)
		},
	)

	// Binary API routes (authenticated, no JSON content-type requirement)
	binApi := r.PathPrefix("/").Subrouter()
	binApi.Use(auth)
	binApi.Methods(http.MethodGet).Path("/admin/export").Name(ExportDatabase.String()).Handler(exportDatabase(db, dbSystem))
	binApi.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/image{ext}").Name(GetPipelineImage.String()).Handler(getPipelineImage(s))

	r.PathPrefix("/css/").Handler(http.FileServer(http.FS(assets.Assets)))
	r.PathPrefix("/js/").Handler(http.FileServer(http.FS(assets.Assets)))
	r.PathPrefix("/images/").Handler(http.FileServer(http.FS(assets.Assets)))
	r.PathPrefix("/fonts/").Handler(http.FileServer(http.FS(assets.Assets)))

	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, ok := templates.Templates["views/layouts/index.tmpl"]
		if !ok {
			http.Error(w, "template not found", http.StatusInternalServerError)
			return
		}
		if err := t.Execute(w, nil); err != nil {
			l.Error("failed to execute template", "error", err)
		}
	})

	return r
}

// encodeError writes an error response as JSON to the response writer.
func encodeError(errs string, w http.ResponseWriter) {
	encodeResponse(ErrorResponse{Err: errs}, w)
}

// ErrorResponse represents a JSON error response returned by the API.
type ErrorResponse struct {
	Err string `json:"error"`
}

// Error returns the error message string, satisfying the Errorer interface.
func (r ErrorResponse) Error() string {
	return r.Err
}

// Errorer is an interface for response types that carry an error message.
// A non-empty Error() return value causes the response to be written with
// an HTTP 400 status code.
type Errorer interface {
	Error() string
}

func membershipsDiffer(jwtUser map[string]interface{}, dbUser *user.WithMemberships) bool {
	jwtAdmin, _ := jwtUser["admin"].(bool)
	if jwtAdmin != dbUser.Admin {
		return true
	}

	jwtMemberships, _ := jwtUser["memberships"].([]interface{})
	if len(jwtMemberships) != len(dbUser.Memberships) {
		return true
	}

	dbSet := make(map[string]bool, len(dbUser.Memberships))
	for _, m := range dbUser.Memberships {
		key := m.TeamCanonical + ":" + string(m.Role)
		dbSet[key] = true
	}
	for _, jm := range jwtMemberships {
		m, ok := jm.(map[string]interface{})
		if !ok {
			return true
		}
		tc, _ := m["team_canonical"].(string)
		// Support both new "role" field and old "admin" field in JWT
		var key string
		if r, ok := m["role"].(string); ok {
			key = tc + ":" + r
		} else {
			// Fallback for old JWT tokens with admin bool
			a, _ := m["admin"].(bool)
			if a {
				key = tc + ":" + string(role.Admin)
			} else {
				key = tc + ":" + string(role.Maintain)
			}
		}
		if !dbSet[key] {
			return true
		}
	}
	return false
}

func encodeResponse(r interface{}, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	e, ok := r.(Errorer)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		r = ErrorResponse{Err: fmt.Sprintf("the response %T is not 'Errorer'", r)}
	} else if e.Error() != "" {
		w.WriteHeader(http.StatusBadRequest)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(r)
}
