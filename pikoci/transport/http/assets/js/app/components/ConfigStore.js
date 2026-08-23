'use strict';

import { html } from 'htm/preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { hasTeamRole } from '../state.js';
import { fetchConfig, setConfig, deleteConfig, fetchTeam, fetchPipeline } from '../api.js';
import { useRequireAuth } from '../hooks.js';
import { showToast } from '../toast.js';
import { Breadcrumb } from './Layout.js';

// Mirrors the server-side rule in pikoci/config_store.go: names are used
// verbatim as lookup keys, so they must look like environment variables.
const NAME_RE = /^[A-Za-z_][A-Za-z0-9_]{0,254}$/;

// mergeEntries produces the effective set a pipeline sees, mirroring the
// server-side precedence in Repository.StoredValues: a pipeline entry shadows
// a team entry of the same name.
//
// Shadowed team entries are dropped rather than listed twice; the surviving
// pipeline row carries an `overrides` flag so the shadowing is still visible.
export function mergeEntries(teamEntries, pipelineEntries) {
  const teamNames = new Set((teamEntries || []).map(e => e.canonical));
  const pipelineNames = new Set((pipelineEntries || []).map(e => e.canonical));

  const own = (pipelineEntries || []).map(e => ({
    ...e, inherited: false, overrides: teamNames.has(e.canonical),
  }));

  const inherited = (teamEntries || [])
    .filter(e => !pipelineNames.has(e.canonical))
    .map(e => ({ ...e, inherited: true, overrides: false }));

  return [...own, ...inherited].sort((a, b) => a.canonical.localeCompare(b.canonical));
}

// ---------------------------------------------------------------------------
// ConfigPanel – the shared table + form, used for both team and pipeline scope.
// Passing pn switches it to the pipeline scope.
// ---------------------------------------------------------------------------

export function ConfigPanel({ tc, pn }) {
  const [entries, setEntries] = useState([]);
  // Entries owned by this scope, as opposed to inherited ones. The new-entry
  // form checks duplicates against these: a name that exists only at team
  // level is a valid override, not a conflict.
  const [ownEntries, setOwnEntries] = useState([]);
  const [loaded, setLoaded] = useState(false);
  const [showNew, setShowNew] = useState(false);

  const canWrite = hasTeamRole(tc, 'maintain');

  const load = () => {
    if (!pn) {
      fetchConfig(tc)
        .then(data => { setEntries(data || []); setOwnEntries(data || []); setLoaded(true); })
        .catch(() => setLoaded(true));
      return;
    }

    // A pipeline shows its effective set, so the team scope is fetched too.
    // Both endpoints require the same role on the same team, so this exposes
    // nothing the viewer could not already read.
    Promise.all([
      fetchConfig(tc, pn).catch(() => []),
      fetchConfig(tc).catch(() => []),
    ]).then(([own, team]) => {
      setOwnEntries(own || []);
      setEntries(mergeEntries(team, own));
      setLoaded(true);
    }).catch(() => setLoaded(true));
  };

  useEffect(() => { load(); }, [tc, pn]);

  const onAdd = (entry) => {
    return setConfig(tc, pn, entry).then(() => {
      showToast(entry.kind === 'secret' ? 'Secret stored' : 'Value stored', 'success');
      setShowNew(false);
      load();
    });
  };

  const onDelete = (entry) => {
    const label = entry.kind === 'secret' ? 'secret' : 'value';
    if (!confirm("Are you sure you want to delete the " + label + " '" + entry.name + "'?")) return;
    deleteConfig(tc, pn, entry.canonical).then(() => {
      showToast('Deleted', 'success');
      // Reload rather than filtering: deleting a pipeline override should
      // reveal the team entry it was shadowing.
      load();
    });
  };

  if (!loaded) return null;

  return html`
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h3 class="h5 fw-bold mb-0">
        ${pn ? 'Pipeline Configuration' : 'Team Configuration'}
      </h3>
      ${canWrite && html`
        <button type="button" id="new-config" class="btn btn-success"
          onClick=${() => setShowNew(v => !v)}>
          <i class="bi bi-plus-lg"></i> New Entry
        </button>
      `}
    </div>

    <p class="text-muted small">
      Secrets are encrypted at rest and masked in build logs; their values are never
      shown again. Plain values are stored as-is and printed in build logs.
      ${pn
        ? ' This is the effective set for the pipeline: entries inherited from the team are shown alongside its own, which override them by name.'
        : ' Team entries are available to every pipeline in the team.'}
      ${' '}Reference one from a pipeline with
      ${' '}<code>secret "pikoci" { key = "NAME" }</code>.
    </p>

    <table class="table">
      <thead>
        <tr>
          <th scope="col" class="col-3">Name</th>
          <th scope="col" class="col-2">Kind</th>
          <th scope="col" class="col-3">Value</th>
          ${pn && html`<th scope="col" class="col-2">Scope</th>`}
          <th scope="col" class="col-2">Options</th>
        </tr>
      </thead>
      <tbody>
        ${showNew && html`
          <${NewEntryRow} onAdd=${onAdd} onCancel=${() => setShowNew(false)}
            existing=${ownEntries} teamEntries=${pn ? entries.filter(e => e.inherited) : []}
            showScope=${!!pn} />
        `}
        ${entries.length === 0 && !showNew && html`
          <tr><td colspan=${pn ? 5 : 4} class="text-muted">No entries yet.</td></tr>
        `}
        ${entries.map(e => html`
          <${EntryRow} key=${e.canonical} entry=${e} canWrite=${canWrite}
            onDelete=${onDelete} showScope=${!!pn} tc=${tc} />
        `)}
      </tbody>
    </table>
  `;
}

