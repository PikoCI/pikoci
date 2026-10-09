package mysql_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/notification"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The repository updates find their row through a subquery on the parent
// tables (team → pipeline → job), not by joining the target table to itself,
// so they run unchanged on MySQL. These tests pin the scoping: every update
// runs against two identically named rows in different teams, and only the
// targeted one may change.

// twinTeams creates two teams with the same pipeline canonical in each and
// returns the target team, the bystander team and the pipeline canonical. Names are unique per test because the in-memory
// database is shared across the package's tests.
func twinTeams(t *testing.T, db *sql.DB, prefix string) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	tr := mysql.NewTeamRepository(db)
	pr := mysql.NewPipelineRepository(db)
	tc, other, pc := prefix+"-team", prefix+"-other", prefix+"-pipe"
	// The bystander is created first: SQLite returns the first row of a
	// scalar subquery that matches several, so a mis-scoped update would
	// then hit the bystander and fail the test instead of passing by luck.
	for _, c := range []string{other, tc} {
		_, err := tr.Create(ctx, team.Team{Name: c, Canonical: c})
		require.NoError(t, err)
		_, err = pr.Create(ctx, c, pipeline.Pipeline{Name: pc, Canonical: pc})
		require.NoError(t, err)
	}
	return tc, other, pc
}

func TestScopedUpdate_Pipeline(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	tc, other, pc := twinTeams(t, db, "su-pipe")
	pr := mysql.NewPipelineRepository(db)

	require.NoError(t, pr.Update(ctx, tc, pc, pipeline.Pipeline{Name: pc, Canonical: pc, Raw: []byte("updated")}))
	require.NoError(t, pr.SetPublic(ctx, tc, pc, true))

	p, err := pr.Find(ctx, tc, pc)
	require.NoError(t, err)
	assert.Equal(t, []byte("updated"), p.Raw)
	assert.True(t, p.Public)

	o, err := pr.Find(ctx, other, pc)
	require.NoError(t, err)
	assert.Empty(t, o.Raw, "the other team's pipeline must be untouched")
	assert.False(t, o.Public)

	assert.Error(t, pr.SetPublic(ctx, tc, "missing", true), "no row matched")
}

func TestScopedUpdate_Job(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	tc, other, pc := twinTeams(t, db, "su-job")
	jr := mysql.NewJobRepository(db)
	for _, c := range []string{tc, other} {
		_, err := jr.Create(ctx, c, pc, job.Job{Name: "build"})
		require.NoError(t, err)
		_, err = jr.Create(ctx, c, pc, job.Job{Name: "deploy"})
		require.NoError(t, err)
	}
	find := func(c, jn string) *job.Job {
		j, err := jr.Find(ctx, c, pc, jn)
		require.NoError(t, err)
		return j
	}

	require.NoError(t, jr.Update(ctx, tc, pc, "build", job.Job{Name: "build", Concurrency: 3}))
	assert.Equal(t, 3, find(tc, "build").Concurrency)
	assert.Zero(t, find(tc, "deploy").Concurrency, "only the named job")
	assert.Zero(t, find(other, "build").Concurrency, "only the named team")

	require.NoError(t, jr.SetPaused(ctx, tc, pc, "build", true))
	assert.True(t, find(tc, "build").Paused)
	assert.False(t, find(tc, "deploy").Paused)
	assert.False(t, find(other, "build").Paused)

	require.NoError(t, jr.PauseAll(ctx, tc, pc))
	assert.True(t, find(tc, "deploy").Paused)
	assert.False(t, find(other, "deploy").Paused, "PauseAll stays inside the pipeline")

	require.NoError(t, jr.UnpauseAll(ctx, tc, pc))
	assert.False(t, find(tc, "build").Paused)
	assert.False(t, find(tc, "deploy").Paused)
}

func TestScopedUpdate_Resource(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	tc, other, pc := twinTeams(t, db, "su-res")
	rr := mysql.NewResourceRepository(db, mysql.Mem)
	for _, c := range []string{tc, other} {
		_, err := rr.Create(ctx, c, pc, resource.Resource{Name: "repo", Type: "git", Canonical: "git.repo"})
		require.NoError(t, err)
	}
	find := func(c string) *resource.Resource {
		r, err := rr.Find(ctx, c, pc, "git.repo")
		require.NoError(t, err)
		return r
	}

	next := time.Now().Add(time.Hour).Truncate(time.Second)
	require.NoError(t, rr.Update(ctx, tc, pc, "git.repo", resource.Resource{Name: "repo", Type: "git", Canonical: "git.repo", CheckInterval: "@every 5m", NextCheck: next}))
	assert.Equal(t, "@every 5m", find(tc).CheckInterval)
	assert.True(t, next.Equal(find(tc).NextCheck), "time columns round-trip")
	assert.Empty(t, find(other).CheckInterval)

	require.NoError(t, rr.UpdateLogs(ctx, tc, pc, "git.repo", "boom"))
	assert.Equal(t, "boom", find(tc).Logs)
	assert.Empty(t, find(other).Logs)

	vID, err := rr.CreateVersion(ctx, tc, pc, "git.repo", resource.Version{Version: map[string]interface{}{"ref": "abc"}})
	require.NoError(t, err)
	require.NoError(t, rr.PinVersion(ctx, tc, pc, "git.repo", vID))
	require.NotNil(t, find(tc).PinnedVersionID)
	assert.Equal(t, vID, *find(tc).PinnedVersionID)
	assert.Nil(t, find(other).PinnedVersionID)

	require.NoError(t, rr.UnpinVersion(ctx, tc, pc, "git.repo"))
	assert.Nil(t, find(tc).PinnedVersionID)

	assert.Error(t, rr.UpdateLogs(ctx, tc, pc, "git.missing", "x"), "no row matched")
}

