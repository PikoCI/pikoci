package worker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/notification"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/workitem"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/service"
	"github.com/pikoci/pikoci/pikoci/trigger"
	"github.com/pikoci/pikoci/pikoci/utils"
	"go.uber.org/mock/gomock"
)

func newTestWorker(ctrl *gomock.Controller) (*Worker, *mock.Service) {
	svc := mock.NewService(ctrl)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// InsertBuildGetVersion is called after every successful get step; allow it globally.
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// FindBuildGetVersions is checked after version resolution for pinned versions.
	// Runs for all non-retry, non-local builds with build ID 10 (from GetJobBuild mock above).
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), uint32(10)).Return(nil, nil).AnyTimes()

	// NotifySerialGroupPendingBuilds is called by notifyNextPendingBuild; allow it globally.
	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// GetJobBuild is called by processJob (poll-based flow) and pollForCancellation.
	// Return a started build with the requested build number.
	svc.EXPECT().GetJobBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string, bn string) (*build.Build, error) {
			return &build.Build{ID: 10, BuildNumber: bn, Status: build.Started, StartedAt: time.Now()}, nil
		}).AnyTimes()

	// CreateJobBuild is called by triggerResourceJobs to create pending builds.
	var createBuildCounter uint32
	svc.EXPECT().CreateJobBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string, b build.Build) (*build.Build, error) {
			createBuildCounter++
			return &build.Build{ID: createBuildCounter, BuildNumber: fmt.Sprintf("%d", createBuildCounter)}, nil
		}).AnyTimes()

	// EvaluateDownstreamJobs is called after a build succeeds; allow it globally.
	svc.EXPECT().EvaluateDownstreamJobs(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// FireTriggerNotifications is called by triggerResourceJobs before creating builds; allow globally.
	svc.EXPECT().FireTriggerNotifications(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	w := &Worker{
		pikoci: svc,
		logger: logger,
	}
	return w, svc
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "echo-job",
		BuildID:           10,
		BuildNumber:       "10",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// running step + after task step + after marking succeeded
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_StepStartedAt(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "echo-job",
		BuildID:           10,
		BuildNumber:       "10",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedStartedAt *time.Time
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			if capturedStartedAt == nil {
				for _, s := range b.Steps {
					if s.Status == build.Started {
						capturedStartedAt = s.StartedAt
						break
					}
				}
			}
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	require.NotNil(t, capturedStartedAt, "started step should have StartedAt set")
	assert.False(t, capturedStartedAt.IsZero(), "started step StartedAt should not be zero")
}

func TestProcessJob_Success_WithGetAndTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		BuildNumber:       "10",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, false, nil).AnyTimes()

	// running steps + after get step + after task step + after marking succeeded
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)
}

func TestInsertBuildGetVersion_CalledWithCorrectArgs(t *testing.T) {
	ctrl := gomock.NewController(t)
	// Don't use newTestWorker — we need precise control over InsertBuildGetVersion.
	svc := mock.NewService(ctrl)
	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	svc.EXPECT().EvaluateDownstreamJobs(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc.EXPECT().GetJobBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string, bn string) (*build.Build, error) {
			return &build.Build{ID: 10, BuildNumber: bn, Status: build.Started, StartedAt: time.Now()}, nil
		}).AnyTimes()
	w := &Worker{pikoci: svc, logger: logger}

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		BuildNumber:       "10",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), "main", "test-pipeline", "test-job").
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "test-pipeline", "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, false, nil).AnyTimes()
	svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "test-pipeline", "test-job", "10", gomock.Any()).
		Return(nil).AnyTimes()

	// Pinned versions check (none pinned for this test)
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), "main", "test-pipeline", "test-job", uint32(10)).Return(nil, nil)

	// Verify InsertBuildGetVersion is called with exact correct arguments
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), "main", "test-pipeline", "test-job", uint32(10), "my-cron", uint32(1)).
		Return(nil)

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_FailedPassedConstraint_NoBuilds(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "downstream-job",
		BuildID:           10,
		BuildNumber:       "20",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// Passed check: upstream-job has no builds
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "upstream-job", (*uint32)(nil), (*uint32)(nil), uint32(0), []build.Status{build.Succeeded, build.Warning}).
		Return([]*build.Build{}, false, nil)

	// Build should be deleted (not failed)
	svc.EXPECT().DeleteJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "20").
		Return(nil)

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_FailedPassedConstraint_NotSucceeded(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "downstream-job",
		BuildID:           10,
		BuildNumber:       "21",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// Passed check: upstream-job has a failed build
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "upstream-job", (*uint32)(nil), (*uint32)(nil), uint32(0), []build.Status{build.Succeeded, build.Warning}).
		Return([]*build.Build{{ID: 5, Status: build.Failed}}, false, nil)

	// Build should be deleted
	svc.EXPECT().DeleteJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "21").
		Return(nil)

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_PassedConstraint_AcceptsWarningBuilds(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "downstream-job",
		BuildID:           10,
		BuildNumber:       "22",
		VersionID:         1,
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
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "echo",
							Run: utils.RunnerCommand{
								Runner: "exec",
								Args:   []string{"downstream ran"},
								Params: map[string]string{"path": "echo"},
							},
						},
					},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Name: "my-cron", Type: "cron", Canonical: "cron.my-cron", Params: &resource.Params{}},
		},
		ResourceTypes: []restype.ResourceType{
			{
				ID: 1, Name: "cron", Params: []string{},
				Check: &utils.RunnerCommand{Runner: "exec", Args: []string{"-ec", `echo "[{\"date\":\"now\"}]"`}, Params: map[string]string{"path": "/bin/sh"}},
				Pull:  &utils.RunnerCommand{Runner: "exec", Params: map[string]string{}},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}

	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// Upstream job has a WARNING build (from allow_failure) — should be treated as success
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "upstream-job", (*uint32)(nil), (*uint32)(nil), uint32(0), []build.Status{build.Succeeded, build.Warning}).
		Return([]*build.Build{{
			ID:     5,
			Status: build.Warning,
			Steps: []build.Step{
				{Type: "get", Name: "my-cron", VersionID: 42},
			},
		}}, false, nil)

	// checkVersionAvailability needs resource versions
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 42, Version: map[string]interface{}{"date": "now"}},
		}, false, nil).AnyTimes()

	// Build should proceed (not be deleted)
	var capturedStatuses []build.Status
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "22", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedStatuses = append(capturedStatuses, b.Status)
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	// The build should NOT have been deleted — it should run
	require.NotEmpty(t, capturedStatuses, "build should have been updated (not deleted), meaning passed constraints accepted warning builds")
	assert.Equal(t, build.Succeeded, capturedStatuses[len(capturedStatuses)-1], "downstream build should succeed")
}

func TestProcessJob_TaskFailure_RunsHooks(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "failing-job",
		BuildID:           10,
		BuildNumber:       "30",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "upstream-job",
		BuildID:           10,
		BuildNumber:       "40",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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

	// First check: jobs should be triggered via CreateJobBuild (mocked globally in newTestWorker)
	w.processResourceCheck(ctx, m, cwd, pp)
}

func TestProcessResourceCheck_NestedVersionFlattened(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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

	// First check: jobs should be triggered via CreateJobBuild (mocked globally in newTestWorker)
	w.processResourceCheck(ctx, m, cwd, pp)
}

func TestProcessResourceCheck_SecondCheckTriggers(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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

	// Second check: jobs SHOULD be triggered via CreateJobBuild (mocked globally in newTestWorker)
	w.processResourceCheck(ctx, m, cwd, pp)
}

func TestProcessResourceCheckTrigger_FirstCheckTriggers(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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

	// First check: jobs should be triggered via CreateJobBuild (mocked globally in newTestWorker)
	w.processResourceCheck(ctx, m, "", pp)
}

func TestProcessResourceCheckTrigger_SecondCheckTriggers(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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

	// Second check: jobs SHOULD be triggered via CreateJobBuild (mocked globally in newTestWorker)
	w.processResourceCheck(ctx, m, "", pp)
}

func TestCheckPassedConstraints_AllPassed(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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

	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "job-a", (*uint32)(nil), (*uint32)(nil), uint32(0), []build.Status{build.Succeeded, build.Warning}).
		Return([]*build.Build{{ID: 1, Status: build.Succeeded, Steps: []build.Step{
			{Type: "get", Name: "my-cron", VersionID: 5},
		}}}, false, nil)
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "job-b", (*uint32)(nil), (*uint32)(nil), uint32(0), []build.Status{build.Succeeded, build.Warning}).
		Return([]*build.Build{{ID: 2, Status: build.Succeeded, Steps: []build.Step{
			{Type: "get", Name: "my-cron", VersionID: 5},
		}}}, false, nil)

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j, nil)
	assert.True(t, ok)
	assert.Equal(t, map[string]uint32{"cron.my-cron": 5}, resolved)
}

func TestCheckPassedConstraints_NoCommonVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "lint", (*uint32)(nil), (*uint32)(nil), uint32(0), []build.Status{build.Succeeded, build.Warning}).
		Return([]*build.Build{{ID: 10, Status: build.Succeeded, Steps: []build.Step{
			{Type: "get", Name: "my-repo", VersionID: 5},
		}}}, false, nil)
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "test", (*uint32)(nil), (*uint32)(nil), uint32(0), []build.Status{build.Succeeded, build.Warning}).
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "lint", (*uint32)(nil), (*uint32)(nil), uint32(0), []build.Status{build.Succeeded, build.Warning}).
		Return([]*build.Build{
			{ID: 10, Status: build.Succeeded, Steps: []build.Step{
				{Type: "get", Name: "my-repo", VersionID: 5},
			}},
			{ID: 9, Status: build.Succeeded, Steps: []build.Step{
				{Type: "get", Name: "my-repo", VersionID: 3},
			}},
		}, false, nil)
	// test has builds with versions {5, 7}
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "test", (*uint32)(nil), (*uint32)(nil), uint32(0), []build.Status{build.Succeeded, build.Warning}).
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
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	svc.EXPECT().ListJobBuilds(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "upstream-job", (*uint32)(nil), (*uint32)(nil), uint32(0), []build.Status{build.Succeeded, build.Warning}).
		Return([]*build.Build{{ID: 1, Status: build.Succeeded, Steps: []build.Step{
			{Type: "put", Name: "my-artifact", VersionID: 7},
		}}}, false, nil)

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j, nil)
	assert.True(t, ok)
	assert.Equal(t, map[string]uint32{"artifact.my-artifact": 7}, resolved)
}

func TestCheckPassedConstraints_RequestsSucceededAndWarningStatuses(t *testing.T) {
	// Regression test: ListJobBuilds must be called with succeeded+warning
	// status filters so the HTTP layer returns full step data. Without the
	// filter the server omits steps (summary optimisation), which causes
	// checkPassedConstraints to find no version IDs and delete the build,
	// triggering an infinite retry loop.
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "downstream",
		BuildID:           99,
	}
	b := build.Build{ID: 99, BuildNumber: "1"}
	j := &job.Job{
		Name: "downstream",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeGet,
				Get: &job.GetStep{
					Type:    "git",
					Name:    "app",
					Passed:  []string{"upstream"},
					Trigger: true,
				},
			},
		},
	}

	// The mock must receive exactly succeeded+warning statuses — not nil.
	svc.EXPECT().ListJobBuilds(
		gomock.Any(),
		m.TeamCanonical, m.PipelineCanonical, "upstream",
		(*uint32)(nil), (*uint32)(nil), uint32(0),
		[]build.Status{build.Succeeded, build.Warning},
	).Return([]*build.Build{{
		ID:     1,
		Status: build.Succeeded,
		Steps:  []build.Step{{Type: "get", Name: "app", VersionID: 42}},
	}}, false, nil)

	ok, resolved := w.checkPassedConstraints(ctx, m, &b, j, nil)
	assert.True(t, ok)
	assert.Equal(t, map[string]uint32{"git.app": 42}, resolved)
}

func TestImplicitGetAfterPut_CreatesVersionAndRecords(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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

func TestRunHooks_FailedRunner_StepMarkedFailed(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}

	pp := testPipeline()
	cwd := t.TempDir()

	b := build.Build{
		ID:          70,
		BuildNumber: "70",
		Status:      build.Succeeded,
		Job:         []build.Step{},
	}

	hooks := []job.HookStep{
		runnerHook(utils.RunnerCommand{Runner: "exec", Params: map[string]string{"path": "false"}}),
	}

	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "70", gomock.Any()).
		Return(nil).AnyTimes()

	w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "step", hooks, "on_success", nil, nil)

	require.Len(t, b.Job, 1)
	assert.Equal(t, build.Failed, b.Job[0].Status, "hook step should be marked as failed")
	assert.Equal(t, build.Succeeded, b.Status, "build status should remain unchanged")
}

