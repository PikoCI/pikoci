package build

import (
	"context"
	"time"
)

//go:generate go tool mockgen -destination=../mock/build_repository.go -mock_names=Repository=BuildRepository -package mock github.com/pikoci/pikoci/pikoci/build Repository

// Repository defines the persistence operations for builds.
type Repository interface {
	// Create persists a new build for the given team, pipeline, and job, returning the build ID and build number.
	Create(ctx context.Context, tc, pn, jn string, b Build) (uint32, string, error)
	// CreateRetry persists a retry of an existing build, linking it to the parent build number.
	CreateRetry(ctx context.Context, tc, pn, jn, parentBuildNumber string, b Build) (uint32, string, error)
	// Find retrieves a single build by team, pipeline, job, and build number.
	Find(ctx context.Context, tc, pn, jn string, buildNumber string) (*Build, error)
	// Filter returns a paginated list of builds for the given team, pipeline, and job.
	// When statuses is non-empty, only builds matching one of the given statuses are returned.
	Filter(ctx context.Context, tc, pn, jn string, before *uint32, after *uint32, limit uint32, statuses []Status) ([]*Build, error)
	// Update updates an existing build identified by team, pipeline, job, and build number.
	Update(ctx context.Context, tc, pn, jn string, buildNumber string, b Build) error
	// Delete removes a build identified by team, pipeline, job, and build number.
	Delete(ctx context.Context, tc, pn, jn string, buildNumber string) error
	// InsertGetVersion records the resource version fetched by a get step during a build.
	InsertGetVersion(ctx context.Context, tc, pn, jn string, buildID uint32, stepName string, versionID uint32) error
	// FindGetVersions returns a map of step names to version IDs for all get steps in a build.
	FindGetVersions(ctx context.Context, buildID uint32) (map[string]uint32, error)
	// FindReadyDownstreamVersion finds a version that has passed through all upstream jobs and is ready for the downstream job.
	FindReadyDownstreamVersion(ctx context.Context, tc, pn string, upstreamJobs []string, downstreamJob string, stepName string, upstreamCount int, baselineVersionID *uint32) (uint32, bool, error)
	// LastBuildAtByPipeline returns the most recent build timestamp for each pipeline in a team.
	LastBuildAtByPipeline(ctx context.Context, tc string) (map[uint32]time.Time, error)
	// CountRunning returns the number of currently running builds for a given job.
	CountRunning(ctx context.Context, tc, pn, jn string) (int, error)
	// FindByID retrieves a single build by its unique ID.
	FindByID(ctx context.Context, buildID uint32) (*Build, error)
	// FindOldestPending returns the oldest pending build for the given job, or nil if none exists.
	FindOldestPending(ctx context.Context, tc, pn, jn string) (*Build, error)
	// StartPending transitions a pending build to started status.
	StartPending(ctx context.Context, tc, pn, jn string, buildID uint32) error
	// CountRunningInSerialGroups returns the number of running builds for jobs (excluding excludeJobName)
	// that share any of the given serial groups within the same pipeline.
	CountRunningInSerialGroups(ctx context.Context, tc, pn string, serialGroups []string, excludeJobName string) (int, error)
	// AggregateStatusByVersionIDs returns the aggregate build status for each version ID.
	AggregateStatusByVersionIDs(ctx context.Context, versionIDs []uint32) (map[uint32]string, error)
	// FindByVersionAndJobs returns builds for the given version ID across the
	// specified jobs, including retries of matched builds.
	// The result maps job name to a slice of builds (main build first, retries after).
	FindByVersionAndJobs(ctx context.Context, tc, pn string, versionID uint32, jobNames []string) (map[string][]*Build, error)
	// FilterByPipeline returns builds across all jobs in a pipeline, optionally filtered by status.
	// Results are grouped by job name and ordered by build ID descending within each job.
	FilterByPipeline(ctx context.Context, tc, pn string, statuses []Status) (map[string][]*Build, error)
	// LatestBuildStatusByPipeline returns all builds for a pipeline with only ID, BuildNumber,
	// and Status fields populated. Callers must not rely on any other Build fields being populated.
	// Used for pipeline image generation where steps data is not needed.
	LatestBuildStatusByPipeline(ctx context.Context, tc, pn string) (map[string][]*Build, error)
	// CountStarted returns the total number of builds with status "started" across all jobs.
	CountStarted(ctx context.Context) (int, error)
	// FailStartedBuilds transitions all builds with status "started" to "failed" with the given reason.
	// It returns the number of builds that were updated.
	FailStartedBuilds(ctx context.Context, reason string) (int, error)

	// FindActiveBuilds returns all builds for the given job with status started, pending, or
	// waiting_for_approval that have an ID less than beforeBuildID.
	FindActiveBuilds(ctx context.Context, tc, pn, jn string, beforeBuildID uint32) ([]*Build, error)

	// CreateApproval records an approve or reject vote for a build.
	CreateApproval(ctx context.Context, buildID uint32, username, action, message string) error
	// FindApprovals returns all approval/rejection votes for a build.
	FindApprovals(ctx context.Context, buildID uint32) ([]Approval, error)
	// CountApprovals returns the number of "approved" votes for a build.
	CountApprovals(ctx context.Context, buildID uint32) (int, error)
}
