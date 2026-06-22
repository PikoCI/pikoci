// Browser globals (localStorage) are provided by test/js/setup.mjs preload.

import test from 'node:test';
import assert from 'node:assert/strict';

import {
  session,
  apiNotice,
  isTeamAdmin,
  isTeamMember,
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
      memberships: [{ admin: true, team_canonical: 'team1' }],
    },
  };
  assert.equal(isTeamAdmin('team1'), true);
});

test('isTeamAdmin: team member but not admin returns false', () => {
  session.value = {
    user: {
      admin: false,
      memberships: [{ admin: false, team_canonical: 'team1' }],
    },
  };
  assert.equal(isTeamAdmin('team1'), false);
});

test('isTeamAdmin: wrong team returns false', () => {
  session.value = {
    user: {
      admin: false,
      memberships: [{ admin: true, team_canonical: 'team1' }],
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
      memberships: [{ admin: false, team_canonical: 'team1' }],
    },
  };
  assert.equal(isTeamMember('team1'), true);
});

test('isTeamMember: non-member returns false', () => {
  session.value = {
    user: {
      admin: false,
      memberships: [{ admin: false, team_canonical: 'other' }],
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
