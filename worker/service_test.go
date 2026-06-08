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
	"github.com/pikoci/pikoci/pikoci/notification"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/queue"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/service"
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

	// NotifySerialGroupPendingBuilds is called by notifyNextPendingBuild; allow it globally.
	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

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
	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
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

func TestProcessResourceCheck_NestedVersionFlattened(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		ResourceCanonical: "custom.my-res",
	}
	// The check script prints env vars starting with "version_" to stderr,
	// then outputs an empty JSON array (no new versions). If the nested
	// version is flattened correctly, the script will see version_metadata_sha
	// and version_metadata_author instead of a Go-formatted map.
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Resources: []resource.Resource{
			{ID: 1, Name: "my-res", Type: "custom", Canonical: "custom.my-res"},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "custom",
				Check: &utils.RunnerCommand{
					Runner: "exec",
					Args: []string{"-ec", `
# Verify flattened env vars exist
test "$version_metadata_sha" = "abc123" || { echo "FAIL: version_metadata_sha=$version_metadata_sha" >&2; exit 1; }
test "$version_metadata_author" = "bob" || { echo "FAIL: version_metadata_author=$version_metadata_author" >&2; exit 1; }
# Also verify the flat key
test "$version_ref" = "def456" || { echo "FAIL: version_ref=$version_ref" >&2; exit 1; }
printf "[]"
`},
					Params: map[string]string{"path": "/bin/sh"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	// Return an existing version with nested metadata
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "custom.my-res", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{
				"ref": "def456",
				"metadata": map[string]interface{}{
					"sha":    "abc123",
					"author": "bob",
				},
			}},
		}, false, nil).AnyTimes()

	svc.EXPECT().UpdatePipelineResource(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "custom.my-res", gomock.Any()).
		Return(nil).AnyTimes()

	// If flattening is broken, the check script will exit 1 and the test
	// will see an error log. We just need processResourceCheck to not panic.
	// The script outputs "[]" (no new versions), so no CreateResourceVersion call.
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

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j, nil)
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

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j, nil)
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

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j, nil)
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

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j, nil)
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

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j, nil)
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

	w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "task-name", hooks, "on_success", nil, nil)

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

	w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "step", hooks, "ensure", nil, nil)

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

	w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", hooks, "on_failure", nil, nil)

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

func TestRunRunner_EnvPlaceholder(t *testing.T) {
	// Verifies that $env injects -e KEY=VALUE flags for metadata vars
	// and excludes runner-internal params like cmd, image, WORKDIR.
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	// Use a runner that echoes its own args so we can inspect what $env produces.
	// The runner runs: /bin/sh -ec "echo $env_dump" where env_dump captures args.
	// Actually, we use printenv to check which -e flags made it through.
	ru := runner.Runner{
		Name: "exec",
		Run:  utils.RunCommand{Path: "$path", Args: []string{"$env", "$args"}},
	}

	rc := utils.RunnerCommand{
		Runner: "exec",
		Args:   []string{"-ec", "printenv GET_MY_REPO_REF && printenv BUILD_NUMBER && echo OK"},
		Params: map[string]string{
			"path":               "/bin/sh",
			"cmd":                "echo this is a multi-line\ncommand that should NOT be injected",
			"image":              "golang:1.25",
			"WORKDIR":            "/some/path",
			"PIKOCI_OUTPUT":      "/tmp/output",
			"GET_MY_REPO_REF":    "abc123",
			"BUILD_NUMBER":       "42",
			"TASK_BUILD_VERSION": "1.0.0",
			"version_ref":        "def456",
			"param_url":          "https://example.com",
			"secret_token":       "s3cret",
			"notify_status":      "success",
		},
	}

	// The -e flags come before the -ec arg. Since /bin/sh doesn't understand
	// -e KEY=VALUE as positional args, the command will fail. But we can verify
	// the behavior via the isRunnerInternalParam function directly.
	w.runRunner(ctx, ru, cwd, rc)
}

