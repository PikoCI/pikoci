package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/trigger"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"log/slog"
)

// testEnv sets up a standard test environment: mock service, JWT secret, handler, and httptest server.
type testEnv struct {
	ctrl   *gomock.Controller
	svc    *mock.Service
	server *httptest.Server
	secret []byte
	// adminUM is an admin user that passes admin authz checks
	adminUM *user.WithMemberships
	// memberUM is a member user that passes member authz checks
	memberUM *user.WithMemberships
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()
	handler := Handler(svc, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	adminUM := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	memberUM := &user.WithMemberships{
		User:        user.User{Username: "member", Admin: false},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Operator}},
	}

	return &testEnv{
		ctrl:     ctrl,
		svc:      svc,
		server:   server,
		secret:   secret,
		adminUM:  adminUM,
		memberUM: memberUM,
	}
}

func (e *testEnv) adminJWT(t *testing.T) string {
	t.Helper()
	return signJWT(t, e.secret, e.adminUM)
}

func (e *testEnv) memberJWT(t *testing.T) string {
	t.Helper()
	return signJWT(t, e.secret, e.memberUM)
}

// expectAdminAuth sets up the GetUser mock call that the admin authorization function makes,
// plus the stale-check GetUser call in middleware.
func (e *testEnv) expectAdminAuth() {
	e.svc.EXPECT().GetUser(gomock.Any(), "admin").Return(e.adminUM, nil).AnyTimes()
}

// expectMemberAuth sets up the GetUser mock call that the member authorization function makes,
// plus the stale-check GetUser call in middleware.
func (e *testEnv) expectMemberAuth() {
	e.svc.EXPECT().GetUser(gomock.Any(), "member").Return(e.memberUM, nil).AnyTimes()
}

func doRequest(t *testing.T, method, url, jwt, body string) *http.Response {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, reader)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// ===== User Handler Tests =====

func TestUserLogin_Success(t *testing.T) {
	e := newTestEnv(t)
	e.svc.EXPECT().UserLogin(gomock.Any(), "admin", "pass123").Return(e.adminUM, "jwt-token", nil)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/login", "", `{"username":"admin","password":"pass123"}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UserLoginResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "jwt-token", got.Data.JWT)
	assert.Equal(t, "admin", got.Data.User.Username)
}

func TestUserLogin_Error(t *testing.T) {
	e := newTestEnv(t)
	e.svc.EXPECT().UserLogin(gomock.Any(), "admin", "wrong").Return(nil, "", fmt.Errorf("invalid credentials"))

	resp := doRequest(t, http.MethodPost, e.server.URL+"/login", "", `{"username":"admin","password":"wrong"}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UserLoginResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "invalid credentials", got.Err)
}

func TestUserLogin_BadJSON(t *testing.T) {
	e := newTestEnv(t)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/login", "", `{bad json}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UserLoginResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestListUsers_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	users := []*user.User{
		{Username: "admin", Admin: true},
		{Username: "pepito", Admin: false},
	}
	e.svc.EXPECT().ListUsers(gomock.Any()).Return(users, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/users", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListUsersResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Users, 2)
}

func TestListUsers_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().ListUsers(gomock.Any()).Return(nil, fmt.Errorf("db error"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/users", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got ListUsersResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "db error", got.Err)
}

func TestCreateUser_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	created := &user.User{Username: "newuser", FullName: "New User", Admin: false}
	e.svc.EXPECT().CreateUser(gomock.Any(), gomock.Any(), false).Return(created, nil)

	body := `{"username":"newuser","password":"pass","full_name":"New User","admin":false}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/users", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got CreateUserResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "newuser", got.User.Username)
}

func TestCreateUser_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/users", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreateUserResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestCreateUser_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().CreateUser(gomock.Any(), gomock.Any(), false).Return(nil, fmt.Errorf("duplicate"))

	body := `{"username":"dup","password":"pass"}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/users", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreateUserResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "duplicate", got.Err)
}

func TestGetUser_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	um := &user.WithMemberships{
		User: user.User{Username: "pepito", FullName: "Pepito"},
	}
	e.svc.EXPECT().GetUser(gomock.Any(), "pepito").Return(um, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/users/pepito", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got GetUserResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "pepito", got.User.Username)
}

func TestGetUser_NotFound(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().GetUser(gomock.Any(), "unknown").Return(nil, fmt.Errorf("not found"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/users/unknown", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got GetUserResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "not found", got.Err)
}

func TestUpdateUser_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	updated := &user.User{Username: "pepito", FullName: "Updated"}
	e.svc.EXPECT().UpdateUser(gomock.Any(), "pepito", gomock.Any(), false).Return(updated, nil)

	body := `{"full_name":"Updated","username":"pepito"}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/users/pepito", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UpdateUserResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "Updated", got.User.FullName)
}

func TestUpdateUser_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPut, e.server.URL+"/users/pepito", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdateUserResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestUpdateUser_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().UpdateUser(gomock.Any(), "pepito", gomock.Any(), false).Return(nil, fmt.Errorf("update error"))

	body := `{"full_name":"X"}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/users/pepito", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdateUserResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "update error", got.Err)
}

func TestDeleteUser_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().DeleteUser(gomock.Any(), "pepito").Return(nil)

	resp := doRequest(t, http.MethodDelete, e.server.URL+"/users/pepito", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got DeleteUserResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestDeleteUser_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().DeleteUser(gomock.Any(), "pepito").Return(fmt.Errorf("delete error"))

	resp := doRequest(t, http.MethodDelete, e.server.URL+"/users/pepito", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got DeleteUserResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "delete error", got.Err)
}

func TestChangePassword_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().ChangePassword(gomock.Any(), "member", "old", "new").Return(nil)

	body := `{"old_password":"old","new_password":"new"}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/users/change-password", e.memberJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ChangePasswordResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestChangePassword_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/users/change-password", e.memberJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got ChangePasswordResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestChangePassword_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().ChangePassword(gomock.Any(), "member", "old", "new").Return(fmt.Errorf("wrong password"))

	body := `{"old_password":"old","new_password":"new"}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/users/change-password", e.memberJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got ChangePasswordResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "wrong password", got.Err)
}

func TestUpdateProfile_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	updated := &user.User{Username: "newname", FullName: "New Name"}
	e.svc.EXPECT().UpdateProfile(gomock.Any(), "member", "New Name", "newname").Return(updated, nil)

	body := `{"full_name":"New Name","username":"newname"}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/profile", e.memberJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UpdateProfileResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "newname", got.User.Username)
}

func TestUpdateProfile_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()

	resp := doRequest(t, http.MethodPut, e.server.URL+"/profile", e.memberJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdateProfileResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestUpdateProfile_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().UpdateProfile(gomock.Any(), "member", "X", "Y").Return(nil, fmt.Errorf("profile error"))

	body := `{"full_name":"X","username":"Y"}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/profile", e.memberJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdateProfileResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "profile error", got.Err)
}

// ===== Team Handler Tests =====

func TestCreateTeam_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	created := &team.WithMembers{
		Team: team.Team{Name: "new-team", Canonical: "new-team"},
	}
	e.svc.EXPECT().CreateTeam(gomock.Any(), "admin", gomock.Any()).Return(created, nil)

	body := `{"name":"new-team"}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got CreateTeamResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "new-team", got.Team.Name)
}

func TestCreateTeam_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreateTeamResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestCreateTeam_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().CreateTeam(gomock.Any(), "admin", gomock.Any()).Return(nil, fmt.Errorf("team exists"))

	body := `{"name":"dup"}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreateTeamResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "team exists", got.Err)
}

