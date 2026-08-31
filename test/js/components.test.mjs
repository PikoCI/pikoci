import test from 'node:test';
import assert from 'node:assert/strict';
import { render } from 'preact-render-to-string';
import { html } from 'htm/preact';

import { Login } from '../../pikoci/transport/http/assets/js/app/components/Login.js';
import { Breadcrumb } from '../../pikoci/transport/http/assets/js/app/components/Layout.js';
import { StepRow } from '../../pikoci/transport/http/assets/js/app/components/Jobs.js';
import {
  EntryRow, EditEntryRow, NewEntryRow, mergeEntries,
  loadEntries, saveEntry, removeEntry,
} from '../../pikoci/transport/http/assets/js/app/components/Secrets.js';

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
// Breadcrumb — with team + pipeline + secrets
// ---------------------------------------------------------------------------

// The secrets page navigates by breadcrumb like every other page, so the
// pipeline crumb has to become a link back rather than stay the active one.
test('Breadcrumb renders the secrets trail with a link back to the pipeline', () => {
  const output = render(html`<${Breadcrumb}
    team=${{ name: 'Main', canonical: 'main' }}
    pipeline=${{ name: 'deploy', canonical: 'deploy' }}
    secrets=${true}
  />`);
  assert.ok(output.includes('Secrets'), 'should show the Secrets crumb');
  assert.ok(output.includes('href="/teams/main/pipelines/deploy"'),
    'the pipeline crumb should link back');
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
});
// ---------------------------------------------------------------------------
// Secrets — EntryRow
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
  assert.ok(withWrite.includes('delete-secret'), 'maintainer sees delete');
  assert.ok(!readOnly.includes('delete-secret'), 'reader must not see delete');
});

test('EntryRow: offers edit to a maintainer and not to a reader', () => {
  const withWrite = render(html`<${EntryRow} entry=${secretEntry} canWrite=${true} />`);
  const readOnly = render(html`<${EntryRow} entry=${secretEntry} canWrite=${false} />`);
  assert.ok(withWrite.includes('edit-secret'), 'maintainer can replace a value');
  assert.ok(!readOnly.includes('edit-secret'), 'reader must not see edit');
});

test('EntryRow: exposes the name as a row hook for the browser tests', () => {
  const output = render(html`<${EntryRow} entry=${secretEntry} canWrite=${true} />`);
  assert.ok(output.includes('piko-secret-row'), 'row should carry the shared class');
  assert.ok(output.includes('data-name="GITHUB_TOKEN"'), 'row should carry its canonical name');
});

// ---------------------------------------------------------------------------
// Secrets — NewEntryRow
// ---------------------------------------------------------------------------

test('NewEntryRow: defaults to secret, matching the server default', () => {
  const output = render(html`<${NewEntryRow} existing=${[]} />`);
  assert.ok(output.includes('id="secret-kind"'), 'should have the secret toggle');
  assert.ok(output.includes('checked'), 'secret should be checked by default');
  assert.ok(output.includes('type="password"'), 'value input should be masked by default');
});

test('NewEntryRow: save is disabled until a name and value are given', () => {
  const output = render(html`<${NewEntryRow} existing=${[]} />`);
  assert.ok(output.includes('id="save-secret"'), 'should have the save button');
  assert.ok(output.includes('disabled'), 'save should start disabled');
});

test('NewEntryRow: renders the name and value inputs', () => {
  const output = render(html`<${NewEntryRow} existing=${[]} />`);
  assert.ok(output.includes('id="secret-name"'), 'should have the name input');
  assert.ok(output.includes('id="secret-value"'), 'should have the value input');
});

// ---------------------------------------------------------------------------
// Secrets — EditEntryRow
// ---------------------------------------------------------------------------

test('EditEntryRow: fixes the name and offers a value input', () => {
  const output = render(html`<${EditEntryRow} entry=${secretEntry} />`);
  assert.ok(output.includes('GITHUB_TOKEN'), 'should show which entry is being edited');
  assert.ok(output.includes('id="secret-edit-value"'), 'should have the value input');
  assert.ok(!output.includes('id="secret-name"'), 'the name must not be editable');
});

test('EditEntryRow: starts empty and masked for a secret', () => {
  const output = render(html`<${EditEntryRow} entry=${secretEntry} />`);
  assert.ok(output.includes('type="password"'), 'a secret value stays masked while typing');
  assert.ok(output.includes('disabled'), 'save is disabled until a value is typed');
});

// The point of editing in place is that the old value is never shown, only
// replaced.
test('EditEntryRow: never renders a stored secret value', () => {
  const leaky = { ...secretEntry, value: 'ghp_should_never_render' };
  const output = render(html`<${EditEntryRow} entry=${leaky} />`);
  assert.ok(!output.includes('ghp_should_never_render'), 'secret value must never reach the DOM');
});

