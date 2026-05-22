package unitwork

import (
	"context"

	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/user"
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
	SecretTypesRepo sectype.Repository
}
