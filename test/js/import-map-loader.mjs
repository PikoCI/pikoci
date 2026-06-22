// Custom ESM loader for Node.js test runner.
// Maps bare specifiers (like 'preact', '@preact/signals') to vendored files.

import { pathToFileURL, fileURLToPath } from 'node:url';
import { resolve as pathResolve } from 'node:path';

const ROOT = pathResolve(fileURLToPath(import.meta.url), '..', '..', '..');
const ASSETS = pathResolve(ROOT, 'pikoci', 'transport', 'http', 'assets');

const importMap = {
  'preact': pathResolve(ASSETS, 'js/vendor/preact.module.js'),
  'preact/hooks': pathResolve(ASSETS, 'js/vendor/preact/hooks.module.js'),
  'htm': pathResolve(ASSETS, 'js/vendor/htm.module.js'),
  'htm/preact': pathResolve(ASSETS, 'js/vendor/htm-preact.module.js'),
  'preact-router': pathResolve(ASSETS, 'js/vendor/preact-router.module.js'),
  'preact-router/match': pathResolve(ASSETS, 'js/vendor/preact-router-match.module.js'),
  '@preact/signals': pathResolve(ASSETS, 'js/vendor/signals.module.js'),
  '@preact/signals-core': pathResolve(ASSETS, 'js/vendor/signals-core.module.js'),
  'preact-render-to-string': pathResolve(ASSETS, 'js/vendor/preact-render-to-string.module.js'),
};

export async function resolve(specifier, context, nextResolve) {
  if (importMap[specifier]) {
    return { url: pathToFileURL(importMap[specifier]).href, shortCircuit: true };
  }
  return nextResolve(specifier, context);
}