test('EditEntryRow: prefills a plain value so a small change is a small edit', () => {
  const output = render(html`<${EditEntryRow} entry=${plainEntry} />`);
  assert.ok(output.includes('value="debug"'), 'plain values are already visible, so prefill them');
  assert.ok(output.includes('type="text"'), 'plain values are not masked');
});

// ---------------------------------------------------------------------------
// Secrets — team/pipeline merge
// ---------------------------------------------------------------------------

const teamOnly = { name: 'NPM_TOKEN', canonical: 'NPM_TOKEN', kind: 'secret', scope: 'team' };
const teamShared = { name: 'SHARED', canonical: 'SHARED', kind: 'plain', scope: 'team', value: 'team-value' };
const pipeOverride = { name: 'SHARED', canonical: 'SHARED', kind: 'plain', scope: 'pipeline', value: 'pipe-value' };
const pipeOwn = { name: 'DB_URL', canonical: 'DB_URL', kind: 'secret', scope: 'pipeline' };

test('mergeEntries: inherits team entries the pipeline does not define', () => {
  const merged = mergeEntries([teamOnly], [pipeOwn]);
  const npm = merged.find(e => e.canonical === 'NPM_TOKEN');
  const db = merged.find(e => e.canonical === 'DB_URL');
  assert.equal(merged.length, 2);
  assert.equal(npm.inherited, true, 'team-only entry is inherited');
  assert.equal(db.inherited, false, 'pipeline entry is not inherited');
});

test('mergeEntries: a pipeline entry shadows the team entry, listed once', () => {
  const merged = mergeEntries([teamShared], [pipeOverride]);
  assert.equal(merged.length, 1, 'shadowed team entry must not be listed twice');
  assert.equal(merged[0].value, 'pipe-value', 'pipeline value wins');
  assert.equal(merged[0].overrides, true, 'should be flagged as overriding');
  assert.equal(merged[0].inherited, false);
});

test('mergeEntries: sorts by name and tolerates empty scopes', () => {
  const merged = mergeEntries([teamShared, teamOnly], [pipeOwn]);
  assert.deepEqual(merged.map(e => e.canonical), ['DB_URL', 'NPM_TOKEN', 'SHARED']);
  assert.deepEqual(mergeEntries(null, null), []);
  assert.equal(mergeEntries([teamOnly], []).length, 1);
});

test('EntryRow: inherited entry shows an inherited badge and no delete', () => {
  const entry = { ...teamOnly, inherited: true, overrides: false };
  const output = render(html`<${EntryRow} entry=${entry} canWrite=${true} showScope=${true} tc="main" />`);
  assert.ok(output.includes('inherited'), 'should show the inherited badge');
  assert.ok(!output.includes('delete-secret'), 'inherited entries are managed on the team, not here');
  assert.ok(!output.includes('edit-secret'), 'inherited entries are edited on the team, not here');
});

test('EntryRow: overriding entry is badged and still deletable', () => {
  const entry = { ...pipeOverride, inherited: false, overrides: true };
  const output = render(html`<${EntryRow} entry=${entry} canWrite=${true} showScope=${true} tc="main" />`);
  assert.ok(output.includes('overrides team'), 'should show the override badge');
  assert.ok(output.includes('delete-secret'), 'its own entry stays deletable');
});

test('EntryRow: omits the scope column at team scope', () => {
  const output = render(html`<${EntryRow} entry=${plainEntry} canWrite=${true} showScope=${false} />`);
  assert.ok(!output.includes('inherited'), 'team view has no scope column');
});

// A name that exists only on the team is a valid override, not a conflict.
test('NewEntryRow: flags an override instead of blocking it', () => {
  const output = render(html`<${NewEntryRow} existing=${[]} teamEntries=${[teamShared]} showScope=${true} />`);
  assert.ok(output.includes('id="secret-name"'), 'renders the form');
  assert.ok(!output.includes('already exists'), 'a team-level name must not read as a duplicate');
});

// ---------------------------------------------------------------------------
// Secrets — panel wiring
//
// SecretsPanel renders nothing until its fetch resolves and render-to-string
// never runs an effect, so the load and mutation paths are exercised through
// the functions the panel delegates to.
// ---------------------------------------------------------------------------

// stubFetch answers by URL, so a test can fail one leg of the pipeline fetch
// without failing the other. Returns the list of URLs that were requested.
function stubFetch(routes) {
  const seen = [];
  globalThis.fetch = (url, opts) => {
    seen.push({ url, opts });
    const body = routes[url];
    if (body === undefined) return Promise.reject(new Error('unexpected request: ' + url));
    if (body instanceof Error) {
      return Promise.resolve({
        ok: false, status: 500, statusText: 'Error',
        headers: { get: () => null },
        json: () => Promise.resolve({ error: body.message }),
        text: () => Promise.resolve('{}'),
      });
    }
    return Promise.resolve({
      ok: true, status: 200, statusText: 'OK',
      headers: { get: () => null },
      json: () => Promise.resolve(body),
      text: () => Promise.resolve(JSON.stringify(body)),
    });
  };
  return seen;
}

