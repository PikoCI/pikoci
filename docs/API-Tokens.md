---
description: "Create API tokens for non-interactive access to the PikoCI API. Use in scripts, automation, and monitoring integrations."
---

# API Tokens

API tokens provide non-interactive authenticated access to the PikoCI API. Use them for scripted automation, CI/CD pipelines, monitoring integrations, and any scenario where interactive JWT login is not practical.

## Overview

- **Personal tokens** grant full access across all teams, mirroring the user's own permissions.
- **Team-scoped tokens** are restricted to a single team with a role cap, providing least-privilege access.
- Tokens are tied to a user. When the user is deleted, all their tokens are deleted too.
- The plaintext token is shown **only once** at creation. Store it securely.

## Token Format

Tokens use the format `pko_` followed by 64 hex characters (32 random bytes):

```
pko_a1b2c3d4e5f6...  (68 characters total)
```

Only the SHA-256 hash is stored in the database. PikoCI cannot recover a lost token. You must delete it and create a new one.

## Authentication

Pass the token in the `Authorization` header:

```bash
curl -H "Authorization: Bearer pko_a1b2c3d4..." \
     -H "Content-Type: application/json" \
     https://ci.example.com/teams/main/pipelines
```

## Token Scopes

### Personal Tokens

A personal token inherits all of the user's permissions across every team. If the user is a global admin, the token has global admin access. If the user's role changes on a team, the token's effective permissions change immediately.

```bash
pikoci client api-tokens create --name "my-script" --personal
```

### Team-Scoped Tokens

A team-scoped token is restricted to one team and capped at a specific role. The effective role is always `min(user_role, token_role)`: if the user is later downgraded to `read` but the token was created with `maintain`, the effective role becomes `read`.

```bash
pikoci client api-tokens create --name "ci-deploy" \
  --team-canonical main --role write
```

**Constraints:**
- The token role must be ≤ the user's current role on that team.
- Team-scoped tokens cannot access routes outside their team.
- Team-scoped tokens cannot access global admin routes.

## Expiration

Tokens can optionally have an expiration date. Expired tokens are rejected at authentication time but remain in the database until manually deleted.

```bash
pikoci client api-tokens create --name "temp-access" --personal \
  --expires-at 2026-12-31T23:59:59Z
```

## Security Considerations

- **Hashing:** Tokens are stored as SHA-256 hashes (not bcrypt), which is safe for high-entropy random secrets and enables O(1) indexed lookup.
- **No self-management:** API tokens cannot create, list, or delete other API tokens. Token management requires a JWT session (interactive login).
- **Header-only auth:** Tokens are sent only in the `Authorization` header to avoid URL/log leakage.
- **Membership cleanup:** When a user is removed from a team, all their team-scoped tokens for that team are automatically deleted. Personal tokens are unaffected.
- **User/team deletion:** Foreign key cascades delete all associated tokens.

## Managing Tokens

### Via the UI

Go to **Profile → API Tokens** tab. From there you can:

- Create personal or team-scoped tokens
- View all your tokens (name, prefix, type, team, role, dates)
- Delete tokens

When you create a token, the plaintext value is shown in a green banner with a copy button. This is the only time it will be displayed.

### Via the CLI

#### Create a personal token

```bash
pikoci client api-tokens create --name "my-script" --personal
```

#### Create a team-scoped token

```bash
pikoci client api-tokens create --name "ci-deploy" \
  --team-canonical main --role write
```

#### Create a token with expiration

```bash
pikoci client api-tokens create --name "temp" --personal \
  --expires-at 2026-12-31T23:59:59Z
```

#### List tokens

```bash
pikoci client api-tokens list
```

#### Delete a token

```bash
pikoci client api-tokens delete --id 42
```

#### Store a token for subsequent commands

Instead of passing `--jwt` on every command, store the token locally (replaces the login JWT):

```bash
pikoci client api-tokens use --token "pko_a1b2c3d4..."
```

All subsequent `pikoci client` commands will use this token. To switch back to interactive login, run `pikoci client login` again.

## CLI Reference

### api-tokens create

| Flag | Required | Description |
|------|----------|-------------|
| `--name` | **yes** | Name for the token (unique per user) |
| `--personal` | one of | Create a personal token with full user access |
| `--team-canonical` | one of | Team canonical for a team-scoped token |
| `--role` | with team | Role cap: `read`, `write`, `maintain`, `admin` |
| `--expires-at` | no | Expiration in RFC3339 format (e.g. `2026-12-31T23:59:59Z`) |

`--personal` and `--team-canonical`/`--role` are mutually exclusive. One mode is required.

### api-tokens list

No additional flags. Lists all tokens for the authenticated user.

### api-tokens delete

| Flag | Required | Description |
|------|----------|-------------|
| `--id` | **yes** | ID of the token to delete |

## API Endpoints

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| `POST` | `/api-tokens` | Create a token | JWT only |
| `GET` | `/api-tokens` | List your tokens | JWT only |
| `DELETE` | `/api-tokens/{token_id}` | Delete a token | JWT only |

These endpoints require JWT authentication (interactive login). API tokens cannot manage other API tokens.

### Create token request

```json
{
  "name": "my-script",
  "personal": true,
  "expires_at": "2026-12-31T23:59:59Z"
}
```

Or for team-scoped:

```json
{
  "name": "ci-deploy",
  "personal": false,
  "team_canonical": "main",
  "role": "write"
}
```

### Create token response

```json
{
  "data": {
    "id": 1,
    "name": "my-script",
    "token_prefix": "pko_a1b2c3d4",
    "personal": true,
    "username": "admin",
    "created_at": "2026-06-26T10:00:00Z",
    "token": "pko_a1b2c3d4e5f6..."
  }
}
```

The `token` field is only present in the creation response.
