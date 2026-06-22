# Retry Build Version Pinning

## Problem

When retrying a build via the "Retry" button, the retried build fetches the latest resource version instead of reusing the same versions from the original build. This means a retry of a git-triggered build could pick up newer commits, defeating the purpose of retrying the exact same build.

## Root Cause

The infrastructure for version pinning on retries already exists:
- `workitem.Body` has `RetryBuildNumber` and `RetryBuildID` fields
- The worker checks these fields and calls `FindBuildGetVersions` to reuse versions
- The gRPC proto and server pass these fields through
- Worker tests already cover the version-pinning path

But two things are missing:
1. `RetryJobBuild` does not persist *which build to copy versions from* when creating the retry
2. `NextWork` does not populate `RetryBuildNumber`/`RetryBuildID` on the work item

## Design

### New DB column

Add `retry_source_build_id` (nullable integer) to the `builds` table. For non-retry builds this is NULL/0. For retry builds, it stores the database ID of the *parent* build (always the original, e.g., build "3", not "3.1").

### Migration

New file `V38RetrySourceBuildID.go`:
```sql
ALTER TABLE builds ADD COLUMN retry_source_build_id INTEGER NOT NULL DEFAULT 0;
```
Register as index 38 in the `Migrations` array (change size from `[38]` to `[39]`).

### Changes

#### 1. `pikoci/build/build.go` - Add field to Build struct

Add `RetrySourceBuildID uint32` field to the `Build` struct.

#### 2. `pikoci/mysql/build.go` - DB layer changes

- Add `RetrySourceBuildID sql.NullInt64` to `dbBuild` struct
- Update `newDBBuild` to include `RetrySourceBuildID`
- Update `toDomainEntity` to read `RetrySourceBuildID`
- Update `scanBuild` to scan the new column
- Update all SELECT queries that use `scanBuild` to include `b.retry_source_build_id`
- Update `CreateRetry` INSERT to include `retry_source_build_id`
- Update `Create` INSERT to include `retry_source_build_id` (value 0)

Queries affected (all that SELECT build columns and use `scanBuild`):
- `Find` (line ~223)
- `Filter` (line ~260)
- `FindOldestPending` (line ~534)
- `FindByID` (line ~507)
- `Create` INSERT (line ~83)
- `CreateRetry` INSERT (line ~169)

#### 3. `pikoci/builds.go` - Service layer

In `RetryJobBuild` (line 216):
- After extracting `parentBN`, find the parent build: `parentBuild, err := q.Builds.Find(ctx, tc, pc, jn, parentBN)`
- Set `RetrySourceBuildID: parentBuild.ID` on the build passed to `CreateRetryJobBuild`

In `CreateRetryJobBuild` (line 253):
- The `RetrySourceBuildID` is already on the `build.Build` struct, so it flows through to `CreateRetry` naturally.

#### 4. `pikoci/work.go` - Work dispatch

In `NextWork` (line 59), after getting the `started` build:
- If `started.RetrySourceBuildID != 0`, populate `RetryBuildID` and `RetryBuildNumber` on the work item:

```go
return &workitem.Item{
    Type: "job",
    Body: workitem.Body{
        TeamCanonical:     pwt.Team.Canonical,
        PipelineCanonical: pwt.Canonical,
        JobName:           j.Name,
        BuildID:           started.ID,
        BuildNumber:       started.BuildNumber,
        VersionID:         started.VersionID,
        RetryBuildID:      started.RetrySourceBuildID,
        RetryBuildNumber:  extractParentBuildNumber(started.BuildNumber),
    },
}, nil
```

Extract the parent build number helper (or inline): split on "." and take the first part.

#### 5. Tests

- `pikoci/builds_test.go`: Update `RetryJobBuild` test to verify `RetrySourceBuildID` is set on the created build via mock expectations
- `pikoci/work_test.go` or relevant test file: Add/update test for `NextWork` to verify retry fields are populated when dispatching a retry build
- Existing worker tests (`worker/service_test.go` lines 3548-3628) already cover the worker-side version pinning

## Files to modify

| File | Change |
|------|--------|
| `pikoci/build/build.go` | Add `RetrySourceBuildID` field |
| `pikoci/mysql/build.go` | Add column to dbBuild, scanBuild, all SELECTs, both INSERTs |
| `pikoci/mysql/migrate/migrations/V38RetrySourceBuildID.go` | New migration file |
| `pikoci/mysql/migrate/migrations/migrations.go` | Add V38, change array size to 39 |
| `pikoci/builds.go` | Find parent build, set RetrySourceBuildID in RetryJobBuild |
| `pikoci/work.go` | Populate RetryBuildID/RetryBuildNumber in NextWork |
| `pikoci/builds_test.go` | Update retry test expectations |
| `pikoci/mock/build_repository.go` | Regenerate mocks (make gen) |

## Verification

1. `make gen` - regenerate mocks
2. `make test` - all existing + new tests pass
3. `make lint` - no lint errors
4. Manual test: create a pipeline with a git resource, trigger a build, wait for new commits, retry the original build, verify it uses the same git commit
