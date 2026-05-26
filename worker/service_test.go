package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xescugc/pikoci/pikoci"
	"github.com/xescugc/pikoci/pikoci/build"
	"github.com/xescugc/pikoci/pikoci/job"
	"github.com/xescugc/pikoci/pikoci/mock"
	"github.com/xescugc/pikoci/pikoci/pipeline"
	"github.com/xescugc/pikoci/pikoci/queue"
	"github.com/xescugc/pikoci/pikoci/resource"
	"github.com/xescugc/pikoci/pikoci/restype"
	"github.com/xescugc/pikoci/pikoci/runner"
	"github.com/xescugc/pikoci/pikoci/sectype"
	"github.com/xescugc/pikoci/pikoci/trigger"
	"github.com/xescugc/pikoci/pikoci/utils"
	"go.uber.org/mock/gomock"
	"gocloud.dev/pubsub"
)

func newTestWorker(ctrl *gomock.Controller) (*Worker, *mock.Service, *mock.Topic) {
	svc := mock.NewService(ctrl)
	topic := mock.NewTopic(ctrl)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// InsertBuildGetVersion is called after every successful get step; allow it globally.
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// GetJobBuild is polled by the cancellation goroutine; return Started by default.
	svc.EXPECT().GetJobBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&build.Build{Status: build.Started}, nil).AnyTimes()

	// FindOldestPendingBuild is called by notifyNextPendingBuild at end of processJob.
	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, nil).AnyTimes()

	// CreateJobBuild is called by triggerResourceJobs to create pending builds.
	var createBuildCounter uint32
	svc.EXPECT().CreateJobBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string, b build.Build) (*build.Build, error) {
			createBuildCounter++
			return &build.Build{ID: createBuildCounter, BuildNumber: fmt.Sprintf("%d", createBuildCounter)}, nil
		}).AnyTimes()

	w := &Worker{
		pikoci: svc,
		topic:  topic,
		logger: logger,
	}
	return w, svc, topic
}

func runnerHook(rc utils.RunnerCommand) job.HookStep {
	return job.HookStep{Type: job.StepTypeRunner, Runner: &rc}
}

