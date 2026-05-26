# Pipeline Reference

Pipelines are defined in [HCL](https://github.com/hashicorp/hcl). A pipeline file contains `variable`, `resource_type`, `resource`, `runner`, `secret_type`, `secret`, `service`, and `job` blocks.

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
| other    | no       | Config attributes passed as `param_<key>` env vars to the get command |

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
| `ready_check` | no       | Runner command polled until exit 0 or timeout                    |
| `stop`        | yes*     | Runner command to stop the service (always runs)                 |

\* Not required when `source` is set.

The `ready_check` block accepts `interval` (default `"1s"`) and `timeout` (default `"60s"`) fields.

## job

Jobs contain a plan of steps executed in order. Each step is one of `get`, `task`, `put`, or `service`.

The optional `concurrency` attribute limits how many builds of the job can run simultaneously. When the limit is reached, new builds are re-queued and wait until a slot frees up. The default value `0` means unlimited.

```hcl
job "deploy" {
  concurrency = 1

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
| `secrets`  | no       | Map of secret_type name to path (e.g. `{"vault" = "secret/data/db"}`) |

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
| `secrets`  | no       | Map of secret_type name to path (e.g. `{"vault" = "secret/data/db"}`) |

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
| `secrets`  | no       | Map of secret_type name to path (e.g. `{"vault" = "secret/data/db"}`) |

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

### Step hooks

Each step (and the job itself) can have `on_success`, `on_failure`, `on_cancel`, and `ensure` blocks:

- `on_success` runs after the step succeeds
- `on_failure` runs after the step fails
- `on_cancel` runs when the build is cancelled (via UI, CLI, or API)
- `ensure` always runs, regardless of success, failure, or cancellation

Hooks can contain runner commands or `put` steps:

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

Put steps in hooks use an unlabeled hook block:

```hcl
job "test" {
  task "run-tests" {
    run "exec" {
      path = "make"
      args = ["test"]
    }
  }

  on_success {
    put "github-check" "ci" {
      conclusion = "success"
    }
  }

  on_failure {
    put "github-check" "ci" {
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
