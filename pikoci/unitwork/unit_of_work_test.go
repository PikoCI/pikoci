package unitwork

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnitOfWork_QuerierRewritesPlaceholdersOnPostgreSQL(t *testing.T) {
	// Repositories in a transaction must get the same placeholder rewrite as
	// the non-transactional ones; without it every write fails on PostgreSQL.
	_, ok := (&unitOfWork{dbSystem: mysql.PostgreSQL}).querier().(*mysql.PGQuerier)
	assert.True(t, ok, "PostgreSQL needs the PGQuerier wrapper")

	for _, system := range []string{mysql.MySQL, mysql.SQLite, mysql.Mem} {
		_, ok := (&unitOfWork{dbSystem: system}).querier().(*sql.Tx)
		assert.True(t, ok, "%s takes the transaction as is", system)
	}
}

func newMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := mysql.New("", 0, "", "", mysql.Options{MultiStatements: true, ClientFoundRows: true, System: mysql.Mem})
	require.NoError(t, err)
	require.NoError(t, migrate.Migrate(db, mysql.Mem))
	return db
}

func TestStartUnitOfWork_CommitsAcrossRepositories(t *testing.T) {
	db := newMemDB(t)
	ctx := context.Background()
	start := NewStartUnitOfWork(db, mysql.Mem)

	err := start(ctx, func(uow UnitOfWork) error {
		for name, repo := range map[string]interface{}{
			"Users": uow.Users(), "Teams": uow.Teams(), "Pipelines": uow.Pipelines(), "Jobs": uow.Jobs(),
			"Resources": uow.Resources(), "ResourceTypes": uow.ResourceTypes(), "Builds": uow.Builds(),
			"Runners": uow.Runners(), "SecretTypes": uow.SecretTypes(), "NotificationTypes": uow.NotificationTypes(),
			"Notifications": uow.Notifications(), "ApiTokens": uow.ApiTokens(), "Secrets": uow.Secrets(),
		} {
			assert.NotNil(t, repo, name)
		}
		if _, err := uow.Teams().Create(ctx, team.Team{Name: "uow-team", Canonical: "uow-team"}); err != nil {
			return err
		}
		if _, err := uow.Pipelines().Create(ctx, "uow-team", pipeline.Pipeline{Name: "uow-pipe", Canonical: "uow-pipe"}); err != nil {
			return err
		}
		if err := uow.Pipelines().Update(ctx, "uow-team", "uow-pipe", pipeline.Pipeline{Name: "uow-pipe", Canonical: "uow-pipe", Raw: []byte("v2")}); err != nil {
			return err
		}
		if _, err := uow.Jobs().Create(ctx, "uow-team", "uow-pipe", job.Job{Name: "build"}); err != nil {
			return err
		}
		_, err := uow.Resources().Create(ctx, "uow-team", "uow-pipe", resource.Resource{Name: "repo", Type: "git", Canonical: "git.repo"})
		return err
	})
	require.NoError(t, err)

	p, err := mysql.NewPipelineRepository(db).Find(ctx, "uow-team", "uow-pipe")
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), p.Raw, "the update inside the transaction is committed")
	_, err = mysql.NewJobRepository(db).Find(ctx, "uow-team", "uow-pipe", "build")
	assert.NoError(t, err)
	_, err = mysql.NewResourceRepository(db, mysql.Mem).Find(ctx, "uow-team", "uow-pipe", "git.repo")
	assert.NoError(t, err)
}

func TestStartUnitOfWork_RollsBackOnError(t *testing.T) {
	db := newMemDB(t)
	ctx := context.Background()
	boom := errors.New("boom")

	err := NewStartUnitOfWork(db, mysql.Mem)(ctx, func(uow UnitOfWork) error {
		if _, err := uow.Teams().Create(ctx, team.Team{Name: "uow-rollback", Canonical: "uow-rollback"}); err != nil {
			return err
		}
		return boom
	})
	require.ErrorIs(t, err, boom)

	_, err = mysql.NewTeamRepository(db).Find(ctx, "uow-rollback")
	assert.Error(t, err, "the team created before the error must be rolled back")
}
