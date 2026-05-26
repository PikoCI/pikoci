package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/xescugc/pikoci/pikoci"
	"github.com/xescugc/pikoci/pikoci/transport/http/assets"
	"github.com/xescugc/pikoci/pikoci/transport/http/templates"
	"github.com/xescugc/pikoci/pikoci/user"
)

type contextKey string

const (
	UsernameContextKey contextKey = "username_context_key"
	IsPublicAccessKey  contextKey = "is_public_access_key"
)

var publicFallbackRoutes = map[RouteName]bool{
	GetPipeline:          true,
	GetPipelineImage:     true,
	GetPipelineJob:       true,
	ListJobBuilds:        true,
	GetPipelineResource:  true,
	ListResourceVersions: true,
}

func Handler(s pikoci.Service, ts []byte, l *slog.Logger, db *sql.DB, dbSystem string) http.Handler {
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
								isFromWorker, _ = claims["is_from_worker"].(bool)
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

				// Check if JWT claims are stale compared to DB
				if un != "" {
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

	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/trigger").Name(TriggerPipelineJob.String()).Handler(triggerPipelineJob(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}").Name(GetPipelineJob.String()).Handler(getPipelineJob(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds").Name(ListJobBuilds.String()).Handler(listJobBuilds(s))
	api.Methods(http.MethodPut).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_number}").Name(UpdateJobBuild.String()).Handler(updateJobBuild(s))
	api.Methods(http.MethodDelete).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_number}").Name(DeleteJobBuild.String()).Handler(deleteJobBuild(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_number}").Name(GetJobBuild.String()).Handler(getJobBuild(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_number}/cancel").Name(CancelJobBuild.String()).Handler(cancelJobBuild(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_number}/retry").Name(RetryJobBuild.String()).Handler(retryJobBuild(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/start-pending").Name(StartPendingBuild.String()).Handler(startPendingBuild(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/oldest-pending").Name(FindOldestPendingBuild.String()).Handler(findOldestPendingBuild(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/retry-builds").Name(CreateRetryJobBuild.String()).Handler(createRetryJobBuild(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds-get-versions/{build_id}").Name(FindBuildGetVersions.String()).Handler(findBuildGetVersions(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs/{job_name}/builds/{build_id}/get-versions").Name(InsertBuildGetVersion.String()).Handler(insertBuildGetVersion(s))

	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}/versions").Name(CreateResourceVersion.String()).Handler(createResourceVersion(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}/versions").Name(ListResourceVersions.String()).Handler(listResourceVersions(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}").Name(GetPipelineResource.String()).Handler(getPipelineResource(s))
	api.Methods(http.MethodPut).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}").Name(UpdatePipelineResource.String()).Handler(updatePipelineResource(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}/trigger").Name(TriggerPipelineResource.String()).Handler(triggerPipelineResource(s))
	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/resources/{resource_canonical}/webhook_token").Name(RegenerateWebhookToken.String()).Handler(regenerateWebhookToken(s))

	api.Methods(http.MethodPost).Path("/teams/{team_canonical}/triggers/{trigger_name}").Name(CreateTrigger.String()).Handler(createTrigger(s))
	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/triggers/{trigger_name}").Name(ListTriggersAfter.String()).Handler(listTriggersAfter(s))

	api.Methods(http.MethodGet).Path("/teams/{team_canonical}/pipelines/{pipeline_canonical}/image{ext}").Name(GetPipelineImage.String()).Handler(getPipelineImage(s))

	api.NotFoundHandler = http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error": "Path not found"}`)
		},
	)

	// Binary API routes (authenticated, no JSON content-type requirement)
	binApi := r.PathPrefix("/").Subrouter()
	binApi.Use(auth)
	binApi.Methods(http.MethodGet).Path("/admin/export").Name(ExportDatabase.String()).Handler(exportDatabase(db, dbSystem))

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

	// Wrap the router: strip .json suffix and set Content-Type before
	// mux route matching, so the jsonr subrouter matches correctly.
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, ".json") {
			req.URL.Path = strings.TrimSuffix(req.URL.Path, ".json")
			req.Header.Set("Content-Type", "application/json")
		}
		r.ServeHTTP(w, req)
	})
}

func encodeError(errs string, w http.ResponseWriter) {
	encodeResponse(ErrorResponse{Err: errs}, w)
}

type ErrorResponse struct {
	Err string `json:"error"`
}

func (r ErrorResponse) Error() string {
	return r.Err
}

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
		key := m.TeamCanonical
		if m.Admin {
			key += ":admin"
		}
		dbSet[key] = true
	}
	for _, jm := range jwtMemberships {
		m, ok := jm.(map[string]interface{})
		if !ok {
			return true
		}
		tc, _ := m["team_canonical"].(string)
		a, _ := m["admin"].(bool)
		key := tc
		if a {
			key += ":admin"
		}
		if !dbSet[key] {
			return true
		}
	}
	return false
}

func encodeResponse(r interface{}, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
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
