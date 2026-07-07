'use strict';

import { html } from 'htm/preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import {
  fetchUsers, fetchUser, createUser, updateUser, deleteUser,
  updateProfile, changePassword, postRefreshToken,
  fetchApiTokens, createApiToken, deleteApiToken,
  fetchTeams,
  fetchLinkedAccounts, unlinkAccount, fetchAuthMethods,
} from '../api.js';
import { session, login, setNoticeError, isAdmin } from '../state.js';
import { useLoading, useRequireAuth } from '../hooks.js';
import { showToast } from '../toast.js';
import { getProviderIcon } from '../provider-icons.js';

// --- UsersList ---

export function UsersList() {
  useRequireAuth({ adminOnly: true });

  const [users, setUsers] = useState([]);

  useEffect(() => {
    fetchUsers().then(data => setUsers(data || [])).catch(() => {});
  }, []);

  return html`
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h1 class="h4 fw-bold mb-0">Users</h1>
      <a type="button" id="user-new" class="btn btn-success" href="/users/new" data-native onClick=${(e) => { e.preventDefault(); route('/users/new'); }}>
        <i class="bi bi-plus"></i> New User
      </a>
    </div>
    <table class="table">
      <thead>
        <tr>
          <th>Username</th>
          <th>Full Name</th>
          <th>Global Admin</th>
        </tr>
      </thead>
      <tbody id="users-table-body">
        ${users.map(u => html`
          <tr key=${u.username}>
            <td><a href="/users/${u.username}" data-native onClick=${(e) => { e.preventDefault(); route('/users/' + u.username); }}>${u.username}</a></td>
            <td>${u.full_name}</td>
            <td>${u.admin
              ? html`<span class="badge bg-primary">Yes</span>`
              : html`<span class="badge bg-secondary">No</span>`
            }</td>
          </tr>
        `)}
      </tbody>
    </table>
  `;
}

// --- UserNew ---

export function UserNew() {
  useRequireAuth({ adminOnly: true });

  const [username, setUsername] = useState('');
  const [fullName, setFullName] = useState('');
  const [password, setPassword] = useState('');
  const [admin, setAdmin] = useState(false);
  const [loading, withLoading] = useLoading();

  const onSubmit = (e) => {
    e.preventDefault();
    withLoading(async () => {
      const resp = await createUser({ username, password, full_name: fullName, admin });
      if (resp.error) {
        setNoticeError(resp.error);
        showToast(resp.error, 'error');
        return;
      }
      route('/users/' + username);
    });
  };

  return html`
    <div class="mb-3">
      <h1 class="h4 fw-bold">New User</h1>
    </div>
    <form id="user-create-form" onSubmit=${onSubmit}>
      <div class="mb-3">
        <label for="username" class="form-label">Username</label>
        <input type="text" class="form-control" id="username" placeholder="Enter username"
          value=${username} onInput=${(e) => setUsername(e.target.value)} />
      </div>
      <div class="mb-3">
        <label for="full_name" class="form-label">Full Name</label>
        <input type="text" class="form-control" id="full_name" placeholder="Enter full name"
          value=${fullName} onInput=${(e) => setFullName(e.target.value)} />
      </div>
      <div class="mb-3">
        <label for="password" class="form-label">Password</label>
        <input type="password" class="form-control" id="password" placeholder="Enter password"
          value=${password} onInput=${(e) => setPassword(e.target.value)} />
      </div>
      <div class="mb-3 form-check">
        <input type="checkbox" class="form-check-input" id="admin"
          checked=${admin} onChange=${(e) => setAdmin(e.target.checked)} />
        <label class="form-check-label" for="admin">Global Admin</label>
      </div>
      <button type="submit" class="btn btn-primary" disabled=${loading}>
        ${loading ? 'Creating...' : 'Create User'}
      </button>
    </form>
  `;
}

// --- UserShow ---