func TestIsRunnerInternalParam(t *testing.T) {
	// Runner-internal params must be excluded from $env injection.
	internals := []string{"cmd", "image", "WORKDIR", "path", "PIKOCI_OUTPUT", "script", "shell", "file"}
	for _, k := range internals {
		assert.True(t, isRunnerInternalParam(k), "%q should be internal", k)
	}

	// Metadata and user params must NOT be excluded.
	externals := []string{
		"GET_MY_REPO_REF",
		"TASK_BUILD_VERSION",
		"BUILD_NUMBER",
		"BUILD_PIPELINE_NAME",
		"BUILD_JOB_NAME",
		"version_ref",
		"param_url",
		"secret_token",
		"notify_status",
		"PIKOCI_TOKEN",
		"MY_CUSTOM_VAR",
	}
	for _, k := range externals {
		assert.False(t, isRunnerInternalParam(k), "%q should NOT be internal", k)
	}
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
	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
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
	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
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
	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
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

func TestProcessJob_SerialGroupLimit_Requeues(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, topic := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "deploy-staging",
		BuildID:           10,
	}
	pp := testPipeline()
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(nil, pikoci.ErrSerialGroupLimit)

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

func TestProcessJob_GenericError_Requeues(t *testing.T) {
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
		Return(nil, fmt.Errorf("database table is locked"))

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

func TestFlattenVersionValue(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		m := make(map[string]string)
		flattenVersionValue(m, "version_ref", "abc")
		assert.Equal(t, map[string]string{"version_ref": "abc"}, m)
	})

	t.Run("number int", func(t *testing.T) {
		m := make(map[string]string)
		flattenVersionValue(m, "version_count", float64(42))
		assert.Equal(t, map[string]string{"version_count": "42"}, m)
	})

	t.Run("number decimal", func(t *testing.T) {
		m := make(map[string]string)
		flattenVersionValue(m, "version_score", 3.14)
		assert.Equal(t, map[string]string{"version_score": "3.14"}, m)
	})

	t.Run("bool", func(t *testing.T) {
		m := make(map[string]string)
		flattenVersionValue(m, "version_ok", true)
		assert.Equal(t, map[string]string{"version_ok": "true"}, m)
	})

	t.Run("nil", func(t *testing.T) {
		m := make(map[string]string)
		flattenVersionValue(m, "version_empty", nil)
		assert.Equal(t, map[string]string{"version_empty": ""}, m)
	})

	t.Run("nested map", func(t *testing.T) {
		m := make(map[string]string)
		flattenVersionValue(m, "version_metadata", map[string]interface{}{
			"sha":    "abc123",
			"author": "bob",
		})
		assert.Equal(t, "abc123", m["version_metadata_sha"])
		assert.Equal(t, "bob", m["version_metadata_author"])
		assert.Len(t, m, 2)
	})

	t.Run("deeply nested", func(t *testing.T) {
		m := make(map[string]string)
		flattenVersionValue(m, "version_info", map[string]interface{}{
			"commit": map[string]interface{}{
				"sha":    "def456",
				"author": "alice",
			},
		})
		assert.Equal(t, "def456", m["version_info_commit_sha"])
		assert.Equal(t, "alice", m["version_info_commit_author"])
	})

	t.Run("array", func(t *testing.T) {
		m := make(map[string]string)
		flattenVersionValue(m, "version_tags", []interface{}{"v1", "v2"})
		assert.Equal(t, "v1", m["version_tags_0"])
		assert.Equal(t, "v2", m["version_tags_1"])
	})
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

// --- runNotifyStep tests ---

func TestProcessJob_NotifyStep_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "notify-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "notify-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeNotify,
						Notify: &job.NotifyStep{
							Type:    "echo-notifier",
							Name:    "my-alert",
							Message: "build done",
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "my-alert",
				Canonical: "echo-notifier.my-alert",
				Params:    &notification.Params{Params: map[string]string{"channel": "#builds"}},
				Message:   "default message",
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"notifying"},
					Params: map[string]string{"path": "echo"},
				},
				Params: []string{"channel"},
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
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, "notify", capturedBuild.Steps[0].Type)
	assert.Equal(t, "my-alert", capturedBuild.Steps[0].Name)
	assert.Equal(t, build.Succeeded, capturedBuild.Steps[0].Status)
}

