package http

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
)

type GenerateTeamWorkerTokenResponse struct {
	Token string `json:"token,omitempty"`
	Err   string `json:"error,omitempty"`
}

func (r GenerateTeamWorkerTokenResponse) Error() string { return r.Err }

func generateTeamWorkerToken(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc := vars["team_canonical"]

		token, err := s.GenerateTeamWorkerToken(r.Context(), tc)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(GenerateTeamWorkerTokenResponse{Token: token, Err: errs}, w)
	}
}

type GetTeamWorkerTokenResponse struct {
	Token string `json:"token,omitempty"`
	Err   string `json:"error,omitempty"`
}

func (r GetTeamWorkerTokenResponse) Error() string { return r.Err }

func getTeamWorkerToken(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tc := vars["team_canonical"]

		token, err := s.GetTeamWorkerToken(r.Context(), tc)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(GetTeamWorkerTokenResponse{Token: token, Err: errs}, w)
	}
}
