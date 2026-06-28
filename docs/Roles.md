# Roles & Permissions

PikoCI uses role-based access control (RBAC) to manage what team members can do. Each team member is assigned a role, and roles are hierarchical — each role inherits all permissions of roles below it.

## Role Hierarchy

| Role | Level | Description |
|------|-------|-------------|
| **Public** | 0 | Unauthenticated access to public pipelines. Not assignable to users. |
| **Read** | 1 | Read-only access. Can view pipelines, jobs, builds, resources, and team info. |
| **Write** | 2 | Can trigger, cancel, and retry builds. Can pause/unpause pipelines and jobs. Can pin/unpin resource versions. |
| **Maintain** | 3 | Can create, update, and delete pipelines. Can manage resources and regenerate webhook tokens. |
| **Admin** | 4 | Full team control. Can add, remove, and change members. Can update team settings and delete the team. Every team must have at least one admin. |

## What Each Role Can Do

### Public

The public role is **not assignable to users**. It represents unauthenticated access and only applies to pipelines marked as public. Public users can:

- View the pipeline graph and configuration
- View pipeline jobs and their status
- View build lists and build logs
- View resources and resource versions
- Access the pipeline image (SVG/PNG)

Public users **cannot** list pipelines, view team info, trigger jobs, or perform any write operations.

### Read

- View pipelines, jobs, builds, and build logs
- View resources and resource versions
- View team information and members
- Change own password and update own profile

### Write

Everything a Read user can do, plus:

- Trigger jobs manually
- Cancel running builds
- Retry failed builds
- Pause and unpause pipelines and jobs
- Pin and unpin resource versions
- Trigger resource checks

### Maintain

Everything a Write user can do, plus:

- Create, edit, and delete pipelines
- Upload pipeline configuration
- Update resource settings
- Create resource versions
- Regenerate webhook tokens
- Create triggers

### Admin

Everything a Maintain user can do, plus:

- Add new members to the team
- Change member roles
- Remove members from the team
- Update team name and settings
- Delete the team

**Note:** Every team must have at least one admin. You cannot demote or remove the last admin.

## Global Admin

The global admin flag (`--admin` on user creation) is separate from team roles. A global admin can:

- Create and manage users
- Create teams
- Manage workers
- Export the database
- Access all teams regardless of membership

## Assigning Roles

### Via the UI

Navigate to your team page and use the **Role** dropdown next to each member to change their role. Only team admins can manage members.

### Via the CLI

```bash
# Add a member with a specific role
pikoci client teams members create \
  --team-canonical my-team \
  --username alice \
  --role write

# Change a member's role
pikoci client teams members update \
  --team-canonical my-team \
  --username alice \
  --role maintain
```

Available roles: `read`, `write`, `maintain`, `admin`.

## Default Roles

- When you **create a team**, you become the **admin**.
- When you **add a member** via the CLI, the default role is **maintain**.
- When you **add a member** via the UI, you choose the role from a dropdown (default: **maintain**).

## API Tokens and Roles

[API tokens](API-Tokens.md) can be scoped to a team with a maximum role. The effective role of a team-scoped token is always `min(user_role, token_role)` — if a user is demoted, the token's effective permissions are reduced immediately without any action needed. Personal tokens inherit all of the user's roles across all teams.

When a user is removed from a team, all their team-scoped tokens for that team are automatically deleted.

## Public Pipelines

Pipelines marked as public can be viewed by anyone without authentication. This includes the pipeline graph, jobs, builds, resources, and resource versions. Public access is read-only and does not require any role.
