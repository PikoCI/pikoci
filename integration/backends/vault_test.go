//go:build integration

package backends_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/worker"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/mempubsub"
)

// setupVault returns the vault address and token, either from an already-running
// vault (via PIKOCI_TEST_VAULT_ADDR) or by starting a new Docker container.
// It skips the test if prerequisites are missing.
func setupVault(t *testing.T) (vaultAddr, vaultToken string) {
	t.Helper()

	vaultToken = envOr("PIKOCI_TEST_VAULT_TOKEN", "test-root-token")

	// If PIKOCI_TEST_VAULT_ADDR is set, use an already-running vault (e.g. from docker-compose)
	if addr := os.Getenv("PIKOCI_TEST_VAULT_ADDR"); addr != "" {
		return addr, vaultToken
	}

	// Otherwise, start our own container
	for _, bin := range []string{"docker", "vault", "jq"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Fatalf("%s not found in PATH", bin)
		}
	}

	containerName := fmt.Sprintf("pikoci-vault-test-%d", os.Getpid())

	// Clean up any leftover container from a previous failed run
	exec.Command("docker", "rm", "-f", containerName).Run()

	startCmd := exec.Command("docker", "run", "-d",
		"--name", containerName,
		"--cap-add=IPC_LOCK",
		"-p", "0:8200",
		"-e", "VAULT_DEV_ROOT_TOKEN_ID="+vaultToken,
		"-e", "VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200",
		"-e", "SKIP_SETCAP=1",
		"hashicorp/vault:latest",
	)
	out, err := startCmd.CombinedOutput()
	require.NoError(t, err, "failed to start vault container: %s", string(out))

	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", containerName).Run()
	})

	portCmd := exec.Command("docker", "port", containerName, "8200/tcp")
	portOut, err := portCmd.Output()
	require.NoError(t, err, "failed to get vault port: %s", string(portOut))
	portLine := strings.Split(strings.TrimSpace(string(portOut)), "\n")[0]

	return "http://" + portLine, vaultToken
}

