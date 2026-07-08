package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/apitoken"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/team"
	thttp "github.com/pikoci/pikoci/pikoci/transport/http"
	"github.com/pikoci/pikoci/pikoci/transport/http/client"
	"github.com/pikoci/pikoci/pikoci/trigger"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/wkr"
)

func Test_IsPikoCI_Service(t *testing.T) {
	assert.Implements(t, (*pikoci.Service)(nil), new(client.Client))
}

func jsonHandler(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func TestUserLogin(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/login", func(w http.ResponseWriter, req *http.Request) {
		var lr thttp.UserLoginRequest
		json.NewDecoder(req.Body).Decode(&lr)
		resp := thttp.UserLoginResponse{}
		resp.Data.User = &user.WithMemberships{User: user.User{Username: lr.Username}}
		resp.Data.JWT = "test-jwt-token"
		jsonHandler(w, resp)
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "")
	require.NoError(t, err)

	u, jwt, err := c.UserLogin(context.Background(), "alice", "pass")
	require.NoError(t, err)
	assert.Equal(t, "alice", u.Username)
	assert.Equal(t, "test-jwt-token", jwt)
}

func TestRefreshToken(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/refresh-token", func(w http.ResponseWriter, req *http.Request) {
		resp := thttp.RefreshTokenResponse{}
		resp.Data.User = &user.WithMemberships{User: user.User{Username: "bob"}}
		resp.Data.JWT = "new-jwt"
		jsonHandler(w, resp)
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "old-jwt")
	require.NoError(t, err)

	u, jwt, err := c.RefreshToken(context.Background(), "bob")
	require.NoError(t, err)
	assert.Equal(t, "bob", u.Username)
	assert.Equal(t, "new-jwt", jwt)
}

func TestCreateUser(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateUserResponse{User: &user.User{Username: "newuser"}})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	u, err := c.CreateUser(context.Background(), user.User{Username: "newuser", Password: "password"}, false)
	require.NoError(t, err)
	assert.Equal(t, "newuser", u.Username)
}

func TestListUsers(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListUsersResponse{Users: []*user.User{{Username: "a"}, {Username: "b"}}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	users, err := c.ListUsers(context.Background())
	require.NoError(t, err)
	assert.Len(t, users, 2)
}

func TestGetUser(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users/{username}", func(w http.ResponseWriter, req *http.Request) {
		vars := mux.Vars(req)
		jsonHandler(w, thttp.GetUserResponse{User: &user.User{Username: vars["username"], FullName: "Admin"}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	u, err := c.GetUser(context.Background(), "admin")
	require.NoError(t, err)
	assert.Equal(t, "admin", u.Username)
	assert.Equal(t, "Admin", u.FullName)
}

func TestUpdateUser(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users/{username}", func(w http.ResponseWriter, req *http.Request) {
		var ur thttp.UpdateUserRequest
		json.NewDecoder(req.Body).Decode(&ur)
		vars := mux.Vars(req)
		jsonHandler(w, thttp.UpdateUserResponse{User: &user.User{Username: vars["username"], FullName: ur.FullName, Admin: ur.Admin}})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	u, err := c.UpdateUser(context.Background(), "admin", user.User{FullName: "New Name", Admin: true}, false)
	require.NoError(t, err)
	assert.Equal(t, "admin", u.Username)
	assert.Equal(t, "New Name", u.FullName)
	assert.True(t, u.Admin)
}

func TestDeleteUser(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users/{username}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.DeleteUserResponse{})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeleteUser(context.Background(), "pepito")
	require.NoError(t, err)
}

func TestChangePassword(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users/change-password", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ChangePasswordResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.ChangePassword(context.Background(), "admin", "oldpass", "newpass")
	require.NoError(t, err)
}

func TestUpdateProfile(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/profile", func(w http.ResponseWriter, req *http.Request) {
		var pr thttp.UpdateProfileRequest
		json.NewDecoder(req.Body).Decode(&pr)
		jsonHandler(w, thttp.UpdateProfileResponse{User: &user.User{Username: pr.Username, FullName: pr.FullName}})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	u, err := c.UpdateProfile(context.Background(), "admin", "New Name", "admin")
	require.NoError(t, err)
	assert.Equal(t, "admin", u.Username)
	assert.Equal(t, "New Name", u.FullName)
}

func TestCreateTeam(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateTeamResponse{Team: &team.WithMembers{Team: team.Team{Name: "myteam"}}})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	tm, err := c.CreateTeam(context.Background(), "user", team.Team{Name: "myteam"})
	require.NoError(t, err)
	assert.Equal(t, "myteam", tm.Name)
}

func TestListTeams(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListTeamsResponse{Teams: []*team.WithMembers{{Team: team.Team{Name: "t1"}}}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	teams, err := c.ListTeams(context.Background(), "user")
	require.NoError(t, err)
	assert.Len(t, teams, 1)
}

func TestGetTeam(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}", func(w http.ResponseWriter, req *http.Request) {
		tc := mux.Vars(req)["tc"]
		jsonHandler(w, thttp.GetTeamResponse{Team: &team.WithMembers{Team: team.Team{Name: tc}}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	tm, err := c.GetTeam(context.Background(), "myteam")
	require.NoError(t, err)
	assert.Equal(t, "myteam", tm.Name)
}

func TestUpdateTeam(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdateTeamResponse{Team: &team.WithMembers{Team: team.Team{Name: "updated"}}})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	tm, err := c.UpdateTeam(context.Background(), "old", team.Team{Name: "updated"})
	require.NoError(t, err)
	assert.Equal(t, "updated", tm.Name)
}

func TestDeleteTeam(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.DeleteTeamResponse{})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeleteTeam(context.Background(), "myteam")
	require.NoError(t, err)
}

func TestCreateTeamMember(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/members", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateTeamMemberResponse{Member: &team.Member{User: user.User{Username: "alice"}}})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	m, err := c.CreateTeamMember(context.Background(), "myteam", team.Member{User: user.User{Username: "alice"}})
	require.NoError(t, err)
	assert.Equal(t, "alice", m.User.Username)
}

func TestUpdateTeamMember(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/members/{mu}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdateTeamMemberResponse{Member: &team.Member{Role: role.Admin, User: user.User{Username: "alice"}}})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	m, err := c.UpdateTeamMember(context.Background(), "myteam", "alice", team.Member{Role: role.Admin})
	require.NoError(t, err)
	assert.Equal(t, role.Admin, m.Role)
}

func TestDeleteTeamMember(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/members/{mu}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.DeleteTeamMemberResponse{})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeleteTeamMember(context.Background(), "myteam", "alice")
	require.NoError(t, err)
}

