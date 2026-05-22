package build

import (
	"context"
	"time"
)

//go:generate go tool mockgen -destination=../mock/build_repository.go -mock_names=Repository=BuildRepository -package mock github.com/pikoci/pikoci/pikoci/build Repository

type Repository interface {
	Create(ctx context.Context, tc, pn, jn string, b Build) (uint32, string, error)
	CreateRetry(ctx context.Context, tc, pn, jn, parentBuildNumber string, b Build) (uint32, string, error)
	Find(ctx context.Context, tc, pn, jn string, buildNumber string) (*Build, error)
	Filter(ctx context.Context, tc, pn, jn string) ([]*Build, error)
	Update(ctx context.Context, tc, pn, jn string, buildNumber string, b Build) error
	Delete(ctx context.Context, tc, pn, jn string, buildNumber string) error
	InsertGetVersion(ctx context.Context, tc, pn, jn string, buildID uint32, stepName string, versionID uint32) error
	FindGetVersions(ctx context.Context, buildID uint32) (map[string]uint32, error)
	FindReadyDownstreamVersion(ctx context.Context, tc, pn string, upstreamJobs []string, downstreamJob string, stepName string, upstreamCount int) (uint32, bool, error)
	LastBuildAtByPipeline(ctx context.Context, tc string) (map[uint32]time.Time, error)
	CountRunning(ctx context.Context, tc, pn, jn string) (int, error)
}
