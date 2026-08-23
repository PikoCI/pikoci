package http

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/secret"
)

// SetSecretRequest is the body for storing a secret. The value is write-only:
// no endpoint ever returns it again.
type SetSecretRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type SetSecretResponse struct {
	Err string `json:"error,omitempty"`
}

func (r SetSecretResponse) Error() string { return r.Err }

type ListSecretsResponse struct {
	Data []*secret.Secret `json:"data"`
	Err  string           `json:"error,omitempty"`
}

func (r ListSecretsResponse) Error() string { return r.Err }

type DeleteSecretResponse struct {
	Err string `json:"error,omitempty"`
}

func (r DeleteSecretResponse) Error() string { return r.Err }

// SecretValuesResponse carries decrypted values to a worker. It is the only
// response in the API that contains secret plaintext, and its route is
// reachable only by a team-scoped worker token.
type SecretValuesResponse struct {
	Data map[string]string `json:"data"`
	Err  string            `json:"error,omitempty"`
}

func (r SecretValuesResponse) Error() string { return r.Err }

func setTeamSecret(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tc := mux.Vars(r)["team_canonical"]

		var req SetSecretRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			encodeResponse(SetSecretResponse{Err: "invalid request body"}, w)
			return
		}

		var errs string
		if err := s.SetTeamSecret(r.Context(), tc, req.Name, req.Value); err != nil {
			errs = err.Error()
		}
		encodeResponse(SetSecretResponse{Err: errs}, w)
	}
}

func setPipelineSecret(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn := vars["team_canonical"], vars["pipeline_canonical"]

		var req SetSecretRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			encodeResponse(SetSecretResponse{Err: "invalid request body"}, w)
			return
		}

		var errs string
		if err := s.SetPipelineSecret(r.Context(), tc, pn, req.Name, req.Value); err != nil {
			errs = err.Error()
		}
		encodeResponse(SetSecretResponse{Err: errs}, w)
	}
}

func listTeamSecrets(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tc := mux.Vars(r)["team_canonical"]

		secrets, err := s.ListTeamSecrets(r.Context(), tc)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListSecretsResponse{Data: secrets, Err: errs}, w)
	}
}

func listPipelineSecrets(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn := vars["team_canonical"], vars["pipeline_canonical"]

		secrets, err := s.ListPipelineSecrets(r.Context(), tc, pn)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListSecretsResponse{Data: secrets, Err: errs}, w)
	}
}

func deleteTeamSecret(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, sn := vars["team_canonical"], vars["secret_name"]

		var errs string
		if err := s.DeleteTeamSecret(r.Context(), tc, sn); err != nil {
			errs = err.Error()
		}
		encodeResponse(DeleteSecretResponse{Err: errs}, w)
	}
}

func deletePipelineSecret(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn, sn := vars["team_canonical"], vars["pipeline_canonical"], vars["secret_name"]

		var errs string
		if err := s.DeletePipelineSecret(r.Context(), tc, pn, sn); err != nil {
			errs = err.Error()
		}
		encodeResponse(DeleteSecretResponse{Err: errs}, w)
	}
}

// getPipelineSecretValues serves decrypted values to a worker running a build
// for this pipeline. Access control for it lives in the auth middleware, which
// requires a team-scoped worker token matching this team.
func getPipelineSecretValues(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc, pn := vars["team_canonical"], vars["pipeline_canonical"]

		values, err := s.ResolvePipelineSecrets(r.Context(), tc, pn)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(SecretValuesResponse{Data: values, Err: errs}, w)
	}
}