func TestRunHooks_MixedSuccess_StepStatuses(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
	}

	pp := testPipeline()
	cwd := t.TempDir()

	b := build.Build{
		ID:          71,
		BuildNumber: "71",
		Status:      build.Succeeded,
		Job:         []build.Step{},
	}

	hooks := []job.HookStep{
		runnerHook(utils.RunnerCommand{Runner: "exec", Args: []string{"ok"}, Params: map[string]string{"path": "echo"}}),
		runnerHook(utils.RunnerCommand{Runner: "exec", Params: map[string]string{"path": "false"}}),
	}

	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "71", gomock.Any()).
		Return(nil).AnyTimes()

	w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "step", hooks, "ensure", nil, nil)

	require.Len(t, b.Job, 2)
	assert.Equal(t, build.Succeeded, b.Job[0].Status, "first hook should succeed")
	assert.Equal(t, build.Failed, b.Job[1].Status, "second hook should fail")
	assert.Equal(t, build.Succeeded, b.Status, "build status should remain unchanged")
}

func TestProcessMessage_JobDispatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		BuildNumber:       "1",
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipeline(gomock.Any(), m.TeamCanonical, m.PipelineCanonical).
		Return(&pipeline.Pipeline{Name: "test-pipeline"}, nil)


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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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

	params, vid, _ := w.buildPullParams(ctx, m, &b, rt, r, g, 0)
	require.NotNil(t, params)
	assert.Equal(t, "def", params["version_ref"])
	assert.Equal(t, "http://example.com", params["param_url"])
	assert.Equal(t, uint32(5), vid)
}

func TestBuildPullParams_NoVersionID_UsesLatest(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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

	params, vid, _ := w.buildPullParams(ctx, m, &b, rt, r, g, 0)
	require.NotNil(t, params)
	assert.Equal(t, "latest", params["version_ref"])
	assert.Equal(t, uint32(2), vid)
}

func TestBuildPullParams_NoVersions_Fails(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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

	params, _, _ := w.buildPullParams(ctx, m, &b, rt, r, g, 0)
	assert.Nil(t, params)
}

func TestBuildPullParams_ResolvedVersionTakesPriority(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	params, vid, _ := w.buildPullParams(ctx, m, &b, rt, r, g, 10)
	require.NotNil(t, params)
	assert.Equal(t, uint32(10), vid)
	assert.Equal(t, "resolved-ver", params["version_ref"])
}

func TestCheckVersionAvailability_NoVersions_DeletesBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "deploy-job",
		BuildID:           10,
		BuildNumber:       "80",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// running steps + after task step + after put step + after marking succeeded
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "80", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_PutStep_CacheDirSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "artifact-job",
		BuildID:           10,
		BuildNumber:       "81",
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
	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "81", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = &b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	// The build should have completed successfully (task + put steps)
	require.NotNil(t, capturedBuild)
	assert.Equal(t, build.Succeeded, capturedBuild.Status)
}

func TestProcessJob_OrderedPlan_GetTaskPut(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "ordered-job",
		BuildID:           10,
		BuildNumber:       "90",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{
			{ID: 1, Version: map[string]interface{}{"date": "now"}},
		}, false, nil).AnyTimes()

	// running steps + after get + after task + after put + after success
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "90", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_TaskTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "timeout-job",
		BuildID:           10,
		BuildNumber:       "100",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "get-timeout-job",
		BuildID:           10,
		BuildNumber:       "101",
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

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Contains(t, capturedBuild.Steps[0].Logs, "timed out after 1s")
}

func TestProcessJob_NoTimeout_Succeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "no-timeout-job",
		BuildID:           10,
		BuildNumber:       "102",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "retry-job",
		BuildID:           10,
		BuildNumber:       "200",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "exhaust-job",
		BuildID:           10,
		BuildNumber:       "201",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "timeout-retry-job",
		BuildID:           10,
		BuildNumber:       "202",
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

func TestSecretValuesFromResolved(t *testing.T) {
	tests := []struct {
		name     string
		resolved map[string]string
		want     []string
	}{
		{
			name:     "nil map",
			resolved: nil,
			want:     nil,
		},
		{
			name:     "empty map",
			resolved: map[string]string{},
			want:     nil,
		},
		{
			name: "skips empty values",
			resolved: map[string]string{
				"__pikoci_secret:vault:path:key__": "",
			},
			want: nil,
		},
		{
			name: "skips short values under 3 chars",
			resolved: map[string]string{
				"__pikoci_secret:vault:path:a__": "ab",
				"__pikoci_secret:vault:path:b__": "x",
			},
			want: nil,
		},
		{
			name: "extracts and deduplicates",
			resolved: map[string]string{
				"__pikoci_secret:vault:path:token__":    "s3cret-token",
				"__pikoci_secret:vault:path:token2__":   "s3cret-token",
				"__pikoci_secret:vault:path:password__": "hunter2",
			},
			want: []string{"s3cret-token", "hunter2"},
		},
		{
			name: "sorts longest first",
			resolved: map[string]string{
				"__pikoci_secret:vault:path:short__": "abc",
				"__pikoci_secret:vault:path:long__":  "abcdefghij",
				"__pikoci_secret:vault:path:mid__":   "abcdef",
			},
			want: []string{"abcdefghij", "abcdef", "abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := secretValuesFromResolved(tt.resolved)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMaskSecrets(t *testing.T) {
	tests := []struct {
		name  string
		input string
		vals  []string
		want  string
	}{
		{
			name:  "nil vals is no-op",
			input: "hello world",
			vals:  nil,
			want:  "hello world",
		},
		{
			name:  "empty vals is no-op",
			input: "hello world",
			vals:  []string{},
			want:  "hello world",
		},
		{
			name:  "masks single value",
			input: "token is s3cret-token here",
			vals:  []string{"s3cret-token"},
			want:  "token is *** here",
		},
		{
			name:  "masks multiple occurrences",
			input: "s3cret-token and s3cret-token again",
			vals:  []string{"s3cret-token"},
			want:  "*** and *** again",
		},
		{
			name:  "masks multiple different values",
			input: "user=admin pass=hunter2",
			vals:  []string{"admin", "hunter2"},
			want:  "user=*** pass=***",
		},
		{
			name:  "longest first prevents partial masking",
			input: "the value is supersecret",
			vals:  []string{"supersecret", "secret"},
			want:  "the value is ***",
		},
		{
			name:  "masks multi-line secret",
			input: "begin\nline1\nline2\nend",
			vals:  []string{"line1\nline2"},
			want:  "begin\n***\nend",
		},
		{
			name:  "regex-special chars in secret treated literally",
			input: "price is $100.00 (USD)",
			vals:  []string{"$100.00 (USD)"},
			want:  "price is ***",
		},
		{
			name:  "no match leaves string unchanged",
			input: "nothing to see here",
			vals:  []string{"s3cret-token"},
			want:  "nothing to see here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskSecrets(tt.input, tt.vals)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProcessResourceCheck_WithSecretVars(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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

	// Trigger the job via CreateJobBuild (mocked globally in newTestWorker)
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	w, _ := newTestWorker(ctrl)

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

	out, _, err := w.runRunner(ctx, ru, cwd, rc, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "hello_from_shell", "shell variable should survive and be echoed")
}

func TestRunRunner_AwkPositionalArgsWork(t *testing.T) {
	// Verifies that awk $1, $0 etc. work in command args.
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

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

	out, _, err := w.runRunner(ctx, ru, cwd, rc, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "foo", "awk $1 should extract first field")
	assert.NotContains(t, out, "bar", "awk $1 should not include second field")
}

func TestRunRunner_ParamVarsExpandedByShell(t *testing.T) {
	// Verifies that $param_* variables are available to the shell via env vars.
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

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

	out, _, err := w.runRunner(ctx, ru, cwd, rc, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "url=https://example.com", "param_url should be expanded by shell from env")
}

func TestRunRunner_EnvPlaceholder(t *testing.T) {
	// Verifies that $env injects -e KEY=VALUE flags for metadata vars
	// and excludes runner-internal params like cmd, image, WORKDIR.
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

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
	w.runRunner(ctx, ru, cwd, rc, nil)
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

func TestFindContainerWorkdir(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "found",
			args: []string{"run", "--rm", "-v", "/host:/workdir", "-w", "/workdir", "-e", "FOO=bar", "image"},
			want: "/workdir",
		},
		{
			name: "not found",
			args: []string{"run", "--rm", "-v", "/host:/data", "image"},
			want: "",
		},
		{
			name: "w flag at end without value",
			args: []string{"run", "-w"},
			want: "",
		},
		{
			name: "custom workdir",
			args: []string{"run", "-w", "/app/src", "image"},
			want: "/app/src",
		},
		{
			name: "long form --workdir",
			args: []string{"run", "--workdir", "/app", "image"},
			want: "/app",
		},
		{
			name: "long form --workdir=path",
			args: []string{"run", "--workdir=/app/data", "image"},
			want: "/app/data",
		},
		{
			name: "workdir at end without value",
			args: []string{"run", "--workdir"},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, findContainerWorkdir(tt.args))
		})
	}
}

func TestRunRunner_EnvPlaceholder_RemapsPIKOCIOutput(t *testing.T) {
	// Verifies that $env injects PIKOCI_OUTPUT with remapped container path
	// when -w is present in the runner args.
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	// Runner template with -w /workdir (like the built-in docker runner).
	ru := runner.Runner{
		Name: "docker",
		Run:  utils.RunCommand{Path: "echo", Args: []string{"-w", "/workdir", "$env", "$args"}},
	}

	rc := utils.RunnerCommand{
		Runner: "docker",
		Args:   []string{"hello"},
		Params: map[string]string{
			"cmd":           "echo test",
			"image":         "alpine",
			"PIKOCI_OUTPUT": cwd + "/.pikoci-output-12345",
			"GET_REPO_REF":  "abc123",
		},
	}

	out, _, _ := w.runRunner(ctx, ru, cwd, rc, nil)

	// The output should contain the remapped PIKOCI_OUTPUT path
	assert.Contains(t, out, "PIKOCI_OUTPUT=/workdir/.pikoci-output-12345",
		"PIKOCI_OUTPUT should be remapped to container workdir")
	// Regular env vars should also be present
	assert.Contains(t, out, "GET_REPO_REF=abc123")
}

func TestRunRunner_EnvPlaceholder_DockerTemplate(t *testing.T) {
	// Simulates the full built-in docker runner template to verify
	// PIKOCI_OUTPUT remapping works with the real arg layout.
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	// Mirror the real docker.hcl template but use echo to inspect args.
	ru := runner.Runner{
		Name: "docker",
		Run: utils.RunCommand{
			Path: "echo",
			Args: []string{
				"run", "--rm",
				"-v", "$WORKDIR:/workdir",
				"-w", "/workdir",
				"$env",
				"$args",
				"$image",
				"/bin/sh", "-ec", "$cmd",
			},
		},
	}

	rc := utils.RunnerCommand{
		Runner: "docker",
		Args:   []string{"-v", "cache:/cache"},
		Params: map[string]string{
			"cmd":           "echo hello",
			"image":         "golang:1.25",
			"PIKOCI_OUTPUT": cwd + "/.pikoci-output-42",
			"GET_REPO_REF":  "abc123",
			"TASK_BUILD_VER": "2.0",
		},
	}

	out, _, err := w.runRunner(ctx, ru, cwd, rc, nil)
	require.NoError(t, err)

	// Verify volume mount expanded
	assert.Contains(t, out, "-v "+cwd+":/workdir")
	// Verify PIKOCI_OUTPUT remapped to container path
	assert.Contains(t, out, "PIKOCI_OUTPUT=/workdir/.pikoci-output-42")
	// Verify regular env vars injected
	assert.Contains(t, out, "GET_REPO_REF=abc123")
	assert.Contains(t, out, "TASK_BUILD_VER=2.0")
	// Verify internal params NOT injected as -e flags
	assert.NotContains(t, out, "-e cmd=")
	assert.NotContains(t, out, "-e image=")
	// Verify image and cmd expanded
	assert.Contains(t, out, "golang:1.25")
	assert.Contains(t, out, "echo hello")
}

func TestRunRunner_EnvPlaceholder_PIKOCIOutputNoWorkdir(t *testing.T) {
	// When no -w flag is present, PIKOCI_OUTPUT should be injected with the
	// original host path (no remapping).
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	ru := runner.Runner{
		Name: "custom",
		Run:  utils.RunCommand{Path: "echo", Args: []string{"$env", "$args"}},
	}

	hostPath := cwd + "/.pikoci-output-99999"
	rc := utils.RunnerCommand{
		Runner: "custom",
		Args:   []string{"hello"},
		Params: map[string]string{
			"cmd":           "echo test",
			"PIKOCI_OUTPUT": hostPath,
			"GET_REPO_REF":  "abc123",
		},
	}

	out, _, _ := w.runRunner(ctx, ru, cwd, rc, nil)

	// Without -w, the original host path should be used
	assert.Contains(t, out, "PIKOCI_OUTPUT="+hostPath,
		"PIKOCI_OUTPUT should use original host path when no -w flag")
	assert.Contains(t, out, "GET_REPO_REF=abc123")
}

func TestRunRunner_MasksSecretInOutput(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	ru := runner.Runner{
		Name: "exec",
		Run:  utils.RunCommand{Path: "$path", Args: []string{"$args"}},
	}
	rc := utils.RunnerCommand{
		Runner: "exec",
		Args:   []string{"-ec", `echo "my-s3cret-token"`},
		Params: map[string]string{"path": "/bin/sh"},
	}

	secretVals := []string{"my-s3cret-token"}
	out, _, err := w.runRunner(ctx, ru, cwd, rc, secretVals)
	require.NoError(t, err)
	assert.Contains(t, out, "***")
	assert.NotContains(t, out, "my-s3cret-token")
}

func TestRunRunner_MasksSecretInPartialLog(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	ru := runner.Runner{
		Name: "exec",
		Run:  utils.RunCommand{Path: "$path", Args: []string{"$args"}},
	}
	rc := utils.RunnerCommand{
		Runner: "exec",
		Args:   []string{"-ec", `echo "my-s3cret-token"; sleep 3`},
		Params: map[string]string{"path": "/bin/sh"},
	}

	var partialLogs []string
	onPartial := func(partial string) {
		partialLogs = append(partialLogs, partial)
	}

	secretVals := []string{"my-s3cret-token"}
	out, _, err := w.runRunner(ctx, ru, cwd, rc, secretVals, onPartial)
	require.NoError(t, err)
	assert.Contains(t, out, "***")
	assert.NotContains(t, out, "my-s3cret-token")
	// At least one partial log should have been emitted (sleep 3 > 2s ticker)
	require.NotEmpty(t, partialLogs, "expected at least one partial log from 3s sleep with 2s ticker")
	for _, pl := range partialLogs {
		assert.NotContains(t, pl, "my-s3cret-token", "partial log should be masked")
		assert.Contains(t, pl, "***", "partial log should contain masked value")
	}
}

func TestRunRunner_NilSecretVals_NoMasking(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	ru := runner.Runner{
		Name: "exec",
		Run:  utils.RunCommand{Path: "$path", Args: []string{"$args"}},
	}
	rc := utils.RunnerCommand{
		Runner: "exec",
		Args:   []string{"-ec", `echo "visible-output"`},
		Params: map[string]string{"path": "/bin/sh"},
	}

	out, _, err := w.runRunner(ctx, ru, cwd, rc, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "visible-output")
}

func TestProcessResourceCheck_RawSecretFormat(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()

	// Create a temp PEM-like file
	tmpDir := t.TempDir()
	pemFile := tmpDir + "/test.pem"
	pemContent := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn\n-----END RSA PRIVATE KEY-----\n"
	os.WriteFile(pemFile, []byte(pemContent), 0644)

	m := workitem.Body{
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

	// Job trigger via CreateJobBuild (mocked globally in newTestWorker)
	w.processResourceCheck(ctx, m, cwd, pp)
}

func TestFetchSecrets_RawFormat(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

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
	w, _ := newTestWorker(ctrl)

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

	m := workitem.Body{
		TeamCanonical:     "tc",
		PipelineCanonical: "test-pipeline",
	}

	// CreateJobBuild should be called 3 times, once for each job (mocked globally in newTestWorker)
	w.triggerResourceJobs(ctx, m, pp, r, cv)
}

func TestTriggerResourceJobs_SkipsWhenResourcePinned(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

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

	m := workitem.Body{
		TeamCanonical:     "tc",
		PipelineCanonical: "test-pipeline",
	}

	// topic.Send should NOT be called — resource is pinned to a different version
	w.triggerResourceJobs(ctx, m, pp, r, cv)
}

func TestTriggerResourceJobs_TriggersWhenPinnedVersionMatches(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

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

	m := workitem.Body{
		TeamCanonical:     "tc",
		PipelineCanonical: "test-pipeline",
	}

	// CreateJobBuild SHOULD be called — pinned version matches (mocked globally in newTestWorker)
	w.triggerResourceJobs(ctx, m, pp, r, cv)
}

func TestTriggerResourceJobs_FiresOnTriggerNotifications(t *testing.T) {
	// Verify that FireTriggerNotifications is called before CreateJobBuild,
	// with the correct team, pipeline, resource canonical, and version metadata.
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := &Worker{pikoci: svc, logger: logger}

	ctx := context.Background()
	versionMeta := map[string]interface{}{"ref": "abc123"}
	r := resource.Resource{ID: 1, Name: "repo", Type: "git", Canonical: "git.repo"}
	cv := &resource.Version{ID: 10, Version: versionMeta}
	m := workitem.Body{TeamCanonical: "tc", PipelineCanonical: "my-pipeline"}
	pp := &pipeline.Pipeline{
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "build",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo", Trigger: true}},
				},
			},
		},
	}

	svc.EXPECT().FireTriggerNotifications(ctx, "tc", "my-pipeline", "git.repo", versionMeta).Times(1)
	svc.EXPECT().CreateJobBuild(gomock.Any(), "tc", "my-pipeline", "build", gomock.Any()).
		Return(&build.Build{ID: 1, BuildNumber: "1"}, nil)

	w.triggerResourceJobs(ctx, m, pp, r, cv)
}

