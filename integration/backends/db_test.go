//go:build integration

package backends_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/utils"
)

func TestDBBackends(t *testing.T) {
	for _, system := range dbSystems() {
		system := system
		t.Run(system, func(t *testing.T) {
			setup := openDB(t, system)
			t.Run("Migrate", func(t *testing.T) {
				migrateDB(t, setup)
			})

			// All subsequent tests depend on migration having run
			ctx := context.Background()

			t.Run("UserRepository", func(t *testing.T) {
				ur := mysql.NewUserRepository(setup.querier)

				// The migration inserts a default admin user
				users, err := ur.Filter(ctx)
				require.NoError(t, err)
				require.GreaterOrEqual(t, len(users), 1)

				// Create a new user
				id, err := ur.Create(ctx, user.User{
					FullName: "Test User",
					Username: "testuser",
					Password: "hashed_password",
					Admin:    false,
				})
				require.NoError(t, err)
				assert.NotZero(t, id)

				// Find the user
				u, err := ur.Find(ctx, "testuser")
				require.NoError(t, err)
				assert.Equal(t, "testuser", u.Username)
				assert.Equal(t, "Test User", u.FullName)

				// FindWithMemberships
				um, err := ur.FindWithMemberships(ctx, "testuser")
				require.NoError(t, err)
				assert.Equal(t, "testuser", um.Username)

				// Filter all users
				users, err = ur.Filter(ctx)
				require.NoError(t, err)
				assert.GreaterOrEqual(t, len(users), 2) // default admin + testuser
			})

			t.Run("TeamRepository", func(t *testing.T) {
				tr := mysql.NewTeamRepository(setup.querier)

				// Default "Main" team exists from migration
				twm, err := tr.Find(ctx, "main")
				require.NoError(t, err)
				assert.Equal(t, "main", twm.Canonical)

				// Create a new team
				teamID, err := tr.Create(ctx, team.Team{
					Name:      "Backend Test",
					Canonical: "backend-test",
				})
				require.NoError(t, err)
				assert.NotZero(t, teamID)

				// Add member to team
				err = tr.CreateMember(ctx, "backend-test", team.Member{
					Admin: true,
					User:  user.User{Username: "admin"},
				})
				require.NoError(t, err)

				// Find team with members
				twm, err = tr.Find(ctx, "backend-test")
				require.NoError(t, err)
				assert.Equal(t, "Backend Test", twm.Name)
				assert.Len(t, twm.Members, 1)

				// FindMember
				member, err := tr.FindMember(ctx, "backend-test", "admin")
				require.NoError(t, err)
				assert.True(t, member.Admin)
			})

			t.Run("PipelineRepository", func(t *testing.T) {
				ppr := mysql.NewPipelineRepository(setup.querier)

				// Create a pipeline under default team
				ppID, err := ppr.Create(ctx, "main", pipeline.Pipeline{
					Name:      "test-pipeline",
					Canonical: "test-pipeline",
					Raw:       []byte("raw config content"),
				})
				require.NoError(t, err)
				assert.NotZero(t, ppID)

				// Find pipeline
				pp, err := ppr.Find(ctx, "main", "test-pipeline")
				require.NoError(t, err)
				assert.Equal(t, "test-pipeline", pp.Name)

				// Filter pipelines
				pps, err := ppr.Filter(ctx, "main")
				require.NoError(t, err)
				assert.GreaterOrEqual(t, len(pps), 1)
			})

			t.Run("JobRepository", func(t *testing.T) {
				jr := mysql.NewJobRepository(setup.querier)

				// Create a job
				jID, err := jr.Create(ctx, "main", "test-pipeline", job.Job{
					Name: "test-job",
				})
				require.NoError(t, err)
				assert.NotZero(t, jID)

				// Find job
				j, err := jr.Find(ctx, "main", "test-pipeline", "test-job")
				require.NoError(t, err)
				assert.Equal(t, "test-job", j.Name)

				// Filter jobs
				jobs, err := jr.Filter(ctx, "main", "test-pipeline")
				require.NoError(t, err)
				assert.Len(t, jobs, 1)
			})

			t.Run("ResourceRepository", func(t *testing.T) {
				rr := mysql.NewResourceRepository(setup.querier, system)

				// Create a resource
				rID, err := rr.Create(ctx, "main", "test-pipeline", resource.Resource{
					Name:          "test-resource",
					Type:          "git",
					Canonical:     "git-test-resource",
					CheckInterval: "@every 1m",
				})
				require.NoError(t, err)
				assert.NotZero(t, rID)

				// Find resource
				r, err := rr.Find(ctx, "main", "test-pipeline", "git-test-resource")
				require.NoError(t, err)
				assert.Equal(t, "test-resource", r.Name)
				assert.Equal(t, "git", r.Type)

				// Create version
				vID, err := rr.CreateVersion(ctx, "main", "test-pipeline", "git-test-resource", resource.Version{
					Version: map[string]interface{}{"ref": "abc123"},
				})
				require.NoError(t, err)
				assert.NotZero(t, vID)

				// Filter versions
				versions, err := rr.FilterVersions(ctx, "main", "test-pipeline", "git-test-resource", nil, nil, 0)
				require.NoError(t, err)
				assert.Len(t, versions, 1)
			})

			t.Run("BuildRepository", func(t *testing.T) {
				br := mysql.NewBuildRepository(setup.querier, system)

				// Create a build
				bID, bn, err := br.Create(ctx, "main", "test-pipeline", "test-job", build.Build{
					Status: build.Started,
				})
				require.NoError(t, err)
				assert.NotZero(t, bID)
				assert.Equal(t, "1", bn)

				// Filter builds
				builds, err := br.Filter(ctx, "main", "test-pipeline", "test-job", nil, nil, 0)
				require.NoError(t, err)
				assert.Len(t, builds, 1)
				assert.Equal(t, build.Started, builds[0].Status)
			})

			t.Run("SecretTypeRepository", func(t *testing.T) {
				str := mysql.NewSecretTypeRepository(setup.querier)

				// Create a secret type with config
				stID, err := str.Create(ctx, "main", "test-pipeline", sectype.SecretType{
					Name:   "vault",
					Params: []string{"path"},
					Config: map[string]string{"address": "http://vault:8200", "token": "test-token"},
					Get: utils.RunnerCommand{
						Runner: "exec",
						Args:   []string{"-ec", "echo '{\"user\":\"admin\"}'"},
					},
				})
				require.NoError(t, err)
				assert.NotZero(t, stID)

				// Find secret type
				st, err := str.Find(ctx, "main", "test-pipeline", "vault")
				require.NoError(t, err)
				assert.Equal(t, "vault", st.Name)
				assert.Equal(t, []string{"path"}, st.Params)
				assert.Equal(t, "http://vault:8200", st.Config["address"])
				assert.Equal(t, "test-token", st.Config["token"])
				assert.Equal(t, "exec", st.Get.Runner)

				// Filter secret types
				sts, err := str.Filter(ctx, "main", "test-pipeline")
				require.NoError(t, err)
				assert.Len(t, sts, 1)

				// Update secret type
				err = str.Update(ctx, "main", "test-pipeline", "vault", sectype.SecretType{
					Name:   "vault",
					Params: []string{"path", "key"},
					Config: map[string]string{"address": "http://vault:8200", "token": "new-token"},
					Get: utils.RunnerCommand{
						Runner: "exec",
						Args:   []string{"-ec", "echo '{\"user\":\"root\"}'"},
					},
				})
				require.NoError(t, err)

				st, err = str.Find(ctx, "main", "test-pipeline", "vault")
				require.NoError(t, err)
				assert.Equal(t, []string{"path", "key"}, st.Params)
				assert.Equal(t, "new-token", st.Config["token"])

				// Delete
				_, err = str.Create(ctx, "main", "test-pipeline", sectype.SecretType{
					Name: "aws-ssm", Params: []string{"name"},
					Get: utils.RunnerCommand{Runner: "exec", Args: []string{"-ec", "echo '{}'"}},
				})
				require.NoError(t, err)

				sts, err = str.Filter(ctx, "main", "test-pipeline")
				require.NoError(t, err)
				assert.Len(t, sts, 2)

				err = str.Delete(ctx, "main", "test-pipeline", "aws-ssm")
				require.NoError(t, err)

				sts, err = str.Filter(ctx, "main", "test-pipeline")
				require.NoError(t, err)
				assert.Len(t, sts, 1)
			})

			t.Run("PipelineWithSecretTypes", func(t *testing.T) {
				ppr := mysql.NewPipelineRepository(setup.querier)

				pp, err := ppr.Find(ctx, "main", "test-pipeline")
				require.NoError(t, err)
				assert.Equal(t, "test-pipeline", pp.Name)
				assert.Len(t, pp.SecretTypes, 1)
				assert.Equal(t, "vault", pp.SecretTypes[0].Name)
			})
		})
	}
}
