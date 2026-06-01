package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/queue"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/team"
	"go.uber.org/mock/gomock"
	"gocloud.dev/pubsub"
)

func newTestScheduler(ctrl *gomock.Controller) (*Scheduler, *mock.ResourceRepository, *mock.PipelineRepository, *mock.BuildRepository, *mock.Topic, *mock.Topic) {
	rr := mock.NewResourceRepository(ctrl)
	pr := mock.NewPipelineRepository(ctrl)
	br := mock.NewBuildRepository(ctrl)
	jobTopic := mock.NewTopic(ctrl)
	checkTopic := mock.NewTopic(ctrl)
	logger := slog.Default()
	s := New(rr, pr, br, jobTopic, checkTopic, logger)
	return s, rr, pr, br, jobTopic, checkTopic
}

// expectEmptyTickJobs sets up expectations for tickJobs when no pipelines exist.
func expectEmptyTickJobs(pr *mock.PipelineRepository) {
	pr.EXPECT().FilterAll(gomock.Any()).Return(nil, nil)
}

func TestTickResources_NoDueResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, _, _, _ := newTestScheduler(ctrl)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)
	expectEmptyTickJobs(pr)

	s.tick(context.Background())
}

func TestTickResources_ProcessesDueResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, _, _, topic := newTestScheduler(ctrl)

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

	expectedBody := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "my-pipeline",
		ResourceCanonical: "cron.timer",
	}
	mb, _ := json.Marshal(expectedBody)
	topic.EXPECT().Send(gomock.Any(), &pubsub.Message{Body: mb}).Return(nil)

	rr.EXPECT().Update(gomock.Any(), "main", "my-pipeline", "cron.timer", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn, rCan string, r resource.Resource) error {
			assert.False(t, r.LastCheck.IsZero(), "LastCheck should be set")
			assert.False(t, r.NextCheck.IsZero(), "NextCheck should be set")
			assert.True(t, r.NextCheck.After(r.LastCheck), "NextCheck should be after LastCheck")
			diff := r.NextCheck.Sub(r.LastCheck)
			assert.InDelta(t, 30*time.Second, diff, float64(2*time.Second))
			return nil
		},
	)

	expectEmptyTickJobs(pr)

	s.tick(context.Background())
}

func TestTickResources_MultipleDueResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, _, _, topic := newTestScheduler(ctrl)

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
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).Times(2)
	rr.EXPECT().Update(gomock.Any(), "main", "pp1", "cron.a", gomock.Any()).Return(nil)
	rr.EXPECT().Update(gomock.Any(), "team2", "pp2", "git.b", gomock.Any()).Return(nil)

	expectEmptyTickJobs(pr)

	s.tick(context.Background())
}

func TestTickResources_DefaultCheckInterval(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, _, _, topic := newTestScheduler(ctrl)

	due := []*resource.ResourceWithPipeline{
		{
			Resource:          resource.Resource{ID: 1, Canonical: "cron.x", CheckInterval: ""},
			TeamCanonical:     "main",
			PipelineCanonical: "pp",
		},
	}

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(due, nil)
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil)
	rr.EXPECT().Update(gomock.Any(), "main", "pp", "cron.x", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn, rCan string, r resource.Resource) error {
			diff := r.NextCheck.Sub(r.LastCheck)
			assert.InDelta(t, 1*time.Minute, diff, float64(2*time.Second))
			return nil
		},
	)

	expectEmptyTickJobs(pr)

	s.tick(context.Background())
}

func TestStart_StopsOnContextCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, _, _, _ := newTestScheduler(ctrl)
	s.interval = 50 * time.Millisecond

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil).AnyTimes()
	pr.EXPECT().FilterAll(gomock.Any()).Return(nil, nil).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	time.Sleep(150 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestTickResources_SendErrorSkipsResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, _, _, topic := newTestScheduler(ctrl)

	due := []*resource.ResourceWithPipeline{
		{
			Resource:          resource.Resource{ID: 1, Canonical: "cron.fail", CheckInterval: "@every 1m"},
			TeamCanonical:     "main",
			PipelineCanonical: "pp",
		},
	}

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(due, nil)
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).Return(assert.AnError)

	expectEmptyTickJobs(pr)

	s.tick(context.Background())
}

// --- tickJobs tests ---

