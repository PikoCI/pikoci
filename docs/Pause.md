# Pause / Unpause

Pipelines and individual jobs can be paused to temporarily stop build triggering without deleting configuration or history.

## Pipeline Pause

Pausing a pipeline **pauses all its jobs**. Each job's paused flag is set individually, so they appear blue in the pipeline graph. Resource checks **continue running** and new versions are recorded, but no builds are created. This prevents a flood of builds when the pipeline is unpaused — only versions discovered after unpausing trigger builds.

In-flight builds (already started) continue to completion.

## Job Pause

Pausing a job blocks build triggering for that specific job only. Resource checks and other jobs in the pipeline are unaffected. New versions of resources used by the paused job are still tracked.

## Unpause Pipeline

Unpausing a pipeline unpauses the pipeline **and all its jobs**.

## Unpause Job

Unpauses only that specific job. You can unpause individual jobs while the pipeline itself remains paused — this allows selectively re-enabling jobs.

## Visual Indicator

Paused jobs appear blue (`#29ADFF`) in the pipeline graph.

## API

```
POST /teams/{tc}/pipelines/{pc}/pause
POST /teams/{tc}/pipelines/{pc}/unpause
POST /teams/{tc}/pipelines/{pc}/jobs/{jn}/pause
POST /teams/{tc}/pipelines/{pc}/jobs/{jn}/unpause
```

## CLI

```bash
# Pause/unpause a pipeline
pikoci client pipelines pause -u <url> --team-canonical <tc> -n <name>
pikoci client pipelines unpause -u <url> --team-canonical <tc> -n <name>
```