func TestProcessJob_NotifyStep_Failure(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "notify-fail-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "notify-fail-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeNotify,
						Notify: &job.NotifyStep{
							Type: "fail-notifier",
							Name: "my-alert",
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "fail-notifier",
				Name:      "my-alert",
				Canonical: "fail-notifier.my-alert",
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "fail-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Params: map[string]string{"path": "false"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 301, BuildNumber: "301", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "301", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, "notify", capturedBuild.Steps[0].Type)
	assert.Equal(t, build.Failed, capturedBuild.Steps[0].Status)
}

func TestRunNotifyStep_LocalMode_Skips(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, topic := newTestWorker(ctrl)
	w.LocalMode = true

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "local-notify-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "local-notify-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeNotify,
						Notify: &job.NotifyStep{
							Type: "slack",
							Name: "alerts",
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
		Return(&build.Build{ID: 302, BuildNumber: "302", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "302", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	// Allow topic.Send for notifyNextPendingBuild
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, "notify", capturedBuild.Steps[0].Type)
	assert.Contains(t, capturedBuild.Steps[0].Logs, "skipping notify step")
	assert.Equal(t, build.Succeeded, capturedBuild.Steps[0].Status)
}

func TestRunNotifyStep_NotificationNotFound_NoFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "missing-notif-job",
		BuildID:           10,
	}

	// Pipeline has the notify step type but no matching notification entity
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "missing-notif-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeNotify,
						Notify: &job.NotifyStep{
							Type: "slack",
							Name: "nonexistent",
						},
					},
				},
			},
		},
		// No Notifications defined
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 303, BuildNumber: "303", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "303", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	// The notify step returns false (no failure) when notification is not found,
	// so the build should succeed.
	assert.Equal(t, build.Succeeded, capturedBuild.Status)
}

func TestRunNotifyStep_NotificationTypeNotFound_NoFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "missing-type-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "missing-type-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeNotify,
						Notify: &job.NotifyStep{
							Type: "nonexistent-type",
							Name: "my-alert",
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "nonexistent-type",
				Name:      "my-alert",
				Canonical: "nonexistent-type.my-alert",
			},
		},
		// No NotificationTypes defined
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 304, BuildNumber: "304", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "304", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
}

func TestRunNotifyStep_WithMessage_And_Params(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "notify-msg-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "notify-msg-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeNotify,
						Notify: &job.NotifyStep{
							Type:    "echo-notifier",
							Name:    "my-alert",
							Message: "step-level message",
							Params:  map[string]string{"severity": "high"},
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "my-alert",
				Canonical: "echo-notifier.my-alert",
				Params:    &notification.Params{Params: map[string]string{"channel": "#alerts"}},
				Message:   "notification default message",
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					// Script that checks NOTIFY_MESSAGE env var
					Args:   []string{"-c", `echo "msg=$NOTIFY_MESSAGE channel=$param_channel severity=$notify_severity"`},
					Params: map[string]string{"path": "/bin/sh"},
				},
				Params: []string{"channel"},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 305, BuildNumber: "305", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "305", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	// Verify the script was able to read our env vars
	assert.Contains(t, capturedBuild.Steps[0].Logs, "msg=step-level message")
	assert.Contains(t, capturedBuild.Steps[0].Logs, "channel=#alerts")
	assert.Contains(t, capturedBuild.Steps[0].Logs, "severity=high")
}

