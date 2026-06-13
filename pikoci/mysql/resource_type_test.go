package mysql_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/utils"
)

func TestResourceType_RunnerPersistence(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'rt-pipe', 'rt-pipe')`)
	require.NoError(t, err)

	repo := mysql.NewResourceTypeRepository(db)

	rt := restype.ResourceType{
		Name:   "git",
		Source: "github.com/example/git",
		Check:  &utils.RunnerCommand{Runner: "exec", Args: []string{"check"}},
		Pull:   &utils.RunnerCommand{Runner: "exec", Args: []string{"pull"}},
		Runner: &utils.RunnerOverride{Runner: "docker", Args: []string{"--privileged"}},
	}

	_, err = repo.Create(ctx, "main", "rt-pipe", rt)
	require.NoError(t, err)

	found, err := repo.Find(ctx, "main", "rt-pipe", "git")
	require.NoError(t, err)

	require.NotNil(t, found.Runner, "Runner should be persisted")
	assert.Equal(t, "docker", found.Runner.Runner)
	assert.Equal(t, []string{"--privileged"}, found.Runner.Args)
}

func TestResourceType_NilRunnerPersistence(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'rt-nil', 'rt-nil')`)
	require.NoError(t, err)

	repo := mysql.NewResourceTypeRepository(db)

	rt := restype.ResourceType{
		Name:   "git",
		Source: "github.com/example/git",
		Check:  &utils.RunnerCommand{Runner: "exec", Args: []string{"check"}},
	}

	_, err = repo.Create(ctx, "main", "rt-nil", rt)
	require.NoError(t, err)

	found, err := repo.Find(ctx, "main", "rt-nil", "git")
	require.NoError(t, err)
	assert.Nil(t, found.Runner, "Runner should be nil when not set")
}

func TestResourceType_UpdateRunner(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'rt-upd', 'rt-upd')`)
	require.NoError(t, err)

	repo := mysql.NewResourceTypeRepository(db)

	rt := restype.ResourceType{
		Name:   "git",
		Source: "github.com/example/git",
		Check:  &utils.RunnerCommand{Runner: "exec", Args: []string{"check"}},
		Runner: &utils.RunnerOverride{Runner: "docker", Args: []string{"--privileged"}},
	}

	_, err = repo.Create(ctx, "main", "rt-upd", rt)
	require.NoError(t, err)

	// Update to remove Runner
	rt.Runner = nil
	err = repo.Update(ctx, "main", "rt-upd", "git", rt)
	require.NoError(t, err)

	found, err := repo.Find(ctx, "main", "rt-upd", "git")
	require.NoError(t, err)
	assert.Nil(t, found.Runner, "Runner should be nil after update removes it")
}
