'use strict';

import { html } from 'htm/preact';
import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import { route } from 'preact-router';
import { fetchPipelinesPage, fetchPipelineImage, fetchTeam } from '../api.js';
import { hasTeamRole } from '../state.js';
import { usePolling } from '../hooks.js';
import { fetchInterval, pikoTimeAgo } from '../utils.js';
import { PipelineGraph } from './PipelineGraph.js';
import { Breadcrumb } from './Layout.js';

export { PipelineNew } from './Editor.js';

// A multiple of the grid's three columns, so only the real last page is ragged.
const PAGE_SIZE = 24;
const SORTS = { name: 'Name', created: 'Newest' };

// The page, search and sort live in the URL so a reload or the back button
// lands where the user was. Read once on mount; written on every change.
function readListState() {
  const p = new URLSearchParams(window.location.search);
  const page = parseInt(p.get('page'), 10);
  return {
    page: page > 0 ? page : 1,
    q: p.get('q') || '',
    sort: Object.hasOwn(SORTS, p.get('sort')) ? p.get('sort') : 'name',
  };
}

function writeListState({ page, q, sort }) {
  const p = new URLSearchParams();
  if (page > 1) p.set('page', page);
  if (q) p.set('q', q);
  if (sort !== 'name') p.set('sort', sort);
  const qs = p.toString();
  window.history.replaceState(null, '', window.location.pathname + (qs ? '?' + qs : ''));
}

/**
 * PipelineList — pipeline card grid page, one page of PAGE_SIZE at a time.
 *
 * Keyed on the team: the router re-renders the same component with a new tc
 * rather than remounting it, and team B would otherwise open on team A's
 * page and search. A remount starts from the new team's own URL.
 */
export function PipelineList({ tc }) {
  return html`<${PipelineListPage} key=${tc} tc=${tc} />`;
}