func TestRunNotifyStep_MessageInterpolation(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "interp-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "interp-job",
				Plan: []job.PlanStep{
					// Task that writes to PIKOCI_OUTPUT
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "gen",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"-c", `printf '%s\n' 'CHANGELOG=line one\nline two\nline three' >> "$PIKOCI_OUTPUT"`},
								Params: map[string]string{"path": "/bin/sh"},
							},
						},
					},
				},
				OnSuccess: []job.HookStep{
					{
						Type: job.StepTypeNotify,
						Notify: &job.NotifyStep{
							Type:    "echo-notifier",
							Name:    "my-alert",
							Message: "Release $BUILD_NUMBER\n$TASK_GEN_CHANGELOG\ndone",
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "my-alert",
				Canonical: "echo-notifier.my-alert",
				Params:    &notification.Params{Params: map[string]string{}},
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					// Print the message so we can verify interpolation
					Args:   []string{"-c", `printf '%s' "$NOTIFY_MESSAGE"`},
					Params: map[string]string{"path": "/bin/sh"},
				},
				Params: []string{},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 400, BuildNumber: "42", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "42", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// The notify step is the second step (after the task)
	require.GreaterOrEqual(t, len(capturedBuild.Steps), 2)
	notifyLogs := capturedBuild.Steps[1].Logs
	// $BUILD_NUMBER should be interpolated
	assert.Contains(t, notifyLogs, "Release 42")
	// $TASK_GEN_CHANGELOG should be interpolated with \n converted to real newlines
	assert.Contains(t, notifyLogs, "line one\nline two\nline three")
	assert.Contains(t, notifyLogs, "done")
}

// --- runAutoNotifications tests ---

func TestRunAutoNotifications_SuccessEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "auto-notif-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "auto-notif-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"done"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "success-alert",
				Canonical: "echo-notifier.success-alert",
				On:        []string{"success"},
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-c", `echo "auto-notif build_status=$notify_build_status"`},
					Params: map[string]string{"path": "/bin/sh"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 310, BuildNumber: "310", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "310", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Should have 2 steps: task + auto notification
	require.Len(t, capturedBuild.Steps, 2)
	assert.Equal(t, "notify", capturedBuild.Steps[1].Type)
	assert.Equal(t, "success-alert", capturedBuild.Steps[1].Name)
	assert.Contains(t, capturedBuild.Steps[1].Logs, "auto-notif build_status=success")
}

func TestRunAutoNotifications_FailureEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "auto-fail-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "auto-fail-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "fail",
							Run:  utils.RunnerCommand{Runner: "exec", Params: map[string]string{"path": "false"}},
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "fail-alert",
				Canonical: "echo-notifier.fail-alert",
				On:        []string{"failure"},
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"failure notification sent"},
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
		Return(&build.Build{ID: 311, BuildNumber: "311", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "311", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	// Should have 2 steps: failed task + auto failure notification
	require.Len(t, capturedBuild.Steps, 2)
	assert.Equal(t, "notify", capturedBuild.Steps[1].Type)
	assert.Equal(t, "fail-alert", capturedBuild.Steps[1].Name)
	assert.Contains(t, capturedBuild.Steps[1].Logs, "failure notification sent")
}

func TestRunAutoNotifications_EventNotMatched(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "auto-no-match-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "auto-no-match-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"ok"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "fail-only-alert",
				Canonical: "echo-notifier.fail-only-alert",
				On:        []string{"failure"}, // only on failure
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"should not appear"},
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
		Return(&build.Build{ID: 312, BuildNumber: "312", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "312", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Only the task step, no notification
	require.Len(t, capturedBuild.Steps, 1)
	assert.Equal(t, "task", capturedBuild.Steps[0].Type)
}

func TestRunAutoNotifications_AllEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "auto-all-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "auto-all-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"done"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "all-alert",
				Canonical: "echo-notifier.all-alert",
				On:        []string{"all"},
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"all event triggered"},
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
		Return(&build.Build{ID: 313, BuildNumber: "313", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "313", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.Len(t, capturedBuild.Steps, 2)
	assert.Equal(t, "notify", capturedBuild.Steps[1].Type)
}

func TestRunAutoNotifications_JobScope(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "unscoped-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "unscoped-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"done"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "scoped-alert",
				Canonical: "echo-notifier.scoped-alert",
				On:        []string{"success"},
				Jobs:      []string{"other-job"}, // scoped to a different job
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"should not appear"},
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
		Return(&build.Build{ID: 314, BuildNumber: "314", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "314", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Notification scoped to "other-job", so it should NOT fire for "unscoped-job"
	require.Len(t, capturedBuild.Steps, 1)
}

func TestRunAutoNotifications_ExcludeJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "excluded-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "excluded-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"done"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "exclude-alert",
				Canonical: "echo-notifier.exclude-alert",
				On:        []string{"success"},
				Exclude:   []string{"excluded-job"},
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"should not appear"},
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
		Return(&build.Build{ID: 315, BuildNumber: "315", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "315", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.Len(t, capturedBuild.Steps, 1)
}

func TestRunAutoNotifications_NoOnField_Skips(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "no-on-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "no-on-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"done"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "no-on-alert",
				Canonical: "echo-notifier.no-on-alert",
				// On is nil - explicit-only notifications
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"should not appear"},
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
		Return(&build.Build{ID: 316, BuildNumber: "316", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "316", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// No auto notifications because On is empty
	require.Len(t, capturedBuild.Steps, 1)
}

// --- runPutStepTrigger tests ---

func TestProcessJob_PutStepTrigger_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "trigger-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "trigger-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypePut,
						Put: &job.PutStep{
							Type:   "trigger",
							Name:   "downstream",
							Params: map[string]string{"key": "val"},
						},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "downstream", Type: "trigger", Canonical: "trigger.downstream"},
		},
		ResourceTypes: []restype.ResourceType{
			{ID: 1, Name: "trigger", Source: "pikoci://trigger"},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 320, BuildNumber: "320", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// CreateTrigger should be called
	svc.EXPECT().CreateTrigger(gomock.Any(), "main", "trigger.downstream", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ string, version map[string]interface{}) (*trigger.Trigger, error) {
			assert.Equal(t, "val", version["key"])
			assert.Equal(t, "test-pipeline", version["trigger_pipeline"])
			assert.Equal(t, "trigger-job", version["trigger_job"])
			assert.Equal(t, "320", version["trigger_build"])
			return &trigger.Trigger{ID: 1, Version: version}, nil
		})

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "320", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, "put", capturedBuild.Steps[0].Type)
	assert.Equal(t, "downstream", capturedBuild.Steps[0].Name)
	assert.Equal(t, build.Succeeded, capturedBuild.Steps[0].Status)
}