func TestCreatePipeline(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreatePipelineResponse{Pipeline: &pipeline.Pipeline{Name: "mypipe"}})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	p, err := c.CreatePipeline(context.Background(), "team", "mypipe", []byte("config"), nil)
	require.NoError(t, err)
	assert.Equal(t, "mypipe", p.Name)
}

func TestUpdatePipeline(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdatePipelineResponse{Pipeline: &pipeline.Pipeline{Name: "mypipe"}})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	p, err := c.UpdatePipeline(context.Background(), "team", "mypipe", []byte("config"), nil)
	require.NoError(t, err)
	assert.Equal(t, "mypipe", p.Name)
}

func TestUpdatePipeline_WithRename(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}", func(w http.ResponseWriter, req *http.Request) {
		var body thttp.UpdatePipelineRequest
		json.NewDecoder(req.Body).Decode(&body)
		assert.Equal(t, "New Name", body.Name)
		jsonHandler(w, thttp.UpdatePipelineResponse{Pipeline: &pipeline.Pipeline{Name: "New Name", Canonical: "new-name"}})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	p, err := c.UpdatePipeline(context.Background(), "team", "old-name", []byte("config"), nil, "New Name")
	require.NoError(t, err)
	assert.Equal(t, "New Name", p.Name)
	assert.Equal(t, "new-name", p.Canonical)
}

