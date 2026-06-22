'use strict';

import { html } from 'htm/preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { isAdmin, isTeamAdmin } from '../state.js';
import { fetchTeams, createTeam, fetchTeam, updateTeam, deleteTeam, fetchUsers, addTeamMember, updateTeamMember, removeTeamMember } from '../api.js';
import { useRequireAuth, useLoading } from '../hooks.js';
import { showToast } from '../toast.js';
import { Breadcrumb } from './Layout.js';

// ---------------------------------------------------------------------------
// TeamsView – list all teams
// ---------------------------------------------------------------------------

export function TeamsView() {
  useRequireAuth();
  const [teams, setTeams] = useState([]);

  useEffect(() => {
    fetchTeams().then(setTeams);
  }, []);

  const onDelete = (tc) => {
    deleteTeam(tc).then(() => {
      showToast('Team deleted', 'success');
      setTeams(teams.filter(t => t.canonical !== tc));
    });
  };

  return html`
    <${Breadcrumb} />
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h1 class="h4 fw-bold mb-0">Teams</h1>
      ${isAdmin.value && html`
        <a type="button" id="team-new" class="btn btn-success" href="/teams/new" data-native onClick=${(e) => { e.preventDefault(); route('/teams/new'); }}>
          <i class="bi bi-plus"></i> New
        </a>
      `}
    </div>
    <div id="team-list">
      ${teams.map(t => html`<${TeamRow} key=${t.canonical} team=${t} onDelete=${onDelete} />`)}
    </div>
  `;
}

// ---------------------------------------------------------------------------
// TeamRow – single team card inside the list
// ---------------------------------------------------------------------------

function TeamRow({ team, onDelete }) {
  const [loading, withLoading] = useLoading();

  const handleDelete = (e) => {
    e.preventDefault();
    e.stopPropagation();
    withLoading(() => onDelete(team.canonical));
  };

  return html`
    <div class="piko-team-row">
      <div class="piko-team-name">${team.name}</div>
      <div class="piko-team-actions">
        <a id="pipelines" href=${'/teams/' + team.canonical + '/pipelines'} data-native type="button" class="btn btn-primary"
          onClick=${(e) => { e.preventDefault(); route('/teams/' + team.canonical + '/pipelines'); }}>
          <i class="bi bi-diagram-2"></i> Pipelines
        </a>
        <a id="manage" href=${'/teams/' + team.canonical} data-native type="button" class="btn btn-info"
          onClick=${(e) => { e.preventDefault(); route('/teams/' + team.canonical); }}>
          <i class="bi bi-gear"></i> Manage
        </a>
        ${isAdmin.value && html`
          <button id="delete" type="button" class="btn btn-danger" disabled=${loading} onClick=${handleDelete}>
            ${loading
              ? html`Deleting... <span class="spinner-border spinner-border-sm" role="status"></span>`
              : html`<i class="bi bi-trash"></i> Delete`}
          </button>
        `}
      </div>
    </div>
  `;
}

// ---------------------------------------------------------------------------
// TeamNew – create a new team
// ---------------------------------------------------------------------------

export function TeamNew() {
  useRequireAuth({ adminOnly: true });
  const [name, setName] = useState('');
  const [loading, withLoading] = useLoading();

  const onSubmit = (e) => {
    e.preventDefault();
    withLoading(async () => {
      const team = await createTeam({ name });
      route('/teams/' + team.canonical);
    });
  };

  return html`
    <div class="mb-3">
      <h1 class="h4 fw-bold">New Team</h1>
    </div>
    <form style="max-width:480px;" onSubmit=${onSubmit}>
      <div class="mb-3">
        <label for="name" class="form-label">Name</label>
        <input type="text" class="form-control" id="name" value=${name}
          placeholder="e.g. platform-team"
          onInput=${(e) => setName(e.target.value)} />
      </div>
      <button type="submit" id="create" class="btn btn-primary" disabled=${loading}>
        ${loading ? 'Creating...' : 'Create'}
      </button>
    </form>
  `;
}

// ---------------------------------------------------------------------------
// TeamShow – team detail with member management
// ---------------------------------------------------------------------------