export function UserShow({ username: usernameParam }) {
  useRequireAuth({ adminOnly: true });

  const [user, setUser] = useState(null);
  const [fullName, setFullName] = useState('');
  const [username, setUsername] = useState('');
  const [adminChecked, setAdminChecked] = useState(false);
  const [newPassword, setNewPassword] = useState('');
  const [loading, withLoading] = useLoading();
  const [resetLoading, withResetLoading] = useLoading();

  useEffect(() => {
    fetchUser(usernameParam).then(u => {
      setUser(u);
      setFullName(u.full_name || '');
      setUsername(u.username || '');
      setAdminChecked(!!u.admin);
    }).catch(() => {});
  }, [usernameParam]);

  const onSubmitForm = (e) => {
    e.preventDefault();
    withLoading(async () => {
      const resp = await updateUser(user.username, { full_name: fullName, username, admin: adminChecked });
      if (resp.error) {
        setNoticeError(resp.error);
        showToast(resp.error, 'error');
        return;
      }
      if (username !== user.username) {
        route('/users/' + username);
      } else {
        setUser(resp.data);
        showToast('User updated', 'success');
      }
    });
  };

  const onResetPassword = (e) => {
    e.preventDefault();
    if (!newPassword) return;
    withResetLoading(async () => {
      const resp = await updateUser(user.username, { password: newPassword, admin: adminChecked });
      if (resp.error) {
        setNoticeError(resp.error);
        showToast(resp.error, 'error');
        return;
      }
      setNewPassword('');
      showToast('Password reset successfully', 'success');
    });
  };

  const onDeleteUser = (e) => {
    e.preventDefault();
    if (!confirm("Are you sure you want to delete user '" + user.username + "'?")) return;
    withLoading(async () => {
      const resp = await deleteUser(user.username);
      if (resp.error) {
        setNoticeError(resp.error);
        showToast(resp.error, 'error');
        return;
      }
      route('/users');
    });
  };

  // Sync value attributes for Selenium compatibility
  useEffect(() => {
    const el = document.getElementById('full_name');
    if (el) el.setAttribute('value', fullName);
  }, [fullName]);
  useEffect(() => {
    const el = document.getElementById('username');
    if (el) el.setAttribute('value', username);
  }, [username]);

  if (!user) return html`<div></div>`;

  return html`
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h1 class="h4 fw-bold mb-0">Edit User: ${user.username}</h1>
    </div>
    <form id="user-form" onSubmit=${onSubmitForm}>
      <div class="mb-3">
        <label for="full_name" class="form-label">Full Name</label>
        <input type="text" class="form-control" id="full_name" value=${fullName}
          onInput=${(e) => setFullName(e.target.value)} />
      </div>
      <div class="mb-3">
        <label for="username" class="form-label">Username</label>
        <input type="text" class="form-control" id="username" value=${username}
          onInput=${(e) => setUsername(e.target.value)} />
      </div>
      <div class="mb-3 form-check">
        <input type="checkbox" class="form-check-input" id="admin"
          checked=${adminChecked} onChange=${(e) => setAdminChecked(e.target.checked)} />
        <label class="form-check-label" for="admin">Global Admin</label>
      </div>
      <button type="submit" class="btn btn-primary" disabled=${loading}>
        ${loading ? 'Saving...' : 'Save Changes'}
      </button>
    </form>
    <hr style="border-color: var(--border); margin: 1.5rem 0;" />
    <h3 class="h5 fw-bold mb-3">Reset Password</h3>
    <form id="reset-password-form" onSubmit=${onResetPassword}>
      <div class="mb-3">
        <label for="new_password" class="form-label">New Password</label>
        <input type="password" class="form-control" id="new_password" placeholder="Enter new password"
          value=${newPassword} onInput=${(e) => setNewPassword(e.target.value)} />
      </div>
      <button type="submit" class="btn btn-warning" disabled=${resetLoading}>
        ${resetLoading ? 'Resetting...' : 'Reset Password'}
      </button>
    </form>
    <hr style="border-color: var(--border); margin: 1.5rem 0;" />
    <button type="button" id="delete-user" class="btn btn-danger" disabled=${loading} onClick=${onDeleteUser}>
      <i class="bi bi-trash"></i> ${loading ? 'Deleting...' : 'Delete User'}
    </button>
  `;
}

