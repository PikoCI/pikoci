'use strict';

import { html } from 'htm/preact';
import { useState, useEffect, useRef } from 'preact/hooks';
import { fetchWorkers, fetchVersion, deleteWorker } from '../api.js';
import { useRequireAuth } from '../hooks.js';
import { pikoTimeAgo } from '../utils.js';

export function WorkersList() {
  useRequireAuth({ adminOnly: true });

  const [workers, setWorkers] = useState([]);
  const [serverVersion, setServerVersion] = useState('');
  const [serverCommit, setServerCommit] = useState('');

  useEffect(() => {
    fetchVersion()
      .then(data => {
        setServerVersion(data.version || '');
        setServerCommit(data.commit || '');
      })
      .catch(() => {});

    fetchWorkers()
      .then(data => setWorkers(data || []))
      .catch(() => {});
  }, []);

  const hasOutdated = serverVersion && workers.some(w =>
    w.version !== serverVersion || w.commit !== serverCommit
  );

  const onDelete = (e, name) => {
    e.preventDefault();
    deleteWorker(name).then(() => {
      setWorkers(prev => prev.filter(w => w.name !== name));
    }).catch(() => {});
  };

  return html`
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h1 class="h4 fw-bold mb-0">Workers</h1>
    </div>
    <div id="worker-health-banner-container">
      ${hasOutdated ? html`
        <div class="piko-worker-banner mb-3">
          \u26A0 Some workers are running an outdated version. Restart them to pick up the latest release.
        </div>
      ` : null}
    </div>
    <table class="table">
      <thead>
        <tr>
          <th>Name</th>
          <th>Status</th>
          <th>Team</th>
          <th>Tags</th>
          <th>Platform</th>
          <th>Version</th>
          <th>Uptime</th>
          <th>Last Seen</th>
          <th></th>
        </tr>
      </thead>
      <tbody id="workers-table-body">
        ${workers.map(w => html`
          <${WorkerRow}
            key=${w.name}
            worker=${w}
            serverVersion=${serverVersion}
            serverCommit=${serverCommit}
            onDelete=${onDelete}
          />
        `)}
      </tbody>
    </table>
  `;
}

function WorkerRow({ worker, serverVersion, serverCommit, onDelete }) {
  const rowRef = useRef(null);
  const isOutdated = serverVersion && (worker.version !== serverVersion || worker.commit !== serverCommit);

  // Bootstrap tooltip init + cleanup
  useEffect(() => {
    if (!rowRef.current) return;
    const tooltipEls = rowRef.current.querySelectorAll('[data-bs-toggle="tooltip"]');
    const tooltips = [];
    tooltipEls.forEach(el => {
      if (window.bootstrap?.Tooltip) {
        tooltips.push(new window.bootstrap.Tooltip(el));
      }
    });
    return () => {
      tooltips.forEach(t => t.dispose());
    };
  });

  return html`
    <tr ref=${rowRef}>
      <td>${worker.name}</td>
      <td>
        ${worker.status === 'healthy'
          ? html`<span class="piko-badge piko-badge-succeeded">healthy</span>`
          : html`<span class="piko-badge piko-badge-started">stale</span>`
        }
      </td>
      <td>${worker.team_canonical || html`<span class="text-muted">Global</span>`}</td>
      <td>
        ${worker.tags && worker.tags.length > 0 ? html`
          ${worker.tags.map(tag => html`<span class="piko-badge piko-badge-pending">${tag}</span> `)}
          ${worker.exclusive_tags ? html`<span class="piko-badge piko-badge-started">exclusive</span>` : null}
        ` : '-'}
      </td>
      <td>${worker.os}/${worker.arch} (${worker.go_version})</td>
      <td>
        ${worker.version}${worker.commit && worker.commit !== 'unknown' ? ' (' + worker.commit + ')' : ''}
        ${isOutdated ? html`
          <span class="text-warning ms-1" title=${'Worker version does not match server (server: ' + serverVersion + ' (' + serverCommit + '))'} data-bs-toggle="tooltip">
            <i class="bi bi-exclamation-triangle-fill"></i>
          </span>
        ` : null}
      </td>
      <td>${pikoTimeAgo(worker.started_at)}</td>
      <td>${pikoTimeAgo(worker.last_ping_at)}</td>
      <td>
        ${worker.status === 'stale' ? html`
          <button class="btn btn-sm btn-outline-danger delete-worker" data-name=${worker.name} onClick=${(e) => onDelete(e, worker.name)}>
            <i class="bi bi-trash"></i>
          </button>
        ` : null}
      </td>
    </tr>
  `;
}
