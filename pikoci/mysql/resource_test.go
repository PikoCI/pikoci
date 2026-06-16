package mysql_test

import (
	"context"
	"testing"

	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindByWebhookToken(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'wh-pipe', 'wh-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx,
		`INSERT INTO resources (pipeline_id, name, type, canonical, webhook_token, tags, cache)
		 VALUES (?, 'repo', 'git', 'git.repo', 'git.repo_abc123', '["deploy"]', 0)`, ppID)
	require.NoError(t, err)

	rr := mysql.NewResourceRepository(db, mysql.Mem)

	r, tc, pc, err := rr.FindByWebhookToken(ctx, "git.repo_abc123")
	require.NoError(t, err)
	assert.Equal(t, "git.repo", r.Canonical)
	assert.Equal(t, "main", tc)
	assert.Equal(t, "wh-pipe", pc)
}

func TestFindByWebhookToken_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	rr := mysql.NewResourceRepository(db, mysql.Mem)

	_, _, _, err := rr.FindByWebhookToken(ctx, "nonexistent-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