func TestGetPipeline(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineResponse{Pipeline: &pipeline.Pipeline{Name: mux.Vars(req)["pn"]}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	p, err := c.GetPipeline(context.Background(), "team", "mypipe")
	require.NoError(t, err)
	assert.Equal(t, "mypipe", p.Name)
}

func TestListPipelines(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListPipelinesResponse{Pipelines: []*pipeline.Pipeline{{Name: "p1"}, {Name: "p2"}}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	pps, err := c.ListPipelines(context.Background(), "team")
	require.NoError(t, err)
	assert.Len(t, pps, 2)
}

func TestDeletePipeline(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.DeletePipelineResponse{})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeletePipeline(context.Background(), "team", "mypipe")
	require.NoError(t, err)
}

func TestGetPipelineImage_DOT(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/image.{format}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineImageResponse{Image: "dot-data"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	img, err := c.GetPipelineImage(context.Background(), "team", "mypipe", "dot", false, false, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("dot-data"), img)
}

func TestGetPipelineImage_SVG(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/image.{format}", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte("svg-data"))
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	img, err := c.GetPipelineImage(context.Background(), "team", "mypipe", "svg", false, false, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("svg-data"), img)
}

func TestGetPipelineImage_PNG(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/image.{format}", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("png-data"))
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	img, err := c.GetPipelineImage(context.Background(), "team", "mypipe", "png", false, false, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("png-data"), img)
}

func TestCreatePipelineImage(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/image.{format}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreatePipelineImageResponse{Image: "png-data"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	img, err := c.CreatePipelineImage(context.Background(), "team", []byte("config"), nil, "png")
	require.NoError(t, err)
	assert.Equal(t, []byte("png-data"), img)
}

func TestTriggerPipelineJob(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/trigger", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.TriggerPipelineJobResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.TriggerPipelineJob(context.Background(), "team", "pipe", "job1")
	require.NoError(t, err)
}

func TestGetPipelineJob(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineJobResponse{Job: &job.Job{Name: mux.Vars(req)["jn"]}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	j, err := c.GetPipelineJob(context.Background(), "team", "pipe", "job1")
	require.NoError(t, err)
	assert.Equal(t, "job1", j.Name)
}

func TestCreateJobBuild(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateJobBuildResponse{Build: &build.Build{ID: 1}})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	b, err := c.CreateJobBuild(context.Background(), "team", "pipe", "job1", build.Build{})
	require.NoError(t, err)
	assert.Equal(t, uint32(1), b.ID)
}

func TestUpdateJobBuild(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/{bid}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdateJobBuildResponse{})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.UpdateJobBuild(context.Background(), "team", "pipe", "job1", "1", build.Build{})
	require.NoError(t, err)
}

func TestDeleteJobBuild(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/{bid}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.DeleteJobBuildResponse{})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeleteJobBuild(context.Background(), "team", "pipe", "job1", "1")
	require.NoError(t, err)
}

func TestInsertBuildGetVersion(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/{bid}/get-versions", func(w http.ResponseWriter, req *http.Request) {
		var body thttp.InsertBuildGetVersionRequest
		json.NewDecoder(req.Body).Decode(&body)
		assert.Equal(t, "repo", body.StepName)
		assert.Equal(t, uint32(42), body.VersionID)
		jsonHandler(w, thttp.InsertBuildGetVersionResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.InsertBuildGetVersion(context.Background(), "team", "pipe", "job1", 10, "repo", 42)
	require.NoError(t, err)
}

func TestListJobBuilds(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListJobBuildsResponse{Builds: []*build.Build{{ID: 1}, {ID: 2}}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	builds, _, err := c.ListJobBuilds(context.Background(), "team", "pipe", "job1", nil, nil, 0, nil)
	require.NoError(t, err)
	assert.Len(t, builds, 2)
}

func TestStartPendingBuild(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/start-pending", func(w http.ResponseWriter, req *http.Request) {
		var body thttp.StartPendingBuildRequest
		json.NewDecoder(req.Body).Decode(&body)
		assert.Equal(t, uint32(42), body.BuildID)
		jsonHandler(w, thttp.StartPendingBuildResponse{Build: &build.Build{ID: 42, BuildNumber: "1"}})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	b, err := c.StartPendingBuild(context.Background(), "team", "pipe", "job1", 42)
	require.NoError(t, err)
	assert.Equal(t, uint32(42), b.ID)
}

func TestStartPendingBuild_ConcurrencyLimit(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/start-pending", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.StartPendingBuildResponse{Err: pikoci.ErrConcurrencyLimit.Error()})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.StartPendingBuild(context.Background(), "team", "pipe", "job1", 42)
	require.ErrorIs(t, err, pikoci.ErrConcurrencyLimit)
}

func TestStartPendingBuild_NotPending(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/start-pending", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.StartPendingBuildResponse{Err: "build 42 is not in pending status: " + pikoci.ErrBuildNotPending.Error()})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.StartPendingBuild(context.Background(), "team", "pipe", "job1", 42)
	require.ErrorIs(t, err, pikoci.ErrBuildNotPending)
}

func TestFindOldestPendingBuild(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/oldest-pending", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.FindOldestPendingBuildResponse{Build: &build.Build{ID: 5, BuildNumber: "5"}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	b, err := c.FindOldestPendingBuild(context.Background(), "team", "pipe", "job1")
	require.NoError(t, err)
	assert.Equal(t, uint32(5), b.ID)
}

func TestFindOldestPendingBuild_None(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/oldest-pending", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.FindOldestPendingBuildResponse{})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	b, err := c.FindOldestPendingBuild(context.Background(), "team", "pipe", "job1")
	require.NoError(t, err)
	assert.Nil(t, b)
}

func TestNotifySerialGroupPendingBuilds(t *testing.T) {
	called := false
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/notify-serial-groups", func(w http.ResponseWriter, req *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	c.NotifySerialGroupPendingBuilds(context.Background(), "team", "pipe", "job1")
	assert.True(t, called, "should have called the endpoint")
}

func TestCreateResourceVersion(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/versions", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateResourceVersionResponse{Version: &resource.Version{ID: 1}})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	v, err := c.CreateResourceVersion(context.Background(), "team", "pipe", "res1", resource.Version{})
	require.NoError(t, err)
	assert.Equal(t, uint32(1), v.ID)
}

func TestListResourceVersions(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/versions", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListResourceVersionsResponse{Versions: []*resource.Version{{ID: 1}}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	versions, _, err := c.ListResourceVersions(context.Background(), "team", "pipe", "res1", nil, nil, 0)
	require.NoError(t, err)
	assert.Len(t, versions, 1)
}

func TestGetPipelineResource(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineResourceResponse{Resource: &resource.Resource{Name: "res1"}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	res, err := c.GetPipelineResource(context.Background(), "team", "pipe", "res1")
	require.NoError(t, err)
	assert.Equal(t, "res1", res.Name)
}

func TestUpdatePipelineResource(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdatePipelineResourceResponse{})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.UpdatePipelineResource(context.Background(), "team", "pipe", "res1", resource.Resource{})
	require.NoError(t, err)
}

func TestTriggerPipelineResource(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.TriggerPipelineResourceResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.TriggerPipelineResource(context.Background(), "team", "pipe", "res1")
	require.NoError(t, err)
}

func TestCreateTrigger(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/triggers/{name}", func(w http.ResponseWriter, req *http.Request) {
		var body thttp.CreateTriggerRequest
		json.NewDecoder(req.Body).Decode(&body)
		assert.Equal(t, "build succeeded", body.Version["payload"])
		jsonHandler(w, thttp.CreateTriggerResponse{Trigger: &trigger.Trigger{
			ID:      1,
			Name:    "trigger.deploy",
			Version: body.Version,
		}})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	tr, err := c.CreateTrigger(context.Background(), "team", "trigger.deploy", map[string]interface{}{"payload": "build succeeded"})
	require.NoError(t, err)
	assert.Equal(t, uint32(1), tr.ID)
	assert.Equal(t, "trigger.deploy", tr.Name)
}

func TestListTriggersAfter(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/triggers/{name}", func(w http.ResponseWriter, req *http.Request) {
		after := req.URL.Query().Get("after")
		assert.Equal(t, "5", after)
		jsonHandler(w, thttp.ListTriggersAfterResponse{Triggers: []*trigger.Trigger{
			{ID: 6, Name: "trigger.deploy"},
			{ID: 7, Name: "trigger.deploy"},
		}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	triggers, err := c.ListTriggersAfter(context.Background(), "team", "trigger.deploy", 5)
	require.NoError(t, err)
	assert.Len(t, triggers, 2)
	assert.Equal(t, uint32(6), triggers[0].ID)
}

func TestExportDatabase(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/admin/export", func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "Bearer jwt", req.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="pikoci.db"`)
		w.Write([]byte("fake-sqlite-data"))
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	outputPath := filepath.Join(t.TempDir(), "export.db")
	err = c.ExportDatabase(context.Background(), outputPath)
	require.NoError(t, err)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Equal(t, "fake-sqlite-data", string(data))
}

func TestExportDatabase_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/admin/export", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("needs to be admin"))
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	outputPath := filepath.Join(t.TempDir(), "export.db")
	err = c.ExportDatabase(context.Background(), outputPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestSetPipelinePublic(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}", func(w http.ResponseWriter, req *http.Request) {
		var body thttp.UpdatePipelineRequest
		json.NewDecoder(req.Body).Decode(&body)
		assert.NotNil(t, body.Public)
		assert.True(t, *body.Public)
		jsonHandler(w, thttp.UpdatePipelineResponse{})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.SetPipelinePublic(context.Background(), "team", "mypipe", true)
	require.NoError(t, err)
}

func TestGetPublicPipeline(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineResponse{Pipeline: &pipeline.Pipeline{Name: mux.Vars(req)["pn"]}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	p, err := c.GetPublicPipeline(context.Background(), "team", "mypipe")
	require.NoError(t, err)
	assert.Equal(t, "mypipe", p.Name)
}

func TestGetPublicPipelineImage(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/image.{format}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineImageResponse{Image: "dot-data"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	img, err := c.GetPublicPipelineImage(context.Background(), "team", "mypipe", "dot", false, false, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("dot-data"), img)
}

func TestGetPublicPipelineJob(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineJobResponse{Job: &job.Job{Name: mux.Vars(req)["jn"]}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	j, err := c.GetPublicPipelineJob(context.Background(), "team", "pipe", "job1")
	require.NoError(t, err)
	assert.Equal(t, "job1", j.Name)
}

func TestListPublicJobBuilds(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListJobBuildsResponse{Builds: []*build.Build{{ID: 1}, {ID: 2}}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	builds, hasMore, err := c.ListPublicJobBuilds(context.Background(), "team", "pipe", "job1", nil, nil, 0, nil)
	require.NoError(t, err)
	assert.Len(t, builds, 2)
	assert.False(t, hasMore)
}

func TestGetPublicPipelineResource(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineResourceResponse{Resource: &resource.Resource{Name: "res1"}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	res, err := c.GetPublicPipelineResource(context.Background(), "team", "pipe", "res1")
	require.NoError(t, err)
	assert.Equal(t, "res1", res.Name)
}

func TestListPublicResourceVersions(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/versions", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListResourceVersionsResponse{Versions: []*resource.Version{{ID: 1}, {ID: 2}}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	versions, hasMore, err := c.ListPublicResourceVersions(context.Background(), "team", "pipe", "res1", nil, nil, 0)
	require.NoError(t, err)
	assert.Len(t, versions, 2)
	assert.False(t, hasMore)
}

func TestPausePipeline(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/pause", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.PausePipelineResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.PausePipeline(context.Background(), "team", "mypipe")
	require.NoError(t, err)
}

func TestUnpausePipeline(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/unpause", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UnpausePipelineResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.UnpausePipeline(context.Background(), "team", "mypipe")
	require.NoError(t, err)
}

func TestPauseJob(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/pause", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.PauseJobResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.PauseJob(context.Background(), "team", "pipe", "job1")
	require.NoError(t, err)
}

func TestUnpauseJob(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/unpause", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UnpauseJobResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.UnpauseJob(context.Background(), "team", "pipe", "job1")
	require.NoError(t, err)
}

func TestGetJobBuild(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/{bid}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetJobBuildResponse{Build: &build.Build{ID: 42, BuildNumber: "1"}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	b, err := c.GetJobBuild(context.Background(), "team", "pipe", "job1", "1")
	require.NoError(t, err)
	assert.Equal(t, uint32(42), b.ID)
	assert.Equal(t, "1", b.BuildNumber)
}

func TestCancelJobBuild(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/{bid}/cancel", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CancelJobBuildResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.CancelJobBuild(context.Background(), "team", "pipe", "job1", "1")
	require.NoError(t, err)
}

func TestRetryJobBuild(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/{bid}/retry", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.RetryJobBuildResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.RetryJobBuild(context.Background(), "team", "pipe", "job1", "1")
	require.NoError(t, err)
}

func TestCreateRetryJobBuild(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/retry-builds", func(w http.ResponseWriter, req *http.Request) {
		var body thttp.CreateRetryJobBuildRequest
		json.NewDecoder(req.Body).Decode(&body)
		assert.Equal(t, "5", body.ParentBuildNumber)
		jsonHandler(w, thttp.CreateRetryJobBuildResponse{Build: &build.Build{ID: 10, BuildNumber: "6"}})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	b, err := c.CreateRetryJobBuild(context.Background(), "team", "pipe", "job1", "5", build.Build{})
	require.NoError(t, err)
	assert.Equal(t, uint32(10), b.ID)
	assert.Equal(t, "6", b.BuildNumber)
}

func TestCreateRetryJobBuild_ConcurrencyLimit(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/retry-builds", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateRetryJobBuildResponse{Err: pikoci.ErrConcurrencyLimit.Error()})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.CreateRetryJobBuild(context.Background(), "team", "pipe", "job1", "5", build.Build{})
	require.ErrorIs(t, err, pikoci.ErrConcurrencyLimit)
}

func TestFindBuildGetVersions(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds-get-versions/{bid}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.FindBuildGetVersionsResponse{Versions: map[string]uint32{"repo": 42, "image": 7}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	versions, err := c.FindBuildGetVersions(context.Background(), "team", "pipe", "job1", 10)
	require.NoError(t, err)
	assert.Equal(t, uint32(42), versions["repo"])
	assert.Equal(t, uint32(7), versions["image"])
}

func TestPinResourceVersion(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/pin", func(w http.ResponseWriter, req *http.Request) {
		var body thttp.PinResourceVersionRequest
		json.NewDecoder(req.Body).Decode(&body)
		assert.Equal(t, uint32(42), body.VersionID)
		jsonHandler(w, thttp.PinResourceVersionResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.PinResourceVersion(context.Background(), "team", "pipe", "res1", 42)
	require.NoError(t, err)
}

func TestUnpinResourceVersion(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/unpin", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UnpinResourceVersionResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.UnpinResourceVersion(context.Background(), "team", "pipe", "res1")
	require.NoError(t, err)
}

func TestTriggerResourceVersion(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/versions/{vid}/trigger", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.TriggerResourceVersionResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.TriggerResourceVersion(context.Background(), "team", "pipe", "res1", 42)
	require.NoError(t, err)
}

func TestWebhookTrigger(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/webhooks/{token}", func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "my-token", mux.Vars(req)["token"])
		jsonHandler(w, thttp.WebhookTriggerResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.WebhookTrigger(context.Background(), "my-token")
	require.NoError(t, err)
}

func TestRegenerateWebhookToken(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/webhook_token", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.RegenerateWebhookTokenResponse{Token: "new-token-123"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	token, err := c.RegenerateWebhookToken(context.Background(), "team", "pipe", "res1")
	require.NoError(t, err)
	assert.Equal(t, "new-token-123", token)
}

func TestRequestError(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(thttp.ErrorResponse{Err: "forbidden"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.ListTeams(context.Background(), "user")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestRequest_RefreshToken(t *testing.T) {
	var refreshCalled atomic.Int32

	r := mux.NewRouter()
	r.HandleFunc("/teams", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-Refresh-Token", "true")
		jsonHandler(w, thttp.ListTeamsResponse{Teams: []*team.WithMembers{{Team: team.Team{Name: "t1"}}}})
	}).Methods("GET")
	r.HandleFunc("/refresh-token", func(w http.ResponseWriter, req *http.Request) {
		refreshCalled.Add(1)
		resp := thttp.RefreshTokenResponse{}
		resp.Data.User = &user.WithMemberships{User: user.User{Username: "alice"}}
		resp.Data.JWT = "refreshed-jwt"
		jsonHandler(w, resp)
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "authentication")

	c, err := client.New(ts.URL, "old-jwt")
	require.NoError(t, err)
	c.SetConfigPath(configPath)

	teams, err := c.ListTeams(context.Background(), "user")
	require.NoError(t, err)
	assert.Len(t, teams, 1)

	assert.Equal(t, int32(1), refreshCalled.Load())

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, "refreshed-jwt", string(data))
}

func TestRequest_NoRefreshToken(t *testing.T) {
	var refreshCalled atomic.Int32

	r := mux.NewRouter()
	r.HandleFunc("/teams", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListTeamsResponse{Teams: []*team.WithMembers{{Team: team.Team{Name: "t1"}}}})
	}).Methods("GET")
	r.HandleFunc("/refresh-token", func(w http.ResponseWriter, req *http.Request) {
		refreshCalled.Add(1)
		resp := thttp.RefreshTokenResponse{}
		resp.Data.JWT = "new-jwt"
		jsonHandler(w, resp)
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "authentication")

	c, err := client.New(ts.URL, "original-jwt")
	require.NoError(t, err)
	c.SetConfigPath(configPath)

	teams, err := c.ListTeams(context.Background(), "user")
	require.NoError(t, err)
	assert.Len(t, teams, 1)

	assert.Equal(t, int32(0), refreshCalled.Load())

	_, err = os.ReadFile(configPath)
	assert.True(t, os.IsNotExist(err))
}

func TestNew_EmptyHost(t *testing.T) {
	_, err := client.New("", "jwt")
	require.Error(t, err)
}

func TestNew_NoScheme(t *testing.T) {
	c, err := client.New("localhost:8080", "jwt")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestUserLogin_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/login", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UserLoginResponse{Err: "bad creds"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "")
	require.NoError(t, err)

	_, _, err = c.UserLogin(context.Background(), "alice", "wrong")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad creds")
}

func TestRefreshToken_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/refresh-token", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.RefreshTokenResponse{Err: "expired"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, _, err = c.RefreshToken(context.Background(), "alice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestGetUser_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users/{un}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetUserResponse{Err: "not found"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.GetUser(context.Background(), "bob")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetUser_NilUser(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users/{un}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetUserResponse{})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.GetUser(context.Background(), "bob")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user not found")
}

func TestCreateUser_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateUserResponse{Err: "duplicate"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.CreateUser(context.Background(), user.User{Username: "bob"}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestListUsers_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListUsersResponse{Err: "forbidden"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestUpdateUser_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users/{un}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdateUserResponse{Err: "conflict"})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.UpdateUser(context.Background(), "bob", user.User{}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
}

func TestDeleteUser_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users/{un}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.DeleteUserResponse{Err: "cannot delete"})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeleteUser(context.Background(), "bob")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot delete")
}

func TestChangePassword_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/users/change-password", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ChangePasswordResponse{Err: "wrong old password"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.ChangePassword(context.Background(), "alice", "wrong", "new")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrong old password")
}

func TestUpdateProfile_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/profile", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdateProfileResponse{Err: "invalid name"})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.UpdateProfile(context.Background(), "alice", "Alice", "alice2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid name")
}

func TestCreateTeam_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateTeamResponse{Err: "team exists"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.CreateTeam(context.Background(), "alice", team.Team{Name: "dup"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "team exists")
}

func TestGetTeam_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetTeamResponse{Err: "not found"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.GetTeam(context.Background(), "no-team")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpdateTeam_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdateTeamResponse{Err: "conflict"})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.UpdateTeam(context.Background(), "team1", team.Team{Name: "new"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
}

func TestDeleteTeam_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.DeleteTeamResponse{Err: "has pipelines"})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeleteTeam(context.Background(), "team1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has pipelines")
}

func TestCreateTeamMember_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/members", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateTeamMemberResponse{Err: "already member"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.CreateTeamMember(context.Background(), "team1", team.Member{User: user.User{Username: "bob"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already member")
}

func TestUpdateTeamMember_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/members/{mu}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdateTeamMemberResponse{Err: "not member"})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.UpdateTeamMember(context.Background(), "team1", "bob", team.Member{Role: role.Admin})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not member")
}

func TestDeleteTeamMember_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/members/{mu}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.DeleteTeamMemberResponse{Err: "last admin"})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeleteTeamMember(context.Background(), "team1", "bob")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "last admin")
}

func TestCreatePipeline_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreatePipelineResponse{Err: "bad config"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.CreatePipeline(context.Background(), "team1", "pipe", []byte("{}"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad config")
}

func TestSetPipelinePublic_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdatePipelineResponse{Err: "not found"})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.SetPipelinePublic(context.Background(), "team1", "pipe", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetPipeline_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineResponse{Err: "not found"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.GetPipeline(context.Background(), "team1", "nopipe")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListPipelines_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListPipelinesResponse{Err: "forbidden"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.ListPipelines(context.Background(), "team1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestDeletePipeline_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.DeletePipelineResponse{Err: "in use"})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeletePipeline(context.Background(), "team1", "pipe1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "in use")
}

func TestTriggerPipelineJob_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/trigger", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.TriggerPipelineJobResponse{Err: "job not found"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.TriggerPipelineJob(context.Background(), "team1", "pipe", "nojob")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "job not found")
}

func TestPausePipeline_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/pause", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.PausePipelineResponse{Err: "forbidden"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.PausePipeline(context.Background(), "team1", "pipe")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestUnpausePipeline_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/unpause", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UnpausePipelineResponse{Err: "forbidden"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.UnpausePipeline(context.Background(), "team1", "pipe")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestPauseJob_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/pause", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.PauseJobResponse{Err: "forbidden"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.PauseJob(context.Background(), "team1", "pipe", "job1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestUnpauseJob_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/unpause", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UnpauseJobResponse{Err: "forbidden"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.UnpauseJob(context.Background(), "team1", "pipe", "job1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestGetPipelineJob_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineJobResponse{Err: "not found"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.GetPipelineJob(context.Background(), "team1", "pipe", "nojob")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCreateJobBuild_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateJobBuildResponse{Err: "concurrency limit reached"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.CreateJobBuild(context.Background(), "team1", "pipe", "job1", build.Build{})
	require.ErrorIs(t, err, pikoci.ErrConcurrencyLimit)
}

func TestCreateJobBuild_OtherError(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateJobBuildResponse{Err: "some error"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.CreateJobBuild(context.Background(), "team1", "pipe", "job1", build.Build{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "some error")
}

func TestUpdateJobBuild_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/{bn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdateJobBuildResponse{Err: "not found"})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.UpdateJobBuild(context.Background(), "team1", "pipe", "job1", "1", build.Build{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteJobBuild_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/{bn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.DeleteJobBuildResponse{Err: "cannot delete"})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeleteJobBuild(context.Background(), "team1", "pipe", "job1", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot delete")
}

func TestGetJobBuild_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/{bn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetJobBuildResponse{Err: "not found"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.GetJobBuild(context.Background(), "team1", "pipe", "job1", "99")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCancelJobBuild_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/{bn}/cancel", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CancelJobBuildResponse{Err: "already completed"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.CancelJobBuild(context.Background(), "team1", "pipe", "job1", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already completed")
}

func TestRetryJobBuild_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/{bn}/retry", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.RetryJobBuildResponse{Err: "build running"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.RetryJobBuild(context.Background(), "team1", "pipe", "job1", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build running")
}

func TestCreateRetryJobBuild_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/retry-builds", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateRetryJobBuildResponse{Err: "concurrency limit reached"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.CreateRetryJobBuild(context.Background(), "team1", "pipe", "job1", "3", build.Build{})
	require.ErrorIs(t, err, pikoci.ErrConcurrencyLimit)
}

func TestCreateRetryJobBuild_OtherError(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/retry-builds", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateRetryJobBuildResponse{Err: "some error"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.CreateRetryJobBuild(context.Background(), "team1", "pipe", "job1", "3", build.Build{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "some error")
}

func TestFindBuildGetVersions_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds-get-versions/{bid}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.FindBuildGetVersionsResponse{Err: "not found"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.FindBuildGetVersions(context.Background(), "team1", "pipe", "job1", 99)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestInsertBuildGetVersion_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/{bid}/get-versions", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.InsertBuildGetVersionResponse{Err: "failed"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.InsertBuildGetVersion(context.Background(), "team1", "pipe", "job1", 5, "step1", 42)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

func TestStartPendingBuild_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/start-pending", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.StartPendingBuildResponse{Err: "concurrency limit reached"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.StartPendingBuild(context.Background(), "team1", "pipe", "job1", 10)
	require.ErrorIs(t, err, pikoci.ErrConcurrencyLimit)
}

func TestStartPendingBuild_NotPendingError(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/start-pending", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.StartPendingBuildResponse{Err: "build is not in pending status"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.StartPendingBuild(context.Background(), "team1", "pipe", "job1", 10)
	require.ErrorIs(t, err, pikoci.ErrBuildNotPending)
}

func TestStartPendingBuild_OtherError(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/start-pending", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.StartPendingBuildResponse{Err: "some other error"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.StartPendingBuild(context.Background(), "team1", "pipe", "job1", 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "some other error")
}

func TestFindOldestPendingBuild_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds/oldest-pending", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.FindOldestPendingBuildResponse{Err: "not found"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.FindOldestPendingBuild(context.Background(), "team1", "pipe", "job1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListJobBuilds_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListJobBuildsResponse{Err: "forbidden"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, _, err = c.ListJobBuilds(context.Background(), "team1", "pipe", "job1", nil, nil, 0, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestListJobBuilds_HasMore(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListJobBuildsResponse{
			Builds: []*build.Build{{ID: 1}},
			Meta:   &thttp.PageMeta{HasMore: true},
		})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	builds, hasMore, err := c.ListJobBuilds(context.Background(), "team1", "pipe", "job1", nil, nil, 10, nil)
	require.NoError(t, err)
	assert.Len(t, builds, 1)
	assert.True(t, hasMore)
}

func TestCreateResourceVersion_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/versions", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateResourceVersionResponse{Err: "invalid"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.CreateResourceVersion(context.Background(), "team1", "pipe", "res1", resource.Version{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestListResourceVersions_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/versions", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListResourceVersionsResponse{Err: "forbidden"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, _, err = c.ListResourceVersions(context.Background(), "team1", "pipe", "res1", nil, nil, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestListResourceVersions_HasMore(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/versions", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListResourceVersionsResponse{
			Versions: []*resource.Version{{ID: 1}},
			Meta:     &thttp.PageMeta{HasMore: true},
		})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	vers, hasMore, err := c.ListResourceVersions(context.Background(), "team1", "pipe", "res1", nil, nil, 10)
	require.NoError(t, err)
	assert.Len(t, vers, 1)
	assert.True(t, hasMore)
}

func TestGetPipelineResource_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineResourceResponse{Err: "not found"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.GetPipelineResource(context.Background(), "team1", "pipe", "nores")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpdatePipelineResource_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdatePipelineResourceResponse{Err: "invalid"})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.UpdatePipelineResource(context.Background(), "team1", "pipe", "res1", resource.Resource{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestTriggerPipelineResource_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.TriggerPipelineResourceResponse{Err: "not found"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.TriggerPipelineResource(context.Background(), "team1", "pipe", "nores")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPinResourceVersion_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/pin", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.PinResourceVersionResponse{Err: "version not found"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.PinResourceVersion(context.Background(), "team1", "pipe", "res1", 99)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version not found")
}

func TestUnpinResourceVersion_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/unpin", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UnpinResourceVersionResponse{Err: "not pinned"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.UnpinResourceVersion(context.Background(), "team1", "pipe", "res1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not pinned")
}

func TestTriggerResourceVersion_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/versions/{vid}/trigger", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.TriggerResourceVersionResponse{Err: "not found"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.TriggerResourceVersion(context.Background(), "team1", "pipe", "res1", 99)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWebhookTrigger_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/webhooks/{token}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.WebhookTriggerResponse{Err: "invalid token"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.WebhookTrigger(context.Background(), "bad-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token")
}

func TestRegenerateWebhookToken_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/webhook_token", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.RegenerateWebhookTokenResponse{Err: "not found"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.RegenerateWebhookToken(context.Background(), "team1", "pipe", "nores")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCreatePipelineImage_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/image.{format}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreatePipelineImageResponse{Err: "bad config"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.CreatePipelineImage(context.Background(), "team1", []byte("{}"), nil, "dot")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad config")
}

func TestGetPipelineImage_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/image.{format}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineImageResponse{Err: "not found"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.GetPipelineImage(context.Background(), "team1", "pipe", "dot", false, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpdatePipeline_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.UpdatePipelineResponse{Err: "bad config"})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.UpdatePipeline(context.Background(), "team1", "pipe", []byte("{}"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad config")
}

func TestCreateTrigger_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/triggers/{name}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.CreateTriggerResponse{Err: "invalid"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.CreateTrigger(context.Background(), "team1", "my-trigger", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestListTriggersAfter_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/triggers/{name}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListTriggersAfterResponse{Err: "forbidden"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.ListTriggersAfter(context.Background(), "team1", "my-trigger", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestExportDatabase_ServerError(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/admin/export", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.ExportDatabase(context.Background(), filepath.Join(t.TempDir(), "export.sql"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "export failed")
}

func TestRequestRaw_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/image.{format}", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(thttp.ErrorResponse{Err: "pipeline not found"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.GetPipelineImage(context.Background(), "team1", "nopipe", "svg", false, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline not found")
}

func TestRequestRaw_RefreshToken(t *testing.T) {
	var refreshCalled atomic.Int32
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/image.{format}", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-Refresh-Token", "true")
		w.Write([]byte("<svg>test</svg>"))
	}).Methods("GET")
	r.HandleFunc("/refresh-token", func(w http.ResponseWriter, req *http.Request) {
		refreshCalled.Add(1)
		resp := thttp.RefreshTokenResponse{}
		resp.Data.User = &user.WithMemberships{User: user.User{Username: "alice"}}
		resp.Data.JWT = "new-jwt"
		jsonHandler(w, resp)
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	data, err := c.GetPipelineImage(context.Background(), "team1", "pipe", "svg", false, false, nil)
	require.NoError(t, err)
	assert.Contains(t, string(data), "<svg>")
	assert.Equal(t, int32(1), refreshCalled.Load())
}

func TestGetPublicPipeline_DelegatesToGetPipeline(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineResponse{Pipeline: &pipeline.Pipeline{Name: "pub-pipe"}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	pp, err := c.GetPublicPipeline(context.Background(), "team1", "pub-pipe")
	require.NoError(t, err)
	assert.Equal(t, "pub-pipe", pp.Name)
}

func TestGetPublicPipelineJob_DelegatesToGetPipelineJob(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineJobResponse{Job: &job.Job{Name: "pub-job"}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	j, err := c.GetPublicPipelineJob(context.Background(), "team1", "pipe", "pub-job")
	require.NoError(t, err)
	assert.Equal(t, "pub-job", j.Name)
}

func TestListPublicJobBuilds_DelegatesToListJobBuilds(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/jobs/{jn}/builds", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListJobBuildsResponse{Builds: []*build.Build{{ID: 1}}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	builds, _, err := c.ListPublicJobBuilds(context.Background(), "team1", "pipe", "job1", nil, nil, 0, nil)
	require.NoError(t, err)
	assert.Len(t, builds, 1)
}

func TestGetPublicPipelineResource_DelegatesToGetPipelineResource(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineResourceResponse{Resource: &resource.Resource{Canonical: "git.repo"}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	res, err := c.GetPublicPipelineResource(context.Background(), "team1", "pipe", "git.repo")
	require.NoError(t, err)
	assert.Equal(t, "git.repo", res.Canonical)
}

func TestListPublicResourceVersions_DelegatesToListResourceVersions(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/versions", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListResourceVersionsResponse{Versions: []*resource.Version{{ID: 42}}})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	vers, _, err := c.ListPublicResourceVersions(context.Background(), "team1", "pipe", "git.repo", nil, nil, 0)
	require.NoError(t, err)
	assert.Len(t, vers, 1)
	assert.Equal(t, uint32(42), vers[0].ID)
}

func TestGetPublicPipelineImage_DelegatesToGetPipelineImage(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/image.{format}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineImageResponse{Image: "digraph {}"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	img, err := c.GetPublicPipelineImage(context.Background(), "team1", "pipe", "dot", false, false, nil)
	require.NoError(t, err)
	assert.Contains(t, string(img), "digraph")
}

func TestWorkerHeartbeat(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/workers/heartbeat", func(w http.ResponseWriter, req *http.Request) {
		var hr thttp.WorkerHeartbeatRequest
		json.NewDecoder(req.Body).Decode(&hr)
		assert.Equal(t, "worker-1", hr.Name)
		assert.Equal(t, "host1", hr.Hostname)
		assert.Equal(t, "linux", hr.OS)
		assert.Equal(t, "amd64", hr.Arch)
		assert.Equal(t, "v0.4.0", hr.Version)
		assert.Equal(t, "abc123", hr.Commit)
		assert.Equal(t, 2, hr.Concurrency)
		jsonHandler(w, thttp.WorkerHeartbeatResponse{})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.WorkerHeartbeat(context.Background(), wkr.Worker{
		Name:        "worker-1",
		Hostname:    "host1",
		OS:          "linux",
		Arch:        "amd64",
		GoVersion:   "go1.22",
		Version:     "v0.4.0",
		Commit:      "abc123",
		Concurrency: 2,
	})
	require.NoError(t, err)
}

func TestWorkerHeartbeat_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/workers/heartbeat", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.WorkerHeartbeatResponse{Err: "upsert failed"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.WorkerHeartbeat(context.Background(), wkr.Worker{Name: "w1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upsert failed")
}

func TestListWorkers(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/workers", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListWorkersResponse{
			Workers: []*wkr.Worker{
				{ID: 1, Name: "worker-1", Status: wkr.StatusHealthy},
				{ID: 2, Name: "worker-2", Status: wkr.StatusStale},
			},
		})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	workers, err := c.ListWorkers(context.Background())
	require.NoError(t, err)
	assert.Len(t, workers, 2)
	assert.Equal(t, "worker-1", workers[0].Name)
	assert.Equal(t, wkr.StatusHealthy, workers[0].Status)
}

func TestListWorkers_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/workers", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListWorkersResponse{Err: "db error"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	workers, err := c.ListWorkers(context.Background())
	require.Error(t, err)
	assert.Nil(t, workers)
}

func TestWorkersHealth(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/workers/health", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.WorkersHealthResponse{Healthy: true})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	healthy, err := c.WorkersHealth(context.Background())
	require.NoError(t, err)
	assert.True(t, healthy)
}

func TestWorkersHealth_NotHealthy(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/workers/health", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.WorkersHealthResponse{Healthy: false})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	healthy, err := c.WorkersHealth(context.Background())
	require.NoError(t, err)
	assert.False(t, healthy)
}

func TestDeleteWorker(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/workers/{worker_name}", func(w http.ResponseWriter, req *http.Request) {
		vars := mux.Vars(req)
		assert.Equal(t, "worker-1", vars["worker_name"])
		jsonHandler(w, thttp.DeleteWorkerResponse{})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeleteWorker(context.Background(), "worker-1")
	require.NoError(t, err)
}

func TestDeleteWorker_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/workers/{worker_name}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.DeleteWorkerResponse{Err: "worker not found"})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeleteWorker(context.Background(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker not found")
}

func TestGetResourceVersionPath(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/versions/{vid}/path", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetResourceVersionPathResponse{
			Data: &resource.VersionPathResponse{
				Resource: resource.VersionPathResource{
					Canonical: "git.repo",
					Version:   map[string]interface{}{"ref": "abc123"},
				},
				Path:      []resource.VersionPathEntry{{JobName: "build"}},
				Completed: 1,
				Total:     2,
			},
		})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	resp, err := c.GetResourceVersionPath(context.Background(), "team", "pipe", "git.repo", 42)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "git.repo", resp.Resource.Canonical)
	assert.Equal(t, "abc123", resp.Resource.Version["ref"])
	assert.Len(t, resp.Path, 1)
	assert.Equal(t, "build", resp.Path[0].JobName)
	assert.Equal(t, 1, resp.Completed)
	assert.Equal(t, 2, resp.Total)
}

func TestGetResourceVersionPath_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/versions/{vid}/path", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetResourceVersionPathResponse{Err: "version not found"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	resp, err := c.GetResourceVersionPath(context.Background(), "team", "pipe", "git.repo", 42)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version not found")
	assert.Nil(t, resp)
}

func TestGetPublicResourceVersionPath_DelegatesToGetResourceVersionPath(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/resources/{rCan}/versions/{vid}/path", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetResourceVersionPathResponse{
			Data: &resource.VersionPathResponse{
				Resource: resource.VersionPathResource{
					Canonical: "git.repo",
					Version:   map[string]interface{}{"ref": "def456"},
				},
				Path:      []resource.VersionPathEntry{{JobName: "test"}},
				Completed: 0,
				Total:     1,
			},
		})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	resp, err := c.GetPublicResourceVersionPath(context.Background(), "team", "pipe", "git.repo", 42)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "git.repo", resp.Resource.Canonical)
	assert.Equal(t, "def456", resp.Resource.Version["ref"])
	assert.Len(t, resp.Path, 1)
	assert.Equal(t, "test", resp.Path[0].JobName)
	assert.Equal(t, 0, resp.Completed)
	assert.Equal(t, 1, resp.Total)
}

func TestGetPipelineImage_WithVersionID(t *testing.T) {
	var capturedURL string
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/image.{format}", func(w http.ResponseWriter, req *http.Request) {
		capturedURL = req.URL.String()
		jsonHandler(w, thttp.GetPipelineImageResponse{Image: "digraph {}"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	vid := uint32(99)
	img, err := c.GetPipelineImage(context.Background(), "team", "pipe", "dot", false, false, &vid)
	require.NoError(t, err)
	assert.Equal(t, []byte("digraph {}"), img)
	assert.Contains(t, capturedURL, "version_id=99")
}

func TestCreateApiToken(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/api-tokens", func(w http.ResponseWriter, req *http.Request) {
		var cr thttp.CreateApiTokenRequest
		json.NewDecoder(req.Body).Decode(&cr)
		jsonHandler(w, thttp.CreateApiTokenResponse{
			Token: &apitoken.WithPlaintext{
				Token:     apitoken.Token{ID: 1, Name: cr.Name, Personal: cr.Personal, TeamCanonical: cr.TeamCanonical, Role: cr.Role},
				Plaintext: "pko_abc123",
			},
		})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	tok, err := c.CreateApiToken(context.Background(), "admin", "my-token", true, "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "my-token", tok.Name)
	assert.True(t, tok.Personal)
	assert.Equal(t, "pko_abc123", tok.Plaintext)
}

func TestCreateApiToken_WithExpiration(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/api-tokens", func(w http.ResponseWriter, req *http.Request) {
		var cr thttp.CreateApiTokenRequest
		json.NewDecoder(req.Body).Decode(&cr)
		assert.NotEmpty(t, cr.ExpiresAt)
		jsonHandler(w, thttp.CreateApiTokenResponse{
			Token: &apitoken.WithPlaintext{
				Token:     apitoken.Token{ID: 2, Name: cr.Name, Personal: true},
				Plaintext: "pko_xyz789",
			},
		})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	exp := time.Now().Add(24 * time.Hour)
	tok, err := c.CreateApiToken(context.Background(), "admin", "expiring", true, "", "", &exp)
	require.NoError(t, err)
	assert.Equal(t, "expiring", tok.Name)
}

func TestCreateApiToken_TeamScoped(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/api-tokens", func(w http.ResponseWriter, req *http.Request) {
		var cr thttp.CreateApiTokenRequest
		json.NewDecoder(req.Body).Decode(&cr)
		assert.False(t, cr.Personal)
		assert.Equal(t, "main", cr.TeamCanonical)
		assert.Equal(t, role.Write, cr.Role)
		jsonHandler(w, thttp.CreateApiTokenResponse{
			Token: &apitoken.WithPlaintext{
				Token:     apitoken.Token{ID: 3, Name: cr.Name, Personal: false, TeamCanonical: "main", Role: role.Write},
				Plaintext: "pko_team123",
			},
		})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	tok, err := c.CreateApiToken(context.Background(), "admin", "ci-deploy", false, "main", role.Write, nil)
	require.NoError(t, err)
	assert.Equal(t, "main", tok.TeamCanonical)
	assert.Equal(t, role.Write, tok.Role)
}

func TestListApiTokens(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/api-tokens", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.ListApiTokensResponse{
			Tokens: []*apitoken.Token{
				{ID: 1, Name: "tok1", Personal: true},
				{ID: 2, Name: "tok2", Personal: false, TeamCanonical: "main"},
			},
		})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	tokens, err := c.ListApiTokens(context.Background(), "admin")
	require.NoError(t, err)
	assert.Len(t, tokens, 2)
	assert.Equal(t, "tok1", tokens[0].Name)
}

func TestDeleteApiToken(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/api-tokens/{id}", func(w http.ResponseWriter, req *http.Request) {
		vars := mux.Vars(req)
		assert.Equal(t, "42", vars["id"])
		jsonHandler(w, thttp.DeleteApiTokenResponse{})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeleteApiToken(context.Background(), "admin", 42)
	require.NoError(t, err)
}

func TestDeleteApiToken_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/api-tokens/{id}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.DeleteApiTokenResponse{Err: "entity not found"})
	}).Methods("DELETE")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	err = c.DeleteApiToken(context.Background(), "admin", 999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFindApiTokenByHash_ClientStub(t *testing.T) {
	c, err := client.New("http://localhost", "jwt")
	require.NoError(t, err)

	_, err = c.FindApiTokenByHash(context.Background(), "hash")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available on the client")
}

func TestUpdateApiTokenLastUsed_ClientStub(t *testing.T) {
	c, err := client.New("http://localhost", "jwt")
	require.NoError(t, err)

	// Should not panic — it's a no-op
	c.UpdateApiTokenLastUsed(context.Background(), 1)
}

func TestGenerateTeamWorkerToken(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/worker-token", func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "POST", req.Method)
		jsonHandler(w, thttp.GenerateTeamWorkerTokenResponse{Token: "eyJhbG..."})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	token, err := c.GenerateTeamWorkerToken(context.Background(), "main")
	require.NoError(t, err)
	assert.Equal(t, "eyJhbG...", token)
}

func TestGenerateTeamWorkerToken_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/worker-token", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GenerateTeamWorkerTokenResponse{Err: "not admin"})
	}).Methods("POST")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.GenerateTeamWorkerToken(context.Background(), "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not admin")
}

func TestGetTeamWorkerToken(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/worker-token", func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "GET", req.Method)
		jsonHandler(w, thttp.GetTeamWorkerTokenResponse{Token: "eyJhbG..."})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	token, err := c.GetTeamWorkerToken(context.Background(), "main")
	require.NoError(t, err)
	assert.Equal(t, "eyJhbG...", token)
}

func TestGetTeamWorkerToken_Error(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/worker-token", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetTeamWorkerTokenResponse{Err: "not found"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	_, err = c.GetTeamWorkerToken(context.Background(), "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
