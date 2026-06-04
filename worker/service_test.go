package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/queue"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/trigger"
	"github.com/pikoci/pikoci/pikoci/utils"
	"go.uber.org/mock/gomock"
	"gocloud.dev/pubsub"
)

func newTestWorker(ctrl *gomock.Controller) (*Worker, *mock.Service, *mock.Topic) {
	svc := mock.NewService(ctrl)
	jobTopic := mock.NewTopic(ctrl)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// InsertBuildGetVersion is called after every successful get step; allow it globally.
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// GetJobBuild is polled by the cancellation goroutine; return Started by default.
	svc.EXPECT().GetJobBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&build.Build{Status: build.Started}, nil).AnyTimes()

	// FindOldestPendingBuild is called by notifyNextPendingBuild at end of processJob.
	// Tests that call processJob must also call expectPendingBuild() to set up
	// the ordering lookup that happens at the start of processJob.
	// (No global fallback here — each test calls expectPendingBuild which
	// registers a DoAndReturn that returns the build once, then nil.)

	// CreateJobBuild is called by triggerResourceJobs to create pending builds.
	var createBuildCounter uint32
	svc.EXPECT().CreateJobBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string, b build.Build) (*build.Build, error) {
			createBuildCounter++
			return &build.Build{ID: createBuildCounter, BuildNumber: fmt.Sprintf("%d", createBuildCounter)}, nil
		}).AnyTimes()

	w := &Worker{
		pikoci:   svc,
		jobTopic: jobTopic,
		logger:   logger,
	}
	return w, svc, jobTopic
}

// expectPendingBuild sets up a FindOldestPendingBuild expectation using
// DoAndReturn: the first call returns a pending build with the given ID,
// and subsequent calls return nil (for notifyNextPendingBuild at the end
// of processJob). This must be called by any test that invokes processJob,
// since processJob queries the DB for the oldest pending build before
// starting it.
func expectPendingBuild(svc *mock.Service, buildID uint32) {
	var pendingCallCount int32
	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string) (*build.Build, error) {
			pendingCallCount++
			if pendingCallCount == 1 {
				return &build.Build{ID: buildID, BuildNumber: "1", Status: build.Pending}, nil
			}
			return nil, nil
		}).AnyTimes()
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

	expectPendingBuild(svc, 10)
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
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, false, nil).AnyTimes()

	// running steps + after get step + after task step + after marking succeeded
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		Return(nil).AnyTimes()

	expectPendingBuild(svc, 10)
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
	var pendingCallCount int32
	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string) (*build.Build, error) {
			pendingCallCount++
			if pendingCallCount == 1 {
				return &build.Build{ID: 10, BuildNumber: "1", Status: build.Pending}, nil
			}
			return nil, nil
		}).AnyTimes()
	w := &Worker{pikoci: svc, jobTopic: topic, logger: logger}

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
	svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "test-pipeline", "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, false, nil).AnyTimes()
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
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "upstream-job", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*build.Build{}, false, nil)

	// Build should be deleted (not failed)
	svc.EXPECT().DeleteJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "20").
		Return(nil)

	expectPendingBuild(svc, 10)
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
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "upstream-job", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*build.Build{{ID: 5, Status: build.Failed}}, false, nil)

	// Build should be deleted
	svc.EXPECT().DeleteJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "21").
		Return(nil)

	expectPendingBuild(svc, 10)
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

	expectPendingBuild(svc, 10)
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

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)
}

func TestProcessResourceCheck_NewVersions(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, topic := newTestWorker(ctrl)

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
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{}, false, nil).AnyTimes()

	// CreateResourceVersion for the new version found
	svc.EXPECT().CreateResourceVersion(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", gomock.Any()).
		Return(&resource.Version{ID: 1, Version: map[string]interface{}{"date": "now"}}, nil)

	// First check: jobs should be triggered
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, msg *pubsub.Message) error {
		var body queue.Body
		err := json.Unmarshal(msg.Body, &body)
		require.NoError(t, err)
		assert.Equal(t, "test-job", body.JobName)
		assert.Equal(t, "cron.my-cron", body.ResourceCanonical)
		assert.Equal(t, uint32(1), body.VersionID)
		return nil
	})

	w.processResourceCheck(ctx, m, cwd, pp)
}

func TestProcessResourceCheck_DuplicateVersionSkipped_FirstCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, topic := newTestWorker(ctrl)

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

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.my-repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{}, false, nil).AnyTimes()

	// First version: duplicate error (already exists)
	svc.EXPECT().CreateResourceVersion(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.my-repo", gomock.Any()).
		Return(nil, fmt.Errorf("failed to Create Resource Version: failed to execute query: constraint failed: UNIQUE constraint failed: resource_versions.resource_id, resource_versions.version (2067)"))

	// Second version: new, created successfully
	svc.EXPECT().CreateResourceVersion(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.my-repo", gomock.Any()).
		Return(&resource.Version{ID: 2, Version: map[string]interface{}{"ref": "new"}}, nil)

	// First check: jobs should be triggered for the non-duplicate version
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, msg *pubsub.Message) error {
		var body queue.Body
		err := json.Unmarshal(msg.Body, &body)
		require.NoError(t, err)
		assert.Equal(t, "git.my-repo", body.ResourceCanonical)
		assert.Equal(t, uint32(2), body.VersionID)
		return nil
	}).Times(2) // Two jobs: lint and test

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
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, false, nil).AnyTimes()

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

