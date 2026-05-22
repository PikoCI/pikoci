<div align="center">
  <img src="pikoci/transport/http/assets/images/logo.svg" width="96" height="96" alt="PikoCI Logo"/>

  # PikoCI

  **The portable CI/CD. One binary, any database, any queue, runs anywhere.**

  [![Go Version](https://img.shields.io/badge/go-1.25+-blue)](https://golang.org)
  [![License](https://img.shields.io/badge/license-Apache%202.0-yellow)](LICENSE)
  [![Go Report Card](https://goreportcard.com/badge/github.com/pikoci/pikoci)](https://goreportcard.com/report/github.com/pikoci/pikoci)

  [Documentation](https://github.com/xescugc/pikoci/wiki) · [Quick Start](#quick-start) · [Contributing](#contributing)
</div>

<!-- GIF goes here -->
<!-- ![PikoCI demo](docs/images/pikoci.gif) -->

## What is PikoCI?

PikoCI is a self-hosted CI/CD system built around a resource/resource-type pipeline model, inspired by [Concourse CI](https://concourse-ci.org), but designed to run anywhere without operational pain.

Most CI/CD tools either lock you into a cloud platform or require spinning up multiple services just to get started. PikoCI runs as a single binary with pluggable database and queue backends. Use what you already have, or run entirely in memory with zero external dependencies. Bundle your binary, your pipelines, and your database file, and move them anywhere.

Pipelines are defined in [HCL](https://github.com/hashicorp/hcl). The runner abstraction means you're not locked into a specific execution environment.


## Features

- **Single binary**: download and run. No Docker Compose, no Kubernetes, no setup scripts.
- **Truly portable**: bundle the binary with your pipeline config and SQLite file. Move it anywhere, run it instantly.
- **In-memory mode**: run the entire system in memory for development and testing. Zero files, zero cleanup.
- **Any SQL database**: SQLite (built-in), MySQL, PostgreSQL, and any other SQL-compatible backend.
- **Any queue backend**: pluggable via [google/go-cloud](https://gocloud.dev/howto/pubsub/), including NATS, Kafka, and RabbitMQ. [AWS SQS and GCP Pub/Sub planned (#209)](https://github.com/xescugc/pikoci/issues/209).
- **Resource model**: pipelines built from resources and resource types. Clean, composable, reusable.
- **HCL pipelines**: more expressive and readable than YAML. Familiar to anyone who has used Terraform.
- **Flexible runners**: run jobs on the host machine or define your own runner. [Docker runner planned (#206)](https://github.com/xescugc/pikoci/issues/206).
- **Pipelines at startup**: pass a pipeline config at launch and it's ready the moment the server starts. No CLI or UI step required.
- **Public pipelines**: mark a pipeline as public so anyone can view its status without an account. Perfect for open source projects.
- **Built-in UI**: visualize pipeline state, stream build logs, manage pipelines from a web interface.
- **Teams and users**: multi-user support with team-based access control. [Granular role management planned (#207)](https://github.com/xescugc/pikoci/issues/207).
- **DOT graph output**: export pipeline state as a DOT graph and pipe it to Graphviz for terminal-native visualization.


## Quick Start

### Download

```bash
# Linux (amd64)
curl -L https://github.com/xescugc/pikoci/releases/latest/download/linux-amd64 -o pikoci
chmod +x pikoci

# macOS (amd64)
curl -L https://github.com/xescugc/pikoci/releases/latest/download/darwin-amd64 -o pikoci
chmod +x pikoci
```

Or build from source:

```bash
git clone https://github.com/xescugc/pikoci.git
cd pikoci
go build -o pikoci .
```

### Run with a pipeline

The fastest way to get started. Pass a pipeline config directly at launch. When the server starts, your pipeline is already loaded and ready:

```bash
./pikoci server \
  --db-system mem \
  --pubsub-system mem \
  --jwt-secret my-secret \
  --run-worker \
  --pipeline-name my-pipeline \
  --pipeline-config pipeline.hcl
```

Open [http://localhost:8080](http://localhost:8080) and log in with the default user `admin` and password `admin123`.

> **Users:** pass `--users 'username:hashed-password'` to add or update users at startup. If a user already exists, their password is updated. Use `pikoci user-password` to generate password hashes.

### Example pipeline

A cron resource checks for new versions every 10 seconds. When a new version is detected, it triggers the `gen` job, which runs `echo IN` on the host machine.

```hcl
resource "cron" "my_cron" {
  check_interval = "@every 10s"
}

job "gen" {
  get "cron" "my_cron" {
    trigger = true
  }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["IN"]
    }
  }
}
```


## Local Execution

Run a single pipeline job locally without starting a server. PikoCI spins up an in-memory database, pubsub, and worker in a single process, executes the job, streams the output, and exits with an appropriate exit code.

```bash
# Run a job from a pipeline file
pikoci run -p pipeline.hcl -j my-job

# Override variables
pikoci run -p pipeline.hcl -j my-job --var db_password=local

# Use a local directory instead of pulling a resource (type.name format)
pikoci run -p pipeline.hcl -j test --resource git.my-repo=./my-repo

# Load variables from a JSON file
pikoci run -p pipeline.hcl -j my-job -v vars.json
```

Resource overrides (`--resource name=path`) copy the local directory into the working directory, skipping the resource type's pull command entirely. This is useful for testing pipeline changes against local code.

See [#161](https://github.com/xescugc/pikoci/issues/161) for more details.


## Examples

The [`examples/`](examples/) folder contains ready-to-run pipelines and a Docker Compose file for one-command evaluation. See the [examples README](examples/README.md) for details.


## Pipeline Visualization

PikoCI can export pipeline state as a [DOT graph](https://graphviz.org/doc/info/lang.html):

```bash
# Export as SVG
pikoci client -u localhost:8080 pipelines graph -n my-pipeline | dot -Tsvg > pipeline.svg

# Live view in terminal
watch -n2 'pikoci client -u localhost:8080 pipelines graph -n my-pipeline | dot -Txtk'
```


## Running Workers Separately

For production setups, run the server and workers as separate processes on different machines:

```bash
# Server (logs a worker token on startup)
./pikoci server --db-system mysql --pubsub-system nats --jwt-secret my-secret --run-worker=false

# Generate a worker token (or copy from server logs)
./pikoci worker-token --jwt-secret my-secret

# Worker, can run anywhere with access to the server
./pikoci worker --pikoci-url http://your-server:8080 --pubsub-system nats --worker-token <token>
```

Full server and worker configuration options are covered in the [documentation](https://github.com/xescugc/pikoci/wiki/Server).


## Dogfooding: PikoCI runs its own CI

PikoCI uses itself for CI. See it live at [pikoci.com/teams/main/pipelines/pr_test](https://pikoci.com/teams/main/pipelines/pr_test). The [full pipeline](deploy/pipeline.hcl) runs lint, unit tests, integration tests, and backend tests with services — all defined in HCL:

```hcl
resource_type "git" {
  source = "pikoci://git"
}

resource "git" "pikoci_pr" {
  params {
    url   = var.git_url
    name  = var.git_name
    pr    = true
    token = var.github_token
  }
}

job "lint" {
  get "git" "pikoci_pr" { trigger = true }
  task "make" {
    run "docker" {
      image = "golang:1.25.1"
      cmd   = "cd ${var.git_name} && make lint"
    }
  }
}

job "test-mock" {
  get "git" "pikoci_pr" { trigger = true }
  task "make" {
    run "docker" {
      image = "golang:1.25.1"
      cmd   = "cd ${var.git_name} && make test-mock"
    }
  }
}

job "test-integration" {
  get "git" "pikoci_pr" { trigger = true }
  task "make" {
    run "docker" {
      image = "golang:1.25.1"
      cmd   = "cd ${var.git_name} && make test-integration"
    }
  }
}

job "test-backends" {
  get "git" "pikoci_pr" {
    trigger = true
    passed  = ["lint", "test-mock", "test-integration"]
  }

  service "mariadb" {}
  service "postgresql" {}
  service "nats" {}
  service "rabbitmq" {}
  service "kafka" {}
  service "vault" {}

  task "make" {
    run "docker" {
      image = "golang:1.25.1"
      cmd   = "cd ${var.git_name} && make test-backends"
      args  = ["--network=host"]
    }
  }
}
```

The `test-backends` job uses [service types](https://github.com/xescugc/pikoci/wiki/Services) to spin up MariaDB, PostgreSQL, NATS, RabbitMQ, Kafka, and Vault as Docker containers, runs the backend integration tests against them, then tears everything down. See the [full pipeline](deploy/pipeline.hcl) for secrets, variables, and service definitions.


## Coming from Concourse?

PikoCI's resource model is directly inspired by Concourse. The main differences:

- **Runners** replace task `image_resource`. Define a runner once, reference it from any job
- **Deployment** is a single binary instead of a multi-service setup requiring PostgreSQL
- **Secrets** use `secret_type` blocks with secret-backed variables. Built-in support for Vault and file-based secrets (JSON, env, raw)

[Concourse pipeline importer planned (#210)](https://github.com/xescugc/pikoci/issues/210).


## Documentation

Full documentation is in the [wiki](https://github.com/xescugc/pikoci/wiki):

- [Pipeline configuration reference](https://github.com/xescugc/pikoci/wiki/Pipeline)
- [Resource types](https://github.com/xescugc/pikoci/wiki/Resource-Types)
- [Runners](https://github.com/xescugc/pikoci/wiki/Runners)
- [Server configuration](https://github.com/xescugc/pikoci/wiki/Server)
- [Variables and secrets](https://github.com/xescugc/pikoci/wiki/Variables)
- [Database backends](https://github.com/xescugc/pikoci/wiki/Database)
- [Queue backends](https://github.com/xescugc/pikoci/wiki/Queue)
- [CLI reference](https://github.com/xescugc/pikoci/wiki/CLI)
- [Public pipelines](https://github.com/xescugc/pikoci/wiki/Public-Pipelines)
- [Running workers separately](https://github.com/xescugc/pikoci/wiki/Workers)
- [Deployment](https://github.com/xescugc/pikoci/wiki/Deployment)
- [Portability and bundling](https://github.com/xescugc/pikoci/wiki/Portability)
- [Coming from Concourse](https://github.com/xescugc/pikoci/wiki/Concourse)


## Contributing

PikoCI is open source and contributions are welcome. Please open an issue before starting work on a large feature so we can discuss the approach.

```bash
git clone https://github.com/xescugc/pikoci.git
cd pikoci
make test
```


## License

Apache 2.0, see [LICENSE](LICENSE).

<div align="center">
  <sub>Built with Go · Inspired by Concourse · Designed to run anywhere</sub>
</div>

