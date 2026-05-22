# Coming from Concourse

PikoCI's resource model is directly inspired by [Concourse CI](https://concourse-ci.org). If you're familiar with Concourse, you'll find the concepts similar but with some key differences.

## Concept mapping

| Concourse | PikoCI | Notes |
|-----------|--------|-------|
| Resource type | `resource_type` | Same concept. PikoCI uses shell commands instead of Docker images. |
| Resource | `resource` | Same concept. |
| Job | `job` | Same concept. |
| Get step | `get` | Same. Fetches a resource version. |
| Task step | `task` | Similar. Uses a runner instead of a task config with `image_resource`. |
| Put step | `put` | Same. Pushes to a resource. |
| `image_resource` | `runner` | Runners replace task image configuration. Define once, use everywhere. |
| Pipeline (YAML) | Pipeline (HCL) | HCL instead of YAML. |
| `fly` CLI | `pikoci client` | Similar commands: `set-pipeline` -> `pipelines create/update`. |
| Web UI | Built-in UI | Similar functionality. |
| `passed` constraint | `passed` | Same. Gates a resource version through upstream jobs. |
| `trigger: true` | `trigger = true` | Same. Auto-triggers the job on new versions. |
| `on_success` / `on_failure` / `ensure` | Same names | Same semantics. Available on both steps and jobs. |
| Webhook triggers | Webhook triggers | Same. `POST /webhooks/<token>` triggers a resource check. |
| Teams | Teams | Similar. PikoCI has team-based scoping. |

## Key differences

### Runners replace image_resource

In Concourse, every task requires an `image_resource` (a Docker image). In PikoCI, you define a **runner** that wraps execution:

**Concourse:**
```yaml
jobs:
- name: test
  plan:
  - get: repo
    trigger: true
  - task: run-tests
    config:
      platform: linux
      image_resource:
        type: docker-image
        source: {repository: golang}
      inputs:
      - name: repo
      run:
        path: sh
        args: ["-c", "cd repo && go test ./..."]
```

**PikoCI:**
```hcl
runner_type "docker" {
  run {
    path = "docker"
    args = ["run", "--rm", "-v", "$WORKDIR:/workdir", "-w", "/workdir", "$image", "$cmd"]
  }
}

job "test" {
  get "git" "repo" {
    trigger = true
  }
  task "run-tests" {
    run "docker" {
      image = "golang:1.25"
      cmd   = "go test ./..."
    }
  }
}
```

### Resource types use shell commands, not Docker images

Concourse resource types are Docker images implementing the check/in/out interface. PikoCI resource types are shell commands:

**Concourse:**
```yaml
resource_types:
- name: git
  type: docker-image
  source:
    repository: concourse/git-resource
```

**PikoCI:**
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

### Single binary deployment

Concourse requires PostgreSQL, a web node, and one or more worker nodes. PikoCI runs as a single binary with optional external dependencies:

```bash
# Concourse: multiple services
concourse web --postgres-* ... &
concourse worker --tsa-host ... &

# PikoCI: one command
pikoci server --jwt-secret my-secret
```

### HCL instead of YAML

Pipelines use HCL syntax, which supports variables, string interpolation, and is less error-prone than YAML.

## Known gaps vs Concourse

- **No built-in resource registry**: Concourse ships with built-in resource types (git, s3, time, etc.). PikoCI ships only with the `cron` resource type. You define your own resource types in HCL.

## Migration tips

1. Start by defining your resource types as shell commands
2. Create a runner for your execution environment (e.g., Docker)
3. Convert your pipeline YAML to HCL, mapping `get`/`task`/`put` steps
4. Use `variable` blocks for values that were in your Concourse credential manager

A [Concourse pipeline importer](https://github.com/pikoci/pikoci/issues/210) is planned.
