'use strict';

import { html } from 'htm/preact';
import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import { route } from 'preact-router';
import { isLoggedIn, hasTeamRole, session } from '../state.js';
import { fetchBuilds, fetchBuild, cancelBuild, retryBuild, approveBuild, rejectBuild, triggerJob, pauseJob, unpauseJob, fetchJob, fetchResources, fetchResourceVersions, fetchVersionPath, fetchTeam, fetchPipeline } from '../api.js';
import { useLoading, usePolling } from '../hooks.js';
import { sortBuilds, selectActiveBuild, durationToString, processLogs, pikoTimeAgo, fetchInterval, versionRef } from '../utils.js';
import { showToast } from '../toast.js';
import { Breadcrumb } from './Layout.js';

const stepIcon = {
  get: 'bi-cloud-download',
  task: 'bi-terminal',
  put: 'bi-cloud-upload',
  notify: 'bi-bell',
  service: 'bi-hdd-stack',
  runner: 'bi-gear',
  job: 'bi-braces',
};

function getStepIcon(type) {
  return stepIcon[type] || 'bi-terminal';
}

function statusBadge(status, hasLogs) {
  if (status === 'failed') return html`<span class="piko-badge piko-badge-failed">fail</span>`;
  if (status === 'started') return html`<span class="piko-badge piko-badge-started">running</span>`;
  if (status === 'pending') return html`<span class="piko-badge piko-badge-pending">pending</span>`;
  if (status === 'cancelled') return html`<span class="piko-badge piko-badge-cancelled">cancel</span>`;
  if (status === 'succeeded' || hasLogs) return html`<span class="piko-badge piko-badge-succeeded">ok</span>`;
  return null;
}

function buildStatusBadge(status) {
  if (status === 'succeeded') return html`<span class="piko-badge piko-badge-succeeded">Succeeded</span>`;
  if (status === 'failed') return html`<span class="piko-badge piko-badge-failed">Failed</span>`;
  if (status === 'started') return html`<span class="piko-badge piko-badge-started">Running</span>`;
  if (status === 'cancelled') return html`<span class="piko-badge piko-badge-cancelled">Cancelled</span>`;
  if (status === 'pending') return html`<span class="piko-badge piko-badge-pending">Pending</span>`;
  if (status === 'waiting_for_approval') return html`<span class="piko-badge piko-badge-waiting_for_approval">Waiting for Approval</span>`;
  return null;
}

// Prepare a build's duration fields for display (nanoseconds -> string)
function prepareBuild(b) {
  if (!b) return b;
  const out = { ...b };
  out.duration = b.duration !== 0 ? durationToString(b.duration) : 0;
  out.steps = (b.steps || []).map(s => {
    const step = { ...s, duration: durationToString(s.duration) };
    if (s.sub_steps) {
      step.sub_steps = s.sub_steps.map(c => ({ ...c, duration: durationToString(c.duration) }));
    }
    return step;
  });
  out.job = (b.job || []).map(j => ({ ...j, duration: durationToString(j.duration) }));
  return out;
}

// ---------- BuildTab ----------

function BuildTab({ build, active, onClick }) {
  return html`
    <div
      class="piko-build-tab ${active ? 'active' : ''}"
      id="t-${build.build_number}"
      onClick=${() => onClick(build)}
    >
      #${build.build_number}
      <span class="piko-tab-status status-${build.status}"></span>
    </div>
  `;
}

// ---------- StepRow ----------

