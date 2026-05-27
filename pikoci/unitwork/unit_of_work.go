package unitwork

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/xescugc/pikoci/pikoci/build"
	"github.com/xescugc/pikoci/pikoci/job"
	"github.com/xescugc/pikoci/pikoci/mysql"
	"github.com/xescugc/pikoci/pikoci/pipeline"
	"github.com/xescugc/pikoci/pikoci/resource"
	"github.com/xescugc/pikoci/pikoci/restype"
	"github.com/xescugc/pikoci/pikoci/runner"
	"github.com/xescugc/pikoci/pikoci/sectype"
	"github.com/xescugc/pikoci/pikoci/team"
	"github.com/xescugc/pikoci/pikoci/user"
)

// unitOfWork is a transactional UnitOfWork backed by an sql.Tx. Repositories
// are lazily created on first access so that only the ones actually used
// participate in the transaction.
type unitOfWork struct {
	tx       *sql.Tx
	dbSystem string

	users         user.Repository
	teams         team.Repository
	pipelines     pipeline.Repository
	jobs          job.Repository
	resources     resource.Repository
	resourceTypes restype.Repository
	builds        build.Repository
	runners       runner.Repository
	secretTypes   sectype.Repository
}

// NewStartUnitOfWork returns a StartUnitOfWork backed by a real SQL database.
// Each invocation begins a new transaction, executes the callback, and commits
// on success or rolls back on error. The dbSystem parameter identifies the
// database dialect (e.g. "mysql") for repositories that need it.
func NewStartUnitOfWork(db *sql.DB, dbSystem string) StartUnitOfWork {
	return func(ctx context.Context, uowFn func(uow UnitOfWork) error) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		uow := &unitOfWork{tx: tx, dbSystem: dbSystem}

		if err := uowFn(uow); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				return fmt.Errorf("failed to rollback: %w (original error: %w)", rbErr, err)
			}
			return err
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		return nil
	}
}

func (u *unitOfWork) Users() user.Repository {
	if u.users == nil {
		u.users = mysql.NewUserRepository(u.tx)
	}
	return u.users
}

func (u *unitOfWork) Teams() team.Repository {
	if u.teams == nil {
		u.teams = mysql.NewTeamRepository(u.tx)
	}
	return u.teams
}

func (u *unitOfWork) Pipelines() pipeline.Repository {
	if u.pipelines == nil {
		u.pipelines = mysql.NewPipelineRepository(u.tx)
	}
	return u.pipelines
}

func (u *unitOfWork) Jobs() job.Repository {
	if u.jobs == nil {
		u.jobs = mysql.NewJobRepository(u.tx)
	}
	return u.jobs
}

func (u *unitOfWork) Resources() resource.Repository {
	if u.resources == nil {
		u.resources = mysql.NewResourceRepository(u.tx, u.dbSystem)
	}
	return u.resources
}

func (u *unitOfWork) ResourceTypes() restype.Repository {
	if u.resourceTypes == nil {
		u.resourceTypes = mysql.NewResourceTypeRepository(u.tx)
	}
	return u.resourceTypes
}

func (u *unitOfWork) Builds() build.Repository {
	if u.builds == nil {
		u.builds = mysql.NewBuildRepository(u.tx, u.dbSystem)
	}
	return u.builds
}

func (u *unitOfWork) Runners() runner.Repository {
	if u.runners == nil {
		u.runners = mysql.NewRunnerRepository(u.tx)
	}
	return u.runners
}

func (u *unitOfWork) SecretTypes() sectype.Repository {
	if u.secretTypes == nil {
		u.secretTypes = mysql.NewSecretTypeRepository(u.tx)
	}
	return u.secretTypes
}