function PipelineListPage({ tc }) {
  const [pipelines, setPipelines] = useState([]);
  const [meta, setMeta] = useState(null);
  const [team, setTeam] = useState(null);
  const [{ page, q, sort }, setListState] = useState(readListState);
  // What is typed, ahead of what is searched: the query is applied after a
  // pause so each keystroke is not a request.
  const [typed, setTyped] = useState(q);
  const [liveEnabled, setLiveEnabled] = useState(
    () => localStorage.getItem('liveStatusEnabled') === 'true'
  );

  useEffect(() => {
    fetchTeam(tc).then(t => setTeam(t)).catch(() => {});
  }, [tc]);

  useEffect(() => {
    writeListState({ page, q, sort });
    let stale = false;
    fetchPipelinesPage(tc, { limit: PAGE_SIZE, offset: (page - 1) * PAGE_SIZE, q, sort })
      .then(resp => {
        if (stale) return;
        const data = resp.data || [];
        const m = resp.meta || null;
        // The page no longer exists: the count shrank under us (a delete,
        // or a stale link). Land on the last page that does.
        if (m && page > 1 && data.length === 0 && m.total > 0) {
          setListState(s => ({ ...s, page: Math.ceil(m.total / PAGE_SIZE) }));
          return;
        }
        setPipelines(data);
        setMeta(m);
      })
      .catch(() => {});
    return () => { stale = true; };
  }, [tc, page, q, sort]);

  useEffect(() => {
    if (typed === q) return;
    const t = setTimeout(() => setListState(s => ({ ...s, q: typed, page: 1 })), 250);
    return () => clearTimeout(t);
  }, [typed, q]);

  const setPage = useCallback((n) => {
    setListState(s => ({ ...s, page: n }));
    window.scrollTo({ top: 0 });
  }, []);

  const setSort = useCallback((e) => {
    const next = e.target.value;
    setListState(s => ({ ...s, sort: next, page: 1 }));
  }, []);

  const toggleLive = useCallback((e) => {
    e.preventDefault();
    setLiveEnabled(prev => {
      const next = !prev;
      localStorage.setItem('liveStatusEnabled', next ? 'true' : 'false');
      return next;
    });
  }, []);

  const total = meta ? meta.total : 0;
  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return html`
    <${Breadcrumb} team=${team} showPipelines=${true} />
    <div class="d-flex align-items-center justify-content-between mb-3">
      <div class="d-flex align-items-center gap-2">
        <h1 class="h4 fw-bold mb-0">Pipelines</h1>
        <span class="piko-toggle${liveEnabled ? ' on' : ''}" id="live-status-toggle" onClick=${toggleLive}>
          <span class="piko-toggle-thumb"></span>
        </span>
        <label for="live-status-toggle" class="form-label mb-0" style="font-size:0.85rem;cursor:pointer;" onClick=${toggleLive}>Live</label>
      </div>
      ${hasTeamRole(tc, 'maintain') && html`
        <a type="button" id="pipelines-new" class="btn btn-success" href=${'/teams/' + tc + '/pipelines/new'} data-native
           onClick=${e => { e.preventDefault(); route('/teams/' + tc + '/pipelines/new'); }}>
          <i class="bi bi-plus"></i> New
        </a>
      `}
    </div>
    <div class="d-flex flex-wrap align-items-center gap-2 mb-3" id="pipelines-controls">
      <input type="search" class="form-control" id="pipelines-search" style="max-width:20rem"
             placeholder="Search by name" value=${typed} onInput=${e => setTyped(e.target.value)} />
      <select class="form-select" id="pipelines-sort" style="width:auto" value=${sort} onChange=${setSort}>
        ${Object.entries(SORTS).map(([k, label]) => html`<option value=${k}>${label}</option>`)}
      </select>
      <span class="ms-auto text-muted" style="font-size:0.85rem" id="pipelines-count">
        ${meta && (total === 0
          ? (q ? 'No pipelines match' : 'No pipelines')
          : `${(page - 1) * PAGE_SIZE + 1}\u2013${(page - 1) * PAGE_SIZE + pipelines.length} of ${total}`)}
      </span>
    </div>
    <div class="row row-cols-1 row-cols-md-3 g-4" id="pipelines">
      ${pipelines.map(p => html`
        <${PipelineCard} key=${p.id || p.canonical} pipeline=${p} tc=${tc} liveEnabled=${liveEnabled} />
      `)}
    </div>
    ${pageCount > 1 && html`<${Pager} page=${page} pageCount=${pageCount} onPage=${setPage} />`}
  `;
}

/**
 * Pager — previous / numbered / next. Shows the first and last page, the
 * current one and its neighbours, with an ellipsis over each gap.
 */
export function Pager({ page, pageCount, onPage }) {
  const items = [];
  let last = 0;
  for (let n = 1; n <= pageCount; n++) {
    if (n !== 1 && n !== pageCount && Math.abs(n - page) > 1) continue;
    if (n - last > 1) items.push(html`<li class="page-item disabled"><span class="page-link">\u2026</span></li>`);
    items.push(html`
      <li class=${'page-item' + (n === page ? ' active' : '')}>
        <a class="page-link" href="#" onClick=${e => { e.preventDefault(); if (n !== page) onPage(n); }}>${n}</a>
      </li>
    `);
    last = n;
  }
  return html`
    <nav aria-label="Pipeline pages" class="mt-4">
      <ul class="pagination justify-content-center mb-0" id="pipelines-pager">
        <li class=${'page-item' + (page <= 1 ? ' disabled' : '')}>
          <a class="page-link" href="#" aria-label="Previous" onClick=${e => { e.preventDefault(); if (page > 1) onPage(page - 1); }}>\u2039</a>
        </li>
        ${items}
        <li class=${'page-item' + (page >= pageCount ? ' disabled' : '')}>
          <a class="page-link" href="#" aria-label="Next" onClick=${e => { e.preventDefault(); if (page < pageCount) onPage(page + 1); }}>\u203a</a>
        </li>
      </ul>
    </nav>
  `;
}

/**
 * PipelineCard — individual card in the pipeline grid.
 */
