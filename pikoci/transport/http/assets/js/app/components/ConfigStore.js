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

// ---------------------------------------------------------------------------
// ConfigPanel – the shared table + form, used for both team and pipeline scope.
// Passing pn switches it to the pipeline scope.
// ---------------------------------------------------------------------------

export function ConfigPanel({ tc, pn }) {
  const [entries, setEntries] = useState([]);
  const [loaded, setLoaded] = useState(false);
  const [showNew, setShowNew] = useState(false);

  const canWrite = hasTeamRole(tc, 'maintain');

  const load = () => {
    fetchConfig(tc, pn)
      .then(data => { setEntries(data || []); setLoaded(true); })
      .catch(() => setLoaded(true));
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
      setEntries(prev => prev.filter(e => e.canonical !== entry.canonical));
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
        ? ' Pipeline entries override team entries with the same name.'
        : ' Team entries are available to every pipeline in the team.'}
      ${' '}Reference one from a pipeline with
      ${' '}<code>secret "pikoci" { key = "NAME" }</code>.
    </p>

    <table class="table">
      <thead>
        <tr>
          <th scope="col" class="col-4">Name</th>
          <th scope="col" class="col-2">Kind</th>
          <th scope="col" class="col-4">Value</th>
          <th scope="col" class="col-2">Options</th>
        </tr>
      </thead>
      <tbody>
        ${showNew && html`
          <${NewEntryRow} onAdd=${onAdd} onCancel=${() => setShowNew(false)} existing=${entries} />
        `}
        ${entries.length === 0 && !showNew && html`
          <tr><td colspan="4" class="text-muted">No entries yet.</td></tr>
        `}
        ${entries.map(e => html`
          <${EntryRow} key=${e.canonical} entry=${e} canWrite=${canWrite} onDelete=${onDelete} />
        `)}
      </tbody>
    </table>
  `;
}

// Exported for tests: ConfigPanel renders nothing until its fetch resolves,
// so the rows are the testable surface.
export function EntryRow({ entry, canWrite, onDelete }) {
  const isSecret = entry.kind === 'secret';

  return html`
    <tr>
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
      <td>
        ${canWrite && html`
          <button type="button" class="btn btn-sm btn-danger delete-config"
            data-name=${entry.canonical} onClick=${() => onDelete(entry)}>
            <i class="bi bi-trash"></i>
          </button>
        `}
      </td>
    </tr>
  `;
}

export function NewEntryRow({ onAdd, onCancel, existing }) {
  const [name, setName] = useState('');
  const [value, setValue] = useState('');
  const [isSecret, setIsSecret] = useState(true);
  const [saving, setSaving] = useState(false);

  const trimmed = name.trim();
  const duplicate = existing.some(e => e.canonical === trimmed);
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
