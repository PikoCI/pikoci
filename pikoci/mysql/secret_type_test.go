package mysql_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/utils"
)

func TestSecretType_RunnerPersistence(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'st-pipe', 'st-pipe')`)
	require.NoError(t, err)

	repo := mysql.NewSecretTypeRepository(db)

	st := sectype.SecretType{
		Name:   "vault",
		Source: "github.com/example/vault",
		Get:    utils.RunnerCommand{Runner: "exec", Args: []string{"get"}},
		Runner: &utils.RunnerOverride{Runner: "docker", Args: []string{"--privileged"}},
	}

	_, err = repo.Create(ctx, "main", "st-pipe", st)
	require.NoError(t, err)

	found, err := repo.Find(ctx, "main", "st-pipe", "vault")
	require.NoError(t, err)

	require.NotNil(t, found.Runner, "Runner should be persisted")
	assert.Equal(t, "docker", found.Runner.Runner)
	assert.Equal(t, []string{"--privileged"}, found.Runner.Args)
}

func TestSecretType_NilRunnerPersistence(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'st-nil', 'st-nil')`)
	require.NoError(t, err)

	repo := mysql.NewSecretTypeRepository(db)

	st := sectype.SecretType{
		Name:   "vault",
		Source: "github.com/example/vault",
		Get:    utils.RunnerCommand{Runner: "exec", Args: []string{"get"}},
	}

	_, err = repo.Create(ctx, "main", "st-nil", st)
	require.NoError(t, err)

	found, err := repo.Find(ctx, "main", "st-nil", "vault")
	require.NoError(t, err)
	assert.Nil(t, found.Runner, "Runner should be nil when not set")
}

func TestSecretType_UpdateRunner(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'st-upd', 'st-upd')`)
	require.NoError(t, err)

	repo := mysql.NewSecretTypeRepository(db)

	st := sectype.SecretType{
		Name:   "vault",
		Source: "github.com/example/vault",
		Get:    utils.RunnerCommand{Runner: "exec", Args: []string{"get"}},
		Runner: &utils.RunnerOverride{Runner: "docker", Args: []string{"--privileged"}},
	}

	_, err = repo.Create(ctx, "main", "st-upd", st)
	require.NoError(t, err)

	// Update to remove Runner
	st.Runner = nil
	err = repo.Update(ctx, "main", "st-upd", "vault", st)
	require.NoError(t, err)

	found, err := repo.Find(ctx, "main", "st-upd", "vault")
	require.NoError(t, err)
	assert.Nil(t, found.Runner, "Runner should be nil after update removes it")
}
