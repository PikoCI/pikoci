package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/apitoken"
	"github.com/pikoci/pikoci/pikoci/role"
)

type CreateApiTokenRequest struct {
	Name          string    `json:"name"`
	Personal      bool      `json:"personal"`
	TeamCanonical string    `json:"team_canonical,omitempty"`
	Role          role.Role `json:"role,omitempty"`
	ExpiresAt     string    `json:"expires_at,omitempty"`
}

type CreateApiTokenResponse struct {
	Token *apitoken.WithPlaintext `json:"data,omitempty"`
	Err   string                  `json:"error,omitempty"`
}

func (r CreateApiTokenResponse) Error() string { return r.Err }

func createApiToken(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req CreateApiTokenRequest
			ctx = r.Context()
		)
		un, ok := ctx.Value(UsernameContextKey).(string)
		if !ok {
			encodeResponse(CreateApiTokenResponse{Err: "missing username in context"}, w)
			return
		}
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(CreateApiTokenResponse{Err: err.Error()}, w)
			return
		}

		var expiresAt *time.Time
		if req.ExpiresAt != "" {
			t, err := time.Parse(time.RFC3339, req.ExpiresAt)
			if err != nil {
				encodeResponse(CreateApiTokenResponse{Err: "invalid expires_at format, use RFC3339"}, w)
				return
			}
			expiresAt = &t
		}

		token, err := s.CreateApiToken(ctx, un, req.Name, req.Personal, req.TeamCanonical, req.Role, expiresAt)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(CreateApiTokenResponse{Token: token, Err: errs}, w)
	}
}

type ListApiTokensResponse struct {
	Tokens []*apitoken.Token `json:"data"`
	Err    string            `json:"error,omitempty"`
}

func (r ListApiTokensResponse) Error() string { return r.Err }

func listApiTokens(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		un, ok := ctx.Value(UsernameContextKey).(string)
		if !ok {
			encodeResponse(ListApiTokensResponse{Err: "missing username in context"}, w)
			return
		}

		tokens, err := s.ListApiTokens(ctx, un)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		if tokens == nil {
			tokens = []*apitoken.Token{}
		}
		encodeResponse(ListApiTokensResponse{Tokens: tokens, Err: errs}, w)
	}
}

type DeleteApiTokenResponse struct {
	Err string `json:"error,omitempty"`
}

func (r DeleteApiTokenResponse) Error() string { return r.Err }

func deleteApiToken(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		un, ok := ctx.Value(UsernameContextKey).(string)
		if !ok {
			encodeResponse(DeleteApiTokenResponse{Err: "missing username in context"}, w)
			return
		}

		vars := mux.Vars(r)
		tokenIDStr := vars["token_id"]
		tokenID, err := strconv.ParseUint(tokenIDStr, 10, 32)
		if err != nil {
			encodeResponse(DeleteApiTokenResponse{Err: "invalid token_id"}, w)
			return
		}

		err = s.DeleteApiToken(ctx, un, uint32(tokenID))
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(DeleteApiTokenResponse{Err: errs}, w)
	}
}
