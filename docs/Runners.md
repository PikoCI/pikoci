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

- **`pikoci://<name>`** resolves to the PikoCI registry. For shipped built-ins (`exec`, `docker`), the embedded definition is used directly (no network call).
- **`https://...`** or **`http://...`** fetches HCL from any URL.

When `source` is set, you must not define an inline `run` block. PikoCI will error if both are present.

> **Note:** The source is resolved once when the pipeline is created or updated. If the remote HCL file changes, you must re-set the pipeline to pick up the new definition.

## Overriding built-ins

All built-in runners (`exec`, `docker`) can be overridden by defining a `runner_type` block with the same name in your pipeline. Inline definitions always take precedence over built-ins.

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
    image = "golang:1.23"
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
    image = "golang:1.23"
    cmd   = "make test"
    args  = ["-e", "CI=true", "-e", "FOO=bar"]
  }
}
```

With volumes:

```hcl
task "test" {
  run "docker" {
    image = "golang:1.23"
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
    image = "golang:1.23"
    cmd   = "make test"
    args  = concat(
      ["-e", "CI=true"],
      ["-e", "FOO=bar"],
      ["-v", "/cache:/cache"],
    )
  }
}
```

## Example: custom shell runner

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