func TestProcessResourceCheckTrigger_FirstCheckTriggers(t *testing.T) {
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

	// No existing versions — first check
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "trigger.my-trigger", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{}, false, nil).AnyTimes()

	// Triggers exist
	svc.EXPECT().ListTriggersAfter(gomock.Any(), m.TeamCanonical, "trigger.my-trigger", uint32(0)).
		Return([]*trigger.Trigger{
			{ID: 1, Version: map[string]interface{}{"key": "val"}},
		}, nil)

	// CreateResourceVersion succeeds
	svc.EXPECT().CreateResourceVersion(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "trigger.my-trigger", gomock.Any()).
		Return(&resource.Version{ID: 1, Version: map[string]interface{}{"key": "val", "trigger_id": float64(1)}}, nil)

	// First check: jobs should be triggered
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, msg *pubsub.Message) error {
		var body queue.Body
		err := json.Unmarshal(msg.Body, &body)
		require.NoError(t, err)
		assert.Equal(t, "deploy", body.JobName)
		assert.Equal(t, "trigger.my-trigger", body.ResourceCanonical)
		assert.Equal(t, uint32(1), body.VersionID)
		return nil
	})

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
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "trigger.my-trigger", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"key": "old", "trigger_id": float64(1)}},
		}, false, nil)

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

	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "job-a", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*build.Build{{ID: 1, Status: build.Succeeded, Steps: []build.Step{
			{Type: "get", Name: "my-cron", VersionID: 5},
		}}}, false, nil)
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "job-b", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*build.Build{{ID: 2, Status: build.Succeeded, Steps: []build.Step{
			{Type: "get", Name: "my-cron", VersionID: 5},
		}}}, false, nil)

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
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "lint", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*build.Build{{ID: 10, Status: build.Succeeded, Steps: []build.Step{
			{Type: "get", Name: "my-repo", VersionID: 5},
		}}}, false, nil)
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "test", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*build.Build{{ID: 11, Status: build.Succeeded, Steps: []build.Step{
			{Type: "get", Name: "my-repo", VersionID: 6},
		}}}, false, nil)

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
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "lint", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*build.Build{
			{ID: 10, Status: build.Succeeded, Steps: []build.Step{
				{Type: "get", Name: "my-repo", VersionID: 5},
			}},
			{ID: 9, Status: build.Succeeded, Steps: []build.Step{
				{Type: "get", Name: "my-repo", VersionID: 3},
			}},
		}, false, nil)
	// test has builds with versions {5, 7}
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "test", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*build.Build{
			{ID: 12, Status: build.Succeeded, Steps: []build.Step{
				{Type: "get", Name: "my-repo", VersionID: 7},
			}},
			{ID: 11, Status: build.Succeeded, Steps: []build.Step{
				{Type: "get", Name: "my-repo", VersionID: 5},
			}},
		}, false, nil)

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

func TestCheckPassedConstraints_PutStepSatisfiesPassed(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "downstream-job",
		BuildID:           10,
	}
	b := build.Build{ID: 54, BuildNumber: "54"}
	j := &job.Job{
		Name: "downstream-job",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeGet,
				Get: &job.GetStep{
					Type:   "artifact",
					Name:   "my-artifact",
					Passed: []string{"upstream-job"},
				},
			},
		},
	}

	// The upstream job has a put step (not a get step) with the version
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "upstream-job", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*build.Build{{ID: 1, Status: build.Succeeded, Steps: []build.Step{
			{Type: "put", Name: "my-artifact", VersionID: 7},
		}}}, false, nil)

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j)
	assert.True(t, ok)
	assert.Equal(t, map[string]uint32{"artifact.my-artifact": 7}, resolved)
}

func TestImplicitGetAfterPut_CreatesVersionAndRecords(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "gen-job",
		BuildID:           10,
	}
	b := &build.Build{
		ID:          60,
		BuildNumber: "60",
		Steps: []build.Step{
			{Type: "put", Name: "my-artifact", Status: build.Succeeded, Logs: "some logs"},
		},
	}
	pp := &pipeline.Pipeline{
		ID:        1,
		Name:      "test-pipeline",
		Canonical: "test-pipeline",
		Jobs:      []job.Job{{ID: 1, Name: "gen-job"}},
	}
	p := job.PutStep{Type: "artifact", Name: "my-artifact"}
	rCan := "artifact.my-artifact"
	r := resource.Resource{ID: 1, Name: "my-artifact", Type: "artifact", Canonical: rCan}

	// Push script output with valid JSON version on last line
	out := "some tar output\n[{\"sha\":\"abc123\",\"timestamp\":\"20260603120000\"}]"

	svc.EXPECT().CreateResourceVersion(gomock.Any(), "main", "test-pipeline", rCan, gomock.Any()).
		Return(&resource.Version{ID: 42, Version: map[string]interface{}{"sha": "abc123", "timestamp": "20260603120000"}}, nil)
	svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "test-pipeline", "gen-job", "60", gomock.Any()).
		Return(nil).AnyTimes()

	w.implicitGetAfterPut(ctx, m, b, pp, p, rCan, r, out, 0)

	// Verify the step's VersionID was set
	assert.Equal(t, uint32(42), b.Steps[0].VersionID)
}

