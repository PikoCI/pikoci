package http

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/secret"
)

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
			encodeResponse(SetConfigResponse{Err: "invalid request body"}, w)
			return
		}

		var errs string
		if err := s.SetTeamConfig(r.Context(), tc, req.Name, req.Value, req.Kind); err != nil {
			errs = err.Error()
		}
		encodeResponse(SetConfigResponse{Err: errs}, w)
	}
}

func setPipelineConfig(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn := vars["team_canonical"], vars["pipeline_canonical"]

		req, ok := decodeSetConfig(r)
		if !ok {
			encodeResponse(SetConfigResponse{Err: "invalid request body"}, w)
			return
		}

		var errs string
		if err := s.SetPipelineConfig(r.Context(), tc, pn, req.Name, req.Value, req.Kind); err != nil {
			errs = err.Error()
		}
		encodeResponse(SetConfigResponse{Err: errs}, w)
	}
}

func listTeamConfig(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tc := mux.Vars(r)["team_canonical"]

		entries, err := s.ListTeamConfig(r.Context(), tc)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListConfigResponse{Data: entries, Err: errs}, w)
	}
}

func listPipelineConfig(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn := vars["team_canonical"], vars["pipeline_canonical"]

		entries, err := s.ListPipelineConfig(r.Context(), tc, pn)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListConfigResponse{Data: entries, Err: errs}, w)
	}
}

func deleteTeamConfig(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, name := vars["team_canonical"], vars["config_name"]

		var errs string
		if err := s.DeleteTeamConfig(r.Context(), tc, name); err != nil {
			errs = err.Error()
		}
		encodeResponse(DeleteConfigResponse{Err: errs}, w)
	}
}

func deletePipelineConfig(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn, name := vars["team_canonical"], vars["pipeline_canonical"], vars["config_name"]

		var errs string
		if err := s.DeletePipelineConfig(r.Context(), tc, pn, name); err != nil {
			errs = err.Error()
		}
		encodeResponse(DeleteConfigResponse{Err: errs}, w)
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
			encodeResponse(ConfigValuesResponse{Err: err.Error()}, w)
			return
		}
		encodeResponse(ConfigValuesResponse{Data: resolved.Values, Plain: resolved.Plain}, w)
	}
}
