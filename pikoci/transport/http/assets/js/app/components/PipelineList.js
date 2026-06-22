'use strict';

import { html } from 'htm/preact';
import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import { route } from 'preact-router';
import { fetchPipelines, fetchPipelineImage, fetchTeam } from '../api.js';
import { isTeamAdmin } from '../state.js';
import { usePolling } from '../hooks.js';
import { fetchInterval, pikoTimeAgo } from '../utils.js';
import { PipelineGraph } from './PipelineGraph.js';
import { Breadcrumb } from './Layout.js';

export { PipelineNew } from './Editor.js';

/**
 * PipelineList — pipeline card grid page.
 */
export function PipelineList({ tc }) {
  const [pipelines, setPipelines] = useState([]);
  const [team, setTeam] = useState(null);
  const [liveEnabled, setLiveEnabled] = useState(
    () => localStorage.getItem('liveStatusEnabled') === 'true'
  );

  useEffect(() => {
    fetchPipelines(tc).then(data => setPipelines(data || [])).catch(() => {});
    fetchTeam(tc).then(t => setTeam(t)).catch(() => {});
  }, [tc]);

  const toggleLive = useCallback((e) => {
    e.preventDefault();
    setLiveEnabled(prev => {
      const next = !prev;
      localStorage.setItem('liveStatusEnabled', next ? 'true' : 'false');
      return next;
    });
  }, []);

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
      ${isTeamAdmin(tc) && html`
        <a type="button" id="pipelines-new" class="btn btn-success" href=${'/teams/' + tc + '/pipelines/new'} data-native
           onClick=${e => { e.preventDefault(); route('/teams/' + tc + '/pipelines/new'); }}>
          <i class="bi bi-plus"></i> New
        </a>
      `}
    </div>
    <div class="row row-cols-1 row-cols-md-3 g-4" id="pipelines">
      ${pipelines.map(p => html`
        <${PipelineCard} key=${p.id || p.canonical} pipeline=${p} tc=${tc} liveEnabled=${liveEnabled} />
      `)}
    </div>
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
  let hasFailed = false, hasRunning = false, hasSucceeded = false;
  svg.querySelectorAll('polygon, rect, ellipse, path').forEach(el => {
    const fill = (el.getAttribute('fill') || '').toLowerCase();
    if (fill === '#ff004d') hasFailed = true;
    if (fill === '#ffa300') hasRunning = true;
    if (fill === '#00a83a') hasSucceeded = true;
  });
  const timeAgo = lastBuildAt ? ' \u00b7 ' + pikoTimeAgo(lastBuildAt) : '';
  if (hasFailed) {
    setStatusHtml(html`<span class="piko-card-status-dot" style="background:var(--status-failed);"></span> Last build failed${timeAgo}`);
  } else if (hasRunning) {
    setStatusHtml(html`<span class="piko-card-status-dot" style="background:var(--status-started);"></span> Running${timeAgo}`);
  } else if (hasSucceeded) {
    setStatusHtml(html`<span class="piko-card-status-dot" style="background:var(--status-succeeded);"></span> Last build passed${timeAgo}`);
  } else {
    setStatusHtml(html`<span style="color:var(--text-muted);">No builds</span>`);
  }
}