func TestImplicitGetAfterPut_InvalidJSON_Skips(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "gen-job",
		BuildID:           10,
	}
	b := &build.Build{
		ID:          61,
		BuildNumber: "61",
		Steps: []build.Step{
			{Type: "put", Name: "repo", Status: build.Succeeded},
		},
	}
	pp := &pipeline.Pipeline{ID: 1, Name: "test-pipeline", Canonical: "test-pipeline"}
	p := job.PutStep{Type: "git", Name: "repo"}
	r := resource.Resource{ID: 1, Name: "repo", Type: "git", Canonical: "git.repo"}

	// Push output is not JSON (e.g. git push output)
	out := "Everything up-to-date"

	// Should not call CreateResourceVersion or InsertBuildGetVersion
	w.implicitGetAfterPut(ctx, m, b, pp, p, "git.repo", r, out, 0)

	// VersionID should remain 0
	assert.Equal(t, uint32(0), b.Steps[0].VersionID)
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

	expectPendingBuild(svc, 10)
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

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 3, Version: map[string]interface{}{"ref": "abc"}},
			{ID: 5, Version: map[string]interface{}{"ref": "def"}},
		}, false, nil).AnyTimes()

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
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"ref": "old"}},
			{ID: 2, Version: map[string]interface{}{"ref": "latest"}},
		}, false, nil).AnyTimes()

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

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{}, false, nil).AnyTimes()

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

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.my-repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 5, Version: map[string]interface{}{"ref": "queue-ver"}},
			{ID: 10, Version: map[string]interface{}{"ref": "resolved-ver"}},
		}, false, nil).AnyTimes()

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
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{}, false, nil)

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

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, false, nil)

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

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_PutStep_CacheDirSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "artifact-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "artifact-job",
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
							Type:   "artifact",
							Name:   "my-artifact",
							Params: map[string]string{"dir": "output"},
						},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "my-artifact", Type: "artifact", Canonical: "artifact.my-artifact", Params: &resource.Params{Params: map[string]string{"dir": "output"}}},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "artifact",
				Cache:  true,
				Params: []string{"dir", "base_dir"},
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

	var capturedBuild *build.Build
	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 81, BuildNumber: "81", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "81", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = &b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	// The build should have completed successfully (task + put steps)
	require.NotNil(t, capturedBuild)
	assert.Equal(t, build.Succeeded, capturedBuild.Status)
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
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, false, nil).AnyTimes()

	// running steps + after get + after task + after put + after success
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "90", gomock.Any()).
		Return(nil).AnyTimes()

	expectPendingBuild(svc, 10)
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

	expectPendingBuild(svc, 10)
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
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, false, nil).AnyTimes()

	// running step + partial logs + failBuild
	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "101", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
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

	expectPendingBuild(svc, 10)
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

	expectPendingBuild(svc, 10)
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

	expectPendingBuild(svc, 10)
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

	expectPendingBuild(svc, 10)
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
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"ref": "old"}},
		}, false, nil).AnyTimes()

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

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{}, false, nil)

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
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.timer", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "old"}},
		}, false, nil)
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

func TestTriggerResourceJobs_SkipsWhenResourcePinned(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	ctx := context.Background()

	pinnedVersion := uint32(99)
	r := resource.Resource{
		ID:              1,
		Name:            "my-repo",
		Type:            "git",
		Canonical:       "git.my-repo",
		PinnedVersionID: &pinnedVersion,
	}
	// New version discovered (ID=42) doesn't match pinned version (99)
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
		},
	}

	m := queue.Body{
		TeamCanonical:     "tc",
		PipelineCanonical: "test-pipeline",
	}

	// topic.Send should NOT be called — resource is pinned to a different version
	w.triggerResourceJobs(ctx, m, pp, r, cv)
}

