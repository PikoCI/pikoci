package pikoci_test

import (
	"context"
	"testing"

	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/workitem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNextWork_TeamWorkerFiltersToOwnTeam(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FilterAll(ctx).Return([]*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{Canonical: "pipe-a", Jobs: []job.Job{{Name: "build"}}},
			Team:     team.Team{Canonical: "teama"},
		},
		{
			Pipeline: pipeline.Pipeline{Canonical: "pipe-b", Jobs: []job.Job{{Name: "build"}}},
			Team:     team.Team{Canonical: "teamb"},
		},
	}, nil)
	// Only teama's build should be queried
	s.Builds.EXPECT().FindOldestPending(ctx, "teama", "pipe-a", "build").
		Return(&build.Build{ID: 1, BuildNumber: "1", Status: build.Pending}, nil)
	s.Jobs.EXPECT().Find(ctx, "teama", "pipe-a", "build").
		Return(&job.Job{Name: "build"}, nil)
	s.Builds.EXPECT().StartPending(ctx, "teama", "pipe-a", "build", uint32(1)).Return(nil)
	s.Builds.EXPECT().FindByID(ctx, uint32(1)).Return(
		&build.Build{ID: 1, BuildNumber: "1", Status: build.Started, VersionID: 1}, nil)

	wc := workitem.WorkerContext{TeamCanonical: "teama"}
	item, err := s.P.NextWork(ctx, wc)
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, "teama", item.Body.TeamCanonical)
}

// fakeTeamWorkerChecker is a test double implementing TeamWorkerChecker.
type fakeTeamWorkerChecker struct {
	teams map[string]bool
}

func (f *fakeTeamWorkerChecker) HasTeamWorkers(tc string) bool {
	return f.teams[tc]
}

func TestNextWork_GlobalWorkerSkipsTeamWithDedicatedWorker(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.P.TeamWorkerChecker = &fakeTeamWorkerChecker{teams: map[string]bool{"teama": true}}

	s.Pipelines.EXPECT().FilterAll(ctx).Return([]*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{Canonical: "pipe-a", Jobs: []job.Job{{Name: "build"}}},
			Team:     team.Team{Canonical: "teama"},
		},
	}, nil)
	s.Resources.EXPECT().FilterDueResources(ctx).Return(nil, nil)

	// Global worker (empty TeamCanonical) should skip teama because it has dedicated workers
	wc := workitem.WorkerContext{TeamCanonical: ""}
	item, err := s.P.NextWork(ctx, wc)
	require.NoError(t, err)
	assert.Nil(t, item, "global worker should skip team with dedicated workers")
}

func TestNextWork_GlobalWorkerServesTeamWithoutDedicatedWorker(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.P.TeamWorkerChecker = &fakeTeamWorkerChecker{teams: map[string]bool{}}

	s.Pipelines.EXPECT().FilterAll(ctx).Return([]*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{Canonical: "pipe-a", Jobs: []job.Job{{Name: "build"}}},
			Team:     team.Team{Canonical: "teama"},
		},
	}, nil)
	s.Builds.EXPECT().FindOldestPending(ctx, "teama", "pipe-a", "build").
		Return(&build.Build{ID: 1, BuildNumber: "1", Status: build.Pending}, nil)
	s.Jobs.EXPECT().Find(ctx, "teama", "pipe-a", "build").
		Return(&job.Job{Name: "build"}, nil)
	s.Builds.EXPECT().StartPending(ctx, "teama", "pipe-a", "build", uint32(1)).Return(nil)
	s.Builds.EXPECT().FindByID(ctx, uint32(1)).Return(
		&build.Build{ID: 1, BuildNumber: "1", Status: build.Started, VersionID: 1}, nil)

	wc := workitem.WorkerContext{TeamCanonical: ""}
	item, err := s.P.NextWork(ctx, wc)
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, "teama", item.Body.TeamCanonical)
}
