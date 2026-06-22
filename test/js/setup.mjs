// Preload script: sets up browser globals and registers the custom ESM loader.
// Usage: node --import ./test/js/setup.mjs --test test/js/*.test.mjs

import { register } from 'node:module';

// Mock browser globals that source modules access at import time.
globalThis.localStorage = {
  _data: {},
  getItem(k) { return this._data[k] ?? null; },
  setItem(k, v) { this._data[k] = String(v); },
  removeItem(k) { delete this._data[k]; },
};

globalThis.requestAnimationFrame = (fn) => setTimeout(fn, 0);

// Register the custom ESM loader for bare specifier resolution.
register('./import-map-loader.mjs', import.meta.url);
