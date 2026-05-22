package http

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/xescugc/pikoci/pikoci"
	"github.com/xescugc/pikoci/pikoci/resource"
)

type CreateResourceVersionRequest struct {
	TeamCanonical     string           `json:"team_canonical"`
	PipelineCanonical string           `json:"pipeline_canonical"`
	ResourceCanonical string           `json:"resource_canonical"`
	Version           resource.Version `json:"version"`
}
type CreateResourceVersionResponse struct {
	Err     string            `json:"error,omitempty"`
	Version *resource.Version `json:"version,omitempty"`
}

func (r CreateResourceVersionResponse) Error() string { return r.Err }

func createResourceVersion(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req CreateResourceVersionRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.ResourceCanonical = vars["resource_canonical"]
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(CreateResourceVersionResponse{Err: err.Error()}, w)
			return
		}
		ver, err := s.CreateResourceVersion(ctx, req.TeamCanonical, req.PipelineCanonical, req.ResourceCanonical, req.Version)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(CreateResourceVersionResponse{Version: ver, Err: errs}, w)
	}
}

type ListResourceVersionsRequest struct {
	TeamCanonical     string `json:"team_canonical"`
	PipelineCanonical string `json:"pipeline_canonical"`
	ResourceCanonical string `json:"resource_canonical"`
}
type ListResourceVersionsResponse struct {
	Versions []*resource.Version `json:"data,omitempty"`
	Err      string              `json:"error,omitempty"`
}

func (r ListResourceVersionsResponse) Error() string { return r.Err }

func listResourceVersions(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req ListResourceVersionsRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.ResourceCanonical = vars["resource_canonical"]
		var vers []*resource.Version
		var err error
		if isPublic, _ := ctx.Value(IsPublicAccessKey).(bool); isPublic {
			vers, err = s.ListPublicResourceVersions(ctx, req.TeamCanonical, req.PipelineCanonical, req.ResourceCanonical)
		} else {
			vers, err = s.ListResourceVersions(ctx, req.TeamCanonical, req.PipelineCanonical, req.ResourceCanonical)
		}
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListResourceVersionsResponse{Versions: vers, Err: errs}, w)
	}
}

type GetPipelineResourceRequest struct {
	TeamCanonical     string `json:"team_canonical"`
	PipelineCanonical string `json:"pipeline_canonical"`
	ResourceCanonical string `json:"resource_canonical"`
}
type GetPipelineResourceResponse struct {
	Resource *resource.Resource `json:"data,omitempty"`
	Err      string             `json:"error,omitempty"`
}

func (r GetPipelineResourceResponse) Error() string { return r.Err }

func getPipelineResource(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req GetPipelineResourceRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.ResourceCanonical = vars["resource_canonical"]
		var res *resource.Resource
		var err error
		if isPublic, _ := ctx.Value(IsPublicAccessKey).(bool); isPublic {
			res, err = s.GetPublicPipelineResource(ctx, req.TeamCanonical, req.PipelineCanonical, req.ResourceCanonical)
		} else {
			res, err = s.GetPipelineResource(ctx, req.TeamCanonical, req.PipelineCanonical, req.ResourceCanonical)
			if res != nil {
				un, _ := ctx.Value(UsernameContextKey).(string)
				if un != "" {
					um, uerr := s.GetUser(ctx, un)
					if uerr != nil || !um.IsAdmin(req.TeamCanonical) {
						res.WebhookToken = ""
					}
				} else {
					res.WebhookToken = ""
				}
			}
		}
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(GetPipelineResourceResponse{Resource: res, Err: errs}, w)
	}
}

type UpdatePipelineResourceRequest struct {
	TeamCanonical     string            `json:"team_canonical"`
	PipelineCanonical string            `json:"pipeline_canonical"`
	ResourceCanonical string            `json:"resource_canonical"`
	Resource          resource.Resource `json:"resource"`
}
type UpdatePipelineResourceResponse struct {
	Err string `json:"error,omitempty"`
}

func (r UpdatePipelineResourceResponse) Error() string { return r.Err }

func updatePipelineResource(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req UpdatePipelineResourceRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.ResourceCanonical = vars["resource_canonical"]
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(UpdatePipelineResourceResponse{Err: err.Error()}, w)
			return
		}
		err = s.UpdatePipelineResource(ctx, req.TeamCanonical, req.PipelineCanonical, req.ResourceCanonical, req.Resource)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(UpdatePipelineResourceResponse{Err: errs}, w)
	}
}

type TriggerPipelineResourceRequest struct {
	TeamCanonical     string `json:"team_canonical"`
	PipelineCanonical string `json:"pipeline_canonical"`
	ResourceCanonical string `json:"resource_canonical"`
}
type TriggerPipelineResourceResponse struct {
	Err string `json:"error,omitempty"`
}

func (r TriggerPipelineResourceResponse) Error() string { return r.Err }

type WebhookTriggerResponse struct {
	Err string `json:"error,omitempty"`
}

func (r WebhookTriggerResponse) Error() string { return r.Err }

func webhookTrigger(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		token := vars["webhook_token"]
		err := s.WebhookTrigger(r.Context(), token)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(WebhookTriggerResponse{Err: errs}, w)
	}
}

type RegenerateWebhookTokenResponse struct {
	Token string `json:"token,omitempty"`
	Err   string `json:"error,omitempty"`
}

func (r RegenerateWebhookTokenResponse) Error() string { return r.Err }

func regenerateWebhookToken(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		rCan := vars["resource_canonical"]
		token, err := s.RegenerateWebhookToken(r.Context(), tc, pc, rCan)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(RegenerateWebhookTokenResponse{Token: token, Err: errs}, w)
	}
}

func triggerPipelineResource(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req TriggerPipelineResourceRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.ResourceCanonical = vars["resource_canonical"]
		err := s.TriggerPipelineResource(ctx, req.TeamCanonical, req.PipelineCanonical, req.ResourceCanonical)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(TriggerPipelineResourceResponse{Err: errs}, w)
	}
}
