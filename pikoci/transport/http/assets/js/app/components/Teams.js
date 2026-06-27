'use strict';

import { html } from 'htm/preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { isAdmin, hasTeamRole } from '../state.js';
import { fetchTeams, createTeam, fetchTeam, updateTeam, deleteTeam, fetchUsers, addTeamMember, updateTeamMember, removeTeamMember, fetchAuditLog, fetchPipelines } from '../api.js';
import { useRequireAuth, useLoading } from '../hooks.js';
import { showToast } from '../toast.js';
import { Breadcrumb } from './Layout.js';

const ASSIGNABLE_ROLES = ['viewer', 'operator', 'maintainer', 'admin'];

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
        ${hasTeamRole(team.canonical, 'admin') && html`
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
// TeamShow – team detail with tabs: Settings | Members | Audit Log
// ---------------------------------------------------------------------------

const VALID_TABS = ['settings', 'members', 'audit'];

export function TeamShow({ tc, tab }) {
  useRequireAuth();
  const [team, setTeam] = useState(null);
  const [members, setMembers] = useState([]);

  const activeTab = VALID_TABS.includes(tab) ? tab : 'settings';

  const switchTab = (t) => {
    route('/teams/' + tc + (t === 'settings' ? '' : '/' + t));
  };

  const loadTeam = () => {
    fetchTeam(tc).then(t => {
      setTeam(t);
      setMembers(t.members || []);
    });
  };

  useEffect(() => {
    loadTeam();
  }, [tc]);

  if (!team) return null;

  return html`
    <${Breadcrumb} team=${team} />
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h1 class="h4 fw-bold mb-0">Team: ${team.name}</h1>
      <a href=${'/teams/' + team.canonical + '/pipelines'} data-native class="btn btn-primary"
        onClick=${(e) => { e.preventDefault(); route('/teams/' + team.canonical + '/pipelines'); }}>
        <i class="bi bi-diagram-2"></i> Pipelines
      </a>
    </div>
    <ul class="nav nav-tabs mb-3">
      <li class="nav-item">
        <a class="nav-link${activeTab === 'settings' ? ' active' : ''}" href=${'/teams/' + tc} data-native id="tab-settings"
          onClick=${(e) => { e.preventDefault(); switchTab('settings'); }}>
          <i class="bi bi-gear"></i> Settings
        </a>
      </li>
      <li class="nav-item">
        <a class="nav-link${activeTab === 'members' ? ' active' : ''}" href=${'/teams/' + tc + '/members'} data-native id="tab-members"
          onClick=${(e) => { e.preventDefault(); switchTab('members'); }}>
          <i class="bi bi-people"></i> Members
        </a>
      </li>
      <li class="nav-item">
        <a class="nav-link${activeTab === 'audit' ? ' active' : ''}" href=${'/teams/' + tc + '/audit'} data-native id="tab-audit"
          onClick=${(e) => { e.preventDefault(); switchTab('audit'); }}>
          <i class="bi bi-journal-text"></i> Audit Log
        </a>
      </li>
    </ul>
    ${activeTab === 'settings' && html`<${SettingsTab} tc=${tc} team=${team} />`}
    ${activeTab === 'members' && html`<${MembersTab} tc=${tc} members=${members} setMembers=${setMembers} loadTeam=${loadTeam} />`}
    ${activeTab === 'audit' && html`<${AuditLogTab} tc=${tc} />`}
  `;
}

// ---------------------------------------------------------------------------
// SettingsTab
// ---------------------------------------------------------------------------

function SettingsTab({ tc, team }) {
  const [name, setName] = useState(team.name);
  const [loading, withLoading] = useLoading();
  const canUpdateTeam = hasTeamRole(tc, 'admin');

  const onUpdate = (e) => {
    e.preventDefault();
    withLoading(async () => {
      const resp = await updateTeam(tc, { name });
      showToast('Team updated', 'success');
      const canonical = (resp && resp.data && resp.data.canonical) || name;
      route('/teams/' + canonical);
    });
  };

  return html`
    <form style="max-width:480px;" onSubmit=${onUpdate}>
      <div class="mb-3">
        <label for="name" class="form-label">Name</label>
        <input type="text" class="form-control" id="name" value=${name}
          disabled=${!canUpdateTeam}
          onInput=${(e) => setName(e.target.value)} />
      </div>
      ${canUpdateTeam && html`
        <button type="submit" class="btn btn-primary" disabled=${loading}>
          ${loading ? html`Saving... <span class="spinner-border spinner-border-sm" role="status"></span>` : 'Update'}
        </button>
      `}
    </form>
  `;
}

// ---------------------------------------------------------------------------
// MembersTab
// ---------------------------------------------------------------------------

function MembersTab({ tc, members, setMembers, loadTeam }) {
  const [showNewMember, setShowNewMember] = useState(false);
  const [users, setUsers] = useState([]);
  const canManageMembers = hasTeamRole(tc, 'admin');

  const onChangeRole = (member, newRole) => {
    updateTeamMember(tc, member.user.username, { role: newRole }).then(() => {
      showToast('Member role updated', 'success');
      setMembers(members.map(m =>
        m.user.username === member.user.username ? { ...m, role: newRole } : m
      ));
    }).catch(() => {
      setMembers(prev => [...prev]);
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

  const onAddMember = (username, memberRole) => {
    return addTeamMember(tc, { role: memberRole, user: { username } }).then(() => {
      showToast('Member added', 'success');
      setShowNewMember(false);
      loadTeam();
    });
  };

  return html`
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h3 class="h5 fw-bold mb-0">Members</h3>
      ${canManageMembers && html`
        <a type="button" id="new-member" class="btn btn-success" onClick=${onClickNewMember}>
          <i class="bi bi-person-plus"></i> New Member
        </a>
      `}
    </div>
    <table class="table">
      <thead>
        <tr>
          <th scope="col" class="col-4">Full Name</th>
          <th scope="col" class="col-4">Role${' '}<a href="https://docs.pikoci.com/Roles" target="_blank" rel="noopener" title="${'Viewer: read-only access\nOperator: trigger, cancel, retry builds; pause/unpause; pin/unpin\nMaintainer: create, edit, delete pipelines and resources\nAdmin: manage members, team settings, delete team\n\nClick to go to docs'}" style="color:var(--text-muted);font-size:0.85em;"><i class="bi bi-info-circle"></i></a></th>
          <th scope="col" class="col-4">Options</th>
        </tr>
      </thead>
      <tbody>
        ${showNewMember && html`<${NewMemberRow} users=${users} onAdd=${onAddMember} onCancel=${() => setShowNewMember(false)} />`}
        ${members.map(m => html`
          <${MemberRow} key=${m.user.username} member=${m} tc=${tc} members=${members} onChangeRole=${onChangeRole} onDelete=${onDeleteMember} />
        `)}
      </tbody>
    </table>
  `;
}

// ---------------------------------------------------------------------------
// AuditLogTab
// ---------------------------------------------------------------------------

const AUDIT_ACTIONS = [
  'pipeline.created', 'pipeline.updated', 'pipeline.deleted',
  'pipeline.paused', 'pipeline.unpaused',
  'job.triggered', 'job.cancelled', 'job.retried', 'job.paused', 'job.unpaused',
  'resource.pinned', 'resource.unpinned', 'resource.check_triggered',
  'member.added', 'member.removed', 'member.role_changed',
];

// FilterChips: renders selected filter items as chips with include/exclude toggle.
// items: [{value, exclude}], options: string[], onAdd(value), onRemove(index), onToggle(index)
function FilterChips({ items, options, onAdd, onRemove, onToggle, placeholder }) {
  const [sel, setSel] = useState('');
  const available = options.filter(o => !items.some(i => i.value === o));

  const handleAdd = () => {
    if (sel && !items.some(i => i.value === sel)) {
      onAdd(sel);
      setSel('');
    }
  };

  return html`
    <div>
      <div class="input-group input-group-sm mb-1">
        <select class="form-select" value=${sel} onChange=${(e) => setSel(e.target.value)}>
          <option value="">${placeholder || 'Select...'}</option>
          ${available.map(o => html`<option value=${o}>${o}</option>`)}
        </select>
        <button type="button" class="btn btn-outline-success" disabled=${!sel} onClick=${handleAdd}>
          <i class="bi bi-plus"></i>
        </button>
      </div>
      <div class="d-flex flex-wrap gap-1">
        ${items.map((item, i) => html`
          <span class="badge ${item.exclude ? 'bg-danger' : 'bg-primary'} d-flex align-items-center gap-1" style="cursor:pointer;font-size:0.85em;">
            <span onClick=${() => onToggle(i)} title="Click to toggle include/exclude">
              ${item.exclude ? '≠' : '='} ${item.value}
            </span>
            <button type="button" class="btn-close btn-close-white" style="font-size:0.5em;" onClick=${() => onRemove(i)}></button>
          </span>
        `)}
      </div>
    </div>
  `;
}

function AuditLogTab({ tc }) {
  const [entries, setEntries] = useState([]);
  const [hasMore, setHasMore] = useState(false);
  const [userFilters, setUserFilters] = useState([]);    // [{value, exclude}]
  const [actionFilters, setActionFilters] = useState([]); // [{value, exclude}]
  const [pipelineFilters, setPipelineFilters] = useState([]); // [{value, exclude:false}]
  const [filterSince, setFilterSince] = useState('');
  const [filterUntil, setFilterUntil] = useState('');
  const [loading, withLoading] = useLoading();
  const [teamMembers, setTeamMembers] = useState([]);
  const [pipelines, setPipelines] = useState([]);

  // Load options for dropdowns — merge current members/pipelines with
  // actors/targets already in the audit log so removed users and deleted
  // pipelines remain filterable.
  useEffect(() => {
    Promise.all([
      fetchTeam(tc),
      fetchPipelines(tc),
      fetchAuditLog(tc, new URLSearchParams({ limit: 0 })),
    ]).then(([t, ps, auditResp]) => {
      const userSet = new Set(['system']);
      if (t && t.members) t.members.forEach(m => userSet.add(m.user.username));
      const pipeSet = new Set((ps || []).map(p => p.canonical));

      // Add actors and pipeline targets from existing audit entries
      for (const e of (auditResp.data || [])) {
        if (e.actor) userSet.add(e.actor);
        if (e.target_type === 'pipeline' && e.target_name) pipeSet.add(e.target_name);
        // For job/resource targets like "pipeline/job", extract pipeline prefix
        if (e.details && e.details.pipeline) pipeSet.add(e.details.pipeline);
      }
      setTeamMembers([...userSet].sort());
      setPipelines([...pipeSet].sort());
    });
  }, [tc]);

  const buildSearchParams = (extra) => {
    const sp = new URLSearchParams();
    for (const f of userFilters) {
      sp.append(f.exclude ? 'exclude_user' : 'user', f.value);
    }
    for (const f of actionFilters) {
      sp.append(f.exclude ? 'exclude_action' : 'action', f.value);
    }
    for (const f of pipelineFilters) {
      sp.append('pipeline', f.value);
    }
    if (filterSince) sp.set('since', new Date(filterSince).toISOString());
    if (filterUntil) sp.set('until', new Date(filterUntil).toISOString());
    if (extra) {
      for (const [k, v] of Object.entries(extra)) sp.set(k, v);
    }
    return sp;
  };

  const loadEntries = () => {
    withLoading(async () => {
      const resp = await fetchAuditLog(tc, buildSearchParams());
      setEntries(resp.data || []);
      setHasMore(resp.meta ? resp.meta.has_more : false);
    });
  };

  useEffect(() => {
    loadEntries();
  }, [tc]);

  const applyFilters = (e) => {
    e.preventDefault();
    loadEntries();
  };

  const loadMore = () => {
    if (!entries.length) return;
    const oldestId = entries[entries.length - 1].id;
    withLoading(async () => {
      const resp = await fetchAuditLog(tc, buildSearchParams({ before: oldestId }));
      setEntries(prev => [...prev, ...(resp.data || [])]);
      setHasMore(resp.meta ? resp.meta.has_more : false);
    });
  };

  const formatTime = (t) => {
    try { return new Date(t).toLocaleString(); } catch { return t; }
  };

  const actionBadgeClass = (action) => {
    if (action.startsWith('pipeline.')) return 'bg-primary';
    if (action.startsWith('job.')) return 'bg-info';
    if (action.startsWith('resource.')) return 'bg-warning text-dark';
    if (action.startsWith('member.')) return 'bg-success';
    return 'bg-secondary';
  };

  const addFilter = (setter) => (value) => setter(prev => [...prev, { value, exclude: false }]);
  const removeFilter = (setter) => (i) => setter(prev => prev.filter((_, idx) => idx !== i));
  const toggleFilter = (setter) => (i) => setter(prev => prev.map((f, idx) => idx === i ? { ...f, exclude: !f.exclude } : f));

  return html`
    <form class="row g-2 mb-3 align-items-start" onSubmit=${applyFilters}>
      <div class="col-auto" style="min-width:180px;">
        <label class="form-label mb-1" style="font-size:0.85em;">User</label>
        <${FilterChips} items=${userFilters} options=${teamMembers}
          onAdd=${addFilter(setUserFilters)} onRemove=${removeFilter(setUserFilters)}
          onToggle=${toggleFilter(setUserFilters)} placeholder="Add user..." />
      </div>
      <div class="col-auto" style="min-width:220px;">
        <label class="form-label mb-1" style="font-size:0.85em;">Action</label>
        <${FilterChips} items=${actionFilters} options=${AUDIT_ACTIONS}
          onAdd=${addFilter(setActionFilters)} onRemove=${removeFilter(setActionFilters)}
          onToggle=${toggleFilter(setActionFilters)} placeholder="Add action..." />
      </div>
      <div class="col-auto" style="min-width:180px;">
        <label class="form-label mb-1" style="font-size:0.85em;">Pipeline</label>
        <${FilterChips} items=${pipelineFilters} options=${pipelines}
          onAdd=${addFilter(setPipelineFilters)} onRemove=${removeFilter(setPipelineFilters)}
          onToggle=${() => {}} placeholder="Add pipeline..." />
      </div>
      <div class="col-auto">
        <label class="form-label mb-1" style="font-size:0.85em;">Since</label>
        <input type="datetime-local" class="form-control form-control-sm" value=${filterSince}
          onInput=${(e) => setFilterSince(e.target.value)} />
      </div>
      <div class="col-auto">
        <label class="form-label mb-1" style="font-size:0.85em;">Until</label>
        <input type="datetime-local" class="form-control form-control-sm" value=${filterUntil}
          onInput=${(e) => setFilterUntil(e.target.value)} />
      </div>
      <div class="col-auto" style="padding-top:1.65em;">
        <button type="submit" class="btn btn-sm btn-primary" disabled=${loading}>Filter</button>
      </div>
    </form>
    <table class="table table-sm">
      <thead>
        <tr>
          <th>Time</th>
          <th>User</th>
          <th>Action</th>
          <th>Target</th>
          <th>Details</th>
        </tr>
      </thead>
      <tbody id="audit-log-body">
        ${entries.length === 0 && !loading ? html`
          <tr><td colspan="5" class="text-muted text-center">No audit log entries.</td></tr>
        ` : entries.map(e => html`
          <tr key=${e.id}>
            <td style="white-space:nowrap;">${formatTime(e.created_at)}</td>
            <td>${e.actor}</td>
            <td><span class="badge ${actionBadgeClass(e.action)}">${e.action}</span></td>
            <td>${e.target_name}</td>
            <td>${e.details ? JSON.stringify(e.details) : ''}</td>
          </tr>
        `)}
      </tbody>
    </table>
    ${hasMore && html`
      <div class="text-center mb-3">
        <button type="button" class="btn btn-outline-primary btn-sm" disabled=${loading} onClick=${loadMore}>
          ${loading ? html`Loading... <span class="spinner-border spinner-border-sm" role="status"></span>` : 'Load more'}
        </button>
      </div>
    `}
  `;
}

// ---------------------------------------------------------------------------
// NewMemberRow – inline row to add a member
// ---------------------------------------------------------------------------

function NewMemberRow({ users, onAdd }) {
  const [username, setUsername] = useState(users.length ? users[0].username : '');
  const [memberRole, setMemberRole] = useState('maintainer');
  const [loading, withLoading] = useLoading();

  const handleCreate = (e) => {
    e.preventDefault();
    withLoading(() => onAdd(username, memberRole));
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
        <select class="form-select" id="role" value=${memberRole}
          onChange=${(e) => setMemberRole(e.target.value)}>
          ${ASSIGNABLE_ROLES.map(r => html`<option value=${r}>${r}</option>`)}
        </select>
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
// MemberRow – existing member row with role dropdown and delete
// ---------------------------------------------------------------------------

function MemberRow({ member, tc, members, onChangeRole, onDelete }) {
  const [loading, withLoading] = useLoading();
  const canManage = hasTeamRole(tc, 'admin');
  const isLastAdmin = member.role === 'admin' && (members || []).filter(m => m.role === 'admin').length <= 1;

  const handleDelete = (e) => {
    e.preventDefault();
    withLoading(() => onDelete(member));
  };

  const handleRoleChange = (e) => {
    onChangeRole(member, e.target.value);
  };

  return html`
    <tr>
      <td>${member.user.full_name}</td>
      <td>
        ${canManage
          ? html`
            <select class="form-select form-select-sm" value=${member.role}
              disabled=${isLastAdmin}
              onChange=${handleRoleChange}>
              ${ASSIGNABLE_ROLES.map(r => html`<option value=${r}>${r}</option>`)}
            </select>
          `
          : html`<span>${member.role}</span>`
        }
      </td>
      <td>
        <div class="btn-group" role="group">
          ${canManage && !isLastAdmin && html`
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