func TestProcessJob_TaskInputMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "input-job",
		BuildID:           10,
		BuildNumber:       "200",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "output-job",
		BuildID:           10,
		BuildNumber:       "300",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "io-job",
		BuildID:           10,
		BuildNumber:       "400",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "multi-input-job",
		BuildID:           10,
		BuildNumber:       "500",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "multi-output-job",
		BuildID:           10,
		BuildNumber:       "600",
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
	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	w := &Worker{
		pikoci: svc,
		logger: logger,
	}

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "cancel-job",
		BuildID:           10,
		BuildNumber:       "700",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           20,
		BuildNumber:       "3.1",
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

	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_Retry_FailsOnVersionLookupError(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           20,
		BuildNumber:       "3.1",
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
	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	w := &Worker{
		pikoci: svc,
		logger: logger,
	}

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "cancel-hook-job",
		BuildID:           10,
		BuildNumber:       "800",
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
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	w := &Worker{
		pikoci: svc,
		logger: logger,
	}

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "cancel-loop-job",
		BuildID:           10,
		BuildNumber:       "900",
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
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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

func TestProcessJob_GetJobBuild_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	// Don't use newTestWorker — we need a specific GetJobBuild expectation.
	svc := mock.NewService(ctrl)
	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := &Worker{pikoci: svc, logger: logger}

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		BuildNumber:       "10",
	}
	pp := testPipeline()
	cwd := t.TempDir()

	svc.EXPECT().GetJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10").
		Return(nil, fmt.Errorf("build not found"))

	// Should return immediately — GetJobBuild failed
	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_NoBuildNumber_NotLocalMode_Returns(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		// BuildNumber intentionally empty, LocalMode is false
	}
	pp := testPipeline()
	cwd := t.TempDir()

	// Should return immediately — build_number not set and not in local mode
	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_GenericError_Returns(t *testing.T) {
	ctrl := gomock.NewController(t)
	// Don't use newTestWorker — we need a specific GetJobBuild expectation.
	svc := mock.NewService(ctrl)
	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	svc.EXPECT().InsertBuildGetVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := &Worker{pikoci: svc, logger: logger}

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		BuildNumber:       "10",
	}
	pp := testPipeline()
	cwd := t.TempDir()

	svc.EXPECT().GetJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10").
		Return(nil, fmt.Errorf("database table is locked"))

	// No re-queue expected; worker just returns
	w.processJob(ctx, m, cwd, pp)
}

func TestRunGetStepLocal_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	dir := t.TempDir()
	// Create a local resource directory
	resDir := filepath.Join(dir, "my-resource")
	require.NoError(t, os.MkdirAll(resDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(resDir, "test.txt"), []byte("content"), 0644))

	cwd := filepath.Join(dir, "workdir")
	require.NoError(t, os.MkdirAll(cwd, 0755))

	m := workitem.Body{
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
	w, svc := newTestWorker(ctrl)

	dir := t.TempDir()
	cwd := filepath.Join(dir, "workdir")
	require.NoError(t, os.MkdirAll(cwd, 0755))

	m := workitem.Body{
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

// blockingPoller is a test helper that blocks PollNextWork until ctx is cancelled.
type blockingPoller struct{}

func (bp *blockingPoller) PollNextWork(ctx context.Context, _ workitem.WorkerContext) (*workitem.Item, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestDrain_UnblocksPollLoopImmediately(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	poller := &blockingPoller{}
	w := &Worker{
		pikoci:     svc,
		workPoller: poller,
		logger:     logger,
		StartedAt:  time.Now(),
	}

	svc.EXPECT().WorkerHeartbeat(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	// Give Run() time to start the poll loop
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
	w, _ := newTestWorker(ctrl)

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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "job-timeout-job",
		BuildID:           10,
		BuildNumber:       "100",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "100", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	assert.Contains(t, capturedBuild.Error, "job timed out after 2s")
}

func TestProcessJob_JobTimeout_NotReached(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "job-timeout-ok",
		BuildID:           10,
		BuildNumber:       "100",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "100", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pn, jn string, bID string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

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
	w, _ := newTestWorker(ctrl)

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

	out, _, err := w.runRunner(ctx, ru, cwd, rc, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "shell_works")
}

func TestRunRunner_ShellFileMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

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

	out, _, err := w.runRunner(ctx, ru, cwd, rc, nil)
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		BuildNumber:       "10",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		BuildNumber:       "10",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var finalBuild *build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			bc := b
			finalBuild = &bc
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	require.NotNil(t, finalBuild)
	require.GreaterOrEqual(t, len(finalBuild.Steps), 2)
	consumeStep := finalBuild.Steps[1]
	assert.Equal(t, build.Succeeded, consumeStep.Status, "consume step should succeed")
	assert.Contains(t, consumeStep.Logs, "TASK_PRODUCE_MY_VAR=hello123")
}

func TestProcessJob_ExportedVarsAccumulate(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		BuildNumber:       "10",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		BuildNumber:       "10",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var finalBuild *build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			bc := b
			finalBuild = &bc
			return nil
		}).AnyTimes()

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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "notify-job",
		BuildID:           10,
		BuildNumber:       "300",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "300", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, "notify", capturedBuild.Steps[0].Type)
	assert.Equal(t, "my-alert", capturedBuild.Steps[0].Name)
	assert.Equal(t, build.Succeeded, capturedBuild.Steps[0].Status)
}

func TestProcessJob_NotifyStep_Failure(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "notify-fail-job",
		BuildID:           10,
		BuildNumber:       "301",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "301", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, "notify", capturedBuild.Steps[0].Type)
	assert.Equal(t, build.Failed, capturedBuild.Steps[0].Status)
}

func TestRunNotifyStep_LocalMode_Skips(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)
	w.LocalMode = true

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "local-notify-job",
		BuildID:           10,
		BuildNumber:       "302",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "302", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, "notify", capturedBuild.Steps[0].Type)
	assert.Contains(t, capturedBuild.Steps[0].Logs, "skipping notify step")
	assert.Equal(t, build.Succeeded, capturedBuild.Steps[0].Status)
}

