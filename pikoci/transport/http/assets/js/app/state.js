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

export function isTeamAdmin(tc) {
  const u = session.value.user;
  if (!u) return false;
  if (u.admin) return true;
  return (u.memberships || []).some(m => m.admin && m.team_canonical === tc);
}

export function isTeamMember(tc) {
  const u = session.value.user;
  if (!u) return false;
  if (u.admin) return true;
  if (!tc) return true;
  return (u.memberships || []).some(m => m.team_canonical === tc);
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