// --- Profile ---

export function Profile() {
  useRequireAuth();

  const s = session.value;
  const u = s.user || {};
  const mustChange = !!u.must_change_password;

  const params = new URLSearchParams(window.location.search);
  const defaultTab = mustChange ? 'password' : (params.get('tab') || 'profile');
  const [activeTab, setActiveTab] = useState(defaultTab);

  const switchTab = (tab) => {
    setActiveTab(tab);
    const url = tab === 'profile' ? '/profile' : '/profile?tab=' + tab;
    window.history.replaceState(null, '', url);
  };

  return html`
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h1 class="h4 fw-bold mb-0">Profile</h1>
    </div>
    ${mustChange ? html`
      <div class="alert alert-warning" id="must-change-password-banner" role="alert">
        <i class="bi bi-exclamation-triangle"></i> Please change your default password before continuing.
      </div>
    ` : null}
    <ul class="nav nav-tabs mb-3">
      <li class="nav-item">
        <button class="nav-link ${activeTab === 'profile' ? 'active' : ''}"
          onClick=${() => switchTab('profile')}>Profile</button>
      </li>
      <li class="nav-item">
        <button class="nav-link ${activeTab === 'password' ? 'active' : ''}"
          onClick=${() => switchTab('password')}>Password</button>
      </li>
      <li class="nav-item">
        <button class="nav-link ${activeTab === 'tokens' ? 'active' : ''}"
          onClick=${() => switchTab('tokens')}>API Tokens</button>
      </li>
      <li class="nav-item">
        <button class="nav-link ${activeTab === 'linked' ? 'active' : ''}"
          onClick=${() => switchTab('linked')}>Linked Accounts</button>
      </li>
    </ul>
    ${activeTab === 'profile' ? html`<${ProfileTab} />` : null}
    ${activeTab === 'password' ? html`<${PasswordTab} mustChange=${mustChange} />` : null}
    ${activeTab === 'tokens' ? html`<${ApiTokensTab} />` : null}
    ${activeTab === 'linked' ? html`<${LinkedAccountsTab} />` : null}
  `;
}

function ProfileTab() {
  const s = session.value;
  const u = s.user || {};
  const [fullName, setFullName] = useState(u.full_name || '');
  const [username, setUsername] = useState(u.username || '');
  const [profileLoading, withProfileLoading] = useLoading();

  const onSubmitProfile = (e) => {
    e.preventDefault();
    withProfileLoading(async () => {
      const resp = await updateProfile({ full_name: fullName, username });
      if (resp.error) {
        setNoticeError(resp.error);
        showToast(resp.error, 'error');
        return;
      }
      try {
        const refreshResp = await postRefreshToken();
        if (refreshResp.data && refreshResp.data.jwt) {
          login(refreshResp.data.jwt, refreshResp.data.user);
        }
      } catch {}
      showToast('Profile updated successfully', 'success');
    });
  };

  return html`
    <form id="profile-form" onSubmit=${onSubmitProfile}>
      <div class="mb-3">
        <label for="full_name" class="form-label">Full Name</label>
        <input type="text" class="form-control" id="full_name" value=${fullName}
          onInput=${(e) => setFullName(e.target.value)} />
      </div>
      <div class="mb-3">
        <label for="username" class="form-label">Username</label>
        <input type="text" class="form-control" id="username" value=${username}
          onInput=${(e) => setUsername(e.target.value)} />
      </div>
      <button type="submit" class="btn btn-primary" disabled=${profileLoading}>
        ${profileLoading ? 'Saving...' : 'Save Changes'}
      </button>
    </form>
  `;
}

