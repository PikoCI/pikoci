package pikoci

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pikoci/pikoci/pikoci/auditlog"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/utils"
)

var (
	// ErrConcurrencyLimit is returned when a build cannot be started because the
	// job's concurrency limit has been reached.
	ErrConcurrencyLimit = errors.New("concurrency limit reached")
	// ErrBuildNotPending is returned when attempting to start a build that is not
	// in the pending state.
	ErrBuildNotPending = build.ErrNotPending
	// ErrJobPaused is returned when attempting to start a build for a paused job.
	ErrJobPaused = errors.New("job is paused")
	// ErrSerialGroupLimit is returned when a build cannot be started because
	// another job in the same serial group already has a running build.
	ErrSerialGroupLimit = errors.New("serial group limit reached")
)

// CreateJobBuild creates a new pending build for the specified job within a unit
// of work. The build status is always set to pending regardless of the input.
func (q *PikoCI) CreateJobBuild(ctx context.Context, tc, pc, jn string, b build.Build) (*build.Build, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	b.Status = build.Pending

	// Check if the job has an approval gate — if so, set WaitingForApproval
	j, jErr := q.Jobs.Find(ctx, tc, pc, jn)
	if jErr == nil && j.ApproveLabel != "" {
		b.Status = build.WaitingForApproval
	}

	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		id, buildNumber, err := uow.Builds().Create(ctx, tc, pc, jn, b)
		if err != nil {
			return fmt.Errorf("failed to Create Build: %w", err)
		}

		b.ID = id
		b.BuildNumber = buildNumber
		return nil
	})
	if err != nil {
		return nil, err
	}

	if b.Status != build.WaitingForApproval {
		q.Notifier.Notify()
	}

	return &b, nil
}

// ListJobBuilds returns paginated builds for a job, supporting cursor-based
// pagination with before and after parameters. Results are returned in
// newest-first order. The boolean return value indicates whether more results
// exist beyond the requested page.
func (q *PikoCI) ListJobBuilds(ctx context.Context, tc, pc, jn string, before *uint32, after *uint32, limit uint32) ([]*build.Build, bool, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, false, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, false, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, false, fmt.Errorf("invalid Job Name format %q", jn)
	}

	fetchLimit := limit
	if limit > 0 {
		fetchLimit = limit + 1
	}

	builds, err := q.Builds.Filter(ctx, tc, pc, jn, before, after, fetchLimit)
	if err != nil {
		return nil, false, fmt.Errorf("failed to list Builds: %w", err)
	}

	hasMore := false
	if limit > 0 && uint32(len(builds)) > limit {
		hasMore = true
		builds = builds[:limit]
	}

	// For "after" queries the DB returns ASC order; reverse to newest-first
	if after != nil {
		slices.Reverse(builds)
	}

	return builds, hasMore, nil
}

// GetJobBuild retrieves a single build by its build number.
func (q *PikoCI) GetJobBuild(ctx context.Context, tc, pc, jn string, buildNumber string) (*build.Build, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	b, err := q.Builds.Find(ctx, tc, pc, jn, buildNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to Find Build: %w", err)
	}
	approvals, aErr := q.Builds.FindApprovals(ctx, b.ID)
	if aErr == nil && len(approvals) > 0 {
		b.Approvals = approvals
	}
	return b, nil
}

