// Browser globals (localStorage) are provided by test/js/setup.mjs preload.

import test from 'node:test';
import assert from 'node:assert/strict';

import {
  session,
  apiNotice,
  isTeamAdmin,
  isTeamMember,
  hasTeamRole,
  getTeamRole,
  login,
  logout,
  setNoticeError,
  setNoticeSuccess,
  clearNotice,
} from '../../pikoci/transport/http/assets/js/app/state.js';

// Reset state before each test
test.beforeEach(() => {
  session.value = {};
  apiNotice.value = { error: '', success: '' };
  localStorage._data = {};
});

// --- isTeamAdmin ---

test('isTeamAdmin: returns false when no user', () => {
  assert.equal(isTeamAdmin('team1'), false);
});

test('isTeamAdmin: global admin returns true for any team', () => {
  session.value = { user: { admin: true, memberships: [] } };
  assert.equal(isTeamAdmin('team1'), true);
  assert.equal(isTeamAdmin('any'), true);
});

test('isTeamAdmin: team admin returns true for their team', () => {
  session.value = {
    user: {
      admin: false,
      memberships: [{ role: 'admin', team_canonical: 'team1' }],
    },
  };
  assert.equal(isTeamAdmin('team1'), true);
});

test('isTeamAdmin: team member but not admin returns false', () => {
  session.value = {
    user: {
      admin: false,
      memberships: [{ role: 'read', team_canonical: 'team1' }],
    },
  };
  assert.equal(isTeamAdmin('team1'), false);
});

test('isTeamAdmin: wrong team returns false', () => {
  session.value = {
    user: {
      admin: false,
      memberships: [{ role: 'admin', team_canonical: 'team1' }],
    },
  };
  assert.equal(isTeamAdmin('team2'), false);
});

// --- isTeamMember ---

test('isTeamMember: returns false when no user', () => {
  assert.equal(isTeamMember('team1'), false);
});

test('isTeamMember: global admin returns true', () => {
  session.value = { user: { admin: true, memberships: [] } };
  assert.equal(isTeamMember('team1'), true);
});

test('isTeamMember: member of team returns true', () => {
  session.value = {
    user: {
      admin: false,
      memberships: [{ role: 'read', team_canonical: 'team1' }],
    },
  };
  assert.equal(isTeamMember('team1'), true);
});

test('isTeamMember: non-member returns false', () => {
  session.value = {
    user: {
      admin: false,
      memberships: [{ role: 'read', team_canonical: 'other' }],
    },
  };
  assert.equal(isTeamMember('team1'), false);
});

test('isTeamMember: null tc returns true for any logged-in user', () => {
  session.value = {
    user: { admin: false, memberships: [] },
  };
  assert.equal(isTeamMember(null), true);
});

// --- hasTeamRole ---

test('hasTeamRole: write can do write actions', () => {
  session.value = {
    user: {
      admin: false,
      memberships: [{ role: 'write', team_canonical: 'team1' }],
    },
  };
  assert.equal(hasTeamRole('team1', 'write'), true);
  assert.equal(hasTeamRole('team1', 'read'), true);
  assert.equal(hasTeamRole('team1', 'maintain'), false);
});

test('hasTeamRole: maintain can do maintain and below', () => {
  session.value = {
    user: {
      admin: false,
      memberships: [{ role: 'maintain', team_canonical: 'team1' }],
    },
  };
  assert.equal(hasTeamRole('team1', 'maintain'), true);
  assert.equal(hasTeamRole('team1', 'write'), true);
  assert.equal(hasTeamRole('team1', 'admin'), false);
});

test('hasTeamRole: global admin bypasses all', () => {
  session.value = { user: { admin: true, memberships: [] } };
  assert.equal(hasTeamRole('team1', 'admin'), true);
});

// --- getTeamRole ---

test('getTeamRole: returns role for matching team', () => {
  session.value = {
    user: {
      admin: false,
      memberships: [{ role: 'write', team_canonical: 'team1' }],
    },
  };
  assert.equal(getTeamRole('team1'), 'write');
  assert.equal(getTeamRole('team2'), null);
});

test('getTeamRole: global admin returns admin', () => {
  session.value = { user: { admin: true, memberships: [] } };
  assert.equal(getTeamRole('any'), 'admin');
});

// --- Component permission gate matrix ---
// Each test validates that the correct role is required for each UI action.
// This catches bugs like using isLoggedIn or adminOnly when a specific role is needed.
//
// Component → UI action → required role:
//   Editor.js (PipelineNew/PipelineEdit): requiredRole='maintain' via useRequireAuth
//   Teams.js (TeamNew): adminOnly=true (global admin) via useRequireAuth
//   Teams.js (TeamRow delete): hasTeamRole(tc, 'admin')
//   Teams.js (member management): hasTeamRole(tc, 'admin')
//   PipelineList.js ("New Pipeline"):    hasTeamRole(tc, 'maintain')
//   PipelineShow.js (pause/unpause):     hasTeamRole(tc, 'write')
//   PipelineShow.js (edit/delete):       hasTeamRole(tc, 'maintain')
//   Jobs.js (trigger/pause/unpause):     hasTeamRole(tc, 'write')
//   Jobs.js (cancel/retry build):        hasTeamRole(tc, 'write')
//   Resources.js (trigger/pin):          hasTeamRole(tc, 'write')
//   Resources.js (webhook):              hasTeamRole(tc, 'maintain')

