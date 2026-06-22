'use strict';

import { html } from 'htm/preact';
import { useRef, useEffect } from 'preact/hooks';

/**
 * PipelineGraph — renders a Graphviz DOT source string as an SVG.
 *
 * Props:
 *   dotSource        — DOT source string
 *   onSVGReady(svg)  — callback after the SVG element is inserted into the DOM
 *   noLinks          — if true, strip xlink:href from <a> elements (for cards)
 *   highlightedEdges — reserved for future use
 */
export function PipelineGraph({ dotSource, onSVGReady, noLinks, highlightedEdges }) {
  const containerRef = useRef(null);

  useEffect(() => {
    if (!dotSource || !containerRef.current) return;

    let cancelled = false;

    window.Viz.instance().then(viz => {
      if (cancelled) return;
      const svg = viz.renderSVGElement(dotSource);
      postProcessSVG(svg, noLinks);

      const el = containerRef.current;
      if (!el) return;
      // Replace contents
      el.innerHTML = '';
      el.appendChild(svg);

      // Apply version tracking edge highlights if present
      if (highlightedEdges && highlightedEdges.length > 0) {
        applyEdgeHighlights(svg, highlightedEdges);
      }

      if (onSVGReady) onSVGReady(svg);
    }).catch(err => {
      if (cancelled) return;
      const el = containerRef.current;
      if (!el) return;
      const escaped = document.createElement('span');
      escaped.textContent = err.message || String(err);
      el.innerHTML = '<div class="piko-graph-error">' + escaped.innerHTML + '</div>';
    });

    return () => { cancelled = true; };
  }, [dotSource, noLinks, highlightedEdges]);

  return html`<div id="pipeline-graph" ref=${containerRef}></div>`;
}

/**
 * Post-process the SVG element produced by Viz.js:
 *  - Set viewBox and responsive width
 *  - Remove background polygons (first polygon or white fills)
 *  - Convert 4-point polygons to rounded rects
 *  - Set custom font on text
 *  - Make nodes clickable (cursor: pointer)
 *  - Optionally strip links
 */
function postProcessSVG(svg, noLinks) {
  const naturalWidth = parseFloat(svg.getAttribute('width'));
  const naturalHeight = parseFloat(svg.getAttribute('height'));
  if (naturalWidth && naturalHeight) {
    svg.setAttribute('viewBox', '0 0 ' + naturalWidth + ' ' + naturalHeight);
  }
  svg.setAttribute('width', '100%');
  svg.removeAttribute('height');
  svg.style.maxWidth = (naturalWidth || 800) + 'px';
  svg.style.maxHeight = '400px';
  svg.style.background = 'transparent';

  svg.querySelectorAll('polygon, rect').forEach((el, i) => {
    const f = (el.getAttribute('fill') || '').toLowerCase();
    if (i === 0 || f === 'white' || f === '#ffffff') {
      el.setAttribute('fill', 'transparent');
      el.setAttribute('stroke', 'transparent');
      return;
    }
    if (el.tagName === 'polygon' && f && f !== 'none' && f !== 'transparent') {
      const points = el.getAttribute('points');
      if (!points) return;
      const pts = points.trim().split(/\s+/).map(p => {
        const xy = p.split(',');
        return { x: parseFloat(xy[0]), y: parseFloat(xy[1]) };
      });
      if (pts.length === 4 || (pts.length === 5 && pts[0].x === pts[4].x && pts[0].y === pts[4].y)) {
        const xs = pts.map(p => p.x);
        const ys = pts.map(p => p.y);
        const minX = Math.min(...xs);
        const maxX = Math.max(...xs);
        const minY = Math.min(...ys);
        const maxY = Math.max(...ys);
        const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
        rect.setAttribute('x', minX);
        rect.setAttribute('y', minY);
        rect.setAttribute('width', maxX - minX);
        rect.setAttribute('height', maxY - minY);
        rect.setAttribute('rx', '4');
        rect.setAttribute('ry', '4');
        rect.setAttribute('fill', el.getAttribute('fill'));
        rect.setAttribute('stroke', el.getAttribute('stroke') || 'none');
        el.parentNode.replaceChild(rect, el);
      }
    }
  });

  svg.querySelectorAll('text').forEach(t => {
    t.style.fontFamily = "'Plus Jakarta Sans', system-ui, sans-serif";
  });
  svg.querySelectorAll('g.node').forEach(g => {
    g.style.cursor = 'pointer';
  });

  if (noLinks) {
    svg.querySelectorAll('a').forEach(a => {
      a.removeAttribute('xlink:href');
      a.removeAttributeNS('http://www.w3.org/1999/xlink', 'href');
    });
  }
}

/**
 * Highlight SVG edges that belong to a tracked version's path.
 * Each edge in Graphviz SVG is a <g class="edge"> with a <title> like "node1->node2".
 * highlightedEdges is an array of job names whose edges should be highlighted.
 */
function applyEdgeHighlights(svg, highlightedEdges) {
  if (!svg || !highlightedEdges || !highlightedEdges.length) return;

  const jobSet = new Set(highlightedEdges);

  // Reset all edges first
  svg.querySelectorAll('g.edge').forEach(g => {
    g.classList.remove('piko-edge-highlighted');
    const path = g.querySelector('path');
    if (path) path.removeAttribute('data-highlighted');
  });

  // Highlight edges connected to tracked jobs
  svg.querySelectorAll('g.edge').forEach(g => {
    const title = g.querySelector('title');
    if (!title) return;
    const text = title.textContent || '';
    // Edge titles are like "resource.name->job.name" or "job.name->resource.name"
    const parts = text.split('->');
    if (parts.length !== 2) return;

    const src = parts[0].trim();
    const dst = parts[1].trim();

    if (jobSet.has(src) || jobSet.has(dst)) {
      g.classList.add('piko-edge-highlighted');
      const path = g.querySelector('path');
      if (path) {
        path.setAttribute('data-highlighted', 'true');
        path.setAttribute('stroke', 'var(--primary, #29ADFF)');
        path.setAttribute('stroke-width', '2.5');
      }
      const polygon = g.querySelector('polygon');
      if (polygon) {
        polygon.setAttribute('fill', 'var(--primary, #29ADFF)');
        polygon.setAttribute('stroke', 'var(--primary, #29ADFF)');
      }
    }
  });
}