func TestTriggerResourceJobs_TriggersWhenPinnedVersionMatches(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _, topic := newTestWorker(ctrl)

	ctx := context.Background()

	pinnedVersion := uint32(42)
	r := resource.Resource{
		ID:              1,
		Name:            "my-repo",
		Type:            "git",
		Canonical:       "git.my-repo",
		PinnedVersionID: &pinnedVersion,
	}
	// New version matches pinned version
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
		},
	}

	m := queue.Body{
		TeamCanonical:     "tc",
		PipelineCanonical: "test-pipeline",
	}

	// topic.Send SHOULD be called — pinned version matches
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil)

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

	expectPendingBuild(svc, 10)
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

	expectPendingBuild(svc, 10)
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

	expectPendingBuild(svc, 10)
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

	expectPendingBuild(svc, 10)
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

	expectPendingBuild(svc, 10)
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
	var pendingCallCount int32
	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string) (*build.Build, error) {
			pendingCallCount++
			if pendingCallCount == 1 {
				return &build.Build{ID: 10, BuildNumber: "1", Status: build.Pending}, nil
			}
			return nil, nil
		}).AnyTimes()

	w := &Worker{
		pikoci: svc,
		jobTopic: topic,
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

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 42, Version: map[string]interface{}{"date": "now"}},
		}, false, nil).AnyTimes()

	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "3.1", gomock.Any()).
		Return(nil).AnyTimes()

	expectPendingBuild(svc, 20)
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

	expectPendingBuild(svc, 20)
	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_Cancellation_RunsOnCancelNotOnFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	topic := mock.NewTopic(ctrl)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	var pendingCallCount int32
	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string) (*build.Build, error) {
			pendingCallCount++
			if pendingCallCount == 1 {
				return &build.Build{ID: 10, BuildNumber: "1", Status: build.Pending}, nil
			}
			return nil, nil
		}).AnyTimes()

	w := &Worker{
		pikoci: svc,
		jobTopic: topic,
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

func TestProcessJob_Cancellation_NoUpdateLoopAfterCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	topic := mock.NewTopic(ctrl)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	var pendingCallCount int32
	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string) (*build.Build, error) {
			atomic.AddInt32(&pendingCallCount, 1)
			if atomic.LoadInt32(&pendingCallCount) == 1 {
				return &build.Build{ID: 10, BuildNumber: "1", Status: build.Pending}, nil
			}
			return nil, nil
		}).AnyTimes()

	w := &Worker{
		pikoci:   svc,
		jobTopic: topic,
		logger:   logger,
	}

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "cancel-loop-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "cancel-loop-job",
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
				OnCancel: []job.HookStep{},
				Ensure:   []job.HookStep{},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 900, BuildNumber: "900", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// Return Cancelled immediately so the poll triggers cancellation fast
	svc.EXPECT().GetJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "900").
		Return(&build.Build{ID: 900, BuildNumber: "900", Status: build.Cancelled}, nil).AnyTimes()

	// Track UpdateJobBuild calls that happen with a cancelled context
	var cancelledCtxCalls int32
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "900", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			if ctx.Err() != nil {
				atomic.AddInt32(&cancelledCtxCalls, 1)
				return ctx.Err()
			}
			return nil
		}).AnyTimes()

	done := make(chan struct{})
	go func() {
		w.processJob(ctx, m, cwd, pp)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("processJob did not finish in time; likely stuck in update loop after cancellation")
	}

	// No UpdateJobBuild calls should have been made with a cancelled context
	assert.Equal(t, int32(0), atomic.LoadInt32(&cancelledCtxCalls),
		"UpdateJobBuild should not be called with a cancelled context")
}

func TestProcessJob_MissingBuildID(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		// BuildID intentionally 0
	}
	pp := testPipeline()
	cwd := t.TempDir()

	// FindOldestPendingBuild returns nil → no pending builds, processJob returns early.
	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, nil).AnyTimes()
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
	expectPendingBuild(svc, 10)
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

	expectPendingBuild(svc, 10)
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

func TestDrain_UnblocksReceiveImmediately(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	jobSub := mock.NewSubscription(ctrl)
	checkSub := mock.NewSubscription(ctrl)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	w := &Worker{
		pikoci:            svc,
		jobSubscription:   jobSub,
		checkSubscription: checkSub,
		logger:            logger,
	}

	// Receive blocks until the context is cancelled (simulating waiting for messages)
	jobSub.EXPECT().Receive(gomock.Any()).DoAndReturn(func(ctx context.Context) (*pubsub.Message, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}).AnyTimes()
	checkSub.EXPECT().Receive(gomock.Any()).DoAndReturn(func(ctx context.Context) (*pubsub.Message, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}).AnyTimes()

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	// Give Run() time to start the receive loops
	time.Sleep(50 * time.Millisecond)

	w.Drain()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Drain() did not unblock Run() within 2 seconds")
	}
}

// Silence the unused import warnings
var _ = time.Now

