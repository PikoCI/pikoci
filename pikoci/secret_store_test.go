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
	"github.com/pikoci/pikoci/pikoci/auditlog"
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
	ms, db, tc, pn := newSecretStoreMocks(t, masterKey, storePipelineHCL)
	return ms.P, db, tc, pn
}

// newSecretStoreMocks is newSecretStoreService with the mocks kept, for tests
// that assert on what was recorded rather than only on the returned value, and
// with the pipeline's raw HCL under the test's control.
func newSecretStoreMocks(t *testing.T, masterKey, raw string) (MockService, *sql.DB, string, string) {
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

	sr := mysql.NewSecretRepository(db)
	ms.P.EnableSecretStore(sr, masterKey)
	ms.withSecretRepo(sr)

	ms.Pipelines.EXPECT().Find(gomock.Any(), tc, pn).Return(&pipeline.Pipeline{
		Name:      pn,
		Canonical: pn,
		Raw:       []byte(raw),
	}, nil).AnyTimes()

	return ms, db, tc, pn
}

func TestResolvePipelineSecrets(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, pn := newSecretStoreService(t, "master-key")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "GITHUB_TOKEN", "ghp_team", secret.KindSecret))
	require.NoError(t, svc.SetTeamSecret(ctx, tc, "DB_PASSWORD", "team-db-pass", secret.KindSecret))

	resolved, err := svc.ResolvePipelineValues(ctx, tc, pn)
	require.NoError(t, err)
	assert.Equal(t, "ghp_team", resolved.Values["GITHUB_TOKEN"])
	assert.Equal(t, "team-db-pass", resolved.Values["DB_PASSWORD"])
}

func TestResolvePipelineSecrets_PipelineOverridesTeam(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, pn := newSecretStoreService(t, "master-key")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "GITHUB_TOKEN", "ghp_team", secret.KindSecret))
	require.NoError(t, svc.SetTeamSecret(ctx, tc, "DB_PASSWORD", "team-db-pass", secret.KindSecret))
	require.NoError(t, svc.SetPipelineSecret(ctx, tc, pn, "GITHUB_TOKEN", "ghp_pipeline", secret.KindSecret))

	resolved, err := svc.ResolvePipelineValues(ctx, tc, pn)
	require.NoError(t, err)
	assert.Equal(t, "ghp_pipeline", resolved.Values["GITHUB_TOKEN"], "pipeline entry must shadow the team one")
	assert.Equal(t, "team-db-pass", resolved.Values["DB_PASSWORD"], "unshadowed team entries are still inherited")
}

// Only the secrets a pipeline actually names are handed out, so one build's
// worker cannot enumerate everything the team has stored.
func TestResolvePipelineSecrets_OnlyReferencedKeys(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, pn := newSecretStoreService(t, "master-key")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "GITHUB_TOKEN", "ghp_team", secret.KindSecret))
	require.NoError(t, svc.SetTeamSecret(ctx, tc, "UNRELATED_SECRET", "must-not-leak", secret.KindSecret))

	resolved, err := svc.ResolvePipelineValues(ctx, tc, pn)
	require.NoError(t, err)

	assert.Equal(t, "ghp_team", resolved.Values["GITHUB_TOKEN"])
	assert.NotContains(t, resolved.Values, "UNRELATED_SECRET", "unreferenced entries must not be sent to the worker")
}

func TestSecretStore_NotConfigured(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, _ := newSecretStoreService(t, "")

	// Storing a secret needs the key.
	err := svc.SetTeamSecret(ctx, tc, "GITHUB_TOKEN", "value", secret.KindSecret)
	assert.ErrorIs(t, err, secret.ErrNotConfigured)

	// Plain entries do not, so a server with no encryption configured is still
	// useful for shared plain values.
	assert.NoError(t, svc.SetTeamSecret(ctx, tc, "LOG_LEVEL", "debug", secret.KindPlain))

	// Neither does listing, so an operator who lost the key can still see and
	// clean up what is stored.
	_, err = svc.ListTeamSecrets(ctx, tc)
	assert.NoError(t, err)
}

func TestSetSecret_RejectsInvalidNames(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, _ := newSecretStoreService(t, "master-key")

	for _, name := range []string{"", "has-dash", "has space", "1leading_digit", "has.dot"} {
		err := svc.SetTeamSecret(ctx, tc, name, "value", secret.KindSecret)
		assert.Errorf(t, err, "expected %q to be rejected", name)
	}

	assert.NoError(t, svc.SetTeamSecret(ctx, tc, "VALID_NAME_1", "value", secret.KindSecret))
	assert.NoError(t, svc.SetTeamSecret(ctx, tc, "_leading_underscore", "value", secret.KindSecret))
}

