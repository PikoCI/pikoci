package http

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/user"
)

type CreateTeamRequest struct {
	Name     string `json:"name"`
	Username string `json:"username"`
}
type CreateTeamResponse struct {
	Team *team.WithMembers `json:"data,omitempty"`
	Err  string            `json:"error,omitempty"`
}

func (r CreateTeamResponse) Error() string { return r.Err }

func createTeam(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req CreateTeamRequest
			ctx = r.Context()
		)
		un, ok := ctx.Value(UsernameContextKey).(string)
		if !ok {
			encodeResponse(CreateTeamResponse{Err: "missing username in context"}, w)
			return
		}
		req.Username = un
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(CreateTeamResponse{Err: err.Error()}, w)
			return
		}
		t, err := s.CreateTeam(ctx, req.Username, team.Team{Name: req.Name})
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(CreateTeamResponse{Team: t, Err: errs}, w)
	}
}

type UpdateTeamRequest struct {
	Name          string `json:"name"`
	TeamCanonical string `json:"team_canonical"`
}
type UpdateTeamResponse struct {
	Team *team.WithMembers `json:"data,omitempty"`
	Err  string            `json:"error,omitempty"`
}

func (r UpdateTeamResponse) Error() string { return r.Err }

func updateTeam(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req UpdateTeamRequest
			ctx = r.Context()
		)
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(UpdateTeamResponse{Err: err.Error()}, w)
			return
		}
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]

		t, err := s.UpdateTeam(ctx, req.TeamCanonical, team.Team{Name: req.Name})
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(UpdateTeamResponse{Team: t, Err: errs}, w)
	}
}

type GetTeamRequest struct {
	TeamCanonical string `json:"team_canonical"`
}
type GetTeamResponse struct {
	Team *team.WithMembers `json:"data,omitempty"`
	Err  string            `json:"error,omitempty"`
}

func (r GetTeamResponse) Error() string { return r.Err }

func getTeam(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req GetTeamRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]

		t, err := s.GetTeam(ctx, req.TeamCanonical)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(GetTeamResponse{Team: t, Err: errs}, w)
	}
}

type ListTeamsRequest struct {
	TeamCanonical string `json:"team_canonical"`
	Username      string `json:"username"`
}
type ListTeamsResponse struct {
	Teams []*team.WithMembers `json:"data,omitempty"`
	Err   string              `json:"error,omitempty"`
}

func (r ListTeamsResponse) Error() string { return r.Err }

func listTeams(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req ListTeamsRequest
			ctx = r.Context()
		)
		un, ok := ctx.Value(UsernameContextKey).(string)
		if !ok {
			encodeResponse(ListTeamsResponse{Err: "missing username in context"}, w)
			return
		}
		req.Username = un
		ts, err := s.ListTeams(ctx, req.Username)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListTeamsResponse{Teams: ts, Err: errs}, w)
	}
}

type DeleteTeamRequest struct {
	TeamCanonical string `json:"team_canonical"`
}
type DeleteTeamResponse struct {
	Err string `json:"error,omitempty"`
}

func (r DeleteTeamResponse) Error() string { return r.Err }

func deleteTeam(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req DeleteTeamRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]

		err := s.DeleteTeam(ctx, req.TeamCanonical)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(DeleteTeamResponse{Err: errs}, w)
	}
}

type CreateTeamMemberRequest struct {
	TeamCanonical string `json:"team_canonical"`

	team.Member
}
type CreateTeamMemberResponse struct {
	Member *team.Member `json:"data,omitempty"`
	Err    string       `json:"error,omitempty"`
}

func (r CreateTeamMemberResponse) Error() string { return r.Err }

func createTeamMember(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req CreateTeamMemberRequest
			ctx = r.Context()
		)
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(CreateTeamMemberResponse{Err: err.Error()}, w)
			return
		}
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		tm, err := s.CreateTeamMember(ctx, req.TeamCanonical, req.Member)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(CreateTeamMemberResponse{Member: tm, Err: errs}, w)
	}
}

type UpdateTeamMemberRequest struct {
	TeamCanonical  string `json:"team_canonical"`
	MemberUsername string `json:"member_username"`
	Role           string `json:"role"`
}
type UpdateTeamMemberResponse struct {
	Member *team.Member `json:"data,omitempty"`
	Err    string       `json:"error,omitempty"`
}

func (r UpdateTeamMemberResponse) Error() string { return r.Err }

func updateTeamMember(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req UpdateTeamMemberRequest
			ctx = r.Context()
		)
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(UpdateTeamMemberResponse{Err: err.Error()}, w)
			return
		}
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.MemberUsername = vars["member_username"]
		tm, err := s.UpdateTeamMember(ctx, req.TeamCanonical, req.MemberUsername, team.Member{
			Role: role.Role(req.Role),
			User: user.User{Username: req.MemberUsername},
		})
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(UpdateTeamMemberResponse{Member: tm, Err: errs}, w)
	}
}

type DeleteTeamMemberRequest struct {
	TeamCanonical  string `json:"team_canonical"`
	MemberUsername string `json:"member_username"`
}
type DeleteTeamMemberResponse struct {
	Err string `json:"error,omitempty"`
}

func (r DeleteTeamMemberResponse) Error() string { return r.Err }

func deleteTeamMember(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req DeleteTeamMemberRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		req.TeamCanonical = vars["team_canonical"]
		req.MemberUsername = vars["member_username"]

		err := s.DeleteTeamMember(ctx, req.TeamCanonical, req.MemberUsername)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(DeleteTeamMemberResponse{Err: errs}, w)
	}
}