func TestTickJobs_TriggersWhenCommonVersionExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br, topic, _ := newTestScheduler(ctrl)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)

	pps := []*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{
				Name: "my-pipeline", Canonical: "my-pipeline",
				Jobs: []job.Job{
					{Name: "lint"},
					{Name: "test-mock"},
					{
						Name: "test-backends",
						Plan: []job.PlanStep{
							{
								Type: job.StepTypeGet,
								Get: &job.GetStep{
									Type:    "git",
									Name:    "repo",
									Passed:  []string{"lint", "test-mock"},
									Trigger: true,
								},
							},
						},
					},
				},
			},
			Team: team.Team{Canonical: "main"},
		},
	}
	pr.EXPECT().FilterAll(gomock.Any()).Return(pps, nil)

	br.EXPECT().FindReadyDownstreamVersion(
		gomock.Any(), "main", "my-pipeline",
		[]string{"lint", "test-mock"}, "test-backends", "repo", 2,
	).Return(uint32(42), true, nil)

	// Pin check — resource is not pinned
	rr.EXPECT().Find(gomock.Any(), "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{}, nil)

	// Check for existing pending build (none)
	br.EXPECT().FindOldestPending(gomock.Any(), "main", "my-pipeline", "test-backends").
		Return(nil, nil)

	// Create a pending build
	br.EXPECT().Create(gomock.Any(), "main", "my-pipeline", "test-backends", gomock.Any()).
		Return(uint32(100), "1", nil)

	topic.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, msg *pubsub.Message) error {
		var body queue.Body
		err := json.Unmarshal(msg.Body, &body)
		require.NoError(t, err)
		assert.Equal(t, "test-backends", body.JobName)
		assert.Equal(t, "my-pipeline", body.PipelineCanonical)
		assert.Equal(t, "main", body.TeamCanonical)
		assert.Equal(t, uint32(42), body.VersionID)
		assert.Equal(t, uint32(100), body.BuildID)
		return nil
	})

	s.tick(context.Background())
}

func TestTickJobs_SkipsWhenNoCommonVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br, _, _ := newTestScheduler(ctrl)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)

	pps := []*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{
				Name: "my-pipeline", Canonical: "my-pipeline",
				Jobs: []job.Job{
					{
						Name: "downstream",
						Plan: []job.PlanStep{
							{
								Type: job.StepTypeGet,
								Get: &job.GetStep{
									Type:    "git",
									Name:    "repo",
									Passed:  []string{"upstream"},
									Trigger: true,
								},
							},
						},
					},
				},
			},
			Team: team.Team{Canonical: "main"},
		},
	}
	pr.EXPECT().FilterAll(gomock.Any()).Return(pps, nil)

	br.EXPECT().FindReadyDownstreamVersion(
		gomock.Any(), "main", "my-pipeline",
		[]string{"upstream"}, "downstream", "repo", 1,
	).Return(uint32(0), false, nil)

	// topic.Send should NOT be called
	s.tick(context.Background())
}

func TestTickJobs_SkipsWhenPendingBuildExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br, _, _ := newTestScheduler(ctrl)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)

	pps := []*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{
				Name: "my-pipeline", Canonical: "my-pipeline",
				Jobs: []job.Job{
					{
						Name: "deploy",
						Plan: []job.PlanStep{
							{
								Type: job.StepTypeGet,
								Get: &job.GetStep{
									Type:    "git",
									Name:    "repo",
									Passed:  []string{"lint"},
									Trigger: true,
								},
							},
						},
					},
				},
			},
			Team: team.Team{Canonical: "main"},
		},
	}
	pr.EXPECT().FilterAll(gomock.Any()).Return(pps, nil)

	br.EXPECT().FindReadyDownstreamVersion(
		gomock.Any(), "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1,
	).Return(uint32(42), true, nil)

	// Pin check — resource is not pinned
	rr.EXPECT().Find(gomock.Any(), "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{}, nil)

	// A pending build already exists — should skip creating another
	br.EXPECT().FindOldestPending(gomock.Any(), "main", "my-pipeline", "deploy").
		Return(&build.Build{ID: 99, BuildNumber: "1", Status: build.Pending}, nil)

	// topic.Send and builds.Create should NOT be called
	s.tick(context.Background())
}

