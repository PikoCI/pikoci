import test from 'node:test';
import assert from 'node:assert/strict';
import { render } from 'preact-render-to-string';
import { html } from 'htm/preact';

import { Login } from '../../pikoci/transport/http/assets/js/app/components/Login.js';
import { Breadcrumb } from '../../pikoci/transport/http/assets/js/app/components/Layout.js';

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

test('Login renders form with username, password, and login button', () => {
  const output = render(html`<${Login} />`);
  assert.ok(output.includes('id="username"'), 'should have #username input');
  assert.ok(output.includes('id="password"'), 'should have #password input');
  assert.ok(output.includes('id="login"'), 'should have #login button');
  assert.ok(output.includes('Log In'), 'should show "Log In" text');
});

test('Login renders piko-login-wrapper', () => {
  const output = render(html`<${Login} />`);
  assert.ok(output.includes('piko-login-wrapper'), 'should have wrapper class');
  assert.ok(output.includes('piko-login-box'), 'should have box class');
});

test('Login renders form labels', () => {
  const output = render(html`<${Login} />`);
  assert.ok(output.includes('Username'), 'should have Username label');
  assert.ok(output.includes('Password'), 'should have Password label');
});

// ---------------------------------------------------------------------------
// Breadcrumb — no props (root)
// ---------------------------------------------------------------------------

test('Breadcrumb renders breadcrumb nav with Teams link', () => {
  const output = render(html`<${Breadcrumb} />`);
  assert.ok(output.includes('id="breadcrumb"'), 'should have #breadcrumb');
  assert.ok(output.includes('Teams'), 'should show Teams link');
});

// ---------------------------------------------------------------------------
// Breadcrumb — with team only
// ---------------------------------------------------------------------------

test('Breadcrumb renders team name as active item', () => {
  const output = render(html`<${Breadcrumb} team=${{ name: 'Main', canonical: 'main' }} />`);
  assert.ok(output.includes('Main'), 'should show team name');
  assert.ok(output.includes('Teams'), 'should show Teams link');
});

// ---------------------------------------------------------------------------
// Breadcrumb — with team + pipeline
// ---------------------------------------------------------------------------

test('Breadcrumb renders team and pipeline', () => {
  const output = render(html`<${Breadcrumb}
    team=${{ name: 'Main', canonical: 'main' }}
    pipeline=${{ name: 'deploy', canonical: 'deploy' }}
  />`);
  assert.ok(output.includes('Main'), 'should show team name');
  assert.ok(output.includes('deploy'), 'should show pipeline name');
  assert.ok(output.includes('Pipelines'), 'should show Pipelines link');
});

// ---------------------------------------------------------------------------
// Breadcrumb — with team + pipeline + job
// ---------------------------------------------------------------------------

test('Breadcrumb renders job breadcrumb trail', () => {
  const output = render(html`<${Breadcrumb}
    team=${{ name: 'Main', canonical: 'main' }}
    pipeline=${{ name: 'deploy', canonical: 'deploy' }}
    job=${{ name: 'build-image' }}
  />`);
  assert.ok(output.includes('build-image'), 'should show job name');
  assert.ok(output.includes('Jobs'), 'should show Jobs crumb');
  assert.ok(output.includes('Builds'), 'should show Builds crumb');
});

// ---------------------------------------------------------------------------
// Breadcrumb — with team + pipeline + resource
// ---------------------------------------------------------------------------

test('Breadcrumb renders resource breadcrumb trail', () => {
  const output = render(html`<${Breadcrumb}
    team=${{ name: 'Main', canonical: 'main' }}
    pipeline=${{ name: 'deploy', canonical: 'deploy' }}
    resource=${{ canonical: 'source-code' }}
  />`);
  assert.ok(output.includes('source-code'), 'should show resource canonical');
  assert.ok(output.includes('Resources'), 'should show Resources crumb');
  assert.ok(output.includes('Versions'), 'should show Versions crumb');
});

// ---------------------------------------------------------------------------
// Breadcrumb — showPipelines without pipeline
// ---------------------------------------------------------------------------

test('Breadcrumb renders Pipelines as active when showPipelines is set', () => {
  const output = render(html`<${Breadcrumb}
    team=${{ name: 'Main', canonical: 'main' }}
    showPipelines=${true}
  />`);
  assert.ok(output.includes('Pipelines'), 'should show Pipelines crumb');
  assert.ok(output.includes('Main'), 'should show team name as link');
});

// ---------------------------------------------------------------------------
// Notice — skipped because it calls registerToastSetter in useEffect which
// requires the full Preact runtime lifecycle.  The component renders an empty
// #notice div on initial render, which is testable, but importing Layout.js
// also pulls in api.js / state.js / utils.js side effects.  We already import
// Breadcrumb from Layout.js above so this is safe.
// ---------------------------------------------------------------------------

// Note: Notice is not exported from Layout.js (it is a file-private const),
// so we cannot import it directly. Its output is tested indirectly via
// integration / Selenium tests.
