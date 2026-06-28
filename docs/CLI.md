# CLI Reference

PikoCI provides three top-level commands: `server`, `worker`, and `client`, plus `run` for local execution, `pipeline edit` for local pipeline editing, and utility commands `user-password` and `worker-token`.

## Global structure

```
pikoci server              [flags]          # Start the server
pikoci worker              [flags]          # Start a standalone worker
pikoci client              [flags] <cmd>    # Interact with the API
pikoci run                 [flags]          # Run a pipeline job locally
pikoci pipeline edit       <file> [flags]   # Edit a pipeline HCL file in the browser
pikoci user-password       [flags]          # Generate hashed passwords
pikoci worker-token        [flags]          # Generate a worker authentication token
```

## client

Manage pipelines and jobs via the PikoCI API.

### Global flags

| Flag | Alias | Default | Required | Description |
|------|-------|---------|----------|-------------|
| `--url` | `-u` | `localhost:8080` | **yes** | PikoCI server URL |
| `--jwt` | | | no | JWT token (if not provided, reads from `$XDG_CONFIG_HOME/pikoci/authentication`) |

### login

Authenticate and store the JWT locally.

```bash
pikoci client -u localhost:8080 login -u admin -p admin123
```

| Flag | Alias | Required | Description |
|------|-------|----------|-------------|
| `--username` | `-u` | **yes** | Username |
| `--password` | `-p` | **yes** | Password |

### pipelines

Pipeline management commands. All require `--team-canonical` (default: `main`).

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--team-canonical` | `-tc` | `main` | Team scope |

#### pipelines create

```bash
pikoci client -u localhost:8080 pipelines create \
  -n my-pipeline -c pipeline.hcl -v vars.json
```

| Flag | Alias | Required | Description |
|------|-------|----------|-------------|
| `--name` | `-n`, `-pn` | **yes** | Pipeline name |
| `--config` | `-c` | **yes** | Path to HCL config file |
| `--vars` | `-v` | no | Path to JSON vars file |

#### pipelines update

```bash
pikoci client -u localhost:8080 pipelines update \
  -n my-pipeline -c pipeline.hcl --public