func TestApplyRunnerOverride_NilOverride(t *testing.T) {
	pp := &pipeline.Pipeline{
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	typeCmd := &utils.RunnerCommand{
		Runner: "exec",
		Args:   []string{"-ec", "echo hello"},
		Params: map[string]string{"path": "/bin/sh"},
	}

	ru, rc, ok := applyRunnerOverride(pp, typeCmd, nil)
	require.True(t, ok)
	assert.Equal(t, "exec", ru.Name)
	assert.Equal(t, "exec", rc.Runner)
	assert.Equal(t, "/bin/sh", rc.Params["path"])
	assert.Equal(t, []string{"-ec", "echo hello"}, rc.Args)
}

func TestApplyRunnerOverride_ExecToDocker(t *testing.T) {
	pp := &pipeline.Pipeline{
		Runners: []runner.Runner{
			{Name: "docker", Run: utils.RunCommand{
				Path: "docker",
				Args: []string{"run", "--rm", "-v", "$WORKDIR:/workdir", "$args", "$image", "/bin/sh", "-ec", "$cmd"},
			}},
		},
	}
	typeCmd := &utils.RunnerCommand{
		Runner: "exec",
		Args:   []string{"-ec", "curl http://example.com"},
		Params: map[string]string{"path": "/bin/sh"},
	}
	override := &utils.RunnerOverride{
		Runner: "docker",
		Params: map[string]string{"image": "curlimages/curl:latest"},
	}

	ru, rc, ok := applyRunnerOverride(pp, typeCmd, override)
	require.True(t, ok)
	assert.Equal(t, "docker", ru.Name)
	assert.Equal(t, "docker", rc.Runner)
	assert.Equal(t, "curlimages/curl:latest", rc.Params["image"])
	assert.Equal(t, "/bin/sh -ec curl http://example.com", rc.Params["cmd"])
	assert.Empty(t, rc.Params["path"]) // path should be deleted
	assert.Nil(t, rc.Args)            // no override args
}

func TestApplyRunnerOverride_ExecToDockerWithArgs(t *testing.T) {
	pp := &pipeline.Pipeline{
		Runners: []runner.Runner{
			{Name: "docker", Run: utils.RunCommand{
				Path: "docker",
				Args: []string{"run", "--rm", "$args", "$image", "/bin/sh", "-ec", "$cmd"},
			}},
		},
	}
	typeCmd := &utils.RunnerCommand{
		Runner: "exec",
		Params: map[string]string{"path": "/opt/check"},
	}
	override := &utils.RunnerOverride{
		Runner: "docker",
		Args:   []string{"-v", "/data:/data", "--privileged"},
		Params: map[string]string{"image": "alpine:latest"},
	}

	ru, rc, ok := applyRunnerOverride(pp, typeCmd, override)
	require.True(t, ok)
	assert.Equal(t, "docker", ru.Name)
	assert.Equal(t, "docker", rc.Runner)
	assert.Equal(t, "alpine:latest", rc.Params["image"])
	assert.Equal(t, "/opt/check", rc.Params["cmd"])
	assert.Equal(t, []string{"-v", "/data:/data", "--privileged"}, rc.Args)
}

func TestApplyRunnerOverride_NonExecIgnored(t *testing.T) {
	pp := &pipeline.Pipeline{
		Runners: []runner.Runner{
			{Name: "docker", Run: utils.RunCommand{Path: "docker", Args: []string{"run"}}},
		},
	}
	typeCmd := &utils.RunnerCommand{
		Runner: "docker",
		Params: map[string]string{"image": "golang:1.25", "cmd": "make test"},
	}
	override := &utils.RunnerOverride{
		Runner: "docker",
		Params: map[string]string{"image": "other:latest"},
	}

	ru, rc, ok := applyRunnerOverride(pp, typeCmd, override)
	require.True(t, ok)
	assert.Equal(t, "docker", ru.Name)
	// Override should NOT be applied since typeCmd is not exec
	assert.Equal(t, "golang:1.25", rc.Params["image"])
	assert.Equal(t, "make test", rc.Params["cmd"])
}

func TestApplyRunnerOverride_PathDeletedAfterMerge(t *testing.T) {
	// Simulates the full flow: applyRunnerOverride transforms path→cmd,
	// then caller merges params (which re-introduces path from typeCmd.Params),
	// then caller deletes path when override is active.
	pp := &pipeline.Pipeline{
		Runners: []runner.Runner{
			{Name: "docker", Run: utils.RunCommand{
				Path: "docker",
				Args: []string{"run", "--rm", "$args", "$image", "/bin/sh", "-ec", "$cmd"},
			}},
		},
	}
	typeCmd := &utils.RunnerCommand{
		Runner: "exec",
		Args:   []string{"-ec", "git clone $param_url"},
		Params: map[string]string{"path": "/bin/sh"},
	}
	override := &utils.RunnerOverride{
		Runner: "docker",
		Params: map[string]string{"image": "alpine/git:latest"},
	}

	_, rc, ok := applyRunnerOverride(pp, typeCmd, override)
	require.True(t, ok)

	// Simulate what buildPullParams does: copies typeCmd.Params (including path)
	externalParams := map[string]string{
		"path":      "/bin/sh",         // re-introduced from typeCmd.Params
		"param_url": "http://repo.git", // resource instance param
	}
	for k, v := range externalParams {
		rc.Params[k] = v
	}

	// This is what the call sites do after merge when override is active
	delete(rc.Params, "path")

	assert.Equal(t, "docker", rc.Runner)
	assert.Equal(t, "alpine/git:latest", rc.Params["image"])
	assert.Equal(t, "/bin/sh -ec git clone $param_url", rc.Params["cmd"])
	assert.Equal(t, "http://repo.git", rc.Params["param_url"])
	assert.Empty(t, rc.Params["path"], "path must not be present after override")
}

func TestApplyRunnerOverride_RunnerNotFound(t *testing.T) {
	pp := &pipeline.Pipeline{
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	typeCmd := &utils.RunnerCommand{
		Runner: "exec",
		Params: map[string]string{"path": "/bin/sh"},
	}
	override := &utils.RunnerOverride{
		Runner: "nonexistent",
		Params: map[string]string{"image": "foo"},
	}

	_, _, ok := applyRunnerOverride(pp, typeCmd, override)
	assert.False(t, ok, "should return false when override runner is not found")
}

func TestApplyRunnerOverride_ExecNilArgs(t *testing.T) {
	pp := &pipeline.Pipeline{
		Runners: []runner.Runner{
			{Name: "docker", Run: utils.RunCommand{
				Path: "docker",
				Args: []string{"run", "--rm", "$image", "/bin/sh", "-ec", "$cmd"},
			}},
		},
	}
	typeCmd := &utils.RunnerCommand{
		Runner: "exec",
		Params: map[string]string{"path": "/opt/check"},
	}
	override := &utils.RunnerOverride{
		Runner: "docker",
		Params: map[string]string{"image": "alpine:latest"},
	}

	_, rc, ok := applyRunnerOverride(pp, typeCmd, override)
	require.True(t, ok)
	assert.Equal(t, "/opt/check", rc.Params["cmd"])
	assert.Nil(t, rc.Args)
}

func TestResourceCacheEnabled(t *testing.T) {
	t.Run("type true, resource nil", func(t *testing.T) {
		rt := restype.ResourceType{Cache: true}
		r := resource.Resource{}
		assert.True(t, resourceCacheEnabled(rt, r))
	})

	t.Run("type false, resource nil", func(t *testing.T) {
		rt := restype.ResourceType{Cache: false}
		r := resource.Resource{}
		assert.False(t, resourceCacheEnabled(rt, r))
	})

	t.Run("type true, resource false", func(t *testing.T) {
		rt := restype.ResourceType{Cache: true}
		f := false
		r := resource.Resource{Cache: &f}
		assert.False(t, resourceCacheEnabled(rt, r))
	})

	t.Run("type false, resource true", func(t *testing.T) {
		rt := restype.ResourceType{Cache: false}
		tr := true
		r := resource.Resource{Cache: &tr}
		assert.True(t, resourceCacheEnabled(rt, r))
	})
}

func TestCacheDir(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	dir, err := w.cacheDir("myteam", "mypipeline", "git.myrepo")
	require.NoError(t, err)
	assert.Contains(t, dir, filepath.Join("pikoci", "cache", "myteam", "mypipeline", "git.myrepo"))

	// Verify the directory was actually created
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Clean up
	os.RemoveAll(dir)
}

func TestProcessJob_JobTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "job-timeout-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:      1,
				Name:    "job-timeout-job",
				Timeout: 2 * time.Second,
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
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

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "100", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	assert.Contains(t, capturedBuild.Error, "job timed out after 2s")
}

func TestProcessJob_JobTimeout_NotReached(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "job-timeout-ok",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:      1,
				Name:    "job-timeout-ok",
				Timeout: 10 * time.Second,
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "fast-task",
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
		Return(&build.Build{ID: 100, BuildNumber: "100", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "100", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
}

func TestPrepareShellRunner_CmdModeDefaultShell(t *testing.T) {
	ru := runner.Runner{
		Name: "shell",
		Run:  utils.RunCommand{Path: "$shell", Args: []string{"-ec", "$cmd"}},
	}
	rc := utils.RunnerCommand{
		Runner: "shell",
		Params: map[string]string{"cmd": "echo hello"},
	}
	cwd := t.TempDir()

	err := prepareShellRunner(&ru, &rc, cwd)
	require.NoError(t, err)
	assert.Equal(t, "/bin/sh", rc.Params["shell"])
	// Run command should remain unchanged (template handles cmd mode)
	assert.Equal(t, "$shell", ru.Run.Path)
	assert.Equal(t, []string{"-ec", "$cmd"}, ru.Run.Args)
}

func TestPrepareShellRunner_CmdModeCustomShell(t *testing.T) {
	ru := runner.Runner{
		Name: "shell",
		Run:  utils.RunCommand{Path: "$shell", Args: []string{"-ec", "$cmd"}},
	}
	rc := utils.RunnerCommand{
		Runner: "shell",
		Params: map[string]string{"cmd": "echo hello", "shell": "/bin/bash"},
	}
	cwd := t.TempDir()

	err := prepareShellRunner(&ru, &rc, cwd)
	require.NoError(t, err)
	assert.Equal(t, "/bin/bash", rc.Params["shell"])
}

func TestPrepareShellRunner_FileModeNoShell(t *testing.T) {
	cwd := t.TempDir()
	scriptPath := filepath.Join(cwd, "test.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hello\n"), 0644)

	ru := runner.Runner{
		Name: "shell",
		Run:  utils.RunCommand{Path: "$shell", Args: []string{"-ec", "$cmd"}},
	}
	rc := utils.RunnerCommand{
		Runner: "shell",
		Params: map[string]string{"file": "test.sh"},
	}

	err := prepareShellRunner(&ru, &rc, cwd)
	require.NoError(t, err)
	assert.Equal(t, scriptPath, ru.Run.Path)
	assert.Nil(t, ru.Run.Args)
	// file param should be removed from params
	_, hasFile := rc.Params["file"]
	assert.False(t, hasFile)
}

func TestPrepareShellRunner_FileModeWithShell(t *testing.T) {
	cwd := t.TempDir()
	scriptPath := filepath.Join(cwd, "test.sh")
	os.WriteFile(scriptPath, []byte("echo hello\n"), 0644)

	ru := runner.Runner{
		Name: "shell",
		Run:  utils.RunCommand{Path: "$shell", Args: []string{"-ec", "$cmd"}},
	}
	rc := utils.RunnerCommand{
		Runner: "shell",
		Params: map[string]string{"file": "test.sh", "shell": "/bin/bash"},
	}

	err := prepareShellRunner(&ru, &rc, cwd)
	require.NoError(t, err)
	assert.Equal(t, "/bin/bash", ru.Run.Path)
	assert.Equal(t, []string{scriptPath}, ru.Run.Args)
}

func TestPrepareShellRunner_FileModeAbsolutePath(t *testing.T) {
	cwd := t.TempDir()
	scriptPath := filepath.Join(cwd, "abs.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/sh\necho abs\n"), 0644)

	ru := runner.Runner{
		Name: "shell",
		Run:  utils.RunCommand{Path: "$shell", Args: []string{"-ec", "$cmd"}},
	}
	rc := utils.RunnerCommand{
		Runner: "shell",
		Params: map[string]string{"file": scriptPath},
	}

	err := prepareShellRunner(&ru, &rc, cwd)
	require.NoError(t, err)
	// Absolute path should be used as-is
	assert.Equal(t, scriptPath, ru.Run.Path)
}

func TestPrepareShellRunner_ErrorBothCmdAndFile(t *testing.T) {
	ru := runner.Runner{
		Name: "shell",
		Run:  utils.RunCommand{Path: "$shell", Args: []string{"-ec", "$cmd"}},
	}
	rc := utils.RunnerCommand{
		Runner: "shell",
		Params: map[string]string{"cmd": "echo hello", "file": "test.sh"},
	}

	err := prepareShellRunner(&ru, &rc, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot set both")
}

func TestPrepareShellRunner_ErrorNeitherCmdNorFile(t *testing.T) {
	ru := runner.Runner{
		Name: "shell",
		Run:  utils.RunCommand{Path: "$shell", Args: []string{"-ec", "$cmd"}},
	}
	rc := utils.RunnerCommand{
		Runner: "shell",
		Params: map[string]string{},
	}

	err := prepareShellRunner(&ru, &rc, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must set either")
}

func TestRunRunner_ShellCmdMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	ru := runner.Runner{
		Name: "shell",
		Run:  utils.RunCommand{Path: "$shell", Args: []string{"-ec", "$cmd"}},
	}
	rc := utils.RunnerCommand{
		Runner: "shell",
		Params: map[string]string{"cmd": "echo shell_works"},
	}

	out, _, err := w.runRunner(ctx, ru, cwd, rc)
	require.NoError(t, err)
	assert.Contains(t, out, "shell_works")
}

func TestRunRunner_ShellFileMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	scriptPath := filepath.Join(cwd, "hello.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/sh\necho file_works\n"), 0644)

	ru := runner.Runner{
		Name: "shell",
		Run:  utils.RunCommand{Path: "$shell", Args: []string{"-ec", "$cmd"}},
	}
	rc := utils.RunnerCommand{
		Runner: "shell",
		Params: map[string]string{"file": "hello.sh"},
	}

	out, _, err := w.runRunner(ctx, ru, cwd, rc)
	require.NoError(t, err)
	assert.Contains(t, out, "file_works")
}

