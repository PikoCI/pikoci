package http

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/job"
)

type TriggerPipelineJobRequest struct {
	TeamCanonical string `json:"team_canonical"`
	PipelineName  string `json:"pipeline_name"`
	JobName       string `json:"job_name"`
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
		req.PipelineName = vars["pipeline_name"]
		req.JobName = vars["job_name"]
		err := s.TriggerPipelineJob(ctx, req.TeamCanonical, req.PipelineName, req.JobName)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(TriggerPipelineJobResponse{Err: errs}, w)
	}
}

type GetPipelineJobRequest struct {
	TeamCanonical string `json:"team_canonical"`
	PipelineName  string `json:"pipeline_name"`
	JobName       string `json:"job_name"`
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
		req.PipelineName = vars["pipeline_name"]
		req.JobName = vars["job_name"]
		var j *job.Job
		var err error
		if isPublic, _ := ctx.Value(IsPublicAccessKey).(bool); isPublic {
			j, err = s.GetPublicPipelineJob(ctx, req.TeamCanonical, req.PipelineName, req.JobName)
		} else {
			j, err = s.GetPipelineJob(ctx, req.TeamCanonical, req.PipelineName, req.JobName)
		}
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(GetPipelineJobResponse{Job: j, Err: errs}, w)
	}
}