func testPipeline() *pipeline.Pipeline {
	return &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "test-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "cron", Name: "my-cron", Trigger: true},
					},
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"hello"},
								Params: map[string]string{
									"path": "echo",
								},
							},
						},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{
				ID:        1,
				Name:      "my-cron",
				Type:      "cron",
				Canonical: "cron.my-cron",
				Params:    &resource.Params{},
			},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID:     1,
				Name:   "cron",
				Params: []string{},
				Check: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", `echo "[{\"date\":\"now\"}]"`},
					Params: map[string]string{
						"path": "/bin/sh",
					},
				},
				Pull: &utils.RunnerCommand{
					Runner: "exec",
					Params: map[string]string{},
				},
			},
		},
		Runners: []runner.Runner{
			{
				ID:   1,
				Name: "exec",
				Run: utils.RunCommand{
					Path: "$path",
					Args: []string{"$args"},
				},
			},
		},
	}
}

func TestProcessJob_Success_TaskOnly(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "echo-job",
		BuildID:           10,
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "echo-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"hello"},
								Params: map[string]string{
									"path": "echo",
								},
							},
						},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 10, BuildNumber: "10", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// running step + after task step + after marking succeeded
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_Success_WithGetAndTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		VersionID:         1,
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "test-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "cron", Name: "my-cron", Trigger: true},
					},
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"hello"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "my-cron", Type: "cron", Canonical: "cron.my-cron"},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "cron",
				Pull: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"pulling"},
					Params: map[string]string{"path": "echo"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 10, BuildNumber: "10", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron").
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, nil).AnyTimes()

	// running steps + after get step + after task step + after marking succeeded
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)
}

func TestInsertBuildGetVersion_CalledWithCorrectArgs(t *testing.T) {
	ctrl := gomock.NewController(t)
	// Don't use newTestWorker — we need precise control over InsertBuildGetVersion.
	svc := mock.NewService(ctrl)
	topic := mock.NewTopic(ctrl)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc.EXPECT().GetJobBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&build.Build{Status: build.Started}, nil).AnyTimes()
	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, nil).AnyTimes()
	w := &Worker{pikoci: svc, topic: topic, logger: logger}

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		VersionID:         1,
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "test-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "cron", Name: "my-cron", Trigger: true},
					},
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"hello"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "my-cron", Type: "cron", Canonical: "cron.my-cron"},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "cron",
				Pull: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"pulling"},
					Params: map[string]string{"path": "echo"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), "main", "test-pipeline", "test-job", m.BuildID).
		Return(&build.Build{ID: 10, BuildNumber: "10", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), "main", "test-pipeline", "test-job").
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "test-pipeline", "cron.my-cron").
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, nil).AnyTimes()
	svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "test-pipeline", "test-job", "10", gomock.Any()).
		Return(nil).AnyTimes()

	// Verify InsertBuildGetVersion is called with exact correct arguments
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), "main", "test-pipeline", "test-job", uint32(10), "my-cron", uint32(1)).
		Return(nil)

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_FailedPassedConstraint_NoBuilds(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "downstream-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   2,
				Name: "downstream-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get: &job.GetStep{
							Type:    "cron",
							Name:    "my-cron",
							Passed:  []string{"upstream-job"},
							Trigger: true,
						},
					},
				},
			},
		},
	}

	cwd := t.TempDir()
	createdBuild := &build.Build{ID: 20, BuildNumber: "20", StartedAt: time.Now()}

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(createdBuild, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// Passed check: upstream-job has no builds
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "upstream-job").
		Return([]*build.Build{}, nil)

	// Build should be deleted (not failed)
	svc.EXPECT().DeleteJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "20").
		Return(nil)

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_FailedPassedConstraint_NotSucceeded(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "downstream-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   2,
				Name: "downstream-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get: &job.GetStep{
							Type:    "cron",
							Name:    "my-cron",
							Passed:  []string{"upstream-job"},
							Trigger: true,
						},
					},
				},
			},
		},
	}

	cwd := t.TempDir()
	createdBuild := &build.Build{ID: 21, BuildNumber: "21", StartedAt: time.Now()}

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(createdBuild, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// Passed check: upstream-job has a failed build
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "upstream-job").
		Return([]*build.Build{{ID: 5, Status: build.Failed}}, nil)

	// Build should be deleted
	svc.EXPECT().DeleteJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "21").
		Return(nil)

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_TaskFailure_RunsHooks(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "failing-job",
		BuildID:           10,
		VersionID:         1,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "failing-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "will-fail",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Params: map[string]string{
									"path": "false", // exits with code 1
								},
							},
						},
						OnFailure: []job.HookStep{
							runnerHook(utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"task failed"},
								Params: map[string]string{
									"path": "echo",
								},
							}),
						},
					},
				},
				OnFailure: []job.HookStep{
					runnerHook(utils.RunnerCommand{
						Runner: "exec",
						Args:   []string{"job failed"},
						Params: map[string]string{
							"path": "echo",
						},
					}),
				},
				Ensure: []job.HookStep{
					runnerHook(utils.RunnerCommand{
						Runner: "exec",
						Args:   []string{"always runs"},
						Params: map[string]string{
							"path": "echo",
						},
					}),
				},
			},
		},
		Runners: []runner.Runner{
			{
				ID:   1,
				Name: "exec",
				Run: utils.RunCommand{
					Path: "$path",
					Args: []string{"$args"},
				},
			},
		},
	}

	cwd := t.TempDir()
	createdBuild := &build.Build{ID: 30, BuildNumber: "30", StartedAt: time.Now()}

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(createdBuild, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// running steps + failBuild + hooks
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "30", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_NoDownstreamTrigger(t *testing.T) {
	// Downstream triggering is now handled by the scheduler (pull-based).
	// This test verifies that the worker does NOT send downstream trigger messages.
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "upstream-job",
		BuildID:           10,
		VersionID:         1,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "upstream-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"hello"},
								Params: map[string]string{
									"path": "echo",
								},
							},
						},
					},
				},
			},
			{
				ID:   2,
				Name: "downstream-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get: &job.GetStep{
							Type:    "cron",
							Name:    "my-cron",
							Passed:  []string{"upstream-job"},
							Trigger: true,
						},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{
				ID:   1,
				Name: "exec",
				Run: utils.RunCommand{
					Path: "$path",
					Args: []string{"$args"},
				},
			},
		},
	}

	cwd := t.TempDir()
	createdBuild := &build.Build{ID: 40, BuildNumber: "40", StartedAt: time.Now()}

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(createdBuild, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// running step + after task step + after success mark
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "40", gomock.Any()).
		Return(nil).AnyTimes()

	// No topic.Send expected — downstream is now scheduler-driven

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessResourceCheck_NewVersions(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		ResourceCanonical: "cron.my-cron",
	}
	// Use a pipeline where the check command outputs valid JSON
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "test-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "cron", Name: "my-cron", Trigger: true},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "my-cron", Type: "cron", Canonical: "cron.my-cron"},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "cron",
				Check: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", `printf "[{\"date\":\"now\"}]\n"`},
					Params: map[string]string{
						"path": "/bin/sh",
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	// ListResourceVersions - no existing versions
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron").
		Return([]*resource.Version{}, nil).AnyTimes()

	// CreateResourceVersion for the new version found
	svc.EXPECT().CreateResourceVersion(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", gomock.Any()).
		Return(&resource.Version{ID: 1, Version: map[string]interface{}{"date": "now"}}, nil)

	// First check: versions are stored but NO jobs should be triggered
	// (topic.Send is NOT expected)

	w.processResourceCheck(ctx, m, cwd, pp)
}

func TestProcessResourceCheck_DuplicateVersionSkipped_FirstCheckNoTrigger(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		ResourceCanonical: "git.my-repo",
	}
	// Check script returns 2 versions: first already exists, second is new
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "lint",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "git", Name: "my-repo", Trigger: true},
					},
				},
			},
			{
				ID:   2,
				Name: "test",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "git", Name: "my-repo", Trigger: true},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "my-repo", Type: "git", Canonical: "git.my-repo"},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "git",
				Check: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", `printf '[{"ref":"old"},{"ref":"new"}]\n'`},
					Params: map[string]string{"path": "/bin/sh"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.my-repo").
		Return([]*resource.Version{}, nil).AnyTimes()

	// First version: duplicate error (already exists)
	svc.EXPECT().CreateResourceVersion(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.my-repo", gomock.Any()).
		Return(nil, fmt.Errorf("failed to Create Resource Version: failed to execute query: constraint failed: UNIQUE constraint failed: resource_versions.resource_id, resource_versions.version (2067)"))

	// Second version: new, created successfully
	svc.EXPECT().CreateResourceVersion(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.my-repo", gomock.Any()).
		Return(&resource.Version{ID: 2, Version: map[string]interface{}{"ref": "new"}}, nil)

	// First check: NO jobs should be triggered even for new versions
	// (topic.Send is NOT expected)

	w.processResourceCheck(ctx, m, cwd, pp)
}

func TestProcessResourceCheck_SecondCheckTriggers(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, topic := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		ResourceCanonical: "cron.my-cron",
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "test-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "cron", Name: "my-cron", Trigger: true},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "my-cron", Type: "cron", Canonical: "cron.my-cron"},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "cron",
				Check: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", `printf "[{\"date\":\"now2\"}]\n"`},
					Params: map[string]string{
						"path": "/bin/sh",
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	// Not a first check: existing versions present
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron").
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, nil).AnyTimes()

	// CreateResourceVersion for the new version
	svc.EXPECT().CreateResourceVersion(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", gomock.Any()).
		Return(&resource.Version{ID: 2, Version: map[string]interface{}{"date": "now2"}}, nil)

	// Second check: jobs SHOULD be triggered
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, msg *pubsub.Message) error {
		var body queue.Body
		err := json.Unmarshal(msg.Body, &body)
		require.NoError(t, err)
		assert.Equal(t, "test-job", body.JobName)
		assert.Equal(t, "cron.my-cron", body.ResourceCanonical)
		assert.Equal(t, uint32(2), body.VersionID)
		return nil
	})

	w.processResourceCheck(ctx, m, cwd, pp)
}

func TestProcessResourceCheckTrigger_FirstCheckNoTrigger(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		ResourceCanonical: "trigger.my-trigger",
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "deploy",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "trigger", Name: "my-trigger", Trigger: true},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "my-trigger", Type: "trigger", Canonical: "trigger.my-trigger"},
		},
		ResourceTypes: []restype.ResourceType{
			{ID: 1, Name: "trigger", Source: "pikoci://trigger"},
		},
	}

	// No existing versions — first check
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "trigger.my-trigger").
		Return([]*resource.Version{}, nil).AnyTimes()

	// Triggers exist
	svc.EXPECT().ListTriggersAfter(gomock.Any(), m.TeamCanonical, "trigger.my-trigger", uint32(0)).
		Return([]*trigger.Trigger{
			{ID: 1, Version: map[string]interface{}{"key": "val"}},
		}, nil)

	// CreateResourceVersion succeeds
	svc.EXPECT().CreateResourceVersion(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "trigger.my-trigger", gomock.Any()).
		Return(&resource.Version{ID: 1, Version: map[string]interface{}{"key": "val", "trigger_id": float64(1)}}, nil)

	// First check: NO jobs should be triggered (topic.Send is NOT expected)

	w.processResourceCheck(ctx, m, "", pp)
}

func TestProcessResourceCheckTrigger_SecondCheckTriggers(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, topic := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		ResourceCanonical: "trigger.my-trigger",
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "deploy",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "trigger", Name: "my-trigger", Trigger: true},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "my-trigger", Type: "trigger", Canonical: "trigger.my-trigger"},
		},
		ResourceTypes: []restype.ResourceType{
			{ID: 1, Name: "trigger", Source: "pikoci://trigger"},
		},
	}

	// Existing versions present — not a first check
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "trigger.my-trigger").
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"key": "old", "trigger_id": float64(1)}},
		}, nil)

	// New trigger after the existing one
	svc.EXPECT().ListTriggersAfter(gomock.Any(), m.TeamCanonical, "trigger.my-trigger", uint32(1)).
		Return([]*trigger.Trigger{
			{ID: 2, Version: map[string]interface{}{"key": "new"}},
		}, nil)

	// CreateResourceVersion succeeds
	svc.EXPECT().CreateResourceVersion(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "trigger.my-trigger", gomock.Any()).
		Return(&resource.Version{ID: 2, Version: map[string]interface{}{"key": "new", "trigger_id": float64(2)}}, nil)

	// Second check: jobs SHOULD be triggered
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, msg *pubsub.Message) error {
		var body queue.Body
		err := json.Unmarshal(msg.Body, &body)
		require.NoError(t, err)
		assert.Equal(t, "deploy", body.JobName)
		assert.Equal(t, "trigger.my-trigger", body.ResourceCanonical)
		assert.Equal(t, uint32(2), body.VersionID)
		return nil
	})

	w.processResourceCheck(ctx, m, "", pp)
}

func TestCheckPassedConstraints_AllPassed(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "downstream-job",
		BuildID:           10,
	}
	b := build.Build{ID: 50, BuildNumber: "50"}
	j := &job.Job{
		Name: "downstream-job",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeGet,
				Get: &job.GetStep{
					Type:   "cron",
					Name:   "my-cron",
					Passed: []string{"job-a", "job-b"},
				},
			},
		},
	}

	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "job-a").
		Return([]*build.Build{{ID: 1, Status: build.Succeeded, Steps: []build.Step{
			{Type: "get", Name: "my-cron", VersionID: 5},
		}}}, nil)
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "job-b").
		Return([]*build.Build{{ID: 2, Status: build.Succeeded, Steps: []build.Step{
			{Type: "get", Name: "my-cron", VersionID: 5},
		}}}, nil)

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j)
	assert.True(t, ok)
	assert.Equal(t, map[string]uint32{"cron.my-cron": 5}, resolved)
}

func TestCheckPassedConstraints_NoCommonVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "downstream-job",
		BuildID:           10,
	}
	b := build.Build{ID: 51, BuildNumber: "51"}
	j := &job.Job{
		Name: "downstream-job",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeGet,
				Get: &job.GetStep{
					Type:   "git",
					Name:   "my-repo",
					Passed: []string{"lint", "test"},
				},
			},
		},
	}

	// lint succeeded with version 5, test succeeded with version 6
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "lint").
		Return([]*build.Build{{ID: 10, Status: build.Succeeded, Steps: []build.Step{
			{Type: "get", Name: "my-repo", VersionID: 5},
		}}}, nil)
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "test").
		Return([]*build.Build{{ID: 11, Status: build.Succeeded, Steps: []build.Step{
			{Type: "get", Name: "my-repo", VersionID: 6},
		}}}, nil)

	// Build should be deleted
	svc.EXPECT().DeleteJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "51").
		Return(nil)

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j)
	assert.False(t, ok)
	assert.Nil(t, resolved)
}

func TestCheckPassedConstraints_PicksNewestCommon(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "deploy",
		BuildID:           10,
	}
	b := build.Build{ID: 52, BuildNumber: "52"}
	j := &job.Job{
		Name: "deploy",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeGet,
				Get: &job.GetStep{
					Type:   "git",
					Name:   "my-repo",
					Passed: []string{"lint", "test"},
				},
			},
		},
	}

	// lint has builds with versions {3, 5}
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "lint").
		Return([]*build.Build{
			{ID: 10, Status: build.Succeeded, Steps: []build.Step{
				{Type: "get", Name: "my-repo", VersionID: 5},
			}},
			{ID: 9, Status: build.Succeeded, Steps: []build.Step{
				{Type: "get", Name: "my-repo", VersionID: 3},
			}},
		}, nil)
	// test has builds with versions {5, 7}
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "test").
		Return([]*build.Build{
			{ID: 12, Status: build.Succeeded, Steps: []build.Step{
				{Type: "get", Name: "my-repo", VersionID: 7},
			}},
			{ID: 11, Status: build.Succeeded, Steps: []build.Step{
				{Type: "get", Name: "my-repo", VersionID: 5},
			}},
		}, nil)

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j)
	assert.True(t, ok)
	assert.Equal(t, map[string]uint32{"git.my-repo": 5}, resolved)
}

func TestCheckPassedConstraints_NoPassedField(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "simple-job",
		BuildID:           10,
	}
	b := build.Build{ID: 53, BuildNumber: "53"}
	j := &job.Job{
		Name: "simple-job",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeGet,
				Get: &job.GetStep{
					Type: "git",
					Name: "my-repo",
				},
			},
		},
	}

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j)
	assert.True(t, ok)
	assert.Equal(t, map[string]uint32{}, resolved)
}

func TestRunHooks(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}

	pp := testPipeline()
	cwd := t.TempDir()

	b := build.Build{
		ID:          60,
		BuildNumber: "60",
		Status:      build.Succeeded,
		Job:         []build.Step{},
	}

	hooks := []job.HookStep{
		runnerHook(utils.RunnerCommand{Runner: "exec", Args: []string{"hook1"}, Params: map[string]string{"path": "echo"}}),
		runnerHook(utils.RunnerCommand{Runner: "exec", Args: []string{"hook2"}, Params: map[string]string{"path": "echo"}}),
	}

	// 2 hooks × (running step + final step) = multiple UpdateJobBuild calls
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "60", gomock.Any()).
		Return(nil).AnyTimes()

	w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "task-name", hooks, "on_success", nil)

	require.Len(t, b.Job, 2)
	assert.Equal(t, "task-name:0:on_success", b.Job[0].Name)
	assert.Equal(t, "task-name:1:on_success", b.Job[1].Name)
}

func TestRunHooks_SingleHook_NoIndex(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}

	pp := testPipeline()
	cwd := t.TempDir()

	b := build.Build{ID: 61, BuildNumber: "61", Job: []build.Step{}}

	hooks := []job.HookStep{
		runnerHook(utils.RunnerCommand{Runner: "exec", Args: []string{"only"}, Params: map[string]string{"path": "echo"}}),
	}

	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "61", gomock.Any()).
		Return(nil).AnyTimes()

	w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "step", hooks, "ensure", nil)

	require.Len(t, b.Job, 1)
	assert.Equal(t, "step:ensure", b.Job[0].Name)
}

func TestRunHooks_JobLevel_NoStepName(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}

	pp := testPipeline()
	cwd := t.TempDir()

	b := build.Build{ID: 62, BuildNumber: "62", Job: []build.Step{}}

	hooks := []job.HookStep{
		runnerHook(utils.RunnerCommand{Runner: "exec", Args: []string{"job-level"}, Params: map[string]string{"path": "echo"}}),
	}

	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "62", gomock.Any()).
		Return(nil).AnyTimes()

	w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", hooks, "on_failure", nil)

	require.Len(t, b.Job, 1)
	assert.Equal(t, "on_failure", b.Job[0].Name)
}

func TestProcessMessage_JobDispatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}
	cwd := t.TempDir()

	// GetPipeline returns empty pipeline → StartPendingBuild will be called
	svc.EXPECT().GetPipeline(gomock.Any(), m.TeamCanonical, m.PipelineCanonical).
		Return(&pipeline.Pipeline{Name: "test-pipeline"}, nil)

	// StartPendingBuild
	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 1, BuildNumber: "1", StartedAt: time.Now()}, nil)

	// GetPipelineJob — no plan steps
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&job.Job{Name: "test-job"}, nil)

	// Succeeded → UpdateJobBuild
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "1", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			assert.Equal(t, build.Succeeded, b.Status)
			return nil
		})

	w.processMessage(ctx, m, cwd)
}

func TestBuildPullParams_WithVersionID(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		ResourceCanonical: "cron.my-cron",
		VersionID:         5,
	}
	b := build.Build{ID: 70, BuildNumber: "70"}

	rt := restype.ResourceType{
		Pull: &utils.RunnerCommand{
			Params: map[string]string{},
		},
		Params: []string{"url"},
	}
	r := resource.Resource{
		Canonical: "cron.my-cron",
		Params: &resource.Params{
			Params: map[string]string{"url": "http://example.com"},
		},
	}
	g := job.GetStep{}

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron").
		Return([]*resource.Version{
			{ID: 3, Version: map[string]interface{}{"ref": "abc"}},
			{ID: 5, Version: map[string]interface{}{"ref": "def"}},
		}, nil).AnyTimes()

	params, vid := w.buildPullParams(ctx, m, &b, rt, r, g, 0)
	require.NotNil(t, params)
	assert.Equal(t, "def", params["version_ref"])
	assert.Equal(t, "http://example.com", params["param_url"])
	assert.Equal(t, uint32(5), vid)
}

func TestBuildPullParams_NoVersionID_UsesLatest(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}
	b := build.Build{ID: 71, BuildNumber: "71"}

	rt := restype.ResourceType{
		Pull: &utils.RunnerCommand{
			Params: map[string]string{},
		},
	}
	r := resource.Resource{Canonical: "cron.my-cron"}
	g := job.GetStep{}

	// Returns versions ordered by ID desc — after Reverse, last becomes first
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron").
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"ref": "old"}},
			{ID: 2, Version: map[string]interface{}{"ref": "latest"}},
		}, nil).AnyTimes()

	params, vid := w.buildPullParams(ctx, m, &b, rt, r, g, 0)
	require.NotNil(t, params)
	assert.Equal(t, "latest", params["version_ref"])
	assert.Equal(t, uint32(2), vid)
}

func TestBuildPullParams_NoVersions_Fails(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}
	b := build.Build{ID: 72, BuildNumber: "72"}

	rt := restype.ResourceType{
		Pull: &utils.RunnerCommand{Params: map[string]string{}},
	}
	r := resource.Resource{Canonical: "cron.my-cron"}
	g := job.GetStep{}

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron").
		Return([]*resource.Version{}, nil).AnyTimes()

	// Should call failBuild
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "72", gomock.Any()).
		Return(nil)

	params, _ := w.buildPullParams(ctx, m, &b, rt, r, g, 0)
	assert.Nil(t, params)
}

func TestBuildPullParams_ResolvedVersionTakesPriority(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		VersionID:         5, // queue says version 5
	}
	b := build.Build{ID: 73, BuildNumber: "73"}

	rt := restype.ResourceType{
		Pull: &utils.RunnerCommand{Params: map[string]string{}},
	}
	r := resource.Resource{Canonical: "git.my-repo"}
	g := job.GetStep{}

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.my-repo").
		Return([]*resource.Version{
			{ID: 5, Version: map[string]interface{}{"ref": "queue-ver"}},
			{ID: 10, Version: map[string]interface{}{"ref": "resolved-ver"}},
		}, nil).AnyTimes()

	// resolvedVersionID=10 should take priority over m.VersionID=5
	params, vid := w.buildPullParams(ctx, m, &b, rt, r, g, 10)
	require.NotNil(t, params)
	assert.Equal(t, uint32(10), vid)
	assert.Equal(t, "resolved-ver", params["version_ref"])
}

func TestCheckVersionAvailability_NoVersions_DeletesBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}
	b := build.Build{ID: 99, BuildNumber: "99"}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Resources: []resource.Resource{
			{ID: 1, Name: "my-cron", Type: "cron", Canonical: "cron.my-cron"},
		},
	}
	j := &job.Job{
		Name: "test-job",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeGet,
				Get:  &job.GetStep{Type: "cron", Name: "my-cron"},
			},
			{
				Type: job.StepTypePut,
				Put:  &job.PutStep{Type: "notify", Name: "slack"},
			},
		},
	}

	// No versions available
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron").
		Return([]*resource.Version{}, nil)

	// Should delete build (not fail it)
	svc.EXPECT().DeleteJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "99").
		Return(nil)

	result := w.checkVersionAvailability(ctx, m, &b, j, pp)
	assert.False(t, result, "should return false when no versions available")
}

func TestCheckVersionAvailability_VersionExists_Passes(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}
	b := build.Build{ID: 99, BuildNumber: "99"}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Resources: []resource.Resource{
			{ID: 1, Name: "my-cron", Type: "cron", Canonical: "cron.my-cron"},
		},
	}
	j := &job.Job{
		Name: "test-job",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeGet,
				Get:  &job.GetStep{Type: "cron", Name: "my-cron"},
			},
		},
	}

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron").
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, nil)

	result := w.checkVersionAvailability(ctx, m, &b, j, pp)
	assert.True(t, result, "should return true when version exists")
}

func TestProcessJob_PutStep_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "deploy-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "deploy-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "build",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"building"},
								Params: map[string]string{"path": "echo"},
							},
						},
					},
					{
						Type: job.StepTypePut,
						Put: &job.PutStep{
							Type:   "git",
							Name:   "repo",
							Params: map[string]string{"tag": "latest"},
						},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "repo", Type: "git", Canonical: "git.repo", Params: &resource.Params{Params: map[string]string{"url": "http://example.com"}}},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "git",
				Params: []string{"url"},
				Push: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"pushing"},
					Params: map[string]string{"path": "echo"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 80, BuildNumber: "80", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// running steps + after task step + after put step + after marking succeeded
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "80", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_OrderedPlan_GetTaskPut(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "ordered-job",
		BuildID:           10,
		VersionID:         1,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "ordered-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "cron", Name: "my-cron", Trigger: true},
					},
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "build",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"building"}, Params: map[string]string{"path": "echo"}},
						},
					},
					{
						Type: job.StepTypePut,
						Put:  &job.PutStep{Type: "git", Name: "repo"},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "my-cron", Type: "cron", Canonical: "cron.my-cron"},
			{ID: 2, Name: "repo", Type: "git", Canonical: "git.repo"},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "cron",
				Pull: &utils.RunnerCommand{Runner: "exec", Args: []string{"pulling"}, Params: map[string]string{"path": "echo"}},
			},
			{
				ID: 2, Name: "git",
				Push: &utils.RunnerCommand{Runner: "exec", Args: []string{"pushing"}, Params: map[string]string{"path": "echo"}},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 90, BuildNumber: "90", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron").
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, nil).AnyTimes()

	// running steps + after get + after task + after put + after success
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "90", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_TaskTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "timeout-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "timeout-job",
				Plan: []job.PlanStep{
					{
						Type:    job.StepTypeTask,
						Timeout: 2 * time.Second,
						Task: &job.TaskStep{
							Name: "slow-task",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"10"},
								Params: map[string]string{
									"path": "sleep",
								},
							},
						},
						OnFailure: []job.HookStep{
							runnerHook(utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"task failed due to timeout"},
								Params: map[string]string{"path": "echo"},
							}),
						},
						Ensure: []job.HookStep{
							runnerHook(utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"ensure runs"},
								Params: map[string]string{"path": "echo"},
							}),
						},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 100, BuildNumber: "100", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// running step + partial logs + failBuild + on_failure hook running + on_failure hook final + ensure hook running + ensure hook final
	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "100", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	// The first step should contain the timeout message
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Contains(t, capturedBuild.Steps[0].Logs, "timed out after 2s")
}

func TestProcessJob_GetTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "get-timeout-job",
		BuildID:           10,
		VersionID:         1,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "get-timeout-job",
				Plan: []job.PlanStep{
					{
						Type:    job.StepTypeGet,
						Timeout: 1 * time.Second,
						Get:     &job.GetStep{Type: "cron", Name: "my-cron", Trigger: true},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "my-cron", Type: "cron", Canonical: "cron.my-cron"},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "cron",
				Pull: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"10"},
					Params: map[string]string{"path": "sleep"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 101, BuildNumber: "101", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron").
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, nil).AnyTimes()

	// running step + partial logs + failBuild
	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "101", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Contains(t, capturedBuild.Steps[0].Logs, "timed out after 1s")
}

func TestProcessJob_NoTimeout_Succeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "no-timeout-job",
		BuildID:           10,
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "no-timeout-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						// Timeout is zero (not set)
						Task: &job.TaskStep{
							Name: "echo",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"hello"},
								Params: map[string]string{"path": "echo"},
							},
						},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 102, BuildNumber: "102", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// running step + after task step + after marking succeeded
	var lastBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "102", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			lastBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, lastBuild.Status)
}

func TestProcessJob_TaskRetry_SucceedsOnSecondAttempt(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "retry-job",
		BuildID:           10,
	}

	// Script that fails on first run (no marker file) and succeeds on second (marker exists).
	script := fmt.Sprintf(`#!/bin/sh
if [ -f "%s/marker" ]; then
  echo "success"
  exit 0
else
  touch "%s/marker"
  echo "fail"
  exit 1
fi
`, cwd, cwd)
	scriptPath := cwd + "/retry.sh"
	os.WriteFile(scriptPath, []byte(script), 0755)

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "retry-job",
				Plan: []job.PlanStep{
					{
						Type:     job.StepTypeTask,
						Attempts: 2,
						Task: &job.TaskStep{
							Name: "flaky",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Params: map[string]string{"path": scriptPath},
							},
						},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 200, BuildNumber: "200", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// running step + after task step (success) + after marking succeeded
	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "200", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Contains(t, capturedBuild.Steps[0].Logs, "attempt 2/2")
	assert.Contains(t, capturedBuild.Steps[0].Logs, "success")
}

func TestProcessJob_TaskRetry_ExhaustsAttempts(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "exhaust-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "exhaust-job",
				Plan: []job.PlanStep{
					{
						Type:     job.StepTypeTask,
						Attempts: 2,
						Task: &job.TaskStep{
							Name: "always-fail",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Params: map[string]string{"path": "false"},
							},
						},
						OnFailure: []job.HookStep{
							runnerHook(utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"failed after retries"},
								Params: map[string]string{"path": "echo"},
							}),
						},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 201, BuildNumber: "201", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// running step + failBuild + on_failure hook running step + on_failure hook final
	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "201", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Contains(t, capturedBuild.Steps[0].Logs, "attempt 2/2")
}

func TestProcessJob_TaskRetry_WithTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "timeout-retry-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "timeout-retry-job",
				Plan: []job.PlanStep{
					{
						Type:     job.StepTypeTask,
						Attempts: 2,
						Timeout:  1 * time.Second,
						Task: &job.TaskStep{
							Name: "slow-task",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"10"},
								Params: map[string]string{"path": "sleep"},
							},
						},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 202, BuildNumber: "202", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// running step + partial logs + failBuild
	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "202", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	logs := capturedBuild.Steps[0].Logs
	assert.Contains(t, logs, "attempt 2/2")
	assert.Contains(t, logs, "timed out after 1s")
}

func TestReplaceSecretPlaceholders(t *testing.T) {
	params := map[string]string{
		"param_token": "__pikoci_secret:vault:secret/data/github:token__",
		"param_url":   "https://github.com/example/repo.git",
		"param_mixed": "prefix-__pikoci_secret:vault:secret/data/github:token__-suffix",
	}
	resolved := map[string]string{
		"__pikoci_secret:vault:secret/data/github:token__": "s3cret-token",
	}

	replaceSecretPlaceholders(params, resolved)

	assert.Equal(t, "s3cret-token", params["param_token"])
	assert.Equal(t, "https://github.com/example/repo.git", params["param_url"])
	assert.Equal(t, "prefix-s3cret-token-suffix", params["param_mixed"])
}

func TestReplaceSecretPlaceholders_NoResolved(t *testing.T) {
	params := map[string]string{
		"param_token": "literal-value",
	}

	replaceSecretPlaceholders(params, nil)
	assert.Equal(t, "literal-value", params["param_token"])
}

func TestProcessResourceCheck_WithSecretVars(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, topic := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		ResourceCanonical: "git.repo",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "test-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "git", Name: "repo", Trigger: true},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{
				ID: 1, Name: "repo", Type: "git", Canonical: "git.repo",
				Params: &resource.Params{
					Params: map[string]string{
						"url":   "https://github.com/example/repo.git",
						"token": "__pikoci_secret:vault:secret/data/github:token__",
					},
				},
			},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "git",
				Params: []string{"url", "token"},
				Check: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", `printf "[{\"ref\":\"abc123\"}]\n"`},
					Params: map[string]string{
						"path": "/bin/sh",
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
		SecretTypes: []sectype.SecretType{
			{
				Name: "vault",
				Get: utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", `printf '{"token":"resolved-secret-value"}\n'`},
					Params: map[string]string{"path": "/bin/sh"},
				},
				Config: map[string]string{"address": "http://vault:8200", "token": "my-token"},
			},
		},
		SecretVars: map[string]pipeline.VariableSecret{
			"git_token": {
				Type: "vault",
				Path: "secret/data/github",
				Key:  "token",
			},
		},
	}
	cwd := t.TempDir()

	// Existing versions present — not a first check
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.repo").
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"ref": "old"}},
		}, nil).AnyTimes()

	// CreateResourceVersion for the new version found
	svc.EXPECT().CreateResourceVersion(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.repo", gomock.Any()).
		Return(&resource.Version{ID: 2, Version: map[string]interface{}{"ref": "abc123"}}, nil)

	// Trigger the job
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil)

	w.processResourceCheck(ctx, m, cwd, pp)
}

func TestParseEnvFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:  "basic key=value",
			input: "DB_HOST=localhost\nDB_PORT=5432\n",
			expected: map[string]string{
				"DB_HOST": "localhost",
				"DB_PORT": "5432",
			},
		},
		{
			name:  "double quoted values stripped",
			input: `DB_USER="admin"`,
			expected: map[string]string{
				"DB_USER": "admin",
			},
		},
		{
			name:  "single quoted values stripped",
			input: `DB_PASS='s3cret'`,
			expected: map[string]string{
				"DB_PASS": "s3cret",
			},
		},
		{
			name:  "comments and blank lines ignored",
			input: "# comment\n\nKEY=value\n  \n",
			expected: map[string]string{
				"KEY": "value",
			},
		},
		{
			name:  "value containing equals sign",
			input: "CONN=host=db;port=5432\n",
			expected: map[string]string{
				"CONN": "host=db;port=5432",
			},
		},
		{
			name:     "empty input",
			input:    "",
			expected: map[string]string{},
		},
		{
			name:  "CRLF line endings",
			input: "KEY=value\r\n",
			expected: map[string]string{
				"KEY": "value",
			},
		},
		{
			name:  "invalid variable names skipped",
			input: "VALID=yes\n123BAD=no\n-also-bad=no\n",
			expected: map[string]string{
				"VALID": "yes",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseEnvFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProcessResourceCheck_SecretResolutionError_UpdatesResourceLogs(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		ResourceCanonical: "git.repo",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Resources: []resource.Resource{
			{
				ID: 1, Name: "repo", Type: "git", Canonical: "git.repo",
				Params: &resource.Params{
					Params: map[string]string{
						"url":   "https://github.com/example/repo.git",
						"token": "__pikoci_secret:vault:secret/bad:token__",
					},
				},
			},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "git",
				Params: []string{"url", "token"},
				Check: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", `echo "check"`},
					Params: map[string]string{"path": "/bin/sh"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
		SecretTypes: []sectype.SecretType{
			{
				Name: "vault",
				Get: utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", `exit 1`},
					Params: map[string]string{"path": "/bin/sh"},
				},
			},
		},
		SecretVars: map[string]pipeline.VariableSecret{
			"git_token": {Type: "vault", Path: "secret/bad", Key: "token"},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.repo").
		Return([]*resource.Version{}, nil)

	// Expect the resource to be updated with error logs
	svc.EXPECT().UpdatePipelineResource(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.repo", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string, r resource.Resource) error {
			assert.NotEmpty(t, r.Logs, "resource logs should contain the error")
			assert.Contains(t, r.Logs, "failed to resolve secrets")
			return nil
		})

	w.processResourceCheck(ctx, m, cwd, pp)
}

func TestRunRunner_ShellVariablesNotDestroyed(t *testing.T) {
	// Verifies that shell-local variables in command args are not
	// destroyed by os.Expand — they should pass through to the shell.
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	ru := runner.Runner{
		Name: "exec",
		Run:  utils.RunCommand{Path: "$path", Args: []string{"$args"}},
	}

	// This script sets a shell variable and uses it.
	// Before the fix, os.Expand would empty $MY_VAR.
	rc := utils.RunnerCommand{
		Runner: "exec",
		Args:   []string{"-ec", `MY_VAR="hello_from_shell"; echo "$MY_VAR"`},
		Params: map[string]string{"path": "/bin/sh"},
	}

	out, _, err := w.runRunner(ctx, ru, cwd, rc)
	require.NoError(t, err)
	assert.Contains(t, out, "hello_from_shell", "shell variable should survive and be echoed")
}

func TestRunRunner_AwkPositionalArgsWork(t *testing.T) {
	// Verifies that awk $1, $0 etc. work in command args.
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	ru := runner.Runner{
		Name: "exec",
		Run:  utils.RunCommand{Path: "$path", Args: []string{"$args"}},
	}

	rc := utils.RunnerCommand{
		Runner: "exec",
		Args:   []string{"-ec", `echo "foo bar" | awk '{print $1}'`},
		Params: map[string]string{"path": "/bin/sh"},
	}

	out, _, err := w.runRunner(ctx, ru, cwd, rc)
	require.NoError(t, err)
	assert.Contains(t, out, "foo", "awk $1 should extract first field")
	assert.NotContains(t, out, "bar", "awk $1 should not include second field")
}

func TestRunRunner_ParamVarsExpandedByShell(t *testing.T) {
	// Verifies that $param_* variables are available to the shell via env vars.
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	ru := runner.Runner{
		Name: "exec",
		Run:  utils.RunCommand{Path: "$path", Args: []string{"$args"}},
	}

	rc := utils.RunnerCommand{
		Runner: "exec",
		Args:   []string{"-ec", `echo "url=$param_url"`},
		Params: map[string]string{
			"path":      "/bin/sh",
			"param_url": "https://example.com",
		},
	}

	out, _, err := w.runRunner(ctx, ru, cwd, rc)
	require.NoError(t, err)
	assert.Contains(t, out, "url=https://example.com", "param_url should be expanded by shell from env")
}

func TestProcessResourceCheck_RawSecretFormat(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, topic := newTestWorker(ctrl)

	ctx := context.Background()

	// Create a temp PEM-like file
	tmpDir := t.TempDir()
	pemFile := tmpDir + "/test.pem"
	pemContent := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn\n-----END RSA PRIVATE KEY-----\n"
	os.WriteFile(pemFile, []byte(pemContent), 0644)

	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		ResourceCanonical: "cron.timer",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "test-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer", Trigger: true}},
				},
			},
		},
		Resources: []resource.Resource{
			{
				ID: 1, Name: "timer", Type: "cron", Canonical: "cron.timer",
				Params: &resource.Params{
					Params: map[string]string{
						"key": "__pikoci_secret:pem::content__",
					},
				},
			},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "cron",
				Params: []string{"key"},
				Check: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", `printf "[{\"date\":\"now\"}]\n"`},
					Params: map[string]string{"path": "/bin/sh"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
		SecretTypes: []sectype.SecretType{
			{
				Name: "pem",
				Get: utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", fmt.Sprintf(`cat "%s"`, pemFile)},
					Params: map[string]string{"path": "/bin/sh"},
				},
				Config: map[string]string{"format": "raw", "path": pemFile},
			},
		},
		SecretVars: map[string]pipeline.VariableSecret{
			"app_key": {Type: "pem", Key: "content"},
		},
	}
	cwd := t.TempDir()

	// Existing versions present — not a first check
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.timer").
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "old"}},
		}, nil)
	svc.EXPECT().CreateResourceVersion(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.timer", gomock.Any()).
		Return(&resource.Version{ID: 2, Version: map[string]interface{}{"date": "now"}}, nil)
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil)

	w.processResourceCheck(ctx, m, cwd, pp)
}

func TestFetchSecrets_RawFormat(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	// Create a PEM-like file
	pemFile := cwd + "/key.pem"
	pemContent := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn\n-----END RSA PRIVATE KEY-----\n"
	os.WriteFile(pemFile, []byte(pemContent), 0644)

	pp := &pipeline.Pipeline{
		SecretTypes: []sectype.SecretType{
			{
				Name: "pem",
				Get: utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", fmt.Sprintf(`cat "%s"`, pemFile)},
					Params: map[string]string{"path": "/bin/sh"},
				},
				Config: map[string]string{"format": "raw", "path": pemFile},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}

	secrets := map[string]string{"pem": ""}
	result, err := w.fetchSecrets(ctx, cwd, pp, secrets)
	require.NoError(t, err)
	assert.Equal(t, pemContent, result["secret_content"], "raw format should return trimmed file content under 'content' key")
}

func TestTriggerResourceJobs_MultipleJobsSameResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _, topic := newTestWorker(ctrl)

	ctx := context.Background()

	r := resource.Resource{
		ID:        1,
		Name:      "my-repo",
		Type:      "git",
		Canonical: "git.my-repo",
	}
	cv := &resource.Version{ID: 42}

	pp := &pipeline.Pipeline{
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "lint",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "my-repo", Trigger: true}},
				},
			},
			{
				ID:   2,
				Name: "test-mock",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "my-repo", Trigger: true}},
				},
			},
			{
				ID:   3,
				Name: "test-integration",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "my-repo", Trigger: true}},
				},
			},
		},
	}

	m := queue.Body{
		TeamCanonical:     "tc",
		PipelineCanonical: "test-pipeline",
	}

	// Expect Send to be called 3 times, once for each job
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).Times(3).Return(nil)

	w.triggerResourceJobs(ctx, m, pp, r, cv)
}

func TestProcessJob_TaskInputMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "input-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "input-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name:   "build",
							Inputs: []string{"nonexistent/"},
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"hello"},
								Params: map[string]string{
									"path": "echo",
								},
							},
						},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 200, BuildNumber: "200", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "200", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Contains(t, capturedBuild.Steps[0].Logs, `input "nonexistent/" does not exist`)
}

func TestProcessJob_TaskOutputMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "output-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "output-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name:    "build",
							Outputs: []string{"missing-file"},
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"somefile"},
								Params: map[string]string{
									"path": "touch",
								},
							},
						},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 300, BuildNumber: "300", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "300", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Contains(t, capturedBuild.Steps[0].Logs, `task finished but output "missing-file" was not produced`)
}

func TestProcessJob_TaskInputsOutputs_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "io-job",
		BuildID:           10,
	}

	cwd := t.TempDir()

	// Create the input files that will be checked
	os.MkdirAll(fmt.Sprintf("%s/src", cwd), 0755)
	os.WriteFile(fmt.Sprintf("%s/src/main.go", cwd), []byte("package main"), 0644)

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "io-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name:    "build",
							Inputs:  []string{"src/"},
							Outputs: []string{"src/main.go"},
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"building"},
								Params: map[string]string{
									"path": "echo",
								},
							},
						},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 400, BuildNumber: "400", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "400", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, build.Succeeded, capturedBuild.Steps[0].Status)
}

func TestProcessJob_TaskMultipleInputs_FailsOnFirst(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "multi-input-job",
		BuildID:           10,
	}

	cwd := t.TempDir()

	// Create only the first input, second will be missing
	os.MkdirAll(fmt.Sprintf("%s/dir1", cwd), 0755)

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "multi-input-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name:   "build",
							Inputs: []string{"dir1/", "file2"},
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"hello"},
								Params: map[string]string{
									"path": "echo",
								},
							},
						},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 500, BuildNumber: "500", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "500", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	// Should fail on "file2" (first missing input), not "dir1/" which exists
	assert.Contains(t, capturedBuild.Steps[0].Logs, `input "file2" does not exist`)
	assert.NotContains(t, capturedBuild.Steps[0].Logs, `input "dir1/" does not exist`)
}

func TestProcessJob_TaskMultipleOutputs_FailsOnFirst(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "multi-output-job",
		BuildID:           10,
	}

	cwd := t.TempDir()

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "multi-output-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name:    "build",
							Outputs: []string{"somefile", "missing-output"},
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"somefile"},
								Params: map[string]string{
									"path": "touch",
								},
							},
						},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 600, BuildNumber: "600", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "600", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	// "somefile" was created by touch, so it should fail on "missing-output"
	assert.Contains(t, capturedBuild.Steps[0].Logs, `task finished but output "missing-output" was not produced`)
	assert.NotContains(t, capturedBuild.Steps[0].Logs, `task finished but output "somefile" was not produced`)
}

func TestProcessJob_Cancellation(t *testing.T) {
	ctrl := gomock.NewController(t)
	// Build a worker with custom GetJobBuild behavior (don't use newTestWorker's default).
	svc := mock.NewService(ctrl)
	topic := mock.NewTopic(ctrl)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, nil).AnyTimes()

	w := &Worker{
		pikoci: svc,
		topic:  topic,
		logger: logger,
	}

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "cancel-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "cancel-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "long-task",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"60"},
								Params: map[string]string{
									"path": "sleep",
								},
							},
						},
					},
				},
				OnFailure: []job.HookStep{},
				Ensure:    []job.HookStep{},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 700, BuildNumber: "700", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// GetJobBuild: return Started first, then Cancelled on subsequent calls
	callCount := 0
	svc.EXPECT().GetJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "700").
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string) (*build.Build, error) {
			callCount++
			if callCount <= 1 {
				return &build.Build{ID: 700, BuildNumber: "700", Status: build.Started}, nil
			}
			return &build.Build{ID: 700, BuildNumber: "700", Status: build.Cancelled}, nil
		}).AnyTimes()

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "700", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	done := make(chan struct{})
	go func() {
		w.processJob(ctx, m, cwd, pp)
		close(done)
	}()

	select {
	case <-done:
		// Job completed (should be fast due to cancellation, not 60s)
	case <-time.After(30 * time.Second):
		t.Fatal("processJob did not finish in time; cancellation may not be working")
	}

	assert.Equal(t, build.Cancelled, capturedBuild.Status)
}

func TestProcessJob_Retry_UsesCreateRetryAndResolvedVersions(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           20,
		RetryBuildNumber:  "3",
		RetryBuildID:      5,
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "test-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "cron", Name: "my-cron", Trigger: true},
					},
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"hello"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "my-cron", Type: "cron", Canonical: "cron.my-cron"},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "cron",
				Pull: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"pulling"},
					Params: map[string]string{"path": "echo"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	// Worker now calls StartPendingBuild (build was created pending by the service)
	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 20, BuildNumber: "3.1", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// Should look up versions from the original build
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, uint32(5)).
		Return(map[string]uint32{"my-cron": 42}, nil)

	// ListResourceVersions should NOT be called (retries skip version availability check)
	// The get step still runs with the resolved version

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron").
		Return([]*resource.Version{
			{ID: 42, Version: map[string]interface{}{"date": "now"}},
		}, nil).AnyTimes()

	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "3.1", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_Retry_FailsOnVersionLookupError(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           20,
		RetryBuildNumber:  "3",
		RetryBuildID:      5,
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "test-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "cron", Name: "my-cron"},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 20, BuildNumber: "3.1", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, uint32(5)).
		Return(nil, fmt.Errorf("db error"))

	// Should fail the build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "3.1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			assert.Equal(t, build.Failed, b.Status)
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_Cancellation_RunsOnCancelNotOnFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	topic := mock.NewTopic(ctrl)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, nil).AnyTimes()

	w := &Worker{
		pikoci: svc,
		topic:  topic,
		logger: logger,
	}

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "cancel-hook-job",
		BuildID:           10,
	}

	cancelMarker := filepath.Join(t.TempDir(), "on_cancel_ran")
	failureMarker := filepath.Join(t.TempDir(), "on_failure_ran")

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "cancel-hook-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "long-task",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"60"},
								Params: map[string]string{"path": "sleep"},
							},
						},
					},
				},
				OnCancel: []job.HookStep{
					{
						Type: job.StepTypeRunner,
						Runner: &utils.RunnerCommand{
							Runner: "exec",
							Args:   []string{"-ec", fmt.Sprintf("touch %s", cancelMarker)},
							Params: map[string]string{"path": "/bin/sh"},
						},
					},
				},
				OnFailure: []job.HookStep{
					{
						Type: job.StepTypeRunner,
						Runner: &utils.RunnerCommand{
							Runner: "exec",
							Args:   []string{"-ec", fmt.Sprintf("touch %s", failureMarker)},
							Params: map[string]string{"path": "/bin/sh"},
						},
					},
				},
				Ensure: []job.HookStep{},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 800, BuildNumber: "800", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// GetJobBuild: return Started first, then Cancelled on subsequent calls
	callCount := 0
	svc.EXPECT().GetJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "800").
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string) (*build.Build, error) {
			callCount++
			if callCount <= 1 {
				return &build.Build{ID: 800, BuildNumber: "800", Status: build.Started}, nil
			}
			return &build.Build{ID: 800, BuildNumber: "800", Status: build.Cancelled}, nil
		}).AnyTimes()

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "800", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	done := make(chan struct{})
	go func() {
		w.processJob(ctx, m, cwd, pp)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("processJob did not finish in time")
	}

	assert.Equal(t, build.Cancelled, capturedBuild.Status)

	// on_cancel hook should have run
	_, err := os.Stat(cancelMarker)
	assert.NoError(t, err, "on_cancel hook should have run (marker file should exist)")

	// on_failure hook should NOT have run
	_, err = os.Stat(failureMarker)
	assert.True(t, os.IsNotExist(err), "on_failure hook should NOT run on cancellation (marker file should not exist)")
}

func TestProcessJob_MissingBuildID(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		// BuildID intentionally 0
	}
	pp := testPipeline()
	cwd := t.TempDir()

	// Should return immediately without calling any service methods
	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_BuildNotPending(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}
	pp := testPipeline()
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(nil, pikoci.ErrBuildNotPending)

	// Should return immediately — build already started by another worker
	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_ConcurrencyLimit_Requeues(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, topic := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}
	pp := testPipeline()
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(nil, pikoci.ErrConcurrencyLimit)

	// Should re-queue the message
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, msg *pubsub.Message) error {
		var body queue.Body
		err := json.Unmarshal(msg.Body, &body)
		require.NoError(t, err)
		assert.Equal(t, m.BuildID, body.BuildID)
		assert.Equal(t, m.JobName, body.JobName)
		return nil
	})

	w.processJob(ctx, m, cwd, pp)
}

func TestRunGetStepLocal_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	dir := t.TempDir()
	// Create a local resource directory
	resDir := filepath.Join(dir, "my-resource")
	require.NoError(t, os.MkdirAll(resDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(resDir, "test.txt"), []byte("content"), 0644))

	cwd := filepath.Join(dir, "workdir")
	require.NoError(t, os.MkdirAll(cwd, 0755))

	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}
	b := build.Build{
		ID:          1,
		BuildNumber: "1",
		Status:      build.Started,
		Steps:       []build.Step{},
	}
	g := job.GetStep{Type: "git", Name: "my-repo"}
	ps := job.PlanStep{Type: job.StepTypeGet, Get: &g}

	svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "test-pipeline", "test-job", "1", gomock.Any()).Return(nil).AnyTimes()

	failed := w.runGetStepLocal(context.Background(), m, &b, cwd, nil, g, ps, resDir)
	assert.False(t, failed)
	assert.Equal(t, 1, len(b.Steps))
	assert.Equal(t, build.Succeeded, b.Steps[0].Status)
	assert.Contains(t, b.Steps[0].Logs, "using local resource override")

	// Verify directory was copied
	dst := filepath.Join(cwd, "my-repo")
	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "expected directory")
	// Verify file contents were copied
	content, err := os.ReadFile(filepath.Join(dst, "test.txt"))
	require.NoError(t, err)
	assert.Equal(t, "content", string(content))
}

func TestRunGetStepLocal_MissingPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	dir := t.TempDir()
	cwd := filepath.Join(dir, "workdir")
	require.NoError(t, os.MkdirAll(cwd, 0755))

	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}
	b := build.Build{
		ID:          1,
		BuildNumber: "1",
		Status:      build.Started,
		Steps:       []build.Step{},
	}
	g := job.GetStep{Type: "git", Name: "my-repo"}
	ps := job.PlanStep{Type: job.StepTypeGet, Get: &g}

	svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "test-pipeline", "test-job", "1", gomock.Any()).Return(nil).AnyTimes()

	missingPath := filepath.Join(dir, "does-not-exist")
	failed := w.runGetStepLocal(context.Background(), m, &b, cwd, nil, g, ps, missingPath)
	assert.True(t, failed)
	assert.Equal(t, 1, len(b.Steps))
	assert.Equal(t, build.Failed, b.Steps[0].Status)
	assert.Contains(t, b.Steps[0].Logs, "does not exist")
}

// Silence the unused import warnings
var _ = time.Now