function StepRow({ step, expanded, onToggle, autoFollow, setAutoFollow, isAutoScrollingRef }) {
  const preRef = useRef(null);
  const logs = processLogs(step.logs);

  const onCopyLogs = useCallback((e) => {
    e.stopPropagation();
    if (preRef.current) {
      navigator.clipboard.writeText(preRef.current.textContent.trim());
    }
  }, []);

  const onGotoBottom = useCallback(() => {
    if (preRef.current) {
      preRef.current.scrollTop = preRef.current.scrollHeight;
    }
  }, []);

  const onScroll = useCallback(() => {
    if (isAutoScrollingRef.current) return;
    const el = preRef.current;
    if (!el) return;
    const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - 20;
    setAutoFollow(atBottom);
  }, [setAutoFollow, isAutoScrollingRef]);

  // Auto-scroll when following
  useEffect(() => {
    if (autoFollow && expanded && preRef.current) {
      const el = preRef.current;
      isAutoScrollingRef.current = true;
      requestAnimationFrame(() => {
        el.scrollTop = el.scrollHeight;
        requestAnimationFrame(() => { isAutoScrollingRef.current = false; });
      });
    }
  });

  return html`
    <div class="piko-step-row" data-status="${step.status}">
      <div class="piko-step-row-header" onClick=${onToggle}>
        <span>
          <span class="piko-step-label"><i class="bi ${getStepIcon(step.type)}"></i> ${step.type || 'step'}</span>
          ${' '}${step.name}${' '}
          <span style="color:var(--text-muted);">(${step.duration})</span>
        </span>
        <span style="display:flex;align-items:center;gap:6px;">
          ${step.logs ? html`
            <button class="piko-copy-logs-header-btn" onClick=${onCopyLogs} title="Copy logs">
              <i class="bi bi-clipboard"></i>
            </button>
          ` : null}
          ${statusBadge(step.status, step.logs)}
        </span>
      </div>
      <div class="piko-step-row-body" style="display:${expanded ? 'block' : 'none'};">
        <div style="position:relative;">
          <button class="piko-goto-bottom-btn" onClick=${onGotoBottom} title="Go to bottom">
            <i class="bi bi-arrow-down"></i>
          </button>
          <pre ref=${preRef} onScroll=${onScroll}>${logs}</pre>
        </div>
      </div>
    </div>
  `;
}

// ---------- ParallelGroup ----------

function ParallelGroup({ step, expandedSteps, onToggleStep, stepIndexBase, autoFollow, setAutoFollow, isAutoScrollingRef }) {
  const [groupExpanded, setGroupExpanded] = useState(true);

  return html`
    <div class="piko-step-row piko-parallel-group" data-status="${step.status}">
      <div class="piko-step-row-header" onClick=${() => setGroupExpanded(!groupExpanded)}>
        <span>
          <span class="piko-step-label"><i class="bi bi-arrows-expand"></i> in_parallel</span>
          ${' '}
          <span style="color:var(--text-muted);">(${(step.sub_steps || []).length} steps, ${step.duration})</span>
        </span>
        <span style="display:flex;align-items:center;gap:6px;">
          ${statusBadge(step.status, false)}
        </span>
      </div>
      <div class="piko-step-row-body" style="display:${groupExpanded ? 'block' : 'none'};padding:4px 4px 4px 16px;">
        ${(step.sub_steps || []).map((child, ci) => html`
          <${StepRow}
            key=${ci}
            step=${child}
            expanded=${!!expandedSteps[stepIndexBase + '_' + ci]}
            onToggle=${() => onToggleStep(stepIndexBase + '_' + ci)}
            autoFollow=${autoFollow}
            setAutoFollow=${setAutoFollow}
            isAutoScrollingRef=${isAutoScrollingRef}
          />
        `)}
      </div>
    </div>
  `;
}

// ---------- ApprovalResourceRow ----------

function ApprovalResourceRow({ rCan, passed, versionMeta, tc, pn }) {
  const [expanded, setExpanded] = useState(false);
  const [versionData, setVersionData] = useState(versionMeta || null);
  const [loading, setLoading] = useState(false);

  const toggle = () => {
    if (!expanded && !versionData && !loading) {
      setLoading(true);
      fetchResourceVersions(tc, pn, rCan, { limit: 1 }).then(resp => {
        const versions = resp.data || resp || [];
        if (versions.length > 0 && versions[0].version) {
          setVersionData(versions[0].version);
        }
      }).catch(() => {}).finally(() => setLoading(false));
    }
    setExpanded(prev => !prev);
  };

  return html`
    <div class="mb-2">
      <div class="d-flex align-items-center gap-2" style="cursor:pointer;" onClick=${toggle}>
        <i class="bi ${expanded ? 'bi-chevron-down' : 'bi-chevron-right'}" style="color:var(--text-muted);"></i>
        <i class="bi bi-cloud-download" style="color:var(--text-muted);"></i>
        <code>${rCan}</code>
        ${passed && passed.length > 0 ? html`<span class="text-muted">passed: ${passed.join(', ')}</span>` : null}
        ${versionMeta ? html`<span class="badge bg-info">${versionRef(versionMeta)}</span>` : null}
      </div>
      ${expanded ? html`
        <div style="margin-left:2.5rem;margin-top:0.4rem;">
          ${loading ? html`<span class="text-muted">Loading...</span>` : null}
          ${versionData ? html`
            <table class="table table-sm table-borderless mb-0">
              <tbody>
                ${Object.entries(versionData).map(([k, v]) => html`
                  <tr key=${k}>
                    <td class="text-muted" style="width:120px;padding:0.25rem 0.5rem;font-weight:600;">${k}</td>
                    <td style="padding:0.25rem 0.5rem;word-break:break-all;">${String(v)}</td>
                  </tr>
                `)}
              </tbody>
            </table>
          ` : !loading ? html`<span class="text-muted">No version data available.</span>` : null}
        </div>
      ` : null}
    </div>
  `;
}

