package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// listPipelinesServer is a server whose one member can read team main.
func listPipelinesServer(t *testing.T) (*mock.Service, *httptest.Server, string) {
	t.Helper()
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")

	handler := Handler(s, secret, slog.Default(), nil, "", "test", "abc1234", "", nil)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	um := &user.WithMemberships{
		User:        user.User{Username: "reader"},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Read}},
	}
	s.EXPECT().GetUser(gomock.Any(), "reader").Return(um, nil).AnyTimes()
	return s, server, signJWT(t, secret, um)
}

func getPipelines(t *testing.T, server *httptest.Server, token, query string) map[string]json.RawMessage {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines"+query, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body
}

// Without a limit the route is what it always was: every pipeline, as a full
// object, and no meta. The CLI and the audit-log filter read this shape.
func TestListPipelines_NoLimitIsUnpaged(t *testing.T) {
	s, server, token := listPipelinesServer(t)

	s.EXPECT().ListPipelines(gomock.Any(), "main").Return([]*pipeline.Pipeline{
		{ID: 1, Name: "one", Canonical: "one", Raw: []byte("raw")},
	}, nil)

	body := getPipelines(t, server, token, "")
	_, hasMeta := body["meta"]
	assert.False(t, hasMeta, "unpaged response carries no meta")

	var pps []*pipeline.Pipeline
	require.NoError(t, json.Unmarshal(body["data"], &pps))
	require.Len(t, pps, 1)
	assert.Equal(t, []byte("raw"), pps[0].Raw, "full object, raw included")
}

func TestListPipelines_Paged(t *testing.T) {
	s, server, token := listPipelinesServer(t)

	s.EXPECT().ListPipelinesPage(gomock.Any(), "main", "tally", pipeline.SortName, uint32(2), uint32(2)).
		Return([]*pipeline.Summary{
			{ID: 3, Name: "tally-c", Canonical: "tally-c"},
			{ID: 4, Name: "tally-d", Canonical: "tally-d", Public: true},
		}, uint32(7), nil)

	body := getPipelines(t, server, token, "?limit=2&offset=2&q=%20tally%20")

	var meta PipelinePageMeta
	require.NoError(t, json.Unmarshal(body["meta"], &meta))
	assert.Equal(t, PipelinePageMeta{Total: 7, Limit: 2, Offset: 2, HasMore: true}, meta)

	var sums []map[string]interface{}
	require.NoError(t, json.Unmarshal(body["data"], &sums))
	require.Len(t, sums, 2)
	assert.Equal(t, "tally-d", sums[1]["name"])
	assert.Equal(t, true, sums[1]["public"])
	_, hasRaw := sums[0]["raw"]
	assert.False(t, hasRaw, "summaries carry no raw config")
	_, hasJobs := sums[0]["jobs"]
	assert.False(t, hasJobs, "summaries carry no jobs")
}

func TestListPipelines_PagedLastPage(t *testing.T) {
	s, server, token := listPipelinesServer(t)

	s.EXPECT().ListPipelinesPage(gomock.Any(), "main", "", pipeline.SortCreated, uint32(24), uint32(24)).
		Return([]*pipeline.Summary{{ID: 1, Name: "z", Canonical: "z"}}, uint32(25), nil)

	body := getPipelines(t, server, token, "?limit=24&offset=24&sort=created")

	var meta PipelinePageMeta
	require.NoError(t, json.Unmarshal(body["meta"], &meta))
	assert.False(t, meta.HasMore, "24 + 1 of 25: nothing after this page")
}

// Bad numbers and unknown sorts fall back rather than fail, like the build
// list's parameters do. A limit that is not a number, or zero, is the default
// page rather than "everything", and one past the cap is the cap: a huge
// limit must never reach the repository.
func TestListPipelines_PagedLenientParams(t *testing.T) {
	for _, tc := range []struct {
		name, params string
		limit        uint32
	}{
		{"not a number", "?limit=lots&offset=-3&sort=sideways", defaultPipelinePageSize},
		{"zero", "?limit=0", defaultPipelinePageSize},
		{"past the cap", "?limit=4294967295", maxPipelinePageSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, server, token := listPipelinesServer(t)

			s.EXPECT().ListPipelinesPage(gomock.Any(), "main", "", pipeline.SortName, tc.limit, uint32(0)).
				Return([]*pipeline.Summary{}, uint32(0), nil)

			body := getPipelines(t, server, token, tc.params)

			var meta PipelinePageMeta
			require.NoError(t, json.Unmarshal(body["meta"], &meta))
			assert.Equal(t, PipelinePageMeta{Limit: tc.limit}, meta)
		})
	}
}

func TestListPipelines_PagedTruncatesLongQuery(t *testing.T) {
	// Multi-byte characters: the cut has to land between characters, not
	// inside one, or the query is invalid UTF-8.
	long := strings.Repeat("\u00e9", 300)
	want := strings.Repeat("\u00e9", maxPipelineQueryLen)

	s, server, token := listPipelinesServer(t)
	s.EXPECT().ListPipelinesPage(gomock.Any(), "main", want, pipeline.SortName, uint32(10), uint32(0)).
		Return(nil, uint32(0), nil)

	getPipelines(t, server, token, "?limit=10&q="+url.QueryEscape(long))
}
