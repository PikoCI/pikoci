package pikoci_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/secret"
)

// storePipelineHCL references two secrets from the built-in store and one
// value that is not a secret at all.
const storePipelineHCL = `
variable "gh_token" {
  type = string
  secret "pikoci" {
    key = "GITHUB_TOKEN"
  }
}

variable "db_pass" {
  type = string
  secret "pikoci" {
    key = "DB_PASSWORD"
  }
}

job "deploy" {
  task "run" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo ${var.gh_token} ${var.db_pass}"]
    }
  }
}
`

// secretScopeSeq gives each test its own team and pipeline. The in-memory
// backend is opened with cache=shared, so every mysql.New in this process
// reaches the same database and fixed names would collide across tests.
var secretScopeSeq atomic.Int64

// newSecretStoreService builds a service backed by a real in-memory database
// and a real cipher, so the test exercises actual encryption rather than a mock.
// It returns the team and pipeline canonicals reserved for this test.
func newSecretStoreService(t *testing.T, masterKey string) (*pikoci.PikoCI, *sql.DB, string, string) {
	t.Helper()

	ctrl := gomock.NewController(t)
	ms := newService(ctrl)

	db, err := mysql.New("", 0, "", "", mysql.Options{
		MultiStatements: true,
		ClientFoundRows: true,
		System:          mysql.Mem,
	})
	require.NoError(t, err)
	require.NoError(t, migrate.Migrate(db, mysql.Mem))

	n := secretScopeSeq.Add(1)
	tc := fmt.Sprintf("team-%d", n)
	pn := fmt.Sprintf("pipe-%d", n)

	res, err := db.Exec(`INSERT INTO teams (name, canonical) VALUES (?, ?)`, tc, tc)
	require.NoError(t, err)
	teamID, err := res.LastInsertId()
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO pipelines (team_id, name, canonical) VALUES (?, ?, ?)`, teamID, pn, pn)
	require.NoError(t, err)

	ms.P.EnableSecretStore(mysql.NewSecretRepository(db), masterKey)

	ms.Pipelines.EXPECT().Find(gomock.Any(), tc, pn).Return(&pipeline.Pipeline{
		Name:      pn,
		Canonical: pn,
		Raw:       []byte(storePipelineHCL),
	}, nil).AnyTimes()

	return ms.P, db, tc, pn
}

func TestResolvePipelineSecrets(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, pn := newSecretStoreService(t, "master-key")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "GITHUB_TOKEN", "ghp_team"))
	require.NoError(t, svc.SetTeamSecret(ctx, tc, "DB_PASSWORD", "team-db-pass"))

	values, err := svc.ResolvePipelineSecrets(ctx, tc, pn)
	require.NoError(t, err)
	assert.Equal(t, "ghp_team", values["GITHUB_TOKEN"])
	assert.Equal(t, "team-db-pass", values["DB_PASSWORD"])
}

func TestResolvePipelineSecrets_PipelineOverridesTeam(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, pn := newSecretStoreService(t, "master-key")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "GITHUB_TOKEN", "ghp_team"))
	require.NoError(t, svc.SetTeamSecret(ctx, tc, "DB_PASSWORD", "team-db-pass"))
	require.NoError(t, svc.SetPipelineSecret(ctx, tc, pn, "GITHUB_TOKEN", "ghp_pipeline"))

	values, err := svc.ResolvePipelineSecrets(ctx, tc, pn)
	require.NoError(t, err)
	assert.Equal(t, "ghp_pipeline", values["GITHUB_TOKEN"], "pipeline secret must shadow the team one")
	assert.Equal(t, "team-db-pass", values["DB_PASSWORD"], "unshadowed team secrets are still inherited")
}

// Only the secrets a pipeline actually names are handed out, so one build's
// worker cannot enumerate everything the team has stored.
func TestResolvePipelineSecrets_OnlyReferencedKeys(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, pn := newSecretStoreService(t, "master-key")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "GITHUB_TOKEN", "ghp_team"))
	require.NoError(t, svc.SetTeamSecret(ctx, tc, "UNRELATED_SECRET", "must-not-leak"))

	values, err := svc.ResolvePipelineSecrets(ctx, tc, pn)
	require.NoError(t, err)

	assert.Equal(t, "ghp_team", values["GITHUB_TOKEN"])
	assert.NotContains(t, values, "UNRELATED_SECRET", "unreferenced secrets must not be sent to the worker")
}

func TestSecretStore_NotConfigured(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, _ := newSecretStoreService(t, "")

	// Storing needs the key.
	err := svc.SetTeamSecret(ctx, tc, "GITHUB_TOKEN", "value")
	assert.ErrorIs(t, err, secret.ErrNotConfigured)

	// Listing does not, so an operator who lost the key can still see and
	// clean up what is stored.
	_, err = svc.ListTeamSecrets(ctx, "main")
	assert.NoError(t, err)
}

func TestSetSecret_RejectsInvalidNames(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, _ := newSecretStoreService(t, "master-key")

	for _, name := range []string{"", "has-dash", "has space", "1leading_digit", "has.dot"} {
		err := svc.SetTeamSecret(ctx, tc, name, "value")
		assert.Errorf(t, err, "expected %q to be rejected", name)
	}

	assert.NoError(t, svc.SetTeamSecret(ctx, tc, "VALID_NAME_1", "value"))
	assert.NoError(t, svc.SetTeamSecret(ctx, tc, "_leading_underscore", "value"))
}

// Values must be unreadable in the database without the master key.
func TestSecretStore_ValuesAreEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	svc, db, tc, _ := newSecretStoreService(t, "master-key")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "GITHUB_TOKEN", "ghp_plaintext_marker"))

	var stored string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT s.value FROM team_secrets AS s
		JOIN teams AS t ON s.team_id = t.id
		WHERE t.canonical = ? AND s.canonical = 'GITHUB_TOKEN'
	`, tc).Scan(&stored))

	assert.NotEmpty(t, stored)
	assert.NotContains(t, stored, "ghp_plaintext_marker", "the stored column must not contain plaintext")
}

func TestSetSecret_UnknownScopeReportsClearly(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, _ := newSecretStoreService(t, "master-key")

	err := svc.SetPipelineSecret(ctx, tc, "does-not-exist", "TOKEN", "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `pipeline "does-not-exist" not found`,
		"a missing pipeline should say so, not surface a constraint violation")

	err = svc.SetTeamSecret(ctx, "no-such-team", "TOKEN", "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `team "no-such-team" not found`)
}
