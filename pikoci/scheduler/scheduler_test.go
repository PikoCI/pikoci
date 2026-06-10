package scheduler

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/team"
	"go.uber.org/mock/gomock"
)

func newTestScheduler(ctrl *gomock.Controller) (*Scheduler, *mock.ResourceRepository, *mock.PipelineRepository, *mock.BuildRepository) {
	rr := mock.NewResourceRepository(ctrl)
	pr := mock.NewPipelineRepository(ctrl)
	br := mock.NewBuildRepository(ctrl)
	wn := notifier.New()
	logger := slog.Default()
	s := New(rr, pr, br, wn, logger)
	return s, rr, pr, br
}

// expectEmptyTickJobs sets up expectations for tickJobs when no pipelines exist.
func expectEmptyTickJobs(pr *mock.PipelineRepository) {
	pr.EXPECT().FilterAll(gomock.Any()).Return(nil, nil)
}

func TestTickResources_NoDueResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, _ := newTestScheduler(ctrl)

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil)
	expectEmptyTickJobs(pr)

	s.tick(context.Background())
}

func TestTickResources_ProcessesDueResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, _ := newTestScheduler(ctrl)

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
	s, rr, pr, _ := newTestScheduler(ctrl)

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
	s, rr, pr, _ := newTestScheduler(ctrl)

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
	s, rr, pr, _ := newTestScheduler(ctrl)
	s.interval = 50 * time.Millisecond

	rr.EXPECT().FilterDueResources(gomock.Any()).Return(nil, nil).AnyTimes()
	pr.EXPECT().FilterAll(gomock.Any()).Return(nil, nil).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	time.Sleep(150 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)
}

// --- tickJobs tests ---

func TestTickJobs_TriggersWhenCommonVersionExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br := newTestScheduler(ctrl)

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

	s.tick(context.Background())
}

func TestTickJobs_SkipsWhenNoCommonVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br := newTestScheduler(ctrl)

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

	s.tick(context.Background())
}

func TestTickJobs_SkipsWhenPendingBuildExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br := newTestScheduler(ctrl)

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

	s.tick(context.Background())
}

func TestTickJobs_SkipsWhenTriggerFalse(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, _ := newTestScheduler(ctrl)

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

	s.tick(context.Background())
}

func TestTickJobs_SkipsJobsWithoutPassedConstraints(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, _ := newTestScheduler(ctrl)

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

	s.tick(context.Background())
}

func TestTickJobs_MultipleGetSteps_AllMustBeReady(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br := newTestScheduler(ctrl)

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

	s.tick(context.Background())
}

func TestTickJobs_MultipleGetSteps_BothReady_TriggersOnce(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br := newTestScheduler(ctrl)

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

	rr.EXPECT().Find(gomock.Any(), "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{}, nil)

	br.EXPECT().FindReadyDownstreamVersion(
		gomock.Any(), "main", "my-pipeline",
		[]string{"build"}, "deploy", "image", 1,
	).Return(uint32(99), true, nil)

	rr.EXPECT().Find(gomock.Any(), "main", "my-pipeline", "docker.image").
		Return(&resource.Resource{}, nil)

	br.EXPECT().FindOldestPending(gomock.Any(), "main", "my-pipeline", "deploy").
		Return(nil, nil)

	br.EXPECT().Create(gomock.Any(), "main", "my-pipeline", "deploy", gomock.Any()).
		Return(uint32(100), "1", nil)

	s.tick(context.Background())
}

func TestTickJobs_SkipsWhenResourcePinnedToDifferentVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br := newTestScheduler(ctrl)

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

	pinnedVersion := uint32(99)
	rr.EXPECT().Find(gomock.Any(), "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{PinnedVersionID: &pinnedVersion}, nil)

	s.tick(context.Background())
}

func TestTickJobs_SkipsWhenPinCheckFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br := newTestScheduler(ctrl)

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

	rr.EXPECT().Find(gomock.Any(), "main", "my-pipeline", "git.repo").
		Return(nil, assert.AnError)

	s.tick(context.Background())
}

func TestTickJobs_FindReadyError_SkipsJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, rr, pr, br := newTestScheduler(ctrl)

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

	s.tick(context.Background())
}