function PipelineCard({ pipeline, tc, liveEnabled }) {
  const [dotSource, setDotSource] = useState(null);
  const [statusHtml, setStatusHtml] = useState(
    html`<span style="color:var(--text-muted);">Loading...</span>`
  );
  const svgRef = useRef(null);

  const fetchImage = useCallback(() => {
    fetchPipelineImage(tc, pipeline.canonical).then(resp => {
      if (resp && resp.image) {
        setDotSource(resp.image);
      } else if (resp && resp.data) {
        setDotSource(resp.data.image || resp.data);
      } else if (typeof resp === 'string') {
        setDotSource(resp);
      }
    }).catch(() => {});
  }, [tc, pipeline.canonical]);

  // Initial fetch (always, regardless of live toggle)
  useEffect(() => {
    fetchImage();
  }, [fetchImage]);

  // Live polling (only when enabled; pauses when tab hidden)
  usePolling(fetchImage, fetchInterval, liveEnabled);

  // Status detection from SVG fill colors
  const onSVGReady = useCallback((svg) => {
    svgRef.current = svg;
    updateStatusFromSVG(svg, pipeline.last_build_at, setStatusHtml);
  }, [pipeline.last_build_at]);

  // Re-check status when dotSource changes (SVG may re-render)
  useEffect(() => {
    if (!dotSource) return;
    // Small delay to allow SVG to render
    const t = setTimeout(() => {
      if (svgRef.current) {
        updateStatusFromSVG(svgRef.current, pipeline.last_build_at, setStatusHtml);
      }
    }, 300);
    return () => clearTimeout(t);
  }, [dotSource, pipeline.last_build_at]);

  const clickCard = useCallback((e) => {
    e.preventDefault();
    route('/teams/' + tc + '/pipelines/' + pipeline.canonical);
  }, [tc, pipeline.canonical]);

  return html`
    <div class="col" onClick=${clickCard} style="cursor:pointer">
      <div class="card h-100">
        <div class="card-header d-flex align-items-center justify-content-between">
          <span>${pipeline.name}</span>
          ${pipeline.public && html`<span class="badge bg-info">Public</span>`}
        </div>
        <div class="card-img-top">
          ${dotSource && html`<${PipelineGraph} dotSource=${dotSource} noLinks=${true} onSVGReady=${onSVGReady} />`}
        </div>
        <div class="card-footer piko-card-status">
          ${statusHtml}
        </div>
      </div>
    </div>
  `;
}

function updateStatusFromSVG(svg, lastBuildAt, setStatusHtml) {
  if (!svg) return;
  let hasFailed = false, hasRunning = false, hasSucceeded = false, hasWarning = false;
  svg.querySelectorAll('polygon, rect, ellipse, path').forEach(el => {
    const fill = (el.getAttribute('fill') || '').toLowerCase();
    if (fill === '#ff004d') hasFailed = true;
    if (fill === '#ffa300') hasRunning = true;
    if (fill === '#00a83a') hasSucceeded = true;
    if (fill === '#fa8072') hasWarning = true;
  });
  const timeAgo = lastBuildAt ? ' \u00b7 ' + pikoTimeAgo(lastBuildAt) : '';
  if (hasFailed) {
    setStatusHtml(html`<span class="piko-card-status-dot" style="background:var(--status-failed);"></span> Last build failed${timeAgo}`);
  } else if (hasRunning) {
    setStatusHtml(html`<span class="piko-card-status-dot" style="background:var(--status-started);"></span> Running${timeAgo}`);
  } else if (hasWarning) {
    setStatusHtml(html`<span class="piko-card-status-dot" style="background:var(--status-warning);"></span> Last build warning${timeAgo}`);
  } else if (hasSucceeded) {
    setStatusHtml(html`<span class="piko-card-status-dot" style="background:var(--status-succeeded);"></span> Last build passed${timeAgo}`);
  } else {
    setStatusHtml(html`<span style="color:var(--text-muted);">No builds</span>`);
  }
}