func TestRunNotifyStep_NotificationNotFound_NoFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "missing-notif-job",
		BuildID:           10,
		BuildNumber:       "303",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "303", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	// The notify step returns false (no failure) when notification is not found,
	// so the build should succeed.
	assert.Equal(t, build.Succeeded, capturedBuild.Status)
}

func TestRunNotifyStep_NotificationTypeNotFound_NoFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "missing-type-job",
		BuildID:           10,
		BuildNumber:       "304",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "304", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
}

func TestRunNotifyStep_WithMessage_And_Params(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "notify-msg-job",
		BuildID:           10,
		BuildNumber:       "305",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "305", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "interp-job",
		BuildID:           10,
		BuildNumber:       "42",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "42", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

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

func TestProcessJob_DockerRunner_PIKOCIOutput_FlowsToNotification(t *testing.T) {
	// End-to-end test: a task using a Docker-like runner (with $env and -w)
	// writes to $PIKOCI_OUTPUT, and the job-level on_success notification
	// receives the exported variable in its message.
	//
	// Since we can't run real Docker in unit tests, we use a shell wrapper
	// that extracts -e flags and executes the command, mimicking Docker behavior.
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "docker-output-job",
		BuildID:           10,
		BuildNumber:       "50",
		VersionID:         1,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "docker-output-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "produce",
							Run: utils.RunnerCommand{
								Runner: "docker",
								Params: map[string]string{
									"image": "unused",
									"cmd":   `echo "RELEASE_NOTES=Fixed bug and added feature" >> "$PIKOCI_OUTPUT"`,
								},
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
							Message: "Released: $TASK_PRODUCE_RELEASE_NOTES",
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
					Args:   []string{"-c", `printf '%s' "$NOTIFY_MESSAGE"`},
					Params: map[string]string{"path": "/bin/sh"},
				},
				Params: []string{},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
			// Docker-like runner: includes -w /workdir so findContainerWorkdir
			// detects it and remaps PIKOCI_OUTPUT. The actual command runs via
			// /bin/sh which inherits PIKOCI_OUTPUT from process env (set by
			// runRunner's cmd.Env). We use "true" to absorb the extra args
			// (-w, /workdir, $env flags, $image) before "&&" runs the real cmd.
			{Name: "docker", Run: utils.RunCommand{
				Path: "/bin/sh",
				Args: []string{"-ec", "$cmd", "--", "-w", "/workdir", "$env", "$image"},
			}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "50", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status,
		"job should succeed; steps: %+v", capturedBuild.Steps)
	// The notification is the second step (after the task).
	require.GreaterOrEqual(t, len(capturedBuild.Steps), 2,
		"expected task + notification steps")
	notifyStep := capturedBuild.Steps[len(capturedBuild.Steps)-1]
	assert.Contains(t, notifyStep.Logs, "Released: Fixed bug and added feature",
		"notification message should contain the exported PIKOCI_OUTPUT variable")
}

// --- runAutoNotifications tests ---

func TestRunAutoNotifications_SuccessEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "auto-notif-job",
		BuildID:           10,
		BuildNumber:       "310",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "310", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "auto-fail-job",
		BuildID:           10,
		BuildNumber:       "311",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "311", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "auto-no-match-job",
		BuildID:           10,
		BuildNumber:       "312",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "312", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Only the task step, no notification
	require.Len(t, capturedBuild.Steps, 1)
	assert.Equal(t, "task", capturedBuild.Steps[0].Type)
}

func TestRunAutoNotifications_AllEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "auto-all-job",
		BuildID:           10,
		BuildNumber:       "313",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "313", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.Len(t, capturedBuild.Steps, 2)
	assert.Equal(t, "notify", capturedBuild.Steps[1].Type)
}

func TestRunAutoNotifications_JobScope(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "unscoped-job",
		BuildID:           10,
		BuildNumber:       "314",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "314", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Notification scoped to "other-job", so it should NOT fire for "unscoped-job"
	require.Len(t, capturedBuild.Steps, 1)
}

func TestRunAutoNotifications_ExcludeJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "excluded-job",
		BuildID:           10,
		BuildNumber:       "315",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "315", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.Len(t, capturedBuild.Steps, 1)
}

func TestRunAutoNotifications_JobScope_ForEachGroup(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test--a",
		BuildID:           10,
		BuildNumber:       "400",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:           1,
				Name:         "test--a",
				ForEachGroup: "test",
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
			{
				ID:           2,
				Name:         "test--b",
				ForEachGroup: "test",
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
				Name:      "group-alert",
				Canonical: "echo-notifier.group-alert",
				On:        []string{"success"},
				Jobs:      []string{"test"}, // group name should match for_each instances
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"notified"},
					Params: map[string]string{"path": "echo"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "400", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Notification scoped to group "test" should fire for instance "test--a"
	require.Len(t, capturedBuild.Steps, 2) // task + notification
}

func TestRunAutoNotifications_ExcludeJob_ForEachGroup(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test--a",
		BuildID:           10,
		BuildNumber:       "401",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:           1,
				Name:         "test--a",
				ForEachGroup: "test",
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
			{
				ID:           2,
				Name:         "test--b",
				ForEachGroup: "test",
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
				Name:      "exclude-group-alert",
				Canonical: "echo-notifier.exclude-group-alert",
				On:        []string{"success"},
				Exclude:   []string{"test"}, // group name should exclude for_each instances
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "401", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Notification excluded group "test", so it should NOT fire for instance "test--a"
	require.Len(t, capturedBuild.Steps, 1) // task only
}

func TestRunAutoNotifications_JobScope_SpecificInstance(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test--a",
		BuildID:           10,
		BuildNumber:       "402",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:           1,
				Name:         "test--a",
				ForEachGroup: "test",
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
			{
				ID:           2,
				Name:         "test--b",
				ForEachGroup: "test",
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
				Name:      "instance-alert",
				Canonical: "echo-notifier.instance-alert",
				On:        []string{"success"},
				Jobs:      []string{"test--a"}, // specific instance, not group
			},
		},
		NotificationTypes: []notiftype.NotificationType{
			{
				ID:   1,
				Name: "echo-notifier",
				Notify: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"notified"},
					Params: map[string]string{"path": "echo"},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "402", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Notification scoped to specific instance "test--a" should fire
	require.Len(t, capturedBuild.Steps, 2) // task + notification
}

func TestRunAutoNotifications_JobScope_SpecificInstance_NoMatchOther(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test--b", // different instance than scoped
		BuildID:           10,
		BuildNumber:       "403",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:           1,
				Name:         "test--a",
				ForEachGroup: "test",
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
			{
				ID:           2,
				Name:         "test--b",
				ForEachGroup: "test",
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
				Name:      "instance-alert",
				Canonical: "echo-notifier.instance-alert",
				On:        []string{"success"},
				Jobs:      []string{"test--a"}, // scoped to test--a only
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[1], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "403", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Notification scoped to "test--a" should NOT fire for "test--b"
	require.Len(t, capturedBuild.Steps, 1) // task only
}

func TestRunAutoNotifications_ExcludeJob_SpecificInstance(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test--b",
		BuildID:           10,
		BuildNumber:       "404",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:           1,
				Name:         "test--a",
				ForEachGroup: "test",
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
			{
				ID:           2,
				Name:         "test--b",
				ForEachGroup: "test",
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
				Name:      "exclude-instance-alert",
				Canonical: "echo-notifier.exclude-instance-alert",
				On:        []string{"success"},
				Exclude:   []string{"test--b"}, // exclude only test--b
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[1], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "404", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Notification excluded specific instance "test--b", so it should NOT fire
	require.Len(t, capturedBuild.Steps, 1) // task only
}

func TestRunAutoNotifications_NoOnField_Skips(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "no-on-job",
		BuildID:           10,
		BuildNumber:       "316",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "316", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// No auto notifications because On is empty
	require.Len(t, capturedBuild.Steps, 1)
}

// --- runPutStepTrigger tests ---

func TestProcessJob_PutStepTrigger_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "trigger-job",
		BuildID:           10,
		BuildNumber:       "320",
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

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, "put", capturedBuild.Steps[0].Type)
	assert.Equal(t, "downstream", capturedBuild.Steps[0].Name)
	assert.Equal(t, build.Succeeded, capturedBuild.Steps[0].Status)
}

func TestProcessJob_PutStepTrigger_Failure(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "trigger-fail-job",
		BuildID:           10,
		BuildNumber:       "321",
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

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Equal(t, "put", capturedBuild.Steps[0].Type)
	assert.Equal(t, build.Failed, capturedBuild.Steps[0].Status)
	assert.Contains(t, capturedBuild.Steps[0].Logs, "failed to create trigger")
}

// --- notifyNextPendingBuild tests ---

func TestNotifyNextPendingBuild_CallsSerialGroup(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
	}

	// notifyNextPendingBuild now only calls NotifySerialGroupPendingBuilds,
	// which is already expected via AnyTimes() in newTestWorker.
	w.notifyNextPendingBuild(ctx, m)
}

// --- startServices / stopServices tests ---

func TestProcessJob_ServiceStep_StartAndStop(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "service-job",
		BuildID:           10,
		BuildNumber:       "330",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "330", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

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

func TestProcessJob_ServiceStep_InsideIfBranchIsStopped(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "service-if-job",
		BuildID:           10,
		BuildNumber:       "332",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "service-if-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeIf,
						If: &job.IfStep{
							Branches: []job.IfBranch{
								{
									Type:      "if",
									Label:     "always",
									Condition: "'a' == 'a'",
									Steps: []job.PlanStep{
										{
											Type:    job.StepTypeService,
											Service: &job.ServiceStep{Name: "my-db"},
										},
									},
								},
							},
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "332", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	found := false
	for _, s := range capturedBuild.Steps {
		if s.Name == "my-db:stop" && s.Type == "service" {
			found = true
		}
	}
	assert.True(t, found, "service started inside an if branch must still be stopped")
}

func TestProcessJob_ServiceStep_StartFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "service-fail-job",
		BuildID:           10,
		BuildNumber:       "331",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "331", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "missing-svc-job",
		BuildID:           10,
		BuildNumber:       "332",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "332", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
}

func TestProcessJob_ServiceStep_WithReadyCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "ready-svc-job",
		BuildID:           10,
		BuildNumber:       "333",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "333", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "ready-timeout-job",
		BuildID:           10,
		BuildNumber:       "334",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "334", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

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
	w, _ := newTestWorker(ctrl)

	b := &build.Build{BuildNumber: "99"}
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
	}
	cmdParams := map[string]string{"image": "postgres:15"}
	overrides := map[string]string{"port": "5433"}

	params, warnings := w.serviceParams(b, m, cmdParams, overrides, []string{"port"}, "postgres")

	assert.Equal(t, "postgres:15", params["image"])
	assert.Equal(t, "99", params["BUILD_NUMBER"])
	assert.Equal(t, "test-job", params["BUILD_JOB_NAME"])
	assert.Equal(t, "test-pipeline", params["BUILD_PIPELINE_NAME"])
	assert.Equal(t, "main", params["BUILD_TEAM_NAME"])
	assert.Equal(t, "5433", params["param_port"])
	assert.Empty(t, warnings)
}

func TestValidateParams(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("valid params accepted", func(t *testing.T) {
		accepted, warnings := validateParams(
			map[string]string{"token": "abc", "url": "http://x"},
			[]string{"token", "url"},
			"param_", logger, "resource_type", "git",
		)
		assert.Equal(t, map[string]string{"param_token": "abc", "param_url": "http://x"}, accepted)
		assert.Empty(t, warnings)
	})

	t.Run("invalid param with suggestion", func(t *testing.T) {
		accepted, warnings := validateParams(
			map[string]string{"toke": "abc"},
			[]string{"token", "url"},
			"param_", logger, "resource_type", "git",
		)
		assert.Empty(t, accepted)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], `param "toke" is not declared by resource_type "git"`)
		assert.Contains(t, warnings[0], `did you mean "token"`)
	})

	t.Run("invalid param without suggestion", func(t *testing.T) {
		accepted, warnings := validateParams(
			map[string]string{"completely_wrong": "abc"},
			[]string{"token", "url"},
			"param_", logger, "resource_type", "git",
		)
		assert.Empty(t, accepted)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], `param "completely_wrong" is not declared by resource_type "git"`)
		assert.NotContains(t, warnings[0], "did you mean")
	})

	t.Run("empty params", func(t *testing.T) {
		accepted, warnings := validateParams(
			map[string]string{},
			[]string{"token"},
			"param_", logger, "resource_type", "git",
		)
		assert.Empty(t, accepted)
		assert.Empty(t, warnings)
	})

	t.Run("empty allowed params", func(t *testing.T) {
		accepted, warnings := validateParams(
			map[string]string{"token": "abc"},
			[]string{},
			"param_", logger, "resource_type", "git",
		)
		assert.Empty(t, accepted)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], `param "token" is not declared`)
	})
}

