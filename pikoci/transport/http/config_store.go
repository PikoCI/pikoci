package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/secret"
)

// configErrorStatus maps a store failure to the status that describes it.
//
// Anything unrecognised is a server-side fault: storage errors and cipher
// failures are not the caller's doing, so they must not be reported as a bad
// request. Permission failures never reach here, being settled by the auth
// middleware before the handler runs.
func configErrorStatus(err error) int {
	switch {
	case errors.Is(err, pikoci.ErrConfigEntryNotFound):
		return http.StatusNotFound
	case errors.Is(err, pikoci.ErrConfigInvalidRequest):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// SetConfigRequest is the body for storing a configuration entry.
//
// Kind defaults to "secret" when omitted, so a caller that does not know about
// plain entries cannot accidentally store a credential in the clear.
type SetConfigRequest struct {
	Name  string      `json:"name"`
	Value string      `json:"value"`
	Kind  secret.Kind `json:"kind,omitempty"`
}

type SetConfigResponse struct {
	Err string `json:"error,omitempty"`
}

func (r SetConfigResponse) Error() string { return r.Err }

// ListConfigResponse carries entry metadata. Plain entries include their
// value; secret entries never do.
type ListConfigResponse struct {
	Data []*secret.Entry `json:"data"`
	Err  string          `json:"error,omitempty"`
}

func (r ListConfigResponse) Error() string { return r.Err }

type DeleteConfigResponse struct {
	Err string `json:"error,omitempty"`
}

func (r DeleteConfigResponse) Error() string { return r.Err }

// ConfigValuesResponse carries resolved values to a worker. It is the only
// response in the API that contains secret plaintext, and its route is
// reachable only by a team-scoped worker token.
type ConfigValuesResponse struct {
	Data  map[string]string `json:"data"`
	Plain map[string]bool   `json:"plain,omitempty"`
	Err   string            `json:"error,omitempty"`
}

func (r ConfigValuesResponse) Error() string { return r.Err }

// decodeSetConfig reads the body and applies the secret-by-default rule.
func decodeSetConfig(r *http.Request) (SetConfigRequest, bool) {
	var req SetConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, false
	}
	if req.Kind == "" {
		req.Kind = secret.KindSecret
	}
	return req, true
}

func setTeamConfig(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tc := mux.Vars(r)["team_canonical"]

		req, ok := decodeSetConfig(r)
		if !ok {
			encodeErrorStatus("invalid request body", http.StatusBadRequest, w)
			return
		}

		if err := s.SetTeamConfig(r.Context(), tc, req.Name, req.Value, req.Kind); err != nil {
			encodeErrorStatus(err.Error(), configErrorStatus(err), w)
			return
		}
		encodeResponse(SetConfigResponse{}, w)
	}
}

func setPipelineConfig(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn := vars["team_canonical"], vars["pipeline_canonical"]

		req, ok := decodeSetConfig(r)
		if !ok {
			encodeErrorStatus("invalid request body", http.StatusBadRequest, w)
			return
		}

		if err := s.SetPipelineConfig(r.Context(), tc, pn, req.Name, req.Value, req.Kind); err != nil {
			encodeErrorStatus(err.Error(), configErrorStatus(err), w)
			return
		}
		encodeResponse(SetConfigResponse{}, w)
	}
}

func listTeamConfig(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tc := mux.Vars(r)["team_canonical"]

		entries, err := s.ListTeamConfig(r.Context(), tc)
		if err != nil {
			encodeErrorStatus(err.Error(), configErrorStatus(err), w)
			return
		}
		encodeResponse(ListConfigResponse{Data: entries}, w)
	}
}

func listPipelineConfig(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn := vars["team_canonical"], vars["pipeline_canonical"]

		entries, err := s.ListPipelineConfig(r.Context(), tc, pn)
		if err != nil {
			encodeErrorStatus(err.Error(), configErrorStatus(err), w)
			return
		}
		encodeResponse(ListConfigResponse{Data: entries}, w)
	}
}

func deleteTeamConfig(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, name := vars["team_canonical"], vars["config_name"]

		if err := s.DeleteTeamConfig(r.Context(), tc, name); err != nil {
			encodeErrorStatus(err.Error(), configErrorStatus(err), w)
			return
		}
		encodeResponse(DeleteConfigResponse{}, w)
	}
}

func deletePipelineConfig(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn, name := vars["team_canonical"], vars["pipeline_canonical"], vars["config_name"]

		if err := s.DeletePipelineConfig(r.Context(), tc, pn, name); err != nil {
			encodeErrorStatus(err.Error(), configErrorStatus(err), w)
			return
		}
		encodeResponse(DeleteConfigResponse{}, w)
	}
}

// getPipelineConfigValues serves resolved values to a worker running a build
// for this pipeline. Access control lives in the auth middleware, which
// requires a team-scoped worker token matching this team.
func getPipelineConfigValues(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn := vars["team_canonical"], vars["pipeline_canonical"]

		resolved, err := s.ResolvePipelineValues(r.Context(), tc, pn)
		if err != nil {
			encodeErrorStatus(err.Error(), configErrorStatus(err), w)
			return
		}
		encodeResponse(ConfigValuesResponse{Data: resolved.Values, Plain: resolved.Plain}, w)
	}
}
