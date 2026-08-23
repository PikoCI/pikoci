---
description: "Store secrets directly in PikoCI, encrypted at rest. Manage them over the API or CLI with team and pipeline scopes, and reference them from pipelines."
---

# Secrets

PikoCI can store secret values itself, encrypted at rest, so credentials never
have to live on a worker filesystem or in an external vault. Secrets are managed
over the API or the CLI, and referenced from a pipeline through the built-in
`pikoci` secret type.

This is the simplest option for most pipelines. For fetching secrets from
HashiCorp Vault or a file on the worker, see [Secret Types](Secret-Types.md).

## Enabling the store

Set a master key on the server:

```bash
pikoci server --secret-key "$(openssl rand -base64 32)"
# or
export PIKOCI_SECRET_KEY="..."
```

The key is **optional**. Without it the server starts and runs exactly as
before; only secret storage is unavailable, and attempts to store or read a
secret report:

```text
secret storage is not configured: set PIKOCI_SECRET_KEY (or --secret-key) to store and read secrets
```

On first use PikoCI generates an [age](https://age-encryption.org) keypair,
encrypts the private half under your master key, and stores it in the database.
Secret values are encrypted to the public half.

!!! warning "Losing the master key means losing every secret"
    There is no recovery path. If the key changes, PikoCI refuses to start using
    the store rather than silently generating a new identity, because doing so
    would orphan every value already stored. Back the key up somewhere other
    than the database.

## Storing a secret

`--team-canonical` is always required; `--pipeline` narrows the scope.

```bash
# Team-wide: every pipeline in the team can read it
pikoci client secrets set GITHUB_TOKEN --team-canonical main

# Pipeline-scoped
pikoci client secrets set DATABASE_URL --team-canonical main --pipeline api
```

The CLI prompts for the value so it does not land in shell history:

```text
GITHUB_TOKEN: ********
secret "GITHUB_TOKEN" stored
```

For scripting, pipe it in rather than passing `--value`, which is visible in the
process list:

```bash
echo -n "$TOKEN" | pikoci client secrets set GITHUB_TOKEN --team-canonical main --stdin
```

## Listing and deleting

```bash
pikoci client secrets list --team-canonical main
pikoci client secrets delete GITHUB_TOKEN --team-canonical main
```

Listing returns names and timestamps only. **No endpoint ever returns a secret
value** — there is deliberately no reveal API, and decrypted values go only to
workers running a build.

Listing and deleting do not need the master key, so secrets can still be
inspected and cleaned up if the key is lost.

## Using a secret in a pipeline

Reference it with the built-in `pikoci` secret type. No `secret_type` block is
needed:

```hcl
variable "github_token" {
  type = string
  secret "pikoci" {
    key = "GITHUB_TOKEN"
  }
}

job "deploy" {
  task "push" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "GH_TOKEN=${var.github_token} make release"]
    }
  }
}
```

Values are resolved at runtime, so rotating a secret takes effect on the next
build with no pipeline change. Values are also masked as `***` in build logs,
like any other secret.

Only the secrets a pipeline actually names are ever sent to a worker, so one
build cannot enumerate everything the team has stored.

## Scope and precedence

A pipeline secret shadows a team secret of the same name:

```text
Team "main":
  GITHUB_TOKEN = team value
  NPM_TOKEN    = shared value

Pipeline "api":
  GITHUB_TOKEN = pipeline value

Resolved for "api":
  GITHUB_TOKEN = pipeline value    <- pipeline wins
  NPM_TOKEN    = shared value      <- inherited from the team
```

Secret names must look like environment variables: letters, digits and
underscores, not starting with a digit. Unlike other PikoCI entities they are
not slugified, so `GITHUB_TOKEN` stays exactly that.

## Permissions

| Operation | Required role |
|-----------|---------------|
| Set a secret | `maintain` |
| Delete a secret | `maintain` |
| List secret names | `read` |

Creating and deleting secrets is recorded in the [audit log](Audit-Log.md) as
`secret.created` and `secret.deleted`, by name only — never the value.

## Workers need a team-scoped token

A worker fetches decrypted values from the server at build time, so it must
prove which team it belongs to. **Store-backed secrets require a team-scoped
worker token**; an unscoped global worker token is rejected:

```bash
pikoci client teams worker-token --team-canonical main
```

See [Workers](Workers.md) for registering a worker with that token.

## Local runs

`pikoci run` executes against a throwaway in-memory database with no server, so
store-backed secrets are not available. Supply values with a vars file instead —
a vars file already takes precedence over a `secret` block:

```bash
pikoci run pipeline.hcl --vars local.json
```

## What the encryption protects

With the master key supplied from the environment, the database file alone is
useless to an attacker: leaked backups, table dumps, and casual database access
all yield only ciphertext.

It is not a substitute for a KMS or HSM. Anyone holding both the database and
the master key holds every secret, and the server necessarily decrypts values in
memory to hand them to workers.