// CancelJobBuild cancels a running or pending build and notifies the next
// pending build in the queue so it can potentially start.
func (q *PikoCI) CancelJobBuild(ctx context.Context, tc, pc, jn string, buildNumber string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}

	b, err := q.Builds.Find(ctx, tc, pc, jn, buildNumber)
	if err != nil {
		return fmt.Errorf("failed to Find Build: %w", err)
	}
	wasRunning := b.Status == build.Started
	if b.Status != build.Started && b.Status != build.Pending && b.Status != build.WaitingForApproval {
		return fmt.Errorf("build %s is not running, pending, or waiting for approval (status: %s)", buildNumber, b.Status)
	}
	b.Status = build.Cancelled
	if wasRunning {
		b.Duration = time.Since(b.StartedAt)
	}
	if err := q.Builds.Update(ctx, tc, pc, jn, buildNumber, *b); err != nil {
		return err
	}

	// If the build was running, push cancellation to the worker via gRPC stream.
	if wasRunning && q.GRPCServer != nil {
		buildID := fmt.Sprintf("%d", b.ID)
		if err := q.GRPCServer.CancelBuild(buildID, "user cancelled"); err != nil {
			q.logger.Debug("gRPC cancel routing failed (worker may be embedded or disconnected)",
				"build_id", buildID, "error", err)
		}
	}

	// Notify the next pending build so it can start (whether we cancelled
	// a running or a pending build, a concurrency slot may have opened).
	q.notifyNextPendingBuild(ctx, tc, pc, jn)
	q.audit(ctx, tc, auditlog.JobCancelled, "job", pc+"/"+jn,
		map[string]interface{}{"pipeline": pc, "build_number": buildNumber})
	return nil
}

// UpdateJobBuild updates an existing build's state and metadata. It prevents
// workers from overwriting a cancelled build back to a non-terminal status and
// automatically computes the duration for completed builds.
func (q *PikoCI) UpdateJobBuild(ctx context.Context, tc, pc, jn string, buildNumber string, b build.Build) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}

	// Prevent worker from overwriting a cancelled build back to a non-terminal status
	existing, err := q.Builds.Find(ctx, tc, pc, jn, buildNumber)
	if err == nil && existing.Status == build.Cancelled {
		b.Status = build.Cancelled
	}

	if b.Status != build.Started && b.Duration == 0 {
		b.Duration = time.Since(b.StartedAt)
	}

	if err = q.Builds.Update(ctx, tc, pc, jn, buildNumber, b); err != nil {
		return fmt.Errorf("failed to Update Build: %w", err)
	}

	// When a build completes, a concurrency slot may have opened.
	// Notify polling workers so pending builds can start.
	q.Notifier.Notify()

	return nil
}

// InsertBuildGetVersion records the resource version fetched by a get step
// during a build execution.
func (q *PikoCI) InsertBuildGetVersion(ctx context.Context, tc, pc, jn string, buildID uint32, stepName string, versionID uint32) error {
	return q.Builds.InsertGetVersion(ctx, tc, pc, jn, buildID, stepName, versionID)
}

// DeleteJobBuild removes a build by its build number.
func (q *PikoCI) DeleteJobBuild(ctx context.Context, tc, pc, jn string, buildNumber string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}

	err := q.Builds.Delete(ctx, tc, pc, jn, buildNumber)
	if err != nil {
		return fmt.Errorf("failed to Delete Build: %w", err)
	}

	return nil
}

// RetryJobBuild creates a retry of a completed build and enqueues it for
// execution. The retry inherits the resource versions from the original parent
// build to ensure consistency.
func (q *PikoCI) RetryJobBuild(ctx context.Context, tc, pc, jn, buildNumber string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}

	b, err := q.Builds.Find(ctx, tc, pc, jn, buildNumber)
	if err != nil {
		return fmt.Errorf("failed to Find Build: %w", err)
	}
	if b.Status == build.Started || b.Status == build.Pending || b.Status == build.WaitingForApproval {
		return fmt.Errorf("build %s is still running, pending, or waiting for approval", buildNumber)
	}

	// Extract parent build number: if "3.1" -> "3", if "3" -> "3"
	parentBN := buildNumber
	if idx := strings.Index(buildNumber, "."); idx != -1 {
		parentBN = buildNumber[:idx]
	}

	// Find the parent build to get its ID for version pinning
	parentBuild, err := q.Builds.Find(ctx, tc, pc, jn, parentBN)
	if err != nil {
		return fmt.Errorf("failed to Find parent Build %q: %w", parentBN, err)
	}

	// Create a pending retry build first
	_, err = q.CreateRetryJobBuild(ctx, tc, pc, jn, parentBN, build.Build{Status: build.Pending, RetrySourceBuildID: parentBuild.ID})
	if err != nil {
		return fmt.Errorf("failed to create pending retry build: %w", err)
	}

	q.Notifier.Notify()

	q.audit(ctx, tc, auditlog.JobRetried, "job", pc+"/"+jn,
		map[string]interface{}{"pipeline": pc, "build_number": buildNumber})
	return nil
}