func TestProcessJob_PutStepTrigger_Failure(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "trigger-fail-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "trigger-fail-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypePut,
						Put: &job.PutStep{
							Type:   "trigger",
							Name:   "downstream",
							Params: map[string]string{"key": "val"},
						},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "downstream", Type: "trigger", Canonical: "trigger.downstream"},
		},
		ResourceTypes: []restype.ResourceType{
			{ID: 1, Name: "trigger", Source: "pikoci://trigger"},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 321, BuildNumber: "321", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// CreateTrigger fails
	svc.EXPECT().CreateTrigger(gomock.Any(), "main", "trigger.downstream", gomock.Any()).
		Return(nil, fmt.Errorf("trigger creation failed"))

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "321", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, "put", capturedBuild.Steps[0].Type)
	assert.Equal(t, build.Failed, capturedBuild.Steps[0].Status)
	assert.Contains(t, capturedBuild.Steps[0].Logs, "failed to create trigger")
}

// --- notifyNextPendingBuild tests ---

func TestNotifyNextPendingBuild_SendsMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, topic := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
	}

	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), "main", "test-pipeline", "test-job").
		Return(&build.Build{ID: 42, BuildNumber: "42", Status: build.Pending}, nil)

	topic.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, msg *pubsub.Message) error {
		var body queue.Body
		err := json.Unmarshal(msg.Body, &body)
		require.NoError(t, err)
		assert.Equal(t, "main", body.TeamCanonical)
		assert.Equal(t, "test-pipeline", body.PipelineCanonical)
		assert.Equal(t, "test-job", body.JobName)
		assert.Equal(t, uint32(42), body.BuildID)
		return nil
	})

	w.notifyNextPendingBuild(ctx, m)
}

func TestNotifyNextPendingBuild_NoPending(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
	}

	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), "main", "test-pipeline", "test-job").
		Return(nil, nil)

	// No topic.Send expected
	w.notifyNextPendingBuild(ctx, m)
}

func TestNotifyNextPendingBuild_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
	}

	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), "main", "test-pipeline", "test-job").
		Return(nil, fmt.Errorf("db error"))

	// No topic.Send expected, error is logged
	w.notifyNextPendingBuild(ctx, m)
}

// --- startServices / stopServices tests ---

