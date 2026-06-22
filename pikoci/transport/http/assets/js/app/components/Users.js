'use strict';

import { html } from 'htm/preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import {
  fetchUsers, fetchUser, createUser, updateUser, deleteUser,
  updateProfile, changePassword, postRefreshToken,
} from '../api.js';
import { session, login, setNoticeError } from '../state.js';
import { useLoading, useRequireAuth } from '../hooks.js';
import { showToast } from '../toast.js';

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
          <th>Role</th>
        </tr>
      </thead>
      <tbody id="users-table-body">
        ${users.map(u => html`
          <tr key=${u.username}>
            <td><a href="/users/${u.username}" data-native onClick=${(e) => { e.preventDefault(); route('/users/' + u.username); }}>${u.username}</a></td>
            <td>${u.full_name}</td>
            <td>${u.admin
              ? html`<span class="badge bg-primary">Admin</span>`
              : html`<span class="badge bg-secondary">Member</span>`
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
        <label class="form-check-label" for="admin">Admin</label>
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
        <label class="form-check-label" for="admin">Admin</label>
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
  const [fullName, setFullName] = useState(u.full_name || '');
  const [username, setUsername] = useState(u.username || '');
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [mustChange, setMustChange] = useState(!!u.must_change_password);
  const [profileLoading, withProfileLoading] = useLoading();
  const [pwLoading, withPwLoading] = useLoading();

  const onSubmitProfile = (e) => {
    e.preventDefault();
    withProfileLoading(async () => {
      const resp = await updateProfile({ full_name: fullName, username });
      if (resp.error) {
        setNoticeError(resp.error);
        showToast(resp.error, 'error');
        return;
      }
      // Refresh token to get updated JWT with new username/name
      try {
        const refreshResp = await postRefreshToken();
        if (refreshResp.data && refreshResp.data.jwt) {
          login(refreshResp.data.jwt, refreshResp.data.user);
        }
      } catch {}
      showToast('Profile updated successfully', 'success');
    });
  };

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
        // Error already handled by api()
        return;
      }
      const wasForcedChange = mustChange;
      setMustChange(false);
      // Update session to clear must_change_password
      const currentUser = session.value.user;
      if (currentUser) {
        const updatedUser = { ...currentUser, must_change_password: false };
        login(session.value.jwt, updatedUser);
      }
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
      showToast('Password changed successfully', 'success');
      if (wasForcedChange) {
        // Delay route to let the toast render before navigating
        setTimeout(() => route('/'), 100);
      }
    });
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
    <hr style="border-color: var(--border); margin: 1.5rem 0;" />
    <h3 class="h5 fw-bold mb-3">Change Password</h3>
    <form id="change-password-form" onSubmit=${onChangePassword}>
      <div class="mb-3">
        <label for="current_password" class="form-label">Current Password</label>
        <input type="password" class="form-control" id="current_password" placeholder="Enter current password"
          value=${currentPassword} onInput=${(e) => setCurrentPassword(e.target.value)} />
      </div>
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
