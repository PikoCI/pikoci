package mysql_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/secret"
)

func TestSecret_TeamUpsertAndFilter(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewSecretRepository(db)

	s := secret.Secret{Name: "GITHUB_TOKEN", Canonical: "upsert-github-token"}
	id, err := repo.UpsertTeam(ctx, "main", s, []byte("cipher-v1"))
	require.NoError(t, err)
	assert.NotZero(t, id)

	// Upserting the same canonical must replace, not duplicate.
	id2, err := repo.UpsertTeam(ctx, "main", s, []byte("cipher-v2"))
	require.NoError(t, err)
	assert.Equal(t, id, id2, "upsert should reuse the existing row")

	found, err := repo.FilterTeam(ctx, "main")
	require.NoError(t, err)
	got := findSecret(found, "upsert-github-token")
	require.NotNil(t, got)
	assert.Equal(t, "GITHUB_TOKEN", got.Name)
	assert.Equal(t, secret.TeamScope, got.Scope)

	values, err := repo.EncryptedValues(ctx, "main", "anything")
	require.NoError(t, err)
	assert.Equal(t, []byte("cipher-v2"), values["upsert-github-token"], "latest value should win")
}

func TestSecret_PipelineShadowsTeam(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'web', 'web')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'api', 'api')`)
	require.NoError(t, err)

	repo := mysql.NewSecretRepository(db)

	_, err = repo.UpsertTeam(ctx, "main", secret.Secret{Name: "TOKEN", Canonical: "shadow-token"}, []byte("team-value"))
	require.NoError(t, err)
	_, err = repo.UpsertTeam(ctx, "main", secret.Secret{Name: "SHARED", Canonical: "shadow-shared"}, []byte("shared-value"))
	require.NoError(t, err)
	_, err = repo.UpsertPipeline(ctx, "main", "web", secret.Secret{Name: "TOKEN", Canonical: "shadow-token"}, []byte("pipeline-value"))
	require.NoError(t, err)

	// The pipeline that defines its own TOKEN sees the override.
	values, err := repo.EncryptedValues(ctx, "main", "web")
	require.NoError(t, err)
	assert.Equal(t, []byte("pipeline-value"), values["shadow-token"], "pipeline secret must shadow the team one")
	assert.Equal(t, []byte("shared-value"), values["shadow-shared"], "team secrets must still be inherited")

	// A sibling pipeline is unaffected by the override.
	values, err = repo.EncryptedValues(ctx, "main", "api")
	require.NoError(t, err)
	assert.Equal(t, []byte("team-value"), values["shadow-token"])
}

func TestSecret_Delete(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewSecretRepository(db)

	_, err := repo.UpsertTeam(ctx, "main", secret.Secret{Name: "GONE", Canonical: "delete-gone"}, []byte("v"))
	require.NoError(t, err)

	require.NoError(t, repo.DeleteTeam(ctx, "main", "delete-gone"))

	found, err := repo.FilterTeam(ctx, "main")
	require.NoError(t, err)
	assert.Nil(t, findSecret(found, "delete-gone"))

	assert.Error(t, repo.DeleteTeam(ctx, "main", "delete-gone"), "deleting a missing secret should report not found")
}

// Secrets must not outlive the pipeline they belong to.
func TestSecret_PipelineDeleteCascades(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'doomed', 'doomed')`)
	require.NoError(t, err)
	ppID, err := res.LastInsertId()
	require.NoError(t, err)

	repo := mysql.NewSecretRepository(db)
	_, err = repo.UpsertPipeline(ctx, "main", "doomed", secret.Secret{Name: "K", Canonical: "cascade-k"}, []byte("v"))
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `DELETE FROM pipelines WHERE canonical = 'doomed'`)
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pipeline_secrets WHERE pipeline_id = ?`, ppID).Scan(&count))
	assert.Zero(t, count, "pipeline secrets should cascade on pipeline delete")
}

func TestSecret_ServerKeyRoundTrip(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewSecretRepository(db)

	// Binary-safe: the wrapped identity is arbitrary bytes, stored base64.
	wrapped := []byte{0x00, 0xff, 0x10, 0x00, 0x7f}
	require.NoError(t, repo.CreateServerKey(ctx, secret.ServerKey{Wrapped: wrapped, Recipient: "age1test"}))

	got, err := repo.FindServerKey(ctx)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, wrapped, got.Wrapped)
	assert.Equal(t, "age1test", got.Recipient)

	assert.Error(t, repo.CreateServerKey(ctx, secret.ServerKey{Wrapped: []byte("other"), Recipient: "age1other"}),
		"a second key must be rejected so stored values are never orphaned")
}

// findSecret locates a secret by canonical name. Tests share one in-memory
// database, so assertions are scoped to names rather than to list length.
func findSecret(secrets []*secret.Secret, canonical string) *secret.Secret {
	for _, s := range secrets {
		if s.Canonical == canonical {
			return s
		}
	}
	return nil
}
