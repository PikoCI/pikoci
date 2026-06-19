package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/resource"
)

type ListPipelineResourcesResponse struct {
	Resources []*resource.Resource `json:"data,omitempty"`
	Err       string               `json:"error,omitempty"`
}

func (r ListPipelineResourcesResponse) Error() string { return r.Err }

func listPipelineResources(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ctx = r.Context()
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		var resources []*resource.Resource
		var err error
		if isPublic, _ := ctx.Value(IsPublicAccessKey).(bool); isPublic {
			resources, err = s.ListPublicPipelineResources(ctx, tc, pc)
		} else {
			resources, err = s.ListPipelineResources(ctx, tc, pc)
			if resources != nil {
				un, _ := ctx.Value(UsernameContextKey).(string)
				isAdmin := false
				if un != "" {
					um, uerr := s.GetUser(ctx, un)
					if uerr == nil && um.IsAdmin(tc) {
						isAdmin = true
					}
				}
				if !isAdmin {
					for _, res := range resources {
						res.WebhookToken = ""
					}
				}
			}
		}
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListPipelineResourcesResponse{Resources: resources, Err: errs}, w)
	}
}

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
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(CreateResourceVersionResponse{Err: err.Error()}, w)
			return
		}
		// URL vars take precedence over JSON body fields
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.ResourceCanonical = vars["resource_canonical"]
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
	Meta     *PageMeta           `json:"meta,omitempty"`
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

		before, after, limit := parsePaginationParams(r)

		var vers []*resource.Version
		var hasMore bool
		var err error
		if isPublic, _ := ctx.Value(IsPublicAccessKey).(bool); isPublic {
			vers, hasMore, err = s.ListPublicResourceVersions(ctx, req.TeamCanonical, req.PipelineCanonical, req.ResourceCanonical, before, after, limit)
		} else {
			vers, hasMore, err = s.ListResourceVersions(ctx, req.TeamCanonical, req.PipelineCanonical, req.ResourceCanonical, before, after, limit)
		}
		var errs string
		if err != nil {
			errs = err.Error()
		}
		var meta *PageMeta
		if len(vers) > 0 {
			meta = &PageMeta{
				HasMore:  hasMore,
				OldestID: vers[len(vers)-1].ID,
				NewestID: vers[0].ID,
			}
		}
		encodeResponse(ListResourceVersionsResponse{Versions: vers, Meta: meta, Err: errs}, w)
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
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(UpdatePipelineResourceResponse{Err: err.Error()}, w)
			return
		}
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.PipelineCanonical = vars["pipeline_canonical"]
		req.ResourceCanonical = vars["resource_canonical"]
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

type PinResourceVersionRequest struct {
	VersionID uint32 `json:"version_id"`
}
type PinResourceVersionResponse struct {
	Err string `json:"error,omitempty"`
}

func (r PinResourceVersionResponse) Error() string { return r.Err }

func pinResourceVersion(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		rCan := vars["resource_canonical"]
		var req PinResourceVersionRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(PinResourceVersionResponse{Err: err.Error()}, w)
			return
		}
		err = s.PinResourceVersion(r.Context(), tc, pc, rCan, req.VersionID)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(PinResourceVersionResponse{Err: errs}, w)
	}
}

type UnpinResourceVersionResponse struct {
	Err string `json:"error,omitempty"`
}

func (r UnpinResourceVersionResponse) Error() string { return r.Err }

func unpinResourceVersion(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		rCan := vars["resource_canonical"]
		err := s.UnpinResourceVersion(r.Context(), tc, pc, rCan)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(UnpinResourceVersionResponse{Err: errs}, w)
	}
}

type TriggerResourceVersionResponse struct {
	Err string `json:"error,omitempty"`
}

func (r TriggerResourceVersionResponse) Error() string { return r.Err }

func triggerResourceVersion(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		rCan := vars["resource_canonical"]
		versionIDStr := vars["version_id"]
		versionID, err := strconv.ParseUint(versionIDStr, 10, 32)
		if err != nil {
			encodeResponse(TriggerResourceVersionResponse{Err: "invalid version_id"}, w)
			return
		}
		err = s.TriggerResourceVersion(r.Context(), tc, pc, rCan, uint32(versionID))
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(TriggerResourceVersionResponse{Err: errs}, w)
	}
}

type GetResourceVersionPathResponse struct {
	Data *resource.VersionPathResponse `json:"data,omitempty"`
	Err  string                        `json:"error,omitempty"`
}

func (r GetResourceVersionPathResponse) Error() string { return r.Err }

func getResourceVersionPath(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc := vars["team_canonical"]
		pc := vars["pipeline_canonical"]
		rCan := vars["resource_canonical"]
		versionIDStr := vars["version_id"]
		versionID, err := strconv.ParseUint(versionIDStr, 10, 32)
		if err != nil {
			encodeResponse(GetResourceVersionPathResponse{Err: "invalid version_id"}, w)
			return
		}
		ctx := r.Context()
		var resp *resource.VersionPathResponse
		if isPublic, _ := ctx.Value(IsPublicAccessKey).(bool); isPublic {
			resp, err = s.GetPublicResourceVersionPath(ctx, tc, pc, rCan, uint32(versionID))
		} else {
			resp, err = s.GetResourceVersionPath(ctx, tc, pc, rCan, uint32(versionID))
		}
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(GetResourceVersionPathResponse{Data: resp, Err: errs}, w)
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
