---
description: "Parameterize PikoCI pipelines with variables and secrets. Pass values at trigger time or from external secret providers."
---

# Variables and Secrets

Pipeline variables let you parameterize your pipeline configuration.

## Declaring variables

```hcl
variable "repo_url" {
  type    = string
  default = "https://github.com/PikoCI/pikoci.git"
}

variable "repo_name" {
  type = string
}
```

| Field     | Required | Description                            |
|-----------|----------|----------------------------------------|
| `type`    | yes      | Variable type (`string`, `number`, or `bool`) |
| `default` | no       | Default value if not provided          |
| `secret`  | no       | Secret block for lazy resolution       |

## Providing values

Variables without a default (and without a `secret` block) must be set via a JSON vars file:

```json
{
  "repo_name": "pikoci"
}
```

Pass the file when creating or updating a pipeline:

```bash
# Via CLI
pikoci client pipelines create -n my-pipeline -c pipeline.hcl -v vars.json

# Or override individual variables
pikoci client pipelines create -n my-pipeline -c pipeline.hcl --var repo_name=pikoci

# At server startup
pikoci server --pipeline-name my-pipeline --pipeline-config pipeline.hcl --vars vars.json
```

## Using variables

Reference variables with `var.<name>` or string interpolation `${var.<name>}`:

```hcl
resource "git" "repo" {
  params {
    url  = var.repo_url
    name = "${var.repo_name}"
  }
}

task "build" {
  run "exec" {
    path = "/bin/sh"
    args = ["-ec", "cd ${var.repo_name} && make build"]
  }
}
```

## Secret-backed variables

Variables can declare a `secret` block to resolve their value from a secret type at runtime. This lets secrets work everywhere variables work, including resource params and task args, without hardcoding sensitive values.

```hcl
variable "git_token" {
  type = string
  secret "vault" {
    path = "secret/data/github"
    key  = "token"
  }
}

resource "git" "repo" {
  params {
    url   = "https://github.com/PikoCI/pikoci.git"
    token = var.git_token  # resolved lazily from vault at runtime
  }
}
```

The `secret` block label is the name of a `secret_type` defined in the pipeline. The block has two fields:

| Field  | Required | Description                                       |
|--------|----------|---------------------------------------------------|
| `path` | no       | Path to fetch from the secret backend              |
| `key`  | yes      | Key to extract from the JSON response              |

### How it works

At **parse time**, the variable value is set to a placeholder string. The pipeline is decoded normally with placeholders flowing into wherever `var.git_token` is used.

At **runtime** (every resource check, get, task, or put execution), the worker resolves the placeholder by fetching the actual secret value. This means rotated secrets are picked up automatically without pipeline updates.

### Precedence

```
vars file (--vars) > secret block > default
```

- If the variable is provided via vars file → use it, skip secret resolution
- If no vars file override and has secret block → placeholder at parse time, resolve at runtime
- If no vars file override, no secret block, has default → use default
- None of the above → error

This lets you override secrets with plaintext for local development:

```json
{
  "git_token": "my-dev-token"
}
```

### Secret chaining

The `path` and `key` fields in a `secret` block can reference other variables using `var.<name>` or string interpolation `${var.<name>}`. This enables patterns where one secret's location is itself stored in another secret:

```hcl
# Step 1: Read the key file path from a .env file
variable "key_path" {
  type = string
  secret "env" {
    key = "GITHUB_APP_KEY_FILE"
  }
}

# Step 2: Read the key content from the file at that path
variable "key_content" {
  type = string
  secret "file" {
    path = var.key_path
    key  = "content"
  }
}

# String interpolation also works
variable "nested_secret" {
  type = string
  secret "file" {
    path = "${var.key_path}/subdir"
    key  = "content"
  }
}

# Key chaining — the key name itself comes from another variable
variable "key_name" {
  type = string
  secret "env" {
    key = "SECRET_KEY_NAME"
  }
}

variable "dynamic_secret" {
  type = string
  secret "vault" {
    path = "secret/data/app"
    key  = var.key_name
  }
}
```

Dependencies are resolved automatically in the correct order. You don't need to worry about declaration order. Circular dependencies are detected with a clear error message. The maximum chain depth is 10 levels.

### Full example

```hcl
variable "vault_token" {
  type    = string
  default = "my-root-token"
}

secret_type "vault" {
  source  = "pikoci://vault"
  address = "http://vault:8200"
  token   = var.vault_token
}

variable "git_token" {
  type = string
  secret "vault" {
    path = "secret/data/github"
    key  = "token"
  }
}

resource "git" "app" {
  check_interval = "@every 1m"
  params {
    url   = "https://github.com/PikoCI/pikoci.git"
    token = var.git_token
  }
}

job "build" {
  get "git" "app" {
    trigger = true
  }
  task "compile" {
    run "exec" {
      path = "make"
      args = ["build"]
    }
  }
}
```
