# Pipeline Reference

Pipelines are defined in [HCL](https://github.com/hashicorp/hcl). A pipeline file contains `variable`, `resource_type`, `resource`, `notification_type`, `notification`, `runner`, `secret_type`, `secret`, `service`, and `job` blocks.

## variable

Declares a pipeline variable. Variables can be referenced as `var.<name>` or `${var.<name>}` anywhere in the pipeline.

```hcl
variable "repo_url" {
  type    = string
  default = "https://github.com/PikoCI/pikoci.git"
}

variable "repo_name" {
  type = string
}
```

| Field     | Required | Description                        |
|-----------|----------|------------------------------------|
| `name`    | yes      | Label on the block                 |
| `type`    | yes      | `string`                           |
| `default` | no       | Default value if not set via vars file |

Variables without a default must be provided via a JSON vars file (`--vars` / `-v`).

## resource_type

Defines how to check, pull, and push a resource. See [Resource Types](Resource-Types.md).

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
| `source` | no       | URL to fetch definition (e.g. `pikoci://git`)       |
| `params` | no       | List of parameter names                             |
| `runner` | no       | Override runner for all commands (see [Runners](Runners.md#type-level-runner-overrides)) |
| `cache`  | no       | Enable persistent cache for check/pull (see [Resource Types](Resource-Types.md#caching)) |
| `check`  | no       | Runner command block for version checking |
| `pull`   | no       | Runner command block for fetching a version |
| `push`   | no       | Runner command block for pushing a version |

When `source` is set, inline commands are not needed. The source is resolved once when the pipeline is created or updated — if the remote definition changes, you must re-set the pipeline to pick up the new version. This applies to all block types that support `source` (`resource_type`, `runner_type`, `secret_type`, `service_type`).

## resource

An instance of a resource type. See [Resource Types](Resource-Types.md).

```hcl
resource "git" "my_repo" {
  params {
    url  = var.repo_url
    name = "my-repo"
  }
}

resource "cron" "every_10s" {
  check_interval = "@every 10s"
}
```

| Field            | Required | Description                                      |
|------------------|----------|--------------------------------------------------|
| `type`           | yes      | Label, must match a `resource_type` name         |
| `name`           | yes      | Label, unique name for this resource              |
| `params`         | no       | Block with key/value pairs passed to the resource type |
| `check_interval` | no       | Cron expression or `@every <duration>` for automatic checks |
| `tags`           | no       | List of tags to route resource checks to matching workers (see [Workers](Workers.md#tags)) |
| `cache`          | no       | Override the resource type's cache setting (see [Resource Types](Resource-Types.md#caching)) |

## notification_type

Defines how to send a fire-and-forget notification. See [Notifications](Notifications.md).

```hcl
notification_type "slack" {
  source = "pikoci://slack"
}

notification_type "custom" {
  params = ["webhook_url"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "curl -sf -X POST \"$param_webhook_url\" -d '{\"text\": \"'\"$NOTIFY_MESSAGE\"'\"}'"]
  }
}
```

| Field    | Required | Description                                                      |
|----------|----------|------------------------------------------------------------------|
| `name`   | yes      | Label on the block                                               |
| `source` | no       | URL to fetch the definition from (mutually exclusive with inline `notify`) |
| `params` | no       | List of parameter names the notification type accepts            |
| `notify` | no       | Runner command block that executes the notification logic        |
| `runner` | no       | Override runner for all commands (see [Runners](Runners.md#type-level-runner-overrides)) |

## notification

An instance of a notification type. See [Notifications](Notifications.md).

```hcl
notification "slack" "deploys" {
  params {
    webhook_url = var.slack_webhook
  }
  on      = ["success", "failure"]
  message = "Build finished"
}
```

| Field     | Required | Description                                                                |
|-----------|----------|----------------------------------------------------------------------------|
| `type`    | yes      | Label, must match a `notification_type` name                              |
| `name`    | yes      | Label, unique name for this notification                                  |
| `params`  | no       | Block with key/value pairs passed to the notification type                |
| `message` | no       | Default message text (available as `$NOTIFY_MESSAGE`)                     |
| `on`      | no       | Events for automatic dispatch: `success`, `failure`, `cancel`, `all`. If omitted, manual-only. |
| `jobs`    | no       | Limit automatic dispatch to these job names (requires `on`)               |
| `exclude` | no       | Exclude these job names from automatic dispatch (requires `on`)           |

## runner_type

Defines a reusable execution environment. See [Runners](Runners.md).

```hcl
runner_type "docker" {
  run {
    path = "docker"
    args = [
      "run", "--rm",
      "-v", "$WORKDIR:/workdir",
      "-w", "/workdir",
      "$image",
      "$cmd",
    ]
  }
}
```

| Field    | Required | Description                                    |
|----------|----------|------------------------------------------------|
| `name`   | yes      | Label on the block                             |
| `source` | no       | URL to fetch definition (e.g. `pikoci://docker`) |
| `run`    | yes*     | Runner command block defining how tasks are executed |

\* Not required when `source` is set.

When `source` is set, inline `run` block is not needed.

## secret_type

Defines how to fetch secrets. See [Secret Types](Secret-Types.md). The `get` command should print a JSON object on its last stdout line with key-value pairs that become `secret_<key>` environment variables. Connection config (address, token, etc.) is set as attributes on the block.

```hcl
secret_type "vault" {
  source  = "pikoci://vault"
  address = var.vault_address
  token   = var.vault_token
}
```

| Field    | Required | Description                                         |
|----------|----------|-----------------------------------------------------|
| `name`   | yes      | Label on the block                                  |
| `source` | no       | URL to fetch definition (e.g. `pikoci://vault`)     |
| `params` | no       | List of parameter names the get command accepts      |
| `runner` | no       | Override runner for all commands (see [Runners](Runners.md#type-level-runner-overrides)) |
| other    | no       | Config attributes passed as `param_<key>` env vars to the get command |
| `get`    | no       | Runner command block that fetches secrets |

When `source` is set, inline `get` block is not needed. Use secret-backed variables to consume secrets:

```hcl
variable "db_password" {
  type = string
  secret "vault" {
    path = "secret/data/db"
    key  = "password"
  }
}

task "migrate" {
  run "exec" {
    path = "/bin/sh"
    args = ["-ec", "DATABASE_PASSWORD=${var.db_password} make migrate"]
  }
}
```

See [Variables](Variables.md) for full secret-backed variable documentation.

## service_type

Defines an ephemeral process that runs alongside a job's tasks. See [Services](Services.md).

```hcl
service_type "postgres" {
  params = ["version"]

  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "docker run -d --name pg-$BUILD_NUMBER postgres:$param_version"]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "docker exec pg-$BUILD_NUMBER pg_isready"]
    interval = "2s"
    timeout  = "30s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "docker rm -f pg-$BUILD_NUMBER"]
  }
}
```

| Field         | Required | Description                                                      |
|---------------|----------|------------------------------------------------------------------|
| `name`        | yes      | Label on the block                                               |
| `source`      | no       | URL to fetch definition (mutually exclusive with inline commands) |
| `params`      | no       | List of parameter names for per-job customization                |
| `start`       | yes*     | Runner command to start the service                              |
| `ready_check` | no       | Runner command polled until exit 0 or timeout (accepts `interval` default `"1s"` and `timeout` default `"60s"`) |
| `stop`        | yes*     | Runner command to stop the service (always runs)                 |
| `runner`      | no       | Override runner for all commands (see [Runners](Runners.md#type-level-runner-overrides)) |

\* Not required when `source` is set.

The `ready_check` block accepts `interval` (default `"1s"`) and `timeout` (default `"60s"`) fields.

## job

Jobs contain a plan of steps executed in order. Each step is one of `get`, `task`, `put`, `notify`, or `service`.

The optional `concurrency` attribute limits how many builds of the job can run simultaneously. When the limit is reached, new builds are re-queued and wait until a slot frees up. The default value `0` means unlimited.

The optional `timeout` attribute limits the total wall-clock time for a build's plan steps. When the timeout is reached, the build fails with a "job timed out" error and `on_cancel`/`ensure` hooks still run. If not set, the job runs with no time limit.

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Label on the block |
| `concurrency` | no | Max concurrent builds (default `0` = unlimited) |
| `timeout` | no | Max wall-clock time for the build (Go duration string) |
| `tags` | no | Route to workers with matching tags |
| `serial_groups` | no | Cross-job mutual exclusion groups |
| `for_each` | no | Generate job instances from a set or map |
| `matrix` | no | Generate job instances from cartesian product (mutually exclusive with `for_each`) |
| `get` | no | Step block — fetches a resource version |
| `task` | no | Step block — runs a command via a runner |
| `put` | no | Step block — pushes to a resource |
| `notify` | no | Step block — sends a fire-and-forget notification |
| `service` | no | Step block — references a service type for the job |
| `in_parallel` | no | Step block — runs multiple steps concurrently |
| `on_success` | no | Hook — runs after all steps succeed |
| `on_failure` | no | Hook — runs after a step fails |
| `on_cancel` | no | Hook — runs when the build is cancelled |
| `ensure` | no | Hook — always runs regardless of outcome |

#### Tags

The `tags` attribute routes a job to workers with matching tags. A job with `tags = ["gpu"]` will only run on workers started with `--tags gpu`. The matching uses AND logic — a job with `tags = ["gpu", "vpn"]` requires a worker with **both** tags.

Jobs without tags run on any non-exclusive worker.

```hcl
job "train-model" {
  tags = ["gpu"]

  task "train" {
    run "exec" { path = "./train.sh" }
  }
}
```

Tags must be valid slugs (lowercase alphanumeric and hyphens). Maximum 10 tags per job. See [Workers](Workers.md#tags) for the worker-side configuration.

#### Serial Groups

The `serial_groups` attribute provides cross-job mutual exclusion — only one job from a group runs at a time. This is useful for deploy jobs that shouldn't overlap, or any set of jobs that share a resource that can't handle concurrent access.

```hcl
job "deploy-staging" {
  serial_groups = ["deploy"]

  get "git" "my_repo" { trigger = true }
  task "deploy" {
    run "exec" { path = "./deploy.sh" }
  }
}

job "deploy-prod" {
  serial_groups = ["deploy"]

  get "git" "my_repo" {}
  task "deploy" {
    run "exec" { path = "./deploy.sh" }
  }
}
```

When `deploy-staging` is running, `deploy-prod` will queue until it finishes, and vice versa. Serial groups are scoped per-pipeline.

A job can belong to multiple serial groups. It will only run when **none** of its groups have a running build from another job.

`serial_groups` is orthogonal to `concurrency` — both checks apply. A build must pass both the per-job concurrency limit and the serial group check before starting. Note that `serial_groups` only provides cross-job mutual exclusion; to also limit a single job to one build at a time, set `concurrency = 1` on the job.

#### for_each

`for_each` generates multiple instances of a job, each with different configuration. It follows Terraform's semantics. Each instance is a real, independent job with its own builds, status, and logs.

**Accepted types:**

- **Set of strings** via `toset()`: `for_each = toset(["a", "b", "c"])`
- **Map**: `for_each = { key1 = "val1", key2 = "val2" }`

Raw lists are not accepted (use `toset()` to convert).

**Accessors:** Inside a `for_each` job, use `each.key` and `each.value`:

- For sets: `each.key` and `each.value` are both the set element
- For maps: `each.key` is the map key, `each.value` is the map value

**Job naming:** Each instance gets a canonical name of `{jobname}--{slugified-key}`. For example, `test` with keys `["1.21", "1.22"]` produces `test--1-21` and `test--1-22`. Dots, spaces, and special characters are slugified to hyphens.

```hcl
# Test against multiple Go versions
job "test" {
  for_each = toset(["1.21", "1.22", "1.23"])

  get "git" "code" { trigger = true }
  task "run" {
    run "docker" {
      image = "golang:${each.value}"
      cmd   = "go test ./..."
    }
  }
}
# Creates: test--1-21, test--1-22, test--1-23
# each.key = each.value = "1.21", "1.22", "1.23"

# Deploy to named environments
job "deploy" {
  for_each = {
    "staging" = "us-east-1"
    "prod"    = "eu-west-1"
  }

  task "deploy" {
    run "exec" {
      path = "deploy.sh"
      args = ["--env", each.key, "--region", each.value]
    }
  }
}
# Creates: deploy--staging, deploy--prod
# each.key = "staging", each.value = "us-east-1"

# With variables
variable "go_versions" {
  type    = string
  default = "1.21,1.22"
}

job "test" {
  for_each = toset(split(",", var.go_versions))
  # ...
}
```

#### matrix

`matrix` is syntactic sugar for the common case of testing across combinations (cartesian product). It expands to an equivalent `for_each` map.

```hcl
job "test" {
  matrix {
    go = ["1.21", "1.22"]
    os = ["linux", "darwin"]
  }

  task "run" {
    run "exec" {
      path = "/bin/sh"
      args = ["-c", "echo go=${each.value.go} os=${each.value.os}"]
    }
  }
}
# Creates 4 jobs (cartesian product):
#   test--1-21-darwin, test--1-21-linux, test--1-22-darwin, test--1-22-linux
#
# each.key   = "1-21-linux" (slugified composite key)
# each.value = { go = "1.21", os = "linux" } (object with named fields)
```

`matrix` is equivalent to writing the `for_each` map manually:

```hcl
# This matrix block:
matrix {
  go = ["1.21", "1.22"]
  os = ["linux", "darwin"]
}

# Is equivalent to:
for_each = {
  "1-21-darwin" = { go = "1.21", os = "darwin" }
  "1-21-linux"  = { go = "1.21", os = "linux" }
  "1-22-darwin" = { go = "1.22", os = "darwin" }
  "1-22-linux"  = { go = "1.22", os = "linux" }
}
```

You can also build the same cartesian product using raw HCL `for` expressions with `setproduct`, which gives you full control over key construction:

```hcl
for_each = {
  for pair in setproduct(["1.21", "1.22"], ["linux", "darwin"]) :
  "${pair[0]}-${pair[1]}" => { go = pair[0], os = pair[1] }
}
```

This produces the same result as the `matrix` block above. Use `setproduct` when you need custom key formatting or want to filter combinations.

`matrix` and `for_each` are **mutually exclusive** on the same job.

#### passed with for_each jobs

When a downstream job has `passed = ["test"]` and `test` is a `for_each` job, **ALL instances** must have successful builds before the downstream job runs:

```hcl
job "test" {
  for_each = toset(["unit", "integration"])

  get "git" "code" { trigger = true }
  task "run" {
    run "exec" {
      path = "/bin/sh"
      args = ["-c", "make test-${each.value}"]
    }
  }
}
# Creates: test--unit, test--integration

job "deploy" {
  get "git" "code" {
    trigger = true
    passed  = ["test"]  # waits for BOTH test--unit AND test--integration
  }
  task "deploy" {
    run "exec" { path = "./deploy.sh" }
  }
}
```

#### Lifecycle on pipeline update

- Adding a key creates a new job instance
- Removing a key deletes that instance (with build history)
- Existing keys update the instance in place
- Evaluation happens at pipeline create/update time, not at build runtime

```hcl
job "deploy" {
  concurrency = 1
  timeout     = "30m"

  get "git" "my_repo" {
    trigger = true
  }

  task "deploy" {
    run "exec" {
      path = "./deploy.sh"
    }
  }
}
```

```hcl
job "build" {
  get "git" "my_repo" {
    trigger = true
  }

  task "compile" {
    run "exec" {
      path = "make"
      args = ["build"]
    }
  }

  put "git" "my_repo" {
    params {
      name = "my-repo"
    }
  }

  on_success "exec" {
    path = "echo"
    args = ["build succeeded"]
  }

  on_failure "exec" {
    path = "echo"
    args = ["build failed"]
  }

  ensure "exec" {
    path = "echo"
    args = ["cleanup"]
  }
}
```

### get

Fetches a resource version. If `trigger = true`, the job runs automatically when a new version is detected.

```hcl
get "git" "my_repo" {
  trigger = true
  passed  = ["test"]
}
```

| Field     | Required | Description                                    |
|-----------|----------|------------------------------------------------|
| `type`    | yes      | Label, resource type name                      |
| `name`    | yes      | Label, resource name                           |
| `trigger`  | no       | Auto-run the job on new versions (default `false`) |
| `passed`   | no       | List of job names that must have run with this version first |
| `timeout`  | no       | Maximum duration for the step (e.g. `"2m"`, `"30s"`) |
| `attempts` | no       | Maximum number of times to try the step (default `1`, no retry) |
| `on_success` | no     | Hook — runs after the step succeeds |
| `on_failure` | no     | Hook — runs after the step fails |
| `on_cancel`  | no     | Hook — runs when the build is cancelled |
| `ensure`     | no     | Hook — always runs regardless of outcome |

#### Exported version metadata

After a get step succeeds, its version metadata is automatically forwarded to all subsequent steps as environment variables. The naming convention is `GET_<STEPNAME>_<KEY>`, where the step name is uppercased with non-alphanumeric characters replaced by `_`, and the `version_` prefix is stripped from the key.

For example, a `get "git" "my-repo"` step that fetches version `{ref: "abc123"}` will export `GET_MY_REPO_REF=abc123` to all subsequent steps.

These variables are available in both exec and Docker-based steps. The built-in Docker runner uses the `$env` placeholder to forward all parameters (including exported metadata) into the container as `-e` flags. If you define a custom Docker runner, include `$env` in the args to get this behavior (see [Runners](Runners.md)).

Nested version objects are recursively flattened with `_` separators. For example, a version like:

```json
{"ref": "abc123", "metadata": {"sha": "def456", "author": "alice"}}
```

produces the following environment variables for the pull command and subsequent steps:

| Version key | Pull env var | Exported env var |
|---|---|---|
| `ref` | `version_ref=abc123` | `GET_MY_REPO_REF=abc123` |
| `metadata.sha` | `version_metadata_sha=def456` | `GET_MY_REPO_METADATA_SHA=def456` |
| `metadata.author` | `version_metadata_author=alice` | `GET_MY_REPO_METADATA_AUTHOR=alice` |

Arrays are flattened with numeric indices (e.g. `tags: ["v1", "v2"]` → `version_tags_0=v1`, `version_tags_1=v2`). Numbers and booleans are converted to their string representation.

### task

Runs a command via a runner.

```hcl
task "test" {
  run "exec" {
    path = "make"
    args = ["test"]
  }
}
```

| Field     | Required | Description                                    |
|-----------|----------|------------------------------------------------|
| `name`     | yes      | Label on the block                             |
| `timeout`  | no       | Maximum duration for the step (e.g. `"10m"`, `"1h"`) |
| `attempts` | no       | Maximum number of times to try the step (default `1`, no retry) |
| `inputs`   | no       | List of paths that must exist before the task runs |
| `outputs`  | no       | List of paths that must exist after the task finishes |
| `run`        | yes      | Runner command block |
| `on_success` | no       | Hook — runs after the step succeeds |
| `on_failure` | no       | Hook — runs after the step fails |
| `on_cancel`  | no       | Hook — runs when the build is cancelled |
| `ensure`     | no       | Hook — always runs regardless of outcome |

Example with inputs and outputs:

```hcl
task "build" {
  inputs  = ["pikoci/"]
  outputs = ["bin/pikoci"]
  run "exec" {
    path = "make"
    args = ["build"]
  }
}
```

Paths are checked with `os.Stat` relative to `$WORKDIR` and work for both files and directories. If an input is missing, the task fails immediately with a clear error. If an output is missing after the task finishes, the build fails with a descriptive message.

#### Exporting values with `$PIKOCI_OUTPUT`

Task steps can export key-value pairs to subsequent steps by writing `KEY=VALUE` lines to the file pointed to by the `$PIKOCI_OUTPUT` environment variable. After the task succeeds, the worker parses this file and makes the values available to all subsequent steps as `TASK_<STEPNAME>_<KEY>` environment variables.

```hcl
task "build" {
  run "exec" {
    path = "bash"
    args = ["-c", "echo VERSION=1.2.3 >> $PIKOCI_OUTPUT"]
  }
}

task "deploy" {
  run "exec" {
    path = "bash"
    args = ["-c", "echo Deploying version $TASK_BUILD_VERSION"]
  }
}
```

Rules for the output file:
- One `KEY=VALUE` per line (split on first `=`, values can contain `=`)
- Lines starting with `#` are treated as comments and skipped
- Empty lines are skipped
- Keys are uppercased with non-alphanumeric characters replaced by `_`
- Maximum file size is 1MB
- If the task fails, its output is not parsed

#### Naming convention

Step names are sanitized for use in environment variable names: uppercased with all non-alphanumeric characters replaced by `_`. For example, `my-repo` becomes `MY_REPO` and `build.app` becomes `BUILD_APP`. Note that different step names may map to the same prefix (e.g. `my-repo` and `my.repo` both become `MY_REPO`); in that case, the last writer wins.

### put

Pushes to a resource, running its `push` command.

```hcl
put "git" "my_repo" {
  params {
    name = "my-repo"
  }
}
```

| Field     | Required | Description                                    |
|-----------|----------|------------------------------------------------|
| `type`     | yes      | Label, resource type name                      |
| `name`     | yes      | Label, resource name                           |
| `timeout`  | no       | Maximum duration for the step (e.g. `"5m"`, `"30s"`) |
| `attempts` | no       | Maximum number of times to try the step (default `1`, no retry) |
| `params`     | no       | Block with key/value pairs passed to the resource type |
| `on_success` | no       | Hook — runs after the step succeeds |
| `on_failure` | no       | Hook — runs after the step fails |
| `on_cancel`  | no       | Hook — runs when the build is cancelled |
| `ensure`     | no       | Hook — always runs regardless of outcome |

### notify

Sends a fire-and-forget notification. See [Notifications](Notifications.md).

```hcl
notify "github-check" "ci" {
  status = "in_progress"
}
```

| Field     | Required | Description                                    |
|-----------|----------|------------------------------------------------|
| `type`     | yes      | Label, notification type name                  |
| `name`     | yes      | Label, notification name                       |
| `message`  | no       | Overrides the notification's default message   |

Any other attributes are passed to the notification type's command with a `notify_` prefix.

Notify steps also work in hooks (`on_success`, `on_failure`, `on_cancel`, `ensure`).

### service

References a top-level `service_type` for the job. Services are started before tasks and stopped unconditionally after.

```hcl
job "test" {
  service "postgres" {
    version = "16"
  }

  get "cron" "timer" { trigger = true }
  task "run-tests" {
    run "exec" {
      path = "make"
      args = ["test"]
    }
  }
}
```

An empty body references a top-level `service` block by name. Attributes in the body are param overrides.

### in_parallel

Runs multiple steps concurrently within a job.

```hcl
job "build" {
  in_parallel {
    limit     = 2        # optional: max concurrent steps (0 = unlimited)
    fail_fast = true     # optional: cancel remaining on first failure (default: false)

    get "git" "frontend" { trigger = true }
    get "git" "backend"  { trigger = true }
    task "lint" {
      run "exec" { path = "/bin/sh" args = ["-c", "echo linting"] }
    }
  }

  task "build" {
    run "exec" { path = "/bin/sh" args = ["-c", "echo building"] }
  }
}
```

| Field       | Required | Description                                          |
|-------------|----------|------------------------------------------------------|
| `limit`     | no       | Max concurrent steps. `0` or omitted = no limit.     |
| `fail_fast` | no       | Cancel remaining steps on first failure. Default: `false`. |
| `get`         | no       | Step block — fetches a resource version |
| `task`        | no       | Step block — runs a command via a runner |
| `put`         | no       | Step block — pushes to a resource |
| `notify`      | no       | Step block — sends a fire-and-forget notification |
| `timeout`     | no       | Wall-clock time limit for the entire group |
| `attempts`    | no       | Retry the entire block on failure |
| `on_success`  | no       | Hook — runs after the group succeeds |
| `on_failure`  | no       | Hook — runs after the group fails |
| `on_cancel`   | no       | Hook — runs when the build is cancelled |
| `ensure`      | no       | Hook — always runs regardless of outcome |

**Allowed inner step types:** `get`, `task`, `put`, `notify`. Services are not allowed inside `in_parallel`.

**Nesting:** `in_parallel` blocks cannot be nested inside other `in_parallel` blocks.

**Exported variables:** Steps inside `in_parallel` see variables exported by steps *before* the block, but not by sibling parallel steps. After the block completes, all exported variables are available to subsequent steps.

**Timeout/Attempts:** `timeout` on the block applies to wall-clock time of the entire group. `attempts` retries the entire block. Inner steps can have their own `timeout` and `attempts` independently.

**Hooks:** The `in_parallel` block supports `on_success`, `on_failure`, `on_cancel`, and `ensure` hooks, which fire based on whether the group as a whole succeeded or failed.

### Step hooks

Each step (and the job itself) can have `on_success`, `on_failure`, `on_cancel`, and `ensure` blocks:

- `on_success` runs after the step succeeds
- `on_failure` runs after the step fails
- `on_cancel` runs when the build is cancelled (via UI, CLI, or API)
- `ensure` always runs, regardless of success, failure, or cancellation

Hooks can contain runner commands, `put` steps, or `notify` steps:

```hcl
task "deploy" {
  run "exec" {
    path = "make"
    args = ["deploy"]
  }
  on_failure "exec" {
    path = "echo"
    args = ["deploy failed"]
  }
}
```

Put and notify steps in hooks use an unlabeled hook block:

```hcl
job "test" {
  task "run-tests" {
    run "exec" {
      path = "make"
      args = ["test"]
    }
  }

  on_success {
    notify "github-check" "ci" {
      conclusion = "success"
    }
  }

  on_failure {
    notify "github-check" "ci" {
      conclusion = "failure"
    }
  }
}
```

Job-level hooks have access to `$BUILD_STATUS` (`succeeded` or `failed`) in addition to all other build metadata environment variables (`$BUILD_NUMBER`, `$BUILD_JOB_NAME`, `$BUILD_PIPELINE_NAME`, `$BUILD_TEAM_NAME`).

### Step timeout

Any step can set a `timeout` to limit how long its runner execution takes. The value is a Go duration string (e.g. `"30s"`, `"5m"`, `"1h30m"`). If the step exceeds the timeout, the process is killed, the step is marked as failed with a "step timed out after ..." message in the logs, and `on_failure`/`ensure` hooks still run normally. If no timeout is set, the step runs with no time limit.

```hcl
task "long-build" {
  timeout = "10m"
  run "exec" {
    path = "make"
    args = ["build"]
  }
  on_failure "exec" {
    path = "echo"
    args = ["build timed out or failed"]
  }
}
```

### Job timeout

Jobs can set a `timeout` to limit the total wall-clock time for all plan steps. The value is a Go duration string (e.g. `"30m"`, `"1h"`, `"2h30m"`). If the build exceeds the timeout, the running step is killed, the build is marked as failed with a "job timed out after ..." message, and `on_cancel`/`ensure` hooks still run — just like user-initiated cancellation. If no timeout is set, the job runs with no time limit.

```hcl
job "integration" {
  timeout = "2h"

  get "git" "my-repo" { trigger = true }
  task "test" {
    timeout = "30m"
    run "exec" {
      path = "make"
      args = ["integration-test"]
    }
  }
}
```

When both a job timeout and a step timeout are set, whichever expires first takes effect.

### Step retry

Any step can set `attempts` to retry on failure. The value is the maximum number of times the step will be tried (default `1`, no retry). If the step fails and attempts remain, the runner is re-invoked. Hooks (`on_failure`, `on_success`, `ensure`) only run after the final attempt. When combined with `timeout`, each attempt gets a fresh timeout. Attempt markers (e.g. `--- attempt 2/3 ---`) appear in the build logs starting from the second attempt onward.

```hcl
task "flaky-test" {
  timeout  = "5m"
  attempts = 3
  run "exec" {
    path = "make"
    args = ["test"]
  }
  on_failure "exec" {
    path = "echo"
    args = ["tests failed after 3 attempts"]
  }
}
```

## Built-in resource type: artifact

The `artifact` resource type passes build outputs (compiled binaries, test reports, etc.) between jobs in the same pipeline. It stores tarballs on the local filesystem using the cache directory, so no external storage service is required for single-worker setups.

### Parameters

| Parameter  | Level    | Description                                                    |
|------------|----------|----------------------------------------------------------------|
| `dir`      | resource | Directory to extract artifacts into during pull                |
| `base_dir` | resource | Override the default storage directory (defaults to `$CACHE_DIR`) |
| `dir`      | put      | Source directory to archive and push as an artifact             |

### Usage example

```hcl
resource "artifact" "build-output" {
  params {
    dir = "build-output"
  }
}

job "build" {
  task "compile" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "mkdir -p output && echo 'hello' > output/result.txt"]
    }
  }

  put "artifact" "build-output" {
    dir = "output"
  }
}

job "deploy" {
  get "artifact" "build-output" {
    trigger = true
    passed  = ["build"]
  }

  task "deploy" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "cat build-output/result.txt"]
    }
  }
}
```

### Multi-worker deployments

By default, artifacts are stored under the worker's XDG cache directory. In multi-worker setups, use a shared filesystem (NFS, GlusterFS, etc.) and set the `base_dir` parameter to point to the shared mount:

```hcl
resource "artifact" "build-output" {
  params {
    dir      = "build-output"
    base_dir = "/mnt/shared/artifacts/build-output"
  }
}
```

## Build Logs & Security

PikoCI is designed to keep sensitive information out of build logs:

- **Command lines are never shown.** The command path and arguments are not printed in build output — only the process's stdout and stderr are captured and displayed.
- **Secret values are redacted from API responses.** When secrets are injected as environment variables, their values are not visible in pipeline configuration returned by the API.
- **Public pipeline responses are sanitized.** Sensitive fields (secrets, variable values) are stripped from public API responses. See [Public Pipelines](Public-Pipelines.md) for details.
- **Echoing secrets is the user's responsibility.** If a step explicitly prints a secret value (e.g. `echo $SECRET`), that output will appear in the build logs.
- **Debug-level server logs may include commands.** When the server is started with debug logging, command details may appear in server-side logs, but these are never exposed to users through the UI or API.

## Full example

Using built-in `git` and `docker` (no inline resource_type or runner blocks needed):

```hcl
variable "repo_url" {
  type    = string
  default = "https://github.com/PikoCI/pikoci.git"
}

variable "repo_name" {
  type    = string
  default = "pikoci"
}

resource "git" "pikoci" {
  params {
    url  = var.repo_url
    name = var.repo_name
  }
}

resource "cron" "schedule" {
  check_interval = "@every 10s"
}

job "test" {
  get "git" "pikoci" {
    trigger = true
  }

  task "run-tests" {
    run "docker" {
      image = "golang:1.25"
      cmd   = "cd ${var.repo_name} && make test"
    }
  }
}

job "deploy" {
  get "git" "pikoci" {
    passed  = ["test"]
    trigger = true
  }

  task "deploy" {
    run "exec" {
      path = "echo"
      args = ["deploying..."]
    }
  }

  on_success "exec" {
    path = "echo"
    args = ["deployed"]
  }
}
```