```

| Flag | Alias | Required | Description |
|------|-------|----------|-------------|
| `--name` | `-n`, `-pn` | **yes** | Pipeline name |
| `--config` | `-c` | **yes** | Path to HCL config file |
| `--vars` | `-v` | no | Path to JSON vars file |
| `--public` | | no | Make the pipeline publicly visible |

#### pipelines list

```bash
pikoci client -u localhost:8080 pipelines list
```

#### pipelines get

```bash
pikoci client -u localhost:8080 pipelines get -n my-pipeline
```

| Flag | Alias | Required | Description |
|------|-------|----------|-------------|
| `--name` | `-n`, `-pn` | **yes** | Pipeline name |

#### pipelines graph

Export the pipeline as a DOT graph.

```bash
pikoci client -u localhost:8080 pipelines graph -n my-pipeline | dot -Tsvg > pipeline.svg
```

| Flag | Alias | Default | Required | Description |
|------|-------|---------|----------|-------------|
| `--name` | `-n`, `-pn` | | **yes** | Pipeline name |
| `--format` | `-f` | `dot` | no | Output format |

#### pipelines delete

```bash
pikoci client -u localhost:8080 pipelines delete -n my-pipeline
```

| Flag | Alias | Required | Description |
|------|-------|----------|-------------|
| `--name` | `-n`, `-pn` | **yes** | Pipeline name |

#### pipelines rename

```bash
pikoci client -u localhost:8080 pipelines rename -n my-pipeline --new-name new-pipeline
```

| Flag | Alias | Required | Description |
|------|-------|----------|-------------|
| `--name` | `-n`, `-pn` | **yes** | Current pipeline name |
| `--new-name` | | **yes** | New name for the pipeline |

### jobs

Job management commands. Require `--team-canonical` and `--pipeline-name`.

| Flag | Alias | Required | Description |
|------|-------|----------|-------------|
| `--team-canonical` | `-tc` | **yes** | Team scope |
| `--pipeline-name` | `-pn` | **yes** | Pipeline name |

#### jobs get

```bash
pikoci client -u localhost:8080 jobs get -tc main -pn my-pipeline -n my-job
```

| Flag | Alias | Required | Description |
|------|-------|----------|-------------|
| `--job-name` | `-n`, `-jn` | **yes** | Job name |

#### jobs trigger

Manually trigger a job.

```bash
pikoci client -u localhost:8080 jobs trigger -tc main -pn my-pipeline -n my-job
```

| Flag | Alias | Required | Description |
|------|-------|----------|-------------|
| `--job-name` | `-n`, `-jn` | **yes** | Job name |

### users

User management commands.

#### users create

```bash
pikoci client users create --username newuser --password secret123
```

| Flag | Required | Description |
|------|----------|-------------|
| `--username` | **yes** | Username for the new User |
| `--password` | **yes** | Password for the new User |

#### users list

```bash
pikoci client users list
```

### teams

Team management commands.

#### teams create

```bash
pikoci client teams create --name my-team
```

| Flag | Required | Description |
|------|----------|-------------|
| `--name` | **yes** | Name of the Team |

#### teams list

```bash
pikoci client teams list
```

#### teams get

```bash
pikoci client teams get --canonical my-team
```

| Flag | Required | Description |
|------|----------|-------------|
| `--canonical` | **yes** | Canonical of the Team |

#### teams update

```bash
pikoci client teams update --canonical my-team --name new-name
```

| Flag | Required | Description |
|------|----------|-------------|
| `--canonical` | **yes** | Canonical of the Team |
| `--name` | **yes** | New name for the Team |

#### teams delete

```bash
pikoci client teams delete --canonical my-team
```

| Flag | Required | Description |
|------|----------|-------------|
| `--canonical` | **yes** | Canonical of the Team |

#### teams members

Team member management. All subcommands require `--team-canonical`.

| Flag | Required | Description |
|------|----------|-------------|
| `--team-canonical` | **yes** | Team scope |

##### teams members create

```bash
pikoci client teams members create --team-canonical my-team --username user1 --admin
```

| Flag | Required | Description |
|------|----------|-------------|
| `--username` | **yes** | Username of the member to add |
| `--admin` | no | Whether the member is a team admin |

##### teams members update

```bash
pikoci client teams members update --team-canonical my-team --username user1 --admin
```

| Flag | Required | Description |
|------|----------|-------------|
| `--username` | **yes** | Username of the member to update |
| `--admin` | no | Whether the member is a team admin |

##### teams members delete

```bash
pikoci client teams members delete --team-canonical my-team --username user1
```

| Flag | Required | Description |
|------|----------|-------------|
| `--username` | **yes** | Username of the member to remove |

### builds

Build management commands. All require `--team-canonical`, `--pipeline-name`, and `--job-name`.

| Flag | Required | Description |
|------|----------|-------------|
| `--team-canonical` | **yes** | Team scope |
| `--pipeline-name` | **yes** | Pipeline name |
| `--job-name` | **yes** | Job name |

#### builds list

```bash
pikoci client builds list --team-canonical main --pipeline-name my-pipeline --job-name my-job
```

#### builds get

```bash
pikoci client builds get --team-canonical main --pipeline-name my-pipeline --job-name my-job --build-number 1
```

| Flag | Required | Description |
|------|----------|-------------|
| `--build-number` | **yes** | Number of the Build |

#### builds delete

```bash
pikoci client builds delete --team-canonical main --pipeline-name my-pipeline --job-name my-job --build-number 1
```

| Flag | Required | Description |
|------|----------|-------------|
| `--build-number` | **yes** | Number of the Build |

#### builds cancel

```bash
pikoci client builds cancel --team-canonical main --pipeline-name my-pipeline --job-name my-job --build-number 1
```

| Flag | Required | Description |
|------|----------|-------------|
| `--build-number` | **yes** | Number of the Build |

#### builds retry

```bash
pikoci client builds retry --team-canonical main --pipeline-name my-pipeline --job-name my-job --build-number 1
```

| Flag | Required | Description |
|------|----------|-------------|
| `--build-number` | **yes** | Number of the Build |

### resources

Resource management commands. All require `--team-canonical` and `--pipeline-name`.

| Flag | Required | Description |
|------|----------|-------------|
| `--team-canonical` | **yes** | Team scope |
| `--pipeline-name` | **yes** | Pipeline name |

#### resources get

```bash
pikoci client resources get --team-canonical main --pipeline-name my-pipeline --resource-canonical my-resource
```

| Flag | Required | Description |
|------|----------|-------------|
| `--resource-canonical` | **yes** | Canonical of the Resource |

#### resources trigger

```bash
pikoci client resources trigger --team-canonical main --pipeline-name my-pipeline --resource-canonical my-resource
```

| Flag | Required | Description |
|------|----------|-------------|
| `--resource-canonical` | **yes** | Canonical of the Resource |

#### resources versions

```bash
pikoci client resources versions --team-canonical main --pipeline-name my-pipeline --resource-canonical my-resource
```

| Flag | Required | Description |
|------|----------|-------------|
| `--resource-canonical` | **yes** | Canonical of the Resource |

#### resources webhook-regenerate

```bash
pikoci client resources webhook-regenerate --team-canonical main --pipeline-name my-pipeline --resource-canonical my-resource
```

| Flag | Required | Description |
|------|----------|-------------|
| `--resource-canonical` | **yes** | Canonical of the Resource |

### triggers

Trigger management commands. All require `--team-canonical`.

| Flag | Required | Description |
|------|----------|-------------|
| `--team-canonical` | **yes** | Team scope |

#### triggers create

```bash
pikoci client triggers create --team-canonical main --name my-trigger --version '{"ref": "abc123"}'
```

| Flag | Required | Description |
|------|----------|-------------|
| `--name` | **yes** | Name of the Trigger |
| `--version` | **yes** | Version data as a JSON string |

#### triggers list

```bash
pikoci client triggers list --team-canonical main --name my-trigger --after 0
```

| Flag | Required | Description |
|------|----------|-------------|
| `--name` | **yes** | Name of the Trigger |
| `--after` | no | List triggers after this ID (default: 0) |

### api-tokens

Manage API tokens for non-interactive authenticated access. See [API Tokens](API-Tokens.md) for full documentation.

#### api-tokens create

Create a personal token (full user access):

```bash
pikoci client api-tokens create --name "my-script" --personal
```

Create a team-scoped token:

```bash
pikoci client api-tokens create --name "ci-deploy" --team-canonical main --role write
```

| Flag | Required | Description |
|------|----------|-------------|
| `--name` | **yes** | Name for the token (unique per user) |
| `--personal` | one of | Create a personal token with full user access |
| `--team-canonical` | one of | Team canonical for a team-scoped token |
| `--role` | with team | Role cap: `read`, `write`, `maintain`, `admin` |
| `--expires-at` | no | Expiration in RFC3339 format |

`--personal` and `--team-canonical`/`--role` are mutually exclusive.

#### api-tokens list

```bash
pikoci client api-tokens list
```

#### api-tokens delete

```bash
pikoci client api-tokens delete --id 42
```

| Flag | Required | Description |
|------|----------|-------------|
| `--id` | **yes** | ID of the token to delete |

#### api-tokens use

Store an API token locally for subsequent commands (replaces the login JWT).

```bash
pikoci client api-tokens use --token "pko_a1b2c3d4..."
```

| Flag | Required | Description |
|------|----------|-------------|
| `--token` | **yes** | The API token (`pko_...`) |

To switch back to interactive login, run `pikoci client login` again.

### export

Export the full database as a portable SQLite file. Requires admin credentials.

```bash
pikoci client -u localhost:8080 export -o backup.db
```

| Flag | Alias | Required | Description |
|------|-------|----------|-------------|
| `--output` | `-o` | **yes** | Output file path for the SQLite export |

This is also available via the web UI admin dropdown and as a `GET /admin/export` API endpoint.

## run

Run a pipeline job locally without needing a server. Creates an ephemeral in-memory environment, executes the specified job, streams output, and exits with the job's status code.

```bash
pikoci run -p pipeline.hcl -j my-job
```

| Flag | Alias | Default | Required | Description |
|------|-------|---------|----------|-------------|
| `--pipeline-config` | `-p` | | **yes** | Path to the pipeline HCL file |
| `--job` | `-j` | | **yes** | Job name to execute |
| `--var` | | | no | Variable overrides in `key=value` format (repeatable) |
| `--vars` | `-v` | | no | Path to a JSON vars file |
| `--resource` | | | no | Resource overrides in `type.name=path` format (repeatable, e.g. `git.my-repo=./local-dir`) |
| `--log-level` | | `error` | no | Log level: `debug`, `info`, `warn`, `error` |

### Resource overrides

Use `--resource` to skip cloning and point a resource at a local directory instead:

```bash
pikoci run -p pipeline.hcl -j test --resource git.my-repo=./
```

This replaces the `pull` step for that resource with a symlink to the local path, so your task runs against local files.

## pipeline edit

Open the browser-based pipeline editor for a local HCL file. Starts a minimal local HTTP server with the full editor UI (CodeMirror syntax highlighting, live graph preview, block navigation), pre-loaded with the file contents. Changes saved in the editor are written back to disk. No PikoCI server or authentication required.

```bash
pikoci pipeline edit ./pipeline.hcl
```

With a specific port:

```bash
pikoci pipeline edit ./pipeline.hcl --port 8181
```

| Flag | Default | Required | Description |
|------|---------|----------|-------------|
| `--port` | `0` (random) | no | Port to listen on (`0` picks a random available port) |

The server binds to `127.0.0.1` only. Press `Ctrl+C` to stop.

## user-password

Generate a `USERNAME:HASHED_PASSWORD` string for the server's `--users` flag.

```bash
pikoci user-password -u myuser -p mypassword
# Output: myuser:$2a$10$...
```

| Flag | Alias | Required | Description |
|------|-------|----------|-------------|
| `--username` | `-u` | **yes** | Username |
| `--password` | `-p` | **yes** | Plain-text password |

## worker-token

Generate a pre-signed worker authentication token. This avoids distributing the raw JWT secret to worker machines.

```bash
pikoci worker-token --jwt-secret my-secret
# Output: eyJhbG...
```

| Flag | Default | Required | Description |
|------|---------|----------|-------------|
| `--jwt-secret` | | **yes** | JWT secret used by the server |

The server also logs a worker token on startup when `--run-worker=false`.

## server

See [Server Configuration](Server.md).

## worker

See [Running Workers Separately](Workers.md).