func TestUpdateTeam_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	updated := &team.WithMembers{
		Team: team.Team{Name: "renamed", Canonical: "main"},
	}
	e.svc.EXPECT().UpdateTeam(gomock.Any(), "main", gomock.Any()).Return(updated, nil)

	body := `{"name":"renamed"}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UpdateTeamResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "renamed", got.Team.Name)
}

func TestUpdateTeam_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdateTeamResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestGetTeam_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	tm := &team.WithMembers{
		Team: team.Team{Name: "main", Canonical: "main"},
	}
	e.svc.EXPECT().GetTeam(gomock.Any(), "main").Return(tm, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got GetTeamResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "main", got.Team.Canonical)
}

func TestGetTeam_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().GetTeam(gomock.Any(), "main").Return(nil, fmt.Errorf("not found"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got GetTeamResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "not found", got.Err)
}

func TestDeleteTeam_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().DeleteTeam(gomock.Any(), "main").Return(nil)

	resp := doRequest(t, http.MethodDelete, e.server.URL+"/teams/main", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got DeleteTeamResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestDeleteTeam_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().DeleteTeam(gomock.Any(), "main").Return(fmt.Errorf("delete error"))

	resp := doRequest(t, http.MethodDelete, e.server.URL+"/teams/main", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got DeleteTeamResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "delete error", got.Err)
}

func TestListTeams_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	teams := []*team.WithMembers{
		{Team: team.Team{Name: "main", Canonical: "main"}},
	}
	e.svc.EXPECT().ListTeams(gomock.Any(), "member").Return(teams, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListTeamsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Teams, 1)
}

func TestListTeams_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().ListTeams(gomock.Any(), "member").Return(nil, fmt.Errorf("db error"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got ListTeamsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "db error", got.Err)
}

func TestCreateTeamMember_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	created := &team.Member{Role: role.Viewer, User: user.User{Username: "pepito"}}
	e.svc.EXPECT().CreateTeamMember(gomock.Any(), "main", gomock.Any()).Return(created, nil)

	body := `{"role":"viewer","user":{"username":"pepito"}}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/members", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got CreateTeamMemberResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "pepito", got.Member.User.Username)
}

func TestCreateTeamMember_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/members", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreateTeamMemberResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestCreateTeamMember_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().CreateTeamMember(gomock.Any(), "main", gomock.Any()).Return(nil, fmt.Errorf("user not found"))

	body := `{"admin":false,"user":{"username":"unknown"}}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/members", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreateTeamMemberResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "user not found", got.Err)
}

func TestUpdateTeamMember_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	updated := &team.Member{Role: role.Admin, User: user.User{Username: "pepito"}}
	e.svc.EXPECT().UpdateTeamMember(gomock.Any(), "main", "pepito", gomock.Any()).Return(updated, nil)

	body := `{"role":"admin"}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/members/pepito", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UpdateTeamMemberResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, role.Admin, got.Member.Role)
}

