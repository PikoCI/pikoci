// Browser globals (localStorage, requestAnimationFrame) are provided by test/js/setup.mjs preload.

import test from 'node:test';
import assert from 'node:assert/strict';

import { ApiError, api } from '../../pikoci/transport/http/assets/js/app/api.js';
import { session, apiNotice } from '../../pikoci/transport/http/assets/js/app/state.js';

// Helper to create a mock Response
function mockResponse(status, body, headers = {}) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 200 ? 'OK' : 'Error',
    headers: {
      get(name) { return headers[name] || null; },
    },
    json() { return Promise.resolve(body); },
    text() { return Promise.resolve(JSON.stringify(body)); },
  };
}

test.beforeEach(() => {
  session.value = {};
  apiNotice.value = { error: '', success: '' };
  localStorage._data = {};
});

// --- ApiError ---

test('ApiError: sets message from body.error', () => {
  const resp = mockResponse(400, null);
  const err = new ApiError(resp, { error: 'bad field' });
  assert.equal(err.message, 'bad field');
  assert.equal(err.status, 400);
});

test('ApiError: falls back to statusText', () => {
  const resp = mockResponse(500, null);
  resp.statusText = 'Internal Server Error';
  const err = new ApiError(resp, null);
  assert.equal(err.message, 'Internal Server Error');
  assert.equal(err.status, 500);
});

test('ApiError: falls back to Unknown error', () => {
  const resp = mockResponse(500, null);
  resp.statusText = '';
  const err = new ApiError(resp, {});
  assert.equal(err.message, 'Unknown error');
});

test('ApiError: is an instance of Error', () => {
  const resp = mockResponse(400, null);
  const err = new ApiError(resp, { error: 'test' });
  assert.ok(err instanceof Error);
});

// --- api function ---

test('api: successful request returns parsed JSON', async () => {
  const original = globalThis.fetch;
  globalThis.fetch = () => Promise.resolve(mockResponse(200, { data: 'hello' }));
  try {
    const result = await api('/test');
    assert.deepEqual(result, { data: 'hello' });
  } finally {
    globalThis.fetch = original;
  }
});

test('api: sets Authorization header when session has jwt', async () => {
  const original = globalThis.fetch;
  let capturedHeaders;
  globalThis.fetch = (_url, opts) => {
    capturedHeaders = opts.headers;
    return Promise.resolve(mockResponse(200, { ok: true }));
  };
  session.value = { jwt: 'my-token' };
  try {
    await api('/test');
    assert.equal(capturedHeaders['Authorization'], 'Bearer my-token');
  } finally {
    globalThis.fetch = original;
    session.value = {};
  }
});

test('api: non-ok response throws ApiError and sets notice', async () => {
  const original = globalThis.fetch;
  globalThis.fetch = () => Promise.resolve(mockResponse(422, { error: 'validation failed' }));
  try {
    await assert.rejects(() => api('/test'), (err) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 422);
      assert.equal(err.message, 'validation failed');
      return true;
    });
    assert.equal(apiNotice.value.error, 'validation failed');
  } finally {
    globalThis.fetch = original;
  }
});

test('api: connection lost message for 502/503/504', async () => {
  const original = globalThis.fetch;
  for (const status of [502, 503, 504]) {
    globalThis.fetch = () => Promise.resolve(mockResponse(status, { error: 'upstream' }));
    apiNotice.value = { error: '', success: '' };
    try {
      await api('/test').catch(() => {});
    } finally {}
    assert.equal(apiNotice.value.error, 'Connection lost. Retrying...',
      `expected connection lost for status ${status}`);
  }
  globalThis.fetch = original;
});

test('api: silent option suppresses notice', async () => {
  const original = globalThis.fetch;
  globalThis.fetch = () => Promise.resolve(mockResponse(400, { error: 'nope' }));
  try {
    await api('/test', { silent: true }).catch(() => {});
    assert.equal(apiNotice.value.error, '');
  } finally {
    globalThis.fetch = original;
  }
});

test('api: successful request clears notice', async () => {
  const original = globalThis.fetch;
  apiNotice.value = { error: 'old error', success: 'old success' };
  globalThis.fetch = () => Promise.resolve(mockResponse(200, { ok: true }));
  try {
    await api('/test');
    assert.equal(apiNotice.value.error, '');
    assert.equal(apiNotice.value.success, '');
  } finally {
    globalThis.fetch = original;
  }
});

// --- Team Worker Token API functions ---

import { generateTeamWorkerToken, getTeamWorkerToken } from '../../pikoci/transport/http/assets/js/app/api.js';

test('generateTeamWorkerToken: POSTs to correct endpoint and returns token', async () => {
  const original = globalThis.fetch;
  let capturedUrl, capturedOpts;
  globalThis.fetch = (url, opts) => {
    capturedUrl = url;
    capturedOpts = opts;
    return Promise.resolve(mockResponse(200, { token: 'eyJhbG...' }));
  };
  try {
    const token = await generateTeamWorkerToken('main');
    assert.equal(token, 'eyJhbG...');
    assert.equal(capturedUrl, '/teams/main/worker-token');
    assert.equal(capturedOpts.method, 'POST');
  } finally {
    globalThis.fetch = original;
  }
});

test('getTeamWorkerToken: GETs from correct endpoint and returns token', async () => {
  const original = globalThis.fetch;
  let capturedUrl;
  globalThis.fetch = (url, opts) => {
    capturedUrl = url;
    return Promise.resolve(mockResponse(200, { token: 'eyJtoken...' }));
  };
  try {
    const token = await getTeamWorkerToken('my-team');
    assert.equal(token, 'eyJtoken...');
    assert.equal(capturedUrl, '/teams/my-team/worker-token');
  } finally {
    globalThis.fetch = original;
  }
});

test('getTeamWorkerToken: returns empty string when no token', async () => {
  const original = globalThis.fetch;
  globalThis.fetch = () => Promise.resolve(mockResponse(200, { token: '' }));
  try {
    const token = await getTeamWorkerToken('main');
    assert.equal(token, '');
  } finally {
    globalThis.fetch = original;
  }
});