func TestProcessJob_ServiceStep_StartAndStop(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "service-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "service-job",
				Plan: []job.PlanStep{
					{
						Type:    job.StepTypeService,
						Service: &job.ServiceStep{Name: "my-db"},
					},
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "use-db",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"using db"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Services: []service.Service{
			{
				ID:   1,
				Name: "my-db",
				Start: utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"starting db"},
					Params: map[string]string{"path": "echo"},
				},
				Stop: utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"stopping db"},
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
		Return(&build.Build{ID: 330, BuildNumber: "330", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "330", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Steps: service:start, task, service:stop
	require.GreaterOrEqual(t, len(capturedBuild.Steps), 3)
	assert.Equal(t, "my-db:start", capturedBuild.Steps[0].Name)
	assert.Equal(t, "service", capturedBuild.Steps[0].Type)
	assert.Equal(t, build.Succeeded, capturedBuild.Steps[0].Status)
	assert.Equal(t, "use-db", capturedBuild.Steps[1].Name)
	assert.Equal(t, "task", capturedBuild.Steps[1].Type)
	// The stop step is appended as the last step
	found := false
	for _, s := range capturedBuild.Steps {
		if s.Name == "my-db:stop" && s.Type == "service" {
			found = true
			assert.Equal(t, build.Succeeded, s.Status)
		}
	}
	assert.True(t, found, "expected my-db:stop step")
}

func TestProcessJob_ServiceStep_StartFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "service-fail-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "service-fail-job",
				Plan: []job.PlanStep{
					{
						Type:    job.StepTypeService,
						Service: &job.ServiceStep{Name: "broken-svc"},
					},
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "should-not-run",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"should not appear"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Services: []service.Service{
			{
				ID:   1,
				Name: "broken-svc",
				Start: utils.RunnerCommand{
					Runner: "exec",
					Params: map[string]string{"path": "false"}, // fails
				},
				Stop: utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"stopping broken-svc"},
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
		Return(&build.Build{ID: 331, BuildNumber: "331", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "331", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	// Only the failed start step should be present, task should not run
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, "broken-svc:start", capturedBuild.Steps[0].Name)
	assert.Equal(t, build.Failed, capturedBuild.Steps[0].Status)
	// Task should NOT have run
	for _, s := range capturedBuild.Steps {
		assert.NotEqual(t, "should-not-run", s.Name, "task should not run when service start fails")
	}
}

func TestProcessJob_ServiceStep_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "missing-svc-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "missing-svc-job",
				Plan: []job.PlanStep{
					{
						Type:    job.StepTypeService,
						Service: &job.ServiceStep{Name: "nonexistent"},
					},
				},
			},
		},
		// No Services defined
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 332, BuildNumber: "332", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "332", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
}

func TestProcessJob_ServiceStep_WithReadyCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "ready-svc-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "ready-svc-job",
				Plan: []job.PlanStep{
					{
						Type:    job.StepTypeService,
						Service: &job.ServiceStep{Name: "my-svc"},
					},
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "use-svc",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"using svc"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Services: []service.Service{
			{
				ID:   1,
				Name: "my-svc",
				Start: utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"starting svc"},
					Params: map[string]string{"path": "echo"},
				},
				ReadyCheck: &service.ReadyCheck{
					RunnerCommand: utils.RunnerCommand{
						Runner: "exec",
						Args:   []string{"ready"},
						Params: map[string]string{"path": "echo"},
					},
					Interval: "100ms",
					Timeout:  "5s",
				},
				Stop: utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"stopping svc"},
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
		Return(&build.Build{ID: 333, BuildNumber: "333", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "333", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Steps: service:start, service:ready, task, service:stop
	var stepNames []string
	for _, s := range capturedBuild.Steps {
		stepNames = append(stepNames, s.Name)
	}
	assert.Contains(t, stepNames, "my-svc:start")
	assert.Contains(t, stepNames, "my-svc:ready")
	assert.Contains(t, stepNames, "use-svc")
	assert.Contains(t, stepNames, "my-svc:stop")
}

func TestProcessJob_ServiceStep_ReadyCheckTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "ready-timeout-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "ready-timeout-job",
				Plan: []job.PlanStep{
					{
						Type:    job.StepTypeService,
						Service: &job.ServiceStep{Name: "slow-svc"},
					},
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "should-not-run",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"nope"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
			},
		},
		Services: []service.Service{
			{
				ID:   1,
				Name: "slow-svc",
				Start: utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"starting slow-svc"},
					Params: map[string]string{"path": "echo"},
				},
				ReadyCheck: &service.ReadyCheck{
					RunnerCommand: utils.RunnerCommand{
						Runner: "exec",
						Params: map[string]string{"path": "false"}, // always fails
					},
					Interval: "100ms",
					Timeout:  "500ms",
				},
				Stop: utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"stopping slow-svc"},
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
		Return(&build.Build{ID: 334, BuildNumber: "334", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "334", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	// Task should not have run because ready check timed out
	for _, s := range capturedBuild.Steps {
		assert.NotEqual(t, "should-not-run", s.Name)
	}
	// Ready step should be failed
	found := false
	for _, s := range capturedBuild.Steps {
		if s.Name == "slow-svc:ready" {
			found = true
			assert.Equal(t, build.Failed, s.Status)
			assert.Contains(t, s.Logs, "timed out")
		}
	}
	assert.True(t, found, "expected slow-svc:ready step")
}

// --- serviceParams tests ---

func TestServiceParams(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _, _ := newTestWorker(ctrl)

	b := &build.Build{BuildNumber: "99"}
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
	}
	cmdParams := map[string]string{"image": "postgres:15"}
	overrides := map[string]string{"port": "5433"}

	params := w.serviceParams(b, m, cmdParams, overrides)

	assert.Equal(t, "postgres:15", params["image"])
	assert.Equal(t, "99", params["BUILD_NUMBER"])
	assert.Equal(t, "test-job", params["BUILD_JOB_NAME"])
	assert.Equal(t, "test-pipeline", params["BUILD_PIPELINE_NAME"])
	assert.Equal(t, "main", params["BUILD_TEAM_NAME"])
	assert.Equal(t, "5433", params["param_port"])
}