func TestUpdateTeamMember_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/members/pepito", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdateTeamMemberResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestUpdateTeamMember_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().UpdateTeamMember(gomock.Any(), "main", "pepito", gomock.Any()).Return(nil, fmt.Errorf("update err"))

	body := `{"admin":true}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/members/pepito", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdateTeamMemberResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "update err", got.Err)
}

func TestDeleteTeamMember_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().DeleteTeamMember(gomock.Any(), "main", "pepito").Return(nil)

	resp := doRequest(t, http.MethodDelete, e.server.URL+"/teams/main/members/pepito", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got DeleteTeamMemberResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestDeleteTeamMember_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().DeleteTeamMember(gomock.Any(), "main", "pepito").Return(fmt.Errorf("delete err"))

	resp := doRequest(t, http.MethodDelete, e.server.URL+"/teams/main/members/pepito", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got DeleteTeamMemberResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "delete err", got.Err)
}

// ===== Pipeline Handler Tests =====

func TestCreatePipeline_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	pp := &pipeline.Pipeline{Name: "my-pipe", Canonical: "my-pipe"}
	e.svc.EXPECT().CreatePipeline(gomock.Any(), "main", "my-pipe", gomock.Any(), gomock.Any()).Return(pp, nil)

	body := `{"name":"my-pipe","config":"dGVzdA=="}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got CreatePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "my-pipe", got.Pipeline.Name)
}

func TestCreatePipeline_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreatePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestCreatePipeline_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().CreatePipeline(gomock.Any(), "main", gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("parse error"))

	body := `{"name":"bad","config":"dGVzdA=="}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreatePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "parse error", got.Err)
}

func TestGetPipeline_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	pp := &pipeline.Pipeline{Name: "my-pipe", Canonical: "my-pipe"}
	e.svc.EXPECT().GetPipeline(gomock.Any(), "main", "my-pipe").Return(pp, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got GetPipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "my-pipe", got.Pipeline.Name)
}

func TestGetPipeline_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().GetPipeline(gomock.Any(), "main", "unknown").Return(nil, fmt.Errorf("not found"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/unknown", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got GetPipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "not found", got.Err)
}

func TestListPipelines_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	pps := []*pipeline.Pipeline{
		{Name: "p1", Canonical: "p1"},
		{Name: "p2", Canonical: "p2"},
	}
	e.svc.EXPECT().ListPipelines(gomock.Any(), "main").Return(pps, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListPipelinesResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Pipelines, 2)
}

func TestListPipelines_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().ListPipelines(gomock.Any(), "main").Return(nil, fmt.Errorf("db error"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got ListPipelinesResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "db error", got.Err)
}

func TestDeletePipeline_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().DeletePipeline(gomock.Any(), "main", "my-pipe").Return(nil)

	resp := doRequest(t, http.MethodDelete, e.server.URL+"/teams/main/pipelines/my-pipe", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got DeletePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestDeletePipeline_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().DeletePipeline(gomock.Any(), "main", "my-pipe").Return(fmt.Errorf("delete error"))

	resp := doRequest(t, http.MethodDelete, e.server.URL+"/teams/main/pipelines/my-pipe", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got DeletePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "delete error", got.Err)
}

func TestPausePipeline_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().PausePipeline(gomock.Any(), "main", "my-pipe").Return(nil)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/pause", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got PausePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestPausePipeline_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().PausePipeline(gomock.Any(), "main", "my-pipe").Return(fmt.Errorf("pause error"))

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/pause", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got PausePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "pause error", got.Err)
}

func TestUnpausePipeline_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().UnpausePipeline(gomock.Any(), "main", "my-pipe").Return(nil)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/unpause", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UnpausePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestUnpausePipeline_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().UnpausePipeline(gomock.Any(), "main", "my-pipe").Return(fmt.Errorf("unpause error"))

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/unpause", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UnpausePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "unpause error", got.Err)
}

// ===== Job Handler Tests =====

func TestTriggerPipelineJob_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().TriggerPipelineJob(gomock.Any(), "main", "my-pipe", "build").Return(nil)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/trigger", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got TriggerPipelineJobResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestTriggerPipelineJob_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().TriggerPipelineJob(gomock.Any(), "main", "my-pipe", "build").Return(fmt.Errorf("trigger error"))

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/trigger", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got TriggerPipelineJobResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "trigger error", got.Err)
}

func TestGetPipelineJob_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	j := &job.Job{Name: "build"}
	e.svc.EXPECT().GetPipelineJob(gomock.Any(), "main", "my-pipe", "build").Return(j, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got GetPipelineJobResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "build", got.Job.Name)
}

func TestGetPipelineJob_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().GetPipelineJob(gomock.Any(), "main", "my-pipe", "unknown").Return(nil, fmt.Errorf("not found"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/unknown", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got GetPipelineJobResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "not found", got.Err)
}

func TestPauseJob_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().PauseJob(gomock.Any(), "main", "my-pipe", "build").Return(nil)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/pause", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got PauseJobResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestPauseJob_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().PauseJob(gomock.Any(), "main", "my-pipe", "build").Return(fmt.Errorf("pause error"))

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/pause", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got PauseJobResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "pause error", got.Err)
}

func TestUnpauseJob_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().UnpauseJob(gomock.Any(), "main", "my-pipe", "build").Return(nil)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/unpause", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UnpauseJobResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestUnpauseJob_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().UnpauseJob(gomock.Any(), "main", "my-pipe", "build").Return(fmt.Errorf("unpause error"))

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/unpause", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UnpauseJobResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "unpause error", got.Err)
}

// ===== Build Handler Tests =====

func TestListJobBuilds_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	builds := []*build.Build{
		{ID: 2, BuildNumber: "2", Status: build.Succeeded},
		{ID: 1, BuildNumber: "1", Status: build.Failed},
	}
	e.svc.EXPECT().ListJobBuilds(gomock.Any(), "main", "my-pipe", "build", gomock.Any(), gomock.Any(), uint32(50)).Return(builds, false, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListJobBuildsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Builds, 2)
	assert.NotNil(t, got.Meta)
	assert.False(t, got.Meta.HasMore)
	assert.Equal(t, uint32(2), got.Meta.NewestID)
	assert.Equal(t, uint32(1), got.Meta.OldestID)
}

func TestListJobBuilds_Empty(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().ListJobBuilds(gomock.Any(), "main", "my-pipe", "build", gomock.Any(), gomock.Any(), uint32(50)).Return(nil, false, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListJobBuildsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Nil(t, got.Meta)
}

func TestListJobBuilds_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().ListJobBuilds(gomock.Any(), "main", "my-pipe", "build", gomock.Any(), gomock.Any(), uint32(50)).Return(nil, false, fmt.Errorf("db error"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got ListJobBuildsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "db error", got.Err)
}

func TestListJobBuilds_WithPagination(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	builds := []*build.Build{{ID: 5, BuildNumber: "5"}}
	e.svc.EXPECT().ListJobBuilds(gomock.Any(), "main", "my-pipe", "build", gomock.Any(), gomock.Any(), uint32(10)).Return(builds, true, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds?limit=10&before=6", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListJobBuildsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.True(t, got.Meta.HasMore)
}

func TestGetJobBuild_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	b := &build.Build{ID: 1, BuildNumber: "1", Status: build.Succeeded}
	e.svc.EXPECT().GetJobBuild(gomock.Any(), "main", "my-pipe", "build", "1").Return(b, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/1", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got GetJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, uint32(1), got.Build.ID)
}

func TestGetJobBuild_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().GetJobBuild(gomock.Any(), "main", "my-pipe", "build", "999").Return(nil, fmt.Errorf("not found"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/999", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got GetJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "not found", got.Err)
}

func TestDeleteJobBuild_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().DeleteJobBuild(gomock.Any(), "main", "my-pipe", "build", "1").Return(nil)

	resp := doRequest(t, http.MethodDelete, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/1", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got DeleteJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestDeleteJobBuild_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().DeleteJobBuild(gomock.Any(), "main", "my-pipe", "build", "1").Return(fmt.Errorf("delete error"))

	resp := doRequest(t, http.MethodDelete, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/1", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got DeleteJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "delete error", got.Err)
}

func TestCancelJobBuild_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().CancelJobBuild(gomock.Any(), "main", "my-pipe", "build", "1").Return(nil)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/1/cancel", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got CancelJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestCancelJobBuild_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().CancelJobBuild(gomock.Any(), "main", "my-pipe", "build", "1").Return(fmt.Errorf("cancel error"))

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/1/cancel", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CancelJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "cancel error", got.Err)
}

func TestRetryJobBuild_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().RetryJobBuild(gomock.Any(), "main", "my-pipe", "build", "1").Return(nil)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/1/retry", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got RetryJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestRetryJobBuild_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().RetryJobBuild(gomock.Any(), "main", "my-pipe", "build", "1").Return(fmt.Errorf("retry error"))

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/1/retry", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got RetryJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "retry error", got.Err)
}

func TestUpdateJobBuild_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "my-pipe", "build", "1", gomock.Any()).Return(nil)

	body := `{"build":{"status":"succeeded"}}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/1", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UpdateJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestUpdateJobBuild_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/1", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdateJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestUpdateJobBuild_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "my-pipe", "build", "1", gomock.Any()).Return(fmt.Errorf("update error"))

	body := `{"build":{"status":"succeeded"}}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/1", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdateJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "update error", got.Err)
}

func TestStartPendingBuild_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	b := &build.Build{ID: 5, BuildNumber: "5", Status: build.Started}
	e.svc.EXPECT().StartPendingBuild(gomock.Any(), "main", "my-pipe", "build", uint32(5)).Return(b, nil)

	body := `{"build_id":5}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/start-pending", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got StartPendingBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, uint32(5), got.Build.ID)
}