test('component gates: maintain can access pipeline editor', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'maintain', team_canonical: 't' }] } };
  assert.equal(hasTeamRole('t', 'maintain'), true, 'maintainer should access pipeline editor');
});

test('component gates: write cannot access pipeline editor', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'write', team_canonical: 't' }] } };
  assert.equal(hasTeamRole('t', 'maintain'), false, 'operator should not access pipeline editor');
});

test('component gates: write can cancel/retry builds', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'write', team_canonical: 't' }] } };
  assert.equal(hasTeamRole('t', 'write'), true, 'operator should see cancel/retry');
});

test('component gates: read cannot cancel/retry builds', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'read', team_canonical: 't' }] } };
  assert.equal(hasTeamRole('t', 'write'), false, 'viewer should not see cancel/retry');
});

test('component gates: admin can manage team members', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'admin', team_canonical: 't' }] } };
  assert.equal(hasTeamRole('t', 'admin'), true, 'admin should manage members');
});

test('component gates: maintain cannot manage team members', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'maintain', team_canonical: 't' }] } };
  assert.equal(hasTeamRole('t', 'admin'), false, 'maintainer should not manage members');
});

test('component gates: global admin can access team creation (adminOnly)', () => {
  session.value = { user: { admin: true, memberships: [] } };
  assert.equal(isTeamAdmin(null), true, 'global admin should access team creation');
});

test('component gates: admin cannot access team creation without global admin', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'admin', team_canonical: 't' }] } };
  assert.equal(isTeamAdmin(null), false, 'non-global-admin should not access team creation');
});

test('component gates: global admin can access workers page (adminOnly)', () => {
  session.value = { user: { admin: true, memberships: [] } };
  assert.equal(isTeamAdmin(null), true, 'global admin should access workers page');
});

test('component gates: non-admin cannot access workers page', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'admin', team_canonical: 't' }] } };
  assert.equal(isTeamAdmin(null), false, 'non-global-admin should not access workers page');
});

// --- Role hierarchy: full matrix ---

test('role hierarchy: read can only view', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'read', team_canonical: 't' }] } };
  assert.equal(hasTeamRole('t', 'read'), true, 'read: can view');
  assert.equal(hasTeamRole('t', 'write'), false, 'read: cannot write');
  assert.equal(hasTeamRole('t', 'maintain'), false, 'read: cannot maintain');
  assert.equal(hasTeamRole('t', 'admin'), false, 'read: cannot admin');
});

test('role hierarchy: write can write and view', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'write', team_canonical: 't' }] } };
  assert.equal(hasTeamRole('t', 'read'), true, 'write: can view');
  assert.equal(hasTeamRole('t', 'write'), true, 'write: can write');
  assert.equal(hasTeamRole('t', 'maintain'), false, 'write: cannot maintain');
  assert.equal(hasTeamRole('t', 'admin'), false, 'write: cannot admin');
});

test('role hierarchy: maintain inherits write', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'maintain', team_canonical: 't' }] } };
  assert.equal(hasTeamRole('t', 'read'), true, 'maintain: can view');
  assert.equal(hasTeamRole('t', 'write'), true, 'maintain: can write');
  assert.equal(hasTeamRole('t', 'maintain'), true, 'maintain: can maintain');
  assert.equal(hasTeamRole('t', 'admin'), false, 'maintain: cannot admin');
});

test('role hierarchy: admin has all permissions (top team role)', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'admin', team_canonical: 't' }] } };
  assert.equal(hasTeamRole('t', 'read'), true, 'admin: can view');
  assert.equal(hasTeamRole('t', 'write'), true, 'admin: can write');
  assert.equal(hasTeamRole('t', 'maintain'), true, 'admin: can maintain');
  assert.equal(hasTeamRole('t', 'admin'), true, 'admin: can admin');
});

test('role hierarchy: wrong team returns false for all levels', () => {
  session.value = { user: { admin: false, memberships: [{ role: 'admin', team_canonical: 'team-a' }] } };
  assert.equal(hasTeamRole('team-b', 'read'), false, 'wrong team: viewer');
  assert.equal(hasTeamRole('team-b', 'admin'), false, 'wrong team: admin');
});

test('role hierarchy: no user returns false', () => {
  session.value = {};
  assert.equal(hasTeamRole('t', 'read'), false);
});

// --- login / logout ---

test('login sets session and localStorage', () => {
  login('tok123', { username: 'alice' });
  assert.equal(session.value.jwt, 'tok123');
  assert.equal(session.value.user.username, 'alice');
  const stored = JSON.parse(localStorage.getItem('piko-user-jwt'));
  assert.equal(stored.jwt, 'tok123');
});

test('logout clears session and localStorage', () => {
  login('tok', { username: 'bob' });
  logout();
  assert.deepEqual(session.value, {});
  assert.equal(localStorage.getItem('piko-user-jwt'), null);
});

// --- notice helpers ---

test('setNoticeError sets error and clears success', () => {
  setNoticeError('bad request');
  assert.equal(apiNotice.value.error, 'bad request');
  assert.equal(apiNotice.value.success, '');
});

test('setNoticeSuccess sets success and clears error', () => {
  setNoticeSuccess('saved!');
  assert.equal(apiNotice.value.success, 'saved!');
  assert.equal(apiNotice.value.error, '');
});

test('clearNotice clears both', () => {
  setNoticeError('err');
  clearNotice();
  assert.equal(apiNotice.value.error, '');
  assert.equal(apiNotice.value.success, '');
});