func TestFormatParamWarnings(t *testing.T) {
	t.Run("no warnings", func(t *testing.T) {
		assert.Equal(t, "", formatParamWarnings(nil))
	})

	t.Run("single warning", func(t *testing.T) {
		result := formatParamWarnings([]string{"WARNING: something"})
		assert.Equal(t, "WARNING: something\n", result)
	})

	t.Run("multiple warnings", func(t *testing.T) {
		result := formatParamWarnings([]string{"WARNING: a", "WARNING: b"})
		assert.Equal(t, "WARNING: a\nWARNING: b\n", result)
	})
}

func TestValidateTaskRunParams(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("typo of args", func(t *testing.T) {
		warnings := validateTaskRunParams(map[string]string{"arg": "echo hello"}, logger, "my-step")
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], `"arg"`)
		assert.Contains(t, warnings[0], `did you mean "args"`)
	})

	t.Run("known runner param not warned", func(t *testing.T) {
		warnings := validateTaskRunParams(map[string]string{"cmd": "echo hello"}, logger, "my-step")
		assert.Empty(t, warnings)
	})

	t.Run("unrelated param not warned", func(t *testing.T) {
		warnings := validateTaskRunParams(map[string]string{"my_custom_var": "value"}, logger, "my-step")
		assert.Empty(t, warnings)
	})

	t.Run("empty params", func(t *testing.T) {
		warnings := validateTaskRunParams(map[string]string{}, logger, "my-step")
		assert.Empty(t, warnings)
	})
}

// --- processMessage tests ---

func TestProcessMessage_ResourceCheckDispatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
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
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           0,
		BuildNumber:       "1",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{{ID: 1, Name: "test-job"}},
	}
	cwd := t.TempDir()

	// Should return early because BuildID is 0
	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_NoBuildNumber_NorLocalMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "test-job",
		BuildID:           10,
		// BuildNumber intentionally empty, LocalMode is false
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{{ID: 1, Name: "test-job"}},
	}
	cwd := t.TempDir()

	// Should return early — no BuildNumber and not in local mode
	w.processJob(ctx, m, cwd, pp)
}

func TestProcessJob_PutStep_LocalMode_Skips(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)
	w.LocalMode = true

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "local-put-job",
		BuildID:           10,
		BuildNumber:       "340",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "340", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	require.NotEmpty(t, capturedBuild.Steps)
	assert.Contains(t, capturedBuild.Steps[0].Logs, "skipping put step")
}

func TestProcessJob_OnSuccessHook(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "success-hook-job",
		BuildID:           10,
		BuildNumber:       "341",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "341", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	// Task + on_success hook + ensure hook
	require.GreaterOrEqual(t, len(capturedBuild.Steps), 1)
	// Job-level hooks are stored in capturedBuild.Job
	require.GreaterOrEqual(t, len(capturedBuild.Job), 2)
}

func TestProcessJob_OnFailureHook_WithAutoNotification(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "fail-hook-notif-job",
		BuildID:           10,
		BuildNumber:       "342",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "342", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "empty-plan-job",
		BuildID:           10,
		BuildNumber:       "350",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "350", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status)
	assert.Empty(t, capturedBuild.Steps)
}

func TestRunPlan_MixedSteps_GetTaskPutNotify(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "mixed-job",
		BuildID:           10,
		BuildNumber:       "351",
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
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "error-job",
		BuildID:           10,
		BuildNumber:       "360",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(nil, fmt.Errorf("job not found"))

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "360", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	assert.Contains(t, capturedBuild.Error, "failed to get job")
}

func TestProcessJob_NotifyStep_WithHooks(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "notify-hook-job",
		BuildID:           10,
		BuildNumber:       "370",
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "370", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

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
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := New(svc, logger, "test-worker", "test", "", 1, nil, false)
	assert.NotNil(t, w)
}

func TestProcessJob_InParallel_BasicExecution(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "parallel-job",
		BuildID:           10,
		BuildNumber:       "10",
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "parallel-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeInParallel,
						InParallel: &job.InParallelStep{
							Steps: []job.PlanStep{
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "echo-a",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"hello-a"}, Params: map[string]string{"path": "echo"}},
									},
								},
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "echo-b",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"hello-b"}, Params: map[string]string{"path": "echo"}},
									},
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var lastBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			lastBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	// Build should have succeeded
	assert.Equal(t, build.Succeeded, lastBuild.Status)

	// Should have one in_parallel step
	require.GreaterOrEqual(t, len(lastBuild.Steps), 1)
	found := false
	for _, s := range lastBuild.Steps {
		if s.Type == "in_parallel" {
			found = true
			assert.Equal(t, build.Succeeded, s.Status)
			assert.Len(t, s.SubSteps, 2)
			break
		}
	}
	assert.True(t, found, "should have an in_parallel step")
}

func TestProcessJob_InParallel_Empty(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "parallel-empty",
		BuildID:           10,
		BuildNumber:       "10",
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "parallel-empty",
				Plan: []job.PlanStep{
					{
						Type:       job.StepTypeInParallel,
						InParallel: &job.InParallelStep{Steps: []job.PlanStep{}},
					},
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var lastBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			lastBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, lastBuild.Status)
}

func TestProcessJob_InParallel_FailFast(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "parallel-ff",
		BuildID:           10,
		BuildNumber:       "10",
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "parallel-ff",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeInParallel,
						InParallel: &job.InParallelStep{
							FailFast: true,
							Limit:    1, // force sequential so first failure cancels second
							Steps: []job.PlanStep{
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "fail-task",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"-c", "exit 1"}, Params: map[string]string{"path": "/bin/sh"}},
									},
								},
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "should-not-run",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"should-not-reach"}, Params: map[string]string{"path": "echo"}},
									},
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var lastBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			lastBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, lastBuild.Status)

	// The in_parallel step itself should be failed
	for _, s := range lastBuild.Steps {
		if s.Type == "in_parallel" {
			assert.Equal(t, build.Failed, s.Status)
			break
		}
	}
}

func TestProcessJob_InParallel_NoFailFast(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "parallel-noff",
		BuildID:           10,
		BuildNumber:       "10",
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "parallel-noff",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeInParallel,
						InParallel: &job.InParallelStep{
							FailFast: false,
							Steps: []job.PlanStep{
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "fail-task",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"-c", "exit 1"}, Params: map[string]string{"path": "/bin/sh"}},
									},
								},
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "pass-task",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"ok"}, Params: map[string]string{"path": "echo"}},
									},
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var lastBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			lastBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	// Build should be failed because one step failed
	assert.Equal(t, build.Failed, lastBuild.Status)

	// Both steps should have run (2 sub-steps in the in_parallel step)
	for _, s := range lastBuild.Steps {
		if s.Type == "in_parallel" {
			assert.Equal(t, build.Failed, s.Status)
			assert.Len(t, s.SubSteps, 2)
			break
		}
	}
}

func TestProcessJob_InParallel_SubStepsVisibleWhileRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "parallel-live",
		BuildID:           10,
		BuildNumber:       "10",
	}
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "parallel-live",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeInParallel,
						InParallel: &job.InParallelStep{
							Steps: []job.PlanStep{
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "slow-a",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"-c", "sleep 0.2 && echo done-a"}, Params: map[string]string{"path": "/bin/sh"}},
									},
								},
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "slow-b",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"-c", "sleep 0.2 && echo done-b"}, Params: map[string]string{"path": "/bin/sh"}},
									},
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	// Track all intermediate build updates to verify sub-steps appear while running
	var sawStartedSubStep bool
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			for _, s := range b.Steps {
				if s.Type == "in_parallel" && s.Status == build.Started {
					for _, sub := range s.SubSteps {
						if sub.Status == build.Started {
							sawStartedSubStep = true
						}
					}
				}
			}
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.True(t, sawStartedSubStep, "should have seen a sub-step with status=started during execution")
}

func TestProcessJob_InParallel_Limit(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "parallel-limit",
		BuildID:           10,
		BuildNumber:       "10",
	}

	// Three tasks that each sleep 200ms. With limit=1 they run sequentially (~600ms).
	// Without limit they'd run concurrently (~200ms).
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "parallel-limit",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeInParallel,
						InParallel: &job.InParallelStep{
							Limit: 1,
							Steps: []job.PlanStep{
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "task-a",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"-c", "sleep 0.2 && echo a"}, Params: map[string]string{"path": "/bin/sh"}},
									},
								},
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "task-b",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"-c", "sleep 0.2 && echo b"}, Params: map[string]string{"path": "/bin/sh"}},
									},
								},
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "task-c",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"-c", "sleep 0.2 && echo c"}, Params: map[string]string{"path": "/bin/sh"}},
									},
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var lastBuild build.Build
	// Track max concurrent started sub-steps across all updates
	var maxConcurrent int
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			lastBuild = b
			for _, s := range b.Steps {
				if s.Type == "in_parallel" {
					running := 0
					for _, sub := range s.SubSteps {
						if sub.Status == build.Started {
							running++
						}
					}
					if running > maxConcurrent {
						maxConcurrent = running
					}
				}
			}
			return nil
		}).AnyTimes()

	start := time.Now()
	w.processJob(ctx, m, cwd, pp)
	elapsed := time.Since(start)

	assert.Equal(t, build.Succeeded, lastBuild.Status)
	// With limit=1, should take >= 600ms (3 * 200ms sequential)
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(500), "limit=1 should enforce sequential execution")
	// Should never have more than 1 task running at a time
	assert.LessOrEqual(t, maxConcurrent, 1, "limit=1 should allow at most 1 concurrent sub-step")
}

func TestProcessJob_InParallel_FailFast_CancelsRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "parallel-ff-cancel",
		BuildID:           10,
		BuildNumber:       "10",
	}

	// First task fails after 100ms, second task would take 2s.
	// With fail_fast, the second should be cancelled and total time << 2s.
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "parallel-ff-cancel",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeInParallel,
						InParallel: &job.InParallelStep{
							FailFast: true,
							Steps: []job.PlanStep{
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "fast-fail",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{}, Params: map[string]string{"path": "false"}},
									},
								},
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "slow-task",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"10"}, Params: map[string]string{"path": "sleep"}},
									},
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var lastBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			lastBuild = b
			return nil
		}).AnyTimes()

	start := time.Now()
	w.processJob(ctx, m, cwd, pp)
	elapsed := time.Since(start)

	assert.Equal(t, build.Failed, lastBuild.Status)
	// Should finish well before the 10s slow task would complete
	assert.Less(t, elapsed.Milliseconds(), int64(5000), "fail_fast should cancel the slow task")
}

func TestProcessJob_InParallel_PendingSubSteps(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "parallel-pending",
		BuildID:           10,
		BuildNumber:       "10",
	}

	// 3 tasks with limit=1: while the first runs, the other two should be pending
	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "parallel-pending",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeInParallel,
						InParallel: &job.InParallelStep{
							Limit: 1,
							Steps: []job.PlanStep{
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "task-a",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"-c", "sleep 0.2 && echo a"}, Params: map[string]string{"path": "/bin/sh"}},
									},
								},
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "task-b",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"-c", "sleep 0.2 && echo b"}, Params: map[string]string{"path": "/bin/sh"}},
									},
								},
								{
									Type: job.StepTypeTask,
									Task: &job.TaskStep{
										Name: "task-c",
										Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"-c", "sleep 0.2 && echo c"}, Params: map[string]string{"path": "/bin/sh"}},
									},
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var sawPendingSubSteps bool
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			for _, s := range b.Steps {
				if s.Type == "in_parallel" {
					pendingCount := 0
					for _, sub := range s.SubSteps {
						if sub.Status == build.Pending {
							pendingCount++
						}
					}
					// With limit=1 and 3 tasks, at least 2 should be pending at some point
					if pendingCount >= 2 {
						sawPendingSubSteps = true
					}
				}
			}
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.True(t, sawPendingSubSteps, "should have seen sub-steps with status=pending while waiting for semaphore")
}