// CreateRetryJobBuild creates a retry build under the given parent build number
// within a unit of work. The retry build number is formatted as "parent.N"
// where N is the retry sequence number.
func (q *PikoCI) CreateRetryJobBuild(ctx context.Context, tc, pc, jn, parentBuildNumber string, b build.Build) (*build.Build, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	b.Status = build.Pending

	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		id, buildNumber, err := uow.Builds().CreateRetry(ctx, tc, pc, jn, parentBuildNumber, b)
		if err != nil {
			return fmt.Errorf("failed to Create Retry Build: %w", err)
		}

		b.ID = id
		b.BuildNumber = buildNumber
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &b, nil
}

// FindBuildGetVersions returns the resource version IDs fetched during get
// steps of the specified build, keyed by step name.
func (q *PikoCI) FindBuildGetVersions(ctx context.Context, tc, pc, jn string, buildID uint32) (map[string]uint32, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	return q.Builds.FindGetVersions(ctx, buildID)
}

// StartPendingBuild transitions a pending build to started status. It checks the
// job's concurrency limit and returns ErrConcurrencyLimit if the limit has been
// reached.
func (q *PikoCI) StartPendingBuild(ctx context.Context, tc, pn, jn string, buildID uint32) (*build.Build, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pn)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		j, err := uow.Jobs().Find(ctx, tc, pn, jn)
		if err != nil {
			return fmt.Errorf("failed to find job: %w", err)
		}
		if j.Paused {
			return ErrJobPaused
		}
		if j.Concurrency > 0 {
			running, err := uow.Builds().CountRunning(ctx, tc, pn, jn)
			if err != nil {
				return fmt.Errorf("failed to count running builds: %w", err)
			}
			if running >= j.Concurrency {
				return ErrConcurrencyLimit
			}
		}
		if len(j.SerialGroups) > 0 {
			sgRunning, err := uow.Builds().CountRunningInSerialGroups(ctx, tc, pn, j.SerialGroups, jn)
			if err != nil {
				return fmt.Errorf("failed to count running builds in serial groups: %w", err)
			}
			if sgRunning > 0 {
				return ErrSerialGroupLimit
			}
		}

		return uow.Builds().StartPending(ctx, tc, pn, jn, buildID)
	})
	if err != nil {
		return nil, err
	}

	return q.Builds.FindByID(ctx, buildID)
}

// FindOldestPendingBuild returns the oldest pending build for the specified job,
// or nil if no pending builds exist.
func (q *PikoCI) FindOldestPendingBuild(ctx context.Context, tc, pn, jn string) (*build.Build, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pn)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	return q.Builds.FindOldestPending(ctx, tc, pn, jn)
}

// notifyNextPendingBuild finds the oldest pending build for the job and sends a
// message to the job topic so it can be picked up for execution. This is called
// after a build is cancelled or completes to fill any freed concurrency slots.
// It also notifies pending builds for other jobs sharing serial groups.
func (q *PikoCI) notifyNextPendingBuild(ctx context.Context, tc, pc, jn string) {
	q.sendPendingBuildNotification(ctx, tc, pc, jn)
	q.NotifySerialGroupPendingBuilds(ctx, tc, pc, jn)
}

// sendPendingBuildNotification finds the oldest pending build for a single job
// and sends a message to the job topic.
func (q *PikoCI) sendPendingBuildNotification(ctx context.Context, tc, pc, jn string) {
	q.Notifier.Notify()
}

