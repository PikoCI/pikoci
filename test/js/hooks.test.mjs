import test from 'node:test';
import assert from 'node:assert/strict';

// ---------------------------------------------------------------------------
// hooks.js wraps Preact hooks (useState, useEffect, useRef) so we cannot call
// the exported hooks directly in a plain Node environment.  Instead we test
// the *core logic* that the hooks encapsulate by reproducing the important
// behavioural patterns (visibility-based polling, loading wrapper) with
// minimal mocks.
// ---------------------------------------------------------------------------

// ---- helpers: minimal document mock for visibilitychange -----------------

function createDocumentMock() {
  const listeners = {};
  return {
    hidden: false,
    addEventListener(evt, fn) {
      (listeners[evt] ??= []).push(fn);
    },
    removeEventListener(evt, fn) {
      if (listeners[evt]) {
        listeners[evt] = listeners[evt].filter(f => f !== fn);
      }
    },
    _fire(evt) {
      for (const fn of listeners[evt] ?? []) fn();
    },
    _listeners: listeners,
  };
}

// ---- usePolling core logic -----------------------------------------------
// Reproduces the start/stop/visibility logic from usePolling's useEffect body.

function createPollingEffect(fnRef, interval, doc) {
  let id = null;

  const start = () => {
    if (id !== null) return;
    try { fnRef.current(); } catch (_) {}
    id = setInterval(() => {
      try { fnRef.current(); } catch (_) {}
    }, interval);
  };

  const stop = () => {
    if (id !== null) {
      clearInterval(id);
      id = null;
    }
  };

  const onVisibility = () => {
    if (doc.hidden) {
      stop();
    } else {
      start();
    }
  };

  doc.addEventListener('visibilitychange', onVisibility);
  if (!doc.hidden) {
    start();
  }

  // cleanup function (returned by useEffect)
  return () => {
    stop();
    doc.removeEventListener('visibilitychange', onVisibility);
  };
}

// ---- tests: usePolling logic ---------------------------------------------

test('usePolling: calls fn immediately on start', () => {
  const doc = createDocumentMock();
  let calls = 0;
  const fnRef = { current: () => calls++ };

  const cleanup = createPollingEffect(fnRef, 60_000, doc);
  assert.equal(calls, 1, 'fn should be called once immediately');
  cleanup();
});

test('usePolling: calls fn on interval', (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });
  const doc = createDocumentMock();
  let calls = 0;
  const fnRef = { current: () => calls++ };

  const cleanup = createPollingEffect(fnRef, 1000, doc);
  assert.equal(calls, 1, 'initial call');

  t.mock.timers.tick(1000);
  assert.equal(calls, 2, 'after 1 tick');

  t.mock.timers.tick(1000);
  assert.equal(calls, 3, 'after 2 ticks');

  cleanup();
});

test('usePolling: pauses when document becomes hidden', (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });
  const doc = createDocumentMock();
  let calls = 0;
  const fnRef = { current: () => calls++ };

  const cleanup = createPollingEffect(fnRef, 1000, doc);
  assert.equal(calls, 1);

  // hide the tab
  doc.hidden = true;
  doc._fire('visibilitychange');

  t.mock.timers.tick(3000);
  assert.equal(calls, 1, 'no new calls while hidden');

  cleanup();
});

test('usePolling: resumes with immediate call when visible again', (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });
  const doc = createDocumentMock();
  let calls = 0;
  const fnRef = { current: () => calls++ };

  const cleanup = createPollingEffect(fnRef, 1000, doc);
  assert.equal(calls, 1);

  // hide
  doc.hidden = true;
  doc._fire('visibilitychange');
  assert.equal(calls, 1);

  // show
  doc.hidden = false;
  doc._fire('visibilitychange');
  assert.equal(calls, 2, 'immediate call on resume');

  t.mock.timers.tick(1000);
  assert.equal(calls, 3, 'interval resumes');

  cleanup();
});

test('usePolling: cleanup removes visibilitychange listener', () => {
  const doc = createDocumentMock();
  const fnRef = { current: () => {} };

  const cleanup = createPollingEffect(fnRef, 1000, doc);
  assert.equal((doc._listeners['visibilitychange'] ?? []).length, 1);

  cleanup();
  assert.equal((doc._listeners['visibilitychange'] ?? []).length, 0);
});

test('usePolling: fn errors do not break polling', (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });
  const doc = createDocumentMock();
  let calls = 0;
  const fnRef = {
    current: () => {
      calls++;
      throw new Error('boom');
    },
  };

  const cleanup = createPollingEffect(fnRef, 1000, doc);
  assert.equal(calls, 1, 'called despite error');

  t.mock.timers.tick(1000);
  assert.equal(calls, 2, 'interval continues after error');

  cleanup();
});

test('usePolling: does not double-start if start called twice', (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });
  const doc = createDocumentMock();
  let calls = 0;
  const fnRef = { current: () => calls++ };

  const cleanup = createPollingEffect(fnRef, 1000, doc);
  assert.equal(calls, 1);

  // Simulate a second visibilitychange to visible (should be a no-op)
  doc.hidden = false;
  doc._fire('visibilitychange');
  assert.equal(calls, 1, 'start guards against double invocation');

  cleanup();
});

// ---- tests: useLoading logic ---------------------------------------------
// Reproduce the withLoading wrapper as a plain async function.

function createWithLoading() {
  let loading = false;
  const setLoading = (v) => { loading = v; };
  const getLoading = () => loading;

  const withLoading = async (fn) => {
    setLoading(true);
    try {
      return await fn();
    } finally {
      setLoading(false);
    }
  };

  return { getLoading, withLoading };
}

test('useLoading: withLoading sets loading true then false', async () => {
  const { getLoading, withLoading } = createWithLoading();
  assert.equal(getLoading(), false);

  let loadingDuringFn = null;
  await withLoading(async () => {
    loadingDuringFn = getLoading();
    return 42;
  });

  assert.equal(loadingDuringFn, true, 'loading should be true during fn');
  assert.equal(getLoading(), false, 'loading should be false after fn');
});

test('useLoading: withLoading returns fn result', async () => {
  const { withLoading } = createWithLoading();
  const result = await withLoading(async () => 'hello');
  assert.equal(result, 'hello');
});

test('useLoading: withLoading resets loading on error', async () => {
  const { getLoading, withLoading } = createWithLoading();

  await assert.rejects(
    () => withLoading(async () => { throw new Error('fail'); }),
    { message: 'fail' },
  );

  assert.equal(getLoading(), false, 'loading should be false after error');
});
