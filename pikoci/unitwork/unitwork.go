// Package unitwork implements the Unit of Work pattern for transactional
// consistency across multiple repository operations. A unit of work groups
// reads and writes into a single database transaction that is committed on
// success or rolled back on failure.
package unitwork

import (
	"context"

	"github.com/pikoci/pikoci/pikoci/apitoken"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/notification"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/user"
)

// StartUnitOfWork is a function that begins a new unit of work, executes the
// provided callback within a transactional scope, and commits or rolls back
// depending on whether the callback returns an error.
type StartUnitOfWork func(ctx context.Context, uowFn func(uow UnitOfWork) error) error

// UnitOfWork provides access to all domain repositories scoped to a single
// database transaction. Implementations lazily initialize repositories so that
// only the ones actually used participate in the transaction.
type UnitOfWork interface {
	// Users returns the user repository for this transaction.
	Users() user.Repository
	// Teams returns the team repository for this transaction.
	Teams() team.Repository
	// Pipelines returns the pipeline repository for this transaction.
	Pipelines() pipeline.Repository
	// Jobs returns the job repository for this transaction.
	Jobs() job.Repository
	// Resources returns the resource repository for this transaction.
	Resources() resource.Repository
	// ResourceTypes returns the resource type repository for this transaction.
	ResourceTypes() restype.Repository
	// Builds returns the build repository for this transaction.
	Builds() build.Repository
	// Runners returns the runner repository for this transaction.
	Runners() runner.Repository
	// SecretTypes returns the secret type repository for this transaction.
	SecretTypes() sectype.Repository
	// NotificationTypes returns the notification type repository for this transaction.
	NotificationTypes() notiftype.Repository
	// Notifications returns the notification repository for this transaction.
	Notifications() notification.Repository
	// ApiTokens returns the API token repository for this transaction.
	ApiTokens() apitoken.Repository
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
	SecretTypesRepo        sectype.Repository
	NotificationTypesRepo notiftype.Repository
	NotificationsRepo     notification.Repository
	ApiTokensRepo         apitoken.Repository
}
