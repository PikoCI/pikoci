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

// --- Trigger resource type ---

func TestTrigger_NoScripts(t *testing.T) {
	rts := builtin.ResourceTypes()
	rt := rts["trigger"]

	assert.Nil(t, rt.Check, "trigger should have no check script")
	assert.Nil(t, rt.Pull, "trigger should have no pull script")
	assert.Nil(t, rt.Push, "trigger should have no push script")
	assert.Equal(t, "pikoci://trigger", rt.Source)
}
