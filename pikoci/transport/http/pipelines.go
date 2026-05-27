package http

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/pipeline"
)

type CreatePipelineRequest struct {
	TeamCanonical string                 `json:"team_canonical"`
	Name          string                 `json:"name"`
	Config        []byte                 `json:"config"`
	Vars          map[string]interface{} `json:"vars"`
}
type CreatePipelineResponse struct {
	Pipeline *pipeline.Pipeline `json:"data,omitempty"`
	Err      string             `json:"error,omitempty"`
}

func (r CreatePipelineResponse) Error() string { return r.Err }

func createPipeline(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req CreatePipelineRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(CreatePipelineResponse{Err: err.Error()}, w)
			return
		}
		pp, err := s.CreatePipeline(ctx, req.TeamCanonical, req.Name, req.Config, req.Vars)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(CreatePipelineResponse{Pipeline: pp, Err: errs}, w)
	}
}

type UpdatePipelineRequest struct {
	TeamCanonical string                 `json:"team_canonical"`
	Name          string                 `json:"name,omitempty"`
	Config        []byte                 `json:"config"`
	Vars          map[string]interface{} `json:"vars"`
	Public        *bool                  `json:"public,omitempty"`
}
type UpdatePipelineResponse struct {
	Pipeline *pipeline.Pipeline `json:"data,omitempty"`
	Err      string             `json:"error,omitempty"`
}

func (r UpdatePipelineResponse) Error() string { return r.Err }

func updatePipeline(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req UpdatePipelineRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pCan := vars["pipeline_canonical"]
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(UpdatePipelineResponse{Err: err.Error()}, w)
			return
		}
		req.TeamCanonical = tc
		var pp *pipeline.Pipeline
		var errs string
		newCan := pCan
		if len(req.Config) > 0 {
			pp, err = s.UpdatePipeline(ctx, req.TeamCanonical, pCan, req.Config, req.Vars, req.Name)
			if err != nil {
				errs = err.Error()
			} else if pp != nil {
				newCan = pp.Canonical
			}
		} else if req.Name != "" {
			// Name-only update (no config change): re-send existing config
			existing, gerr := s.GetPipeline(ctx, req.TeamCanonical, pCan)
			if gerr != nil {
				encodeResponse(UpdatePipelineResponse{Err: gerr.Error()}, w)
				return
			}
			pp, err = s.UpdatePipeline(ctx, req.TeamCanonical, pCan, existing.Raw, nil, req.Name)
			if err != nil {
				errs = err.Error()
			} else if pp != nil {
				newCan = pp.Canonical
			}
		}
		if err == nil && req.Public != nil {
			err = s.SetPipelinePublic(ctx, req.TeamCanonical, newCan, *req.Public)
			if err != nil {
				errs = err.Error()
			}
		}
		if errs == "" && pp == nil {
			pp, err = s.GetPipeline(ctx, req.TeamCanonical, newCan)
			if err != nil {
				errs = err.Error()
			}
		}
		encodeResponse(UpdatePipelineResponse{Pipeline: pp, Err: errs}, w)
	}
}

type ListPipelinesRequest struct {
	TeamCanonical string `json:"team_canonical"`
}
type ListPipelinesResponse struct {
	Pipelines []*pipeline.Pipeline `json:"data,omitempty"`
	Err       string               `json:"error,omitempty"`
}

func (r ListPipelinesResponse) Error() string { return r.Err }

func listPipelines(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req ListPipelinesRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		pps, err := s.ListPipelines(ctx, req.TeamCanonical)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListPipelinesResponse{Pipelines: pps, Err: errs}, w)
	}
}

type GetPipelineRequest struct {
	TeamCanonical string `json:"team_canonical"`
	Name          string `json:"name"`
}
type GetPipelineResponse struct {
	Pipeline *pipeline.Pipeline `json:"data,omitempty"`
	Err      string             `json:"error,omitempty"`
}

func (r GetPipelineResponse) Error() string { return r.Err }

func getPipeline(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req GetPipelineRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.Name = vars["pipeline_canonical"]
		req.TeamCanonical = vars["team_canonical"]
		var pp *pipeline.Pipeline
		var err error
		if isPublic, _ := ctx.Value(IsPublicAccessKey).(bool); isPublic {
			pp, err = s.GetPublicPipeline(ctx, req.TeamCanonical, req.Name)
		} else {
			pp, err = s.GetPipeline(ctx, req.TeamCanonical, req.Name)
		}
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(GetPipelineResponse{Pipeline: pp, Err: errs}, w)
	}
}

type DeletePipelineRequest struct {
	TeamCanonical string `json:"team_canonical"`
	Name          string `json:"name"`
}
type DeletePipelineResponse struct {
	Err string `json:"error,omitempty"`
}

func (r DeletePipelineResponse) Error() string { return r.Err }

func deletePipeline(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req DeletePipelineRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.Name = vars["pipeline_canonical"]
		req.TeamCanonical = vars["team_canonical"]
		err := s.DeletePipeline(ctx, req.TeamCanonical, req.Name)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(DeletePipelineResponse{Err: errs}, w)
	}
}

type GetPipelineImageRequest struct {
	TeamCanonical string `json:"team_canonical"`
	Name          string `json:"name"`
	Format        string `json:"format"`
}
type GetPipelineImageResponse struct {
	Image string `json:"image,omitempty"`
	Err   string `json:"error,omitempty"`
}

func (r GetPipelineImageResponse) Error() string { return r.Err }

func getPipelineImage(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req GetPipelineImageRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.Name = vars["pipeline_canonical"]
		req.Format = vars["format"]
		var img []byte
		var err error
		if isPublic, _ := ctx.Value(IsPublicAccessKey).(bool); isPublic {
			img, err = s.GetPublicPipelineImage(ctx, req.TeamCanonical, req.Name, req.Format)
		} else {
			img, err = s.GetPipelineImage(ctx, req.TeamCanonical, req.Name, req.Format)
		}
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(GetPipelineImageResponse{Image: string(img), Err: errs}, w)
	}
}

type CreatePipelineImageRequest struct {
	TeamCanonical string                 `json:"team_canonical"`
	Config        []byte                 `json:"config"`
	Vars          map[string]interface{} `json:"vars"`
	Format        string                 `json:"format"`
}
type CreatePipelineImageResponse struct {
	Image string `json:"image,omitempty"`
	Err   string `json:"error,omitempty"`
}

func (r CreatePipelineImageResponse) Error() string { return r.Err }

func createPipelineImage(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req CreatePipelineImageRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.Format = vars["format"]
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(CreatePipelineImageResponse{Err: err.Error()}, w)
			return
		}
		img, err := s.CreatePipelineImage(ctx, req.TeamCanonical, req.Config, req.Vars, req.Format)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(CreatePipelineImageResponse{Image: string(img), Err: errs}, w)
	}
}
