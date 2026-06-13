package mysql_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/utils"
)

func TestNotificationType_RunnerPersistence(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'nt-pipe', 'nt-pipe')`)
	require.NoError(t, err)

	repo := mysql.NewNotificationTypeRepository(db)

	nt := notiftype.NotificationType{
		Name:   "slack",
		Source: "github.com/example/slack",
		Notify: &utils.RunnerCommand{Runner: "exec", Args: []string{"notify"}},
		Runner: &utils.RunnerOverride{Runner: "docker", Args: []string{"--privileged"}},
	}

	_, err = repo.Create(ctx, "main", "nt-pipe", nt)
	require.NoError(t, err)

	found, err := repo.Find(ctx, "main", "nt-pipe", "slack")
	require.NoError(t, err)

	require.NotNil(t, found.Runner, "Runner should be persisted")
	assert.Equal(t, "docker", found.Runner.Runner)
	assert.Equal(t, []string{"--privileged"}, found.Runner.Args)
}

func TestNotificationType_NilRunnerPersistence(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'nt-nil', 'nt-nil')`)
	require.NoError(t, err)

	repo := mysql.NewNotificationTypeRepository(db)

	nt := notiftype.NotificationType{
		Name:   "slack",
		Source: "github.com/example/slack",
		Notify: &utils.RunnerCommand{Runner: "exec", Args: []string{"notify"}},
	}

	_, err = repo.Create(ctx, "main", "nt-nil", nt)
	require.NoError(t, err)

	found, err := repo.Find(ctx, "main", "nt-nil", "slack")
	require.NoError(t, err)
	assert.Nil(t, found.Runner, "Runner should be nil when not set")
}

func TestNotificationType_UpdateRunner(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'nt-upd', 'nt-upd')`)
	require.NoError(t, err)

	repo := mysql.NewNotificationTypeRepository(db)

	nt := notiftype.NotificationType{
		Name:   "slack",
		Source: "github.com/example/slack",
		Notify: &utils.RunnerCommand{Runner: "exec", Args: []string{"notify"}},
		Runner: &utils.RunnerOverride{Runner: "docker", Args: []string{"--privileged"}},
	}

	_, err = repo.Create(ctx, "main", "nt-upd", nt)
	require.NoError(t, err)

	// Update to remove Runner
	nt.Runner = nil
	err = repo.Update(ctx, "main", "nt-upd", "slack", nt)
	require.NoError(t, err)

	found, err := repo.Find(ctx, "main", "nt-upd", "slack")
	require.NoError(t, err)
	assert.Nil(t, found.Runner, "Runner should be nil after update removes it")
}
