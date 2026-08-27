'use strict';

import { html } from 'htm/preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { hasTeamRole } from '../state.js';
import { fetchSecrets, setSecret, deleteSecret, fetchTeam, fetchPipeline } from '../api.js';
import { useRequireAuth } from '../hooks.js';
import { showToast } from '../toast.js';
import { Breadcrumb } from './Layout.js';

// Mirrors the server-side rule in pikoci/secret_store.go: names are used
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
// Data wiring, kept out of the component so it can be tested directly: the
// unit tests render to a string, which never runs an effect.
// ---------------------------------------------------------------------------

// loadEntries returns the rows to display and, separately, the ones this scope
// owns. They differ only for a pipeline, where the displayed set also carries
// the team entries it inherits.
//
// A failing leg yields no entries rather than rejecting: a pipeline is still
// worth showing when the team scope cannot be read, and vice versa.
export function loadEntries(tc, pn) {
  if (!pn) {
    return fetchSecrets(tc).then(data => ({
      entries: data || [], ownEntries: data || [],
    }));
  }

  // A pipeline shows its effective set, so the team scope is fetched too.
  // Both endpoints require the same role on the same team, so this exposes
  // nothing the viewer could not already read.
  return Promise.all([
    fetchSecrets(tc, pn).catch(() => []),
    fetchSecrets(tc).catch(() => []),
  ]).then(([own, team]) => ({
    entries: mergeEntries(team, own),
    ownEntries: own || [],
  }));
}

// saveEntry stores an entry. The server treats a repeated name as a replace,
// so this is also the update path; nothing has to be deleted first.
export function saveEntry(tc, pn, entry) {
  return setSecret(tc, pn, entry);
}

export function removeEntry(tc, pn, canonical) {
  return deleteSecret(tc, pn, canonical);
}

// ---------------------------------------------------------------------------
// SecretsPanel – the shared table + form, used for both team and pipeline
// scope. Passing pn switches it to the pipeline scope.
// ---------------------------------------------------------------------------