func TestScopedUpdate_PipelineEntities(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	tc, other, pc := twinTeams(t, db, "su-ent")
	rtr := mysql.NewResourceTypeRepository(db)
	rur := mysql.NewRunnerRepository(db)
	str := mysql.NewSecretTypeRepository(db)
	ntr := mysql.NewNotificationTypeRepository(db)
	nr := mysql.NewNotificationRepository(db)
	for _, c := range []string{tc, other} {
		_, err := rtr.Create(ctx, c, pc, restype.ResourceType{Name: "tick"})
		require.NoError(t, err)
		_, err = rur.Create(ctx, c, pc, runner.Runner{Name: "sh"})
		require.NoError(t, err)
		_, err = str.Create(ctx, c, pc, sectype.SecretType{Name: "vault"})
		require.NoError(t, err)
		_, err = ntr.Create(ctx, c, pc, notiftype.NotificationType{Name: "log"})
		require.NoError(t, err)
		_, err = nr.Create(ctx, c, pc, notification.Notification{Type: "log", Name: "ci", Canonical: "log.ci"})
		require.NoError(t, err)
	}

	require.NoError(t, rtr.Update(ctx, tc, pc, "tick", restype.ResourceType{Name: "tick", Source: "pikoci://updated"}))
	require.NoError(t, rur.Update(ctx, tc, pc, "sh", runner.Runner{Name: "sh", Source: "pikoci://updated"}))
	require.NoError(t, str.Update(ctx, tc, pc, "vault", sectype.SecretType{Name: "vault", Source: "pikoci://updated"}))
	require.NoError(t, ntr.Update(ctx, tc, pc, "log", notiftype.NotificationType{Name: "log", Source: "pikoci://updated"}))
	require.NoError(t, nr.Update(ctx, tc, pc, "log.ci", notification.Notification{Type: "log", Name: "ci", Canonical: "log.ci", Message: "updated"}))

	for c, want := range map[string]string{tc: "pikoci://updated", other: ""} {
		rt, err := rtr.Find(ctx, c, pc, "tick")
		require.NoError(t, err)
		assert.Equal(t, want, rt.Source, "resource type in %s", c)
		ru, err := rur.Find(ctx, c, pc, "sh")
		require.NoError(t, err)
		assert.Equal(t, want, ru.Source, "runner in %s", c)
		st, err := str.Find(ctx, c, pc, "vault")
		require.NoError(t, err)
		assert.Equal(t, want, st.Source, "secret type in %s", c)
		nt, err := ntr.Find(ctx, c, pc, "log")
		require.NoError(t, err)
		assert.Equal(t, want, nt.Source, "notification type in %s", c)
	}
	n, err := nr.Find(ctx, tc, pc, "log.ci")
	require.NoError(t, err)
	assert.Equal(t, "updated", n.Message)
	o, err := nr.Find(ctx, other, pc, "log.ci")
	require.NoError(t, err)
	assert.Empty(t, o.Message, "notification in the other team")
}

func TestScopedUpdate_Build(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	tc, other, pc := twinTeams(t, db, "su-build")
	jr := mysql.NewJobRepository(db)
	br := mysql.NewBuildRepository(db, mysql.Mem)
	var bn string
	for _, c := range []string{tc, other} {
		for _, jn := range []string{"build", "deploy"} {
			_, err := jr.Create(ctx, c, pc, job.Job{Name: jn})
			require.NoError(t, err)
			_, bn, err = br.Create(ctx, c, pc, jn, build.Build{Status: build.Started})
			require.NoError(t, err)
		}
	}
	status := func(c, jn string) build.Status {
		b, err := br.Find(ctx, c, pc, jn, bn)
		require.NoError(t, err)
		return b.Status
	}

	require.NoError(t, br.Update(ctx, tc, pc, "build", bn, build.Build{Status: build.Succeeded}))
	assert.Equal(t, build.Succeeded, status(tc, "build"))
	assert.Equal(t, build.Started, status(tc, "deploy"), "same build number, other job")
	assert.Equal(t, build.Started, status(other, "build"), "same job and number, other team")
}

func TestScopedUpdate_TeamMember(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	tc, other, _ := twinTeams(t, db, "su-member")
	ur := mysql.NewUserRepository(db)
	tr := mysql.NewTeamRepository(db)
	_, err := ur.Create(ctx, user.User{FullName: "Scoped", Username: "su-member-user", Password: "x"})
	require.NoError(t, err)
	_, err = ur.Create(ctx, user.User{FullName: "Bystander", Username: "su-member-bystander", Password: "x"})
	require.NoError(t, err)
	for _, c := range []string{tc, other} {
		for _, un := range []string{"su-member-user", "su-member-bystander"} {
			require.NoError(t, tr.CreateMember(ctx, c, team.Member{Role: role.Read, User: user.User{Username: un}}))
		}
	}
	roleOf := func(c, un string) role.Role {
		m, err := tr.FindMember(ctx, c, un)
		require.NoError(t, err)
		return m.Role
	}

	require.NoError(t, tr.UpdateMember(ctx, tc, "su-member-user", team.Member{Role: role.Maintain}))
	assert.Equal(t, role.Maintain, roleOf(tc, "su-member-user"))
	assert.Equal(t, role.Read, roleOf(tc, "su-member-bystander"), "same team, other user")
	assert.Equal(t, role.Read, roleOf(other, "su-member-user"), "same user, other team")
}
