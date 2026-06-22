import test from 'node:test';
import assert from 'node:assert/strict';

import {
  fetchInterval,
  durationToString,
  processLogs,
  pikoTimeAgo,
  parseHCLErrors,
  blockTypes,
  sortBuilds,
  selectActiveBuild,
  versionRef,
} from '../../pikoci/transport/http/assets/js/app/utils.js';

// --- fetchInterval ---

test('fetchInterval equals 2000', () => {
  assert.equal(fetchInterval, 2000);
});

// --- durationToString ---

test('durationToString: zero returns 00:00:00', () => {
  assert.equal(durationToString(0), '00:00:00');
});

test('durationToString: 1 second (1e9 nanoseconds)', () => {
  assert.equal(durationToString(1e9), '00:00:01');
});

test('durationToString: 1 minute', () => {
  assert.equal(durationToString(60e9), '00:01:00');
});

test('durationToString: 1 hour', () => {
  assert.equal(durationToString(3600e9), '01:00:00');
});

test('durationToString: mixed 1h 23m 45s', () => {
  const ns = (1 * 3600 + 23 * 60 + 45) * 1e9;
  assert.equal(durationToString(ns), '01:23:45');
});

test('durationToString: 10 hours pads correctly', () => {
  assert.equal(durationToString(10 * 3600e9), '10:00:00');
});

test('durationToString: sub-second duration returns 00:00:00', () => {
  // 500ms = 5e8 ns, floors to 0 seconds
  assert.equal(durationToString(5e8), '00:00:00');
});

// --- processLogs ---

test('processLogs: returns falsy input as-is', () => {
  assert.equal(processLogs(''), '');
  assert.equal(processLogs(null), null);
  assert.equal(processLogs(undefined), undefined);
});

test('processLogs: text without \\r unchanged', () => {
  assert.equal(processLogs('line1\nline2'), 'line1\nline2');
});

test('processLogs: \\r keeps last segment', () => {
  assert.equal(processLogs('old\rnew'), 'new');
});

test('processLogs: multiple \\r per line', () => {
  assert.equal(processLogs('a\rb\rc'), 'c');
});

test('processLogs: multiline with mixed \\r', () => {
  assert.equal(processLogs('ok\nfoo\rbar\nbaz'), 'ok\nbar\nbaz');
});

// --- pikoTimeAgo ---

test('pikoTimeAgo: null returns never', () => {
  assert.equal(pikoTimeAgo(null), 'never');
});

test('pikoTimeAgo: empty string returns never', () => {
  assert.equal(pikoTimeAgo(''), 'never');
});

test('pikoTimeAgo: zero date returns never', () => {
  assert.equal(pikoTimeAgo('0001-01-01T00:00:00Z'), 'never');
});

test('pikoTimeAgo: just now', () => {
  const now = new Date().toISOString();
  assert.equal(pikoTimeAgo(now), 'just now');
});

test('pikoTimeAgo: minutes ago', () => {
  const fiveMinAgo = new Date(Date.now() - 5 * 60 * 1000).toISOString();
  assert.equal(pikoTimeAgo(fiveMinAgo), '5m ago');
});

test('pikoTimeAgo: hours ago', () => {
  const twoHoursAgo = new Date(Date.now() - 2 * 3600 * 1000).toISOString();
  assert.equal(pikoTimeAgo(twoHoursAgo), '2h ago');
});

test('pikoTimeAgo: days ago', () => {
  const threeDaysAgo = new Date(Date.now() - 3 * 86400 * 1000).toISOString();
  assert.equal(pikoTimeAgo(threeDaysAgo), '3d ago');
});

// --- parseHCLErrors ---

test('parseHCLErrors: line/col error', () => {
  const input = 'pipeline.hcl:5,3-10: Missing argument; The argument "name" is required.';
  const result = parseHCLErrors(input);
  assert.equal(result.length, 1);
  assert.equal(result[0].line, 5);
  assert.equal(result[0].colStart, 3);
  assert.equal(result[0].colEnd, 10);
  assert.ok(result[0].message.includes('Missing argument'));
});

test('parseHCLErrors: stdin line/col error', () => {
  const input = '<stdin>:2,1-3: Unsupported block; Blocks of type "foo" are not expected here.';
  const result = parseHCLErrors(input);
  assert.equal(result.length, 1);
  assert.equal(result[0].line, 2);
  assert.equal(result[0].colStart, 1);
  assert.equal(result[0].colEnd, 3);
});

test('parseHCLErrors: source resolution error', () => {
  const input = 'failed to resolve source for resource_type "git": no worker available';
  const result = parseHCLErrors(input);
  assert.equal(result.length, 1);
  assert.equal(result[0].blockType, 'resource_type');
  assert.equal(result[0].blockName, 'git');
  assert.equal(result[0].attribute, 'source');
  assert.ok(result[0].message.includes('no worker available'));
});

