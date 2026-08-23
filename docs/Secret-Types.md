---
description: "Manage secrets in PikoCI pipelines. Built-in Vault and JSON file secret types, plus custom secret providers with masking support."
---

# Secret Types

!!! tip "Looking for the simple case?"
    PikoCI can store secrets itself, encrypted at rest, with no external system
    to run. See [Secrets](Secrets.md). Secret types are for fetching from
    somewhere else, such as Vault or a file on the worker.

A secret type defines how PikoCI fetches secrets from an external system (e.g. Vault, a JSON file). It has one operation: **get** (fetch secret values). Connection config (address, token, etc.) is set on the secret_type block.

## Defining a secret type

```hcl
secret_type "vault" {
  params  = ["path", "address", "token"]
  address = var.vault_address
  token   = var.vault_token

  get "exec" {
    path = "/bin/sh"
    args = ["-ec", "VAULT_ADDR=$param_address VAULT_TOKEN=$param_token vault kv get -format=json $param_path | jq -c '.data.data // .data'"]
  }
}
```

| Field    | Required | Description                                         |
|----------|----------|-----------------------------------------------------|
| `name`   | yes      | Label on the block                                  |
| `source` | no       | URL to fetch the definition from (mutually exclusive with inline `get`) |
| `params` | no       | List of parameter names the get command accepts      |
| `get`    | yes*     | Runner command to fetch the secret values            |
| other    | no       | Config attributes (address, token, etc.) passed as `param_<key>` env vars to the get command |

\* Not required when `source` is set.

### The get operation

**get** must output a JSON object on its last stdout line. Each key-value pair is available for extraction by secret-backed variables. Example output:

```json
{"username": "admin", "password": "s3cret"}
```

### Environment variables

Inside the `get` command, PikoCI exposes:

| Variable            | Description                                  |
|---------------------|----------------------------------------------|
| `$param_<name>`     | Config values from the secret_type block + `$param_path` from the variable's secret block |
| `$WORKDIR`          | Temporary working directory for the job      |

## Sourcing from URL

Instead of defining the `get` command inline, you can point to an external HCL file:

```hcl
secret_type "my-vault" {
  source  = "pikoci://vault"
  address = var.vault_address
  token   = var.vault_token
}
```

Two URL formats are supported:

- **`pikoci://<name>`** resolves to the PikoCI registry. For shipped built-ins (`vault`, `file`), the embedded definition is used directly (no network call).
- **`https://...`** or **`http://...`** fetches HCL from any URL.

When `source` is set, you must not define an inline `get` block. PikoCI will error if both are present. Config attributes (like `address`, `token`) are still set on the block and merged with the resolved definition.

> **Note:** The source is resolved once when the pipeline is created or updated. If the remote HCL file changes, you must re-set the pipeline to pick up the new definition.

