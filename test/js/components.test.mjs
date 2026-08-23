import test from 'node:test';
import assert from 'node:assert/strict';
import { render } from 'preact-render-to-string';
import { html } from 'htm/preact';

import { Login } from '../../pikoci/transport/http/assets/js/app/components/Login.js';
import { Breadcrumb } from '../../pikoci/transport/http/assets/js/app/components/Layout.js';
import { StepRow } from '../../pikoci/transport/http/assets/js/app/components/Jobs.js';
import { EntryRow, NewEntryRow } from '../../pikoci/transport/http/assets/js/app/components/ConfigStore.js';

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

// ---------------------------------------------------------------------------
// StepRow — running step shows live elapsed time
// ---------------------------------------------------------------------------

test('StepRow shows live elapsed time when stepElapsed is provided for a started step', () => {
  const isAutoScrollingRef = { current: false };
  const step = { type: 'task', name: 'build', status: 'started', duration: 0, logs: '' };
  const output = render(html`<${StepRow}
    step=${step}
    expanded=${false}
    onToggle=${() => {}}
    autoFollow=${false}
    setAutoFollow=${() => {}}
    isAutoScrollingRef=${isAutoScrollingRef}
    stepElapsed=${{ build: '00:00:42' }}
  />`);
  assert.ok(output.includes('00:00:42'), 'should show live elapsed time for running step');
});

test('StepRow shows duration when step is completed and stepElapsed not provided', () => {
  const isAutoScrollingRef = { current: false };
  const step = { type: 'task', name: 'build', status: 'succeeded', duration: '00:00:05', logs: '' };
  const output = render(html`<${StepRow}
    step=${step}
    expanded=${false}
    onToggle=${() => {}}
    autoFollow=${false}
    setAutoFollow=${() => {}}
    isAutoScrollingRef=${isAutoScrollingRef}
    stepElapsed=${{}}
  />`);
  assert.ok(output.includes('00:00:05'), 'should show completed step duration');
// ConfigStore — EntryRow
// ---------------------------------------------------------------------------

const secretEntry = { name: 'GITHUB_TOKEN', canonical: 'GITHUB_TOKEN', kind: 'secret', scope: 'team' };
const plainEntry = { name: 'LOG_LEVEL', canonical: 'LOG_LEVEL', kind: 'plain', scope: 'team', value: 'debug' };

test('EntryRow: secret hides the value behind dots', () => {
  const output = render(html`<${EntryRow} entry=${secretEntry} canWrite=${true} />`);
  assert.ok(output.includes('GITHUB_TOKEN'), 'should show the name');
  assert.ok(output.includes('••••••••'), 'should mask the value');
  assert.ok(output.includes('bi-lock-fill'), 'should show the lock badge');
});

test('EntryRow: plain shows the value in the clear', () => {
  const output = render(html`<${EntryRow} entry=${plainEntry} canWrite=${true} />`);
  assert.ok(output.includes('LOG_LEVEL'), 'should show the name');
  assert.ok(output.includes('debug'), 'should show the value');
  assert.ok(!output.includes('••••••••'), 'plain values must not be masked');
});

// A secret value is never returned by the API, but if one ever were, the row
// must not put it in the DOM.
test('EntryRow: never renders a value on a secret entry', () => {
  const leaky = { ...secretEntry, value: 'ghp_should_never_render' };
  const output = render(html`<${EntryRow} entry=${leaky} canWrite=${true} />`);
  assert.ok(!output.includes('ghp_should_never_render'), 'secret value must never reach the DOM');
});

test('EntryRow: hides delete for a user without write access', () => {
  const withWrite = render(html`<${EntryRow} entry=${plainEntry} canWrite=${true} />`);
  const readOnly = render(html`<${EntryRow} entry=${plainEntry} canWrite=${false} />`);
  assert.ok(withWrite.includes('delete-config'), 'maintainer sees delete');
  assert.ok(!readOnly.includes('delete-config'), 'reader must not see delete');
});

// ---------------------------------------------------------------------------
// ConfigStore — NewEntryRow
// ---------------------------------------------------------------------------

test('NewEntryRow: defaults to secret, matching the server default', () => {
  const output = render(html`<${NewEntryRow} existing=${[]} />`);
  assert.ok(output.includes('id="config-secret"'), 'should have the secret toggle');
  assert.ok(output.includes('checked'), 'secret should be checked by default');
  assert.ok(output.includes('type="password"'), 'value input should be masked by default');
});

test('NewEntryRow: save is disabled until a name and value are given', () => {
  const output = render(html`<${NewEntryRow} existing=${[]} />`);
  assert.ok(output.includes('id="save-config"'), 'should have the save button');
  assert.ok(output.includes('disabled'), 'save should start disabled');
});

test('NewEntryRow: renders the name and value inputs', () => {
  const output = render(html`<${NewEntryRow} existing=${[]} />`);
  assert.ok(output.includes('id="config-name"'), 'should have the name input');
  assert.ok(output.includes('id="config-value"'), 'should have the value input');
});