// ---------- BuildContent ----------

function BuildContent({ build: rawBuild, tc, pn, jn, job: jobData, onRetry }) {
  const [fullBuild, setFullBuild] = useState(null);
  // Merge: use rawBuild (latest from polling) but overlay approvals from fullBuild
  const mergedBuild = rawBuild && fullBuild && rawBuild.id === fullBuild.id
    ? { ...rawBuild, approvals: fullBuild.approvals }
    : (fullBuild || rawBuild);
  const build = prepareBuild(mergedBuild);
  const isOperator = hasTeamRole(tc, 'write');
  const [autoFollow, setAutoFollow] = useState(true);
  const [expandedSteps, setExpandedSteps] = useState({});
  const [elapsed, setElapsed] = useState('');
  const [timeAgo, setTimeAgo] = useState('');
  const isAutoScrollingRef = useRef(false);
  const [cancelLoading, withCancelLoading] = useLoading();
  const [retryLoading, withRetryLoading] = useLoading();
  const [approveMsg, setApproveMsg] = useState('');
  const [rejectMsg, setRejectMsg] = useState('');
  const [approveLoading, withApproveLoading] = useLoading();
  const [rejectLoading, withRejectLoading] = useLoading();
  const initializedRef = useRef(false);

  // Fetch full build detail to get approvals (not in list endpoint).
  const rawBuildId = rawBuild && rawBuild.id;
  const fetchedIdRef = useRef(null);
  const refreshFullBuild = useCallback(() => {
    if (rawBuild && rawBuild.build_number) {
      fetchBuild(tc, pn, jn, rawBuild.build_number).then(b => {
        if (b) setFullBuild(b);
      }).catch(() => {});
    }
  }, [tc, pn, jn, rawBuild && rawBuild.build_number]);
  // Fetch on build switch
  useEffect(() => {
    if (rawBuildId && rawBuildId !== fetchedIdRef.current) {
      fetchedIdRef.current = rawBuildId;
      refreshFullBuild();
    }
  }, [rawBuildId, refreshFullBuild]);

  // Initialize expanded steps - running steps start expanded
  useEffect(() => {
    if (!initializedRef.current && build) {
      const exp = {};
      (build.steps || []).forEach((s, i) => {
        if (s.type === 'in_parallel') {
          (s.sub_steps || []).forEach((c, ci) => {
            if (c.status === 'started') exp[i + '_' + ci] = true;
          });
        } else {
          if (s.status === 'started') exp['s_' + i] = true;
        }
      });
      (build.job || []).forEach((j, ji) => {
        if (j.status === 'started') exp['j_' + ji] = true;
      });
      setExpandedSteps(exp);
      initializedRef.current = true;
    }
  }, [build]);

  // Elapsed timer
  useEffect(() => {
    if (!build) return;
    const update = () => {
      if (build.status === 'started' && rawBuild.duration === 0 && rawBuild.started_at) {
        const secs = Math.floor((Date.now() - new Date(rawBuild.started_at).getTime()) / 1000);
        const h = Math.floor(secs / 3600);
        const m = Math.floor((secs % 3600) / 60);
        const s = secs % 60;
        let text = '';
        if (h > 0) text += h + 'h ';
        if (m > 0 || h > 0) text += m + 'm ';
        text += s + 's';
        setElapsed('(' + text + ')');
      }
      if (rawBuild.started_at) {
        setTimeAgo(pikoTimeAgo(rawBuild.started_at));
      }
    };
    update();
    const id = setInterval(update, 1000);
    return () => clearInterval(id);
  }, [rawBuild.status, rawBuild.started_at, rawBuild.duration]);

  const toggleStep = useCallback((key) => {
    setExpandedSteps(prev => ({ ...prev, [key]: !prev[key] }));
  }, []);

  const onCancel = useCallback(async () => {
    await withCancelLoading(async () => {
      await cancelBuild(tc, pn, jn, build.build_number);
      showToast('Build cancelled', 'success');
    });
  }, [tc, pn, jn, build.build_number, withCancelLoading]);

  const handleRetry = useCallback(async () => {
    await withRetryLoading(async () => {
      await retryBuild(tc, pn, jn, build.build_number);
      showToast('Build retried', 'success');
      if (onRetry) onRetry();
    });
  }, [tc, pn, jn, build.build_number, withRetryLoading, onRetry]);

  const toggleFollowBtn = useCallback(() => {
    setAutoFollow(prev => !prev);
  }, []);

  if (!build) return null;

  const steps = build.steps || [];
  const jobSteps = build.job || [];
  const isRunningOrPending = build.status === 'started' || build.status === 'pending';
  const isWaitingApproval = build.status === 'waiting_for_approval';
  const isMaintainer = hasTeamRole(tc, 'maintain');

  return html`
    <div class="piko-build-content-inner">
      ${build.error && !(build.approvals || []).some(a => a.action === 'rejected') ? html`<div class="alert alert-danger" role="alert">${build.error}</div>` : null}
      ${(build.approvals || []).length > 0 || isWaitingApproval ? html`
        <div class="piko-step-row" data-status="${isWaitingApproval ? 'started' : (build.approvals || []).some(a => a.action === 'rejected') ? 'failed' : 'succeeded'}" style="border-left: 3px solid var(--status-waiting_for_approval);">
          <div class="piko-step-row-header" style="cursor:default;">
            <i class="bi bi-shield-check" style="color:var(--status-waiting_for_approval);"></i>
            <span class="piko-step-name" style="color:var(--status-waiting_for_approval);font-weight:600;">Approval Gate</span>
            ${isWaitingApproval && jobData && jobData.approve_count ? html`
              <span class="text-muted" style="font-size:0.85em;">${(build.approvals || []).filter(a => a.action === 'approved').length}/${jobData.approve_count} approvals</span>
            ` : null}
            ${buildStatusBadge(isWaitingApproval ? 'waiting_for_approval' : (build.approvals || []).some(a => a.action === 'rejected') ? 'failed' : 'succeeded')}
          </div>
          <div class="piko-step-row-body" style="display:block;padding:0.5rem 1rem 0.5rem 2rem;">
            ${jobData && jobData.plan ? html`
              <div class="mb-2">
                ${jobData.plan.filter(s => s.type === 'get' && s.get).map(s => {
                  const rCan = s.get.type + '.' + s.get.name;
                  const isTrigger = rCan === build.resource_canonical;
                  return html`<${ApprovalResourceRow}
                    key=${rCan}
                    rCan=${rCan}
                    passed=${s.get.passed}
                    versionMeta=${isTrigger ? build.version_metadata : null}
                    tc=${tc}
                    pn=${pn}
                  />`;
                })}
              </div>
            ` : build.resource_canonical ? html`
              <div class="mb-2">
                <${ApprovalResourceRow}
                  rCan=${build.resource_canonical}
                  versionMeta=${build.version_metadata}
                  tc=${tc}
                  pn=${pn}
                />
              </div>
            ` : null}
            ${(build.approvals || []).length > 0 ? html`
              <div class="mb-2">
                ${(build.approvals || []).map(a => html`
                  <div key=${a.id} class="d-flex align-items-center gap-2 mb-1" style="font-size:0.9em;">
                    <span class="badge ${a.action === 'approved' ? 'bg-success' : 'bg-danger'}">${a.action}</span>
                    <strong>${a.username}</strong>
                    ${a.message ? html`<span class="text-muted">— ${a.message}</span>` : null}
                  </div>
                `)}
              </div>
            ` : html`<p class="text-muted mb-1" style="font-size:0.9em;">No votes yet.</p>`}
            ${isWaitingApproval && isMaintainer && !(build.approvals || []).some(a => a.username === (session.value.user && session.value.user.username)) ? html`
              <div class="d-flex gap-2 mt-2">
                <form class="input-group input-group-sm" style="max-width:400px;" onSubmit=${(e) => {
                  e.preventDefault();
                  withApproveLoading(async () => {
                    await approveBuild(tc, pn, jn, build.build_number, approveMsg);
                    showToast('Build approved', 'success');
                    setApproveMsg('');
                    refreshFullBuild();
                    if (onRetry) onRetry();
                  });
                }}>
                  <input type="text" class="form-control" placeholder="Optional message"
                    value=${approveMsg} onInput=${(e) => setApproveMsg(e.target.value)} />
                  <button type="submit" class="btn btn-success" disabled=${approveLoading}>
                    <i class="bi bi-check-circle"></i> ${approveLoading ? 'Approving...' : 'Approve'}
                  </button>
                </form>
                <form class="input-group input-group-sm" style="max-width:400px;" onSubmit=${(e) => {
                  e.preventDefault();
                  if (!rejectMsg) return;
                  withRejectLoading(async () => {
                    await rejectBuild(tc, pn, jn, build.build_number, rejectMsg);
                    showToast('Build rejected', 'success');
                    setRejectMsg('');
                    refreshFullBuild();
                    if (onRetry) onRetry();
                  });
                }}>
                  <input type="text" class="form-control" placeholder="Reason (required)"
                    value=${rejectMsg} onInput=${(e) => setRejectMsg(e.target.value)} />
                  <button type="submit" class="btn btn-danger" disabled=${rejectLoading || !rejectMsg}>
                    <i class="bi bi-x-circle"></i> ${rejectLoading ? 'Rejecting...' : 'Reject'}
                  </button>
                </form>
              </div>
            ` : null}
          </div>
        </div>
      ` : null}
      <div class="piko-build-meta">
        ${build.status !== 'pending' && build.status !== 'waiting_for_approval' ? html`
          <span>
            <span class="piko-build-label">Started</span>
            ${' '}
            <span class="piko-time-ago" title="${new Date(rawBuild.started_at).toLocaleString()}">${timeAgo}</span>
          </span>
          ${build.status === 'started' && rawBuild.duration === 0 ? html`
            <span><span class="piko-build-label">Duration</span> <span class="piko-elapsed">${elapsed}</span></span>
          ` : build.duration !== 0 ? html`
            <span><span class="piko-build-label">Duration</span> ${build.duration}</span>
          ` : null}
        ` : null}
        ${buildStatusBadge(build.status)}
        ${isRunningOrPending ? html`
          <span style="margin-left:auto;display:flex;gap:6px;align-items:center;">
            ${build.status === 'started' ? html`
              <button type="button" class="btn btn-sm ${autoFollow ? 'btn-info' : 'btn-outline-info'} piko-follow-toggle" title="Auto-scroll logs" onClick=${toggleFollowBtn}>
                <i class="bi ${autoFollow ? 'bi-arrow-down-circle-fill' : 'bi-arrow-down-circle'}"></i> ${autoFollow ? 'Following' : 'Follow'}
              </button>
            ` : null}
            ${isOperator ? html`
              <button type="button" class="btn btn-sm btn-outline-danger piko-cancel-build" onClick=${onCancel} disabled=${cancelLoading}>
                <i class="bi bi-x-circle"></i> ${cancelLoading ? 'Cancelling...' : 'Cancel'}
              </button>
            ` : null}
          </span>
        ` : isOperator && !isWaitingApproval ? html`
          <button type="button" class="btn btn-sm btn-outline-warning piko-retry-build" style="margin-left:auto;" onClick=${handleRetry} disabled=${retryLoading}>
            <i class="bi bi-arrow-clockwise"></i> ${retryLoading ? 'Retrying...' : 'Retry'}
          </button>
        ` : null}
      </div>
      ${steps.map((s, i) => {
        if (s.type === 'in_parallel') {
          return html`
            <${ParallelGroup}
              key=${i}
              step=${s}
              expandedSteps=${expandedSteps}
              onToggleStep=${toggleStep}
              stepIndexBase=${i}
              autoFollow=${autoFollow}
              setAutoFollow=${setAutoFollow}
              isAutoScrollingRef=${isAutoScrollingRef}
            />
          `;
        }
        return html`
          <${StepRow}
            key=${'s_' + i}
            step=${s}
            expanded=${!!expandedSteps['s_' + i]}
            onToggle=${() => toggleStep('s_' + i)}
            autoFollow=${autoFollow}
            setAutoFollow=${setAutoFollow}
            isAutoScrollingRef=${isAutoScrollingRef}
          />
        `;
      })}
      ${jobSteps.map((j, ji) => html`
        <${StepRow}
          key=${'j_' + ji}
          step=${{ ...j, type: 'job' }}
          expanded=${!!expandedSteps['j_' + ji]}
          onToggle=${() => toggleStep('j_' + ji)}
          autoFollow=${autoFollow}
          setAutoFollow=${setAutoFollow}
          isAutoScrollingRef=${isAutoScrollingRef}
        />
      `)}
    </div>
  `;
}

