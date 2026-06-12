package scheduler

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/team"
	"go.uber.org/mock/gomock"
)

func newTestScheduler(ctrl *gomock.Controller) (*Scheduler, *mock.ResourceRepository, *mock.PipelineRepository) {
	rr := mock.NewResourceRepository(ctrl)
	pr := mock.NewPipelineRepository(ctrl)
	wn := notifier.New()
	logger := slog.Default()
	s := New(rr, pr, wn, logger)
	return s, rr, pr
}

// expectEmptyTickJobs sets up expectations for tickJobs when no pipelines exist.
func expectEmptyTickJobs(pr *mock.PipelineRepository) {
	pr.EXPECT().FilterAll(gomock.Any()).Return(nil, nil)
}

func TestTickResources_NoDueResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr := newTestScheduler(ctrl)
	eval := &mockEvaluator{}
	s.SetEvaluator(eval)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)
	expectEmptyTickJobs(pr)

	s.tick(context.Background())
}

func TestTickResources_ProcessesDueResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr := newTestScheduler(ctrl)
	eval := &mockEvaluator{}
	s.SetEvaluator(eval)

	due := []*resource.ResourceWithPipeline{
		{
			Resource: resource.Resource{
				ID:            1,
				Canonical:     "cron.timer",
				CheckInterval: "@every 30s",
			},
			TeamCanonical:     "main",
			PipelineCanonical: "my-pipeline",
		},
	}

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(due, nil)
	expectEmptyTickJobs(pr)

	s.tick(context.Background())
}

func TestTickResources_MultipleDueResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr := newTestScheduler(ctrl)
	eval := &mockEvaluator{}
	s.SetEvaluator(eval)

	due := []*resource.ResourceWithPipeline{
		{
			Resource:          resource.Resource{ID: 1, Canonical: "cron.a", CheckInterval: "@every 1m"},
			TeamCanonical:     "main",
			PipelineCanonical: "pp1",
		},
		{
			Resource:          resource.Resource{ID: 2, Canonical: "git.b", CheckInterval: "@every 5m"},
			TeamCanonical:     "team2",
			PipelineCanonical: "pp2",
		},
	}

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(due, nil)
	expectEmptyTickJobs(pr)

	s.tick(context.Background())
}

func TestTickResources_DefaultCheckInterval(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr := newTestScheduler(ctrl)
	eval := &mockEvaluator{}
	s.SetEvaluator(eval)

	due := []*resource.ResourceWithPipeline{
		{
			Resource:          resource.Resource{ID: 1, Canonical: "cron.x", CheckInterval: ""},
			TeamCanonical:     "main",
			PipelineCanonical: "pp",
		},
	}

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(due, nil)
	expectEmptyTickJobs(pr)

	s.tick(context.Background())
}

func TestStart_StopsOnContextCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr := newTestScheduler(ctrl)
	eval := &mockEvaluator{}
	s.SetEvaluator(eval)
	s.interval = 50 * time.Millisecond

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil).AnyTimes()
	pr.EXPECT().FilterAll(gomock.Any()).Return(nil, nil).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	time.Sleep(150 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)
}

// --- mockEvaluator ---

type mockEvaluator struct {
	calls []evaluateCall
}

type evaluateCall struct {
	tc, pn, completedJobName string
}

func (m *mockEvaluator) EvaluateDownstreamJobs(_ context.Context, tc, pn, completedJobName string) error {
	m.calls = append(m.calls, evaluateCall{tc, pn, completedJobName})
	return nil
}

// --- tickJobs tests ---

func TestTickJobs_CallsEvaluatorForEachJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr := newTestScheduler(ctrl)
	eval := &mockEvaluator{}
	s.SetEvaluator(eval)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)
	pps := []*pipeline.WithTeam{{
		Pipeline: pipeline.Pipeline{Canonical: "pp1", Jobs: []job.Job{{Name: "lint"}, {Name: "test"}}},
		Team:     team.Team{Canonical: "main"},
	}}
	pr.EXPECT().FilterAll(gomock.Any()).Return(pps, nil)
	s.tick(context.Background())

	assert.Equal(t, 2, len(eval.calls))
	assert.Equal(t, evaluateCall{"main", "pp1", "lint"}, eval.calls[0])
	assert.Equal(t, evaluateCall{"main", "pp1", "test"}, eval.calls[1])
}

func TestTickJobs_SkipsPausedJobs(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr := newTestScheduler(ctrl)
	eval := &mockEvaluator{}
	s.SetEvaluator(eval)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)
	pps := []*pipeline.WithTeam{{
		Pipeline: pipeline.Pipeline{Canonical: "pp1", Jobs: []job.Job{{Name: "active"}, {Name: "paused-job", Paused: true}}},
		Team:     team.Team{Canonical: "main"},
	}}
	pr.EXPECT().FilterAll(gomock.Any()).Return(pps, nil)
	s.tick(context.Background())

	assert.Equal(t, 1, len(eval.calls))
	assert.Equal(t, "active", eval.calls[0].completedJobName)
}

func TestTickJobs_NilEvaluator(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, _ := newTestScheduler(ctrl)
	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)
	// FilterAll should NOT be called — no expectation
	s.tick(context.Background())
}
