'use strict';

import { session, apiNotice, login } from './state.js';
import { showToast } from './toast.js';

export class ApiError extends Error {
  constructor(response, body) {
    super(body?.error || response.statusText || 'Unknown error');
    this.status = response.status;
  }
}

export function refreshToken() {
  fetch('/refresh-token', {
    method: 'POST',
    headers: {
      'Authorization': 'Bearer ' + session.value.jwt,
      'Content-Type': 'application/json',
    },
  }).then(r => r.json()).then(resp => {
    if (resp.data && resp.data.jwt) {
      login(resp.data.jwt, resp.data.user);
    }
  }).catch(() => {});
}

export async function api(url, opts = {}) {
  const headers = { 'Content-Type': 'application/json' };
  if (session.value.jwt) {
    headers['Authorization'] = 'Bearer ' + session.value.jwt;
  }

  const res = await fetch(url, { ...opts, headers });

  // Handle token refresh
  if (res.headers.get('X-Refresh-Token') === 'true') {
    refreshToken();
  }

  if (!res.ok) {
    const body = await res.json().catch(() => null);
    const err = new ApiError(res, body);

    if (opts.isInterval && apiNotice.value.error !== '') {
      // Already showing an error — don't overwrite during polling
      throw err;
    }

    let msg;
    if (res.status === 0 || res.status === 502 || res.status === 503 || res.status === 504) {
      msg = 'Connection lost. Retrying...';
    } else {
      msg = err.message;
    }

    if (!opts.silent) {
      apiNotice.value = { ...apiNotice.value, error: msg };
      showToast(msg, 'error');
    }
    throw err;
  }

  if (!opts.silent) {
    if (opts.isInterval) {
      // Interval success: clear error only, preserve success messages
      if (apiNotice.value.error) {
        apiNotice.value = { ...apiNotice.value, error: '' };
      }
    } else {
      apiNotice.value = { error: '', success: '' };
    }
  }

  return res.json();
}

// For interval-based polling: show error on first failure, suppress duplicates.
// On success, clear error but preserve success messages.
// This matches current Backbone.sync isInterval behavior.
export async function apiInterval(url, opts = {}) {
  return api(url, { ...opts, isInterval: true });
}

// --- Auth ---
export const postLogin = (data) => api('/login', { method: 'POST', body: JSON.stringify(data) });
export const postRefreshToken = () => api('/refresh-token', { method: 'POST' });
export const fetchVersion = () => api('/version');

// --- Teams ---
export const fetchTeams = () => api('/teams').then(r => r.data);
export const createTeam = (data) => api('/teams', { method: 'POST', body: JSON.stringify(data) }).then(r => r.data);
export const fetchTeam = (tc) => api('/teams/' + tc).then(r => r.data);
export const updateTeam = (tc, data) => api('/teams/' + tc, { method: 'PUT', body: JSON.stringify(data) });
export const deleteTeam = (tc) => api('/teams/' + tc, { method: 'DELETE' });

// --- Team Members ---
export const fetchTeamMembers = (tc) => api('/teams/' + tc + '/members').then(r => r.data);
export const addTeamMember = (tc, data) => api('/teams/' + tc + '/members', { method: 'POST', body: JSON.stringify(data) });
export const updateTeamMember = (tc, username, data) => api('/teams/' + tc + '/members/' + username, { method: 'PUT', body: JSON.stringify(data) });
export const removeTeamMember = (tc, username) => api('/teams/' + tc + '/members/' + username, { method: 'DELETE' });

// --- Pipelines ---
export const fetchPipelines = (tc) => api('/teams/' + tc + '/pipelines').then(r => r.data);
export const fetchPipeline = (tc, pn) => api('/teams/' + tc + '/pipelines/' + pn).then(r => r.data);
export const createPipeline = (tc, data) => api('/teams/' + tc + '/pipelines', { method: 'POST', body: JSON.stringify(data) });
export const updatePipeline = (tc, pn, data) => api('/teams/' + tc + '/pipelines/' + pn, { method: 'PUT', body: JSON.stringify(data) });
export const deletePipeline = (tc, pn) => api('/teams/' + tc + '/pipelines/' + pn, { method: 'DELETE' });
export const pausePipeline = (tc, pn) => api('/teams/' + tc + '/pipelines/' + pn + '/pause', { method: 'POST' });
export const unpausePipeline = (tc, pn) => api('/teams/' + tc + '/pipelines/' + pn + '/unpause', { method: 'POST' });