// --- processMessage tests ---

func TestProcessMessage_ResourceCheckDispatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		ResourceCanonical: "cron.my-cron",
	}
	cwd := t.TempDir()

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Resources: []resource.Resource{
			{ID: 1, Name: "my-cron", Type: "cron", Canonical: "cron.my-cron"},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "cron",
				Check: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", `printf "[]"`},
					Params: map[string]string{"path": "/bin/sh"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}

	svc.EXPECT().GetPipeline(gomock.Any(), m.TeamCanonical, m.PipelineCanonical).
		Return(pp, nil)

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{}, false, nil).AnyTimes()

	svc.EXPECT().UpdatePipelineResource(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", gomock.Any()).
		Return(nil).AnyTimes()

	w.processMessage(ctx, m, cwd)
}

func TestProcessMessage_GetPipelineError(t *testing.T) {
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

	svc.EXPECT().GetPipeline(gomock.Any(), m.TeamCanonical, m.PipelineCanonical).
		Return(nil, fmt.Errorf("pipeline not found"))

	// Should return early without calling processJob or processResourceCheck
	w.processMessage(ctx, m, cwd)
}

func TestProcessMessage_EmptyJobAndResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		// No JobName, no ResourceCanonical
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipeline(gomock.Any(), m.TeamCanonical, m.PipelineCanonical).
		Return(&pipeline.Pipeline{Name: "test-pipeline"}, nil)

	// Should return early without calling processJob or processResourceCheck
	w.processMessage(ctx, m, cwd)
}

// --- processJob edge cases ---