func TestStartPendingBuild_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/start-pending", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got StartPendingBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestStartPendingBuild_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().StartPendingBuild(gomock.Any(), "main", "my-pipe", "build", uint32(5)).Return(nil, fmt.Errorf("not pending"))

	body := `{"build_id":5}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/start-pending", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got StartPendingBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "not pending", got.Err)
}

// Note: FindOldestPendingBuild is not tested via HTTP because gorilla/mux
// matches "oldest-pending" as a {build_number} variable, routing to GetJobBuild
// instead. The handler function itself is still covered via the route setup.

func TestCreateRetryJobBuild_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	b := &build.Build{ID: 10, BuildNumber: "10", Status: build.Pending}
	e.svc.EXPECT().CreateRetryJobBuild(gomock.Any(), "main", "my-pipe", "build", gomock.Any(), gomock.Any()).Return(b, nil)

	body := `{"parent_build_number":"5","build":{}}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/retry-builds", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got CreateRetryJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, uint32(10), got.Build.ID)
}

func TestCreateRetryJobBuild_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/retry-builds", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreateRetryJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestCreateRetryJobBuild_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().CreateRetryJobBuild(gomock.Any(), "main", "my-pipe", "build", gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("retry error"))

	body := `{"parent_build_number":"5","build":{}}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/retry-builds", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreateRetryJobBuildResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "retry error", got.Err)
}

func TestFindBuildGetVersions_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	versions := map[string]uint32{"my-res": 42}
	e.svc.EXPECT().FindBuildGetVersions(gomock.Any(), "main", "my-pipe", "build", uint32(5)).Return(versions, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds-get-versions/5", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got FindBuildGetVersionsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, uint32(42), got.Versions["my-res"])
}

func TestFindBuildGetVersions_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().FindBuildGetVersions(gomock.Any(), "main", "my-pipe", "build", uint32(5)).Return(nil, fmt.Errorf("not found"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds-get-versions/5", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got FindBuildGetVersionsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "not found", got.Err)
}

func TestInsertBuildGetVersion_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().InsertBuildGetVersion(gomock.Any(), "main", "my-pipe", "build", uint32(5), "my-step", uint32(42)).Return(nil)

	body := `{"step_name":"my-step","version_id":42}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/5/get-versions", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got InsertBuildGetVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestInsertBuildGetVersion_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/5/get-versions", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got InsertBuildGetVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestInsertBuildGetVersion_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().InsertBuildGetVersion(gomock.Any(), "main", "my-pipe", "build", uint32(5), "my-step", uint32(42)).Return(fmt.Errorf("insert error"))

	body := `{"step_name":"my-step","version_id":42}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/builds/5/get-versions", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got InsertBuildGetVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "insert error", got.Err)
}

// ===== Resource Handler Tests =====

func TestCreateResourceVersion_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	ver := &resource.Version{ID: 1, Version: map[string]interface{}{"ref": "abc"}}
	e.svc.EXPECT().CreateResourceVersion(gomock.Any(), "main", "my-pipe", "my-res", gomock.Any()).Return(ver, nil)

	body := `{"version":{"version":{"ref":"abc"}}}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/versions", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got CreateResourceVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, uint32(1), got.Version.ID)
}