func TestVersionValueToString(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want string
	}{
		{"string", "abc", "abc"},
		{"float64 int", float64(42), "42"},
		{"float64 decimal", 3.14, "3.14"},
		{"bool", true, "true"},
		{"nil", nil, ""},
		{"nested map", map[string]interface{}{"sha": "abc", "num": float64(1)}, ""},
		{"slice", []interface{}{"a", "b"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := versionValueToString(tt.in)
			if tt.name == "nested map" {
				// JSON encoding, order may vary
				assert.Contains(t, got, `"sha":"abc"`)
				assert.Contains(t, got, `"num":1`)
			} else if tt.name == "slice" {
				assert.Equal(t, `["a","b"]`, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestSanitizeStepName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"my-repo", "MY_REPO"},
		{"build.app", "BUILD_APP"},
		{"a-b.c_d", "A_B_C_D"},
		{"simple", "SIMPLE"},
		{"ALREADY_UPPER", "ALREADY_UPPER"},
		{"has spaces", "HAS_SPACES"},
		{"123num", "123NUM"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeStepName(tt.in))
		})
	}
}

func TestParseOutputFile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("missing file", func(t *testing.T) {
		result := parseOutputFile("/nonexistent/path", logger)
		assert.Empty(t, result)
	})

	t.Run("empty file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "out")
		require.NoError(t, os.WriteFile(f, []byte(""), 0644))
		result := parseOutputFile(f, logger)
		assert.Empty(t, result)
	})

	t.Run("valid lines", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "out")
		require.NoError(t, os.WriteFile(f, []byte("VERSION=1.2.3\nCOMMIT=abc"), 0644))
		result := parseOutputFile(f, logger)
		assert.Equal(t, map[string]string{"VERSION": "1.2.3", "COMMIT": "abc"}, result)
	})

	t.Run("equals in value", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "out")
		require.NoError(t, os.WriteFile(f, []byte("DATA=a=b=c"), 0644))
		result := parseOutputFile(f, logger)
		assert.Equal(t, map[string]string{"DATA": "a=b=c"}, result)
	})

	t.Run("comments and blank lines", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "out")
		require.NoError(t, os.WriteFile(f, []byte("# comment\n\nKEY=val\n  \n"), 0644))
		result := parseOutputFile(f, logger)
		assert.Equal(t, map[string]string{"KEY": "val"}, result)
	})

	t.Run("whitespace trimming", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "out")
		require.NoError(t, os.WriteFile(f, []byte("  MY_KEY  =  value  \n"), 0644))
		result := parseOutputFile(f, logger)
		assert.Equal(t, map[string]string{"MY_KEY": "value"}, result)
	})

	t.Run("key sanitization", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "out")
		require.NoError(t, os.WriteFile(f, []byte("my-key=val\n"), 0644))
		result := parseOutputFile(f, logger)
		assert.Equal(t, map[string]string{"MY_KEY": "val"}, result)
	})

	t.Run("file exceeds size limit", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "out")
		data := make([]byte, maxOutputFileSize+1)
		for i := range data {
			data[i] = 'x'
		}
		require.NoError(t, os.WriteFile(f, data, 0644))
		result := parseOutputFile(f, logger)
		assert.Empty(t, result)
	})
}