func TestTickJobs_SkipsWhenTriggerFalse(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, _, _, _ := newTestScheduler(ctrl)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)

	pps := []*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{
				Name: "my-pipeline", Canonical: "my-pipeline",
				Jobs: []job.Job{
					{
						Name: "downstream",
						Plan: []job.PlanStep{
							{
								Type: job.StepTypeGet,
								Get: &job.GetStep{
									Type:    "git",
									Name:    "repo",
									Passed:  []string{"upstream"},
									Trigger: false,
								},
							},
						},
					},
				},
			},
			Team: team.Team{Canonical: "main"},
		},
	}
	pr.EXPECT().FilterAll(gomock.Any()).Return(pps, nil)

	// FindReadyDownstreamVersion should NOT be called
	// topic.Send should NOT be called
	s.tick(context.Background())
}

func TestTickJobs_SkipsJobsWithoutPassedConstraints(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, _, _, _ := newTestScheduler(ctrl)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)

	pps := []*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{
				Name: "my-pipeline", Canonical: "my-pipeline",
				Jobs: []job.Job{
					{
						Name: "simple-job",
						Plan: []job.PlanStep{
							{
								Type: job.StepTypeGet,
								Get: &job.GetStep{
									Type:    "git",
									Name:    "repo",
									Trigger: true,
								},
							},
						},
					},
				},
			},
			Team: team.Team{Canonical: "main"},
		},
	}
	pr.EXPECT().FilterAll(gomock.Any()).Return(pps, nil)

	// No downstream checks or sends
	s.tick(context.Background())
}

func TestTickJobs_MultipleGetSteps_AllMustBeReady(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br, _, _ := newTestScheduler(ctrl)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)

	// Job with TWO get steps with passed constraints — both must be ready
	pps := []*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{
				Name: "my-pipeline", Canonical: "my-pipeline",
				Jobs: []job.Job{
					{
						Name: "deploy",
						Plan: []job.PlanStep{
							{
								Type: job.StepTypeGet,
								Get: &job.GetStep{
									Type:    "git",
									Name:    "repo",
									Passed:  []string{"lint"},
									Trigger: true,
								},
							},
							{
								Type: job.StepTypeGet,
								Get: &job.GetStep{
									Type:    "docker",
									Name:    "image",
									Passed:  []string{"build"},
									Trigger: true,
								},
							},
						},
					},
				},
			},
			Team: team.Team{Canonical: "main"},
		},
	}
	pr.EXPECT().FilterAll(gomock.Any()).Return(pps, nil)

	// First get step IS ready
	br.EXPECT().FindReadyDownstreamVersion(
		gomock.Any(), "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1,
	).Return(uint32(42), true, nil)

	// Pin check — resource is not pinned
	rr.EXPECT().Find(gomock.Any(), "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{}, nil)

	// Second get step is NOT ready
	br.EXPECT().FindReadyDownstreamVersion(
		gomock.Any(), "main", "my-pipeline",
		[]string{"build"}, "deploy", "image", 1,
	).Return(uint32(0), false, nil)

	// topic.Send should NOT be called — not all get steps are ready
	s.tick(context.Background())
}

func TestTickJobs_MultipleGetSteps_BothReady_TriggersOnce(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br, topic, _ := newTestScheduler(ctrl)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)

	pps := []*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{
				Name: "my-pipeline", Canonical: "my-pipeline",
				Jobs: []job.Job{
					{
						Name: "deploy",
						Plan: []job.PlanStep{
							{
								Type: job.StepTypeGet,
								Get: &job.GetStep{
									Type:    "git",
									Name:    "repo",
									Passed:  []string{"lint"},
									Trigger: true,
								},
							},
							{
								Type: job.StepTypeGet,
								Get: &job.GetStep{
									Type:    "docker",
									Name:    "image",
									Passed:  []string{"build"},
									Trigger: true,
								},
							},
						},
					},
				},
			},
			Team: team.Team{Canonical: "main"},
		},
	}
	pr.EXPECT().FilterAll(gomock.Any()).Return(pps, nil)

	br.EXPECT().FindReadyDownstreamVersion(
		gomock.Any(), "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1,
	).Return(uint32(42), true, nil)

	// Pin check — resource is not pinned
	rr.EXPECT().Find(gomock.Any(), "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{}, nil)

	br.EXPECT().FindReadyDownstreamVersion(
		gomock.Any(), "main", "my-pipeline",
		[]string{"build"}, "deploy", "image", 1,
	).Return(uint32(99), true, nil)

	// Pin check — resource is not pinned
	rr.EXPECT().Find(gomock.Any(), "main", "my-pipeline", "docker.image").
		Return(&resource.Resource{}, nil)

	// Check for existing pending build (none)
	br.EXPECT().FindOldestPending(gomock.Any(), "main", "my-pipeline", "deploy").
		Return(nil, nil)

	// Create a pending build
	br.EXPECT().Create(gomock.Any(), "main", "my-pipeline", "deploy", gomock.Any()).
		Return(uint32(100), "1", nil)

	// Should trigger exactly once
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, msg *pubsub.Message) error {
		var body queue.Body
		err := json.Unmarshal(msg.Body, &body)
		require.NoError(t, err)
		assert.Equal(t, "deploy", body.JobName)
		assert.Equal(t, uint32(42), body.VersionID) // first candidate's version
		assert.Equal(t, uint32(100), body.BuildID)
		return nil
	})

	s.tick(context.Background())
}

