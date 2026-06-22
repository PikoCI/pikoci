import test from 'node:test';
import assert from 'node:assert/strict';

import {
  registerToastSetter,
  showToast,
  dismissToast,
} from '../../pikoci/transport/http/assets/js/app/toast.js';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// Creates a mock setter that records every call and lets us replay the state
// transitions (each call receives prev => next).
function createMockSetter() {
  let state = [];
  const calls = [];
  const setter = (updater) => {
    const next = updater(state);
    state = next;
    calls.push([...next]);
  };
  return {
    setter,
    getState: () => state,
    getCalls: () => calls,
    reset: () => { state = []; calls.length = 0; },
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test('showToast without registered setter does not throw', () => {
  registerToastSetter(null);
  assert.doesNotThrow(() => showToast('hello', 'success'));
});

test('registerToastSetter stores the setter and showToast uses it', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const mock = createMockSetter();
  registerToastSetter(mock.setter);

  showToast('test msg', 'success');

  // First call: adds the toast with show: false
  const state = mock.getState();
  assert.equal(state.length, 1);
  assert.equal(state[0].msg, 'test msg');
  assert.equal(state[0].type, 'success');
  assert.equal(state[0].show, false);
  assert.equal(typeof state[0].id, 'number');

  // Cleanup
  registerToastSetter(null);
});

test('showToast sets show: true via requestAnimationFrame', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const mock = createMockSetter();
  registerToastSetter(mock.setter);

  showToast('appear', 'success');
  assert.equal(mock.getState()[0].show, false, 'initially show is false');

  // requestAnimationFrame is mocked as setTimeout(fn, 0) in setup.mjs
  t.mock.timers.tick(0);
  assert.equal(mock.getState()[0].show, true, 'show becomes true after rAF');

  registerToastSetter(null);
});

test('dismissToast sets show: false then removes after 300ms', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const mock = createMockSetter();
  registerToastSetter(mock.setter);

  showToast('bye', 'success');
  t.mock.timers.tick(0); // rAF
  const id = mock.getState()[0].id;
  assert.equal(mock.getState()[0].show, true);

  dismissToast(id);
  assert.equal(mock.getState()[0].show, false, 'show set to false immediately');
  assert.equal(mock.getState().length, 1, 'toast still present');

  t.mock.timers.tick(300);
  assert.equal(mock.getState().length, 0, 'toast removed after 300ms');

  registerToastSetter(null);
});

test('showToast error auto-dismisses after 8000ms', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const mock = createMockSetter();
  registerToastSetter(mock.setter);

  showToast('error msg', 'error');
  t.mock.timers.tick(0); // rAF
  assert.equal(mock.getState().length, 1);

  // Should still be visible before 8000ms
  t.mock.timers.tick(7999);
  assert.equal(mock.getState().length, 1, 'still present before 8s');
  assert.equal(mock.getState()[0].show, true);

  // At 8000ms: dismissToast fires, sets show: false
  t.mock.timers.tick(1);
  assert.equal(mock.getState()[0].show, false, 'show false at 8s');

  // After another 300ms: removed
  t.mock.timers.tick(300);
  assert.equal(mock.getState().length, 0, 'removed after dismiss animation');

  registerToastSetter(null);
});

test('showToast success auto-dismisses after 4000ms', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const mock = createMockSetter();
  registerToastSetter(mock.setter);

  showToast('success msg', 'success');
  t.mock.timers.tick(0); // rAF

  t.mock.timers.tick(3999);
  assert.equal(mock.getState().length, 1, 'still present before 4s');

  t.mock.timers.tick(1);
  assert.equal(mock.getState()[0].show, false, 'show false at 4s');

  t.mock.timers.tick(300);
  assert.equal(mock.getState().length, 0, 'removed after dismiss animation');

  registerToastSetter(null);
});

test('registerToastSetter(null) prevents subsequent showToast from crashing', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const mock = createMockSetter();
  registerToastSetter(mock.setter);

  // Show a toast, then unregister before rAF fires
  showToast('will vanish', 'success');
  registerToastSetter(null);

  // rAF callback should not crash
  assert.doesNotThrow(() => t.mock.timers.tick(0));
});

test('dismissToast without registered setter does not throw', () => {
  registerToastSetter(null);
  assert.doesNotThrow(() => dismissToast(12345));
});