func TestProcessJob_GetStepExportsVersionMetadata(t *testing.T) {
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
	// The task step uses "printenv" to dump all env vars. We'll capture the
	// build step logs and check for the exported GET_* vars.
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
							Name: "check-env",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"-c", "printenv | grep GET_MY_CRON"}, Params: map[string]string{"path": "bash"}},
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
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "2024-01-01"}},
		}, false, nil).AnyTimes()

	var finalBuild *build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			bc := b
			finalBuild = &bc
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	// Verify that the task step received GET_MY_CRON_DATE env var.
	// The task step runs: bash -c "printenv | grep GET_MY_CRON"
	// If the env var is present, the output should contain it.
	require.NotNil(t, finalBuild)
	require.GreaterOrEqual(t, len(finalBuild.Steps), 2)
	taskStep := finalBuild.Steps[1]
	assert.Equal(t, build.Succeeded, taskStep.Status, "task step should succeed")
	assert.Contains(t, taskStep.Logs, "GET_MY_CRON_DATE=2024-01-01")
}

func TestProcessJob_TaskExportsPikoOutput(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
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
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "produce",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"-c", `echo "MY_VAR=hello123" >> "$PIKOCI_OUTPUT"`},
								Params: map[string]string{"path": "bash"},
							},
						},
					},
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "consume",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"-c", "printenv | grep TASK_PRODUCE_MY_VAR"},
								Params: map[string]string{"path": "bash"},
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

	var finalBuild *build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			bc := b
			finalBuild = &bc
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	require.NotNil(t, finalBuild)
	require.GreaterOrEqual(t, len(finalBuild.Steps), 2)
	consumeStep := finalBuild.Steps[1]
	assert.Equal(t, build.Succeeded, consumeStep.Status, "consume step should succeed")
	assert.Contains(t, consumeStep.Logs, "TASK_PRODUCE_MY_VAR=hello123")
}