You can override the runner used for the `get` command by adding a `runner` block. See [Runners: Type-level runner overrides](Runners.md#type-level-runner-overrides).

## Using secrets via variables

Secrets are consumed through **secret-backed variables**. Declare a variable with a `secret` block referencing a secret type, then use `var.<name>` anywhere in your pipeline:

```hcl
variable "db_password" {
  type = string
  secret "vault" {
    path = "secret/data/db"
    key  = "password"
  }
}

resource "git" "repo" {
  params {
    url   = "https://github.com/example/repo.git"
    token = var.db_password
  }
}

job "deploy" {
  get "git" "repo" {
    trigger = true
  }
  task "migrate" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "DATABASE_PASSWORD=$param_token make migrate"]
    }
  }
}
```

Secret-backed variables are resolved lazily at runtime. Every resource check, get, task, or put execution fetches the latest secret value. This means rotated secrets are picked up automatically without pipeline updates.

### Config chaining

Secret type config fields can reference secret-backed variables. This enables patterns like reading a vault address from a `.env` file, then using it to connect to vault:

```hcl
variable "vault_addr" {
  type = string
  secret "env" {
    key = "VAULT_ADDR"
  }
}

variable "vault_token" {
  type = string  # provided via vars file
}

secret_type "my-vault" {
  source  = "pikoci://vault"
  address = var.vault_addr    # resolved from .env before vault secrets are fetched
  token   = var.vault_token
}

variable "db_pass" {
  type = string
  secret "my-vault" {
    path = "secret/data/db"
    key  = "password"
  }
}
```

Dependencies between secrets are resolved automatically in the correct order.

For more details on secret-backed variables, including precedence rules, override behavior, and secret chaining, see [Variables](Variables.md).

## Built-in: vault

The `vault` secret type is built in. It uses the [Vault CLI](https://developer.hashicorp.com/vault/docs/commands) to fetch secrets from HashiCorp Vault. Requires `vault` and `jq` to be installed on the worker.

```hcl
secret_type "my-vault" {
  source  = "pikoci://vault"
  address = var.vault_address
  token   = var.vault_token
}
```

### Config

| Attribute | Required | Description                                      |
|-----------|----------|--------------------------------------------------|
| `address` | yes      | Vault server address (e.g. `http://vault:8200`)  |
| `token`   | yes      | Vault authentication token                       |

### How it works

The built-in `get` command runs:

```sh
VAULT_ADDR=$param_address VAULT_TOKEN=$param_token vault kv get -format=json "$param_path" | jq -c '.data.data // .data'
```

This handles both KV v1 (`.data`) and KV v2 (`.data.data`) secret engines.

### Vault authentication

There are three ways to provide Vault credentials:

1. **Pipeline variables** (recommended): pass `address` and `token` as pipeline variables, provided via a vars file at pipeline creation time. This keeps secrets out of the HCL.

2. **Worker environment variables**: if `VAULT_ADDR` and `VAULT_TOKEN` are set on the worker, you can define a custom secret_type that omits the address/token params.

3. **Vault agent/AppRole**: run Vault Agent on the worker with auto-auth configured.

### Example

```hcl
variable "vault_address" {
  type    = string
  default = "http://vault:8200"
}

variable "vault_token" {
  type = string
}

secret_type "my-vault" {
  source  = "pikoci://vault"
  address = var.vault_address
  token   = var.vault_token
}

variable "db_password" {
  type = string
  secret "my-vault" {
    path = "secret/data/db"
    key  = "password"
  }
}

job "deploy" {
  get "cron" "timer" { trigger = true }
  task "migrate" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "DATABASE_URL=postgres://admin:${var.db_password}@localhost/app make migrate"]
    }
  }
}
```

Provide the token in your vars file: `{"vault_token": "hvs.CAESI..."}`.

## Built-in: file

The `file` secret type is built in. It reads a file from disk and exposes its keys for extraction by secret-backed variables.

```hcl
secret_type "my-file" {
  source = "pikoci://file"
}
```

### Config

| Attribute | Required | Default | Description                                      |
|-----------|----------|---------|--------------------------------------------------|
| `format`  | no       | auto    | File format: `json`, `yaml`, `env`, or `raw`. When omitted, inferred from the file extension (see below). |
| `path`    | no       |         | Default file path; can be overridden per-variable. Relative paths resolve from the server's working directory. |

### Format auto-detection

When `format` is not explicitly set, it is inferred from the file extension of `path`:

| Extension        | Inferred format |
|------------------|-----------------|
| `.json`          | `json`          |
| `.env`           | `env`           |
| `.yml`, `.yaml`  | `yaml`          |
| anything else    | `raw`           |

An explicit `format` always takes precedence over auto-detection. This is useful for files whose extension doesn't match their content (e.g. a JSON file without a `.json` extension).

The file path can be set on the `secret_type` block as a default, on each variable's `secret` block, or both (the variable-level `path` takes precedence):

```hcl
# Default path on the secret_type — format is inferred from extension
secret_type "db-file" {
  source = "pikoci://file"
  path   = "/run/secrets/db.json"
}

variable "db_user" {
  type = string
  secret "db-file" {
    key = "username"
  }
}

variable "db_password" {
  type = string
  secret "db-file" {
    key = "password"
  }
}

# Override path for a specific variable
variable "api_key" {
  type = string
  secret "db-file" {
    path = "/run/secrets/api.json"
    key  = "key"
  }
}
```

If no default `path` is set on the secret_type, each variable must provide its own:

```hcl
secret_type "my-file" {
  source = "pikoci://file"
}

variable "db_user" {
  type = string
  secret "my-file" {
    path = "/run/secrets/db.json"
    key  = "username"
  }
}
```

### JSON format

When `format` is `"json"` (or inferred from a `.json` extension), the file must contain a JSON object:

```json
{"username": "admin", "password": "s3cret", "host": "db.example.com"}
```

Multi-line JSON is fully supported. The entire file content is parsed as a single JSON object.

### YAML format

When `format` is `"yaml"` (or inferred from a `.yml`/`.yaml` extension), the file must contain a YAML mapping with string values:

```yaml
username: admin
password: s3cret
host: db.example.com
```

### `.env` format

When `format = "env"`, the file is parsed as a `.env` file with `KEY=VALUE` lines:

```hcl
secret_type "env-creds" {
  source = "pikoci://file"
}

variable "db_password" {
  type = string
  secret "env-creds" {
    path = "/run/secrets/db.env"
    key  = "DB_PASSWORD"
  }
}
```

The `.env` file uses one `KEY=VALUE` per line. Comment lines (starting with `#`), blank lines, and any lines not matching a valid variable name are safely ignored. Values may optionally be wrapped in single or double quotes, which are stripped:

```
# Database credentials
DB_HOST=db.example.com
DB_PASSWORD=s3cret
DB_USER="admin"
```

### `raw` format

When `format = "raw"`, the entire file content is returned as a single value under the key `content`. This is useful for files that aren't structured as JSON or key-value pairs, such as PEM certificates, SSH keys, or tokens:

```hcl
secret_type "app-key" {
  source = "pikoci://file"
  path   = "/etc/pikoci/github-app.pem"
}

variable "github_app_key" {
  type = string
  secret "app-key" {
    key = "content"
  }
}
```

The variable receives the full file content as-is. The `.pem` extension is automatically detected as `raw`, so no explicit `format` is needed.

## Example: custom secret type

You can define any secret type with a custom `get` command. The only requirement is that the last line of stdout is a JSON object:

```hcl
secret_type "aws-ssm" {
  params = ["name", "region"]
  region = "us-east-1"

  get "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      VALUE=$(aws ssm get-parameter --name $param_path \
        --region $param_region --with-decryption \
        --query 'Parameter.Value' --output text)
      echo "{\"value\": \"$VALUE\"}"
      EOT
    ]
  }
}

variable "api_key" {
  type = string
  secret "aws-ssm" {
    path = "/prod/api-key"
    key  = "value"
  }
}

job "deploy" {
  get "cron" "timer" { trigger = true }
  task "use-key" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "curl -H 'Authorization: ${var.api_key}' https://api.example.com"]
    }
  }
}
```