// NotifySerialGroupPendingBuilds finds all jobs sharing serial groups with the
// given job and enqueues their oldest pending builds for execution.
func (q *PikoCI) NotifySerialGroupPendingBuilds(ctx context.Context, tc, pc, jn string) {
	j, err := q.Jobs.Find(ctx, tc, pc, jn)
	if err != nil || len(j.SerialGroups) == 0 {
		return
	}
	peerJobs, err := q.Jobs.FindJobsBySerialGroups(ctx, tc, pc, j.SerialGroups)
	if err != nil {
		return
	}
	for _, pj := range peerJobs {
		if pj.Name == jn {
			continue
		}
		q.sendPendingBuildNotification(ctx, tc, pc, pj.Name)
	}
}

// ReEnqueuePendingBuilds scans all jobs for pending builds and re-publishes
// queue messages so workers can pick them up. This is called on server startup
// to recover builds that were stranded when the previous server instance stopped.
//
// This is safe to call even if some messages are still in the queue: the worker
// does not trust the BuildID in the message — it always calls FindOldestPending
// on the DB to determine which build to run (worker/service.go:238-266).
// Duplicate messages simply result in redundant DB lookups with no side effects.
func (q *PikoCI) ReEnqueuePendingBuilds(ctx context.Context) error {
	q.Notifier.Notify()
	return nil
}

// CountStartedBuilds returns the total number of builds with status "started" across all jobs.
func (q *PikoCI) CountStartedBuilds(ctx context.Context) (int, error) {
	return q.Builds.CountStarted(ctx)
}

// RecoverOrphanedBuilds fails all builds stuck in "started" status. This is
// used on startup to clean up builds orphaned by a previous server crash, and
// during graceful shutdown when the drain timeout expires.
func (q *PikoCI) RecoverOrphanedBuilds(ctx context.Context) (int, error) {
	return q.Builds.FailStartedBuilds(ctx, "server shutdown: build was orphaned")
}

// resolvePassedJobNames expands for_each group names in a passed list to all
// instance names. If a name matches a for_each group, it is replaced by all
// instance names in that group. Non-group names are kept as-is.
func resolvePassedJobNames(passed []string, jobs []job.Job) []string {
	groupInstances := make(map[string][]string)
	for _, j := range jobs {
		if j.ForEachGroup != "" {
			groupInstances[j.ForEachGroup] = append(groupInstances[j.ForEachGroup], j.Name)
		}
	}

	var expanded []string
	for _, name := range passed {
		if instances, ok := groupInstances[name]; ok {
			expanded = append(expanded, instances...)
		} else {
			expanded = append(expanded, name)
		}
	}
	return expanded
}

// jobReferencedInPassed checks whether completedJobName is referenced in a
// passed list, either directly or as a for_each instance of a group name.
func jobReferencedInPassed(completedJobName string, passed []string, allJobs []job.Job) bool {
	for _, p := range passed {
		if p == completedJobName {
			return true
		}
	}
	for _, pj := range allJobs {
		if pj.Name == completedJobName && pj.ForEachGroup != "" {
			for _, p := range passed {
				if p == pj.ForEachGroup {
					return true
				}
			}
		}
	}
	return false
}

// EvaluateDownstreamJobs checks all jobs in the pipeline that have passed
// constraints referencing completedJobName. For each that is ready (all
// upstream jobs succeeded with a common version), it creates a pending build.
func (q *PikoCI) EvaluateDownstreamJobs(ctx context.Context, tc, pn, completedJobName string) error {
	pp, err := q.Pipelines.Find(ctx, tc, pn)
	if err != nil {
		return fmt.Errorf("failed to find pipeline %q: %w", pn, err)
	}

	for _, j := range pp.Jobs {
		if j.Paused {
			continue
		}
		if err := q.evaluateJobDownstream(ctx, tc, pn, completedJobName, &j, pp.Jobs); err != nil {
			q.logger.Error("failed to evaluate downstream job",
				"pipeline", pn, "job", j.Name, "completed", completedJobName, "error", err)
		}
	}
	return nil
}

