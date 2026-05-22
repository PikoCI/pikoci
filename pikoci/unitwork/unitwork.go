package unitwork

import (
	"context"

	"github.com/xescugc/pikoci/pikoci/build"
	"github.com/xescugc/pikoci/pikoci/job"
	"github.com/xescugc/pikoci/pikoci/pipeline"
	"github.com/xescugc/pikoci/pikoci/resource"
	"github.com/xescugc/pikoci/pikoci/restype"
	"github.com/xescugc/pikoci/pikoci/runner"
	"github.com/xescugc/pikoci/pikoci/sectype"
	"github.com/xescugc/pikoci/pikoci/team"
	"github.com/xescugc/pikoci/pikoci/user"
)

type StartUnitOfWork func(ctx context.Context, uowFn func(uow UnitOfWork) error) error

type UnitOfWork interface {
	Users() user.Repository
	Teams() team.Repository
	Pipelines() pipeline.Repository
	Jobs() job.Repository
	Resources() resource.Repository
	ResourceTypes() restype.Repository
	Builds() build.Repository
	Runners() runner.Repository
	SecretTypes() sectype.Repository
}

// Repositories holds all repository interfaces, used to construct a noop UoW for testing.
type Repositories struct {
	UsersRepo         user.Repository
	TeamsRepo         team.Repository
	PipelinesRepo     pipeline.Repository
	JobsRepo          job.Repository
	ResourcesRepo     resource.Repository
	ResourceTypesRepo restype.Repository
	BuildsRepo        build.Repository
	RunnersRepo       runner.Repository
	SecretTypesRepo   sectype.Repository
}