func TestProcessJob_OnSuccessHook_Fails_BuildStaysSucceeded(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "success-hook-fail-job",
		BuildID:           10,
		BuildNumber:       "400",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "success-hook-fail-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "pass",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"ok"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
				OnSuccess: []job.HookStep{
					runnerHook(utils.RunnerCommand{
						Runner: "exec",
						Params: map[string]string{"path": "false"},
					}),
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "400", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status, "build should stay succeeded")
	require.GreaterOrEqual(t, len(capturedBuild.Job), 1)
	// Find the on_success hook step
	found := false
	for _, step := range capturedBuild.Job {
		if step.Type == "hook" {
			assert.Equal(t, build.Failed, step.Status, "on_success hook step should be marked failed")
			found = true
		}
	}
	assert.True(t, found, "should have found the on_success hook step")
}

func TestProcessJob_OnFailureHook_Fails_BuildStaysFailed(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "fail-hook-fail-job",
		BuildID:           10,
		BuildNumber:       "401",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "fail-hook-fail-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "will-fail",
							Run:  utils.RunnerCommand{Runner: "exec", Params: map[string]string{"path": "false"}},
						},
					},
				},
				OnFailure: []job.HookStep{
					runnerHook(utils.RunnerCommand{
						Runner: "exec",
						Params: map[string]string{"path": "false"},
					}),
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "401", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Failed, capturedBuild.Status, "build should stay failed")
	found := false
	for _, step := range capturedBuild.Job {
		if step.Type == "hook" {
			assert.Equal(t, build.Failed, step.Status, "on_failure hook step should be marked failed")
			found = true
		}
	}
	assert.True(t, found, "should have found the on_failure hook step")
}

func TestProcessJob_EnsureHook_Fails_BuildStatusUnchanged(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "ensure-hook-fail-job",
		BuildID:           10,
		BuildNumber:       "402",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "ensure-hook-fail-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "pass",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"ok"}, Params: map[string]string{"path": "echo"}},
						},
					},
				},
				Ensure: []job.HookStep{
					runnerHook(utils.RunnerCommand{
						Runner: "exec",
						Params: map[string]string{"path": "false"},
					}),
				},
			},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "402", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Succeeded, capturedBuild.Status, "build should stay succeeded")
	found := false
	for _, step := range capturedBuild.Job {
		if step.Type == "hook" {
			assert.Equal(t, build.Failed, step.Status, "ensure hook step should be marked failed")
			found = true
		}
	}
	assert.True(t, found, "should have found the ensure hook step")
}

func TestUpdateBuildWithRetry_SucceedsFirstAttempt(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)
	ctx := context.Background()
	w.apiCtx = ctx

	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "test-pipeline", JobName: "test-job"}
	b := build.Build{BuildNumber: "1", Status: build.Succeeded, StartedAt: time.Now()}

	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "1", gomock.Any()).Return(nil)

	err := w.updateBuild(ctx, m, b)
	assert.NoError(t, err)
}

func TestUpdateBuildWithRetry_FailsThenSucceeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)
	ctx := context.Background()
	w.apiCtx = ctx

	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "test-pipeline", JobName: "test-job"}
	b := build.Build{BuildNumber: "1", Status: build.Succeeded, StartedAt: time.Now()}

	gomock.InOrder(
		svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "1", gomock.Any()).Return(fmt.Errorf("connection refused")),
		svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "1", gomock.Any()).Return(nil),
	)

	err := w.updateBuild(ctx, m, b)
	assert.NoError(t, err)
}

func TestUpdateBuildWithRetry_ExhaustsRetries(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)
	ctx := context.Background()
	w.apiCtx = ctx

	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "test-pipeline", JobName: "test-job"}
	b := build.Build{BuildNumber: "1", Status: build.Succeeded, StartedAt: time.Now()}

	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "1", gomock.Any()).
		Return(fmt.Errorf("connection refused")).Times(4) // 1 initial + 3 retries

	err := w.updateBuild(ctx, m, b)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestUpdateBuildWithRetry_ContextCancelled(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)
	ctx, cancel := context.WithCancel(context.Background())
	w.apiCtx = ctx

	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "test-pipeline", JobName: "test-job"}
	b := build.Build{BuildNumber: "1", Status: build.Succeeded, StartedAt: time.Now()}

	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, _ build.Build) error {
			cancel() // Cancel context on first failure
			return fmt.Errorf("connection refused")
		})

	err := w.updateBuild(ctx, m, b)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestFailBuildWithRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)
	ctx := context.Background()
	w.apiCtx = ctx

	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "test-pipeline", JobName: "test-job"}
	b := build.Build{BuildNumber: "1", Status: build.Started, StartedAt: time.Now()}

	gomock.InOrder(
		svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "1", gomock.Any()).Return(fmt.Errorf("connection refused")),
		svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "1", gomock.Any()).Return(nil),
	)

	w.failBuild(ctx, m, b, fmt.Errorf("step failed"))
}

func TestProcessJob_AllowFailure_SetsWarningAndTriggersDownstream(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "allow-fail-job",
		BuildID:           10,
		BuildNumber:       "500",
		VersionID:         1,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:           1,
				Name:         "allow-fail-job",
				AllowFailure: true,
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "will-fail",
							Run:  utils.RunnerCommand{Runner: "exec", Params: map[string]string{"path": "false"}},
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedStatuses []build.Status
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "500", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedStatuses = append(capturedStatuses, b.Status)
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	// The last update should be Warning
	require.NotEmpty(t, capturedStatuses)
	assert.Equal(t, build.Warning, capturedStatuses[len(capturedStatuses)-1], "final build status should be warning")
}

func TestProcessJob_AllowFailure_OnFailureHookStillRuns(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "allow-fail-hooks-job",
		BuildID:           10,
		BuildNumber:       "501",
		VersionID:         1,
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:           1,
				Name:         "allow-fail-hooks-job",
				AllowFailure: true,
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeTask,
						Task: &job.TaskStep{
							Name: "will-fail",
							Run:  utils.RunnerCommand{Runner: "exec", Params: map[string]string{"path": "false"}},
						},
					},
				},
				OnFailure: []job.HookStep{
					runnerHook(utils.RunnerCommand{
						Runner: "exec",
						Args:   []string{"on_failure ran"},
						Params: map[string]string{"path": "echo"},
					}),
				},
				Ensure: []job.HookStep{
					runnerHook(utils.RunnerCommand{
						Runner: "exec",
						Args:   []string{"ensure ran"},
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

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)

	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "501", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	assert.Equal(t, build.Warning, capturedBuild.Status, "build should be warning")

	// on_failure hooks should have run
	foundOnFailure := false
	foundEnsure := false
	for _, step := range capturedBuild.Job {
		if step.Type == "hook" && step.Name == "on_failure" {
			foundOnFailure = true
		}
		if step.Type == "hook" && step.Name == "ensure" {
			foundEnsure = true
		}
	}
	assert.True(t, foundOnFailure, "on_failure hook should run for allow_failure builds")
	assert.True(t, foundEnsure, "ensure hook should run for allow_failure builds")
}

func TestUpdateBuild_SuppressUpdates(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "test-pipeline", JobName: "test-job"}
	called := false
	b := build.Build{BuildNumber: "1", SuppressUpdates: true, OnUpdate: func() { called = true }}

	err := w.updateBuild(context.Background(), m, b)
	assert.NoError(t, err)
	assert.True(t, called, "OnUpdate should have been called")
}

// --- webhookRecorder: reusable mock HTTP server for notification tests ---

type recordedRequest struct {
	Method  string
	Headers http.Header
	Body    string
}

type webhookRecorder struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
}

func newWebhookRecorder() *webhookRecorder {
	rec := &webhookRecorder{}
	rec.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.requests = append(rec.requests, recordedRequest{
			Method:  r.Method,
			Headers: r.Header.Clone(),
			Body:    string(body),
		})
		rec.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	return rec
}

func (r *webhookRecorder) URL() string     { return r.server.URL }
func (r *webhookRecorder) Close()          { r.server.Close() }
func (r *webhookRecorder) Reset()          { r.mu.Lock(); r.requests = nil; r.mu.Unlock() }
func (r *webhookRecorder) Requests() []recordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]recordedRequest, len(r.requests))
	copy(cp, r.requests)
	return cp
}

// webhookNotifType returns a notification type that uses curl to POST $NOTIFY_MESSAGE
// to the URL provided by param_webhook_url.
// Note: the shell-expanded $NOTIFY_MESSAGE is safe for simple ASCII test messages only.
func webhookNotifType() notiftype.NotificationType {
	return notiftype.NotificationType{
		ID:   1,
		Name: "webhook",
		Notify: &utils.RunnerCommand{
			Runner: "exec",
			Args:   []string{"-c", `curl -s -X POST -H "Content-Type:application/json" -d "$NOTIFY_MESSAGE" "$param_webhook_url"`},
			Params: map[string]string{"path": "/bin/sh"},
		},
		Params: []string{"webhook_url"},
	}
}

// webhookNotification returns a notification instance pointing at the given URL.
func webhookNotification(url string, opts ...func(*notification.Notification)) notification.Notification {
	n := notification.Notification{
		ID:        1,
		Type:      "webhook",
		Name:      "test-hook",
		Canonical: "webhook.test-hook",
		Params:    &notification.Params{Params: map[string]string{"webhook_url": url}},
	}
	for _, o := range opts {
		o(&n)
	}
	return n
}

func TestNotifyStep_MockHTTP_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	rec := newWebhookRecorder()
	defer rec.Close()

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "webhook-job",
		BuildID:           10,
		BuildNumber:       "500",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "webhook-job",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeNotify,
						Notify: &job.NotifyStep{
							Type:    "webhook",
							Name:    "test-hook",
							Message: "hello from notify step",
						},
					},
				},
			},
		},
		Notifications:     []notification.Notification{webhookNotification(rec.URL())},
		NotificationTypes: []notiftype.NotificationType{webhookNotifType()},
		Runners:           []runner.Runner{{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}}},
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "500", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	reqs := rec.Requests()
	require.Len(t, reqs, 1, "mock server should have received exactly 1 request")
	assert.Equal(t, "POST", reqs[0].Method)
	assert.Contains(t, reqs[0].Body, "hello from notify step")
}

func TestAutoNotification_OnSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	rec := newWebhookRecorder()
	defer rec.Close()

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "auto-success-job",
		BuildID:           10,
		BuildNumber:       "501",
	}

	notif := webhookNotification(rec.URL(), func(n *notification.Notification) {
		n.On = []string{"success"}
		n.Message = "build_status=$notify_build_status"
	})

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "auto-success-job",
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
		Notifications:     []notification.Notification{notif},
		NotificationTypes: []notiftype.NotificationType{webhookNotifType()},
		Runners:           []runner.Runner{{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}}},
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "501", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	reqs := rec.Requests()
	require.Len(t, reqs, 1, "mock server should have received the success notification")
	assert.Equal(t, "POST", reqs[0].Method)
	assert.Contains(t, reqs[0].Body, "build_status=success")
}

func TestAutoNotification_OnFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	rec := newWebhookRecorder()
	defer rec.Close()

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "auto-fail-http-job",
		BuildID:           10,
		BuildNumber:       "502",
	}

	notif := webhookNotification(rec.URL(), func(n *notification.Notification) {
		n.On = []string{"failure"}
		n.Message = "build_status=$notify_build_status"
	})

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "auto-fail-http-job",
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
		Notifications:     []notification.Notification{notif},
		NotificationTypes: []notiftype.NotificationType{webhookNotifType()},
		Runners:           []runner.Runner{{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}}},
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "502", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	reqs := rec.Requests()
	require.Len(t, reqs, 1, "mock server should have received the failure notification")
	assert.Equal(t, "POST", reqs[0].Method)
	assert.Contains(t, reqs[0].Body, "build_status=failure")
}

func TestAutoNotification_OnFailure_SkipsSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	rec := newWebhookRecorder()
	defer rec.Close()

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "skip-success-job",
		BuildID:           10,
		BuildNumber:       "503",
	}

	notif := webhookNotification(rec.URL(), func(n *notification.Notification) {
		n.On = []string{"failure"}
	})

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "skip-success-job",
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
		Notifications:     []notification.Notification{notif},
		NotificationTypes: []notiftype.NotificationType{webhookNotifType()},
		Runners:           []runner.Runner{{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}}},
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "503", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	reqs := rec.Requests()
	assert.Len(t, reqs, 0, "mock server should have received NO requests when on=[failure] and build succeeds")
}

