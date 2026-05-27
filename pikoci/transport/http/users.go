package http

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/user"
)

type UserLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type UserLoginResponse struct {
	Err  string `json:"error,omitempty"`
	Data struct {
		User *user.WithMemberships `json:"user,omitempty"`
		JWT  string                `json:"jwt,omitempty"`
	} `json:"data,omitempty"`
}

func (r UserLoginResponse) Error() string {
	return r.Err
}

func userLogin(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			ctx = r.Context()
			req UserLoginRequest
		)
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(UserLoginResponse{Err: err.Error()}, w)
			return
		}

		u, jwt, err := s.UserLogin(ctx, req.Username, req.Password)
		var errs string
		if err != nil {
			errs = err.Error()
		}

		resp := UserLoginResponse{
			Data: struct {
				User *user.WithMemberships `json:"user,omitempty"`
				JWT  string                `json:"jwt,omitempty"`
			}{
				User: u,
				JWT:  jwt,
			},
			Err: errs,
		}
		encodeResponse(resp, w)
	}
}

type RefreshTokenResponse struct {
	Err  string `json:"error,omitempty"`
	Data struct {
		User *user.WithMemberships `json:"user,omitempty"`
		JWT  string                `json:"jwt,omitempty"`
	} `json:"data,omitempty"`
}

func (r RefreshTokenResponse) Error() string {
	return r.Err
}

func refreshToken(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		un, _ := ctx.Value(UsernameContextKey).(string)
		if un == "" {
			encodeResponse(RefreshTokenResponse{Err: "missing username"}, w)
			return
		}

		u, jwt, err := s.RefreshToken(ctx, un)
		var errs string
		if err != nil {
			errs = err.Error()
		}

		resp := RefreshTokenResponse{
			Data: struct {
				User *user.WithMemberships `json:"user,omitempty"`
				JWT  string                `json:"jwt,omitempty"`
			}{
				User: u,
				JWT:  jwt,
			},
			Err: errs,
		}
		encodeResponse(resp, w)
	}
}

type ListUsersResponse struct {
	Err   string       `json:"error,omitempty"`
	Users []*user.User `json:"data,omitempty"`
}

func (r ListUsersResponse) Error() string {
	return r.Err
}

func listUsers(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			ctx = r.Context()
		)
		us, err := s.ListUsers(ctx)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListUsersResponse{Users: us, Err: errs}, w)
	}
}

type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
	Admin    bool   `json:"admin"`
	IsHash   bool   `json:"is_hash"`
}
type CreateUserResponse struct {
	User *user.User `json:"data,omitempty"`
	Err  string     `json:"error,omitempty"`
}

func (r CreateUserResponse) Error() string {
	return r.Err
}

func createUser(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req CreateUserRequest
			ctx = r.Context()
		)
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(CreateUserResponse{Err: err.Error()}, w)
			return
		}
		u, err := s.CreateUser(ctx, user.User{Username: req.Username, Password: req.Password, FullName: req.FullName, Admin: req.Admin}, req.IsHash)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(CreateUserResponse{User: u, Err: errs}, w)
	}
}

type GetUserResponse struct {
	User *user.User `json:"data,omitempty"`
	Err  string     `json:"error,omitempty"`
}

func (r GetUserResponse) Error() string {
	return r.Err
}

func getUser(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		vars := mux.Vars(r)
		un := vars["username"]

		um, err := s.GetUser(ctx, un)
		var errs string
		if err != nil {
			errs = err.Error()
			encodeResponse(GetUserResponse{Err: errs}, w)
			return
		}
		encodeResponse(GetUserResponse{User: &um.User, Err: errs}, w)
	}
}

type UpdateUserRequest struct {
	FullName string `json:"full_name"`
	Username string `json:"username"`
	Password string `json:"password"`
	Admin    bool   `json:"admin"`
	IsHash   bool   `json:"is_hash"`
}
type UpdateUserResponse struct {
	User *user.User `json:"data,omitempty"`
	Err  string     `json:"error,omitempty"`
}

func (r UpdateUserResponse) Error() string {
	return r.Err
}

func updateUser(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req UpdateUserRequest
			ctx = r.Context()
		)
		vars := mux.Vars(r)
		un := vars["username"]

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(UpdateUserResponse{Err: err.Error()}, w)
			return
		}
		u, err := s.UpdateUser(ctx, un, user.User{
			FullName: req.FullName,
			Username: req.Username,
			Password: req.Password,
			Admin:    req.Admin,
		}, req.IsHash)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(UpdateUserResponse{User: u, Err: errs}, w)
	}
}

type DeleteUserResponse struct {
	Err string `json:"error,omitempty"`
}

func (r DeleteUserResponse) Error() string {
	return r.Err
}

func deleteUser(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		vars := mux.Vars(r)
		un := vars["username"]

		err := s.DeleteUser(ctx, un)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(DeleteUserResponse{Err: errs}, w)
	}
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}
type ChangePasswordResponse struct {
	Err string `json:"error,omitempty"`
}

func (r ChangePasswordResponse) Error() string {
	return r.Err
}

func changePassword(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req ChangePasswordRequest
			ctx = r.Context()
		)
		un, _ := ctx.Value(UsernameContextKey).(string)
		if un == "" {
			encodeResponse(ChangePasswordResponse{Err: "missing username"}, w)
			return
		}

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(ChangePasswordResponse{Err: err.Error()}, w)
			return
		}

		err = s.ChangePassword(ctx, un, req.OldPassword, req.NewPassword)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ChangePasswordResponse{Err: errs}, w)
	}
}

type UpdateProfileRequest struct {
	FullName string `json:"full_name"`
	Username string `json:"username"`
}
type UpdateProfileResponse struct {
	User *user.User `json:"data,omitempty"`
	Err  string     `json:"error,omitempty"`
}

func (r UpdateProfileResponse) Error() string {
	return r.Err
}

func updateProfile(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req UpdateProfileRequest
			ctx = r.Context()
		)
		un, _ := ctx.Value(UsernameContextKey).(string)
		if un == "" {
			encodeResponse(UpdateProfileResponse{Err: "missing username"}, w)
			return
		}

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(UpdateProfileResponse{Err: err.Error()}, w)
			return
		}

		u, err := s.UpdateProfile(ctx, un, req.FullName, req.Username)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(UpdateProfileResponse{User: u, Err: errs}, w)
	}
}
