---
description: "Store configuration and secrets directly in PikoCI. Secrets are encrypted at rest and masked in logs; plain values are shown in the clear. Managed over the API or CLI."
---

# Configuration and Secrets

PikoCI can store configuration itself, so credentials never have to live on a
worker filesystem or in an external vault. Entries are managed over the API or
the CLI and referenced from a pipeline through the built-in `pikoci` secret
type.

There are two kinds:

| | `secret` | `plain` |
|---|---|---|
| Encrypted at rest | yes | no |
| Masked in build logs | yes | no |
| Value shown by `list` | never | yes |
| Needs a master key | yes | **no** |

Use `plain` for things like a log level or a registry URL, and `secret` for
credentials. The kind is fixed when the entry is created; to change it, delete
the entry and create it again.

This is the simplest option for most pipelines. For fetching secrets from
HashiCorp Vault or a file on the worker, see [Secret Types](Secret-Types.md).

## Enabling secrets

Plain entries work out of the box and need no configuration. Secrets need a
master key on the server:

```bash
pikoci server --secret-key "$(openssl rand -base64 32)"
# or
export PIKOCI_SECRET_KEY="..."
```

The key is **optional**. Without it the server starts and runs exactly as
before, plain entries work normally, and only secrets are unavailable:

```text
secret storage is not configured: set PIKOCI_SECRET_KEY (or --secret-key) to store and read secrets
```

An entry stored with no `kind` is treated as a secret, so forgetting the flag
fails loudly rather than writing a credential to the database in the clear.

On first use PikoCI generates an [age](https://age-encryption.org) keypair,
encrypts the private half under your master key, and stores it in the database.
Secret values are encrypted to the public half.

!!! warning "Losing the master key means losing every secret"
    There is no recovery path. If the key changes, secret operations fail with
    an explicit error rather than silently generating a new identity, because
    doing so would orphan every value already stored. Back the key up somewhere
    other than the database.

## Storing values

`--team-canonical` is always required; `--pipeline` narrows the scope.

```bash
# Plain value, given inline
pikoci client config set LOG_LEVEL debug --team-canonical main

# Secret: prompted, so it does not land in shell history
pikoci client config set GITHUB_TOKEN --secret --team-canonical main

# Pipeline-scoped
pikoci client config set DATABASE_URL --secret --team-canonical main --pipeline api
```

```text
GITHUB_TOKEN: ********
secret "GITHUB_TOKEN" stored
```

Passing a secret as a command-line argument is refused, because it would be
visible in shell history and the process list. For scripting, pipe it in:

```bash
echo -n "$TOKEN" | pikoci client config set GITHUB_TOKEN --secret --team-canonical main --stdin
```

## Listing and deleting

```bash
pikoci client config list --team-canonical main
pikoci client config delete GITHUB_TOKEN --team-canonical main
```

Listing shows plain values in full and secret entries as metadata only. **No
endpoint ever returns a secret value** — there is deliberately no reveal API,
and decrypted values go only to workers running a build.

Listing and deleting do not need the master key, so entries can still be
inspected and cleaned up if the key is lost.

## Using a value in a pipeline

Reference it with the built-in `pikoci` secret type — the same block for both
kinds, since a pipeline should not care where a value comes from. No
`secret_type` block is needed:

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

Values are resolved at runtime, so changing one takes effect on the next build
with no pipeline change. Secret values are masked as `***` in build logs; plain
values are printed in full, which is the point of the kind.

Only the entries a pipeline actually names are ever sent to a worker, so one
build cannot enumerate everything the team has stored.

## Scope and precedence

A pipeline entry shadows a team entry of the same name, regardless of kind:

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

Names must look like environment variables: letters, digits and
underscores, not starting with a digit. Unlike other PikoCI entities they are
not slugified, so `GITHUB_TOKEN` stays exactly that.

## Permissions

| Operation | Required role |
|-----------|---------------|
| Set an entry | `maintain` |
| Delete an entry | `maintain` |
| List entries | `read` |

Note that `read` is enough to see **plain values in full**, since they are
non-sensitive by definition. Secret values are never returned at any role.

Writes are recorded in the [audit log](Audit-Log.md) as `secret.created` /
`secret.deleted` and `config.created` / `config.deleted`, by name only — never
the value.

## Workers need a team-scoped token

A worker fetches resolved values from the server at build time, so it must
prove which team it belongs to. **Store-backed values require a team-scoped
worker token**; an unscoped global worker token is rejected:

```bash
pikoci client teams worker-token --team-canonical main
```

See [Workers](Workers.md) for registering a worker with that token.

## Local runs

`pikoci run` executes against a throwaway in-memory database with no server, so
store-backed values are not available. Supply values with a vars file instead —
a vars file already takes precedence over a `secret` block:

```bash
pikoci run pipeline.hcl --vars local.json
```

## What the encryption protects

This applies to `secret` entries only; plain entries are stored in the clear by
design.

With the master key supplied from the environment, the database file alone is
useless to an attacker: leaked backups, table dumps, and casual database access
all yield only ciphertext.

It is not a substitute for a KMS or HSM. Anyone holding both the database and
the master key holds every secret, and the server necessarily decrypts values in
memory to hand them to workers.