test('parseHCLErrors: fallback for unrecognized error', () => {
  const input = 'something went wrong';
  const result = parseHCLErrors(input);
  assert.equal(result.length, 1);
  assert.equal(result[0].line, 1);
  assert.equal(result[0].message, 'something went wrong');
});

test('parseHCLErrors: empty string returns empty array', () => {
  assert.deepEqual(parseHCLErrors(''), []);
});

test('parseHCLErrors: whitespace-only returns empty array', () => {
  assert.deepEqual(parseHCLErrors('   '), []);
});

// --- sortBuilds ---

test('sortBuilds: simple numbers descending', () => {
  const builds = [{ number: '1' }, { number: '3' }, { number: '2' }];
  const sorted = sortBuilds(builds);
  assert.deepEqual(sorted.map(b => b.number), ['3', '2', '1']);
});

test('sortBuilds: major.minor (retries) descending', () => {
  const builds = [
    { number: '1.1' },
    { number: '2.1' },
    { number: '1.2' },
    { number: '2.2' },
  ];
  const sorted = sortBuilds(builds);
  assert.deepEqual(sorted.map(b => b.number), ['2.2', '2.1', '1.2', '1.1']);
});

test('sortBuilds: empty array', () => {
  assert.deepEqual(sortBuilds([]), []);
});

test('sortBuilds: does not mutate original', () => {
  const builds = [{ number: '2' }, { number: '1' }];
  const sorted = sortBuilds(builds);
  assert.equal(builds[0].number, '2');
  assert.notStrictEqual(sorted, builds);
});

// --- selectActiveBuild ---

test('selectActiveBuild: returns null for empty', () => {
  assert.equal(selectActiveBuild([], null), null);
  assert.equal(selectActiveBuild(null, null), null);
});

test('selectActiveBuild: finds by requestedID', () => {
  const builds = [
    { id: 1, status: 'succeeded' },
    { id: 2, status: 'started' },
    { id: 3, status: 'succeeded' },
  ];
  const result = selectActiveBuild(builds, 3);
  assert.equal(result.id, 3);
});

test('selectActiveBuild: requestedID as string matches', () => {
  const builds = [{ id: 5, status: 'succeeded' }];
  const result = selectActiveBuild(builds, '5');
  assert.equal(result.id, 5);
});

test('selectActiveBuild: prefers started build when no requestedID', () => {
  const builds = [
    { id: 1, status: 'succeeded' },
    { id: 2, status: 'started' },
  ];
  const result = selectActiveBuild(builds, null);
  assert.equal(result.id, 2);
});

test('selectActiveBuild: prefers pending build', () => {
  const builds = [
    { id: 1, status: 'succeeded' },
    { id: 2, status: 'pending' },
  ];
  const result = selectActiveBuild(builds, null);
  assert.equal(result.id, 2);
});

test('selectActiveBuild: falls back to first build', () => {
  const builds = [
    { id: 1, status: 'succeeded' },
    { id: 2, status: 'failed' },
  ];
  const result = selectActiveBuild(builds, null);
  assert.equal(result.id, 1);
});

// --- versionRef ---

test('versionRef: null returns empty string', () => {
  assert.equal(versionRef(null), '');
});

test('versionRef: undefined returns empty string', () => {
  assert.equal(versionRef(undefined), '');
});

test('versionRef: string returns itself', () => {
  assert.equal(versionRef('abc123'), 'abc123');
});

test('versionRef: empty string returns empty string', () => {
  assert.equal(versionRef(''), '');
});

test('versionRef: object with ref', () => {
  assert.equal(versionRef({ ref: 'main' }), 'main');
});

test('versionRef: object with digest', () => {
  assert.equal(versionRef({ digest: 'sha256:abc' }), 'sha256:abc');
});

test('versionRef: object with tag', () => {
  assert.equal(versionRef({ tag: 'v1.0' }), 'v1.0');
});

test('versionRef: object with version string', () => {
  assert.equal(versionRef({ version: '2.0.1' }), '2.0.1');
});

test('versionRef: ref takes priority over digest', () => {
  assert.equal(versionRef({ ref: 'main', digest: 'sha256:abc' }), 'main');
});

test('versionRef: fallback to first key:value', () => {
  assert.equal(versionRef({ custom: 'val' }), 'custom: val');
});

test('versionRef: empty object returns empty string', () => {
  assert.equal(versionRef({}), '');
});

// --- blockTypes ---

test('blockTypes is an array', () => {
  assert.ok(Array.isArray(blockTypes));
});

test('blockTypes has expected entries', () => {
  const types = blockTypes.map(b => b.type);
  assert.ok(types.includes('resource_type'));
  assert.ok(types.includes('resource'));
  assert.ok(types.includes('job'));
  assert.ok(types.includes('secret_type'));
  assert.ok(types.includes('variable'));
});

test('blockTypes entries have type, label, icon, letter', () => {
  for (const bt of blockTypes) {
    assert.ok(bt.type, 'missing type');
    assert.ok(bt.label, 'missing label');
    assert.ok(bt.icon, 'missing icon');
    assert.ok(bt.letter, 'missing letter');
  }
});
