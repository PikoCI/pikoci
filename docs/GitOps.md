---
description: Manage PikoCI pipelines with GitOps. Keep the pipeline definition in git, apply it automatically with a set-pipeline job, and reconcile drift on a schedule.
---

# GitOps: managing pipelines from git

PikoCI has no `set_pipeline` step. What it has instead is a declarative apply
(`pikoci client pipelines update`) that reconciles the whole pipeline, and a job
model expressive enough to call that apply on itself. Put the two together and
the pipeline manages its own definition from git.

This page drafts that setup against this repository's own
[`deploy/pipeline.hcl`](https://github.com/PikoCI/pikoci/blob/master/deploy/pipeline.hcl).

## Why the apply is a reconcile

`UpdatePipeline` diffs the incoming HCL against what is stored and creates,
updates **and deletes** jobs, resources, resource types, runners, secret types
and notification types. Delete a `job` block from the file and the next apply
deletes the job. The file is the desired state, not a patch — which is the
property GitOps needs.

Two things it deliberately does *not* touch:

- `paused` on a job or pipeline
- a resource's pinned version and webhook token

So an apply will not un-pause a job you paused during an incident, and will not
unpin a resource. Those stay imperative, and consequently they are drift that
git cannot describe.

## Blocks to add to `deploy/pipeline.hcl`

The pipeline already defines everything the reconcile job needs except a clock,
a token, and the job itself. The existing `git` resource `pikoci_master`, the
`env` secret type and `var.pikoci_domain` are reused as-is.

```hcl
# --- Jobs: GitOps ---

job "set-pipeline" {
  concurrency = 1

  # Either input triggers the job: a commit to master, or the cron tick.
  get "git"  "pikoci_master"  { trigger = true }
  get "cron" "reconcile_tick" { trigger = true }

  task "validate" {
    run "shell" {
      cmd = "cd ${var.git_name} && pikoci pipeline validate deploy/pipeline.hcl"
    }
  }

  task "apply" {
    run "shell" {
      cmd = <<-EOT
        cd ${var.git_name}
        pikoci client pipelines update \
          --url "https://${var.pikoci_domain}" \
          --team-canonical main \
          --name pikoci \
          --config deploy/pipeline.hcl
      EOT
    }
  }
}

# --- Resources ---

# Drives periodic reconciliation so out-of-band edits are corrected.
resource "cron" "reconcile_tick" {
  check_interval = "@every 30m"
}
```

`cron` is a built-in resource type, so no `resource_type "cron"` block is
needed. Note that no variable holds the API token — see
[Why the token is not in the pipeline](#why-the-token-is-not-in-the-pipeline).

### Why two triggers

They cover the two halves of GitOps, and only the second is real reconciliation:

| Input | Fires when | Gives you |
|---|---|---|
| `pikoci_master` | a commit lands on master | commit-driven apply |
| `reconcile_tick` | every 30m | drift correction |

Each `get` resolves its own resource's latest version, so a cron-triggered build
still applies the current master `deploy/pipeline.hcl`. Without the cron input,
an edit made in the web UI would survive until the next commit.

## Bootstrap

The pipeline cannot install itself, so the first apply is manual.

1. Create a team-scoped token capped at `maintain` — the role
   `UpdatePipeline` requires:

    ```bash
    pikoci client api-tokens create \
      --url https://<domain> \
      --name gitops-set-pipeline \
      --team-canonical main \
      --role maintain
    ```

2. Store it once on the server host, as the user the worker runs as. The CLI
   writes it to `$XDG_CONFIG_HOME/pikoci/authentication` with mode `0600`, and
   every later `pikoci client` call reads it from there:

    ```bash
    pikoci client api-tokens use --url https://<domain> --token pko_...
    ```

    (`--url` is required by the flag parser even though this command makes no
    network call.)

3. Apply once by hand:

    ```bash
    pikoci client pipelines update \
      --url https://<domain> \
      --team-canonical main \
      --name pikoci \
      --config deploy/pipeline.hcl
    ```

From here the `set-pipeline` job owns the pipeline.

## Why the token is not in the pipeline

The obvious way to authenticate the apply is a secret-backed variable passed as
`--jwt "${var.pikoci_api_token}"`. Do not do that.

The `shell` runner is defined as `path = "$shell"`, `args = ["-ec", "$cmd"]`,
and the worker resolves `$cmd` with `os.Expand` before building the argv. The
fully interpolated command — token included — therefore becomes `argv[2]` of
`/bin/sh` and is visible in `ps` to any local user for as long as the task runs.
Build logs are masked; the process table is not.

Storing the token with `api-tokens use` avoids this entirely: the credential
lives in one `0600` file, never appears in the pipeline definition, never
reaches git, and never reaches argv. The trade-off is that it becomes host
state rather than something the config declares, and it does not travel to a
containerised or remote worker. For those, fall back to a secret-backed
variable and accept the argv exposure — or run the apply on a host worker.

This is a pre-existing property of the whole pipeline, not something the
reconcile job introduces: `var.ghcr_token`, `var.codecov_token` and
`var.github_token` are already interpolated into `cmd` strings the same way. A
`maintain`-capped pipeline-write token is simply a more valuable credential
than those, which is why it is worth handling differently.

## Secrets are not needed to validate or apply

Neither task needs a secret *value*. `ReadPipeline` substitutes a placeholder
for every variable backed by a `secret` block and resolves it at build time on
the worker, so:

- `pikoci pipeline validate` runs offline, with no credentials — it is safe as a
  pull-request check, including on forks.
- The apply ships the raw HCL to the server, which stores it with the
  placeholders intact.

## Pre-merge checks

Add to the existing `backend` job, or to a GitHub PR check:

```bash
pikoci pipeline validate deploy/pipeline.hcl
```

`pikoci client pipelines graph` renders the pipeline from a config file without
applying it. Posting that graph on a pull request is the closest thing to a diff
preview.

Every apply is recorded in the audit log as `pipeline.updated`, attributed to
the token's user — so `pikoci client audit list` becomes the record of what git
changed and when.

## Known limitations

Understand these before adopting the pattern.

**Deletes take build history with them.** Removing a job — or a `for_each` key —
deletes the job instance *and its builds*. `git revert` restores the definition,
not the history. A careless merge is unrecoverable; this is the sharpest edge in
the model.

**There is no dry run.** Nothing reports what an apply would change before it
changes it. The graph render is a visual approximation only.

**No path filtering on the git resource.** Its params are `url`, `branch`,
`name`, `token`, `pr`, `tag`, `provider` — no path scoping. Every master commit
re-triggers `set-pipeline`, even commits that do not touch
`deploy/pipeline.hcl`. The apply is idempotent, so this is build noise rather
than a correctness problem, but it is noise: at `@every 30m` the cron input
alone adds ~48 builds a day. Lengthen the interval if the build list matters
more than reconciliation latency.

**The job rewrites the pipeline it lives in.** Jobs update in place and in-flight
builds are unaffected, but renaming or removing `set-pipeline` itself in the same
commit means editing the thing that is mid-apply. Keep the job's name stable.

**One pipeline per apply.** There is no multi-pipeline manifest and nothing
prunes a pipeline whose file you deleted. Managing N pipelines from one repo
means N applies, each named explicitly.