// Values must be unreadable in the database without the master key.
func TestSecretStore_SecretValuesAreEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	svc, db, tc, _ := newSecretStoreService(t, "master-key")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "GITHUB_TOKEN", "ghp_plaintext_marker", secret.KindSecret))

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

	err := svc.SetPipelineSecret(ctx, tc, "does-not-exist", "TOKEN", "value", secret.KindSecret)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `pipeline "does-not-exist" not found`,
		"a missing pipeline should say so, not surface a constraint violation")

	err = svc.SetTeamSecret(ctx, "no-such-team", "TOKEN", "value", secret.KindSecret)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `team "no-such-team" not found`)
}

// A plain entry resolves in the clear and is flagged so the worker will not
// mask it, while a secret alongside it still resolves decrypted and masked.
func TestResolvePipelineValues_PlainAndSecret(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, pn := newSecretStoreService(t, "master-key")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "GITHUB_TOKEN", "ghp_secret", secret.KindSecret))
	require.NoError(t, svc.SetTeamSecret(ctx, tc, "DB_PASSWORD", "log-level-debug", secret.KindPlain))

	resolved, err := svc.ResolvePipelineValues(ctx, tc, pn)
	require.NoError(t, err)

	assert.Equal(t, "ghp_secret", resolved.Values["GITHUB_TOKEN"])
	assert.False(t, resolved.Plain["GITHUB_TOKEN"], "secrets must stay maskable")

	assert.Equal(t, "log-level-debug", resolved.Values["DB_PASSWORD"])
	assert.True(t, resolved.Plain["DB_PASSWORD"], "plain entries must be flagged so they are not masked")
}

