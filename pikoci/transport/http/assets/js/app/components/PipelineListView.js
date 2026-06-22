'use strict';

import { html } from 'htm/preact';
import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import { fetchJobs, triggerResource, fetchResourceVersions, fetchVersionPath } from '../api.js';
import { session } from '../state.js';
import { usePolling } from '../hooks.js';
import { showToast } from '../toast.js';
import { fetchInterval, pikoTimeAgo, versionRef } from '../utils.js';
import { JobBuilds } from './Jobs.js';

// ---------------------------------------------------------------------------
// Helper: find trigger resources
// ---------------------------------------------------------------------------
function findTriggerResources(pipeline) {
  const jobs = pipeline.jobs || [];
  const seen = {};
  const result = [];
  for (let i = 0; i < jobs.length; i++) {
    const plan = jobs[i].plan || [];
    for (let j = 0; j < plan.length; j++) {
      const s = plan[j];
      if (s.type === 'get' && s.get && s.get.trigger && (!s.get.passed || s.get.passed.length === 0)) {
        const canonical = s.get.type + '.' + s.get.name;
        if (!seen[canonical]) {
          seen[canonical] = true;
          result.push(canonical);
        }
      }
    }
  }
  return result;
}

// ---------------------------------------------------------------------------
// Helper: resolve chain (BFS through jobs via passed constraints)
// ---------------------------------------------------------------------------
function resolveChain(pipeline, resourceCanonical) {
  const allJobs = pipeline.jobs || [];
  const visited = {};
  const queue = [];
  const chain = [];

  // Find entry-point jobs that get this resource directly (no passed)
  for (let i = 0; i < allJobs.length; i++) {
    const plan = allJobs[i].plan || [];
    for (let j = 0; j < plan.length; j++) {
      const s = plan[j];
      if (s.type === 'get' && s.get) {
        const can = s.get.type + '.' + s.get.name;
        if (can === resourceCanonical && (!s.get.passed || s.get.passed.length === 0)) {
          if (!visited[allJobs[i].name]) {
            visited[allJobs[i].name] = true;
            queue.push(allJobs[i].name);
          }
        }
      }
    }
  }

  // BFS — for each job in the chain, find downstream jobs via passed constraints
  while (queue.length > 0) {
    const jobName = queue.shift();
    chain.push(jobName);
    for (let i = 0; i < allJobs.length; i++) {
      if (visited[allJobs[i].name]) continue;
      const plan = allJobs[i].plan || [];
      for (let j = 0; j < plan.length; j++) {
        const s = plan[j];
        if (s.type === 'get' && s.get && s.get.passed) {
          for (let k = 0; k < s.get.passed.length; k++) {
            if (s.get.passed[k] === jobName) {
              visited[allJobs[i].name] = true;
              queue.push(allJobs[i].name);
              break;
            }
          }
        }
        if (visited[allJobs[i].name]) break;
      }
    }
  }

  return chain;
}

// ---------------------------------------------------------------------------
// Helper: build tree from chain
// ---------------------------------------------------------------------------
function buildTree(pipeline, chainJobs) {
  const allJobs = pipeline.jobs || [];
  const chainSet = {};
  for (let i = 0; i < chainJobs.length; i++) chainSet[chainJobs[i]] = true;

  const parents = {};
  const children = {};
  for (let i = 0; i < chainJobs.length; i++) {
    parents[chainJobs[i]] = [];
    children[chainJobs[i]] = [];
  }

  for (let i = 0; i < chainJobs.length; i++) {
    const name = chainJobs[i];
    const pj = allJobs.find(j => j.name === name);
    if (!pj) continue;
    const plan = pj.plan || [];
    for (let j = 0; j < plan.length; j++) {
      const s = plan[j];
      if (s.type === 'get' && s.get && s.get.passed) {
        for (let k = 0; k < s.get.passed.length; k++) {
          if (chainSet[s.get.passed[k]]) {
            parents[name].push(s.get.passed[k]);
            children[s.get.passed[k]].push(name);
          }
        }
      }
    }
  }

  const roots = chainJobs.filter(n => parents[n].length === 0);
  return { roots, children, parents };
}

// ---------------------------------------------------------------------------
// PipelineListView
// ---------------------------------------------------------------------------

