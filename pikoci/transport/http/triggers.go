package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/xescugc/pikoci/pikoci"
	"github.com/xescugc/pikoci/pikoci/trigger"
)

type CreateTriggerRequest struct {
	TeamCanonical string                 `json:"team_canonical"`
	Name          string                 `json:"name"`
	Version       map[string]interface{} `json:"version"`
}
type CreateTriggerResponse struct {
	Trigger *trigger.Trigger `json:"data,omitempty"`
	Err     string           `json:"error,omitempty"`
}

func (r CreateTriggerResponse) Error() string { return r.Err }

func createTrigger(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req CreateTriggerRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.Name = vars["trigger_name"]
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(CreateTriggerResponse{Err: err.Error()}, w)
			return
		}
		t, err := s.CreateTrigger(ctx, req.TeamCanonical, req.Name, req.Version)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(CreateTriggerResponse{Trigger: t, Err: errs}, w)
	}
}

type ListTriggersAfterRequest struct {
	TeamCanonical string `json:"team_canonical"`
	Name          string `json:"name"`
	AfterID       uint32 `json:"after_id"`
}
type ListTriggersAfterResponse struct {
	Triggers []*trigger.Trigger `json:"data,omitempty"`
	Err      string             `json:"error,omitempty"`
}

func (r ListTriggersAfterResponse) Error() string { return r.Err }

func listTriggersAfter(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req ListTriggersAfterRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.Name = vars["trigger_name"]
		if after := r.URL.Query().Get("after"); after != "" {
			v, _ := strconv.ParseUint(after, 10, 32)
			req.AfterID = uint32(v)
		}
		triggers, err := s.ListTriggersAfter(ctx, req.TeamCanonical, req.Name, req.AfterID)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListTriggersAfterResponse{Triggers: triggers, Err: errs}, w)
	}
}