func TestProcessJob_ExportedVarsAccumulate(t *testing.T) {
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
							Name: "build",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"-c", `echo "VERSION=1.0.0" >> "$PIKOCI_OUTPUT"`},
								Params: map[string]string{"path": "bash"},
							},
						},
					},
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "verify",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"-c", "printenv | grep -E '(GET_MY_CRON_|TASK_BUILD_)' | sort"},
								Params: map[string]string{"path": "bash"},
							},
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
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "2024-01-01"}},
		}, false, nil).AnyTimes()

	var finalBuild *build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			bc := b
			finalBuild = &bc
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	require.NotNil(t, finalBuild)
	require.GreaterOrEqual(t, len(finalBuild.Steps), 3)
	verifyStep := finalBuild.Steps[2]
	assert.Equal(t, build.Succeeded, verifyStep.Status, "verify step should succeed")
	assert.Contains(t, verifyStep.Logs, "GET_MY_CRON_DATE=2024-01-01")
	assert.Contains(t, verifyStep.Logs, "TASK_BUILD_VERSION=1.0.0")
}

func TestProcessJob_FailedStepDoesNotExport(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
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
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "failing",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"-c", `echo "LEAKED=secret" >> "$PIKOCI_OUTPUT"; exit 1`},
								Params: map[string]string{"path": "bash"},
							},
						},
					},
					{
						// This step should never run because the previous failed.
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "consume",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"-c", "printenv | grep TASK_FAILING"},
								Params: map[string]string{"path": "bash"},
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

	var finalBuild *build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			bc := b
			finalBuild = &bc
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	require.NotNil(t, finalBuild)
	assert.Equal(t, build.Failed, finalBuild.Status)
	// Only 1 step should have run (the failing one)
	require.Equal(t, 1, len(finalBuild.Steps))
	assert.Equal(t, build.Failed, finalBuild.Steps[0].Status)
}
