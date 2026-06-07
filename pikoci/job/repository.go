package job

import "context"

//go:generate go tool mockgen -destination=../mock/job_repository.go -mock_names=Repository=JobRepository -package mock github.com/pikoci/pikoci/pikoci/job Repository

// Repository defines the persistence operations for jobs.
type Repository interface {
	// Create persists a new job in the given team and pipeline, returning the job ID.
	Create(ctx context.Context, tc, pn string, j Job) (uint32, error)
	// Update updates an existing job identified by team, pipeline, and job name.
	Update(ctx context.Context, tc, pn, jn string, j Job) error
	// Find retrieves a single job by team, pipeline, and job name.
	Find(ctx context.Context, tc, pn, jn string) (*Job, error)
	// Filter returns all jobs belonging to the given team and pipeline.
	Filter(ctx context.Context, tc, pn string) ([]*Job, error)
	// Delete removes a job identified by team, pipeline, and job name.
	Delete(ctx context.Context, tc, pn, jn string) error
	// SetPaused updates the paused flag for a job.
	SetPaused(ctx context.Context, tc, pn, jn string, paused bool) error
	// PauseAll sets the paused flag for all jobs in a pipeline.
	PauseAll(ctx context.Context, tc, pn string) error
	// UnpauseAll clears the paused flag for all jobs in a pipeline.
	UnpauseAll(ctx context.Context, tc, pn string) error
	// FindJobsBySerialGroups returns all jobs in the pipeline that share any of the given serial groups.
	FindJobsBySerialGroups(ctx context.Context, tc, pn string, serialGroups []string) ([]*Job, error)
}
