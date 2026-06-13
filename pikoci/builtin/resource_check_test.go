package builtin_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/builtin"
	"github.com/pikoci/pikoci/pikoci/utils"
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

// fakeCurlDirFailThenSucceed creates a fake curl that fails (exit 22, like
// curl -f on 404) on the first invocation and returns the canned response on
// the second. This simulates the fallback chain where the GitLab API probe
// fails and the Gitea API probe succeeds.
func fakeCurlDirFailThenSucceed(t *testing.T, response string) (dir string, logFile string) {
	t.Helper()
	dir = t.TempDir()
	logFile = filepath.Join(dir, "curl.log")
	counterFile := filepath.Join(dir, "curl.count")

	script := `#!/bin/sh
echo "$@" >> ` + logFile + `
if [ ! -f ` + counterFile + ` ]; then
  echo 1 > ` + counterFile + `
  exit 22
fi
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
	// v3 and v4 are newer than the known tag v2; v1 is older.
	cannedTags := `[{"name":"v4","commit":{"sha":"ddd"}},{"name":"v3","commit":{"sha":"aaa"}},{"name":"v2","commit":{"sha":"bbb"}},{"name":"v1","commit":{"sha":"ccc"}}]`
	curlDir, logFile := fakeCurlDir(t, cannedTags)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://github.com/example/repo",
		"param_token": "fake-token",
		"param_tag":   "true",
		"version_tag": "v2",
		"version_ref": "bbb",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitHub tag, subsequent) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "per_page=100",
		"subsequent check should request per_page=100")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 2, "should only return tags newer than known tag v2")
	assert.Equal(t, "v4", versions[0]["tag"])
	assert.Equal(t, "v3", versions[1]["tag"])
}

func TestGitCheck_TagMode_GitHub_SubsequentCheck_NoNewTags(t *testing.T) {
	// Known tag is the latest — no new tags to return.
	cannedTags := `[{"name":"v2","commit":{"sha":"bbb"}},{"name":"v1","commit":{"sha":"ccc"}}]`
	curlDir, _ := fakeCurlDir(t, cannedTags)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://github.com/example/repo",
		"param_token": "fake-token",
		"param_tag":   "true",
		"version_tag": "v2",
		"version_ref": "bbb",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitHub tag, no new) failed: %s", out)

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 0, "should return empty list when no new tags exist")
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
	// v4 and v3 are newer than the known tag v2; v1 is older.
	cannedTags := `[{"name":"v4","commit":{"id":"ddd"}},{"name":"v3","commit":{"id":"aaa"}},{"name":"v2","commit":{"id":"bbb"}},{"name":"v1","commit":{"id":"ccc"}}]`
	curlDir, logFile := fakeCurlDir(t, cannedTags)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://gitlab.com/example/repo",
		"param_token": "fake-token",
		"param_tag":   "true",
		"version_tag": "v2",
		"version_ref": "bbb",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitLab tag, subsequent) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "per_page=100")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 2, "should only return tags newer than known tag v2")
	assert.Equal(t, "v4", versions[0]["tag"])
	assert.Equal(t, "v3", versions[1]["tag"])
}

func TestGitCheck_TagMode_GitLab_SubsequentCheck_NoNewTags(t *testing.T) {
	cannedTags := `[{"name":"v2","commit":{"id":"bbb"}},{"name":"v1","commit":{"id":"ccc"}}]`
	curlDir, _ := fakeCurlDir(t, cannedTags)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://gitlab.com/example/repo",
		"param_token": "fake-token",
		"param_tag":   "true",
		"version_tag": "v2",
		"version_ref": "bbb",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (GitLab tag, no new) failed: %s", out)

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 0, "should return empty list when no new tags exist")
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

// --- Git resource type: Gitea/Forgejo check ---

func TestGitCheck_TagMode_Gitea_FirstCheck(t *testing.T) {
	cannedTags := `[{"name":"v3","commit":{"sha":"aaa"}},{"name":"v2","commit":{"sha":"bbb"}}]`
	curlDir, logFile := fakeCurlDir(t, cannedTags)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":      "https://forgejo.example.com/myorg/myrepo",
		"param_token":    "fake-token",
		"param_tag":      "true",
		"param_provider": "gitea",
		"PATH":           curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (Gitea tag, first) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "forgejo.example.com/api/v1/repos/myorg/myrepo/tags")
	assert.Contains(t, string(curlLog), "limit=1")
}

func TestGitCheck_TagMode_Gitea_SubsequentCheck(t *testing.T) {
	cannedTags := `[{"name":"v4","commit":{"sha":"ddd"}},{"name":"v3","commit":{"sha":"aaa"}},{"name":"v2","commit":{"sha":"bbb"}},{"name":"v1","commit":{"sha":"ccc"}}]`
	curlDir, logFile := fakeCurlDir(t, cannedTags)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":      "https://forgejo.example.com/myorg/myrepo",
		"param_token":    "fake-token",
		"param_tag":      "true",
		"param_provider": "gitea",
		"version_tag":    "v2",
		"version_ref":    "bbb",
		"PATH":           curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (Gitea tag, subsequent) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "limit=100")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 2, "should only return tags newer than known tag v2")
	assert.Equal(t, "v4", versions[0]["tag"])
	assert.Equal(t, "v3", versions[1]["tag"])
}

func TestGitCheck_TagMode_Gitea_NoNewTags(t *testing.T) {
	cannedTags := `[{"name":"v2","commit":{"sha":"bbb"}},{"name":"v1","commit":{"sha":"ccc"}}]`
	curlDir, _ := fakeCurlDir(t, cannedTags)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":      "https://forgejo.example.com/myorg/myrepo",
		"param_token":    "fake-token",
		"param_tag":      "true",
		"param_provider": "gitea",
		"version_tag":    "v2",
		"version_ref":    "bbb",
		"PATH":           curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (Gitea tag, no new) failed: %s", out)

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 0, "should return empty list when no new tags exist")
}

func TestGitCheck_PRMode_Gitea_FirstCheck(t *testing.T) {
	cannedPRs := `[{"number":10,"head":{"sha":"aaa"}},{"number":9,"head":{"sha":"bbb"}}]`
	curlDir, logFile := fakeCurlDir(t, cannedPRs)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":      "https://forgejo.example.com/myorg/myrepo",
		"param_token":    "fake-token",
		"param_pr":       "true",
		"param_provider": "gitea",
		"PATH":           curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (Gitea PR, first) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "forgejo.example.com/api/v1/repos/myorg/myrepo/pulls")
	assert.Contains(t, string(curlLog), "limit=1")
}

func TestGitCheck_PRMode_Gitea_SubsequentCheck(t *testing.T) {
	cannedPRs := `[{"number":10,"head":{"sha":"aaa"}},{"number":9,"head":{"sha":"bbb"}}]`
	curlDir, logFile := fakeCurlDir(t, cannedPRs)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":      "https://forgejo.example.com/myorg/myrepo",
		"param_token":    "fake-token",
		"param_pr":       "true",
		"param_provider": "gitea",
		"version_pr":     "8",
		"version_ref":    "old-sha",
		"PATH":           curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (Gitea PR, subsequent) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "limit=100")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 2)
}

func TestGitCheck_GiteaAPI_ReturnsSingleVersion(t *testing.T) {
	cannedCommits := `[{"sha":"ggg789"}]`
	curlDir, logFile := fakeCurlDir(t, cannedCommits)

	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":      "https://forgejo.example.com/myorg/myrepo",
		"param_token":    "fake-token",
		"param_provider": "gitea",
		"PATH":           curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (Gitea API) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "forgejo.example.com/api/v1/repos/myorg/myrepo/commits")
	assert.Contains(t, string(curlLog), "limit=1")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1)
	assert.Equal(t, "ggg789", versions[0]["ref"])
}

func TestGitCheck_ProviderParam_Gitea(t *testing.T) {
	cannedCommits := `[{"sha":"ggg789"}]`
	curlDir, logFile := fakeCurlDir(t, cannedCommits)

	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":      "https://git.mycompany.com/team/project",
		"param_token":    "fake-token",
		"param_provider": "gitea",
		"PATH":           curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (provider=gitea) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "git.mycompany.com/api/v1/repos/team/project/commits")
}

func TestGitCheck_ProviderParam_Forgejo_Alias(t *testing.T) {
	cannedCommits := `[{"sha":"ggg789"}]`
	curlDir, logFile := fakeCurlDir(t, cannedCommits)

	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":      "https://forgejo.example.com/myorg/myrepo",
		"param_token":    "fake-token",
		"param_provider": "forgejo",
		"PATH":           curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (provider=forgejo) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "forgejo.example.com/api/v1/repos/myorg/myrepo/commits",
		"forgejo should be treated as gitea")
}

func TestGitCheck_ProviderParam_GitLab_SelfHosted(t *testing.T) {
	cannedCommits := `[{"id":"def456"}]`
	curlDir, logFile := fakeCurlDir(t, cannedCommits)

	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":      "https://gitlab.mycompany.com/team/project",
		"param_token":    "fake-token",
		"param_provider": "gitlab",
		"PATH":           curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (provider=gitlab self-hosted) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(curlLog), "gitlab.mycompany.com/api/v4/projects/team%2Fproject/repository/commits")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1)
	assert.Equal(t, "def456", versions[0]["ref"])
}

func TestGitCheck_TagMode_Fallback_GiteaAfterGitLabFails(t *testing.T) {
	// Single tag — simulates what the API returns with limit=1 on first check
	cannedTags := `[{"name":"v2","commit":{"sha":"aaa"}}]`
	curlDir, logFile := fakeCurlDirFailThenSucceed(t, cannedTags)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://gitea.example.com/myorg/myrepo",
		"param_token": "fake-token",
		"param_tag":   "true",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (fallback tag) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	logStr := string(curlLog)
	assert.Contains(t, logStr, "api/v4/projects",
		"should try GitLab API first")
	assert.Contains(t, logStr, "api/v1/repos",
		"should fall back to Gitea API")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1)
	assert.Equal(t, "v2", versions[0]["tag"])
}

func TestGitCheck_PRMode_Fallback_GiteaAfterGitLabFails(t *testing.T) {
	cannedPRs := `[{"number":5,"head":{"sha":"fff"}}]`
	curlDir, logFile := fakeCurlDirFailThenSucceed(t, cannedPRs)
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":   "https://gitea.example.com/myorg/myrepo",
		"param_token": "fake-token",
		"param_pr":    "true",
		"PATH":        curlDir + ":" + os.Getenv("PATH"),
	})
	require.NoError(t, err, "git check (fallback PR) failed: %s", out)

	curlLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	logStr := string(curlLog)
	assert.Contains(t, logStr, "api/v4/projects",
		"should try GitLab API first")
	assert.Contains(t, logStr, "api/v1/repos",
		"should fall back to Gitea API")

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1)
	assert.Equal(t, "5", versions[0]["pr"])
}

func TestGitPull_PRMode_Gitea_PullRefFormat(t *testing.T) {
	bareDir, _, runWork := initBareRepo(t)
	_ = runWork

	rts := builtin.ResourceTypes()
	rt := rts["git"]

	dir := t.TempDir()
	logFile := filepath.Join(dir, "git.log")
	script := `#!/bin/sh
if [ "$1" = "fetch" ]; then
  echo "$@" >> ` + logFile + `
  exit 0
fi
exec /usr/bin/git "$@"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0755))

	pullDir := t.TempDir()
	_, _ = runScript(t, rt.Pull, pullDir, map[string]string{
		"param_url":      bareDir,
		"param_name":     "myrepo",
		"param_pr":       "true",
		"param_provider": "gitea",
		"version_pr":     "7",
		"version_ref":    "abc123",
		"PATH":           dir + ":" + os.Getenv("PATH"),
	})

	gitLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(gitLog), "pull/7/head",
		"Gitea PR mode should use pull/N/head ref")
}

