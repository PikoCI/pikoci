package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/build"
)

type CreateJobBuildRequest struct {
	TeamCanonical     string      `json:"team_canonical"`
	PipelineCanonical string      `json:"pipeline_canonical"`
	JobName           string      `json:"job_name"`
	Build             build.Build `json:"build"`
}
type CreateJobBuildResponse struct {
	Build *build.Build `json:"build,omitempty"`
	Err   string       `json:"error,omitempty"`
}

func (r CreateJobBuildResponse) Error() string { return r.Err }

func createJobBuild(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req CreateJobBuildRequest
			ctx = r.Context()
		)
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(CreateJobBuildResponse{Err: err.Error()}, w)
			return
		}
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		b, err := s.CreateJobBuild(ctx, tc, pc, jn, req.Build)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(CreateJobBuildResponse{Build: b, Err: errs}, w)
	}
}

type StartPendingBuildRequest struct {
	BuildID uint32 `json:"build_id"`
}
type StartPendingBuildResponse struct {
	Build *build.Build `json:"build,omitempty"`
	Err   string       `json:"error,omitempty"`
}

func (r StartPendingBuildResponse) Error() string { return r.Err }

func startPendingBuild(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req StartPendingBuildRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(StartPendingBuildResponse{Err: err.Error()}, w)
			return
		}
		b, err := s.StartPendingBuild(ctx, tc, pc, jn, req.BuildID)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(StartPendingBuildResponse{Build: b, Err: errs}, w)
	}
}

type FindOldestPendingBuildResponse struct {
	Build *build.Build `json:"data,omitempty"`
	Err   string       `json:"error,omitempty"`
}

func (r FindOldestPendingBuildResponse) Error() string { return r.Err }

func findOldestPendingBuild(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ctx = r.Context()
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		b, err := s.FindOldestPendingBuild(ctx, tc, pc, jn)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(FindOldestPendingBuildResponse{Build: b, Err: errs}, w)
	}
}

func notifySerialGroupPendingBuilds(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ctx = r.Context()
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		s.NotifySerialGroupPendingBuilds(ctx, tc, pc, jn)
		w.WriteHeader(http.StatusNoContent)
	}
}

func evaluateDownstreamJobs(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ctx = r.Context()
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		if err := s.EvaluateDownstreamJobs(ctx, tc, pc, jn); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type UpdateJobBuildRequest struct {
	TeamCanonical     string      `json:"team_canonical"`
	PipelineCanonical string      `json:"pipeline_canonical"`
	JobName           string      `json:"job_name"`
	BuildNumber       string      `json:"build_number"`
	Build             build.Build `json:"build"`
}
type UpdateJobBuildResponse struct {
	Err string `json:"error,omitempty"`
}

func (r UpdateJobBuildResponse) Error() string { return r.Err }

func updateJobBuild(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req UpdateJobBuildRequest
			ctx = r.Context()
		)
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(UpdateJobBuildResponse{Err: err.Error()}, w)
			return
		}
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.JobName = vars["job_name"]
		req.BuildNumber = vars["build_number"]
		err = s.UpdateJobBuild(ctx, req.TeamCanonical, req.PipelineCanonical, req.JobName, req.BuildNumber, req.Build)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(UpdateJobBuildResponse{Err: errs}, w)
	}
}

type DeleteJobBuildRequest struct {
	TeamCanonical     string `json:"team_canonical"`
	PipelineCanonical string `json:"pipeline_canonical"`
	JobName           string `json:"job_name"`
	BuildNumber       string `json:"build_number"`
}
type DeleteJobBuildResponse struct {
	Err string `json:"error,omitempty"`
}

func (r DeleteJobBuildResponse) Error() string { return r.Err }

func deleteJobBuild(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req DeleteJobBuildRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.JobName = vars["job_name"]
		req.BuildNumber = vars["build_number"]
		err := s.DeleteJobBuild(ctx, req.TeamCanonical, req.PipelineCanonical, req.JobName, req.BuildNumber)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(DeleteJobBuildResponse{Err: errs}, w)
	}
}

type GetJobBuildResponse struct {
	Build *build.Build `json:"data,omitempty"`
	Err   string       `json:"error,omitempty"`
}

func (r GetJobBuildResponse) Error() string { return r.Err }

func getJobBuild(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ctx = r.Context()
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		bn := vars["build_number"]
		b, err := s.GetJobBuild(ctx, tc, pc, jn, bn)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(GetJobBuildResponse{Build: b, Err: errs}, w)
	}
}