function PasswordTab({ mustChange }) {
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [pwLoading, withPwLoading] = useLoading();
  const u = session.value.user || {};
  const hasPassword = !!u.has_password;

  const onChangePassword = (e) => {
    e.preventDefault();
    if (newPassword !== confirmPassword) {
      setNoticeError('New passwords do not match');
      showToast('New passwords do not match', 'error');
      return;
    }
    withPwLoading(async () => {
      try {
        await changePassword({ old_password: currentPassword, new_password: newPassword });
      } catch {
        return;
      }
      const currentUser = session.value.user;
      if (currentUser) {
        const updatedUser = { ...currentUser, must_change_password: false };
        login(session.value.jwt, updatedUser);
      }
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
      showToast('Password changed successfully', 'success');
      if (mustChange) {
        setTimeout(() => route('/'), 100);
      }
    });
  };

  return html`
    <form id="change-password-form" onSubmit=${onChangePassword}>
      ${!hasPassword ? html`
        <div class="alert alert-info small mb-3">
          <i class="bi bi-info-circle"></i> You don't have a local password yet. Set one below to enable username/password login.
        </div>
      ` : html`
        <div class="mb-3">
          <label for="current_password" class="form-label">Current Password</label>
          <input type="password" class="form-control" id="current_password"
            placeholder="Enter current password"
            value=${currentPassword} onInput=${(e) => setCurrentPassword(e.target.value)} />
        </div>
      `}
      <div class="mb-3">
        <label for="new_password" class="form-label">New Password</label>
        <input type="password" class="form-control" id="new_password" placeholder="Enter new password"
          value=${newPassword} onInput=${(e) => setNewPassword(e.target.value)} />
      </div>
      <div class="mb-3">
        <label for="confirm_password" class="form-label">Confirm New Password</label>
        <input type="password" class="form-control" id="confirm_password" placeholder="Confirm new password"
          value=${confirmPassword} onInput=${(e) => setConfirmPassword(e.target.value)} />
      </div>
      <button type="submit" class="btn btn-warning" disabled=${pwLoading}>
        ${pwLoading ? 'Changing...' : 'Change Password'}
      </button>
    </form>
  `;
}