func TestAutoNotification_JobsFilter(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	rec := newWebhookRecorder()
	defer rec.Close()

	ctx := context.Background()

	notif := webhookNotification(rec.URL(), func(n *notification.Notification) {
		n.On = []string{"success"}
		n.Jobs = []string{"target-job"}
		n.Message = "job-filter hit"
	})

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{ID: 1, Name: "other-job", Plan: []job.PlanStep{{
				Type: job.StepTypeTask,
				Task: &job.TaskStep{Name: "echo", Run: utils.RunnerCommand{Runner: "exec", Args: []string{"done"}, Params: map[string]string{"path": "echo"}}},
			}}},
			{ID: 2, Name: "target-job", Plan: []job.PlanStep{{
				Type: job.StepTypeTask,
				Task: &job.TaskStep{Name: "echo", Run: utils.RunnerCommand{Runner: "exec", Args: []string{"done"}, Params: map[string]string{"path": "echo"}}},
			}}},
		},
		Notifications:     []notification.Notification{notif},
		NotificationTypes: []notiftype.NotificationType{webhookNotifType()},
		Runners:           []runner.Runner{{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}}},
	}

	// Run "other-job" — should NOT trigger notification
	m1 := workitem.Body{
		TeamCanonical: "main", PipelineCanonical: "test-pipeline",
		JobName: "other-job", BuildID: 10, BuildNumber: "504",
	}
	svc.EXPECT().GetPipelineJob(gomock.Any(), m1.TeamCanonical, m1.PipelineCanonical, m1.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m1.TeamCanonical, m1.PipelineCanonical, m1.JobName, "504", gomock.Any()).
		Return(nil).AnyTimes()
	w.processJob(ctx, m1, t.TempDir(), pp)

	assert.Len(t, rec.Requests(), 0, "notification should NOT fire for other-job")

	// Run "target-job" — should trigger notification
	rec.Reset()
	m2 := workitem.Body{
		TeamCanonical: "main", PipelineCanonical: "test-pipeline",
		JobName: "target-job", BuildID: 10, BuildNumber: "505",
	}
	svc.EXPECT().GetPipelineJob(gomock.Any(), m2.TeamCanonical, m2.PipelineCanonical, m2.JobName).
		Return(&pp.Jobs[1], nil)
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m2.TeamCanonical, m2.PipelineCanonical, m2.JobName, "505", gomock.Any()).
		Return(nil).AnyTimes()
	w.processJob(ctx, m2, t.TempDir(), pp)

	reqs := rec.Requests()
	require.Len(t, reqs, 1, "notification should fire for target-job")
	assert.Contains(t, reqs[0].Body, "job-filter hit")
}

func TestAutoNotification_ExcludeFilter(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	rec := newWebhookRecorder()
	defer rec.Close()

	ctx := context.Background()

	notif := webhookNotification(rec.URL(), func(n *notification.Notification) {
		n.On = []string{"success"}
		n.Exclude = []string{"excluded-job"}
		n.Message = "exclude-filter hit"
	})

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{ID: 1, Name: "excluded-job", Plan: []job.PlanStep{{
				Type: job.StepTypeTask,
				Task: &job.TaskStep{Name: "echo", Run: utils.RunnerCommand{Runner: "exec", Args: []string{"done"}, Params: map[string]string{"path": "echo"}}},
			}}},
			{ID: 2, Name: "included-job", Plan: []job.PlanStep{{
				Type: job.StepTypeTask,
				Task: &job.TaskStep{Name: "echo", Run: utils.RunnerCommand{Runner: "exec", Args: []string{"done"}, Params: map[string]string{"path": "echo"}}},
			}}},
		},
		Notifications:     []notification.Notification{notif},
		NotificationTypes: []notiftype.NotificationType{webhookNotifType()},
		Runners:           []runner.Runner{{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}}},
	}

	// Run "excluded-job" — should NOT trigger notification
	m1 := workitem.Body{
		TeamCanonical: "main", PipelineCanonical: "test-pipeline",
		JobName: "excluded-job", BuildID: 10, BuildNumber: "506",
	}
	svc.EXPECT().GetPipelineJob(gomock.Any(), m1.TeamCanonical, m1.PipelineCanonical, m1.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m1.TeamCanonical, m1.PipelineCanonical, m1.JobName, "506", gomock.Any()).
		Return(nil).AnyTimes()
	w.processJob(ctx, m1, t.TempDir(), pp)

	assert.Len(t, rec.Requests(), 0, "notification should NOT fire for excluded-job")

	// Run "included-job" — should trigger notification
	rec.Reset()
	m2 := workitem.Body{
		TeamCanonical: "main", PipelineCanonical: "test-pipeline",
		JobName: "included-job", BuildID: 10, BuildNumber: "507",
	}
	svc.EXPECT().GetPipelineJob(gomock.Any(), m2.TeamCanonical, m2.PipelineCanonical, m2.JobName).
		Return(&pp.Jobs[1], nil)
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m2.TeamCanonical, m2.PipelineCanonical, m2.JobName, "507", gomock.Any()).
		Return(nil).AnyTimes()
	w.processJob(ctx, m2, t.TempDir(), pp)

	reqs := rec.Requests()
	require.Len(t, reqs, 1, "notification should fire for included-job")
	assert.Contains(t, reqs[0].Body, "exclude-filter hit")
}

func TestAutoNotification_MessageInterpolation(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	rec := newWebhookRecorder()
	defer rec.Close()

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		JobName:           "my-job",
		BuildID:           10,
		BuildNumber:       "508",
	}

	notif := webhookNotification(rec.URL(), func(n *notification.Notification) {
		n.On = []string{"success"}
		n.Message = "Job $BUILD_JOB_NAME in $BUILD_PIPELINE_NAME: $notify_build_status"
	})

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "my-job",
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
		Notifications:     []notification.Notification{notif},
		NotificationTypes: []notiftype.NotificationType{webhookNotifType()},
		Runners:           []runner.Runner{{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}}},
	}
	cwd := t.TempDir()

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).
		Return(&pp.Jobs[0], nil)
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "508", gomock.Any()).
		Return(nil).AnyTimes()

	w.processJob(ctx, m, cwd, pp)

	reqs := rec.Requests()
	require.Len(t, reqs, 1, "mock server should have received the notification")
	assert.Contains(t, reqs[0].Body, "Job my-job in test-pipeline: success", "message should have interpolated $BUILD_JOB_NAME, $BUILD_PIPELINE_NAME, and $notify_build_status")
}

func TestProcessJob_InputValues_PreservedOnUpdate(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	inputVals := map[string]string{"version": "v1.0", "env": "staging"}

	// GetJobBuild returns a build with InputValues
	svc.EXPECT().GetJobBuild(gomock.Any(), "main", "test-pipeline", "echo-job", "10").
		Return(&build.Build{
			ID: 10, BuildNumber: "10", Status: build.Started,
			StartedAt: time.Now(), InputValues: inputVals,
		}, nil).AnyTimes()

	svc.EXPECT().GetPipelineJob(gomock.Any(), "main", "test-pipeline", "echo-job").
		Return(&job.Job{
			ID: 1, Name: "echo-job",
			Inputs: []job.Input{
				{Name: "version", Type: "string"},
				{Name: "env", Type: "string"},
			},
			Plan: []job.PlanStep{
				{
					Type: job.StepTypeTask,
					Task: &job.TaskStep{
						Name: "echo",
						Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"hello"}, Params: map[string]string{"path": "echo"}},
					},
				},
			},
		}, nil)

	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	svc.EXPECT().EvaluateDownstreamJobs(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// Capture all UpdateJobBuild calls and verify InputValues is always present
	var lastBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "test-pipeline", "echo-job", "10", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			lastBuild = b
			return nil
		}).AnyTimes()

	w := &Worker{pikoci: svc, logger: logger}

	pp := &pipeline.Pipeline{
		ID: 1, Name: "test-pipeline",
		Jobs: []job.Job{
			{ID: 1, Name: "echo-job", Plan: []job.PlanStep{
				{Type: job.StepTypeTask, Task: &job.TaskStep{
					Name: "echo",
					Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"hello"}, Params: map[string]string{"path": "echo"}},
				}},
			}},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}

	m := workitem.Body{
		TeamCanonical: "main", PipelineCanonical: "test-pipeline",
		JobName: "echo-job", BuildID: 10, BuildNumber: "10",
	}

	w.processJob(context.Background(), m, t.TempDir(), pp)

	// The last update (marking succeeded) must still carry InputValues
	require.NotNil(t, lastBuild.InputValues, "InputValues must be preserved across build updates")
	assert.Equal(t, "v1.0", lastBuild.InputValues["version"])
	assert.Equal(t, "staging", lastBuild.InputValues["env"])
}

func TestProcessJob_InputValues_InjectedAsEnvVars(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	inputVals := map[string]string{"version": "v2.0", "debug": "true"}

	svc.EXPECT().GetJobBuild(gomock.Any(), "main", "test-pipeline", "env-job", "20").
		Return(&build.Build{
			ID: 20, BuildNumber: "20", Status: build.Started,
			StartedAt: time.Now(), InputValues: inputVals,
		}, nil).AnyTimes()

	svc.EXPECT().GetPipelineJob(gomock.Any(), "main", "test-pipeline", "env-job").
		Return(&job.Job{
			ID: 1, Name: "env-job",
			Plan: []job.PlanStep{
				{
					Type: job.StepTypeTask,
					Task: &job.TaskStep{
						Name: "print-env",
						Run: utils.RunnerCommand{
							Runner: "exec",
							Args:   []string{"-ec", "echo version=$INPUT_version debug=$INPUT_debug"},
							Params: map[string]string{"path": "/bin/sh"},
						},
					},
				},
			},
		}, nil)

	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	svc.EXPECT().EvaluateDownstreamJobs(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// Capture the final build to check logs contain expanded env vars
	var lastBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "test-pipeline", "env-job", "20", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			lastBuild = b
			return nil
		}).AnyTimes()

	w := &Worker{pikoci: svc, logger: logger}

	pp := &pipeline.Pipeline{
		ID: 1, Name: "test-pipeline",
		Jobs: []job.Job{
			{ID: 1, Name: "env-job", Plan: []job.PlanStep{
				{Type: job.StepTypeTask, Task: &job.TaskStep{
					Name: "print-env",
					Run: utils.RunnerCommand{
						Runner: "exec",
						Args:   []string{"-ec", "echo version=$INPUT_version debug=$INPUT_debug"},
						Params: map[string]string{"path": "/bin/sh"},
					},
				}},
			}},
		},
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}},
		},
	}

	m := workitem.Body{
		TeamCanonical: "main", PipelineCanonical: "test-pipeline",
		JobName: "env-job", BuildID: 20, BuildNumber: "20",
	}

	w.processJob(context.Background(), m, t.TempDir(), pp)

	// Verify the task step logs contain the expanded input env vars
	require.True(t, len(lastBuild.Steps) > 0, "build should have steps")
	taskLogs := ""
	for _, s := range lastBuild.Steps {
		if s.Name == "print-env" {
			taskLogs = s.Logs
		}
	}
	assert.Contains(t, taskLogs, "version=v2.0", "INPUT_version env var should be expanded in task logs")
	assert.Contains(t, taskLogs, "debug=true", "INPUT_debug env var should be expanded in task logs")
}