// ---------- JobBuilds (main) ----------

export function JobBuilds({ tc, pn, jn, bid, embedded, trackedVersionID: trackedVersionIDProp }) {
  // Read ?version= from URL if not passed as prop (e.g., navigating from graph view)
  const trackedVersionID = trackedVersionIDProp || (() => {
    const params = new URLSearchParams(window.location.search);
    const v = params.get('version');
    return v ? parseInt(v, 10) : null;
  })();

  const [builds, setBuilds] = useState([]);
  const [activeBuildID, setActiveBuildID] = useState(null);
  const [job, setJob] = useState(null);
  const [team, setTeam] = useState(null);
  const [pipeline, setPipeline] = useState(null);
  const [versionBanner, setVersionBanner] = useState(null);
  const [triggerLoading, withTriggerLoading] = useLoading();
  const [pauseLoading, withPauseLoading] = useLoading();
  const [unpauseLoading, withUnpauseLoading] = useLoading();
  const tabsRef = useRef(null);
  const metaRef = useRef({ newestID: null, oldestID: null, hasMore: false });
  const trackedBuildIDsRef = useRef(null);

  // Fetch tracked builds (version path resolution)
  const fetchTrackedBuilds = useCallback(async () => {
    if (!trackedVersionID) return;
    try {
      const resources = await fetchResources(tc, pn);
      if (!resources) return;
      for (const res of resources) {
        try {
          const pathResp = await fetchVersionPath(tc, pn, res.canonical, trackedVersionID, { silent: true });
          if (pathResp.data && pathResp.data.path && pathResp.data.path.length > 0) {
            const ids = {};
            for (const entry of pathResp.data.path) {
              if (entry.job_name === jn && entry.build) {
                ids[entry.build.id] = true;
                if (entry.retries) {
                  for (const r of entry.retries) {
                    ids[r.id] = true;
                  }
                }
              }
            }
            trackedBuildIDsRef.current = ids;

            // Set banner info
            const v = pathResp.data.resource.version || {};
            const ref = v.ref || v.digest || v.tag || (() => {
              for (const k in v) { if (v.hasOwnProperty(k)) return k + ': ' + v[k]; }
              return '';
            })();
            setVersionBanner({ resource: pathResp.data.resource.canonical, ref });
            return;
          }
        } catch { /* try next resource */ }
      }
    } catch { /* ignore */ }
  }, [tc, pn, jn, trackedVersionID]);

  // Filter builds by tracked IDs
  const filterByTracked = useCallback((allBuilds) => {
    if (!trackedBuildIDsRef.current) return allBuilds;
    return allBuilds.filter(b => trackedBuildIDsRef.current[b.id]);
  }, []);

  // Load builds
  const loadBuilds = useCallback(async () => {
    try {
      const resp = await fetchBuilds(tc, pn, jn);
      const sorted = sortBuilds(resp.data || []);
      metaRef.current = {
        newestID: resp.meta?.newest_id || null,
        oldestID: resp.meta?.oldest_id || null,
        hasMore: resp.meta?.has_more || false,
      };
      return sorted;
    } catch { return []; }
  }, [tc, pn, jn]);

  // Fetch new builds (cursor-based)
  const fetchNewBuilds = useCallback(async () => {
    if (!metaRef.current.newestID) return null;
    try {
      const resp = await fetchBuilds(tc, pn, jn, { after: metaRef.current.newestID });
      if (resp.meta?.newest_id) {
        metaRef.current.newestID = resp.meta.newest_id;
      }
      return resp.data || [];
    } catch { return null; }
  }, [tc, pn, jn]);

  // Fetch more builds (pagination on scroll)
  const fetchMoreBuilds = useCallback(async () => {
    if (!metaRef.current.hasMore || !metaRef.current.oldestID) return;
    try {
      const resp = await fetchBuilds(tc, pn, jn, { before: metaRef.current.oldestID });
      if (resp.meta) {
        metaRef.current.oldestID = resp.meta.oldest_id || metaRef.current.oldestID;
        metaRef.current.hasMore = resp.meta.has_more || false;
      }
      if (resp.data && resp.data.length > 0) {
        setBuilds(prev => {
          const existing = new Set(prev.map(b => b.id));
          const newOnes = resp.data.filter(b => !existing.has(b.id));
          return sortBuilds([...prev, ...newOnes]);
        });
      }
    } catch { /* ignore */ }
  }, [tc, pn, jn]);

  // Select active build
  const doActivate = useCallback((requestedID, buildList) => {
    const active = selectActiveBuild(buildList, requestedID);
    if (active) {
      setActiveBuildID(active.id);
      if (!embedded) {
        const navPath = '/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/builds/' + active.build_number;
        const versionParam = trackedVersionID ? '?version=' + trackedVersionID : '';
        history.replaceState(null, '', navPath + versionParam);
      }
    }
    return active;
  }, [tc, pn, jn, embedded, trackedVersionID]);

  // Fetch the active build to get fresh data (for running builds)
  const refreshActiveBuild = useCallback(async (buildList) => {
    const active = buildList.find(b => b.id === (activeBuildID || (selectActiveBuild(buildList)?.id)));
    if (active) {
      const status = active.status;
      if (status !== 'succeeded' && status !== 'failed' && status !== 'cancelled') {
        try {
          const fresh = await fetchBuild(tc, pn, jn, active.build_number);
          setBuilds(prev => sortBuilds(prev.map(b => b.id === fresh.id ? fresh : b)));
        } catch { /* ignore */ }
      }
    }
    // Also refresh other non-terminal builds
    for (const b of buildList) {
      if (b.id === active?.id) continue;
      if (b.status === 'started' || b.status === 'pending' || b.status === 'waiting_for_approval') {
        try {
          const fresh = await fetchBuild(tc, pn, jn, b.build_number);
          setBuilds(prev => sortBuilds(prev.map(x => x.id === fresh.id ? fresh : x)));
        } catch { /* ignore */ }
      }
    }
  }, [tc, pn, jn, activeBuildID]);

  // Initial load
  useEffect(() => {
    let cancelled = false;

    const init = async () => {
      // Fetch team and pipeline info for breadcrumb
      if (isLoggedIn.value) {
        fetchTeam(tc).then(t => { if (!cancelled) setTeam(t); }).catch(() => {});
      }
      fetchPipeline(tc, pn).then(p => { if (!cancelled) setPipeline(p); }).catch(() => {});

      // Fetch job info
      try {
        const j = await fetchJob(tc, pn, jn);
        if (!cancelled) setJob(j);
      } catch { /* ignore */ }

      // Fetch tracked build IDs first if tracking, otherwise clear filter
      if (trackedVersionID) {
        await fetchTrackedBuilds();
      } else {
        trackedBuildIDsRef.current = null;
        setVersionBanner(null);
        metaRef.current = { newestID: null, oldestID: null, hasMore: false };
      }

      // Load builds
      let allBuilds = await loadBuilds();
      if (cancelled) return;

      allBuilds = filterByTracked(allBuilds);
      setBuilds(allBuilds);

      // If a specific build was requested but not in the list, fetch it
      if (bid) {
        const found = allBuilds.find(b => String(b.build_number) === String(bid));
        if (!found) {
          try {
            const single = await fetchBuild(tc, pn, jn, bid);
            allBuilds = sortBuilds([...allBuilds, single]);
            allBuilds = filterByTracked(allBuilds);
            if (!cancelled) setBuilds(allBuilds);
          } catch { /* ignore */ }
        }
      }

      // Set active
      const requestedID = bid ? (allBuilds.find(b => String(b.build_number) === String(bid))?.id) : null;
      doActivate(requestedID, allBuilds);
    };

    init();
    return () => { cancelled = true; };
  }, [tc, pn, jn, bid, trackedVersionID]);

  // Polling interval
  const pollBuilds = useCallback(async () => {
    if (trackedVersionID) {
      await fetchTrackedBuilds();
    }

    const newBuilds = await fetchNewBuilds();
    if (newBuilds && newBuilds.length > 0) {
      setBuilds(prev => {
        const existing = new Set(prev.map(b => b.id));
        const fresh = newBuilds.filter(b => !existing.has(b.id));
        const merged = sortBuilds([...fresh, ...prev]);
        return filterByTracked(merged);
      });
    }

    // Refresh active build
    setBuilds(prev => {
      // Trigger a refresh for non-terminal builds asynchronously
      refreshActiveBuild(prev);
      return prev;
    });
  }, [trackedVersionID, fetchNewBuilds, fetchTrackedBuilds, filterByTracked, refreshActiveBuild]);
  usePolling(pollBuilds, fetchInterval);

  // Scroll listener for build tabs pagination
  useEffect(() => {
    const el = tabsRef.current;
    if (!el) return;
    const handler = () => {
      if (el.scrollLeft + el.clientWidth >= el.scrollWidth - 50) {
        fetchMoreBuilds();
      }
    };
    el.addEventListener('scroll', handler);
    return () => el.removeEventListener('scroll', handler);
  }, [fetchMoreBuilds]);

  // Tab click handler
  const onTabClick = useCallback((build) => {
    setActiveBuildID(build.id);
    if (!embedded) {
      const navPath = '/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/builds/' + build.build_number;
      const versionParam = trackedVersionID ? '?version=' + trackedVersionID : '';
      history.replaceState(null, '', navPath + versionParam);
    }
  }, [tc, pn, jn, embedded, trackedVersionID]);

  // Trigger/pause/unpause handlers
  const onTrigger = useCallback(async () => {
    await withTriggerLoading(async () => {
      await triggerJob(tc, pn, jn);
      showToast('Job triggered', 'success');
    });
  }, [tc, pn, jn, withTriggerLoading]);

  const onPause = useCallback(async () => {
    await withPauseLoading(async () => {
      await pauseJob(tc, pn, jn);
      showToast('Job paused', 'success');
      const j = await fetchJob(tc, pn, jn);
      setJob(j);
    });
  }, [tc, pn, jn, withPauseLoading]);

  const onUnpause = useCallback(async () => {
    await withUnpauseLoading(async () => {
      await unpauseJob(tc, pn, jn);
      showToast('Job unpaused', 'success');
      const j = await fetchJob(tc, pn, jn);
      setJob(j);
    });
  }, [tc, pn, jn, withUnpauseLoading]);

  // Retry handler - refresh builds after retrying
  const onRetry = useCallback(async () => {
    const newBuilds = await fetchNewBuilds();
    if (newBuilds && newBuilds.length > 0) {
      setBuilds(prev => {
        const existing = new Set(prev.map(b => b.id));
        const fresh = newBuilds.filter(b => !existing.has(b.id));
        return sortBuilds([...fresh, ...prev]);
      });
    }
  }, [fetchNewBuilds]);

  // Back to pipeline (version tracking)
  const onBackToPipeline = useCallback((e) => {
    e.preventDefault();
    const pipelinePath = '/teams/' + tc + '/pipelines/' + pn;
    const versionParam = trackedVersionID ? '?version=' + trackedVersionID : '';
    route(pipelinePath + versionParam);
  }, [tc, pn, trackedVersionID]);

  const isMember = hasTeamRole(tc, 'write');

  return html`
    <div>
      ${!embedded && html`<${Breadcrumb} team=${team} pipeline=${pipeline} job=${job || { name: jn }} />`}
      <div class="d-flex align-items-center justify-content-between mb-3">
        <h1 class="h4 fw-bold mb-0">${job ? job.name : jn}</h1>
        <div class="d-flex gap-2">
          ${isMember ? html`
            ${job && job.paused ? html`
              <button type="button" id="unpause-job" class="btn btn-primary" onClick=${onUnpause} disabled=${unpauseLoading}>
                <i class="bi bi-play-circle"></i> ${unpauseLoading ? 'Unpausing...' : 'Unpause Job'}
              </button>
            ` : html`
              <button type="button" id="trigger-job" class="btn btn-warning" onClick=${onTrigger} disabled=${triggerLoading}>
                <i class="bi bi-play-circle"></i> ${triggerLoading ? 'Triggering...' : 'Trigger Job'}
              </button>
              <button type="button" id="pause-job" class="btn btn-primary" onClick=${onPause} disabled=${pauseLoading}>
                <i class="bi bi-pause-circle"></i> ${pauseLoading ? 'Pausing...' : 'Pause Job'}
              </button>
            `}
          ` : null}
        </div>
      </div>
      ${trackedVersionID && !embedded && versionBanner ? html`
        <div id="job-version-banner" class="piko-version-banner">
          <div class="piko-version-banner-inner">
            <i class="bi bi-signpost-2 piko-version-banner-icon"></i>
            <span class="piko-version-banner-label">Tracking version</span>
            <span class="piko-version-banner-resource" id="job-version-banner-resource">${versionBanner.resource}</span>
            <span class="piko-version-banner-ref" id="job-version-banner-ref">${versionBanner.ref}</span>
            <a href="#" id="job-version-back" class="piko-version-banner-clear" onClick=${onBackToPipeline}>
              <i class="bi bi-arrow-left"></i> Back to pipeline
            </a>
          </div>
        </div>
      ` : null}
      <div class="piko-build-tabs" id="builds-tabs" ref=${tabsRef}>
        ${builds.map(b => html`
          <${BuildTab}
            key=${b.id}
            build=${b}
            active=${b.id === activeBuildID}
            onClick=${onTabClick}
          />
        `)}
      </div>
      <div id="builds-content">
        ${builds.map(b => html`
          <div key=${b.id} class="piko-build-content ${b.id !== activeBuildID ? 'd-none' : ''}">
            ${b.id === activeBuildID ? html`
              <${BuildContent}
                build=${b}
                tc=${tc}
                pn=${pn}
                jn=${jn}
                job=${job}
                onRetry=${onRetry}
              />
            ` : null}
          </div>
        `)}
      </div>
    </div>
  `;
}
