'use strict';

import { html } from 'htm/preact';
import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import { route } from 'preact-router';
import {
  fetchPipeline, fetchPipelineImage, deletePipeline,
  pausePipeline, unpausePipeline, fetchResources,
  triggerResource, fetchResourceVersions, fetchVersionPath,
  triggerVersion, pinResource, unpinResource, fetchTeam,
} from '../api.js';
import { hasTeamRole, isLoggedIn } from '../state.js';
import { usePolling } from '../hooks.js';
import { showToast } from '../toast.js';
import { fetchInterval, pikoTimeAgo, versionRef } from '../utils.js';
import { PipelineGraph } from './PipelineGraph.js';
import { PipelineListView } from './PipelineListView.js';
import { PikoGraphZoom } from '../graph-zoom.js';
import { Breadcrumb } from './Layout.js';

// Re-export PipelineEdit from Editor.js (app.js imports it from here)
export { PipelineEdit } from './Editor.js';

// ---------------------------------------------------------------------------
// PipelineResourcesPanel (sub-component)
// ---------------------------------------------------------------------------

function PipelineResourcesPanel({ tc, pn, resources, isMember, onClose, onTrackVersion, trackedVersion, onRefreshResources }) {
  const [expandedResources, setExpandedResources] = useState({});
  const [versionLists, setVersionLists] = useState({});

  const basePath = '/teams/' + tc + '/pipelines/' + pn;

  const closePanel = (e) => {
    e.preventDefault();
    e.stopPropagation();
    onClose();
  };

  const checkNow = (e, canonical) => {
    e.preventDefault();
    e.stopPropagation();
    triggerResource(tc, pn, canonical).then(() => {
      showToast('Resource check triggered', 'success');
      if (onRefreshResources) onRefreshResources();
    }).catch(() => {
      showToast('Failed to trigger resource check', 'error');
    });
  };

  const toggleVersionsList = (e, canonical) => {
    e.preventDefault();
    e.stopPropagation();
    setExpandedResources(prev => {
      const next = { ...prev };
      if (next[canonical]) {
        delete next[canonical];
      } else {
        next[canonical] = true;
        fetchPanelVersions(canonical);
      }
      return next;
    });
  };

  const fetchPanelVersions = (canonical) => {
    fetchResourceVersions(tc, pn, canonical, { limit: 5 }).then(resp => {
      if (resp && resp.data) {
        setVersionLists(prev => ({ ...prev, [canonical]: resp.data }));
      }
    }).catch(() => {});
  };

  // Refresh expanded versions when trackedVersion changes
  useEffect(() => {
    Object.keys(expandedResources).forEach(canonical => {
      fetchPanelVersions(canonical);
    });
  }, [trackedVersion]);

  // Poll expanded version lists periodically
  const hasExpanded = Object.keys(expandedResources).length > 0;
  const refreshExpanded = useCallback(() => {
    return Promise.all(Object.keys(expandedResources).map(canonical => fetchPanelVersions(canonical)));
  }, [expandedResources]);
  usePolling(refreshExpanded, fetchInterval, hasExpanded);

  const handleTrack = (e, canonical, versionID, ref) => {
    e.preventDefault();
    e.stopPropagation();
    if (onTrackVersion) onTrackVersion(canonical, versionID, ref);
  };

  const handleTrigger = (e, canonical, versionID) => {
    e.preventDefault();
    e.stopPropagation();
    triggerVersion(tc, pn, canonical, versionID).then(() => {
      showToast('Triggered downstream jobs with version #' + versionID, 'success');
    }).catch(() => {});
  };

  const handlePin = (e, canonical, versionID, isPinned) => {
    e.preventDefault();
    e.stopPropagation();
    if (isPinned) {
      unpinResource(tc, pn, canonical).then(() => {
        showToast('Resource unpinned', 'success');
        if (onRefreshResources) onRefreshResources();
      }).catch(() => {});
    } else {
      pinResource(tc, pn, canonical, versionID).then(() => {
        showToast('Resource pinned to version #' + versionID, 'success');
        if (onRefreshResources) onRefreshResources();
      }).catch(() => {});
    }
  };

  const resourceUrl = (canonical) => basePath + '/resources/' + canonical + '/versions';

  return html`
    <div class="piko-resources-panel-header">
      <span class="fw-bold">Resources</span>
      <button type="button" id="close-resources-panel" onClick=${closePanel}
        style="background:none;border:none;color:var(--text-secondary);font-size:1rem;cursor:pointer;padding:0;line-height:1"><i class="bi bi-x-lg"></i></button>
    </div>
    <div class="piko-resources-panel-body">
      ${resources.length === 0 && html`
        <p class="piko-resource-card-meta small mb-0">No resources in this pipeline.</p>
      `}
      ${resources.map(r => {
        const trackedVersionID = trackedVersion ? trackedVersion.versionID : null;
        return html`
          <div class="piko-resource-card" data-canonical=${r.canonical}>
            <div class="piko-resource-card-header">
              ${r.latest_version && r.latest_version.status && html`
                <span class="piko-version-status-dot" style=${'background:var(--status-' + r.latest_version.status + ');'} title=${r.latest_version.status}></span>
              `}
              <a href=${resourceUrl(r.canonical)} class="piko-resource-card-name" data-navigate="true" data-native
                 onClick=${e => { e.preventDefault(); route(resourceUrl(r.canonical)); }}>${r.canonical}</a>
              <span class="piko-resource-card-type">${r.type}</span>
            </div>
            ${r.latest_version && r.latest_version.version && html`
              <div class="piko-resource-card-version">
                ${Object.entries(r.latest_version.version).map(([k, v]) => html`${k}: <span>${v}</span> `)}
              </div>
            `}
            <div class="piko-resource-card-meta">
              ${r.check_interval && html`<span>${r.check_interval}</span>`}
              ${r.last_check && !r.last_check.startsWith('0001-01-01') && html`<span>Checked: ${pikoTimeAgo(r.last_check)}</span>`}
            </div>
            <div class="piko-resource-card-actions mt-1">
              ${isMember && html`
                <button class="btn btn-sm btn-outline-warning check-resource-now" data-canonical=${r.canonical}
                  onClick=${e => checkNow(e, r.canonical)}>
                  <i class="bi bi-arrow-clockwise"></i> Check Now
                </button>
              `}
              <button class="piko-resource-expand-toggle" data-canonical=${r.canonical}
                onClick=${e => toggleVersionsList(e, r.canonical)}>
                <i class=${'bi ' + (expandedResources[r.canonical] ? 'bi-chevron-up' : 'bi-chevron-down')}></i> Versions
              </button>
            </div>
            <div class="piko-resource-panel-versions" data-canonical=${r.canonical}
                 style=${expandedResources[r.canonical] ? '' : 'display:none;'}>
              ${(versionLists[r.canonical] || []).map(v => {
                const ref = v.version ? versionRef(v.version) : '';
                const status = v.status || '';
                const isPinned = r.pinned_version_id === v.id;
                const isTracked = trackedVersionID === v.id;
                return html`
                  <div class=${'piko-resource-panel-version-item' + (isTracked ? ' tracked' : '')}>
                    <span class=${'piko-vsel-dot piko-status-dot-' + status}></span>
                    <span class="piko-vsel-ref">${ref}</span>
                    ${isTracked
                      ? html`<span class="piko-panel-tracking-badge"><i class="bi bi-signpost-2"></i></span>`
                      : html`<button class="piko-panel-track-btn" data-canonical=${r.canonical} data-version-id=${v.id} data-version-ref=${ref} title="Track"
                               onClick=${e => handleTrack(e, r.canonical, v.id, ref)}><i class="bi bi-signpost-2"></i></button>`
                    }
                    ${isMember && html`
                      <button class="piko-panel-trigger-btn" data-canonical=${r.canonical} data-version-id=${v.id} title="Trigger"
                        onClick=${e => handleTrigger(e, r.canonical, v.id)}><i class="bi bi-play-fill"></i></button>
                      <button class=${'piko-panel-pin-btn' + (isPinned ? ' pinned' : '')} data-canonical=${r.canonical} data-version-id=${v.id}
                        title=${isPinned ? 'Unpin' : 'Pin'}
                        onClick=${e => handlePin(e, r.canonical, v.id, isPinned)}>
                        <i class=${'bi ' + (isPinned ? 'bi-pin-fill' : 'bi-pin')}></i>
                      </button>
                    `}
                  </div>
                `;
              })}
            </div>
          </div>
        `;
      })}
    </div>
  `;
}