type GetBuildReportResponse struct {
	Report *build.BuildReport `json:"data,omitempty"`
	Err    string             `json:"error,omitempty"`
}

func (r GetBuildReportResponse) Error() string { return r.Err }

func getBuildReport(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ctx = r.Context()
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		bn := vars["build_number"]
		report, err := s.GetBuildReport(ctx, tc, pc, jn, bn)
		var errs string
		if err != nil {
			errs = err.Error()
		} else {
			w.Header().Set("Content-Disposition", "attachment; filename=build-report-"+bn+".json")
		}
		encodeResponse(GetBuildReportResponse{Report: report, Err: errs}, w)
	}
}

type CancelJobBuildResponse struct {
	Err string `json:"error,omitempty"`
}

func (r CancelJobBuildResponse) Error() string { return r.Err }

func cancelJobBuild(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ctx = r.Context()
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		bn := vars["build_number"]
		err := s.CancelJobBuild(ctx, tc, pc, jn, bn)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(CancelJobBuildResponse{Err: errs}, w)
	}
}

type InsertBuildGetVersionRequest struct {
	TeamCanonical     string `json:"team_canonical"`
	PipelineCanonical string `json:"pipeline_canonical"`
	JobName           string `json:"job_name"`
	BuildID           uint32 `json:"build_id"`
	StepName          string `json:"step_name"`
	VersionID         uint32 `json:"version_id"`
}
type InsertBuildGetVersionResponse struct {
	Err string `json:"error,omitempty"`
}

func (r InsertBuildGetVersionResponse) Error() string { return r.Err }

func insertBuildGetVersion(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req InsertBuildGetVersionRequest
			ctx = r.Context()
		)
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(InsertBuildGetVersionResponse{Err: err.Error()}, w)
			return
		}
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.JobName = vars["job_name"]
		bid, _ := strconv.Atoi(vars["build_id"])
		req.BuildID = uint32(bid)
		// Note: build_id here is the internal DB ID, passed in the URL for this internal endpoint

		err = s.InsertBuildGetVersion(ctx, req.TeamCanonical, req.PipelineCanonical, req.JobName, req.BuildID, req.StepName, req.VersionID)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(InsertBuildGetVersionResponse{Err: errs}, w)
	}
}

type RetryJobBuildResponse struct {
	Err string `json:"error,omitempty"`
}

func (r RetryJobBuildResponse) Error() string { return r.Err }

func retryJobBuild(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ctx = r.Context()
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		bn := vars["build_number"]
		err := s.RetryJobBuild(ctx, tc, pc, jn, bn)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(RetryJobBuildResponse{Err: errs}, w)
	}
}

type CreateRetryJobBuildRequest struct {
	TeamCanonical     string      `json:"team_canonical"`
	PipelineCanonical string      `json:"pipeline_canonical"`
	JobName           string      `json:"job_name"`
	ParentBuildNumber string      `json:"parent_build_number"`
	Build             build.Build `json:"build"`
}
type CreateRetryJobBuildResponse struct {
	Build *build.Build `json:"build,omitempty"`
	Err   string       `json:"error,omitempty"`
}

func (r CreateRetryJobBuildResponse) Error() string { return r.Err }

func createRetryJobBuild(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req CreateRetryJobBuildRequest
			ctx = r.Context()
		)
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(CreateRetryJobBuildResponse{Err: err.Error()}, w)
			return
		}
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.JobName = vars["job_name"]
		b, err := s.CreateRetryJobBuild(ctx, req.TeamCanonical, req.PipelineCanonical, req.JobName, req.ParentBuildNumber, req.Build)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(CreateRetryJobBuildResponse{Build: b, Err: errs}, w)
	}
}

type FindBuildGetVersionsResponse struct {
	Versions map[string]uint32 `json:"data,omitempty"`
	Err      string            `json:"error,omitempty"`
}

func (r FindBuildGetVersionsResponse) Error() string { return r.Err }

func findBuildGetVersions(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ctx = r.Context()
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		bid, _ := strconv.Atoi(vars["build_id"])
		versions, err := s.FindBuildGetVersions(ctx, tc, pc, jn, uint32(bid))
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(FindBuildGetVersionsResponse{Versions: versions, Err: errs}, w)
	}
}