// TestSecretsVaultE2E creates a pipeline with the built-in pikoci://vault secret_type,
// seeds a secret in Vault, triggers a job, and verifies secret values appear in the task logs.
//
// Requires: vault CLI, jq, and either:
//   - PIKOCI_TEST_VAULT_ADDR set (e.g. from make test-services-up), or
//   - docker available (will start its own container)
//
// Skipped unless PIKOCI_TEST_VAULT=1 is set.
func TestSecretsVaultE2E(t *testing.T) {
	if os.Getenv("PIKOCI_TEST_VAULT") == "" {
		t.Skip("set PIKOCI_TEST_VAULT=1 to run Vault integration test (requires vault, jq)")
	}

	for _, bin := range []string{"vault", "jq"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not found in PATH, skipping", bin)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	vaultAddr, vaultToken := setupVault(t)

	// Wait for Vault to be ready
	require.Eventually(t, func() bool {
		cmd := exec.CommandContext(ctx, "vault", "status")
		cmd.Env = append(os.Environ(), "VAULT_ADDR="+vaultAddr, "VAULT_TOKEN="+vaultToken)
		return cmd.Run() == nil
	}, 15*time.Second, 500*time.Millisecond, "vault not reachable at %s", vaultAddr)

	// Seed a secret
	seedCmd := exec.CommandContext(ctx, "vault", "kv", "put", "secret/db-creds",
		"username=dbadmin", "password=sup3rs3cret",
	)
	seedCmd.Env = append(os.Environ(), "VAULT_ADDR="+vaultAddr, "VAULT_TOKEN="+vaultToken)
	seedOut, err := seedCmd.CombinedOutput()
	require.NoError(t, err, "failed to seed vault secret: %s", string(seedOut))

	// Set up PikoCI with in-memory DB
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("service", "vault-test")

	db, err := mysql.New("", 0, "", "", mysql.Options{
		MultiStatements: true,
		ClientFoundRows: true,
		System:          mysql.Mem,
	})
	require.NoError(t, err)

	err = migrate.Migrate(db, mysql.Mem)
	require.NoError(t, err)

	topic, err := pubsub.OpenTopic(ctx, fmt.Sprintf("%s://vault-test", mempubsub.Scheme))
	require.NoError(t, err)
	defer topic.Shutdown(ctx)

	ur := mysql.NewUserRepository(db)
	tr := mysql.NewTeamRepository(db)
	ppr := mysql.NewPipelineRepository(db)
	jr := mysql.NewJobRepository(db)
	rr := mysql.NewResourceRepository(db, mysql.Mem)
	rt := mysql.NewResourceTypeRepository(db)
	br := mysql.NewBuildRepository(db, mysql.Mem)
	rur := mysql.NewRunnerRepository(db)
	str := mysql.NewSecretTypeRepository(db)
	tgr := mysql.NewTriggerRepository(db)
	suow := unitwork.NewStartUnitOfWork(db, mysql.Mem)

	svc := pikoci.New(ctx, topic, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, suow, []byte("jwt"), logger)
	svc.StartScheduler(ctx)

	_, _ = svc.CreateUser(ctx, user.User{
		FullName: "admin", Username: "admin",
		Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
	}, true)

	// Start worker
	subscription, err := pubsub.OpenSubscription(ctx, fmt.Sprintf("%s://vault-test", mempubsub.Scheme))
	require.NoError(t, err)
	defer subscription.Shutdown(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w := worker.New(svc, topic, subscription, logger.With("component", "worker"))
		w.Run(ctx)
	}()

	// Create pipeline using built-in pikoci://vault secret_type
	// Address and token are config on the secret_type, secrets are resolved via variables
	hclConfig := []byte(fmt.Sprintf(`
secret_type "my-vault" {
  source  = "pikoci://vault"
  address = "%s"
  token   = "%s"
}

variable "db_username" {
  type = string
  secret "my-vault" {
    path = "secret/db-creds"
    key  = "username"
  }
}

variable "db_password" {
  type = string
  secret "my-vault" {
    path = "secret/db-creds"
    key  = "password"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  get "cron" "timer" {
    trigger = true
  }
  task "use-vault-secrets" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo username=${var.db_username} password=${var.db_password}"]
    }
  }
}
`, vaultAddr, vaultToken))

	pp, err := svc.CreatePipeline(ctx, "main", "vault-e2e", hclConfig, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)
	assert.Equal(t, "pikoci://vault", pp.SecretTypes[0].Source)

	// Trigger the resource to create a version
	err = svc.TriggerPipelineResource(ctx, "main", "vault-e2e", "cron.timer")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		vers, err := svc.ListResourceVersions(ctx, "main", "vault-e2e", "cron.timer")
		return err == nil && len(vers) > 0
	}, 10*time.Second, 200*time.Millisecond)

	// Wait for the build to finish
	var builds []*build.Build
	require.Eventually(t, func() bool {
		builds, err = svc.ListJobBuilds(ctx, "main", "vault-e2e", "deploy")
		if err != nil || len(builds) == 0 {
			return false
		}
		return builds[0].Status != build.Started
	}, 20*time.Second, 200*time.Millisecond)

	require.NotEmpty(t, builds)
	b := builds[0]
	assert.Equal(t, build.Succeeded, b.Status, "build should succeed, error: %s", b.Error)

	var taskStep *build.Step
	for i, s := range b.Steps {
		if s.Type == "task" && s.Name == "use-vault-secrets" {
			taskStep = &b.Steps[i]
			break
		}
	}
	require.NotNil(t, taskStep, "task step 'use-vault-secrets' should exist")
	assert.Contains(t, taskStep.Logs, "username=dbadmin", "logs should contain username=dbadmin, got: %s", taskStep.Logs)
	assert.Contains(t, taskStep.Logs, "password=sup3rs3cret", "logs should contain password=sup3rs3cret, got: %s", taskStep.Logs)

	cancel()
	wg.Wait()
}