func TestGitCheck_ProviderParam_Invalid(t *testing.T) {
	rts := builtin.ResourceTypes()
	rt := rts["git"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_url":      "https://bitbucket.org/example/repo",
		"param_token":    "fake-token",
		"param_provider": "bitbucket",
	})
	assert.Error(t, err, "invalid provider should fail")
	assert.Contains(t, out, "invalid provider")
}

func TestGitPull_PRMode_GitLab_MergeRequestRef(t *testing.T) {
	bareDir, _, runWork := initBareRepo(t)
	_ = runWork

	rts := builtin.ResourceTypes()
	rt := rts["git"]

	// We can't fully test the fetch (no real MR ref in the bare repo),
	// but we can verify the script constructs the right ref by checking
	// that it attempts the merge-requests/N/head pattern.
	// Use a fake git wrapper to capture the fetch command.
	dir := t.TempDir()
	logFile := filepath.Join(dir, "git.log")
	// Create a fake git that logs fetch commands but delegates everything else
	script := `#!/bin/sh
if [ "$1" = "fetch" ]; then
  echo "$@" >> ` + logFile + `
  exit 0
fi
exec /usr/bin/git "$@"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0755))

	pullDir := t.TempDir()
	_, _ = runScript(t, rt.Pull, pullDir, map[string]string{
		"param_url":      bareDir,
		"param_name":     "myrepo",
		"param_pr":       "true",
		"param_provider": "gitlab",
		"version_pr":     "42",
		"version_ref":    "abc123",
		"PATH":           dir + ":" + os.Getenv("PATH"),
	})

	gitLog, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(gitLog), "merge-requests/42/head",
		"GitLab PR mode should use merge-requests/N/head ref")
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

// --- Fs resource type: check ---

func TestFsCheck_File_EmitsVersionOnChange(t *testing.T) {
	rts := builtin.ResourceTypes()
	rt := rts["fs"]

	// Create a temp file
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	require.NoError(t, os.WriteFile(tmpFile, []byte("hello"), 0644))

	// First check: no previous version → should emit a version
	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_path": tmpFile,
	})
	require.NoError(t, err, "fs check (file, first) failed: %s", out)

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1)
	assert.Equal(t, tmpFile, versions[0]["path"])
	assert.NotEmpty(t, versions[0]["hash"])
	assert.NotEmpty(t, versions[0]["modified"])
	assert.NotEmpty(t, versions[0]["size"])

	oldHash := versions[0]["hash"].(string)

	// Same hash → no new version
	out, err = runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_path":   tmpFile,
		"version_hash": oldHash,
	})
	require.NoError(t, err, "fs check (file, same hash) failed: %s", out)
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 0)

	// Modify file → new version
	require.NoError(t, os.WriteFile(tmpFile, []byte("world"), 0644))
	out, err = runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_path":   tmpFile,
		"version_hash": oldHash,
	})
	require.NoError(t, err, "fs check (file, changed) failed: %s", out)
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1)
	assert.NotEqual(t, oldHash, versions[0]["hash"])
}

func TestFsCheck_Directory_EmitsVersionOnChange(t *testing.T) {
	rts := builtin.ResourceTypes()
	rt := rts["fs"]

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("aaa"), 0644))

	// First check
	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_path": tmpDir,
	})
	require.NoError(t, err, "fs check (dir, first) failed: %s", out)

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1)
	assert.Equal(t, tmpDir, versions[0]["path"])
	assert.NotEmpty(t, versions[0]["hash"])
	_, hasModified := versions[0]["modified"]
	assert.False(t, hasModified, "directory version should not have modified field")

	oldHash := versions[0]["hash"].(string)

	// Same content → no new version
	out, err = runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_path":   tmpDir,
		"version_hash": oldHash,
	})
	require.NoError(t, err, "fs check (dir, same) failed: %s", out)
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 0)

	// Add a file → new version
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("bbb"), 0644))
	out, err = runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_path":   tmpDir,
		"version_hash": oldHash,
	})
	require.NoError(t, err, "fs check (dir, changed) failed: %s", out)
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 1)
	assert.NotEqual(t, oldHash, versions[0]["hash"])
}

func TestFsCheck_PathNotFound(t *testing.T) {
	rts := builtin.ResourceTypes()
	rt := rts["fs"]

	out, err := runScript(t, rt.Check, t.TempDir(), map[string]string{
		"param_path": "/nonexistent/path/that/does/not/exist",
	})
	require.NoError(t, err, "fs check (not found) failed: %s", out)

	var versions []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &versions))
	assert.Len(t, versions, 0)
}

// --- Fs resource type: pull ---

func TestFsPull_File_CopiesToWorkdir(t *testing.T) {
	rts := builtin.ResourceTypes()
	rt := rts["fs"]

	tmpFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(tmpFile, []byte("key: value"), 0644))

	workDir := t.TempDir()
	out, err := runScript(t, rt.Pull, workDir, map[string]string{
		"param_path": tmpFile,
		"WORKDIR":    workDir,
	})
	require.NoError(t, err, "fs pull (file) failed: %s", out)

	data, err := os.ReadFile(filepath.Join(workDir, "config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "key: value", string(data))
}

func TestFsPull_Directory_CopiesToWorkdir(t *testing.T) {
	rts := builtin.ResourceTypes()
	rt := rts["fs"]

	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("aaa"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "sub"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("bbb"), 0644))

	workDir := t.TempDir()
	out, err := runScript(t, rt.Pull, workDir, map[string]string{
		"param_path": srcDir,
		"WORKDIR":    workDir,
	})
	require.NoError(t, err, "fs pull (dir) failed: %s", out)

	data, err := os.ReadFile(filepath.Join(workDir, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "aaa", string(data))

	data, err = os.ReadFile(filepath.Join(workDir, "sub", "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, "bbb", string(data))
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