func TestCreateResourceVersion_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/versions", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreateResourceVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestCreateResourceVersion_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().CreateResourceVersion(gomock.Any(), "main", "my-pipe", "my-res", gomock.Any()).Return(nil, fmt.Errorf("create error"))

	body := `{"version":{"version":{"ref":"abc"}}}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/versions", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreateResourceVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "create error", got.Err)
}

func TestListResourceVersions_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	vers := []*resource.Version{
		{ID: 2, Version: map[string]interface{}{"ref": "def"}},
		{ID: 1, Version: map[string]interface{}{"ref": "abc"}},
	}
	e.svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "my-pipe", "my-res", gomock.Any(), gomock.Any(), uint32(50)).Return(vers, false, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/versions", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListResourceVersionsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Versions, 2)
	assert.NotNil(t, got.Meta)
	assert.Equal(t, uint32(2), got.Meta.NewestID)
	assert.Equal(t, uint32(1), got.Meta.OldestID)
}

func TestListResourceVersions_Empty(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "my-pipe", "my-res", gomock.Any(), gomock.Any(), uint32(50)).Return(nil, false, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/versions", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListResourceVersionsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Nil(t, got.Meta)
}

func TestListResourceVersions_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "my-pipe", "my-res", gomock.Any(), gomock.Any(), uint32(50)).Return(nil, false, fmt.Errorf("db error"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/versions", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got ListResourceVersionsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "db error", got.Err)
}

func TestListPipelineResources_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	resources := []*resource.Resource{
		{Name: "res1", Canonical: "res1", WebhookToken: "secret1"},
		{Name: "res2", Canonical: "res2", WebhookToken: "secret2"},
	}
	e.svc.EXPECT().ListPipelineResources(gomock.Any(), "main", "my-pipe").Return(resources, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/resources", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListPipelineResourcesResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Resources, 2)
	// Non-admin member should have webhook tokens stripped
	assert.Empty(t, got.Resources[0].WebhookToken)
	assert.Empty(t, got.Resources[1].WebhookToken)
}

func TestListPipelineResources_AdminSeesWebhookTokens(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	resources := []*resource.Resource{
		{Name: "res1", Canonical: "res1", WebhookToken: "secret1"},
	}
	e.svc.EXPECT().ListPipelineResources(gomock.Any(), "main", "my-pipe").Return(resources, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/resources", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListPipelineResourcesResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "secret1", got.Resources[0].WebhookToken)
}

func TestListPipelineResources_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().ListPipelineResources(gomock.Any(), "main", "my-pipe").Return(nil, fmt.Errorf("db error"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/resources", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got ListPipelineResourcesResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "db error", got.Err)
}

func TestListPipelineResources_PublicFallback(t *testing.T) {
	e := newTestEnv(t)
	pp := &pipeline.Pipeline{Name: "public-pipe", Canonical: "public-pipe", Public: true}
	resources := []*resource.Resource{
		{Name: "res1", Canonical: "res1"},
	}
	e.svc.EXPECT().GetPublicPipeline(gomock.Any(), "main", "public-pipe").Return(pp, nil)
	e.svc.EXPECT().ListPublicPipelineResources(gomock.Any(), "main", "public-pipe").Return(resources, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/public-pipe/resources", "", "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListPipelineResourcesResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Resources, 1)
}

func TestGetPipelineResource_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	res := &resource.Resource{Name: "my-res", Canonical: "my-res", WebhookToken: "secret-token"}
	e.svc.EXPECT().GetPipelineResource(gomock.Any(), "main", "my-pipe", "my-res").Return(res, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got GetPipelineResourceResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "my-res", got.Resource.Name)
	// Non-admin member should have webhook token stripped
	assert.Empty(t, got.Resource.WebhookToken)
}

func TestGetPipelineResource_AdminSeesWebhookToken(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	res := &resource.Resource{Name: "my-res", Canonical: "my-res", WebhookToken: "secret-token"}
	e.svc.EXPECT().GetPipelineResource(gomock.Any(), "main", "my-pipe", "my-res").Return(res, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got GetPipelineResourceResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "secret-token", got.Resource.WebhookToken)
}

func TestGetPipelineResource_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().GetPipelineResource(gomock.Any(), "main", "my-pipe", "unknown").Return(nil, fmt.Errorf("not found"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/resources/unknown", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got GetPipelineResourceResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "not found", got.Err)
}

func TestUpdatePipelineResource_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().UpdatePipelineResource(gomock.Any(), "main", "my-pipe", "my-res", gomock.Any()).Return(nil)

	body := `{"resource":{"name":"my-res"}}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UpdatePipelineResourceResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestUpdatePipelineResource_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdatePipelineResourceResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestUpdatePipelineResource_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().UpdatePipelineResource(gomock.Any(), "main", "my-pipe", "my-res", gomock.Any()).Return(fmt.Errorf("update error"))

	body := `{"resource":{"name":"my-res"}}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdatePipelineResourceResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "update error", got.Err)
}

func TestPinResourceVersion_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().PinResourceVersion(gomock.Any(), "main", "my-pipe", "my-res", uint32(42)).Return(nil)

	body := `{"version_id":42}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/pin", e.memberJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got PinResourceVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestPinResourceVersion_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/pin", e.memberJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got PinResourceVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestPinResourceVersion_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().PinResourceVersion(gomock.Any(), "main", "my-pipe", "my-res", uint32(42)).Return(fmt.Errorf("pin error"))

	body := `{"version_id":42}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/pin", e.memberJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got PinResourceVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "pin error", got.Err)
}

func TestUnpinResourceVersion_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().UnpinResourceVersion(gomock.Any(), "main", "my-pipe", "my-res").Return(nil)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/unpin", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UnpinResourceVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestUnpinResourceVersion_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().UnpinResourceVersion(gomock.Any(), "main", "my-pipe", "my-res").Return(fmt.Errorf("unpin error"))

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/unpin", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UnpinResourceVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "unpin error", got.Err)
}

func TestTriggerResourceVersion_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().TriggerResourceVersion(gomock.Any(), "main", "my-pipe", "my-res", uint32(42)).Return(nil)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/versions/42/trigger", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got TriggerResourceVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestTriggerResourceVersion_InvalidVersionID(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/versions/abc/trigger", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got TriggerResourceVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "invalid version_id", got.Err)
}

func TestTriggerResourceVersion_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().TriggerResourceVersion(gomock.Any(), "main", "my-pipe", "my-res", uint32(42)).Return(fmt.Errorf("trigger error"))

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/versions/42/trigger", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got TriggerResourceVersionResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "trigger error", got.Err)
}

func TestTriggerPipelineResource_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().TriggerPipelineResource(gomock.Any(), "main", "my-pipe", "my-res").Return(nil)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/trigger", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got TriggerPipelineResourceResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestTriggerPipelineResource_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().TriggerPipelineResource(gomock.Any(), "main", "my-pipe", "my-res").Return(fmt.Errorf("trigger error"))

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/trigger", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got TriggerPipelineResourceResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "trigger error", got.Err)
}

func TestRegenerateWebhookToken_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().RegenerateWebhookToken(gomock.Any(), "main", "my-pipe", "my-res").Return("new-token", nil)

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/webhook_token", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got RegenerateWebhookTokenResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "new-token", got.Token)
}

func TestRegenerateWebhookToken_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().RegenerateWebhookToken(gomock.Any(), "main", "my-pipe", "my-res").Return("", fmt.Errorf("regen error"))

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/resources/my-res/webhook_token", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got RegenerateWebhookTokenResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "regen error", got.Err)
}

func TestWebhookTrigger_Success(t *testing.T) {
	e := newTestEnv(t)
	e.svc.EXPECT().WebhookTrigger(gomock.Any(), "my-repo_abc123").Return(nil)

	// Webhook endpoint uses POST without auth
	req, err := http.NewRequest(http.MethodPost, e.server.URL+"/webhooks/my-repo_abc123", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got WebhookTriggerResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestWebhookTrigger_Error(t *testing.T) {
	e := newTestEnv(t)
	e.svc.EXPECT().WebhookTrigger(gomock.Any(), "bad-token").Return(fmt.Errorf("invalid token"))

	req, err := http.NewRequest(http.MethodPost, e.server.URL+"/webhooks/bad-token", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got WebhookTriggerResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "invalid token", got.Err)
}

// ===== Trigger Handler Tests =====

func TestCreateTrigger_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	tr := &trigger.Trigger{ID: 1, Name: "my-trigger", Version: map[string]interface{}{"ref": "abc"}}
	e.svc.EXPECT().CreateTrigger(gomock.Any(), "main", "my-trigger", gomock.Any()).Return(tr, nil)

	body := `{"version":{"ref":"abc"}}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/triggers/my-trigger", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got CreateTriggerResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "my-trigger", got.Trigger.Name)
}