export function SecretsPanel({ tc, pn }) {
  const [entries, setEntries] = useState([]);
  // Entries owned by this scope, as opposed to inherited ones. The new-entry
  // form checks duplicates against these: a name that exists only at team
  // level is a valid override, not a conflict.
  const [ownEntries, setOwnEntries] = useState([]);
  const [loaded, setLoaded] = useState(false);
  const [showNew, setShowNew] = useState(false);
  // Canonical name of the row being edited, or null.
  const [editing, setEditing] = useState(null);

  const canWrite = hasTeamRole(tc, 'maintain');

  const load = () => {
    loadEntries(tc, pn)
      .then(({ entries: rows, ownEntries: own }) => {
        setEntries(rows);
        setOwnEntries(own);
        setLoaded(true);
      })
      .catch(() => setLoaded(true));
  };

  useEffect(() => { load(); }, [tc, pn]);

  const onAdd = (entry) => {
    return saveEntry(tc, pn, entry).then(() => {
      showToast(entry.kind === 'secret' ? 'Secret stored' : 'Value stored', 'success');
      setShowNew(false);
      load();
    });
  };

  // Kind is taken from the stored entry, never from the form: a replace must
  // not be able to turn a secret into a plain value.
  const onSave = (entry, value) => {
    return saveEntry(tc, pn, { name: entry.name, value, kind: entry.kind }).then(() => {
      showToast(entry.kind === 'secret' ? 'Secret updated' : 'Value updated', 'success');
      setEditing(null);
      load();
    });
  };

  const onDelete = (entry) => {
    const label = entry.kind === 'secret' ? 'secret' : 'value';
    if (!confirm("Are you sure you want to delete the " + label + " '" + entry.name + "'?")) return;
    removeEntry(tc, pn, entry.canonical).then(() => {
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
        ${pn ? 'Pipeline Secrets' : 'Team Secrets'}
      </h3>
      ${canWrite && html`
        <button type="button" id="new-secret" class="btn btn-success"
          onClick=${() => { setEditing(null); setShowNew(v => !v); }}>
          <i class="bi bi-plus-lg"></i> New Entry
        </button>
      `}
    </div>

    <p class="text-muted small">
      Secrets are encrypted at rest and masked in build logs; their values are never
      shown again, though they can be replaced. Plain values are stored as-is and
      printed in build logs.
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
      <tbody id="secrets-body">
        ${showNew && html`
          <${NewEntryRow} onAdd=${onAdd} onCancel=${() => setShowNew(false)}
            existing=${ownEntries} teamEntries=${pn ? entries.filter(e => e.inherited) : []}
            showScope=${!!pn} />
        `}
        ${entries.length === 0 && !showNew && html`
          <tr><td colspan=${pn ? 5 : 4} class="text-muted">No entries yet.</td></tr>
        `}
        ${entries.map(e => (editing === e.canonical
          ? html`
            <${EditEntryRow} key=${e.canonical} entry=${e} onSave=${onSave}
              onCancel=${() => setEditing(null)} showScope=${!!pn} />
          `
          : html`
            <${EntryRow} key=${e.canonical} entry=${e} canWrite=${canWrite}
              onEdit=${() => { setShowNew(false); setEditing(e.canonical); }}
              onDelete=${onDelete} showScope=${!!pn} tc=${tc} />
          `))}
      </tbody>
    </table>
  `;
}

// Exported for tests: SecretsPanel renders nothing until its fetch resolves,
// so the rows are the testable surface.
export function EntryRow({ entry, canWrite, onEdit, onDelete, showScope, tc }) {
  const isSecret = entry.kind === 'secret';

  return html`
    <tr class=${'piko-secret-row' + (entry.inherited ? ' table-light' : '')} data-name=${entry.canonical}>
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
          ? html`<a class="btn btn-sm btn-outline-secondary" href=${'/teams/' + tc + '/secrets'} data-native
              title="Inherited entries are managed on the team"
              onClick=${(e) => { e.preventDefault(); route('/teams/' + tc + '/secrets'); }}>
              <i class="bi bi-box-arrow-up-right"></i> Team
            </a>`
          : canWrite && html`
            <button type="button" class="btn btn-sm btn-outline-primary edit-secret"
              data-name=${entry.canonical} title="Replace the value" onClick=${() => onEdit(entry)}>
              <i class="bi bi-pencil"></i>
            </button>
            ${' '}
            <button type="button" class="btn btn-sm btn-danger delete-secret"
              data-name=${entry.canonical} onClick=${() => onDelete(entry)}>
              <i class="bi bi-trash"></i>
            </button>
          `}
      </td>
    </tr>
  `;
}

// EditEntryRow replaces a row in place to swap its value. The name and the
// kind are fixed: the point is to replace a value without a window in which
// the entry does not exist, so re-creating it under a new shape is a delete
// followed by an add, not an edit.
export function EditEntryRow({ entry, onSave, onCancel, showScope }) {
  const isSecret = entry.kind === 'secret';
  // A secret's value is never returned, so there is nothing to prefill. A
  // plain value starts from what is stored, so a small correction is a small
  // edit rather than a retype.
  const [value, setValue] = useState(isSecret ? '' : (entry.value || ''));
  const [saving, setSaving] = useState(false);

  const canSave = value !== '' && !saving;

  const submit = () => {
    if (!canSave) return;
    setSaving(true);
    onSave(entry, value).catch(() => setSaving(false));
  };

  return html`
    <tr class="piko-secret-row" data-name=${entry.canonical}>
      <td><code>${entry.name}</code></td>
      <td>
        ${isSecret
          ? html`<span class="badge bg-warning text-dark"><i class="bi bi-lock-fill"></i> secret</span>`
          : html`<span class="badge bg-secondary">plain</span>`}
      </td>
      <td>
        <input type=${isSecret ? 'password' : 'text'} class="form-control" id="secret-edit-value"
          autocomplete="off" placeholder=${isSecret ? 'New value' : ''}
          value=${value} onInput=${e => setValue(e.target.value)}
          onKeyDown=${e => { if (e.key === 'Enter') submit(); }} />
      </td>
      ${showScope && html`<td></td>`}
      <td>
        <button type="button" id="save-secret-edit" class="btn btn-sm btn-success"
          disabled=${!canSave} onClick=${submit}>
          <i class="bi bi-check-lg"></i>
        </button>
        ${' '}
        <button type="button" id="cancel-secret-edit" class="btn btn-sm btn-secondary" onClick=${onCancel}>
          <i class="bi bi-x-lg"></i>
        </button>
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
        <input type="text" class="form-control" id="secret-name" placeholder="GITHUB_TOKEN"
          value=${name} onInput=${e => setName(e.target.value)}
          onKeyDown=${e => { if (e.key === 'Enter') submit(); }} />
        ${trimmed !== '' && !nameValid && html`
          <div class="form-text text-danger">
            Letters, digits and underscores only, not starting with a digit.
          </div>
        `}
        ${duplicate && html`
          <div class="form-text text-danger">
            An entry named ${trimmed} already exists. Edit it to replace its value.
          </div>
        `}
        ${!duplicate && willOverride && html`
          <div class="form-text text-primary">
            Overrides the team entry of the same name for this pipeline.
          </div>
        `}
      </td>
      <td>
        <div class="form-check">
          <input class="form-check-input" type="checkbox" id="secret-kind"
            checked=${isSecret} onChange=${e => setIsSecret(e.target.checked)} />
          <label class="form-check-label" for="secret-kind">Secret</label>
        </div>
      </td>
      <td>
        <input type=${isSecret ? 'password' : 'text'} class="form-control" id="secret-value"
          autocomplete="off" placeholder=${isSecret ? '' : 'debug'}
          value=${value} onInput=${e => setValue(e.target.value)}
          onKeyDown=${e => { if (e.key === 'Enter') submit(); }} />
      </td>
      ${showScope && html`<td></td>`}
      <td>
        <button type="button" id="save-secret" class="btn btn-sm btn-success"
          disabled=${!canSave} onClick=${submit}>
          <i class="bi bi-check-lg"></i>
        </button>
        ${' '}
        <button type="button" id="cancel-secret" class="btn btn-sm btn-secondary" onClick=${onCancel}>
          <i class="bi bi-x-lg"></i>
        </button>
      </td>
    </tr>
  `;
}

// ---------------------------------------------------------------------------
// PipelineSecrets – standalone page for the pipeline scope.
// The team scope is rendered as a tab inside TeamShow instead.
// ---------------------------------------------------------------------------

export function PipelineSecrets({ tc, pn }) {
  useRequireAuth();
  const [team, setTeam] = useState(null);
  const [pipeline, setPipeline] = useState(null);

  useEffect(() => {
    fetchTeam(tc).then(setTeam).catch(() => {});
    fetchPipeline(tc, pn).then(setPipeline).catch(() => {});
  }, [tc, pn]);

  if (!team || !pipeline) return null;

  // Going back is the breadcrumb's job, the same as on every other page.
  return html`
    <${Breadcrumb} team=${team} pipeline=${pipeline} secrets=${true} />
    <h1 class="h4 fw-bold mb-3">${pipeline.name}</h1>
    <${SecretsPanel} tc=${tc} pn=${pn} />
  `;
}