function ApiTokensTab() {
  const [tokens, setTokens] = useState([]);
  const [showForm, setShowForm] = useState(false);
  const [createdToken, setCreatedToken] = useState(null);
  const [name, setName] = useState('');
  const [personal, setPersonal] = useState(true);
  const [teamCanonical, setTeamCanonical] = useState('');
  const [tokenRole, setTokenRole] = useState('read');
  const [useExpiration, setUseExpiration] = useState(false);
  const [expiresAt, setExpiresAt] = useState('');
  const [teams, setTeams] = useState([]);
  const [createLoading, withCreateLoading] = useLoading();

  const userMemberships = (session.value.user && session.value.user.memberships) || [];

  useEffect(() => {
    fetchApiTokens().then(data => setTokens(data || [])).catch(() => {});
    fetchTeams().then(data => {
      setTeams(data || []);
    }).catch(() => {});
  }, []);

  const onCreateToken = (e) => {
    e.preventDefault();
    if (!personal && !teamCanonical) {
      showToast('Please select a team', 'error');
      return;
    }
    withCreateLoading(async () => {
      const req = { name, personal };
      if (!personal) {
        req.team_canonical = teamCanonical;
        req.role = tokenRole;
      }
      if (useExpiration && expiresAt) {
        req.expires_at = new Date(expiresAt).toISOString();
      }
      try {
        const result = await createApiToken(req);
        setCreatedToken(result.data || result);
        setName('');
        setUseExpiration(false);
        setExpiresAt('');
        setShowForm(false);
        fetchApiTokens().then(data => setTokens(data || [])).catch(() => {});
      } catch {
        // api() already showed the error toast
      }
    });
  };

  const onDeleteToken = async (id) => {
    if (!confirm('Are you sure you want to delete this token?')) return;
    try {
      await deleteApiToken(id);
      setTokens(tokens.filter(t => t.id !== id));
      showToast('Token deleted', 'success');
    } catch {
      // api() already showed the error toast
    }
  };

  const copyToClipboard = (text) => {
    navigator.clipboard.writeText(text).then(() => {
      showToast('Token copied to clipboard', 'success');
    }).catch(() => {
      showToast('Failed to copy token', 'error');
    });
  };

  const roleOptions = ['read', 'write', 'maintain', 'admin'];

  const teamMembership = userMemberships.find(m => m.team_canonical === teamCanonical);
  const maxRoleLevel = teamMembership ? roleOptions.indexOf(teamMembership.role) : -1;
  // Global admins can assign any role; non-members see an empty list
  const filteredRoles = maxRoleLevel >= 0 ? roleOptions.slice(0, maxRoleLevel + 1)
    : (isAdmin.value ? roleOptions : []);

  const fmtDate = (d) => {
    if (!d) return '\u2014';
    const dt = new Date(d);
    return isNaN(dt) ? '\u2014' : dt.toLocaleDateString();
  };

  return html`
    ${createdToken ? html`
      <div class="alert alert-success alert-dismissible" role="alert">
        <strong>Token created!</strong> Copy it now — you won't be able to see it again.
        <div class="input-group mt-2">
          <input type="text" class="form-control font-monospace" value=${createdToken.token} readonly autocomplete="off" />
          <button class="btn btn-outline-secondary" type="button" onClick=${() => copyToClipboard(createdToken.token)}>
            <i class="bi bi-clipboard"></i> Copy
          </button>
        </div>
        <button type="button" class="btn-close" onClick=${() => setCreatedToken(null)}></button>
      </div>
    ` : null}

    <div class="d-flex justify-content-between align-items-center mb-3">
      <span></span>
      <button class="btn btn-success btn-sm" onClick=${() => setShowForm(!showForm)}>
        <i class="bi bi-plus"></i> New Token
      </button>
    </div>

    ${showForm ? html`
      <div class="card mb-3">
        <div class="card-body">
          <form onSubmit=${onCreateToken}>
            <div class="mb-3">
              <label class="form-label">Token Name</label>
              <input type="text" class="form-control" value=${name} required
                onInput=${(e) => setName(e.target.value)} placeholder="e.g. ci-deploy" />
            </div>
            <div class="mb-3">
              <label class="form-label">Scope</label>
              <div class="form-check">
                <input class="form-check-input" type="radio" id="scope-personal" name="scope"
                  checked=${personal} onChange=${() => setPersonal(true)} />
                <label class="form-check-label" for="scope-personal">
                  Personal <small class="text-muted">— full user access across all teams</small>
                </label>
              </div>
              <div class="form-check">
                <input class="form-check-input" type="radio" id="scope-team" name="scope"
                  checked=${!personal} onChange=${() => setPersonal(false)} />
                <label class="form-check-label" for="scope-team">
                  Team-scoped <small class="text-muted">— limited to one team with a role cap</small>
                </label>
              </div>
            </div>
            ${!personal ? html`
              <div class="row mb-3">
                <div class="col">
                  <label class="form-label">Team</label>
                  <select class="form-select" value=${teamCanonical}
                    onChange=${(e) => { setTeamCanonical(e.target.value); setTokenRole('read'); }}>
                    <option value="">Select a team</option>
                    ${teams.map(t => html`
                      <option value=${t.canonical}>${t.name}</option>
                    `)}
                  </select>
                </div>
                <div class="col">
                  <label class="form-label">Max Role</label>
                  <select class="form-select" value=${tokenRole}
                    onChange=${(e) => setTokenRole(e.target.value)}>
                    ${filteredRoles.map(r => html`
                      <option value=${r}>${r}</option>
                    `)}
                  </select>
                </div>
              </div>
            ` : null}
            <div class="mb-3">
              <div class="form-check mb-2">
                <input class="form-check-input" type="checkbox" id="use-expiration"
                  checked=${useExpiration} onChange=${(e) => {
                    setUseExpiration(e.target.checked);
                    if (!e.target.checked) setExpiresAt('');
                  }} />
                <label class="form-check-label" for="use-expiration">Set expiration date</label>
              </div>
              ${useExpiration ? html`
                <input type="date" class="form-control" value=${expiresAt}
                  min=${new Date().toISOString().split('T')[0]}
                  onInput=${(e) => setExpiresAt(e.target.value)} />
              ` : null}
            </div>
            <button type="submit" class="btn btn-primary" disabled=${createLoading}>
              ${createLoading ? 'Creating...' : 'Create Token'}
            </button>
            <button type="button" class="btn btn-secondary ms-2" onClick=${() => setShowForm(false)}>Cancel</button>
          </form>
        </div>
      </div>
    ` : null}

    ${tokens.length === 0 && !showForm ? html`
      <div class="text-muted text-center py-4">
        No API tokens yet. Create one to authenticate API requests.
      </div>
    ` : null}

    ${tokens.length > 0 ? html`
      <div class="table-responsive">
        <table class="table table-sm">
          <thead>
            <tr>
              <th>Name</th>
              <th>Prefix</th>
              <th>Type</th>
              <th>Team</th>
              <th>Role</th>
              <th>Created</th>
              <th>Last Used</th>
              <th>Expires</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            ${tokens.map(t => html`
              <tr key=${t.id}>
                <td>${t.name}</td>
                <td><code>${t.token_prefix}...</code></td>
                <td><span class="badge ${t.personal ? 'bg-primary' : 'bg-info'}">${t.personal ? 'Personal' : 'Team'}</span></td>
                <td>${t.team_canonical || '—'}</td>
                <td>${t.role || '—'}</td>
                <td>${fmtDate(t.created_at)}</td>
                <td>${fmtDate(t.last_used_at)}</td>
                <td>${t.expires_at ? fmtDate(t.expires_at) : 'Never'}</td>
                <td>
                  <button class="btn btn-outline-danger btn-sm" aria-label="Delete token" onClick=${() => onDeleteToken(t.id)}>
                    <i class="bi bi-trash"></i>
                  </button>
                </td>
              </tr>
            `)}
          </tbody>
        </table>
      </div>
    ` : null}
  `;
}