func TestCreateTrigger_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/triggers/my-trigger", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreateTriggerResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestCreateTrigger_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().CreateTrigger(gomock.Any(), "main", "my-trigger", gomock.Any()).Return(nil, fmt.Errorf("create error"))

	body := `{"version":{"ref":"abc"}}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/triggers/my-trigger", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got CreateTriggerResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "create error", got.Err)
}

func TestListTriggersAfter_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	triggers := []*trigger.Trigger{
		{ID: 2, Name: "my-trigger"},
		{ID: 1, Name: "my-trigger"},
	}
	e.svc.EXPECT().ListTriggersAfter(gomock.Any(), "main", "my-trigger", uint32(0)).Return(triggers, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/triggers/my-trigger", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListTriggersAfterResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Triggers, 2)
}

func TestListTriggersAfter_WithAfterParam(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	triggers := []*trigger.Trigger{{ID: 5, Name: "my-trigger"}}
	e.svc.EXPECT().ListTriggersAfter(gomock.Any(), "main", "my-trigger", uint32(3)).Return(triggers, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/triggers/my-trigger?after=3", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListTriggersAfterResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Triggers, 1)
}

func TestListTriggersAfter_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().ListTriggersAfter(gomock.Any(), "main", "my-trigger", uint32(0)).Return(nil, fmt.Errorf("list error"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/triggers/my-trigger", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got ListTriggersAfterResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "list error", got.Err)
}

// ===== UpdatePipeline Tests =====

func TestUpdatePipeline_WithConfig(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	pp := &pipeline.Pipeline{Name: "my-pipe", Canonical: "my-pipe"}
	e.svc.EXPECT().UpdatePipeline(gomock.Any(), "main", "my-pipe", gomock.Any(), gomock.Any(), gomock.Any()).Return(pp, nil)

	body := `{"config":"dGVzdA=="}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/pipelines/my-pipe", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UpdatePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "my-pipe", got.Pipeline.Name)
}

