# Resource Types

A resource type defines how PikoCI interacts with an external system. It has three operations: **check** (detect new versions), **pull** (fetch a version), and **push** (publish to the resource).

## Defining a resource type

```hcl
resource_type "git" {
  params = ["url", "name"]

  check "exec" {
    path = "/bin/sh"
    args = ["-ec", "git ls-remote $param_url HEAD | awk '{print $1}'"]
  }

  pull "exec" {
    path = "/bin/sh"
    args = ["-ec", "git clone $param_url $param_name && git checkout $version_ref"]
  }

  push "exec" {
    path = "/bin/sh"
    args = ["-ec", "cd $param_name && git push"]
  }
}
```

| Field    | Required | Description                                         |
|----------|----------|-----------------------------------------------------|
| `name`   | yes      | Label on the block                                  |
| `source` | no       | URL to fetch the definition from (mutually exclusive with inline commands) |
| `params` | no       | List of parameter names the resource type accepts   |
| `cache`  | no       | Enable persistent cache for check/pull (see [Caching](#caching)) |
| `check`  | no       | Runner command to detect new versions               |
| `pull`   | no       | Runner command to fetch a specific version           |
| `push`   | no       | Runner command to publish (used by `put` steps)     |

All three operations are optional. A resource type that only defines `push` can be used exclusively with `put` steps (e.g. `github-check`). When `source` is set, inline commands are not needed.

### Operations

**check** must output a JSON array of version objects to stdout. Each object becomes a version that PikoCI tracks. Example output:

```json
[{"ref": "abc123"}, {"ref": "def456"}]
```

> **First check convention:** When no previous version exists, the `check` command should return only the **current/latest** version (a single-element array). PikoCI triggers builds for all versions returned, including the first check. Returning a large list of historical versions (e.g. all git tags) on the first check will create a build for each one. Resource types should be designed to return only what is new.

**pull** fetches a specific version into the working directory. The version fields are available as `$version_<key>` environment variables.

**push** publishes to the resource. Used when a job has a `put` step.

### Environment variables

Inside `check`, `pull`, and `push` commands, PikoCI exposes:

| Variable            | Description                                  |
|---------------------|----------------------------------------------|
| `$param_<name>`      | Resource instance parameter value            |
| `$version_<key>`     | Version field from the last check (pull/push only) |
| `$put_<key>`         | Put step parameter value (push only)         |
| `$WORKDIR`           | Temporary working directory for the job      |
| `$BUILD_NUMBER`      | Sequential build number for the current job  |
| `$BUILD_JOB_NAME`    | Name of the current job                      |
| `$BUILD_PIPELINE_NAME` | Name of the current pipeline              |
| `$BUILD_TEAM_NAME`   | Canonical name of the team                   |
| `$BUILD_STATUS`      | Build status: `succeeded` or `failed` (hooks only) |
| `$CACHE_DIR`         | Persistent cache directory (check/pull only, when [caching](#caching) is enabled) |
| `$path`, `$args`     | Runner `run` block values (for the exec runner) |

## Sourcing from URL

Instead of defining commands inline, you can point to an external HCL file:

```hcl
resource_type "my-git" {
  source = "pikoci://git"
}
```

Two URL formats are supported:

- **`pikoci://<name>`** resolves to the PikoCI registry. For shipped built-ins (`git`, `cron`), the embedded definition is used directly (no network call). For other names, fetches from `https://raw.githubusercontent.com/PikoCI/pikoci/master/pikoci/builtin/resource_types/<name>.hcl`.
- **`https://...`** or **`http://...`** fetches HCL from any URL.

When `source` is set, you must not define inline `check`, `pull`, or `push` blocks. PikoCI will error if both are present.

> **Note:** The source is resolved once when the pipeline is created or updated. If the remote HCL file changes, you must re-set the pipeline to pick up the new definition.

## Runner overrides

You can override the runner used for all commands of a resource type by adding a `runner` block. This lets you run sourced or inline exec commands inside Docker without modifying the type's command definitions. See [Runners — Type-level runner overrides](Runners.md#type-level-runner-overrides) for details.

## Overriding built-ins

All built-in resource types (`cron`, `git`) can be overridden by defining a `resource_type` block with the same name in your pipeline. Inline definitions always take precedence over built-ins.

This is useful when the built-in behavior doesn't match your needs. For example, the built-in `git` resource type uses `git ls-remote` or the GitHub/GitLab API to check for new commits. If you need a simpler check (no API, no token support) or want to add custom logic, define your own:

```hcl
resource_type "git" {
  params = ["url", "name"]

  check "exec" {
    path = "/bin/sh"
    args = ["-ec", "git ls-remote $param_url HEAD | awk '{print $1}' | jq -Rsc '[{\"ref\": .}]'"]
  }

  pull "exec" {
    path = "/bin/sh"
    args = ["-ec", "git clone $param_url $param_name && cd $param_name && git checkout $version_ref"]
  }

  push "exec" { }
}
```

This replaces the built-in `git` entirely for this pipeline. Resources using `resource "git" "..."` will use your definition instead.

## Defining a resource

A resource is an instance of a resource type with concrete parameter values:

```hcl
resource "git" "my_repo" {
  params {
    url  = "https://github.com/PikoCI/pikoci.git"
    name = "pikoci"
  }
}
```

| Field            | Required | Description                                           |
|------------------|----------|-------------------------------------------------------|
| `type`           | yes      | Must match a `resource_type` name                     |
| `name`           | yes      | Unique name for this resource instance                |
| `params`         | yes      | Key/value pairs matching the resource type's `params` |
| `check_interval` | no       | Schedule for automatic checks (cron syntax or `@every <duration>`) |
| `cache`          | no       | Override the resource type's cache setting (`true`/`false`) |

### Webhook triggers

Each resource gets a webhook token that can be used to trigger a check externally:

```
POST /webhooks/<webhook_token>
```

You can regenerate a webhook token via the API:

```
POST /teams/{team}/pipelines/{pipeline}/resources/{resource}/webhook_token
```

## Caching

Resource types can enable persistent caching so that check and pull scripts can reuse state across runs instead of starting from scratch every time. This is especially useful for large Git repositories where a full clone on every pull is slow.

### Enabling cache

Set `cache = true` on a `resource_type` to enable caching for all resources of that type:

```hcl
resource_type "git" {
  source = "pikoci://git"
  cache  = true
}
```

Individual resources can override the type default:

```hcl
resource "git" "small-repo" {
  cache = false   # opt out even though type defaults to true
  params {
    url = "https://github.com/small/repo"
  }
}
```

### How it works

When caching is enabled, PikoCI creates a persistent directory for each resource instance and passes it as the `$CACHE_DIR` environment variable to **check** and **pull** scripts (not push). The directory is namespaced per team, pipeline, and resource:

```
~/.cache/pikoci/cache/{team}/{pipeline}/{resource_canonical}/
```

Scripts are responsible for managing the cache contents. PikoCI only creates the directory and passes the path — it does not manage the data inside.

### Writing cache-aware scripts

A cache-aware check script should populate the cache as a side effect:

```sh
if [ -n "$CACHE_DIR" ]; then
  if [ -d "$CACHE_DIR/repo" ]; then
    git -C "$CACHE_DIR/repo" fetch --prune
  else
    git clone --bare "$URL" "$CACHE_DIR/repo"
  fi
fi
```

A cache-aware pull script can use `--reference-if-able` to avoid re-downloading objects:

```sh
if [ -n "$CACHE_DIR" ] && [ -d "$CACHE_DIR/repo" ]; then
  git clone --reference-if-able "$CACHE_DIR/repo" "$URL" "$name"
else
  git clone "$URL" "$name"
fi
```

The built-in `git` resource type has `cache = true` by default and uses this pattern automatically.

### Notes

- Each worker has its own cache — there is no shared state between workers.
- Cache directories persist indefinitely. No automatic cleanup is performed in the current version.
- If `$CACHE_DIR` is not set (caching disabled), scripts should fall back to their normal behavior.

## Built-in: cron

The `cron` resource type is built in. You do not need to define it, just use it directly as a resource:

```hcl
resource "cron" "every_minute" {
  check_interval = "@every 1m"
}
```

The cron check command outputs the current date as a version:

```json
[{"date": "Mon Jan 2 15:04:05 UTC 2006"}]
```

### Supported schedules

The `check_interval` field accepts:

- `@every <duration>`, e.g. `@every 10s`, `@every 5m`, `@every 1h`
- Standard cron expressions, e.g. `0 */5 * * *`

The minimum `check_interval` is 10 seconds. Intervals shorter than 10s will be rejected on pipeline create/update.

Manual triggers (via the UI or API) and webhook triggers reset the check timer, so the next automatic check happens one full interval after the trigger.

## Built-in: git

The `git` resource type is built in with API-aware check support for GitHub and GitLab. You do not need to define it, just use it directly:

```hcl
resource "git" "my-repo" {
  params {
    url  = "https://github.com/PikoCI/pikoci.git"
    name = "pikoci"
  }
}
```

### Params

| Param    | Required | Description                                      |
|----------|----------|--------------------------------------------------|
| `url`    | yes      | Repository URL (HTTPS)                           |
| `name`   | yes      | Directory name to clone into                     |
| `branch` | no       | Branch to track (defaults to HEAD)               |
| `token`  | no       | API/HTTPS auth token for private repos           |
| `pr`     | no       | Set to `"true"` to check for open pull requests instead of commits (requires `token`, GitHub/GitLab only) |
| `tag`    | no       | Set to `true` to check for tags instead of commits (requires `token`, GitHub/GitLab only) |

### Token setup

**GitHub**: Create a personal access token at **Settings > Developer settings > Personal access tokens > Fine-grained tokens**. The token needs **Contents** (read) permission for commit checks and cloning. For PR mode, it also needs **Pull requests** (read). For private repos, the token must have access to the repository.

**GitLab**: Create a project or personal access token at **Settings > Access Tokens**. The token needs the `read_repository` scope for commit checks and cloning. For PR mode (merge requests), it also needs `read_api`.

Pass the token via a pipeline variable to avoid hardcoding it:

```hcl
variable "git_token" {
  type = string
}

resource "git" "my-repo" {
  params {
    url   = "https://github.com/myorg/my-repo.git"
    name  = "my-repo"
    token = var.git_token
  }
}
```

Then provide the value in your vars file: `{"git_token": "ghp_..."}`.

### Check behavior

When `token` is provided and the URL matches a supported provider, the check uses the provider's API for efficiency:

- **GitHub** (`github.com`): Uses `GET /repos/{owner}/{repo}/commits?sha={branch}` with `Authorization: token` header
- **GitLab** (`gitlab.com`): Uses `GET /api/v4/projects/{id}/repository/commits?ref_name={branch}` with `PRIVATE-TOKEN` header
- **Other providers**: Falls back to `git ls-remote`

Without a token, all providers use `git ls-remote`.

### PR mode

When `pr = "true"` is set, the check command lists open pull requests (or merge requests on GitLab) instead of checking for commits. Each open PR becomes a version with its head SHA and PR number:

```json
[{"ref": "abc123", "pr": "42"}, {"ref": "def456", "pr": "43"}]
```

When a new PR is opened or an existing PR is updated (new commits pushed), PikoCI detects the change and triggers the job. The pull step fetches the PR's head ref so your CI runs against the PR code.

This requires a `token` and is supported on GitHub and GitLab.

### Tag mode

When `tag = "true"` is set, the check command lists tags instead of checking for commits. Each tag becomes a version with its commit SHA and tag name:

```json
[{"ref": "abc123", "tag": "v1.0.0"}, {"ref": "def456", "tag": "v0.9.0"}]
```

When a new tag is pushed, PikoCI detects it and triggers the job. The pull step clones the repository at the specific tag. Since version variables (`$version_tag`) are only available in pull steps, use `git describe --tags --exact-match` in task steps to retrieve the tag name.

This requires a `token` and is supported on GitHub and GitLab.

### Pull behavior

Clones the repository with `git clone`, injecting the token into the HTTPS URL when provided. In PR mode, fetches the PR head ref. Otherwise, checks out the specific version ref.

### Push behavior

Pushes from the cloned directory, injecting the token into the remote URL when provided.

### Examples

Public repository:

```hcl
resource "git" "my-repo" {
  params {
    url  = "https://github.com/PikoCI/pikoci.git"
    name = "pikoci"
  }
}
```

Private repository with token:

```hcl
resource "git" "private-repo" {
  params {
    url    = "https://github.com/myorg/private-repo.git"
    name   = "private-repo"
    branch = "main"
    token  = var.github_token
  }
}
```

CI on pull requests:

```hcl
resource "git" "prs" {
  params {
    url   = "https://github.com/myorg/my-repo.git"
    name  = "my-repo"
    token = var.github_token
    pr    = "true"
  }
}

job "ci" {
  get "git" "prs" {
    trigger = true
  }

  task "test" {
    run "docker" {
      image = "golang:1.25"
      cmd   = "cd my-repo && make test"
    }
  }
}
```

Build Docker image on new tags:

```hcl
resource "git" "tags" {
  params {
    url   = "https://github.com/myorg/my-repo.git"
    name  = "my-repo"
    token = var.github_token
    tag   = true
  }
}

job "release" {
  get "git" "tags" {
    trigger = true
  }

  task "build" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "cd my-repo && TAG=$(git describe --tags --exact-match) && docker build -t myorg/my-repo:$TAG ."]
    }
  }
}
```

## Built-in: trigger

The `trigger` resource type enables cross-pipeline and cross-job triggering without webhook tokens or external auth. It uses the internal database as a team-scoped event bus.

You do not need to define the resource type — it is built in. Just declare a resource:

```hcl
resource "trigger" "deploy" {}
```

Trigger resources are scoped by team and matched by their canonical name (`trigger.deploy`). Any pipeline in the same team that declares a resource with the same type and name shares the same trigger stream.

### How it works

- **put** — inserts a trigger event into the database with the put step's params as the version payload
- **check** — queries the database for trigger events newer than the last seen, creates resource versions, and triggers downstream jobs
- **get** — no special handling; the standard resource version flow passes version fields as `$version_<key>` env vars

No container or script is executed for check/put — the worker handles these operations internally.

### Data passing

Put step params become the trigger's version payload. When a downstream job's get step runs, the standard resource version mechanism passes them as `version_<key>` env vars:

| Pipeline A put step | Pipeline B get step env vars |
|---|---|
| `build_sha = "abc123"` | `$version_build_sha=abc123` |
| `status = "success"` | `$version_status=success` |
| *(auto)* `trigger_pipeline` | `$version_trigger_pipeline=pipeline-a` |
| *(auto)* `trigger_job` | `$version_trigger_job=build` |
| *(auto)* `trigger_build` | `$version_trigger_build=42` |

The `trigger_pipeline`, `trigger_job`, and `trigger_build` fields are added automatically by the worker.

### Example: cross-pipeline triggering

**Pipeline A** — fires the trigger after a successful build:

```hcl
resource "trigger" "deploy" {}

job "build" {
  get "git" "repo" { trigger = true }

  task "compile" {
    run "exec" {
      path = "make"
      args = ["build"]
    }
  }

  on_success {
    put "trigger" "deploy" {
      build_sha = "abc123"
      status    = "success"
    }
  }
}
```

**Pipeline B** — listens for the trigger and deploys:

```hcl
resource "trigger" "deploy" {}

job "deploy" {
  get "trigger" "deploy" { trigger = true }

  task "run-deploy" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo deploying $version_build_sha"]
    }
  }
}
```

### Visibility

Trigger events appear as resource versions in the existing resource versions UI. Each pipeline's `trigger.deploy` resource shows the trigger events just like any other resource — no dedicated trigger UI is needed.