function LinkedAccountsTab() {
  const [accounts, setAccounts] = useState([]);
  const [providers, setProviders] = useState([]);

  useEffect(() => {
    fetchLinkedAccounts().then(data => setAccounts(data || [])).catch(() => {});
    fetchAuthMethods().then(data => setProviders((data && data.providers) || [])).catch(() => {});

    // Link success toast is handled by app.js on initial load
  }, []);

  const onUnlink = async (canonical) => {
    if (!confirm('Are you sure you want to unlink this account?')) return;
    try {
      await unlinkAccount(canonical);
      setAccounts(accounts.filter(a => a.provider_canonical !== canonical));
      showToast('Account unlinked', 'success');
    } catch {}
  };

  const onLink = (canonical) => {
    const jwt = session.value.jwt;
    window.location.href = '/auth/oauth/' + canonical + '?link=true&token=' + encodeURIComponent(jwt);
  };

  const linkedCanonicals = new Set(accounts.map(a => a.provider_canonical));
  const unlinkableProviders = providers.filter(p => !linkedCanonicals.has(p.canonical));

  return html`
    ${accounts.length > 0 ? html`
      <div class="table-responsive">
        <table class="table table-sm">
          <thead>
            <tr>
              <th>Provider</th>
              <th>Email</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            ${accounts.map(a => html`
              <tr key=${a.provider_canonical}>
                <td><span class="d-inline-flex align-items-center gap-2">${getProviderIcon(a.provider_canonical)} ${a.provider_name}</span></td>
                <td>${a.email || '\u2014'}</td>
                <td>
                  <button class="btn btn-outline-danger btn-sm" onClick=${() => onUnlink(a.provider_canonical)}>
                    <i class="bi bi-x-circle"></i> Unlink
                  </button>
                </td>
              </tr>
            `)}
          </tbody>
        </table>
      </div>
    ` : html`
      <div class="text-muted text-center py-3">No linked accounts.</div>
    `}
    ${unlinkableProviders.length > 0 ? html`
      <div class="mt-3">
        <h6 class="text-muted">Link an account</h6>
        ${unlinkableProviders.map(p => html`
          <button key=${p.canonical} class="btn btn-outline-secondary btn-sm me-2 mb-2 d-inline-flex align-items-center gap-1"
            onClick=${() => onLink(p.canonical)}>
            ${getProviderIcon(p.canonical)} Link ${p.name}
          </button>
        `)}
      </div>
    ` : null}
  `;
}
