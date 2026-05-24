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

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xescugc/pikoci/pikoci"
	"github.com/xescugc/pikoci/pikoci/build"
	"github.com/xescugc/pikoci/pikoci/job"
	"github.com/xescugc/pikoci/pikoci/pipeline"
	"github.com/xescugc/pikoci/pikoci/resource"
	"github.com/xescugc/pikoci/pikoci/team"
	thttp "github.com/xescugc/pikoci/pikoci/transport/http"
	"github.com/xescugc/pikoci/pikoci/transport/http/client"
	"github.com/xescugc/pikoci/pikoci/trigger"
	"github.com/xescugc/pikoci/pikoci/user"
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

	u, err := c.CreateUser(context.Background(), user.User{Username: "newuser", Password: "pass"}, false)
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
		jsonHandler(w, thttp.UpdateTeamMemberResponse{Member: &team.Member{Admin: true, User: user.User{Username: "alice"}}})
	}).Methods("PUT")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	m, err := c.UpdateTeamMember(context.Background(), "myteam", "alice", team.Member{Admin: true})
	require.NoError(t, err)
	assert.True(t, m.Admin)
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

func TestGetPipelineImage(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/teams/{tc}/pipelines/{pn}/image.{format}", func(w http.ResponseWriter, req *http.Request) {
		jsonHandler(w, thttp.GetPipelineImageResponse{Image: "svg-data"})
	}).Methods("GET")
	ts := httptest.NewServer(r)
	defer ts.Close()

	c, err := client.New(ts.URL, "jwt")
	require.NoError(t, err)

	img, err := c.GetPipelineImage(context.Background(), "team", "mypipe", "svg")
	require.NoError(t, err)
	assert.Equal(t, []byte("svg-data"), img)
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

	builds, err := c.ListJobBuilds(context.Background(), "team", "pipe", "job1")
	require.NoError(t, err)
	assert.Len(t, builds, 2)
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

	versions, err := c.ListResourceVersions(context.Background(), "team", "pipe", "res1")
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
