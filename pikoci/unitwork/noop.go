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

type noopUnitOfWork struct {
	repos Repositories
}

func NewNoopStartUnitOfWork(repos Repositories) StartUnitOfWork {
	return func(ctx context.Context, uowFn func(uow UnitOfWork) error) error {
		uow := &noopUnitOfWork{repos: repos}
		return uowFn(uow)
	}
}

func (u *noopUnitOfWork) Users() user.Repository         { return u.repos.UsersRepo }
func (u *noopUnitOfWork) Teams() team.Repository         { return u.repos.TeamsRepo }
func (u *noopUnitOfWork) Pipelines() pipeline.Repository { return u.repos.PipelinesRepo }
func (u *noopUnitOfWork) Jobs() job.Repository           { return u.repos.JobsRepo }
func (u *noopUnitOfWork) Resources() resource.Repository { return u.repos.ResourcesRepo }
func (u *noopUnitOfWork) ResourceTypes() restype.Repository {
	return u.repos.ResourceTypesRepo
}
func (u *noopUnitOfWork) Builds() build.Repository     { return u.repos.BuildsRepo }
func (u *noopUnitOfWork) Runners() runner.Repository   { return u.repos.RunnersRepo }
func (u *noopUnitOfWork) SecretTypes() sectype.Repository { return u.repos.SecretTypesRepo }
