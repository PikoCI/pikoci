package build

import (
	"context"
	"time"
)

//go:generate go tool mockgen -destination=../mock/build_repository.go -mock_names=Repository=BuildRepository -package mock github.com/xescugc/pikoci/pikoci/build Repository

// Repository defines the persistence operations for builds.
type Repository interface {
	// Create persists a new build for the given team, pipeline, and job, returning the build ID and build number.
	Create(ctx context.Context, tc, pn, jn string, b Build) (uint32, string, error)
	// CreateRetry persists a retry of an existing build, linking it to the parent build number.
	CreateRetry(ctx context.Context, tc, pn, jn, parentBuildNumber string, b Build) (uint32, string, error)
	// Find retrieves a single build by team, pipeline, job, and build number.
	Find(ctx context.Context, tc, pn, jn string, buildNumber string) (*Build, error)
	// Filter returns a paginated list of builds for the given team, pipeline, and job.
	Filter(ctx context.Context, tc, pn, jn string, before *uint32, after *uint32, limit uint32) ([]*Build, error)
	// Update updates an existing build identified by team, pipeline, job, and build number.
	Update(ctx context.Context, tc, pn, jn string, buildNumber string, b Build) error
	// Delete removes a build identified by team, pipeline, job, and build number.
	Delete(ctx context.Context, tc, pn, jn string, buildNumber string) error
	// InsertGetVersion records the resource version fetched by a get step during a build.
	InsertGetVersion(ctx context.Context, tc, pn, jn string, buildID uint32, stepName string, versionID uint32) error
	// FindGetVersions returns a map of step names to version IDs for all get steps in a build.
	FindGetVersions(ctx context.Context, buildID uint32) (map[string]uint32, error)
	// FindReadyDownstreamVersion finds a version that has passed through all upstream jobs and is ready for the downstream job.
	FindReadyDownstreamVersion(ctx context.Context, tc, pn string, upstreamJobs []string, downstreamJob string, stepName string, upstreamCount int) (uint32, bool, error)
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
}
