package http

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/job"
)

type ListPipelineJobsResponse struct {
	Jobs []job.WithStatus `json:"data,omitempty"`
	Err  string           `json:"error,omitempty"`
}

func (r ListPipelineJobsResponse) Error() string { return r.Err }

func listPipelineJobs(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ctx = r.Context()
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		var jobs []job.WithStatus
		var err error
		if isPublic, _ := ctx.Value(IsPublicAccessKey).(bool); isPublic {
			jobs, err = s.ListPublicPipelineJobs(ctx, tc, pc)
		} else {
			jobs, err = s.ListPipelineJobs(ctx, tc, pc)
		}
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListPipelineJobsResponse{Jobs: jobs, Err: errs}, w)
	}
}

type TriggerPipelineJobRequest struct {
	TeamCanonical     string            `json:"team_canonical"`
	PipelineCanonical string            `json:"pipeline_canonical"`
	JobName           string            `json:"job_name"`
	InputValues       map[string]string `json:"input_values,omitempty"`
}
type TriggerPipelineJobResponse struct {
	Err string `json:"error,omitempty"`
}

func (r TriggerPipelineJobResponse) Error() string { return r.Err }

func triggerPipelineJob(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req TriggerPipelineJobRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.JobName = vars["job_name"]
		// Decode optional JSON body for input values
		if r.Body != nil && r.ContentLength > 0 {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
		manual := len(req.InputValues) > 0 || r.ContentLength > 0
		err := s.TriggerPipelineJob(ctx, req.TeamCanonical, req.PipelineCanonical, req.JobName, req.InputValues, manual)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(TriggerPipelineJobResponse{Err: errs}, w)
	}
}

type GetPipelineJobRequest struct {
	TeamCanonical     string `json:"team_canonical"`
	PipelineCanonical string `json:"pipeline_canonical"`
	JobName           string `json:"job_name"`
}
type GetPipelineJobResponse struct {
	Job *job.Job `json:"data,omitempty"`
	Err string   `json:"error,omitempty"`
}

func (r GetPipelineJobResponse) Error() string { return r.Err }

func getPipelineJob(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req GetPipelineJobRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.JobName = vars["job_name"]
		var j *job.Job
		var err error
		if isPublic, _ := ctx.Value(IsPublicAccessKey).(bool); isPublic {
			j, err = s.GetPublicPipelineJob(ctx, req.TeamCanonical, req.PipelineCanonical, req.JobName)
		} else {
			j, err = s.GetPipelineJob(ctx, req.TeamCanonical, req.PipelineCanonical, req.JobName)
		}
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(GetPipelineJobResponse{Job: j, Err: errs}, w)
	}
}

type PauseJobResponse struct {
	Err string `json:"error,omitempty"`
}

func (r PauseJobResponse) Error() string { return r.Err }

func pauseJob(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		err := s.PauseJob(r.Context(), tc, pc, jn)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(PauseJobResponse{Err: errs}, w)
	}
}

type UnpauseJobResponse struct {
	Err string `json:"error,omitempty"`
}

func (r UnpauseJobResponse) Error() string { return r.Err }

func unpauseJob(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		jn := vars["job_name"]
		err := s.UnpauseJob(r.Context(), tc, pc, jn)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(UnpauseJobResponse{Err: errs}, w)
	}
}