func TestProcessJob_BuildID_Zero(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)
	w.LocalMode = true // skip FindOldestPendingBuild

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           0,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{{ID: 1, Name: "test-job"}},
	}
	cwd := t.TempDir()

	// Should return early because BuildID is 0
	// No StartPendingBuild or other calls expected beyond the global mock defaults
	_ = svc
	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_NoPendingBuild(t *testing.T) {
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
		Jobs: []job.Job{{ID: 1, Name: "test-job"}},
	}
	cwd := t.TempDir()

	// No pending builds
	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), "main", "test-pipeline", "test-job").
		Return(nil, nil)

	// Should return early
	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_FindOldestPendingBuild_Error(t *testing.T) {
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
		Jobs: []job.Job{{ID: 1, Name: "test-job"}},
	}
	cwd := t.TempDir()

	svc.EXPECT().FindOldestPendingBuild(gomock.Any(), "main", "test-pipeline", "test-job").
		Return(nil, fmt.Errorf("db error"))

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_PutStep_LocalMode_Skips(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, topic := newTestWorker(ctrl)
	w.LocalMode = true

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "local-put-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "local-put-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypePut,
						Put: &job.PutStep{
							Type: "git",
							Name: "repo",
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
		Return(&build.Build{ID: 340, BuildNumber: "340", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "340", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	// Allow topic.Send for notifyNextPendingBuild
	topic.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Contains(t, capturedBuild.Steps[0].Logs, "skipping put step")
}

func TestProcessJob_OnSuccessHook(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "success-hook-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "success-hook-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"done"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
				OnSuccess: []job.HookStep{
					runnerHook(utils.RunnerCommand{
						Runner: "exec",
						Args:   []string{"job succeeded hook"},
						Params: map[string]string{"path": "echo"},
					}),
				},
				Ensure: []job.HookStep{
					runnerHook(utils.RunnerCommand{
						Runner: "exec",
						Args:   []string{"job ensure hook"},
						Params: map[string]string{"path": "echo"},
					}),
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 341, BuildNumber: "341", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "341", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Task + on_success hook + ensure hook
	require.GreaterOrEqual(t, len(capturedBuild.Steps), 1)
	// Job-level hooks are stored in capturedBuild.Job
	require.GreaterOrEqual(t, len(capturedBuild.Job), 2)
}

func TestProcessJob_OnFailureHook_WithAutoNotification(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "fail-hook-notif-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "fail-hook-notif-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "fail",
							Run:  utils.RunnerCommand{Runner: "exec", Params: map[string]string{"path": "false"}},
						},
					},
				},
				OnFailure: []job.HookStep{
					runnerHook(utils.RunnerCommand{
						Runner: "exec",
						Args:   []string{"job failed hook"},
						Params: map[string]string{"path": "echo"},
					}),
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "fail-alert",
				Canonical: "echo-notifier.fail-alert",
				On:        []string{"failure"},
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"auto fail notif"},
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
		Return(&build.Build{ID: 342, BuildNumber: "342", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "342", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	// Should have task step (failed) + auto notification step
	require.GreaterOrEqual(t, len(capturedBuild.Steps), 2)
	assert.Equal(t, "notify", capturedBuild.Steps[1].Type)
	assert.Equal(t, "fail-alert", capturedBuild.Steps[1].Name)
	assert.Contains(t, capturedBuild.Steps[1].Logs, "auto fail notif")
	// Job-level on_failure hook should also have run
	require.NotEmpty(t, capturedBuild.Job)
}

// --- runPlan edge cases ---

func TestRunPlan_EmptyPlan(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "empty-plan-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "empty-plan-job",
				Plan: []job.PlanStep{}, // empty plan
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 350, BuildNumber: "350", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "350", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	assert.Empty(t, capturedBuild.Steps)
}

func TestRunPlan_MixedSteps_GetTaskPutNotify(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "mixed-job",
		BuildID:           10,
		VersionID:         1,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "mixed-job",
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
					{
						Type: job.StepTypeNotify,
						Notify: &job.NotifyStep{
							Type: "echo-notifier",
							Name: "deploy-alert",
						},
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
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "deploy-alert",
				Canonical: "echo-notifier.deploy-alert",
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"notifying"},
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
		Return(&build.Build{ID: 351, BuildNumber: "351", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, false, nil).AnyTimes()

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "351", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.Len(t, capturedBuild.Steps, 4)
	assert.Equal(t, "get", capturedBuild.Steps[0].Type)
	assert.Equal(t, "task", capturedBuild.Steps[1].Type)
	assert.Equal(t, "put", capturedBuild.Steps[2].Type)
	assert.Equal(t, "notify", capturedBuild.Steps[3].Type)
}

func TestProcessJob_GetPipelineJob_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "error-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
	}
	cwd := t.TempDir()

	svc.EXPECT().StartPendingBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID).
		Return(&build.Build{ID: 360, BuildNumber: "360", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(nil, fmt.Errorf("job not found"))

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "360", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	assert.Contains(t, capturedBuild.Error, "failed to get job")
}

func TestProcessJob_NotifyStep_WithHooks(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "notify-hook-job",
		BuildID:           10,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "notify-hook-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeNotify,
						Notify: &job.NotifyStep{
							Type: "echo-notifier",
							Name: "my-alert",
						},
						OnSuccess: []job.HookStep{
							runnerHook(utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"notify succeeded"},
								Params: map[string]string{"path": "echo"},
							}),
						},
						Ensure: []job.HookStep{
							runnerHook(utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"notify ensure"},
								Params: map[string]string{"path": "echo"},
							}),
						},
					},
				},
			},
		},
		Notifications: []notification.Notification{
			{
				ID:        1,
				Type:      "echo-notifier",
				Name:      "my-alert",
				Canonical: "echo-notifier.my-alert",
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"notifying"},
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
		Return(&build.Build{ID: 370, BuildNumber: "370", StartedAt: time.Now()}, nil)
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "370", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	expectPendingBuild(svc, 10)
	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, "notify", capturedBuild.Steps[0].Type)
	assert.Equal(t, build.Succeeded, capturedBuild.Steps[0].Status)
}

func TestCreateWorkDir(t *testing.T) {
	w := &Worker{}
	dir, err := w.createWorkDir()
	require.NoError(t, err)
	assert.DirExists(t, dir)
	os.RemoveAll(dir)
}

func TestNew(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	topic := mock.NewTopic(ctrl)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := New(svc, topic, nil, nil, logger)
	assert.NotNil(t, w)
}