export function PipelineListView({ tc, pn, pipeline, resources, trackedVersion, onTrackVersion, onClearVersionScope, onRefreshResources, imperativeRef }) {
  const triggerResources = useRef(findTriggerResources(pipeline)).current;
  const storagePrefix = 'piko-list-' + (pipeline.canonical || pn) + '-';

  const [selectedResource, setSelectedResource] = useState(() => {
    const saved = localStorage.getItem(storagePrefix + 'resource');
    if (saved && triggerResources.indexOf(saved) >= 0) return saved;
    return triggerResources.length > 0 ? triggerResources[0] : null;
  });
  const [selectedJob, setSelectedJob] = useState(null);
  const [jobsData, setJobsData] = useState([]);
  const [resourceMenuOpen, setResourceMenuOpen] = useState(false);
  const [versionMenuOpen, setVersionMenuOpen] = useState(false);
  const [recentVersions, setRecentVersions] = useState(null);
  const [scopedVersionID, setScopedVersionID] = useState(null);
  const [scopedVersionRef, setScopedVersionRef] = useState(null);
  const [versionChainJobs, setVersionChainJobs] = useState(null);
  const [versionBuildMap, setVersionBuildMap] = useState(null);
  const [collapsedGroups, setCollapsedGroups] = useState(() => {
    try { return JSON.parse(localStorage.getItem(storagePrefix + 'collapsed') || '{}'); }
    catch { return {}; }
  });

  const selectedJobRef = useRef(selectedJob);
  selectedJobRef.current = selectedJob;

  // Build chain from selectedResource
  const chainJobs = versionChainJobs || (selectedResource ? resolveChain(pipeline, selectedResource) : []);

  // Expose imperative methods
  useEffect(() => {
    if (imperativeRef) {
      imperativeRef.current = {
        applyVersionScope: (pathData) => {
          applyVersionScopeInternal(pathData);
        },
        clearVersionScope: () => {
          clearVersionScopeInternal();
        },
      };
    }
    return () => {
      if (imperativeRef) imperativeRef.current = null;
    };
  }, [imperativeRef]);

  // --- Data fetching ---

  const fetchJobsData = useCallback(() => {
    fetchJobs(tc, pn).then(data => {
      if (data) setJobsData(data);
    }).catch(() => {});
  }, [tc, pn]);

  // Jobs polling (calls immediately, then every fetchInterval ms)
  usePolling(fetchJobsData, fetchInterval);

  // Resources polling
  const refreshResourcesCb = useCallback(() => {
    if (onRefreshResources) onRefreshResources();
  }, [onRefreshResources]);
  usePolling(refreshResourcesCb, fetchInterval);

  // Auto-select first non-succeeded job on initial load
  useEffect(() => {
    if (selectedJobRef.current || chainJobs.length === 0 || jobsData.length === 0) return;
    const statusMap = buildStatusMap(jobsData);
    const savedJob = localStorage.getItem(storagePrefix + 'job');
    let pick = null;
    if (savedJob && chainJobs.indexOf(savedJob) >= 0) {
      pick = savedJob;
    } else {
      pick = chainJobs[0];
      for (let i = 0; i < chainJobs.length; i++) {
        const d = statusMap[chainJobs[i]];
        if (d && d.latest_status && d.latest_status !== 'succeeded') {
          pick = chainJobs[i];
          break;
        }
      }
    }
    if (pick) selectJob(pick);
  }, [jobsData, chainJobs.length]);

  // --- Status map ---

  function buildStatusMap(jobs) {
    const map = {};
    for (let i = 0; i < jobs.length; i++) {
      map[jobs[i].name] = jobs[i];
    }
    return map;
  }

  // --- Version scoping ---

  function applyVersionScopeInternal(pathData) {
    if (!pathData || !pathData.path) return;
    if (pathData.resource && pathData.resource.version) {
      setScopedVersionRef(versionRef(pathData.resource.version));
    }
    // Extract version ID
    let vid = null;
    for (let i = 0; i < pathData.path.length; i++) {
      if (pathData.path[i].build && pathData.path[i].build.version_id) {
        vid = pathData.path[i].build.version_id;
        break;
      }
    }
    if (vid) setScopedVersionID(vid);

    const pathJobs = pathData.path.map(p => p.job_name);
    const pathBuildMap = {};
    for (let i = 0; i < pathData.path.length; i++) {
      if (pathData.path[i].build) {
        pathBuildMap[pathData.path[i].job_name] = pathData.path[i].build;
      }
    }
    setVersionChainJobs(prev => {
      const wasScoped = !!prev;
      // Re-select job only when scope is first applied
      if (!wasScoped) {
        const currentJob = selectedJobRef.current;
        if (currentJob && pathJobs.indexOf(currentJob) >= 0) {
          selectJob(currentJob);
        } else if (pathJobs.length > 0) {
          selectJob(pathJobs[0]);
        }
      }
      return pathJobs;
    });
    setVersionBuildMap(pathBuildMap);
  }

  function clearVersionScopeInternal() {
    setScopedVersionID(null);
    setScopedVersionRef(null);
    setVersionChainJobs(null);
    setVersionBuildMap(null);
    setRecentVersions(null);
  }

  // --- Job selection ---

  function selectJob(jobName) {
    setSelectedJob(jobName);
    selectedJobRef.current = jobName;
    localStorage.setItem(storagePrefix + 'job', jobName);
    // Update URL
    window.history.replaceState(null, '', '/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jobName + '/builds');
    const trackedVID = trackedVersion ? trackedVersion.versionID : null;
    if (trackedVID) {
      window.history.replaceState(null, '', window.location.pathname + '?version=' + trackedVID);
    }
  }

  // --- Resource selector ---

  const onSelectResource = useCallback((e, canonical) => {
    e.preventDefault();
    e.stopPropagation();
    setResourceMenuOpen(false);
    if (!canonical || canonical === selectedResource) return;
    setSelectedResource(canonical);
    setRecentVersions(null);
    clearVersionScopeInternal();
    if (onClearVersionScope) onClearVersionScope();
    localStorage.setItem(storagePrefix + 'resource', canonical);
    setSelectedJob(null);
    selectedJobRef.current = null;
  }, [selectedResource, storagePrefix, onClearVersionScope]);

  const toggleResourceMenu = useCallback((e) => {
    e.preventDefault();
    e.stopPropagation();
    setResourceMenuOpen(prev => !prev);
  }, []);

  // Close resource menu on outside click
  useEffect(() => {
    if (!resourceMenuOpen) return;
    const handler = () => setResourceMenuOpen(false);
    document.addEventListener('click', handler);
    return () => document.removeEventListener('click', handler);
  }, [resourceMenuOpen]);

  // --- Version selector ---

  const fetchRecentVersions = useCallback(() => {
    if (!selectedResource) return;
    fetchResourceVersions(tc, pn, selectedResource, { limit: 10 }).then(resp => {
      if (resp && resp.data) {
        setRecentVersions(resp.data);
      }
    }).catch(() => {});
  }, [tc, pn, selectedResource]);

  useEffect(() => {
    if (selectedResource) fetchRecentVersions();
  }, [selectedResource]);

  const toggleVersionMenu = useCallback((e) => {
    e.preventDefault();
    e.stopPropagation();
    setVersionMenuOpen(prev => {
      if (!prev) fetchRecentVersions();
      return !prev;
    });
  }, [fetchRecentVersions]);

  // Close version menu on outside click
  useEffect(() => {
    if (!versionMenuOpen) return;
    const handler = () => setVersionMenuOpen(false);
    setTimeout(() => {
      document.addEventListener('click', handler);
    }, 0);
    return () => document.removeEventListener('click', handler);
  }, [versionMenuOpen]);

  const onSelectVersion = useCallback((e, versionID, ref) => {
    e.preventDefault();
    e.stopPropagation();
    setVersionMenuOpen(false);
    if (versionID === '' || versionID === undefined || versionID === null) {
      // "All" selected - clear scope
      clearVersionScopeInternal();
      if (onClearVersionScope) onClearVersionScope();
      return;
    }
    const vid = parseInt(versionID, 10);
    setScopedVersionID(vid);
    setScopedVersionRef(ref);
    if (onTrackVersion) {
      onTrackVersion(selectedResource, vid, ref);
    } else {
      // Fallback: fetch path directly
      fetchVersionPath(tc, pn, selectedResource, vid).then(resp => {
        if (resp && resp.data) {
          applyVersionScopeInternal(resp.data);
        }
      }).catch(() => {});
    }
  }, [selectedResource, tc, pn, onTrackVersion, onClearVersionScope]);

  // --- Check resource ---
  const onCheckResource = useCallback((e) => {
    e.preventDefault();
    e.stopPropagation();
    if (!selectedResource) return;
    triggerResource(tc, pn, selectedResource).then(() => {
      showToast('Resource check triggered', 'success');
      if (onRefreshResources) onRefreshResources();
    }).catch(() => {
      showToast('Failed to trigger resource check', 'error');
    });
  }, [tc, pn, selectedResource, onRefreshResources]);

  // --- Toggle parallel groups ---
  const onToggleParallel = useCallback((e, groupKey) => {
    e.preventDefault();
    setCollapsedGroups(prev => {
      const next = { ...prev };
      if (next[groupKey]) {
        delete next[groupKey];
      } else {
        next[groupKey] = true;
      }
      localStorage.setItem(storagePrefix + 'collapsed', JSON.stringify(next));
      return next;
    });
  }, [storagePrefix]);

  // --- Build status map with version overrides ---
  const statusMap = buildStatusMap(jobsData);
  if (versionBuildMap) {
    for (const jn in versionBuildMap) {
      if (versionBuildMap.hasOwnProperty(jn)) {
        const b = versionBuildMap[jn];
        statusMap[jn] = statusMap[jn] || {};
        statusMap[jn].latest_status = b.status;
        statusMap[jn].has_running = (b.status === 'started' || b.status === 'pending');
      }
    }
  }

  // --- Build tree ---
  const tree = buildTree(pipeline, chainJobs);

  // --- Resource bar info ---
  const resMap = {};
  (resources || []).forEach(r => { resMap[r.canonical] = r; });
  const selRes = resMap[selectedResource] || {};
  const selLv = selRes.latest_version;
  const selStatus = (selLv && selLv.status) ? selLv.status : '';

  // --- Render resource selector ---
  function renderResourceSelector() {
    if (triggerResources.length === 0) {
      return html`<span style="color:var(--text-muted)">No trigger resources</span>`;
    }

    return html`
      <div class="piko-rsel">
        <button class="piko-rsel-trigger" type="button" onClick=${toggleResourceMenu}>
          ${selStatus && html`<span class=${'piko-rsel-dot piko-status-dot-' + selStatus}></span>`}
          <span class="piko-rsel-label">${selectedResource}</span>
          <i class="bi bi-chevron-down piko-rsel-arrow"></i>
        </button>
        <div class=${'piko-rsel-menu' + (resourceMenuOpen ? ' open' : '')}>
          ${triggerResources.map(canonical => {
            const r = resMap[canonical] || {};
            const rlv = r.latest_version;
            const rStatus = (rlv && rlv.status) ? rlv.status : '';
            const active = canonical === selectedResource ? ' active' : '';
            return html`
              <div class=${'piko-rsel-option' + active} data-canonical=${canonical}
                onClick=${e => onSelectResource(e, canonical)}>
                <span class=${'piko-rsel-dot piko-status-dot-' + rStatus}></span>
                <span>${canonical}</span>
              </div>
            `;
          })}
        </div>
      </div>
      <div class="piko-vsel-wrap">
        ${renderVersionSelector()}
      </div>
      <span class="piko-resource-bar-info">
        ${selLv && selLv.version && (() => {
          for (const key in selLv.version) {
            if (selLv.version.hasOwnProperty(key)) {
              return html`<span class="piko-resource-bar-ver">${key + ': ' + selLv.version[key]}</span>`;
            }
          }
          return null;
        })()}
        ${selRes.check_interval && html`<span class="piko-resource-bar-meta">${selRes.check_interval}</span>`}
        ${selRes.last_check && html`<span class="piko-resource-bar-meta">checked ${pikoTimeAgo(selRes.last_check)}</span>`}
      </span>
      ${session.value.jwt && html`
        <button class="btn btn-sm btn-outline-warning piko-resource-check-btn" onClick=${onCheckResource}>
          <i class="bi bi-arrow-clockwise"></i> Check Now
        </button>
      `}
    `;
  }

  // --- Render version selector ---
  function renderVersionSelector() {
    if (!selectedResource) return null;
    const label = scopedVersionRef || 'All';
    return html`
      <button class="piko-vsel-btn" type="button" onClick=${toggleVersionMenu}>
        <i class="bi bi-signpost-2"></i>
        <span>${label}</span>
        <i class="bi bi-chevron-down"></i>
      </button>
      <div class=${'piko-vsel-menu' + (versionMenuOpen ? ' open' : '')}>
        <div class="piko-vsel-item piko-vsel-item-all" data-version-id="" onClick=${e => onSelectVersion(e, '', '')}>All versions</div>
        ${(recentVersions || []).map(v => {
          const ref = v.version ? versionRef(v.version) : '';
          const status = v.status || '';
          const active = (scopedVersionID && scopedVersionID === v.id) ? ' active' : '';
          return html`
            <div class=${'piko-vsel-item' + active} data-version-id=${v.id} data-version-ref=${ref}
              onClick=${e => onSelectVersion(e, v.id, ref)}>
              <span class=${'piko-vsel-dot piko-status-dot-' + status}></span>
              <span class="piko-vsel-ref">${ref}</span>
            </div>
          `;
        })}
      </div>
    `;
  }

  // --- Render job row ---
  function renderJobRow(data, extraClass) {
    const status = data.paused ? 'paused' : (data.has_running ? 'started' : (data.latest_status || ''));
    const isActive = selectedJob === data.name;
    return html`
      <div class=${'piko-job-row' + (isActive ? ' active' : '') + (extraClass || '')} data-job=${data.name}
        onClick=${e => { e.preventDefault(); selectJob(data.name); }}>
        <div class=${'piko-job-row-dot piko-status-dot-' + status}></div>
        <span class="piko-job-row-name">${data.name}</span>
      </div>
    `;
  }

  // --- Render parallel group ---
  function renderParallelGroup(jobNames, rendered, renderChildrenFn) {
    const groupKey = jobNames.slice().sort().join(',');
    const isCollapsed = collapsedGroups[groupKey];
    const arrow = isCollapsed ? '\u25B6' : '\u25BC';
    const counts = {};
    for (let i = 0; i < jobNames.length; i++) {
      const d = statusMap[jobNames[i]] || {};
      const s = d.paused ? 'paused' : (d.has_running ? 'started' : (d.latest_status || ''));
      counts[s] = (counts[s] || 0) + 1;
    }

    // Identify fan-in children
    const groupSet = {};
    for (let i = 0; i < jobNames.length; i++) groupSet[jobNames[i]] = true;
    const fanInChildren = [];
    const fanInParentSet = {};
    for (let i = 0; i < jobNames.length; i++) {
      const kids = tree.children[jobNames[i]] || [];
      for (let k = 0; k < kids.length; k++) {
        if (rendered[kids[k]]) continue;
        const kidParents = tree.parents[kids[k]] || [];
        if (kidParents.length >= 2) {
          let allInGroup = true;
          for (let p = 0; p < kidParents.length; p++) {
            if (!groupSet[kidParents[p]]) { allInGroup = false; break; }
          }
          if (allInGroup) {
            rendered[kids[k]] = true;
            fanInChildren.push(kids[k]);
            for (let p = 0; p < kidParents.length; p++) {
              fanInParentSet[kidParents[p]] = true;
            }
          }
        }
      }
    }

    const hasFanIn = fanInChildren.length > 0;
    const alignClass = hasFanIn ? ' piko-fan-in-aligned' : '';

    // Build fan-in section
    let fanInSection = null;
    if (hasFanIn) {
      const fanInParents = jobNames.filter(n => fanInParentSet[n]);
      fanInSection = html`
        <div class="piko-fan-in-section">
          ${fanInParents.map(n => {
            const data = statusMap[n] || { name: n, latest_status: '' };
            rendered[n] = true;
            return html`${renderJobRow(data)}${renderChildrenFn([n], rendered)}`;
          })}
        </div>
        <div class="piko-fan-in-cont">
          ${fanInChildren.map(n => {
            const data = statusMap[n] || { name: n, latest_status: '' };
            return html`${renderJobRow(data)}${renderChildrenFn([n], rendered)}`;
          })}
        </div>
      `;
    }

    let fanInInserted = false;
    const memberRows = jobNames.map(n => {
      if (fanInParentSet[n]) {
        if (!fanInInserted) {
          fanInInserted = true;
          return fanInSection;
        }
        return null;
      }
      const data = statusMap[n] || { name: n, latest_status: '' };
      rendered[n] = true;
      return html`${renderJobRow(data, alignClass)}${renderChildrenFn([n], rendered)}`;
    }).filter(Boolean);

    if (fanInSection && !fanInInserted) {
      memberRows.push(fanInSection);
    }

    return html`
      <div class="piko-parallel-header" data-group=${groupKey} onClick=${e => onToggleParallel(e, groupKey)}>
        <span>${arrow} parallel</span>
        <span class="piko-parallel-counts">
          ${Object.entries(counts).filter(([st]) => st).map(([st, count]) => html`
            <span class=${'piko-status-dot-' + st} style="width:8px;height:8px;border-radius:50%;display:inline-block"></span> ${count}${' '}
          `)}
        </span>
      </div>
      <div class="piko-parallel-nested" style=${isCollapsed ? 'display:none' : ''}>
        ${memberRows}
      </div>
    `;
  }

  // --- Render tree ---
  function renderTree() {
    if (!selectedResource) return null;
    const rendered = {};

    function renderChildren(parentNames, rendered) {
      const results = [];
      for (let p = 0; p < parentNames.length; p++) {
        const parentName = parentNames[p];
        const kids = tree.children[parentName] || [];
        if (kids.length === 0) continue;

        // Group siblings by their parent set
        const groupKey = (name) => (tree.parents[name] || []).slice().sort().join(',');
        const keyToKids = {};
        const kidOrder = [];
        for (let i = 0; i < kids.length; i++) {
          if (rendered[kids[i]]) continue;
          const gk = groupKey(kids[i]);
          if (!keyToKids[gk]) {
            keyToKids[gk] = [];
            kidOrder.push(gk);
          }
          keyToKids[gk].push(kids[i]);
        }

        for (let i = 0; i < kidOrder.length; i++) {
          let group = keyToKids[kidOrder[i]].filter(n => !rendered[n]);
          if (group.length === 0) continue;

          if (group.length >= 2) {
            for (let j = 0; j < group.length; j++) rendered[group[j]] = true;
            results.push(renderParallelGroup(group, rendered, renderChildren));
          } else {
            const name = group[0];
            rendered[name] = true;
            const data = statusMap[name] || { name, latest_status: '' };
            results.push(renderJobRow(data));
            results.push(renderChildren([name], rendered));
          }
        }
      }
      return results;
    }

    const roots = tree.roots;
    const content = [];
    if (roots.length >= 2) {
      for (let i = 0; i < roots.length; i++) rendered[roots[i]] = true;
      content.push(renderParallelGroup(roots, rendered, renderChildren));
    } else if (roots.length === 1) {
      rendered[roots[0]] = true;
      const data = statusMap[roots[0]] || { name: roots[0], latest_status: '' };
      content.push(renderJobRow(data));
      content.push(renderChildren([roots[0]], rendered));
    }

    return content;
  }

  // Determine the tracked version ID for JobBuilds
  const trackedVID = trackedVersion ? trackedVersion.versionID : null;

  return html`
    <div class="piko-list-resource-bar">
      <div class="piko-rsel-container">
        ${renderResourceSelector()}
      </div>
    </div>
    <div class="piko-list-panels">
      <div class="piko-list-left">
        <div class="piko-job-list">
          ${renderTree()}
        </div>
      </div>
      <div class="piko-job-detail">
        ${selectedJob && html`
          <${JobBuilds}
            key=${selectedJob + '-' + (trackedVID || 'all')}
            tc=${tc}
            pn=${pn}
            jn=${selectedJob}
            embedded=${true}
            trackedVersionID=${trackedVID}
          />
        `}
      </div>
    </div>
  `;
}