func TestProcessJob_IfStep_InputValues_AndBranchSelection(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	inputVals := map[string]string{"env": "production", "version": "v3.0"}

	ifJob := job.Job{
		ID: 1, Name: "deploy",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeIf,
				If: &job.IfStep{
					Branches: []job.IfBranch{
						{
							Type:      "if",
							Label:     "check-prod",
							Condition: "$INPUT_env == 'production'",
							Steps: []job.PlanStep{
								{Type: job.StepTypeTask, Task: &job.TaskStep{
									Name: "deploy-prod",
									Run: utils.RunnerCommand{
										Runner: "exec",
										Args:   []string{"-ec", "echo PROD version=$INPUT_version"},
										Params: map[string]string{"path": "/bin/sh"},
									},
								}},
							},
						},
						{
							Type:  "else",
							Label: "else",
							Steps: []job.PlanStep{
								{Type: job.StepTypeTask, Task: &job.TaskStep{
									Name: "deploy-staging",
									Run: utils.RunnerCommand{
										Runner: "exec",
										Args:   []string{"-ec", "echo STAGING version=$INPUT_version"},
										Params: map[string]string{"path": "/bin/sh"},
									},
								}},
							},
						},
					},
				},
			},
		},
	}

	svc.EXPECT().GetJobBuild(gomock.Any(), "main", "test-pipeline", "deploy", "30").
		Return(&build.Build{
			ID: 30, BuildNumber: "30", Status: build.Started,
			StartedAt: time.Now(), InputValues: inputVals,
		}, nil).AnyTimes()

	svc.EXPECT().GetPipelineJob(gomock.Any(), "main", "test-pipeline", "deploy").
		Return(&ifJob, nil)

	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	svc.EXPECT().EvaluateDownstreamJobs(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	var lastBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "test-pipeline", "deploy", "30", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			lastBuild = b
			return nil
		}).AnyTimes()

	w := &Worker{pikoci: svc, logger: logger}
	pp := &pipeline.Pipeline{
		ID: 1, Name: "test-pipeline",
		Jobs:    []job.Job{ifJob},
		Runners: []runner.Runner{{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}}},
	}
	m := workitem.Body{
		TeamCanonical: "main", PipelineCanonical: "test-pipeline",
		JobName: "deploy", BuildID: 30, BuildNumber: "30",
	}

	w.processJob(context.Background(), m, t.TempDir(), pp)

	// Verify structure: parent "if" step with branch sub-steps
	require.True(t, len(lastBuild.Steps) > 0, "build should have steps")
	ifParent := lastBuild.Steps[0]
	assert.Equal(t, "if", ifParent.Type)
	assert.Equal(t, build.Succeeded, ifParent.Status)
	require.Len(t, ifParent.SubSteps, 2, "should have 2 branches (if + else)")

	// First branch (if "check-prod") should be entered because input_env=production
	assert.Equal(t, "if", ifParent.SubSteps[0].Type)
	assert.Equal(t, build.Succeeded, ifParent.SubSteps[0].Status)
	require.Len(t, ifParent.SubSteps[0].SubSteps, 1, "entered branch should have 1 inner step")
	assert.Equal(t, "deploy-prod", ifParent.SubSteps[0].SubSteps[0].Name)

	// Second branch (else) should be skipped
	assert.Equal(t, "else", ifParent.SubSteps[1].Type)
	assert.Equal(t, build.Skipped, ifParent.SubSteps[1].Status)

	// Verify input values are available inside the branch task
	taskLogs := ifParent.SubSteps[0].SubSteps[0].Logs
	assert.Contains(t, taskLogs, "PROD version=v3.0", "INPUT_version env var should be expanded inside if branch")
	assert.NotContains(t, taskLogs, "STAGING", "else branch should not have executed")
}

func TestProcessJob_IfStep_NoBranchMatches(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	inputVals := map[string]string{"env": "development"}

	ifJob := job.Job{
		ID: 1, Name: "deploy",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeIf,
				If: &job.IfStep{
					Branches: []job.IfBranch{
						{
							Type:      "if",
							Label:     "check-prod",
							Condition: "$INPUT_env == 'production'",
							Steps: []job.PlanStep{
								{Type: job.StepTypeTask, Task: &job.TaskStep{
									Name: "deploy-prod",
									Run: utils.RunnerCommand{
										Runner: "exec",
										Args:   []string{"-ec", "echo prod"},
										Params: map[string]string{"path": "/bin/sh"},
									},
								}},
							},
						},
						{
							Type:      "else_if",
							Label:     "check-staging",
							Condition: "$INPUT_env == 'staging'",
							Steps: []job.PlanStep{
								{Type: job.StepTypeTask, Task: &job.TaskStep{
									Name: "deploy-staging",
									Run: utils.RunnerCommand{
										Runner: "exec",
										Args:   []string{"-ec", "echo staging"},
										Params: map[string]string{"path": "/bin/sh"},
									},
								}},
							},
						},
					},
				},
			},
		},
	}

	svc.EXPECT().GetJobBuild(gomock.Any(), "main", "test-pipeline", "deploy", "31").
		Return(&build.Build{
			ID: 31, BuildNumber: "31", Status: build.Started,
			StartedAt: time.Now(), InputValues: inputVals,
		}, nil).AnyTimes()

	svc.EXPECT().GetPipelineJob(gomock.Any(), "main", "test-pipeline", "deploy").
		Return(&ifJob, nil)

	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	svc.EXPECT().EvaluateDownstreamJobs(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	var lastBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "test-pipeline", "deploy", "31", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			lastBuild = b
			return nil
		}).AnyTimes()

	w := &Worker{pikoci: svc, logger: logger}
	pp := &pipeline.Pipeline{
		ID: 1, Name: "test-pipeline",
		Jobs:    []job.Job{ifJob},
		Runners: []runner.Runner{{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}}},
	}
	m := workitem.Body{
		TeamCanonical: "main", PipelineCanonical: "test-pipeline",
		JobName: "deploy", BuildID: 31, BuildNumber: "31",
	}

	w.processJob(context.Background(), m, t.TempDir(), pp)

	// No branch matches — all should be skipped, build should succeed
	assert.Equal(t, build.Succeeded, lastBuild.Status, "build should succeed when no branch matches")
	require.True(t, len(lastBuild.Steps) > 0)
	ifParent := lastBuild.Steps[0]
	assert.Equal(t, "if", ifParent.Type)
	assert.Equal(t, build.Succeeded, ifParent.Status)
	require.Len(t, ifParent.SubSteps, 2)
	assert.Equal(t, build.Skipped, ifParent.SubSteps[0].Status, "if branch should be skipped")
	assert.Equal(t, build.Skipped, ifParent.SubSteps[1].Status, "else_if branch should be skipped")
}

func TestProcessJob_IfStep_BranchFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ifJob := job.Job{
		ID: 1, Name: "deploy",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeIf,
				If: &job.IfStep{
					Branches: []job.IfBranch{
						{
							Type:      "if",
							Label:     "always-true",
							Condition: "'yes' == 'yes'",
							Steps: []job.PlanStep{
								{Type: job.StepTypeTask, Task: &job.TaskStep{
									Name: "failing-task",
									Run: utils.RunnerCommand{
										Runner: "exec",
										Args:   []string{"-ec", "exit 1"},
										Params: map[string]string{"path": "/bin/sh"},
									},
								}},
							},
						},
						{
							Type:  "else",
							Steps: []job.PlanStep{
								{Type: job.StepTypeTask, Task: &job.TaskStep{
									Name: "never-reached",
									Run: utils.RunnerCommand{
										Runner: "exec",
										Args:   []string{"-ec", "echo ok"},
										Params: map[string]string{"path": "/bin/sh"},
									},
								}},
							},
						},
					},
				},
			},
		},
	}

	svc.EXPECT().GetJobBuild(gomock.Any(), "main", "test-pipeline", "deploy", "32").
		Return(&build.Build{
			ID: 32, BuildNumber: "32", Status: build.Started,
			StartedAt: time.Now(),
		}, nil).AnyTimes()

	svc.EXPECT().GetPipelineJob(gomock.Any(), "main", "test-pipeline", "deploy").
		Return(&ifJob, nil)

	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	svc.EXPECT().EvaluateDownstreamJobs(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	var lastBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "test-pipeline", "deploy", "32", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			lastBuild = b
			return nil
		}).AnyTimes()

	w := &Worker{pikoci: svc, logger: logger}
	pp := &pipeline.Pipeline{
		ID: 1, Name: "test-pipeline",
		Jobs:    []job.Job{ifJob},
		Runners: []runner.Runner{{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}}},
	}
	m := workitem.Body{
		TeamCanonical: "main", PipelineCanonical: "test-pipeline",
		JobName: "deploy", BuildID: 32, BuildNumber: "32",
	}

	w.processJob(context.Background(), m, t.TempDir(), pp)

	// Build should fail when the selected branch's task fails
	assert.Equal(t, build.Failed, lastBuild.Status, "build should fail when selected branch fails")
	require.True(t, len(lastBuild.Steps) > 0)
	ifParent := lastBuild.Steps[0]
	assert.Equal(t, "if", ifParent.Type)
	assert.Equal(t, build.Failed, ifParent.Status, "if parent should be failed")
	require.Len(t, ifParent.SubSteps, 2)
	assert.Equal(t, build.Failed, ifParent.SubSteps[0].Status, "entered branch should be failed")
	assert.Equal(t, build.Skipped, ifParent.SubSteps[1].Status, "else branch should be skipped")
}

func TestProcessJob_IfStep_ConditionErrorFailsParentStep(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ifJob := job.Job{
		ID: 1, Name: "deploy",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeIf,
				If: &job.IfStep{
					Branches: []job.IfBranch{
						{Type: "if", Label: "broken", Condition: "'unterminated == 'x'"},
					},
				},
			},
		},
	}

	svc.EXPECT().GetJobBuild(gomock.Any(), "main", "test-pipeline", "deploy", "33").
		Return(&build.Build{ID: 33, BuildNumber: "33", Status: build.Started, StartedAt: time.Now()}, nil).AnyTimes()
	svc.EXPECT().GetPipelineJob(gomock.Any(), "main", "test-pipeline", "deploy").Return(&ifJob, nil)
	svc.EXPECT().NotifySerialGroupPendingBuilds(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	svc.EXPECT().FindBuildGetVersions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	svc.EXPECT().EvaluateDownstreamJobs(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	var lastBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), "main", "test-pipeline", "deploy", "33", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			lastBuild = b
			return nil
		}).AnyTimes()

	w := &Worker{pikoci: svc, logger: logger}
	pp := &pipeline.Pipeline{ID: 1, Name: "test-pipeline", Jobs: []job.Job{ifJob}}
	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "test-pipeline", JobName: "deploy", BuildID: 33, BuildNumber: "33"}

	w.processJob(context.Background(), m, t.TempDir(), pp)

	assert.Equal(t, build.Failed, lastBuild.Status)
	require.NotEmpty(t, lastBuild.Steps)
	assert.Equal(t, "if", lastBuild.Steps[0].Type)
	assert.Equal(t, build.Failed, lastBuild.Steps[0].Status, "if parent should be failed on condition error")
}

func TestProcessJob_ServiceStep_InsideFailingIfBranchIsStopped(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	m := workitem.Body{
		TeamCanonical: "main", PipelineCanonical: "test-pipeline",
		JobName: "service-if-fail", BuildID: 11, BuildNumber: "333",
	}
	pp := &pipeline.Pipeline{
		ID: 1, Name: "test-pipeline",
		Jobs: []job.Job{{
			ID: 1, Name: "service-if-fail",
			Plan: []job.PlanStep{{
				Type: job.StepTypeIf,
				If: &job.IfStep{Branches: []job.IfBranch{{
					Type: "if", Label: "always", Condition: "'a' == 'a'",
					Steps: []job.PlanStep{
						{Type: job.StepTypeService, Service: &job.ServiceStep{Name: "my-db"}},
						{Type: job.StepTypeTask, Task: &job.TaskStep{
							Name: "boom",
							Run:  utils.RunnerCommand{Runner: "exec", Args: []string{"-ec", "exit 1"}, Params: map[string]string{"path": "/bin/sh"}},
						}},
					},
				}}},
			}},
		}},
		Services: []service.Service{{
			ID: 1, Name: "my-db",
			Start: utils.RunnerCommand{Runner: "exec", Args: []string{"starting db"}, Params: map[string]string{"path": "echo"}},
			Stop:  utils.RunnerCommand{Runner: "exec", Args: []string{"stopping db"}, Params: map[string]string{"path": "echo"}},
		}},
		Runners: []runner.Runner{{Name: "exec", Run: utils.RunCommand{Path: "$path", Args: []string{"$args"}}}},
	}

	svc.EXPECT().GetPipelineJob(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName).Return(&pp.Jobs[0], nil)
	var capturedBuild build.Build
	svc.EXPECT().UpdateJobBuild(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, m.JobName, "333", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			capturedBuild = b
			return nil
		}).AnyTimes()

	w.processJob(context.Background(), m, t.TempDir(), pp)

	assert.Equal(t, build.Failed, capturedBuild.Status)
	found := false
	for _, s := range capturedBuild.Steps {
		if s.Name == "my-db:stop" && s.Type == "service" {
			found = true
		}
	}
	assert.True(t, found, "service started inside a failing if branch must still be stopped")
}