// Exported for tests: ConfigPanel renders nothing until its fetch resolves,
// so the rows are the testable surface.
export function EntryRow({ entry, canWrite, onDelete, showScope, tc }) {
  const isSecret = entry.kind === 'secret';

  return html`
    <tr class=${entry.inherited ? 'table-light' : ''}>
      <td><code>${entry.name}</code></td>
      <td>
        ${isSecret
          ? html`<span class="badge bg-warning text-dark"><i class="bi bi-lock-fill"></i> secret</span>`
          : html`<span class="badge bg-secondary">plain</span>`}
      </td>
      <td>
        ${isSecret
          ? html`<span class="text-muted" title="Secret values are never returned">••••••••</span>`
          : html`<code>${entry.value}</code>`}
      </td>
      ${showScope && html`
        <td>
          ${entry.inherited
            ? html`<span class="badge bg-light text-dark border" title="Defined on the team and inherited by every pipeline">
                <i class="bi bi-diagram-3"></i> inherited
              </span>`
            : entry.overrides
              ? html`<span class="badge bg-primary" title="Shadows a team entry of the same name">
                  <i class="bi bi-arrow-up-circle"></i> overrides team
                </span>`
              : html`<span class="badge bg-secondary">pipeline</span>`}
        </td>
      `}
      <td>
        ${entry.inherited
          ? html`<a class="btn btn-sm btn-outline-secondary" href=${'/teams/' + tc + '/config'} data-native
              title="Inherited entries are managed on the team"
              onClick=${(e) => { e.preventDefault(); route('/teams/' + tc + '/config'); }}>
              <i class="bi bi-box-arrow-up-right"></i> Team
            </a>`
          : canWrite && html`
            <button type="button" class="btn btn-sm btn-danger delete-config"
              data-name=${entry.canonical} onClick=${() => onDelete(entry)}>
              <i class="bi bi-trash"></i>
            </button>
          `}
      </td>
    </tr>
  `;
}

export function NewEntryRow({ onAdd, onCancel, existing, teamEntries, showScope }) {
  const [name, setName] = useState('');
  const [value, setValue] = useState('');
  const [isSecret, setIsSecret] = useState(true);
  const [saving, setSaving] = useState(false);

  const trimmed = name.trim();
  // Only entries owned by this scope conflict. A name that exists at team
  // level is a deliberate override, so it is allowed and merely flagged.
  const duplicate = (existing || []).some(e => e.canonical === trimmed);
  const willOverride = (teamEntries || []).some(e => e.canonical === trimmed);
  const nameValid = NAME_RE.test(trimmed);
  const canSave = nameValid && !duplicate && value !== '' && !saving;

  const submit = () => {
    if (!canSave) return;
    setSaving(true);
    onAdd({ name: trimmed, value, kind: isSecret ? 'secret' : 'plain' })
      .catch(() => setSaving(false));
  };

  return html`
    <tr>
      <td>
        <input type="text" class="form-control" id="config-name" placeholder="GITHUB_TOKEN"
          value=${name} onInput=${e => setName(e.target.value)}
          onKeyDown=${e => { if (e.key === 'Enter') submit(); }} />
        ${trimmed !== '' && !nameValid && html`
          <div class="form-text text-danger">
            Letters, digits and underscores only, not starting with a digit.
          </div>
        `}
        ${duplicate && html`
          <div class="form-text text-danger">An entry named ${trimmed} already exists.</div>
        `}
        ${!duplicate && willOverride && html`
          <div class="form-text text-primary">
            Overrides the team entry of the same name for this pipeline.
          </div>
        `}
      </td>
      <td>
        <div class="form-check">
          <input class="form-check-input" type="checkbox" id="config-secret"
            checked=${isSecret} onChange=${e => setIsSecret(e.target.checked)} />
          <label class="form-check-label" for="config-secret">Secret</label>
        </div>
      </td>
      <td>
        <input type=${isSecret ? 'password' : 'text'} class="form-control" id="config-value"
          autocomplete="off" placeholder=${isSecret ? '' : 'debug'}
          value=${value} onInput=${e => setValue(e.target.value)}
          onKeyDown=${e => { if (e.key === 'Enter') submit(); }} />
      </td>
      ${showScope && html`<td></td>`}
      <td>
        <button type="button" id="save-config" class="btn btn-sm btn-success"
          disabled=${!canSave} onClick=${submit}>
          <i class="bi bi-check-lg"></i>
        </button>
        ${' '}
        <button type="button" class="btn btn-sm btn-secondary" onClick=${onCancel}>
          <i class="bi bi-x-lg"></i>
        </button>
      </td>
    </tr>
  `;
}

// ---------------------------------------------------------------------------
// PipelineConfig – standalone page for the pipeline scope.
// The team scope is rendered as a tab inside TeamShow instead.
// ---------------------------------------------------------------------------

export function PipelineConfig({ tc, pn }) {
  useRequireAuth();
  const [team, setTeam] = useState(null);
  const [pipeline, setPipeline] = useState(null);

  useEffect(() => {
    fetchTeam(tc).then(setTeam).catch(() => {});
    fetchPipeline(tc, pn).then(setPipeline).catch(() => {});
  }, [tc, pn]);

  if (!team || !pipeline) return null;

  return html`
    <${Breadcrumb} team=${team} pipeline=${pipeline} />
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h1 class="h4 fw-bold mb-0">${pipeline.name}</h1>
      <button type="button" class="btn btn-secondary"
        onClick=${() => route('/teams/' + tc + '/pipelines/' + pn)}>
        <i class="bi bi-arrow-left"></i> Back to Pipeline
      </button>
    </div>
    <${ConfigPanel} tc=${tc} pn=${pn} />
  `;
}