func TestTickJobs_SkipsWhenResourcePinnedToDifferentVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br, _, _ := newTestScheduler(ctrl)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)

	pps := []*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{
				Name: "my-pipeline", Canonical: "my-pipeline",
				Jobs: []job.Job{
					{
						Name: "deploy",
						Plan: []job.PlanStep{
							{
								Type: job.StepTypeGet,
								Get: &job.GetStep{
									Type:    "git",
									Name:    "repo",
									Passed:  []string{"lint"},
									Trigger: true,
								},
							},
						},
					},
				},
			},
			Team: team.Team{Canonical: "main"},
		},
	}
	pr.EXPECT().FilterAll(gomock.Any()).Return(pps, nil)

	// Version 42 is ready downstream
	br.EXPECT().FindReadyDownstreamVersion(
		gomock.Any(), "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1,
	).Return(uint32(42), true, nil)

	// But the resource is pinned to version 99 — should skip
	pinnedVersion := uint32(99)
	rr.EXPECT().Find(gomock.Any(), "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{PinnedVersionID: &pinnedVersion}, nil)

	// No build should be created, no message sent
	s.tick(context.Background())
}

func TestTickJobs_SkipsWhenPinCheckFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br, _, _ := newTestScheduler(ctrl)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)

	pps := []*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{
				Name: "my-pipeline", Canonical: "my-pipeline",
				Jobs: []job.Job{
					{
						Name: "deploy",
						Plan: []job.PlanStep{
							{
								Type: job.StepTypeGet,
								Get: &job.GetStep{
									Type:    "git",
									Name:    "repo",
									Passed:  []string{"lint"},
									Trigger: true,
								},
							},
						},
					},
				},
			},
			Team: team.Team{Canonical: "main"},
		},
	}
	pr.EXPECT().FilterAll(gomock.Any()).Return(pps, nil)

	br.EXPECT().FindReadyDownstreamVersion(
		gomock.Any(), "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1,
	).Return(uint32(42), true, nil)

	// Find fails — should skip the job (fail-closed)
	rr.EXPECT().Find(gomock.Any(), "main", "my-pipeline", "git.repo").
		Return(nil, assert.AnError)

	// No build should be created, no message sent
	s.tick(context.Background())
}

func TestTickJobs_FindReadyError_SkipsJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br, _, _ := newTestScheduler(ctrl)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)

	pps := []*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{
				Name: "my-pipeline", Canonical: "my-pipeline",
				Jobs: []job.Job{
					{
						Name: "downstream",
						Plan: []job.PlanStep{
							{
								Type: job.StepTypeGet,
								Get: &job.GetStep{
									Type:    "git",
									Name:    "repo",
									Passed:  []string{"upstream"},
									Trigger: true,
								},
							},
						},
					},
				},
			},
			Team: team.Team{Canonical: "main"},
		},
	}
	pr.EXPECT().FilterAll(gomock.Any()).Return(pps, nil)

	br.EXPECT().FindReadyDownstreamVersion(
		gomock.Any(), "main", "my-pipeline",
		[]string{"upstream"}, "downstream", "repo", 1,
	).Return(uint32(0), false, assert.AnError)

	// topic.Send should NOT be called
	s.tick(context.Background())
}
