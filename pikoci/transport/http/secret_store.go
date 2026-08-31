package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/secret"
)

// secretErrorStatus maps a store failure to the status that describes it.
//
// Anything unrecognised is a server-side fault: storage errors and cipher
// failures are not the caller's doing, so they must not be reported as a bad
// request. Permission failures never reach here, being settled by the auth
// middleware before the handler runs.
func secretErrorStatus(err error) int {
	switch {
	case errors.Is(err, pikoci.ErrSecretEntryNotFound):
		return http.StatusNotFound
	case errors.Is(err, pikoci.ErrSecretInvalidRequest):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// SetSecretRequest is the body for storing a secret store entry.
//
// Kind defaults to "secret" when omitted, so a caller that does not know about
// plain entries cannot accidentally store a credential in the clear.
type SetSecretRequest struct {
	Name  string      `json:"name"`
	Value string      `json:"value"`
	Kind  secret.Kind `json:"kind,omitempty"`
}

type SetSecretResponse struct {
	Err string `json:"error,omitempty"`
}

func (r SetSecretResponse) Error() string { return r.Err }

// ListSecretsResponse carries entry metadata. Plain entries include their
// value; secret entries never do.
type ListSecretsResponse struct {
	Data []*secret.Entry `json:"data"`
	Err  string          `json:"error,omitempty"`
}

func (r ListSecretsResponse) Error() string { return r.Err }

type DeleteSecretResponse struct {
	Err string `json:"error,omitempty"`
}

func (r DeleteSecretResponse) Error() string { return r.Err }

// SecretValuesResponse carries resolved values to a worker. It is the only
// response in the API that contains secret plaintext, and its route is
// reachable only by a team-scoped worker token.
type SecretValuesResponse struct {
	Data  map[string]string `json:"data"`
	Plain map[string]bool   `json:"plain,omitempty"`
	Err   string            `json:"error,omitempty"`
}

func (r SecretValuesResponse) Error() string { return r.Err }

// decodeSetSecret reads the body and applies the secret-by-default rule.
func decodeSetSecret(r *http.Request) (SetSecretRequest, bool) {
	var req SetSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, false
	}
	if req.Kind == "" {
		req.Kind = secret.KindSecret
	}
	return req, true
}

func setTeamSecret(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tc := mux.Vars(r)["team_canonical"]

		req, ok := decodeSetSecret(r)
		if !ok {
			encodeErrorStatus("invalid request body", http.StatusBadRequest, w)
			return
		}

		if err := s.SetTeamSecret(r.Context(), tc, req.Name, req.Value, req.Kind); err != nil {
			encodeErrorStatus(err.Error(), secretErrorStatus(err), w)
			return
		}
		encodeResponse(SetSecretResponse{}, w)
	}
}

func setPipelineSecret(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn := vars["team_canonical"], vars["pipeline_canonical"]

		req, ok := decodeSetSecret(r)
		if !ok {
			encodeErrorStatus("invalid request body", http.StatusBadRequest, w)
			return
		}

		if err := s.SetPipelineSecret(r.Context(), tc, pn, req.Name, req.Value, req.Kind); err != nil {
			encodeErrorStatus(err.Error(), secretErrorStatus(err), w)
			return
		}
		encodeResponse(SetSecretResponse{}, w)
	}
}

func listTeamSecrets(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tc := mux.Vars(r)["team_canonical"]

		entries, err := s.ListTeamSecrets(r.Context(), tc)
		if err != nil {
			encodeErrorStatus(err.Error(), secretErrorStatus(err), w)
			return
		}
		encodeResponse(ListSecretsResponse{Data: entries}, w)
	}
}

func listPipelineSecrets(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn := vars["team_canonical"], vars["pipeline_canonical"]

		entries, err := s.ListPipelineSecrets(r.Context(), tc, pn)
		if err != nil {
			encodeErrorStatus(err.Error(), secretErrorStatus(err), w)
			return
		}
		encodeResponse(ListSecretsResponse{Data: entries}, w)
	}
}

func deleteTeamSecret(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, name := vars["team_canonical"], vars["secret_name"]

		if err := s.DeleteTeamSecret(r.Context(), tc, name); err != nil {
			encodeErrorStatus(err.Error(), secretErrorStatus(err), w)
			return
		}
		encodeResponse(DeleteSecretResponse{}, w)
	}
}

func deletePipelineSecret(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn, name := vars["team_canonical"], vars["pipeline_canonical"], vars["secret_name"]

		if err := s.DeletePipelineSecret(r.Context(), tc, pn, name); err != nil {
			encodeErrorStatus(err.Error(), secretErrorStatus(err), w)
			return
		}
		encodeResponse(DeleteSecretResponse{}, w)
	}
}

// getPipelineSecretValues serves resolved values to a worker running a build
// for this pipeline. Access control lives in the auth middleware, which
// requires a team-scoped worker token matching this team.
func getPipelineSecretValues(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn := vars["team_canonical"], vars["pipeline_canonical"]

		resolved, err := s.ResolvePipelineValues(r.Context(), tc, pn)
		if err != nil {
			encodeErrorStatus(err.Error(), secretErrorStatus(err), w)
			return
		}
		encodeResponse(SecretValuesResponse{Data: resolved.Values, Plain: resolved.Plain}, w)
	}
}
