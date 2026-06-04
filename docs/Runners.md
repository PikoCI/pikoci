# Runners

Runners define how commands are executed. A runner wraps process execution so you can run jobs on the host, inside Docker containers, or in any custom environment.

## Defining a runner

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

| Field    | Required | Description                          |
|----------|----------|--------------------------------------|
| `name`   | yes      | Label on the block                   |
| `source` | no       | URL to fetch the definition from (mutually exclusive with inline `run`) |
| `run`    | yes*     | Block with `path` and `args`         |
| `path`   | no       | Executable path                      |
| `args`   | no       | List of arguments                    |

\* Not required when `source` is set.

### Variable expansion

Inside `path` and `args`, PikoCI expands:

| Variable    | Description                                 |
|-------------|---------------------------------------------|
| `$WORKDIR`  | Temporary working directory for the job     |
| `$<param>`  | Any parameter passed from a `run` block in a task |

For example, when a task uses `run "docker" { image = "golang:1.25" cmd = "make test" }`, the runner receives `$image` and `$cmd` as expandable variables.

## Sourcing from URL

Instead of defining the runner inline, you can point to an external HCL file:

```hcl
runner_type "my-docker" {
  source = "pikoci://docker"
}
```

Two URL formats are supported:

- **`pikoci://<name>`** resolves to the PikoCI registry. For shipped built-ins (`exec`, `docker`, `shell`), the embedded definition is used directly (no network call).
- **`https://...`** or **`http://...`** fetches HCL from any URL.

When `source` is set, you must not define an inline `run` block. PikoCI will error if both are present.

> **Note:** The source is resolved once when the pipeline is created or updated. If the remote HCL file changes, you must re-set the pipeline to pick up the new definition.

## Overriding built-ins

All built-in runners (`exec`, `docker`, `shell`) can be overridden by defining a `runner_type` block with the same name in your pipeline. Inline definitions always take precedence over built-ins.

This is useful when you need different default behavior. For example, the built-in `docker` runner uses `/bin/sh -ec` to run commands. If you want to always run with `--network=host` or use a different shell:

```hcl
runner_type "docker" {
  run {
    path = "docker"
    args = [
      "run", "--rm",
      "--network=host",
      "-v", "$WORKDIR:/workdir",
      "-w", "/workdir",
      "$args",
      "$image",
      "/bin/bash", "-ec", "$cmd",
    ]
  }
}
```

This replaces the built-in `docker` runner entirely for this pipeline.

## Type-level runner overrides

By default, a type's commands run using the runner specified in their command blocks (e.g. `check "exec" { ... }`). You can override this for all commands of a type by adding a `runner` block to the type definition.

This is useful when a type uses `exec` to run commands directly on the host, but you want all its operations to run inside Docker — for example, to provide the right CLI tools without installing them on the host.

### Resource type

```hcl
resource_type "git" {
  source = "pikoci://git"
  runner "docker" {
    image = "alpine/git:latest"
  }
}
```

All `check`, `pull`, and `push` commands for this resource type will run inside the specified Docker image instead of directly on the host.

### Notification type

```hcl
notification_type "slack" {
  source = "pikoci://slack"
  runner "docker" {
    image = "curlimages/curl:latest"
  }
}
```

### Secret type

```hcl
secret_type "vault" {
  source = "pikoci://vault"
  runner "docker" {
    image = "hashicorp/vault:latest"
  }
}
```

### Service type

```hcl
service_type "postgresql" {
  source = "pikoci://postgresql"
  runner "docker" {
    image = "docker:latest"
  }
}
```

### Passing extra Docker flags

Override params are passed to the runner template as variables. The built-in `docker` runner supports `image`, `cmd`, and `args`. Use `args` to pass extra `docker run` flags like volumes, environment variables, or privileged mode:

```hcl
resource_type "git" {
  source = "pikoci://git"
  runner "docker" {
    image = "alpine/git:latest"
    args  = ["-v", "/var/run/docker.sock:/var/run/docker.sock", "-e", "GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no"]
  }
}
```

### Restrictions

- **Only exec commands can be overridden.** If any command in the type already uses a non-exec runner, specifying a runner override is an error.
- The override applies to **all commands** of the type (check/pull/push for resource types, start/stop/ready_check for services, etc.).
- Override parameters (like `image`, `args`) are merged with the command's parameters.
- Works with sourced types: you can use `source = "pikoci://git"` and add a `runner` block to change where the sourced commands execute.

