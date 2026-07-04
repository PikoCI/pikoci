# Worker Team Isolation

**Issue**: #12, #98
**Date**: 2026-07-03

## Problem

Workers are currently global — any worker can process any team's builds, accessing their secrets and source code. For multi-tenant environments, teams need a hard security boundary ensuring their workers only process their own builds.

## Solution: Team-Scoped Worker Tokens + Dispatch Filtering

### Team Worker Tokens

Each team can generate a dedicated worker token (JWT with `team_canonical` and `salt` claims). The salt is stored on the `teams` table and enables token revocation by regenerating the salt.

- `POST /teams/{tc}/worker-token` — generate/regenerate (Admin only)
- `GET /teams/{tc}/worker-token` — get current token (Admin only)

### Worker Registration

When a worker registers with a team-scoped token:
1. The gRPC `Register()` validates the JWT and extracts `team_canonical`
2. The `registeredWorker` and `WorkerStream` carry the team canonical
3. HTTP heartbeats extract team canonical from the JWT context and store it on the worker record

### Dispatch Filtering (NextWork)

For each pipeline in the work queue:
- **Team worker** (`TeamCanonical != ""`): skip pipelines not belonging to its team
- **Global worker** (`TeamCanonical == ""`): skip pipelines whose team has online team-scoped workers (defers to dedicated workers)

This composes with existing tag matching — team filtering happens first, then tag filtering.

### Database Changes

- `teams.worker_token_salt VARCHAR(36)` — salt for JWT revocation
- `workers.team_canonical VARCHAR(255)` — team association for heartbeat records

### UI

- **Team Settings > Workers tab** (Admin only): generate/regenerate token, show usage example
- **Workers list**: "Team" column showing team canonical or "Global"

## Backward Compatibility

- Existing global workers continue to work unchanged (empty `team_canonical`)
- Global workers serve teams that have no dedicated team workers
- No changes to existing JWT tokens or worker registration flow