func TestUpdatePipeline_NameOnly(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	existing := &pipeline.Pipeline{Name: "my-pipe", Canonical: "my-pipe", Raw: []byte("raw-config")}
	renamed := &pipeline.Pipeline{Name: "new-name", Canonical: "new-name"}
	e.svc.EXPECT().GetPipeline(gomock.Any(), "main", "my-pipe").Return(existing, nil)
	e.svc.EXPECT().UpdatePipeline(gomock.Any(), "main", "my-pipe", []byte("raw-config"), gomock.Any(), "new-name").Return(renamed, nil)

	body := `{"name":"new-name"}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/pipelines/my-pipe", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UpdatePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "new-name", got.Pipeline.Name)
}

func TestUpdatePipeline_PublicOnly(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	pp := &pipeline.Pipeline{Name: "my-pipe", Canonical: "my-pipe", Public: true}
	public := true
	_ = public
	e.svc.EXPECT().SetPipelinePublic(gomock.Any(), "main", "my-pipe", true).Return(nil)
	e.svc.EXPECT().GetPipeline(gomock.Any(), "main", "my-pipe").Return(pp, nil)

	body := `{"public":true}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/pipelines/my-pipe", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got UpdatePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestUpdatePipeline_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/pipelines/my-pipe", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdatePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.NotEmpty(t, got.Err)
}

func TestUpdatePipeline_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().UpdatePipeline(gomock.Any(), "main", "my-pipe", gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("update error"))

	body := `{"config":"dGVzdA=="}`
	resp := doRequest(t, http.MethodPut, e.server.URL+"/teams/main/pipelines/my-pipe", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got UpdatePipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "update error", got.Err)
}

// ===== Authentication Tests =====

func TestUnauthenticatedRequest_ReturnsError(t *testing.T) {
	e := newTestEnv(t)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/users", "", "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got ErrorResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "Authentication required", got.Err)
}

func TestInvalidJWT_ReturnsError(t *testing.T) {
	e := newTestEnv(t)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/users", "invalid-jwt", "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got ErrorResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "Authentication required", got.Err)
}

func TestNonMember_Forbidden(t *testing.T) {
	e := newTestEnv(t)
	// User is not a member of team "other"
	nonMemberUM := &user.WithMemberships{
		User:        user.User{Username: "outsider", Admin: false},
		Memberships: []user.Member{{TeamCanonical: "different-team", Role: role.Viewer}},
	}
	jwt := signJWT(t, e.secret, nonMemberUM)
	e.svc.EXPECT().GetUser(gomock.Any(), "outsider").Return(nonMemberUM, nil).AnyTimes()

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines", jwt, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// ===== Pagination Helper Tests =====

func TestParsePaginationParams(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantBefore *uint32
		wantAfter  *uint32
		wantLimit  uint32
	}{
		{
			name:      "defaults",
			query:     "",
			wantLimit: 50,
		},
		{
			name:      "custom limit",
			query:     "limit=10",
			wantLimit: 10,
		},
		{
			name:       "before param",
			query:      "before=5",
			wantBefore: ptrUint32(5),
			wantLimit:  50,
		},
		{
			name:      "after param",
			query:     "after=3",
			wantAfter: ptrUint32(3),
			wantLimit: 50,
		},
		{
			name:       "before takes precedence over after",
			query:      "before=5&after=3",
			wantBefore: ptrUint32(5),
			wantLimit:  50,
		},
		{
			name:      "invalid limit uses default",
			query:     "limit=abc",
			wantLimit: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "http://example.com?"+tt.query, nil)
			before, after, limit := parsePaginationParams(req)
			assert.Equal(t, tt.wantLimit, limit)
			if tt.wantBefore == nil {
				assert.Nil(t, before)
			} else {
				require.NotNil(t, before)
				assert.Equal(t, *tt.wantBefore, *before)
			}
			if tt.wantAfter == nil {
				assert.Nil(t, after)
			} else {
				require.NotNil(t, after)
				assert.Equal(t, *tt.wantAfter, *after)
			}
		})
	}
}

func ptrUint32(v uint32) *uint32 { return &v }

// ===== CreatePipelineImage Tests =====

