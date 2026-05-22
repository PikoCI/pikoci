package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/xescugc/pikoci/pikoci"
	"github.com/xescugc/pikoci/pikoci/build"
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
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.JobName = vars["job_name"]
		req.BuildNumber = vars["build_number"]
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(UpdateJobBuildResponse{Err: err.Error()}, w)
			return
		}
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
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.JobName = vars["job_name"]
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(CreateRetryJobBuildResponse{Err: err.Error()}, w)
			return
		}
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
type ListJobBuildsResponse struct {
	Builds []*build.Build `json:"data,omitempty"`
	Err    string         `json:"error,omitempty"`
}

func (r ListJobBuildsResponse) Error() string { return r.Err }

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
		var builds []*build.Build
		var err error
		if isPublic, _ := ctx.Value(IsPublicAccessKey).(bool); isPublic {
			builds, err = s.ListPublicJobBuilds(ctx, req.TeamCanonical, req.PipelineCanonical, req.JobName)
		} else {
			builds, err = s.ListJobBuilds(ctx, req.TeamCanonical, req.PipelineCanonical, req.JobName)
		}
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListJobBuildsResponse{Builds: builds, Err: errs}, w)
	}
}