type ListJobBuildsRequest struct {
	TeamCanonical     string `json:"team_canonical"`
	PipelineCanonical string `json:"pipeline_canonical"`
	JobName           string `json:"job_name"`
}

type PageMeta struct {
	HasMore  bool   `json:"has_more"`
	OldestID uint32 `json:"oldest_id"`
	NewestID uint32 `json:"newest_id"`
}

type ListJobBuildsResponse struct {
	Builds []*build.Build `json:"data,omitempty"`
	Meta   *PageMeta      `json:"meta,omitempty"`
	Err    string         `json:"error,omitempty"`
}

func (r ListJobBuildsResponse) Error() string { return r.Err }

func parsePaginationParams(r *http.Request) (before *uint32, after *uint32, limit uint32) {
	limit = 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			limit = uint32(n)
		}
	}
	if v := r.URL.Query().Get("before"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			val := uint32(n)
			before = &val
		}
	}
	if v := r.URL.Query().Get("after"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			val := uint32(n)
			after = &val
		}
	}
	// before and after are mutually exclusive; before takes precedence
	if before != nil && after != nil {
		after = nil
	}
	return
}

// ApproveBuildRequest is the request body for the approve endpoint.
type ApproveBuildRequest struct {
	Message string `json:"message"`
}

// ApproveBuildResponse is the response body for the approve endpoint.
type ApproveBuildResponse struct {
	Err string `json:"error,omitempty"`
}

func (r ApproveBuildResponse) Error() string { return r.Err }

func approveBuild(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req ApproveBuildRequest
			ctx = r.Context()
		)
		_ = json.NewDecoder(r.Body).Decode(&req)
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		bn := vars["build_number"]
		un, _ := ctx.Value(UsernameContextKey).(string)
		err := s.ApproveBuild(ctx, tc, pc, jn, bn, un, req.Message)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ApproveBuildResponse{Err: errs}, w)
	}
}

// RejectBuildRequest is the request body for the reject endpoint.
type RejectBuildRequest struct {
	Message string `json:"message"`
}

// RejectBuildResponse is the response body for the reject endpoint.
type RejectBuildResponse struct {
	Err string `json:"error,omitempty"`
}

func (r RejectBuildResponse) Error() string { return r.Err }

func rejectBuild(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req RejectBuildRequest
			ctx = r.Context()
		)
		_ = json.NewDecoder(r.Body).Decode(&req)
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		bn := vars["build_number"]
		un, _ := ctx.Value(UsernameContextKey).(string)
		err := s.RejectBuild(ctx, tc, pc, jn, bn, un, req.Message)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(RejectBuildResponse{Err: errs}, w)
	}
}

// MarkBuildAsWarningResponse is the response body for the mark-as-warning endpoint.
type MarkBuildAsWarningResponse struct {
	Err string `json:"error,omitempty"`
}

func (r MarkBuildAsWarningResponse) Error() string { return r.Err }

func markBuildAsWarning(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		bn := vars["build_number"]
		err := s.MarkBuildAsWarning(ctx, tc, pc, jn, bn)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(MarkBuildAsWarningResponse{Err: errs}, w)
	}
}

func listJobBuilds(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req ListJobBuildsRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.JobName = vars["job_name"]

		before, after, limit := parsePaginationParams(r)

		var statuses []build.Status
		for _, sv := range r.URL.Query()["status"] {
			st, sErr := build.StatusString(sv)
			if sErr == nil {
				statuses = append(statuses, st)
			}
		}

		var builds []*build.Build
		var hasMore bool
		var err error
		if isPublic, _ := ctx.Value(IsPublicAccessKey).(bool); isPublic {
			builds, hasMore, err = s.ListPublicJobBuilds(ctx, req.TeamCanonical, req.PipelineCanonical, req.JobName, before, after, limit, statuses)
		} else {
			builds, hasMore, err = s.ListJobBuilds(ctx, req.TeamCanonical, req.PipelineCanonical, req.JobName, before, after, limit, statuses)
		}
		var errs string
		if err != nil {
			errs = err.Error()
		}
		var meta *PageMeta
		if len(builds) > 0 {
			meta = &PageMeta{
				HasMore:  hasMore,
				OldestID: builds[len(builds)-1].ID,
				NewestID: builds[0].ID,
			}
		}
		encodeResponse(ListJobBuildsResponse{Builds: builds, Meta: meta, Err: errs}, w)
	}
}