## Using a runner

Reference a runner by name in `task`, `on_success`, `on_failure`, `ensure`, and resource type `check`/`pull`/`push` blocks:

```hcl
job "build" {
  get "git" "repo" {
    trigger = true
  }

  task "compile" {
    run "docker" {
      image = "golang:1.25"
      cmd   = "make build"
    }
  }
}
```

Parameters in the `run` block (like `image` and `cmd` above) are passed to the runner as environment variables.

## Built-in: exec

The `exec` runner is built in. It runs commands directly on the host machine:

```hcl
task "hello" {
  run "exec" {
    path = "echo"
    args = ["hello world"]
  }
}
```

The exec runner expands `$path` and `$args` from the `run` block:

```go
// Built-in exec runner definition
runner_type "exec" {
  run {
    path = "$path"
    args = ["$args"]
  }
}
```

You do not need to declare the `exec` runner in your pipeline. It is always available.

## Built-in: docker

The `docker` runner is built in. It runs commands inside Docker containers:

```hcl
task "test" {
  run "docker" {
    image = "golang:1.25"
    cmd   = "make test"
  }
}
```

### Params

| Param   | Required | Description                              |
|---------|----------|------------------------------------------|
| `image` | yes      | Docker image to run                      |
| `cmd`   | yes      | Shell command to execute inside the container |
| `args`  | no       | Extra docker flags (env, volumes, etc.)  |

The docker runner mounts `$WORKDIR` as `/workdir` inside the container and runs the command with `/bin/sh -ec`.

### Extra docker flags

Use the `args` parameter to pass additional flags to `docker run`:

```hcl
task "test" {
  run "docker" {
    image = "golang:1.25"
    cmd   = "make test"
    args  = ["-e", "CI=true", "-e", "FOO=bar"]
  }
}
```

With volumes:

```hcl
task "test" {
  run "docker" {
    image = "golang:1.25"
    cmd   = "make test"
    args  = ["-v", "/data:/data"]
  }
}
```

With privileged mode:

```hcl
task "build-image" {
  run "docker" {
    image = "docker:latest"
    cmd   = "docker build -t myapp ."
    args  = ["--privileged"]
  }
}
```

Using HCL functions to build args dynamically:

```hcl
task "test" {
  run "docker" {
    image = "golang:1.25"
    cmd   = "make test"
    args  = concat(
      ["-e", "CI=true"],
      ["-e", "FOO=bar"],
      ["-v", "/cache:/cache"],
    )
  }
}
```

## Built-in: shell

The `shell` runner simplifies running shell commands in tasks and hooks. It replaces the common `run "exec" { path = "/bin/sh" args = ["-ec", "..."] }` pattern. It has two mutually exclusive modes: **inline** (`cmd`) and **file** (`file`).

### Inline mode (`cmd`)

Runs a shell command string via `<shell> -ec "<cmd>"`:

```hcl
task "test" {
  run "shell" {
    cmd = <<-EOT
      cd app
      make test
      make lint
    EOT
  }
}
```

### File mode (`file`)

Runs a script file. Relative paths are resolved against the build working directory:

```hcl
task "deploy" {
  run "shell" {
    file = "app/scripts/deploy.sh"
  }
}
```

When no `shell` param is set, the file is executed directly (chmod +x is applied), so the OS uses the script's shebang line. When `shell` is set, the file is passed as an argument to the specified shell.

### Params

| Param   | Required | Description                                              |
|---------|----------|----------------------------------------------------------|
| `cmd`   | no*      | Shell command string to run inline                       |
| `file`  | no*      | Path to a script file to execute                         |
| `shell` | no       | Shell binary to use (default `/bin/sh`)                  |

\* Exactly one of `cmd` or `file` must be set.

### Custom shell

Both modes accept an optional `shell` param to override the default `/bin/sh`:

```hcl
task "test" {
  run "shell" {
    shell = "/bin/bash"
    cmd   = "set -o pipefail; make test 2>&1 | tee test.log"
  }
}
```

### When to use shell vs exec

Use `shell` for shell commands and scripts. Use `exec` when you need to run a binary directly with specific arguments.

## Example: custom runner

```hcl
runner_type "bash" {
  run {
    path = "/bin/bash"
    args = ["-c", "$script"]
  }
}

job "example" {
  get "cron" "tick" { trigger = true }

  task "greet" {
    run "bash" {
      script = "echo hello from bash"
    }
  }
}
```
