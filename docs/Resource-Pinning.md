# Resource Pinning

Resources can be pinned to a specific version to prevent the scheduler from advancing past it. This is useful for locking a pipeline to a known-good version while investigating issues or coordinating deployments.

## Pin

Pinning a resource sets a `pinned_version_id` on the resource. While pinned:

- **Resource checks continue running** — new versions are still discovered and recorded.
- **No jobs are triggered for mismatched versions** — when a new version is discovered by a resource check, the worker only triggers immediate downstream jobs (get steps with `trigger: true` and no `passed` constraints) if the version matches the pinned version. Newer versions are ignored.
- **The scheduler skips mismatched versions** — downstream jobs with `passed` constraints only trigger if the ready version matches the pinned version.
- **Existing pending builds with a different version are cancelled** — when a pin is applied, any pending builds for jobs that use this resource are cancelled if their version doesn't match the pinned version. This prevents stale versions from running after the pin takes effect.
- **Pinned resources are visually distinct** — in the pipeline graph, pinned resources have a bold amber border to indicate they are locked.
- **Manual triggers are unaffected** — you can still trigger a job manually or use "Trigger with version" on any version.

## Unpin

Unpinning removes the lock. The scheduler resumes normal behavior and will trigger downstream jobs with the latest ready version on the next tick.

## Trigger with Version

Independently of pinning, you can trigger downstream jobs with a specific version using the play button on the resource versions page. This creates pending builds for all immediate downstream jobs (those with get steps that have no `passed` constraints) using the chosen version. Further downstream jobs cascade naturally via the scheduler.

## UI

On the resource versions page, each version row has two action buttons:

- **Play** (`▶`) — triggers immediate downstream jobs with that version.
- **Pin** (`📌`) — toggles the pin on/off. The pinned version shows a "pinned" badge.

## API

```
POST /teams/{tc}/pipelines/{pc}/resources/{rc}/pin          — pin to a version (body: {"version_id": 42})
POST /teams/{tc}/pipelines/{pc}/resources/{rc}/unpin         — remove pin
POST /teams/{tc}/pipelines/{pc}/resources/{rc}/versions/{id}/trigger — trigger downstream jobs with version
```

All three endpoints require team member authorization.