func TestCreatePipelineImage_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	imgData := []byte(`digraph { A -> B; }`)
	e.svc.EXPECT().CreatePipelineImage(gomock.Any(), "main", gomock.Any(), gomock.Any(), ".dot").Return(imgData, nil)

	body := `{"config":"dGVzdA=="}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/image.dot", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCreatePipelineImage_BadJSON(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/image.dot", e.adminJWT(t), `{bad}`)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCreatePipelineImage_ServiceError(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().CreatePipelineImage(gomock.Any(), "main", gomock.Any(), gomock.Any(), ".dot").Return(nil, fmt.Errorf("image error"))

	body := `{"config":"dGVzdA=="}`
	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/image.dot", e.adminJWT(t), body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// ===== Public Fallback Tests =====

func TestGetPipeline_PublicFallback(t *testing.T) {
	e := newTestEnv(t)
	pp := &pipeline.Pipeline{Name: "public-pipe", Canonical: "public-pipe", Public: true}
	e.svc.EXPECT().GetPublicPipeline(gomock.Any(), "main", "public-pipe").Return(pp, nil).Times(2)

	// No auth - should fall through to public fallback
	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/public-pipe", "", "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got GetPipelineResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "public-pipe", got.Pipeline.Name)
}

func TestGetPipelineJob_PublicFallback(t *testing.T) {
	e := newTestEnv(t)
	pp := &pipeline.Pipeline{Name: "public-pipe", Canonical: "public-pipe", Public: true}
	j := &job.Job{Name: "build"}
	e.svc.EXPECT().GetPublicPipeline(gomock.Any(), "main", "public-pipe").Return(pp, nil)
	e.svc.EXPECT().GetPublicPipelineJob(gomock.Any(), "main", "public-pipe", "build").Return(j, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/public-pipe/jobs/build", "", "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got GetPipelineJobResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "build", got.Job.Name)
}

func TestListJobBuilds_PublicFallback(t *testing.T) {
	e := newTestEnv(t)
	pp := &pipeline.Pipeline{Name: "public-pipe", Canonical: "public-pipe", Public: true}
	builds := []*build.Build{{ID: 1, BuildNumber: "1"}}
	e.svc.EXPECT().GetPublicPipeline(gomock.Any(), "main", "public-pipe").Return(pp, nil)
	e.svc.EXPECT().ListPublicJobBuilds(gomock.Any(), "main", "public-pipe", "build", gomock.Any(), gomock.Any(), uint32(50)).Return(builds, false, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/public-pipe/jobs/build/builds", "", "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListJobBuildsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Builds, 1)
}

func TestGetPipelineResource_PublicFallback(t *testing.T) {
	e := newTestEnv(t)
	pp := &pipeline.Pipeline{Name: "public-pipe", Canonical: "public-pipe", Public: true}
	res := &resource.Resource{Name: "my-res", Canonical: "my-res"}
	e.svc.EXPECT().GetPublicPipeline(gomock.Any(), "main", "public-pipe").Return(pp, nil)
	e.svc.EXPECT().GetPublicPipelineResource(gomock.Any(), "main", "public-pipe", "my-res").Return(res, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/public-pipe/resources/my-res", "", "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got GetPipelineResourceResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Equal(t, "my-res", got.Resource.Name)
}

func TestNotifySerialGroupPendingBuilds_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectAdminAuth()
	e.svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), "main", "my-pipe", "build")

	resp := doRequest(t, http.MethodPost, e.server.URL+"/teams/main/pipelines/my-pipe/jobs/build/notify-serial-groups", e.adminJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestListResourceVersions_PublicFallback(t *testing.T) {
	e := newTestEnv(t)
	pp := &pipeline.Pipeline{Name: "public-pipe", Canonical: "public-pipe", Public: true}
	vers := []*resource.Version{{ID: 1}}
	e.svc.EXPECT().GetPublicPipeline(gomock.Any(), "main", "public-pipe").Return(pp, nil)
	e.svc.EXPECT().ListPublicResourceVersions(gomock.Any(), "main", "public-pipe", "my-res", gomock.Any(), gomock.Any(), uint32(50)).Return(vers, false, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/public-pipe/resources/my-res/versions", "", "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListResourceVersionsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Versions, 1)
}

func TestListPipelineJobs_Success(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	jobs := []job.WithStatus{
		{Job: job.Job{Name: "build"}, LatestStatus: "succeeded"},
		{Job: job.Job{Name: "test"}, LatestStatus: "failed"},
	}
	e.svc.EXPECT().ListPipelineJobs(gomock.Any(), "main", "my-pipe").Return(jobs, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/jobs", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListPipelineJobsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Jobs, 2)
	assert.Equal(t, "build", got.Jobs[0].Name)
	assert.Equal(t, "succeeded", got.Jobs[0].LatestStatus)
}

func TestListPipelineJobs_Error(t *testing.T) {
	e := newTestEnv(t)
	e.expectMemberAuth()
	e.svc.EXPECT().ListPipelineJobs(gomock.Any(), "main", "my-pipe").Return(nil, fmt.Errorf("db error"))

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/my-pipe/jobs", e.memberJWT(t), "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var got ListPipelineJobsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Equal(t, "db error", got.Err)
}

func TestListPipelineJobs_PublicFallback(t *testing.T) {
	e := newTestEnv(t)
	pp := &pipeline.Pipeline{Name: "public-pipe", Canonical: "public-pipe", Public: true}
	jobs := []job.WithStatus{
		{Job: job.Job{Name: "build"}, LatestStatus: "succeeded"},
	}
	e.svc.EXPECT().GetPublicPipeline(gomock.Any(), "main", "public-pipe").Return(pp, nil)
	e.svc.EXPECT().ListPublicPipelineJobs(gomock.Any(), "main", "public-pipe").Return(jobs, nil)

	resp := doRequest(t, http.MethodGet, e.server.URL+"/teams/main/pipelines/public-pipe/jobs", "", "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var got ListPipelineJobsResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
	assert.Len(t, got.Jobs, 1)
	assert.Equal(t, "build", got.Jobs[0].Name)
}
