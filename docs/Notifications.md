# Notifications

Notifications are fire-and-forget messages sent during or after job execution. Unlike resources, notifications have no versioning, no check intervals, and no pull lifecycle. They are ideal for sending status updates to external services like GitHub Checks, Slack, or Discord.

## Notification Types

A `notification_type` defines **how** to send a notification. It specifies the runner command used to deliver the message.

```hcl
notification_type "slack" {
  params = ["webhook_url"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "curl -sf -X POST -H 'Content-Type: application/json' \"$param_webhook_url\" -d '{\"text\": \"'\"$NOTIFY_MESSAGE\"'\"}'"]
  }
}
```

Notification types can also be loaded from a remote source:

```hcl
notification_type "github-check" {
  source = "pikoci://github-check"
}
```

### Parameters

- `params` - List of parameter names that notifications of this type can provide.
- `notify` - A runner command block that executes the notification logic.

## Notifications

A `notification` defines **what** to send and **where**. It references a notification type and provides concrete parameter values.

A notification without an `on` field is **manual-only** — it will never fire automatically. You must explicitly use `notify` steps in job plans or hooks to trigger it. This is useful for notifications like `github-check` where you need precise control over when each notification is sent (e.g., `status = "in_progress"` at the start, `conclusion = "success"` at the end).

```hcl
notification "slack" "deploys" {
  params {
    webhook_url = "https://hooks.slack.com/services/T00/B00/xxx"
  }
  message = "Deploy finished for ${var.app_name}"
}
```

### Fields

| Field | Description |
|-------|-------------|
| `type` | The notification type name (first label) |
| `name` | A unique name for this notification (second label) |
| `params` | Key-value parameters passed to the notification type |
| `message` | Default message text (available as `$NOTIFY_MESSAGE`). Supports HCL variables (`var.*`). |
| `on` | List of events for automatic dispatch: `success`, `failure`, `cancel`, `all`. **If omitted, the notification is manual-only.** |
| `jobs` | Limit automatic dispatch to these job names only (requires `on`) |
| `exclude` | Exclude these job names from automatic dispatch (requires `on`) |

## Using Notify

Use `notify` steps directly in a job plan or hooks to send notifications at specific points during execution. This is the only way to trigger notifications that don't have an `on` field.

```hcl
notification_type "github-check" {
  source = "pikoci://github-check"
}

notification "github-check" "ci" {
  params {
    app_id          = var.github_app_id
    installation_id = var.github_app_installation_id
    private_key     = var.github_app_pem
    repository      = "org/repo"
    base_url        = "https://ci.example.com"
  }
}

job "deploy" {
  get "git" "repo" { trigger = true }

  notify "github-check" "ci" { status = "in_progress" }

  task "build" {
    run "exec" {
      path = "make"
      args = ["build"]
    }
  }

  on_success {
    notify "github-check" "ci" { conclusion = "success" }
  }
  on_failure {
    notify "github-check" "ci" { conclusion = "failure" }
  }
}
```

In this example, `notification "github-check" "ci"` has no `on` field, so it only fires when explicitly called via `notify` steps in the plan and hooks.

### Notify Step Parameters

Attributes on a `notify` step are passed to the notification type's command with a `notify_` prefix. For example, `status = "in_progress"` becomes the environment variable `$notify_status`.

The special `message` attribute overrides the notification's default message.

Notify steps work in all hook types (`on_success`, `on_failure`, `on_cancel`, `ensure`) and at the top level of a job plan.

## Automatic Notifications

The `on` field enables automatic notification dispatch after job hooks complete, based on the final build status:

```hcl
notification "slack" "all-builds" {
  params {
    webhook_url = var.slack_webhook
  }
  on = ["success", "failure"]
  message = "Build finished"
}
```

### Event Mapping

| Build Status | Event Name |
|-------------|------------|
| Succeeded | `success` |
| Failed | `failure` |
| Cancelled | `cancel` |

Use `on = ["all"]` to match every event. `all` cannot be combined with other events.

### Job Scoping

By default, automatic notifications fire for every job. Use `jobs` or `exclude` (mutually exclusive) to limit scope:

```hcl
notification "slack" "deploy-only" {
  params { webhook_url = var.slack_webhook }
  on = ["success", "failure"]
  jobs = ["deploy", "release"]
}

notification "slack" "skip-lint" {
  params { webhook_url = var.slack_webhook }
  on = ["failure"]
  exclude = ["lint"]
}
```

## Built-in Notification Types

### github-check

Creates and updates GitHub Check Runs via the GitHub App API.

**Parameters:** `app_id`, `installation_id`, `private_key`, `repository`, `base_url`

**Notify params:** `status` (set to `in_progress` to create), `conclusion` (set to `success`/`failure` to complete)

```hcl
notification_type "github-check" {
  source = "pikoci://github-check"
}

notification "github-check" "ci" {
  params {
    app_id          = var.github_app_id
    installation_id = var.github_app_installation_id
    private_key     = var.github_app_pem
    repository      = "org/repo"
    base_url        = "https://ci.example.com"
  }
}
```

### slack

Posts messages to a Slack incoming webhook.

**Parameters:** `webhook_url`

```hcl
notification_type "slack" {
  source = "pikoci://slack"
}

notification "slack" "builds" {
  params {
    webhook_url = var.slack_webhook
  }
  on = ["failure"]
  message = "Build failed!"
}
```

### discord

Posts messages to a Discord webhook.

**Parameters:** `webhook_url`

```hcl
notification_type "discord" {
  source = "pikoci://discord"
}

notification "discord" "builds" {
  params {
    webhook_url = var.discord_webhook
  }
  on = ["failure"]
  message = "Build failed!"
}
```

## Message Resolution

The message sent to the notification type is resolved in this order:

1. `message` attribute on the `notify` step (highest priority)
2. `message` field on the `notification` definition
3. Empty — `$NOTIFY_MESSAGE` is not set

When `$NOTIFY_MESSAGE` is empty, the notification type script is responsible for providing a default. The built-in `slack` and `discord` types default to:

```
[my-pipeline/deploy] Build #42 - success
```

When triggered by automatic notifications (via the `on` field), the build status is included. For manual `notify` steps without a status, the default is:

```
[my-pipeline/deploy] Build #42
```

These are constructed from build metadata variables (`$BUILD_PIPELINE_NAME`, `$BUILD_JOB_NAME`, `$BUILD_NUMBER`) and the `$notify_build_status` variable (automatically set by automatic notifications).

The `message` field in HCL supports pipeline variables (`${var.app_name}`), which are resolved at pipeline parse time. Runtime build metadata (`$BUILD_NUMBER`, etc.) is **not** available in the HCL `message` field — it is available as separate environment variables in the notification type script, which can use them to construct or enrich the message.

The resolved message is available as the `$NOTIFY_MESSAGE` environment variable.

## Environment Variables

Notify commands receive all of the following as environment variables:

- `$param_*` — Notification-level parameters (from the `notification` block's `params`)
- `$notify_*` — Step-level parameters (from the `notify` step's attributes)
- `$NOTIFY_MESSAGE` — Resolved message text (may be empty)
- `$BUILD_NUMBER` — Current build number
- `$BUILD_JOB_NAME` — Job name
- `$BUILD_PIPELINE_NAME` — Pipeline canonical name
- `$BUILD_TEAM_NAME` — Team canonical name
- `$WORKDIR` — Working directory (shared with get/task steps)

These environment variables are available inside the notification type's `notify` command script. For example, you can reference `$BUILD_NUMBER` in your script even if it wasn't part of `$NOTIFY_MESSAGE`.

## Migration from Resource-Based Notifications

If you previously used `resource_type`/`resource`/`put` for push-only resources like `github-check`, migrate to the notification system:

| Before | After |
|--------|-------|
| `resource_type "github-check" { source = "pikoci://github-check" }` | `notification_type "github-check" { source = "pikoci://github-check" }` |
| `resource "github-check" "ci" { params { ... } }` | `notification "github-check" "ci" { params { ... } }` |
| `put "github-check" "ci" { status = "in_progress" }` | `notify "github-check" "ci" { status = "in_progress" }` |

The `$put_*` environment variable prefix becomes `$notify_*` in notification type scripts.
