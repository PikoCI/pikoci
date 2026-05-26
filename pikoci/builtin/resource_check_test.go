package builtin_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2/hclsimple"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xescugc/pikoci/pikoci/builtin"
	"github.com/xescugc/pikoci/pikoci/restype"
	"github.com/xescugc/pikoci/pikoci/utils"
)

// runScript extracts a shell script from a RunnerCommand and executes it
// with the given environment variables. It returns combined stdout+stderr.
func runScript(t *testing.T, rc *utils.RunnerCommand, workDir string, env map[string]string) (string, error) {
	t.Helper()
	require.NotNil(t, rc)
	require.GreaterOrEqual(t, len(rc.Args), 2, "expected at least 2 args (flag + script)")

	script := rc.Args[1]

	cmd := exec.Command("/bin/sh", "-ec", script)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	out, err := cmd.CombinedOutput()
	return string(out), err
}

// fakeCurlDir creates a directory with a fake curl script that logs all
// arguments to curl.log and prints the canned response to stdout.
// Returns the directory (to prepend to PATH) and the log file path.
func fakeCurlDir(t *testing.T, response string) (dir string, logFile string) {
	t.Helper()
	dir = t.TempDir()
	logFile = filepath.Join(dir, "curl.log")

	script := `#!/bin/sh
# Log all arguments so the test can inspect them
echo "$@" >> ` + logFile + `
cat << 'RESP'
` + response + `
RESP
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "curl"), []byte(script), 0755))
	return dir, logFile
}

// initBareRepo creates a bare git repo with one commit and returns the
// bare dir, the work dir, and a helper to run git in the work dir.
func initBareRepo(t *testing.T) (bareDir, workDir string, runWork func(args ...string)) {
	t.Helper()
	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)

	bareDir = t.TempDir()
	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = bareDir
	cmd.Env = gitEnv
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git init --bare failed: %s", out)

	workDir = t.TempDir()
	cmd = exec.Command("git", "clone", bareDir, workDir)
	cmd.Env = gitEnv
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "git clone failed: %s", out)

	runWork = func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = workDir
		cmd.Env = gitEnv
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v failed: %s", args, out)
	}

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "file.txt"), []byte("hello"), 0644))
	runWork("add", ".")
	runWork("commit", "-m", "first commit")
	runWork("push", "origin", "HEAD")

	return bareDir, workDir, runWork
}

// headSHA returns the HEAD sha of the given git directory.
func headSHA(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git rev-parse HEAD failed: %s", out)
	return strings.TrimSpace(string(out))
}

// --- Cron resource type ---

func TestCronCheck_ReturnsSingleVersion(t *testing.T) {
	rts := builtin.ResourceTypes()
	rt := rts["cron"]

	out, err := runScript(t, rt.Check, t.TempDir(), nil)
	require.NoError(t, err, "cron check failed: %s", out)

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1, "cron check should return exactly 1 version")
	assert.Contains(t, versions[0], "date")
}

// --- Git resource type: check ---

func TestGitCheck_LsRemote_ReturnsSingleVersion(t *testing.T) {
	bareDir, _, _ := initBareRepo(t)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url": bareDir,
	})
	require.NoError(t, err, "git check (ls-remote) failed: %s", out)

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1)
	assert.Contains(t, versions[0], "ref")
	assert.NotEmpty(t, versions[0]["ref"])
}

func TestGitCheck_LsRemote_WithBranch(t *testing.T) {
	bareDir, workDir, runWork := initBareRepo(t)
	runWork("checkout", "-b", "develop")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "file2.txt"), []byte("world"), 0644))
	runWork("add", ".")
	runWork("commit", "-m", "second commit")
	runWork("push", "origin", "develop")

	expectedSHA := headSHA(t, workDir)

	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":    bareDir,
		"param_branch": "develop",
	})
	require.NoError(t, err, "git check (ls-remote branch) failed: %s", out)

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1)
	assert.Equal(t, expectedSHA, versions[0]["ref"])
}

func TestGitCheck_GitHubAPI_ReturnsSingleVersion(t *testing.T) {
	cannedCommits := `[{"sha":"abc123"}]`
	curlDir, logFile := fakeCurlDir(t, cannedCommits)

	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://github.com/example/repo",
		"param_token": "fake-token",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitHub API) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "api.github.com/repos/example/repo/commits")
	assert.Contains(t, string(curlLog), "per_page=1")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1)
	assert.Equal(t, "abc123", versions[0]["ref"])
}

func TestGitCheck_GitLabAPI_ReturnsSingleVersion(t *testing.T) {
	cannedCommits := `[{"id":"def456"}]`
	curlDir, logFile := fakeCurlDir(t, cannedCommits)

	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://gitlab.com/example/repo",
		"param_token": "fake-token",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitLab API) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "gitlab.com/api/v4/projects/example%2Frepo/repository/commits")
	assert.Contains(t, string(curlLog), "per_page=1")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1)
	assert.Equal(t, "def456", versions[0]["ref"])
}

func TestGitCheck_TagMode_GitHub_FirstCheck(t *testing.T) {
	cannedTags := `[{"name":"v3","commit":{"sha":"aaa"}},{"name":"v2","commit":{"sha":"bbb"}},{"name":"v1","commit":{"sha":"ccc"}}]`
	curlDir, logFile := fakeCurlDir(t, cannedTags)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://github.com/example/repo",
		"param_token": "fake-token",
		"param_tag":   "true",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitHub tag, first) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "per_page=1",
		"first check should request per_page=1")
}

func TestGitCheck_TagMode_GitHub_SubsequentCheck(t *testing.T) {
	cannedTags := `[{"name":"v3","commit":{"sha":"aaa"}},{"name":"v2","commit":{"sha":"bbb"}}]`
	curlDir, logFile := fakeCurlDir(t, cannedTags)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://github.com/example/repo",
		"param_token": "fake-token",
		"param_tag":   "true",
		"version_tag": "v1",
		"version_ref": "old-sha",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitHub tag, subsequent) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "per_page=100",
		"subsequent check should request per_page=100")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 2)
}

func TestGitCheck_TagMode_GitLab_FirstCheck(t *testing.T) {
	cannedTags := `[{"name":"v3","commit":{"id":"aaa"}},{"name":"v2","commit":{"id":"bbb"}}]`
	curlDir, logFile := fakeCurlDir(t, cannedTags)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://gitlab.com/example/repo",
		"param_token": "fake-token",
		"param_tag":   "true",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitLab tag, first) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "gitlab.com/api/v4/projects/example%2Frepo/repository/tags")
	assert.Contains(t, string(curlLog), "per_page=1")
}

func TestGitCheck_TagMode_GitLab_SubsequentCheck(t *testing.T) {
	cannedTags := `[{"name":"v3","commit":{"id":"aaa"}},{"name":"v2","commit":{"id":"bbb"}}]`
	curlDir, logFile := fakeCurlDir(t, cannedTags)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://gitlab.com/example/repo",
		"param_token": "fake-token",
		"param_tag":   "true",
		"version_tag": "v1",
		"version_ref": "old-sha",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitLab tag, subsequent) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "per_page=100")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 2)
}

func TestGitCheck_PRMode_GitHub_FirstCheck(t *testing.T) {
	cannedPRs := `[{"number":10,"head":{"sha":"aaa"}},{"number":9,"head":{"sha":"bbb"}}]`
	curlDir, logFile := fakeCurlDir(t, cannedPRs)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://github.com/example/repo",
		"param_token": "fake-token",
		"param_pr":    "true",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitHub PR, first) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "per_page=1")
}

func TestGitCheck_PRMode_GitHub_SubsequentCheck(t *testing.T) {
	cannedPRs := `[{"number":10,"head":{"sha":"aaa"}},{"number":9,"head":{"sha":"bbb"}}]`
	curlDir, logFile := fakeCurlDir(t, cannedPRs)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://github.com/example/repo",
		"param_token": "fake-token",
		"param_pr":    "true",
		"version_pr":  "8",
		"version_ref": "old-sha",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitHub PR, subsequent) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "per_page=100")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 2)
}

func TestGitCheck_PRMode_GitLab_FirstCheck(t *testing.T) {
	cannedMRs := `[{"iid":10,"sha":"aaa"},{"iid":9,"sha":"bbb"}]`
	curlDir, logFile := fakeCurlDir(t, cannedMRs)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://gitlab.com/example/repo",
		"param_token": "fake-token",
		"param_pr":    "true",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitLab MR, first) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "gitlab.com/api/v4/projects/example%2Frepo/merge_requests")
	assert.Contains(t, string(curlLog), "per_page=1")
}

func TestGitCheck_PRMode_GitLab_SubsequentCheck(t *testing.T) {
	cannedMRs := `[{"iid":10,"sha":"aaa"},{"iid":9,"sha":"bbb"}]`
	curlDir, logFile := fakeCurlDir(t, cannedMRs)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://gitlab.com/example/repo",
		"param_token": "fake-token",
		"param_pr":    "true",
		"version_pr":  "8",
		"version_ref": "old-sha",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitLab MR, subsequent) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "per_page=100")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 2)
}

func TestGitCheck_TagMode_RequiresToken(t *testing.T) {
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url": "https://github.com/example/repo",
		"param_tag": "true",
	})
	assert.Error(t, err, "tag mode without token should fail")
	assert.Contains(t, out, "tag=true requires a token")
}

func TestGitCheck_PRMode_RequiresToken(t *testing.T) {
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url": "https://github.com/example/repo",
		"param_pr":  "true",
	})
	assert.Error(t, err, "PR mode without token should fail")
	assert.Contains(t, out, "pr=true requires a token")
}

// --- Git resource type: pull ---

func TestGitPull_DefaultBranch(t *testing.T) {
	bareDir, workDir, _ := initBareRepo(t)
	sha := headSHA(t, workDir)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	pullDir := t.TempDir()
	out, err := runScript(t, rt.Pull, pullDir, map[string]string{
		"param_url":  bareDir,
		"param_name": "myrepo",
		"version_ref": sha,
	})
	require.NoError(t, err, "git pull (default) failed: %s", out)

	// Verify the repo was cloned and checked out at the right ref
	clonedSHA := headSHA(t, filepath.Join(pullDir, "myrepo"))
	assert.Equal(t, sha, clonedSHA)
}

func TestGitPull_WithBranch(t *testing.T) {
	bareDir, workDir, runWork := initBareRepo(t)
	runWork("checkout", "-b", "feature")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "feature.txt"), []byte("feature"), 0644))
	runWork("add", ".")
	runWork("commit", "-m", "feature commit")
	runWork("push", "origin", "feature")
	featureSHA := headSHA(t, workDir)

	rts := builtin.ResourceTypes()
	rt := rts["git"]

	pullDir := t.TempDir()
	out, err := runScript(t, rt.Pull, pullDir, map[string]string{
		"param_url":    bareDir,
		"param_name":   "myrepo",
		"param_branch": "feature",
		"version_ref":  featureSHA,
	})
	require.NoError(t, err, "git pull (branch) failed: %s", out)

	clonedSHA := headSHA(t, filepath.Join(pullDir, "myrepo"))
	assert.Equal(t, featureSHA, clonedSHA)

	// Verify the feature file exists
	_, err = os.Stat(filepath.Join(pullDir, "myrepo", "feature.txt"))
	assert.NoError(t, err)
}

func TestGitPull_TagMode(t *testing.T) {
	bareDir, workDir, runWork := initBareRepo(t)
	runWork("tag", "v1.0.0")
	runWork("push", "origin", "v1.0.0")
	tagSHA := headSHA(t, workDir)

	rts := builtin.ResourceTypes()
	rt := rts["git"]

	pullDir := t.TempDir()
	out, err := runScript(t, rt.Pull, pullDir, map[string]string{
		"param_url":   bareDir,
		"param_name":  "myrepo",
		"param_tag":   "true",
		"version_tag": "v1.0.0",
		"version_ref": tagSHA,
	})
	require.NoError(t, err, "git pull (tag) failed: %s", out)

	clonedSHA := headSHA(t, filepath.Join(pullDir, "myrepo"))
	assert.Equal(t, tagSHA, clonedSHA)
}

// --- Git resource type: push ---

func TestGitPush(t *testing.T) {
	bareDir, workDir, runWork := initBareRepo(t)
	_ = workDir

	// Clone into a fresh dir (simulating the pull step output)
	pushDir := t.TempDir()
	cmd := exec.Command("git", "clone", bareDir, filepath.Join(pushDir, "myrepo"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "clone failed: %s", out)

	// Make a change and commit
	repoDir := filepath.Join(pushDir, "myrepo")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "pushed.txt"), []byte("pushed"), 0644))
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v failed: %s", args, out)
	}
	gitRun("add", ".")
	gitRun("commit", "-m", "push test")

	pushSHA := headSHA(t, repoDir)

	rts := builtin.ResourceTypes()
	rt := rts["git"]

	pushOut, err := runScript(t, rt.Push, pushDir, map[string]string{
		"param_url":  bareDir,
		"param_name": "myrepo",
	})
	require.NoError(t, err, "git push failed: %s", pushOut)

	// Verify the bare repo received the push
	_ = runWork // silence unused
	remoteSHA := headSHA(t, bareDir)
	assert.Equal(t, pushSHA, remoteSHA)
}

// --- GitHub Check resource type: push ---
//
// github-check.hcl is not included in the embed directive (builtin.go),
// so we parse and test it directly from the HCL file.

// parseGitHubCheckPush parses the github-check.hcl file and returns
// the push script. Returns empty string if parsing fails.
func parseGitHubCheckPush(t *testing.T) string {
	t.Helper()

	// github-check.hcl is not embedded; read it directly from disk.
	data, err := os.ReadFile("resource_types/github-check.hcl")
	require.NoError(t, err, "failed to read github-check.hcl")

	var parsed struct {
		ResourceTypes []restype.ResourceType `hcl:"resource_type,block"`
	}
	err = hclsimple.Decode("github-check.hcl", data, nil, &parsed)
	require.NoError(t, err, "failed to parse github-check.hcl")
	require.Len(t, parsed.ResourceTypes, 1)
	require.NotNil(t, parsed.ResourceTypes[0].Push)
	require.GreaterOrEqual(t, len(parsed.ResourceTypes[0].Push.Args), 2)
	return parsed.ResourceTypes[0].Push.Args[1]
}

func runGitHubCheckPush(t *testing.T, workDir string, env map[string]string) (string, error) {
	t.Helper()
	script := parseGitHubCheckPush(t)

	cmd := exec.Command("/bin/sh", "-ec", script)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestGitHubCheckPush_CreateCheckRun(t *testing.T) {
	// The github-check push script needs: openssl, jq, and curl.
	// We fake curl; openssl and jq must be available on the system.
	for _, tool := range []string{"openssl", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available, skipping github-check push test", tool)
		}
	}

	// Generate a throwaway RSA key for JWT signing
	workDir := t.TempDir()
	keyFile := filepath.Join(workDir, "test-key.pem")
	cmd := exec.Command("openssl", "genrsa", "-out", keyFile, "2048")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "openssl genrsa failed: %s", out)
	keyData, err := os.ReadFile(keyFile)
	require.NoError(t, err)

	// Fake curl: first call returns installation token, second returns check run
	curlDir := t.TempDir()
	logFile := filepath.Join(curlDir, "curl.log")
	curlScript := `#!/bin/sh
echo "$@" >> ` + logFile + `
COUNT=$(wc -l < ` + logFile + `)
if [ "$COUNT" -le 1 ]; then
  echo '{"token":"fake-install-token"}'
else
  echo '{"id":42}'
fi
`
	require.NoError(t, os.WriteFile(filepath.Join(curlDir, "curl"), []byte(curlScript), 0755))

	pushOut, err := runGitHubCheckPush(t, workDir, map[string]string{
		"param_app_id":          "12345",
		"param_installation_id": "67890",
		"param_private_key":     string(keyData),
		"param_repository":      "example/repo",
		"put_status":            "in_progress",
		"put_head_sha":          "abc123",
		"put_name":              "ci-test",
		"WORKDIR":               workDir,
		"BUILD_PIPELINE_NAME":   "my-pipeline",
		"BUILD_JOB_NAME":        "my-job",
		"PATH":                  curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "github-check push (create) failed: %s", pushOut)

	assert.Contains(t, pushOut, "Created check run 42")

	// Verify curl was called with correct endpoints
	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	log := string(curlLog)
	assert.Contains(t, log, "api.github.com/app/installations/67890/access_tokens")
	assert.Contains(t, log, "api.github.com/repos/example/repo/check-runs")
}

func TestGitHubCheckPush_UpdateCheckRun(t *testing.T) {
	for _, tool := range []string{"openssl", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available, skipping github-check push test", tool)
		}
	}

	workDir := t.TempDir()
	keyFile := filepath.Join(workDir, "test-key.pem")
	cmd := exec.Command("openssl", "genrsa", "-out", keyFile, "2048")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "openssl genrsa failed: %s", out)
	keyData, err := os.ReadFile(keyFile)
	require.NoError(t, err)

	// Pre-create the check run ID file (simulating a prior in_progress call)
	idFile := filepath.Join(workDir, ".github-check-ci-test.id")
	require.NoError(t, os.WriteFile(idFile, []byte("42"), 0644))

	curlDir := t.TempDir()
	logFile := filepath.Join(curlDir, "curl.log")
	curlScript := `#!/bin/sh
echo "$@" >> ` + logFile + `
COUNT=$(wc -l < ` + logFile + `)
if [ "$COUNT" -le 1 ]; then
  echo '{"token":"fake-install-token"}'
fi
`
	require.NoError(t, os.WriteFile(filepath.Join(curlDir, "curl"), []byte(curlScript), 0755))

	pushOut, err := runGitHubCheckPush(t, workDir, map[string]string{
		"param_app_id":          "12345",
		"param_installation_id": "67890",
		"param_private_key":     string(keyData),
		"param_repository":      "example/repo",
		"put_conclusion":        "success",
		"put_name":              "ci-test",
		"put_head_sha":          "abc123",
		"WORKDIR":               workDir,
		"BUILD_PIPELINE_NAME":   "my-pipeline",
		"BUILD_JOB_NAME":        "my-job",
		"PATH":                  curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "github-check push (update) failed: %s", pushOut)

	assert.Contains(t, pushOut, "Updated check run 42 with conclusion=success")

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	log := string(curlLog)
	assert.Contains(t, log, "api.github.com/app/installations/67890/access_tokens")
	assert.Contains(t, log, "api.github.com/repos/example/repo/check-runs/42")
}

func TestGitHubCheckPush_RequiresStatusOrConclusion(t *testing.T) {
	for _, tool := range []string{"openssl", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available, skipping github-check push test", tool)
		}
	}

	workDir := t.TempDir()
	keyFile := filepath.Join(workDir, "test-key.pem")
	cmd := exec.Command("openssl", "genrsa", "-out", keyFile, "2048")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "openssl genrsa failed: %s", out)
	keyData, err := os.ReadFile(keyFile)
	require.NoError(t, err)

	curlDir, _ := fakeCurlDir(t, `{"token":"fake-install-token"}`)

	pushOut, err := runGitHubCheckPush(t, workDir, map[string]string{
		"param_app_id":          "12345",
		"param_installation_id": "67890",
		"param_private_key":     string(keyData),
		"param_repository":      "example/repo",
		"put_head_sha":          "abc123",
		"WORKDIR":               workDir,
		"PATH":                  curlDir + ":" + os.Getenv("PATH"),
	})
	assert.Error(t, err, "should fail without status or conclusion")
	assert.Contains(t, pushOut, "either put_status or put_conclusion must be set")
}

// --- Trigger resource type ---

func TestTrigger_NoScripts(t *testing.T) {
	rts := builtin.ResourceTypes()
	rt := rts["trigger"]

	assert.Nil(t, rt.Check, "trigger should have no check script")
	assert.Nil(t, rt.Pull, "trigger should have no pull script")
	assert.Nil(t, rt.Push, "trigger should have no push script")
	assert.Equal(t, "pikoci://trigger", rt.Source)
}