func (q *PikoCI) evaluateJobDownstream(ctx context.Context, tc, pn, completedJobName string, j *job.Job, allJobs []job.Job) error {
	// Pre-check: does this job have ANY get step with passed+trigger that
	// references completedJobName? If not, skip entirely.
	relevant := false
	for _, ps := range j.FlatPlanSteps() {
		if ps.Type != job.StepTypeGet || ps.Get == nil {
			continue
		}
		g := ps.Get
		if len(g.Passed) == 0 || !g.Trigger {
			continue
		}
		if jobReferencedInPassed(completedJobName, g.Passed, allJobs) {
			relevant = true
			break
		}
	}
	if !relevant {
		return nil
	}

	// Job is relevant. Now evaluate ALL get steps with passed+trigger —
	// ALL must be ready for the job to trigger (same as original evaluateJob).
	type candidate struct {
		stepName  string
		versionID uint32
	}
	var candidates []candidate

	for _, ps := range j.FlatPlanSteps() {
		if ps.Type != job.StepTypeGet || ps.Get == nil {
			continue
		}
		g := ps.Get
		if len(g.Passed) == 0 || !g.Trigger {
			continue
		}

		expandedPassed := resolvePassedJobNames(g.Passed, allJobs)

		versionID, ready, err := q.Builds.FindReadyDownstreamVersion(
			ctx, tc, pn,
			expandedPassed, j.Name, g.Name, len(expandedPassed),
			j.BaselineVersionID,
		)
		if err != nil {
			return fmt.Errorf("FindReadyDownstreamVersion: %w", err)
		}
		if !ready {
			return nil
		}

		rCan := g.ResourceCanonical()
		res, err := q.Resources.Find(ctx, tc, pn, rCan)
		if err != nil {
			return fmt.Errorf("resource pin check for %q: %w", rCan, err)
		}
		if res.PinnedVersionID != nil && versionID != *res.PinnedVersionID {
			return nil
		}

		candidates = append(candidates, candidate{g.Name, versionID})
	}

	if len(candidates) == 0 {
		return nil
	}

	versionID := candidates[0].versionID

	// Atomic check-and-create to prevent duplicate pending/waiting builds
	// from concurrent callers (multiple workers or worker + scheduler).
	var triggered bool
	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		pending, err := uow.Builds().FindOldestPending(ctx, tc, pn, j.Name)
		if err != nil {
			return fmt.Errorf("FindOldestPending: %w", err)
		}
		if pending != nil {
			return nil
		}
		// Also check for existing waiting-for-approval builds (FindOldestPending
		// only returns pending builds; workers must not see waiting builds).
		if j.ApproveLabel != "" {
			latest, lErr := uow.Builds().Filter(ctx, tc, pn, j.Name, nil, nil, 1)
			if lErr == nil && len(latest) > 0 && latest[0].Status == build.WaitingForApproval {
				return nil
			}
		}

		status := build.Pending
		if j.ApproveLabel != "" {
			status = build.WaitingForApproval
		}
		id, buildNumber, err := uow.Builds().Create(ctx, tc, pn, j.Name, build.Build{
			Status:    status,
			VersionID: versionID,
		})
		if err != nil {
			return fmt.Errorf("create pending build: %w", err)
		}

		q.logger.Info("triggered downstream job",
			"pipeline", pn, "job", j.Name, "version_id", versionID,
			"build_id", id, "build_number", buildNumber,
			"completed_job", completedJobName)

		triggered = true
		return nil
	})
	if err != nil {
		return err
	}
	if triggered && j.ApproveLabel == "" {
		q.Notifier.Notify()
	}
	return nil
}
