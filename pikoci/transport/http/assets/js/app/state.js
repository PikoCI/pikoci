'use strict';

import { signal, computed } from '@preact/signals';

export const userSessionKey = 'piko-user-jwt';
export const session = signal(JSON.parse(localStorage.getItem(userSessionKey) || '{}'));
export const apiNotice = signal({ error: '', success: '' });
export const teams = signal([]);

export const isLoggedIn = computed(() => !!session.value.jwt);
export const isAdmin = computed(() => {
  const u = session.value.user;
  return u && u.admin;
});

const ROLE_LEVELS = { public: 0, read: 1, write: 2, maintain: 3, admin: 4 };

export function getTeamRole(tc) {
  const u = session.value.user;
  if (!u) return null;
  if (u.admin) return 'admin';
  const m = (u.memberships || []).find(m => m.team_canonical === tc);
  return m ? m.role : null;
}

export function hasTeamRole(tc, requiredRole) {
  const u = session.value.user;
  if (!u) return false;
  if (u.admin) return true;
  const r = getTeamRole(tc);
  if (!r) return false;
  return (ROLE_LEVELS[r] || 0) >= (ROLE_LEVELS[requiredRole] || 0);
}

export function isTeamAdmin(tc) {
  return hasTeamRole(tc, 'admin');
}

export function isTeamMember(tc) {
  const u = session.value.user;
  if (!u) return false;
  if (u.admin) return true;
  if (!tc) return true;
  return hasTeamRole(tc, 'read');
}

export function login(jwt, user) {
  session.value = { jwt, user };
  localStorage.setItem(userSessionKey, JSON.stringify(session.value));
}

export function logout() {
  session.value = {};
  localStorage.removeItem(userSessionKey);
}

export function setNoticeError(msg) {
  apiNotice.value = { error: msg, success: '' };
}

export function setNoticeSuccess(msg) {
  apiNotice.value = { error: '', success: msg };
}

export function clearNotice() {
  apiNotice.value = { error: '', success: '' };
}