// ---------------------------------------------------------------------------
// PipelineShow (main component)
// ---------------------------------------------------------------------------

export function PipelineShow({ tc, pn }) {
  const [pipeline, setPipeline] = useState(null);
  const [team, setTeam] = useState(null);
  const [dotSource, setDotSource] = useState(null);
  const [resources, setResources] = useState([]);
  const [currentView, setCurrentView] = useState(() => {
    const saved = localStorage.getItem('piko-pipeline-view') || 'graph';
    return saved === 'pipeline' ? 'graph' : saved;
  });
  const [resourcesPanelOpen, setResourcesPanelOpen] = useState(false);
  const [gearPanelOpen, setGearPanelOpen] = useState(false);
  const [sharePanelOpen, setSharePanelOpen] = useState(false);
  const [hideIntermediates, setHideIntermediates] = useState(
    () => localStorage.getItem('piko-hide-intermediates') === '1'
  );
  const [groupParallel, setGroupParallel] = useState(
    () => localStorage.getItem('piko-group-parallel') === '1'
  );
  const [trackedVersion, setTrackedVersion] = useState(null);
  const [versionBanner, setVersionBanner] = useState(null);

  const graphZoomRef = useRef(null);
  const graphContainerRef = useRef(null);
  const listViewRef = useRef(null);

  // Parse ?version= from URL on mount
  const pendingVersionID = useRef(() => {
    const urlParams = new URLSearchParams(window.location.search);
    const v = urlParams.get('version');
    return v ? parseInt(v, 10) : null;
  });
  const initialVID = pendingVersionID.current();

  // Fetch pipeline and team data
  useEffect(() => {
    fetchPipeline(tc, pn).then(data => {
      setPipeline(data);
    }).catch(() => {});
    if (isLoggedIn.value) {
      fetchTeam(tc).then(t => setTeam(t)).catch(() => {});
    }
  }, [tc, pn]);

  // Fetch resources
  const refreshResources = useCallback(() => {
    fetchResources(tc, pn).then(data => setResources(data || [])).catch(() => {});
  }, [tc, pn]);

  useEffect(() => {
    refreshResources();
  }, [refreshResources]);

  // Fetch pipeline image
  const fetchImage = useCallback((versionID) => {
    const params = {};
    if (hideIntermediates) params.hide_intermediates = '1';
    if (groupParallel) params.group_parallel = '1';
    if (versionID) params.version_id = versionID;
    fetchPipelineImage(tc, pn, Object.keys(params).length ? params : null).then(resp => {
      if (resp && resp.image) {
        setDotSource(resp.image);
      } else if (resp && resp.data) {
        setDotSource(resp.data.image || resp.data);
      } else if (typeof resp === 'string') {
        setDotSource(resp);
      }
    }).catch(() => {});
  }, [tc, pn, hideIntermediates, groupParallel]);

  // Polling (calls immediately, then every fetchInterval ms)
  const pollCallback = useCallback(() => {
    fetchImage(trackedVersion ? trackedVersion.versionID : initialVID);
    if (trackedVersion) {
      pollVersionPath(trackedVersion);
    }
  }, [fetchImage, trackedVersion, initialVID]);
  usePolling(pollCallback, fetchInterval);

  // Resolve version resource on mount if ?version= present
  const resolvedRef = useRef(false);
  useEffect(() => {
    if (!initialVID || resources.length === 0 || resolvedRef.current) return;
    resolvedRef.current = true;
    resolveVersionResource(initialVID, resources);
  }, [resources]);

  // --- Version tracking ---

  const pollVersionPath = useCallback((tv) => {
    if (!tv || !tv.resource) return;
    fetchVersionPath(tc, pn, tv.resource, tv.versionID).then(resp => {
      if (!resp || !resp.data) return;
      const data = resp.data;
      let ref = tv.ref || '';
      if (!ref && data.resource && data.resource.version) {
        ref = versionRef(data.resource.version);
      }
      setVersionBanner({
        resource: data.resource.canonical,
        ref,
        completed: data.completed,
        total: data.total,
      });
      if (listViewRef.current && listViewRef.current.applyVersionScope) {
        listViewRef.current.applyVersionScope(data);
      }
    }).catch(() => {});
  }, [tc, pn]);

  const trackVersion = useCallback((resourceCanonical, versionID, ref) => {
    const tv = { resource: resourceCanonical, versionID, ref };
    setTrackedVersion(tv);
    fetchImage(versionID);
    window.history.replaceState(null, '', '/teams/' + tc + '/pipelines/' + pn + '?version=' + versionID);
    pollVersionPath(tv);
  }, [tc, pn, pollVersionPath, fetchImage]);

  const clearVersionScope = useCallback((e) => {
    if (e) { e.preventDefault(); e.stopPropagation(); }
    setTrackedVersion(null);
    setVersionBanner(null);
    fetchImage(null);
    window.history.replaceState(null, '', '/teams/' + tc + '/pipelines/' + pn);
    if (listViewRef.current && listViewRef.current.clearVersionScope) {
      listViewRef.current.clearVersionScope();
    }
  }, [tc, pn, fetchImage]);

  const resolveVersionResource = useCallback((versionID, resourcesList) => {
    let idx = 0;
    function tryNext() {
      if (idx >= resourcesList.length) return;
      const rCan = resourcesList[idx].canonical;
      idx++;
      fetchVersionPath(tc, pn, rCan, versionID, { silent: true }).then(resp => {
        if (resp && resp.data && resp.data.path && resp.data.path.length > 0) {
          const data = resp.data;
          const ref = data.resource && data.resource.version ? versionRef(data.resource.version) : '';
          const tv = { resource: rCan, versionID, ref };
          setTrackedVersion(tv);
          setVersionBanner({
            resource: rCan,
            ref,
            completed: data.completed,
            total: data.total,
          });
          if (listViewRef.current && listViewRef.current.applyVersionScope) {
            listViewRef.current.applyVersionScope(data);
          }
        } else {
          tryNext();
        }
      }).catch(() => tryNext());
    }
    tryNext();
  }, [tc, pn]);

  // --- SVG link click interception ---
  const onGraphClick = useCallback((e) => {
    const target = e.target;
    const parent = target.parentElement;
    if (parent && parent.href && parent.href.baseVal) {
      e.preventDefault();
      e.stopPropagation();
      let href = parent.href.baseVal;
      if (trackedVersion && trackedVersion.versionID) {
        href += (href.includes('?') ? '&' : '?') + 'version=' + trackedVersion.versionID;
      }
      route(href);
    }
  }, [trackedVersion]);

  // --- SVG ready callback ---
  const onSVGReady = useCallback((svg) => {
    const container = graphContainerRef.current;
    if (!container) return;
    if (!graphZoomRef.current) {
      graphZoomRef.current = new PikoGraphZoom(container);
    }
    graphZoomRef.current.attachSVG(svg);
  }, []);

  // Cleanup graph zoom on unmount
  useEffect(() => {
    return () => {
      if (graphZoomRef.current) {
        graphZoomRef.current.destroy();
        graphZoomRef.current = null;
      }
    };
  }, []);

  // --- View switching ---
  const switchView = useCallback((e, mode) => {
    e.preventDefault();
    e.stopPropagation();
    setCurrentView(mode);
    localStorage.setItem('piko-pipeline-view', mode);
    setGearPanelOpen(false);
    setSharePanelOpen(false);
  }, []);

  // --- Gear panel ---
  const toggleGearPanel = useCallback((e) => {
    e.preventDefault();
    e.stopPropagation();
    setSharePanelOpen(false);
    setGearPanelOpen(prev => !prev);
  }, []);

  const onHideIntermediates = useCallback((e) => {
    const checked = e.target.checked;
    localStorage.setItem('piko-hide-intermediates', checked ? '1' : '0');
    setHideIntermediates(checked);
  }, []);

  const onGroupParallel = useCallback((e) => {
    const checked = e.target.checked;
    localStorage.setItem('piko-group-parallel', checked ? '1' : '0');
    setGroupParallel(checked);
  }, []);

  // --- Share panel ---
  const toggleSharePanel = useCallback((e) => {
    e.preventDefault();
    e.stopPropagation();
    setGearPanelOpen(false);
    setSharePanelOpen(prev => !prev);
  }, []);

  const getShareUrls = useCallback(() => {
    const base = window.location.origin + '/teams/' + tc + '/pipelines/' + pn;
    const params = [];
    if (hideIntermediates) params.push('hide_intermediates=1');
    if (groupParallel) params.push('group_parallel=1');
    const qs = params.length ? '?' + params.join('&') : '';
    const svgUrl = base + '/image.svg' + qs;
    const pngUrl = base + '/image.png' + qs;
    const pipelineName = pipeline ? pipeline.name : pn;
    return {
      svg: svgUrl,
      png: pngUrl,
      md: '![' + pipelineName + '](' + svgUrl + ')',
    };
  }, [tc, pn, hideIntermediates, groupParallel, pipeline]);

  const copyShareUrl = useCallback((e, targetId) => {
    e.preventDefault();
    e.stopPropagation();
    const input = document.getElementById(targetId);
    if (!input) return;
    navigator.clipboard.writeText(input.value).then(() => {
      const btn = e.currentTarget;
      const original = btn.textContent;
      btn.textContent = 'Copied!';
      setTimeout(() => { btn.textContent = original; }, 1500);
    });
  }, []);

  // --- Resources panel ---
  const toggleResourcesPanel = useCallback((e) => {
    e.preventDefault();
    e.stopPropagation();
    setResourcesPanelOpen(prev => {
      if (!prev) refreshResources();
      return !prev;
    });
  }, [refreshResources]);

  // Resource panel polling
  usePolling(refreshResources, fetchInterval, resourcesPanelOpen);

  // --- Pipeline actions ---
  const clickEdit = useCallback((e) => {
    e.preventDefault();
    route('/teams/' + tc + '/pipelines/' + pn + '/edit');
  }, [tc, pn]);

  const clickSecrets = useCallback((e) => {
    e.preventDefault();
    route('/teams/' + tc + '/pipelines/' + pn + '/secrets');
  }, [tc, pn]);

  const clickDelete = useCallback((e) => {
    e.preventDefault();
    if (confirm("Are you sure you want to delete Pipeline '" + (pipeline ? pipeline.name : pn) + "'")) {
      deletePipeline(tc, pn).then(() => {
        route('/teams/' + tc + '/pipelines');
      }).catch(() => {});
    }
  }, [tc, pn, pipeline]);

  const clickPause = useCallback((e) => {
    e.preventDefault();
    pausePipeline(tc, pn).then(() => {
      showToast('Pipeline paused', 'success');
      fetchPipeline(tc, pn).then(data => setPipeline(data)).catch(() => {});
    }).catch(() => {});
  }, [tc, pn]);

  const clickUnpause = useCallback((e) => {
    e.preventDefault();
    unpausePipeline(tc, pn).then(() => {
      showToast('Pipeline unpaused', 'success');
      fetchPipeline(tc, pn).then(data => setPipeline(data)).catch(() => {});
    }).catch(() => {});
  }, [tc, pn]);

  // Close gear/share panels on outside click
  useEffect(() => {
    if (!gearPanelOpen && !sharePanelOpen) return;
    const handler = (e) => {
      if (gearPanelOpen && !e.target.closest('#gear-panel') && !e.target.closest('#toggle-gear-panel')) {
        setGearPanelOpen(false);
      }
      if (sharePanelOpen && !e.target.closest('#share-panel') && !e.target.closest('#toggle-share-panel')) {
        setSharePanelOpen(false);
      }
    };
    document.addEventListener('click', handler);
    return () => document.removeEventListener('click', handler);
  }, [gearPanelOpen, sharePanelOpen]);

  if (!pipeline) return null;

  const isOperator = hasTeamRole(tc, 'write');
  const isMaintainer = hasTeamRole(tc, 'maintain');
  const hasPaused = pipeline.jobs && pipeline.jobs.some(j => j.paused);
  const shareUrls = getShareUrls();
  const showGraphView = currentView === 'graph';

  return html`
    <${Breadcrumb} team=${team} pipeline=${pipeline} />
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h1 class="h4 fw-bold mb-0">
        ${pipeline.name}
        ${pipeline.public && html`<span class="badge bg-info fs-6 ms-2">Public</span>`}
      </h1>
      <div class="d-flex gap-2">
        ${isOperator && pipeline.jobs && pipeline.jobs.length > 0 && html`
          ${hasPaused
            ? html`<button type="button" id="unpause-pipeline" class="btn btn-primary" onClick=${clickUnpause}><i class="bi bi-play-circle"></i> Unpause</button>`
            : html`<button type="button" id="pause-pipeline" class="btn btn-primary" onClick=${clickPause}><i class="bi bi-pause-circle"></i> Pause</button>`
          }
        `}
        ${isMaintainer && html`
          <button type="button" id="secrets-pipeline" class="btn btn-secondary" onClick=${clickSecrets}><i class="bi bi-key"></i> Secrets</button>
          <button type="button" id="edit-pipeline" class="btn btn-info" onClick=${clickEdit}><i class="bi bi-pencil"></i> Edit</button>
          <button type="button" id="delete-pipeline" class="btn btn-danger" onClick=${clickDelete}><i class="bi bi-trash"></i> Delete</button>
        `}
      </div>
    </div>

    <div class="piko-view-toolbar">
      <button class=${'piko-view-btn' + (currentView === 'graph' ? ' active' : '')} data-view="graph"
        onClick=${e => switchView(e, 'graph')}>Graph</button>
      <button class=${'piko-view-btn' + (currentView === 'list' ? ' active' : '')} data-view="list"
        onClick=${e => switchView(e, 'list')}>List</button>
      <button class="piko-view-btn" id="toggle-resources-panel" style="margin-left:auto"
        onClick=${toggleResourcesPanel}><i class="bi bi-box"></i> Resources</button>
      ${showGraphView && html`
        <div class="piko-gear-wrap">
          <button class="piko-gear-btn" id="toggle-gear-panel" onClick=${toggleGearPanel}><i class="bi bi-gear"></i></button>
          <div class=${'piko-gear-panel' + (gearPanelOpen ? ' open' : '')} id="gear-panel">
            <label class="piko-gear-option">
              <input type="checkbox" id="gear-hide-intermediates" checked=${hideIntermediates} onChange=${onHideIntermediates} /> Hide intermediate resources
            </label>
            <label class="piko-gear-option">
              <input type="checkbox" id="gear-group-parallel" checked=${groupParallel} onChange=${onGroupParallel} /> Group parallel jobs
            </label>
          </div>
        </div>
        <div class="piko-share-wrap">
          <button class="piko-gear-btn" id="toggle-share-panel" onClick=${toggleSharePanel}><i class="bi bi-share"></i></button>
          <div class=${'piko-share-panel' + (sharePanelOpen ? ' open' : '')} id="share-panel">
            <div class="piko-share-row">
              <span class="piko-share-label">SVG</span>
              <input type="text" class="piko-share-url" id="share-svg-url" readonly value=${shareUrls.svg} />
              <button class="piko-share-copy" data-target="share-svg-url" onClick=${e => copyShareUrl(e, 'share-svg-url')}>Copy</button>
            </div>
            <div class="piko-share-row">
              <span class="piko-share-label">PNG</span>
              <input type="text" class="piko-share-url" id="share-png-url" readonly value=${shareUrls.png} />
              <button class="piko-share-copy" data-target="share-png-url" onClick=${e => copyShareUrl(e, 'share-png-url')}>Copy</button>
            </div>
            <div class="piko-share-row">
              <span class="piko-share-label">Markdown</span>
              <input type="text" class="piko-share-url" id="share-md-url" readonly value=${shareUrls.md} />
              <button class="piko-share-copy" data-target="share-md-url" onClick=${e => copyShareUrl(e, 'share-md-url')}>Copy</button>
            </div>
          </div>
        </div>
      `}
    </div>

    ${versionBanner && html`
      <div id="version-scope-banner" class="piko-version-banner">
        <div class="piko-version-banner-inner">
          <i class="bi bi-signpost-2 piko-version-banner-icon"></i>
          <span class="piko-version-banner-label">Tracking version</span>
          <span class="piko-version-banner-resource" id="version-banner-resource">${versionBanner.resource}</span>
          <span class="piko-version-banner-ref" id="version-banner-ref">${versionBanner.ref}</span>
          <span class="piko-version-banner-progress" id="version-banner-progress">${versionBanner.completed}/${versionBanner.total} completed</span>
          <button class="piko-version-banner-clear" id="clear-version-scope" onClick=${clearVersionScope}><i class="bi bi-x-lg"></i> Clear</button>
        </div>
      </div>
    `}

    <div class="piko-pipeline-body">
      <div id="pipeline-resources-panel" class=${'piko-resources-panel' + (resourcesPanelOpen ? ' open' : '')}>
        ${resourcesPanelOpen && html`
          <${PipelineResourcesPanel}
            tc=${tc} pn=${pn}
            resources=${resources}
            isMember=${isOperator}
            onClose=${() => setResourcesPanelOpen(false)}
            onTrackVersion=${trackVersion}
            trackedVersion=${trackedVersion}
            onRefreshResources=${refreshResources}
          />
        `}
      </div>

      <div class="piko-view-graph" style=${showGraphView ? '' : 'display:none'}>
        <div class="piko-graph-wrapper">
          <div class="piko-graph-container" id="graphviz" ref=${graphContainerRef} onClick=${onGraphClick}>
            ${dotSource && html`<${PipelineGraph} dotSource=${dotSource} onSVGReady=${onSVGReady} />`}
          </div>
        </div>
        <div class="piko-graph-legend">
          <span class="piko-graph-legend-item">
            <svg width="22" height="14" viewBox="0 0 22 14"><polygon points="0,0 16,0 22,7 16,14 0,14" fill="#83769C" stroke="#5F574F" stroke-width="1"/></svg>
            Resource
          </span>
          <span class="piko-graph-legend-item">
            <span class="piko-graph-legend-swatch" style="background:var(--status-succeeded);"></span>
            Succeeded
          </span>
          <span class="piko-graph-legend-item">
            <span class="piko-graph-legend-swatch" style="background:var(--status-failed);"></span>
            Failed
          </span>
          <span class="piko-graph-legend-item">
            <span class="piko-graph-legend-swatch" style="background:var(--status-started);border:1.5px dashed var(--status-started);"></span>
            Running
          </span>
          <span class="piko-graph-legend-item">
            <span class="piko-graph-legend-swatch" style="background:var(--status-cancelled);"></span>
            Cancelled
          </span>
          <span class="piko-graph-legend-item">
            <span class="piko-graph-legend-swatch" style="background:var(--status-pending);"></span>
            No builds
          </span>
          <span class="piko-graph-legend-item">
            <span class="piko-graph-legend-swatch" style="background:var(--status-waiting_for_approval);"></span>
            Waiting for approval
          </span>
          <span class="piko-graph-legend-item">
            <span class="piko-graph-legend-swatch" style="background:var(--status-warning);"></span>
            Warning
          </span>
          <span class="piko-graph-legend-item">
            <span class="piko-graph-legend-swatch" style="background:var(--primary);"></span>
            Paused
          </span>
        </div>
      </div>

      <div class="piko-view-list" style=${showGraphView ? 'display:none' : ''}>
        ${!showGraphView && pipeline && html`
          <${PipelineListView}
            imperativeRef=${listViewRef}
            tc=${tc} pn=${pn}
            pipeline=${pipeline}
            resources=${resources}
            trackedVersion=${trackedVersion}
            onTrackVersion=${trackVersion}
            onClearVersionScope=${clearVersionScope}
            onRefreshResources=${refreshResources}
          />
        `}
      </div>
    </div>
  `;
}