export function TeamShow({ tc }) {
  useRequireAuth();
  const [team, setTeam] = useState(null);
  const [members, setMembers] = useState([]);
  const [name, setName] = useState('');
  const [showNewMember, setShowNewMember] = useState(false);
  const [users, setUsers] = useState([]);
  const [loading, withLoading] = useLoading();

  const loadTeam = () => {
    fetchTeam(tc).then(t => {
      setTeam(t);
      setName(t.name);
      setMembers(t.members || []);
    });
  };

  useEffect(() => {
    loadTeam();
  }, [tc]);

  const onUpdate = (e) => {
    e.preventDefault();
    withLoading(async () => {
      const resp = await updateTeam(tc, { name });
      showToast('Team updated', 'success');
      const canonical = (resp && resp.data && resp.data.canonical) || name;
      route('/teams/' + canonical);
    });
  };

  const onToggleAdmin = (member, checkboxEl) => {
    updateTeamMember(tc, member.user.username, { admin: !member.admin }).then(() => {
      showToast('Member role updated', 'success');
      setMembers(members.map(m =>
        m.user.username === member.user.username ? { ...m, admin: !m.admin } : m
      ));
    }).catch(() => {
      // Revert checkbox visual state on error (e.g., "cannot have team with no admins")
      if (checkboxEl) checkboxEl.checked = member.admin;
    });
  };

  const onDeleteMember = (member) => {
    return removeTeamMember(tc, member.user.username).then(() => {
      showToast('Member removed', 'success');
      setMembers(prev => prev.filter(m => m.user.username !== member.user.username));
    });
  };

  const onClickNewMember = (e) => {
    e.preventDefault();
    if (!showNewMember) {
      fetchUsers().then(allUsers => {
        const memberUsernames = new Set(members.map(m => m.user.username));
        setUsers(allUsers.filter(u => !memberUsernames.has(u.username)));
        setShowNewMember(true);
      });
    }
  };

  const onAddMember = (username, admin) => {
    return addTeamMember(tc, { admin, user: { username } }).then(() => {
      showToast('Member added', 'success');
      setShowNewMember(false);
      loadTeam();
    });
  };

  if (!team) return null;

  const admin = isTeamAdmin(tc);

  return html`
    <${Breadcrumb} team=${team} />
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h1 class="h4 fw-bold mb-0">Team: ${team.name}</h1>
      <a href=${'/teams/' + team.canonical + '/pipelines'} data-native class="btn btn-primary"
        onClick=${(e) => { e.preventDefault(); route('/teams/' + team.canonical + '/pipelines'); }}>
        <i class="bi bi-diagram-2"></i> Pipelines
      </a>
    </div>
    <form style="max-width:480px;" onSubmit=${onUpdate}>
      <div class="mb-3">
        <label for="name" class="form-label">Name</label>
        <input type="text" class="form-control" id="name" value=${name}
          disabled=${!admin}
          onInput=${(e) => setName(e.target.value)} />
      </div>
      ${admin && html`
        <button type="submit" class="btn btn-primary" disabled=${loading}>
          ${loading ? html`Saving... <span class="spinner-border spinner-border-sm" role="status"></span>` : 'Update'}
        </button>
      `}
    </form>
    <hr style="border-color: var(--border); margin: 1.5rem 0;" />
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h3 class="h5 fw-bold mb-0">Members</h3>
      ${admin && html`
        <a type="button" id="new-member" class="btn btn-success" onClick=${onClickNewMember}>
          <i class="bi bi-person-plus"></i> New Member
        </a>
      `}
    </div>
    <table class="table">
      <thead>
        <tr>
          <th scope="col" class="col-5">Full Name</th>
          <th scope="col" class="col-5">Admin</th>
          <th scope="col" class="col-2">Options</th>
        </tr>
      </thead>
      <tbody>
        ${showNewMember && html`<${NewMemberRow} users=${users} onAdd=${onAddMember} onCancel=${() => setShowNewMember(false)} />`}
        ${members.map(m => html`
          <${MemberRow} key=${m.user.username} member=${m} tc=${tc} onToggleAdmin=${onToggleAdmin} onDelete=${onDeleteMember} />
        `)}
      </tbody>
    </table>
  `;
}

// ---------------------------------------------------------------------------
// NewMemberRow – inline row to add a member
// ---------------------------------------------------------------------------

function NewMemberRow({ users, onAdd }) {
  const [username, setUsername] = useState(users.length ? users[0].username : '');
  const [admin, setAdmin] = useState(false);
  const [loading, withLoading] = useLoading();

  const handleCreate = (e) => {
    e.preventDefault();
    withLoading(() => onAdd(username, admin));
  };

  return html`
    <tr id="create-member">
      <td>
        <select class="form-select" id="username" value=${username}
          onChange=${(e) => setUsername(e.target.value)}>
          ${users.map(u => html`<option value=${u.username}>${u.full_name}</option>`)}
        </select>
      </td>
      <td>
        <div class="form-check form-switch">
          <input class="form-check-input" type="checkbox" role="switch" id="admin"
            checked=${admin} onChange=${(e) => setAdmin(e.target.checked)} />
        </div>
      </td>
      <td>
        <div class="btn-group" role="group">
          <button id="create" type="button" class="btn btn-success" disabled=${loading} onClick=${handleCreate}>
            ${loading
              ? html`Adding... <span class="spinner-border spinner-border-sm" role="status"></span>`
              : 'Create'}
          </button>
        </div>
      </td>
    </tr>
  `;
}

// ---------------------------------------------------------------------------
// MemberRow – existing member row with admin toggle and delete
// ---------------------------------------------------------------------------

function MemberRow({ member, tc, onToggleAdmin, onDelete }) {
  const [loading, withLoading] = useLoading();
  const admin = isTeamAdmin(tc);

  const handleDelete = (e) => {
    e.preventDefault();
    withLoading(() => onDelete(member));
  };

  const handleToggleAdmin = (e) => {
    onToggleAdmin(member, e.target);
  };

  return html`
    <tr>
      <td>${member.user.full_name}</td>
      <td>
        <div class="form-check form-switch">
          <input class="form-check-input" type="checkbox" role="switch" id="admin"
            checked=${member.admin}
            disabled=${!admin}
            onChange=${handleToggleAdmin} />
        </div>
      </td>
      <td>
        <div class="btn-group" role="group">
          ${admin && html`
            <button id="delete" type="button" class="btn btn-danger" disabled=${loading} onClick=${handleDelete}>
              ${loading
                ? html`Removing... <span class="spinner-border spinner-border-sm" role="status"></span>`
                : html`<i class="bi bi-trash"></i> Delete`}
            </button>
          `}
        </div>
      </td>
    </tr>
  `;
}
