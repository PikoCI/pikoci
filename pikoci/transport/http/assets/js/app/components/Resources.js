'use strict';

import { html } from 'htm/preact';
import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import { route } from 'preact-router';
import {
  fetchResource, fetchResourceVersions, triggerResource,
  pinResource, unpinResource, regenerateWebhookToken, triggerVersion,
  fetchTeam, fetchPipeline,
} from '../api.js';
import { apiInterval } from '../api.js';
import { isLoggedIn, isTeamMember, isTeamAdmin } from '../state.js';
import { useLoading, usePolling } from '../hooks.js';
import { fetchInterval, pikoTimeAgo } from '../utils.js';
import { showToast } from '../toast.js';
import { Breadcrumb } from './Layout.js';

export function ResourceVersions({ tc, pn, rCan }) {
  // No useRequireAuth — resource versions support public access for public pipelines

  const [resource, setResource] = useState(null);
  const [team, setTeam] = useState(null);
  const [pipeline, setPipeline] = useState(null);
  const [versions, setVersions] = useState([]);
  const [hasMore, setHasMore] = useState(false);
  const [oldestId, setOldestId] = useState(null);
  const [expanded, setExpanded] = useState({});
  const [webhookOpen, setWebhookOpen] = useState(false);
  const [loading, withLoading] = useLoading();
  const fetchingMore = useRef(false);

  const isMember = isTeamMember(tc);
  const isAdm = isTeamAdmin(tc);

  // Initial fetch
  useEffect(() => {
    if (isLoggedIn.value) {
      fetchTeam(tc).then(t => setTeam(t)).catch(() => {});
    }
    fetchPipeline(tc, pn).then(p => setPipeline(p)).catch(() => {});
    fetchResource(tc, pn, rCan).then(r => setResource(r)).catch(() => {});
    fetchResourceVersions(tc, pn, rCan).then(resp => {
      setVersions(resp.data || []);
      setHasMore(resp.meta?.has_more || false);
      setOldestId(resp.meta?.oldest_id || null);
    }).catch(() => {});
  }, [tc, pn, rCan]);

  // Polling
  const pollCallback = useCallback(() => {
    // Poll resource with isInterval
    apiInterval('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan)
      .then(resp => setResource(resp.data))
      .catch(() => {});

    // Poll versions: merge new ones, preserve existing
    fetchResourceVersions(tc, pn, rCan).then(resp => {
      const fresh = resp.data || [];
      setVersions(prev => {
        const byId = new Map(prev.map(v => [v.id, v]));
        fresh.forEach(v => {
          if (byId.has(v.id)) {
            // Update status of existing versions
            byId.set(v.id, { ...byId.get(v.id), ...v });
          } else {
            byId.set(v.id, v);
          }
        });
        // Sort by id descending
        return Array.from(byId.values()).sort((a, b) => b.id - a.id);
      });
    }).catch(() => {});
  }, [tc, pn, rCan]);
  usePolling(pollCallback, fetchInterval);

  // Scroll-based pagination
  useEffect(() => {
    const onScroll = () => {
      if (fetchingMore.current || !hasMore) return;
      if (window.scrollY + window.innerHeight >= document.documentElement.scrollHeight - 200) {
        fetchingMore.current = true;
        fetchResourceVersions(tc, pn, rCan, { before: oldestId }).then(resp => {
          const more = resp.data || [];
          setVersions(prev => {
            const byId = new Map(prev.map(v => [v.id, v]));
            more.forEach(v => { if (!byId.has(v.id)) byId.set(v.id, v); });
            return Array.from(byId.values()).sort((a, b) => b.id - a.id);
          });
          setHasMore(resp.meta?.has_more || false);
          setOldestId(resp.meta?.oldest_id || null);
        }).catch(() => {}).finally(() => { fetchingMore.current = false; });
      }
    };
    window.addEventListener('scroll', onScroll);
    return () => window.removeEventListener('scroll', onScroll);
  }, [tc, pn, rCan, hasMore, oldestId]);

  const onTriggerResource = () => {
    withLoading(async () => {
      await triggerResource(tc, pn, rCan);
      showToast('Resource check triggered', 'success');
    });
  };

  const onToggleWebhook = (e) => {
    e.preventDefault();
    setWebhookOpen(v => !v);
  };

  const webhookUrl = resource?.webhook_token
    ? window.location.origin + '/webhooks/' + resource.webhook_token
    : '';

  const onCopyWebhook = (e) => {
    e.preventDefault();
    navigator.clipboard.writeText(webhookUrl);
  };

  const onRegenerateWebhook = () => {
    withLoading(async () => {
      const resp = await regenerateWebhookToken(tc, pn, rCan);
      if (resp.token) {
        setResource(prev => ({ ...prev, webhook_token: resp.token }));
        showToast('Webhook token regenerated', 'success');
      }
    });
  };

  const onUnpin = () => {
    withLoading(async () => {
      await unpinResource(tc, pn, rCan);
      setResource(prev => ({ ...prev, pinned_version_id: null }));
      showToast('Resource unpinned', 'success');
    });
  };

  const toggleExpand = (id) => {
    setExpanded(prev => ({ ...prev, [id]: !prev[id] }));
  };

  const onTrackVersion = (e, versionID) => {
    e.preventDefault();
    e.stopPropagation();
    route('/teams/' + tc + '/pipelines/' + pn + '?version=' + versionID);
  };

  const onTriggerVersion = (e, versionID) => {
    e.preventDefault();
    e.stopPropagation();
    withLoading(async () => {
      await triggerVersion(tc, pn, rCan, versionID);
      showToast('Triggered downstream jobs with version #' + versionID, 'success');
    });
  };

  const onPinVersion = (e, versionID) => {
    e.preventDefault();
    e.stopPropagation();
    const isPinned = resource?.pinned_version_id === versionID;
    withLoading(async () => {
      if (isPinned) {
        await unpinResource(tc, pn, rCan);
        setResource(prev => ({ ...prev, pinned_version_id: null }));
        showToast('Resource unpinned', 'success');
      } else {
        await pinResource(tc, pn, rCan, versionID);
        setResource(prev => ({ ...prev, pinned_version_id: versionID }));
        showToast('Resource pinned to version #' + versionID, 'success');
      }
    });
  };

  if (!resource) return html`<div></div>`;

  const pinnedId = resource.pinned_version_id;
  const urlParams = new URLSearchParams(window.location.search);
  const trackedVersionId = urlParams.get('version') ? parseInt(urlParams.get('version'), 10) : null;

  // Build pinned banner KV
  let pinnedKV = '';
  if (pinnedId) {
    const pinnedModel = versions.find(v => v.id === pinnedId);
    if (pinnedModel && pinnedModel.version) {
      pinnedKV = Object.entries(pinnedModel.version).map(([k, v]) => k + ': ' + v).join('  ');
    }
  }

  return html`
    <${Breadcrumb} team=${team} pipeline=${pipeline} resource=${resource} />
    ${resource.logs ? html`
      <div class="alert alert-danger" role="alert">${resource.logs}</div>
    ` : null}

    <div class="d-flex align-items-center justify-content-between mb-3">
      <h1 class="h4 fw-bold mb-0">${resource.canonical}</h1>
      ${isMember ? html`
        <div class="btn-group">
          <button type="button" id="trigger-resource" class="btn btn-warning" disabled=${loading} onClick=${onTriggerResource}>
            <i class="bi bi-play-circle"></i> ${loading ? 'Triggering...' : 'Trigger Resource'}
          </button>
          ${isAdm ? html`
            <button type="button" class="btn btn-warning dropdown-toggle dropdown-toggle-split" data-bs-toggle="dropdown">
              <span class="visually-hidden">Toggle Dropdown</span>
            </button>
            <ul class="dropdown-menu dropdown-menu-end">
              <li><a class="dropdown-item" href="#" id="toggle-webhook-panel" onClick=${onToggleWebhook}>Webhook URL</a></li>
            </ul>
          ` : null}
        </div>
      ` : null}
    </div>

    ${webhookOpen ? html`
      <div id="webhook-panel" class="piko-webhook-panel">
        <label class="fw-bold">Webhook URL</label>
        <div class="d-flex gap-2 align-items-center">
          <code id="webhook-url" class="text-break">${webhookUrl}</code>
          <button id="copy-webhook" class="btn btn-sm btn-info" onClick=${onCopyWebhook}><i class="bi bi-clipboard"></i> Copy</button>
        </div>
        <div class="mt-2 d-flex justify-content-between align-items-center">
          <small class="text-muted">Regenerating invalidates the current URL</small>
          <button id="regenerate-webhook" class="btn btn-sm btn-outline-danger" disabled=${loading} onClick=${onRegenerateWebhook}>
            <i class="bi bi-arrow-clockwise"></i> ${loading ? 'Regenerating...' : 'Regenerate Token'}
          </button>
        </div>
      </div>
    ` : null}

    <div class="piko-resource-info">
      <span class="piko-build-label">Last checked</span> (${resource.check_interval || '@every 1m'})${(!resource.last_check || resource.last_check.startsWith('0001-01-01')) ? ':' : ' at:'} ${' '}
      <span class="piko-time-ago" data-time=${resource.last_check} title=${resource.last_check ? new Date(resource.last_check).toLocaleString() : ''} style="color: var(--text-primary);">
        ${pikoTimeAgo(resource.last_check)}
      </span>
    </div>

    <div id="pinned-version-banner">
      ${pinnedId ? html`
        <div class="piko-pinned-banner">
          <span>
            <i class="bi bi-pin-fill"></i> Pinned to version #${pinnedId}
            ${pinnedKV ? html` — <span class="piko-version-kv">${pinnedKV}</span>` : null}
          </span>
          ${isMember ? html`
            <button id="unpin-banner" class="btn btn-sm btn-outline-warning" onClick=${onUnpin}>Unpin</button>
          ` : null}
        </div>
      ` : null}
    </div>

    <div id="resource-versions">
      ${versions.map((v, idx) => {
        const isFirst = idx === 0;
        const isPinned = pinnedId === v.id;
        const isTracked = trackedVersionId === v.id;
        const isExpanded = expanded[v.id] || false;

        return html`
          <div class="piko-version-row" key=${v.id}>
            <div class="piko-version-row-header" onClick=${(e) => {
              if (!e.target.closest('.piko-version-actions')) toggleExpand(v.id);
            }}>
              <span style="display:flex;align-items:center;gap:8px;">
                ${isFirst ? html`<span class="piko-badge piko-badge-succeeded">latest</span>` : null}
                ${isPinned ? html`<span class="piko-badge piko-badge-pending">pinned</span>` : null}
                ${isTracked ? html`<span class="piko-badge piko-badge-primary"><i class="bi bi-signpost-2"></i> tracked</span>` : null}
                ${v.status ? html`<span class="piko-version-status-dot" style=${'background:var(--status-' + v.status + ');'} title=${v.status}></span>` : null}
                <span class="piko-version-kv">
                  ${v.version ? Object.entries(v.version).map(([k, val]) => html`${k}: <span>${val}</span> `) : null}
                </span>
              </span>
              ${isMember ? html`
                <span class="piko-version-actions">
                  <button class="btn btn-sm btn-outline-primary track-version" title="Track this version through the pipeline" onClick=${(e) => onTrackVersion(e, v.id)}><i class="bi bi-signpost-2"></i></button>
                  <button class="btn btn-sm btn-outline-warning trigger-version" title="Trigger downstream jobs with this version" onClick=${(e) => onTriggerVersion(e, v.id)}><i class="bi bi-play-fill"></i></button>
                  <button class="btn btn-sm ${isPinned ? 'btn-warning' : 'btn-outline-secondary'} pin-version" title=${isPinned ? 'Unpin version' : 'Pin to this version'} onClick=${(e) => onPinVersion(e, v.id)}>
                    <i class="bi ${isPinned ? 'bi-pin-fill' : 'bi-pin'}"></i>
                  </button>
                </span>
              ` : null}
            </div>
            <div class="piko-version-row-body" style=${isExpanded ? 'display:block;' : 'display:none;'}>
              <table class="piko-version-table">
                <tr><td class="piko-version-key">ID</td><td>${v.id}</td></tr>
                ${v.version ? Object.entries(v.version).map(([k, val]) => html`
                  <tr><td class="piko-version-key">${k}</td><td>${val}</td></tr>
                `) : null}
              </table>
            </div>
          </div>
        `;
      })}
    </div>
  `;
}