// Plain values are stored verbatim, so an operator can read them straight out
// of the database and the API can return them.
func TestSecretStore_PlainValuesAreReadable(t *testing.T) {
	ctx := context.Background()
	svc, db, tc, _ := newSecretStoreService(t, "master-key")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "LOG_LEVEL", "debug", secret.KindPlain))

	var stored string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT s.value FROM team_secrets AS s
		JOIN teams AS t ON s.team_id = t.id
		WHERE t.canonical = ? AND s.canonical = 'LOG_LEVEL'
	`, tc).Scan(&stored))
	assert.Equal(t, "debug", stored, "plain values are stored verbatim, not encoded")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "API_TOKEN", "tok", secret.KindSecret))

	entries, err := svc.ListTeamSecrets(ctx, tc)
	require.NoError(t, err)

	var plain, sec *secret.Entry
	for _, e := range entries {
		switch e.Canonical {
		case "LOG_LEVEL":
			plain = e
		case "API_TOKEN":
			sec = e
		}
	}

	require.NotNil(t, plain)
	require.NotNil(t, sec)
	assert.Equal(t, "debug", plain.Value, "list must return plain values")
	assert.Empty(t, sec.Value, "list must never return a secret value")
	assert.Equal(t, secret.KindPlain, plain.Kind)
	assert.Equal(t, secret.KindSecret, sec.Kind)
}

// Kind must be explicit and valid; a typo must not silently store a credential
// in the clear.
func TestSetSecret_RejectsUnknownKind(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, _ := newSecretStoreService(t, "master-key")

	err := svc.SetTeamSecret(ctx, tc, "NAME", "value", secret.Kind("plaintext"))
	require.Error(t, err)
	assert.ErrorIs(t, err, pikoci.ErrSecretInvalidRequest,
		"an unknown kind is a caller mistake, so the transport can answer 400")
	assert.Contains(t, err.Error(), "plaintext", "the message should name the bad kind")
}

// Storing the same name again replaces the value. This is how the UI updates a
// secret: there is no window in which the entry does not exist.
func TestSetSecret_ReplacesTheValueInPlace(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, pn := newSecretStoreService(t, "master-key")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "LOG_LEVEL", "debug", secret.KindPlain))
	require.NoError(t, svc.SetTeamSecret(ctx, tc, "LOG_LEVEL", "info", secret.KindPlain))

	entries, err := svc.ListTeamSecrets(ctx, tc)
	require.NoError(t, err)
	require.Len(t, entries, 1, "a replace must not add a second row")
	assert.Equal(t, "info", entries[0].Value)

	require.NoError(t, svc.SetPipelineSecret(ctx, tc, pn, "DB_URL", "one", secret.KindPlain))
	require.NoError(t, svc.SetPipelineSecret(ctx, tc, pn, "DB_URL", "two", secret.KindPlain))

	pipeEntries, err := svc.ListPipelineSecrets(ctx, tc, pn)
	require.NoError(t, err)
	require.Len(t, pipeEntries, 1)
	assert.Equal(t, "two", pipeEntries[0].Value)
}

// Because a set is a replace, it must not also be able to change the kind: the
// storage layer overwrites the kind column, so an unguarded set would turn a
// secret into a plain value that is then printed in build logs.
func TestSetSecret_RefusesToChangeTheKindOfAnExistingEntry(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, pn := newSecretStoreService(t, "master-key")

	require.NoError(t, svc.SetTeamSecret(ctx, tc, "API_TOKEN", "tok", secret.KindSecret))

	err := svc.SetTeamSecret(ctx, tc, "API_TOKEN", "tok", secret.KindPlain)
	require.Error(t, err)
	assert.ErrorIs(t, err, pikoci.ErrSecretInvalidRequest,
		"changing the kind is a caller mistake, so the transport can answer 400")

	entries, err := svc.ListTeamSecrets(ctx, tc)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, secret.KindSecret, entries[0].Kind, "the stored kind must be untouched")
	assert.Empty(t, entries[0].Value, "it must still be treated as a secret")

	// The same rule applies at pipeline scope.
	require.NoError(t, svc.SetPipelineSecret(ctx, tc, pn, "LOG_LEVEL", "debug", secret.KindPlain))
	err = svc.SetPipelineSecret(ctx, tc, pn, "LOG_LEVEL", "debug", secret.KindSecret)
	require.Error(t, err)
	assert.ErrorIs(t, err, pikoci.ErrSecretInvalidRequest)
}

// A delete used to always audit plain.deleted, even for a secret, so the log
// disagreed with the secret.created entry the same entry was stored under.
func TestDeleteSecret_AuditsTheKindThatWasDeleted(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		name string
		kind secret.Kind
		want auditlog.Action
	}{
		{"secret", secret.KindSecret, auditlog.SecretDeleted},
		{"plain", secret.KindPlain, auditlog.PlainDeleted},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ms, _, tc, pn := newSecretStoreMocks(t, "master-key", storePipelineHCL)

			require.NoError(t, ms.P.SetTeamSecret(ctx, tc, "TEAM_ENTRY", "v", tt.kind))
			require.NoError(t, ms.P.SetPipelineSecret(ctx, tc, pn, "PIPE_ENTRY", "v", tt.kind))

			require.NoError(t, ms.P.DeleteTeamSecret(ctx, tc, "TEAM_ENTRY"))
			require.NoError(t, ms.P.DeletePipelineSecret(ctx, tc, pn, "PIPE_ENTRY"))

			var deletes []auditlog.Action
			for _, e := range *ms.Audited {
				if e.Action == auditlog.SecretDeleted || e.Action == auditlog.PlainDeleted {
					deletes = append(deletes, e.Action)
				}
			}
			assert.Equal(t, []auditlog.Action{tt.want, tt.want}, deletes,
				"team and pipeline deletes should both audit the entry's own kind")
		})
	}
}

func TestDeleteSecret_MissingEntry(t *testing.T) {
	ctx := context.Background()
	svc, _, tc, pn := newSecretStoreService(t, "master-key")

	err := svc.DeleteTeamSecret(ctx, tc, "NOPE")
	assert.ErrorIs(t, err, pikoci.ErrSecretEntryNotFound)

	err = svc.DeletePipelineSecret(ctx, tc, pn, "NOPE")
	assert.ErrorIs(t, err, pikoci.ErrSecretEntryNotFound)
}

// A malformed pipeline used to resolve to an empty value set, so the build
// failed later reporting every referenced key as missing instead of saying the
// configuration would not parse.
func TestResolvePipelineValues_ReportsParseFailure(t *testing.T) {
	ctx := context.Background()
	ms, _, tc, pn := newSecretStoreMocks(t, "master-key", `job "broken" { this is not valid hcl`)

	require.NoError(t, ms.P.SetTeamSecret(ctx, tc, "GITHUB_TOKEN", "ghp_team", secret.KindSecret))

	_, err := ms.P.ResolvePipelineValues(ctx, tc, pn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse",
		"the error should name the parse failure, not look like a missing key")
}