async function withFetch(routes, fn) {
  const original = globalThis.fetch;
  const seen = stubFetch(routes);
  try {
    return await fn(seen);
  } finally {
    globalThis.fetch = original;
  }
}

test('loadEntries: team scope issues one request and owns everything it shows', async () => {
  await withFetch({ '/teams/main/secrets': { data: [teamOnly, teamShared] } }, async (seen) => {
    const { entries, ownEntries } = await loadEntries('main');
    assert.equal(seen.length, 1, 'team scope must not fetch a pipeline');
    assert.deepEqual(entries.map(e => e.canonical), ['NPM_TOKEN', 'SHARED']);
    assert.deepEqual(ownEntries, entries, 'a team owns every entry it shows');
  });
});

test('loadEntries: pipeline scope fetches both scopes and merges them', async () => {
  const routes = {
    '/teams/main/pipelines/web/secrets': { data: [pipeOverride, pipeOwn] },
    '/teams/main/secrets': { data: [teamOnly, teamShared] },
  };
  await withFetch(routes, async (seen) => {
    const { entries, ownEntries } = await loadEntries('main', 'web');
    assert.equal(seen.length, 2, 'the effective set needs both scopes');

    assert.deepEqual(entries.map(e => e.canonical), ['DB_URL', 'NPM_TOKEN', 'SHARED']);
    assert.equal(entries.find(e => e.canonical === 'NPM_TOKEN').inherited, true);
    assert.equal(entries.find(e => e.canonical === 'SHARED').overrides, true,
      'the pipeline entry shadows the team one');
    assert.equal(entries.find(e => e.canonical === 'SHARED').value, 'pipe-value');

    assert.deepEqual(ownEntries.map(e => e.canonical), ['SHARED', 'DB_URL'],
      'only the pipeline entries are owned here');
  });
});

// A team a viewer cannot read must not blank the page: the pipeline's own
// entries are still worth showing.
test('loadEntries: a failing team fetch leaves the pipeline entries', async () => {
  const routes = {
    '/teams/main/pipelines/web/secrets': { data: [pipeOwn] },
    '/teams/main/secrets': new Error('nope'),
  };
  await withFetch(routes, async () => {
    const { entries, ownEntries } = await loadEntries('main', 'web');
    assert.deepEqual(entries.map(e => e.canonical), ['DB_URL']);
    assert.deepEqual(ownEntries.map(e => e.canonical), ['DB_URL']);
  });
});

test('loadEntries: a failing pipeline fetch still shows what is inherited', async () => {
  const routes = {
    '/teams/main/pipelines/web/secrets': new Error('nope'),
    '/teams/main/secrets': { data: [teamOnly] },
  };
  await withFetch(routes, async () => {
    const { entries, ownEntries } = await loadEntries('main', 'web');
    assert.deepEqual(entries.map(e => e.canonical), ['NPM_TOKEN']);
    assert.equal(entries[0].inherited, true);
    assert.deepEqual(ownEntries, [], 'the pipeline owns nothing it could not read');
  });
});

test('saveEntry: posts to the scope it was given', async () => {
  await withFetch({ '/teams/main/secrets': {} }, async (seen) => {
    await saveEntry('main', null, { name: 'GITHUB_TOKEN', value: 'ghp_new', kind: 'secret' });
    assert.equal(seen[0].opts.method, 'POST');
    assert.deepEqual(JSON.parse(seen[0].opts.body),
      { name: 'GITHUB_TOKEN', value: 'ghp_new', kind: 'secret' });
  });

  await withFetch({ '/teams/main/pipelines/web/secrets': {} }, async (seen) => {
    await saveEntry('main', 'web', { name: 'DB_URL', value: 'x', kind: 'secret' });
    assert.equal(seen[0].url, '/teams/main/pipelines/web/secrets');
  });
});

// Replacing a value is the same request as creating one: the server upserts,
// so the entry is never absent in between.
test('saveEntry: a replace is an ordinary set of the same name', async () => {
  await withFetch({ '/teams/main/secrets': {} }, async (seen) => {
    await saveEntry('main', null, { name: 'GITHUB_TOKEN', value: 'first', kind: 'secret' });
    await saveEntry('main', null, { name: 'GITHUB_TOKEN', value: 'second', kind: 'secret' });
    assert.equal(seen.length, 2);
    assert.equal(seen[0].url, seen[1].url, 'no separate update endpoint');
    assert.equal(seen[1].opts.method, 'POST');
    assert.equal(JSON.parse(seen[1].opts.body).value, 'second');
  });
});

test('removeEntry: deletes the name, escaped, from the right scope', async () => {
  await withFetch({ '/teams/main/pipelines/web/secrets/A%20B': {} }, async (seen) => {
    await removeEntry('main', 'web', 'A B');
    assert.equal(seen[0].opts.method, 'DELETE');
  });
});