// --- Pipeline Image (Graphviz DOT) ---
// GET returns DOT source; POST previews from raw config (sends byte array)
export const fetchPipelineImage = (tc, pn, params) => {
  const qs = params ? '?' + new URLSearchParams(params) : '';
  return api('/teams/' + tc + '/pipelines/' + pn + '/image.dot' + qs);
};
export const previewPipelineImage = (tc, data) =>
  api('/teams/' + tc + '/pipelines/image.dot', { method: 'POST', body: JSON.stringify(data) });

// --- Jobs ---
export const fetchJobs = (tc, pn) => api('/teams/' + tc + '/pipelines/' + pn + '/jobs').then(r => r.data);
export const fetchJob = (tc, pn, jn) => api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn).then(r => r.data);
export const triggerJob = (tc, pn, jn) => api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/trigger', { method: 'POST' });
export const pauseJob = (tc, pn, jn) => api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/pause', { method: 'POST' });
export const unpauseJob = (tc, pn, jn) => api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/unpause', { method: 'POST' });

// --- Builds ---
// Returns { data, meta: { has_more, newest_id, oldest_id } }
export const fetchBuilds = (tc, pn, jn, params) => {
  const qs = params ? '?' + new URLSearchParams(params) : '';
  return api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/builds' + qs);
};
export const cancelBuild = (tc, pn, jn, bid) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/builds/' + bid + '/cancel', { method: 'POST' });
export const fetchBuild = (tc, pn, jn, bid) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/builds/' + bid).then(r => r.data);
export const retryBuild = (tc, pn, jn, bid) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/builds/' + bid + '/retry', { method: 'POST' });

// --- Resources ---
export const fetchResources = (tc, pn) => api('/teams/' + tc + '/pipelines/' + pn + '/resources').then(r => r.data);
export const fetchResource = (tc, pn, rCan) => api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan).then(r => r.data);
export const triggerResource = (tc, pn, rCan) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/trigger', { method: 'POST' });
export const pinResource = (tc, pn, rCan, versionID) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/pin', { method: 'POST', body: JSON.stringify({ version_id: versionID }) });
export const unpinResource = (tc, pn, rCan) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/unpin', { method: 'POST' });

// --- Resource Versions ---
// Returns { data, meta: { has_more, newest_id, oldest_id } }
export const fetchResourceVersions = (tc, pn, rCan, params) => {
  const qs = params ? '?' + new URLSearchParams(params) : '';
  return api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/versions' + qs);
};
export const fetchVersionPath = (tc, pn, rCan, vid, opts) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/versions/' + vid + '/path', opts);
export const triggerVersion = (tc, pn, rCan, vid) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/versions/' + vid + '/trigger', { method: 'POST' });

// --- Users ---
export const fetchUsers = () => api('/users').then(r => r.data);
export const fetchUser = (username) => api('/users/' + username).then(r => r.data);
export const createUser = (data) => api('/users', { method: 'POST', body: JSON.stringify(data) });
export const updateUser = (username, data) => api('/users/' + username, { method: 'PUT', body: JSON.stringify(data) });
export const deleteUser = (username) => api('/users/' + username, { method: 'DELETE' });
export const updateProfile = (data) => api('/profile', { method: 'PUT', body: JSON.stringify(data) });
export const changePassword = (data) => api('/users/change-password', { method: 'POST', body: JSON.stringify(data) });

// --- API Tokens ---
export const fetchApiTokens = () => api('/api-tokens').then(r => r.data);
export const createApiToken = (data) => api('/api-tokens', { method: 'POST', body: JSON.stringify(data) });
export const deleteApiToken = (id) => api('/api-tokens/' + id, { method: 'DELETE' });

// --- Workers ---
export const fetchWorkers = () => api('/workers').then(r => r.data);
export const fetchWorkersHealth = () => api('/workers/health');
export const deleteWorker = (name) => api('/workers/' + name, { method: 'DELETE' });

// --- Webhooks ---
export const regenerateWebhookToken = (tc, pn, rCan) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/webhook_token', { method: 'POST' });

// --- Local Editor Mode ---
export const fetchLocalConfig = () => fetch('/local/config').then(r => r.json());
export const saveLocalConfig = (data) => api('/local/save', { method: 'POST', body: JSON.stringify(data) });
