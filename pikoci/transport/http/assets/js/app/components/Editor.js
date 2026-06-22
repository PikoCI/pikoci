'use strict';

import { html } from 'htm/preact';
import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import { route } from 'preact-router';
import { useRequireAuth } from '../hooks.js';
import { apiNotice } from '../state.js';
import { createPipeline, updatePipeline, fetchPipeline, previewPipelineImage } from '../api.js';
import { parseHCLErrors, blockTypes } from '../utils.js';
import { PikoGraphZoom } from '../graph-zoom.js';

// ================================================================
// HCL stream language for CodeMirror
// ================================================================
function hclLanguage() {
  const CM = window.PikoCM;
  const kwList = 'job resource resource_type runner_type secret_type service_type notification_type notification variable get put notify task service plan params start stop ready_check secret on_success on_failure on_cancel ensure concurrency';
  const keywordSet = {};
  kwList.split(' ').forEach(k => { keywordSet[k] = true; });
  const atoms = { true: true, false: true, null: true };
  return CM.StreamLanguage.define({
    startState() { return { inString: false, inComment: false }; },
    token(stream, state) {
      if (state.inComment) {
        const end = stream.match(/.*?\*\//);
        if (end) state.inComment = false;
        else stream.skipToEnd();
        return 'comment';
      }
      if (state.inString) {
        while (!stream.eol()) {
          const ch = stream.next();
          if (ch === '\\') stream.next();
          else if (ch === '"') { state.inString = false; break; }
        }
        return 'string';
      }
      if (stream.match(/\/\*/)) { state.inComment = true; return 'comment'; }
      if (stream.match(/\/\//) || stream.match(/#/)) { stream.skipToEnd(); return 'comment'; }
      if (stream.match(/"/)) { state.inString = true; return 'string'; }
      if (stream.match(/\d+(\.\d+)?/)) return 'number';
      if (stream.match(/[{}[\]()]/)) return 'bracket';
      if (stream.match(/[=:]/)) return 'operator';
      if (stream.match(/[a-zA-Z_][a-zA-Z0-9_]*/)) {
        const w = stream.current();
        if (keywordSet[w]) return 'keyword';
        if (atoms[w]) return 'atom';
        return 'variableName';
      }
      stream.next();
      return null;
    }
  });
}

// ================================================================
// CodeMirror themes
// ================================================================
function cmLightTheme() {
  const CM = window.PikoCM;
  return CM.EditorView.theme({
    '&': { backgroundColor: 'var(--bg-surface)', color: 'var(--text-primary)', fontFamily: 'var(--font-mono)', fontSize: '0.85rem' },
    '.cm-content': { caretColor: 'var(--text-primary)' },
    '.cm-gutters': { backgroundColor: 'var(--bg-muted)', color: 'var(--text-muted)', borderRight: '1px solid var(--border)' },
    '.cm-activeLineGutter': { backgroundColor: 'var(--primary-light)' },
    '.cm-activeLine': { backgroundColor: 'var(--primary-light)' },
    '.cm-selectionBackground': { backgroundColor: 'rgba(41,173,255,0.2) !important' },
    '.cm-cursor': { borderLeftColor: 'var(--text-primary)' },
    '.cm-matchingBracket': { backgroundColor: 'rgba(41,173,255,0.3)', outline: 'none' },
  }, { dark: false });
}

function cmDarkTheme() {
  const CM = window.PikoCM;
  return CM.EditorView.theme({
    '&': { backgroundColor: 'var(--bg-surface)', color: 'var(--text-primary)', fontFamily: 'var(--font-mono)', fontSize: '0.85rem' },
    '.cm-content': { caretColor: 'var(--text-primary)' },
    '.cm-gutters': { backgroundColor: 'var(--bg-muted)', color: 'var(--text-muted)', borderRight: '1px solid var(--border)' },
    '.cm-activeLineGutter': { backgroundColor: 'rgba(41,173,255,0.15)' },
    '.cm-activeLine': { backgroundColor: 'rgba(41,173,255,0.1)' },
    '.cm-selectionBackground': { backgroundColor: 'rgba(41,173,255,0.3) !important' },
    '.cm-cursor': { borderLeftColor: 'var(--text-primary)' },
    '.cm-matchingBracket': { backgroundColor: 'rgba(41,173,255,0.4)', outline: 'none' },
  }, { dark: true });
}

function cmHighlightLight() {
  const t = window.PikoCM.tags;
  return window.PikoCM.HighlightStyle.define([
    { tag: t.keyword, color: '#7B2FBE' },
    { tag: t.atom, color: '#AB5236' },
    { tag: t.string, color: '#00A83A' },
    { tag: t.comment, color: '#83769C', fontStyle: 'italic' },
    { tag: t.number, color: '#FF004D' },
    { tag: t.bracket, color: '#5F574F' },
    { tag: t.operator, color: '#5F574F' },
    { tag: t.variableName, color: '#1D2B53' },
  ]);
}

function cmHighlightDark() {
  const t = window.PikoCM.tags;
  return window.PikoCM.HighlightStyle.define([
    { tag: t.keyword, color: '#FF77A8' },
    { tag: t.atom, color: '#FFA300' },
    { tag: t.string, color: '#00E756' },
    { tag: t.comment, color: '#83769C', fontStyle: 'italic' },
    { tag: t.number, color: '#FF004D' },
    { tag: t.bracket, color: '#C2C3C7' },
    { tag: t.operator, color: '#C2C3C7' },
    { tag: t.variableName, color: '#FFF1E8' },
  ]);
}

// ================================================================
// Helper: render DOT → SVG with Viz.js, apply PikoCI styling
// ================================================================
function renderGraphSVG(dotSource) {
  return window.Viz.instance().then(viz => {
    const svg = viz.renderSVGElement(dotSource);
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
          const minX = Math.min(...xs), maxX = Math.max(...xs);
          const minY = Math.min(...ys), maxY = Math.max(...ys);
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
    return svg;
  });
}

// ================================================================
// Helper: find block attribute position in doc text
// ================================================================
function findBlockAttribute(docText, blockType, blockName, attribute) {
  const esc = blockName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const blockRe = new RegExp(blockType + '\\s+"' + esc + '"\\s*\\{');
  const bm = blockRe.exec(docText);
  if (!bm) return null;
  const start = bm.index + bm[0].length;
  let depth = 1;
  let blockEnd = docText.length;
  for (let j = start; j < docText.length; j++) {
    if (docText[j] === '{') depth++;
    else if (docText[j] === '}') { depth--; if (depth === 0) { blockEnd = j; break; } }
  }
  const blockContent = docText.substring(start, blockEnd);
  const attrRe = new RegExp('(^|\\n)(\\s*)' + attribute + '\\s*=\\s*("(?:[^"\\\\]|\\\\.)*"|[^\\n]+)');
  const am = attrRe.exec(blockContent);
  if (!am) return null;
  const attrStart = start + am.index + am[1].length + am[2].length;
  const attrEnd = attrStart + am[0].length - am[1].length - am[2].length;
  return { from: attrStart, to: attrEnd };
}

// ================================================================
// Helper: escape HTML for safe insertion
// ================================================================
function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

// ================================================================
// Editor component
// ================================================================
export function Editor({ pipeline, teamCanonical, _isLocal, onSave, onSaveSuccess }) {
  const editorRef = useRef(null);       // CodeMirror EditorView
  const editorElRef = useRef(null);     // #pipeline-editor DOM element
  const graphRef = useRef(null);        // #graph container (strip)
  const graphFsRef = useRef(null);      // #graph-fullscreen container (bottom)
  const graphZoomRef = useRef(null);
  const graphZoomFsRef = useRef(null);
  const previewTimerRef = useRef(null);
  const themeObserverRef = useRef(null);
  const themeCompRef = useRef(null);
  const highlightCompRef = useRef(null);
  const errorLinesRef = useRef({});
  const escHandlerRef = useRef(null);
  const docClickHandlerRef = useRef(null);

  const [activeTab, setActiveTab] = useState('hcl');
  const [docsOpen, setDocsOpen] = useState(false);
  const [blocksCollapsed, setBlocksCollapsed] = useState(false);
  const [graphBottomVisible, setGraphBottomVisible] = useState(false);
  const [graphStripOpen, setGraphStripOpen] = useState(true);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [blocksHtml, setBlocksHtml] = useState('');
  const [hasErrors, setHasErrors] = useState(false);

  const nameRef = useRef(null);
  const publicRef = useRef(null);
  const varsRef = useRef(null);
  const pipelineRef = useRef(null);

  const initialRaw = pipeline && pipeline.raw ? atob(pipeline.raw) : '';
  const isUpdate = !!(pipeline && pipeline.id);

  // Pre-fill name and public checkbox for edit mode
  useEffect(() => {
    if (pipeline && nameRef.current) {
      nameRef.current.value = pipeline.name || '';
      nameRef.current.setAttribute('value', pipeline.name || '');
    }
    if (pipeline && publicRef.current) {
      publicRef.current.checked = !!pipeline.public;
    }
  }, [pipeline]);

  // ---- Build blocks panel HTML from editor content ----
  const updateBlocksPanel = useCallback(() => {
    const editor = editorRef.current;
    if (!editor) return;
    const doc = editor.state.doc.toString();
    const errorLines = errorLinesRef.current;
    let result = '';
    blockTypes.forEach(bt => {
      const twoLabel = bt.type === 'resource' || bt.type === 'notification';
      const re = twoLabel
        ? new RegExp(bt.type + '\\s+"([^"]+)"\\s+"([^"]+)"', 'g')
        : new RegExp(bt.type + '\\s+"([^"]+)"', 'g');
      const matches = [];
      let m;
      while ((m = re.exec(doc)) !== null) {
        const displayName = twoLabel ? m[1] + '.' + m[2] : m[1];
        matches.push({ name: displayName, pos: m.index });
      }
      if (matches.length === 0) return;
      result += '<div class="piko-blocks-section">';
      result += '<div class="piko-blocks-section-title">' + bt.label + '</div>';
      matches.forEach(match => {
        const hasErr = errorLines[bt.type + ':"' + match.name + '"'];
        result += '<div class="piko-blocks-item' + (hasErr ? ' has-error' : '') + '" data-pos="' + match.pos + '">';
        result += '<span class="piko-block-icon ' + bt.icon + '">' + bt.letter + '</span> ';
        result += escapeHtml(match.name);
        if (hasErr) result += '<span class="piko-error-dot"></span>';
        result += '</div>';
      });
      result += '</div>';
    });
    setBlocksHtml(result || '<div style="padding:12px;color:var(--text-muted);font-size:0.78rem">No blocks found</div>');
    setHasErrors(Object.keys(errorLines).length > 0);
  }, []);

  // ---- Show editor error diagnostics ----
  const showEditorErrors = useCallback((errorStr) => {
    const editor = editorRef.current;
    if (!editor) return;
    const CM = window.PikoCM;
    const diags = parseHCLErrors(errorStr);
    const doc = editor.state.doc;
    const docText = doc.toString();
    const diagnostics = [];
    const errorLines = {};

    for (let i = 0; i < diags.length; i++) {
      const d = diags[i];
      if (d.blockType) {
        errorLines[d.blockType + ':"' + d.blockName + '"'] = true;
        const pos = findBlockAttribute(docText, d.blockType, d.blockName, d.attribute);
        if (pos) {
          diagnostics.push({ from: pos.from, to: pos.to, severity: 'error', message: d.message });
        } else {
          const esc = d.blockName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
          const headerRe = new RegExp(d.blockType + '\\s+"' + esc + '"');
          const hm = headerRe.exec(docText);
          if (hm) {
            diagnostics.push({ from: hm.index, to: hm.index + hm[0].length, severity: 'error', message: d.message });
          }
        }
      } else {
        if (d.line < 1 || d.line > doc.lines) continue;
        const lineOffset = doc.line(d.line).from;
        const blockRe = /(?:job|resource|resource_type|runner_type|secret_type|service_type|notification_type|notification|variable)\s+"([^"]+)"/g;
        let bm, lastBlock = null;
        while ((bm = blockRe.exec(docText)) !== null) {
          if (bm.index <= lineOffset) lastBlock = bm;
          else break;
        }
        if (lastBlock) {
          const btMatch = lastBlock[0].match(/^(\w+)/);
          if (btMatch) errorLines[btMatch[1] + ':"' + lastBlock[1] + '"'] = true;
        }
        const line = doc.line(d.line);
        let from = line.from + Math.max(0, d.colStart - 1);
        let to = line.from + Math.min(line.length, d.colEnd - 1);
        if (from > doc.length) from = doc.length;
        if (to > doc.length) to = doc.length;
        if (from >= to) to = from + 1;
        if (to > doc.length) to = doc.length;
        diagnostics.push({ from, to, severity: 'error', message: d.message });
      }
    }

    errorLinesRef.current = errorLines;
    editor.dispatch(CM.setDiagnostics(editor.state, diagnostics));
    updateBlocksPanel();
  }, [updateBlocksPanel]);

  // ---- Clear editor error diagnostics ----
  const clearEditorErrors = useCallback(() => {
    const editor = editorRef.current;
    if (!editor) return;
    const CM = window.PikoCM;
    errorLinesRef.current = {};
    editor.dispatch(CM.setDiagnostics(editor.state, []));
    updateBlocksPanel();
  }, [updateBlocksPanel]);

  // ---- Render graph into a container ----
  const renderGraphToContainer = useCallback((dotSource, containerEl, zoomRef) => {
    if (!containerEl || !dotSource) return;
    renderGraphSVG(dotSource).then(svg => {
      // Clear all children except piko-graph-controls
      const controls = containerEl.querySelector('.piko-graph-controls');
      containerEl.innerHTML = '';
      if (controls) containerEl.appendChild(controls);
      containerEl.appendChild(svg);
      // Remove links
      containerEl.querySelectorAll('a').forEach(a => a.removeAttribute('xlink:href'));
      if (!zoomRef.current) {
        zoomRef.current = new PikoGraphZoom(containerEl);
      }
      zoomRef.current.attachSVG(svg);
    }).catch(err => {
      const controls = containerEl.querySelector('.piko-graph-controls');
      containerEl.innerHTML = '';
      if (controls) containerEl.appendChild(controls);
      const errDiv = document.createElement('div');
      errDiv.className = 'piko-graph-error';
      errDiv.textContent = err.message || String(err);
      containerEl.appendChild(errDiv);
    });
  }, []);

  // ---- Preview pipeline (fetch graph image) ----
  const changePipeline = useCallback(() => {
    const editor = editorRef.current;
    const pp = editor ? editor.state.doc.toString() : '';
    const rvars = (varsRef.current && varsRef.current.value) || '{}';
    let vars;
    try {
      vars = JSON.parse(rvars);
    } catch (error) {
      apiNotice.value = { ...apiNotice.value, error: 'Error parsing Vars: ' + error };
      return;
    }
    const data = [];
    for (let i = 0; i < pp.length; i++) {
      data.push(pp.charCodeAt(i));
    }
    previewPipelineImage(teamCanonical, { config: data, vars }).then(response => {
      clearEditorErrors();
      if (response && response.image) {
        renderGraphToContainer(response.image, graphRef.current, graphZoomRef);
        renderGraphToContainer(response.image, graphFsRef.current, graphZoomFsRef);
      }
    }).catch(err => {
      let errorMsg = '';
      if (err && err.error) {
        errorMsg = err.error;
      } else if (err && err.message) {
        errorMsg = err.message;
      } else {
        errorMsg = String(err);
      }
      if (errorMsg) showEditorErrors(errorMsg);
    });
  }, [teamCanonical, clearEditorErrors, showEditorErrors, renderGraphToContainer]);

  // ---- Click block item → jump to position ----
  const clickBlock = useCallback((e) => {
    const item = e.target.closest('.piko-blocks-item');
    if (!item) return;
    const pos = parseInt(item.getAttribute('data-pos'), 10);
    if (isNaN(pos) || !editorRef.current) return;
    const panel = item.closest('#blocks-panel');
    if (panel) {
      panel.querySelectorAll('.piko-blocks-item').forEach(el => el.classList.remove('active'));
    }
    item.classList.add('active');
    const CM = window.PikoCM;
    editorRef.current.dispatch({
      selection: { anchor: pos },
      effects: CM.EditorView.scrollIntoView(pos, { y: 'start' }),
    });
    editorRef.current.focus();
  }, []);

  // ---- Click graph node → jump to block in editor ----
  const clickGraphNode = useCallback((e) => {
    const g = e.target.closest('g.node');
    if (!g) return;
    const texts = g.querySelectorAll('text');
    const textEl = texts[texts.length - 1];
    if (!textEl || !editorRef.current) return;
    const name = textEl.textContent.trim();
    if (!name) return;
    const doc = editorRef.current.state.doc.toString();
    const dotIdx = name.indexOf('.');
    let re;
    if (dotIdx !== -1) {
      const rType = name.substring(0, dotIdx).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      const rLabel = name.substring(dotIdx + 1).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      re = new RegExp('(?:resource|notification)\\s+"' + rType + '"\\s+"' + rLabel + '"');
    } else {
      let searchName = name;
      const dashDashIdx = name.indexOf('--');
      if (dashDashIdx !== -1) {
        searchName = name.substring(0, dashDashIdx);
      }
      re = new RegExp('(?:job|resource|resource_type|runner_type|secret_type|service_type|notification_type|notification)\\s+"' + searchName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '"');
    }
    const match = re.exec(doc);
    if (match) {
      const CM = window.PikoCM;
      editorRef.current.dispatch({
        selection: { anchor: match.index, head: match.index + match[0].length },
        effects: CM.EditorView.scrollIntoView(match.index, { y: 'start' }),
      });
      editorRef.current.focus();
    }
  }, []);

  // ---- Submit handler ----
  const handleSubmit = useCallback((e) => {
    e.preventDefault();
    const editor = editorRef.current;
    const nameVal = nameRef.current ? nameRef.current.value : '';
    const pp = editor ? editor.state.doc.toString() : (pipelineRef.current ? pipelineRef.current.value : '');
    const rvars = (varsRef.current && varsRef.current.value) || '{}';
    const isPublic = publicRef.current ? publicRef.current.checked : false;
    let vars;
    try {
      vars = JSON.parse(rvars);
    } catch (error) {
      apiNotice.value = { ...apiNotice.value, error: 'Error parsing Vars: ' + error };
      return;
    }
    const data = [];
    for (let i = 0; i < pp.length; i++) {
      data.push(pp.charCodeAt(i));
    }
    const payload = isUpdate
      ? { name: nameVal, canonical: pipeline.canonical, config: data, vars, public: isPublic }
      : { name: nameVal, config: data, vars };

    onSave(payload).then(resp => {
      if (onSaveSuccess) onSaveSuccess(resp);
    }).catch(err => {
      const msg = (err && err.error) || (err && err.message) || String(err);
      apiNotice.value = { ...apiNotice.value, error: msg };
    });
  }, [isUpdate, pipeline, onSave, onSaveSuccess]);

  // ---- Initialize CodeMirror ----
  useEffect(() => {
    const CM = window.PikoCM;
    if (!CM || !editorElRef.current) return;

    const themeComp = new CM.Compartment();
    const highlightComp = new CM.Compartment();
    themeCompRef.current = themeComp;
    highlightCompRef.current = highlightComp;

    const isDark = document.documentElement.getAttribute('data-theme') === 'dark';
    const theme = isDark ? cmDarkTheme() : cmLightTheme();
    const highlight = isDark ? cmHighlightDark() : cmHighlightLight();

    const editor = new CM.EditorView({
      state: CM.EditorState.create({
        doc: initialRaw,
        extensions: [
          CM.lineNumbers(),
          CM.highlightActiveLine(),
          CM.drawSelection(),
          CM.history(),
          CM.bracketMatching(),
          CM.closeBrackets(),
          CM.indentOnInput(),
          CM.foldGutter(),
          CM.lintGutter(),
          CM.search(),
          CM.highlightSelectionMatches(),
          themeComp.of(theme),
          highlightComp.of(CM.syntaxHighlighting(highlight)),
          hclLanguage(),
          CM.keymap.of([
            ...CM.closeBracketsKeymap,
            ...CM.defaultKeymap,
            ...CM.searchKeymap,
            ...CM.historyKeymap,
            CM.indentWithTab,
          ]),
          CM.EditorView.updateListener.of(update => {
            if (update.docChanged) {
              if (pipelineRef.current) {
                pipelineRef.current.value = editorRef.current.state.doc.toString();
              }
              clearTimeout(previewTimerRef.current);
              previewTimerRef.current = setTimeout(() => {
                changePipeline();
                updateBlocksPanel();
              }, 500);
            }
          }),
        ],
      }),
      parent: editorElRef.current,
    });

    editorRef.current = editor;
    window._pikoEditor = editor;

    // Theme observer
    const observer = new MutationObserver(() => {
      const dark = document.documentElement.getAttribute('data-theme') === 'dark';
      editor.dispatch({
        effects: [
          themeComp.reconfigure(dark ? cmDarkTheme() : cmLightTheme()),
          highlightComp.reconfigure(CM.syntaxHighlighting(dark ? cmHighlightDark() : cmHighlightLight())),
        ]
      });
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
    themeObserverRef.current = observer;

    // Escape handler for fullscreen
    const escHandler = (e) => {
      if (e.key === 'Escape') {
        const card = document.getElementById('editor-card');
        if (card && card.classList.contains('fullscreen')) {
          card.classList.remove('fullscreen');
          document.body.classList.remove('piko-fullscreen');
          setIsFullscreen(false);
        }
      }
    };
    document.addEventListener('keydown', escHandler);
    escHandlerRef.current = escHandler;

    // Doc click handler for docs dropdown
    const docClick = (e) => {
      if (!e.target.closest('.piko-docs-dropdown')) {
        setDocsOpen(false);
      }
    };
    document.addEventListener('click', docClick);
    docClickHandlerRef.current = docClick;

    // Initial blocks + graph
    updateBlocksPanel();
    if (initialRaw) {
      changePipeline();
    }

    // Cleanup
    return () => {
      clearTimeout(previewTimerRef.current);
      if (themeObserverRef.current) themeObserverRef.current.disconnect();
      if (editorRef.current) editorRef.current.destroy();
      if (escHandlerRef.current) document.removeEventListener('keydown', escHandlerRef.current);
      if (docClickHandlerRef.current) document.removeEventListener('click', docClickHandlerRef.current);
      if (graphZoomRef.current) graphZoomRef.current.destroy();
      if (graphZoomFsRef.current) graphZoomFsRef.current.destroy();
      document.body.classList.remove('piko-fullscreen');
      window._pikoEditor = null;
    };
  }, []); // run once on mount

  // ---- Graph node click delegation ----
  useEffect(() => {
    const graphEl = graphRef.current;
    const graphFsEl = graphFsRef.current;
    if (graphEl) graphEl.addEventListener('click', clickGraphNode);
    if (graphFsEl) graphFsEl.addEventListener('click', clickGraphNode);
    return () => {
      if (graphEl) graphEl.removeEventListener('click', clickGraphNode);
      if (graphFsEl) graphFsEl.removeEventListener('click', clickGraphNode);
    };
  }, [clickGraphNode]);

  // ---- Tab switching ----
  const showHclTab = useCallback(() => setActiveTab('hcl'), []);
  const showVarsTab = useCallback(() => setActiveTab('vars'), []);

  // ---- Toggle docs ----
  const toggleDocs = useCallback((e) => {
    e.stopPropagation();
    setDocsOpen(prev => !prev);
  }, []);

  // ---- Toggle blocks ----
  const toggleBlocks = useCallback(() => {
    setBlocksCollapsed(prev => !prev);
  }, []);

  // ---- Toggle graph bottom panel ----
  const toggleGraphPanel = useCallback(() => {
    setGraphBottomVisible(prev => !prev);
  }, []);

  // ---- Toggle fullscreen ----
  const toggleFullscreen = useCallback((e) => {
    if (e && e.preventDefault) e.preventDefault();
    setIsFullscreen(prev => {
      const next = !prev;
      const card = document.getElementById('editor-card');
      if (card) {
        if (next) card.classList.add('fullscreen');
        else card.classList.remove('fullscreen');
      }
      if (next) document.body.classList.add('piko-fullscreen');
      else document.body.classList.remove('piko-fullscreen');
      return next;
    });
  }, []);

  // ---- Toggle graph strip ----
  const toggleGraphStrip = useCallback(() => {
    setGraphStripOpen(prev => !prev);
  }, []);

  return html`
    <div class="piko-page-header mb-3">
      <h1 class="h4 fw-bold mb-0">${isUpdate ? 'Update Pipeline' : 'New Pipeline'}</h1>
    </div>
    <form onSubmit=${handleSubmit}>
      <div class="piko-settings-row mb-3">
        <div class="piko-field">
          <label class="form-label">Name</label>
          <input type="text" class="form-control" id="name" ref=${nameRef}
            placeholder="e.g. deploy-production" style="width:280px" />
        </div>
        ${isUpdate && html`
          <div class="piko-checkbox-inline">
            <input type="checkbox" class="form-check-input" id="public" ref=${publicRef} />
            <label class="form-check-label" for="public">Public</label>
          </div>
        `}
      </div>
      <div class="piko-editor-card" id="editor-card">
        <div class="piko-editor-toolbar">
          <div class="piko-tab${activeTab === 'hcl' ? ' active' : ''}" id="tab-hcl" onClick=${showHclTab}>pipeline.hcl</div>
          <div class="piko-tab${activeTab === 'vars' ? ' active' : ''}" id="tab-vars" onClick=${showVarsTab}>vars.json</div>
          <div class="piko-toolbar-spacer"></div>
          <div class="piko-docs-dropdown" id="docs-dropdown">
            <button type="button" class="piko-tbtn" id="docs-btn" title="Pipeline documentation" onClick=${toggleDocs}>
              <i class="bi bi-book"></i> <span class="piko-tbtn-label">Docs</span> <i class="bi bi-chevron-down" style="font-size:0.6rem;margin-left:2px"></i>
            </button>
            <div class="piko-docs-menu${docsOpen ? ' open' : ''}" id="docs-menu">
              <a href="https://docs.pikoci.com/Pipeline" target="_blank" rel="noopener"><i class="bi bi-file-text"></i> Pipeline overview</a>
              <div class="piko-docs-divider"></div>
              <div class="piko-docs-label">Blocks</div>
              <a href="https://docs.pikoci.com/Pipeline#job" target="_blank" rel="noopener"><span class="piko-block-icon jb">J</span> job</a>
              <a href="https://docs.pikoci.com/Pipeline#resource" target="_blank" rel="noopener"><span class="piko-block-icon rs">R</span> resource</a>
              <a href="https://docs.pikoci.com/Pipeline#resource_type" target="_blank" rel="noopener"><span class="piko-block-icon rt">R</span> resource_type</a>
              <a href="https://docs.pikoci.com/Pipeline#runner_type" target="_blank" rel="noopener"><span class="piko-block-icon rn">R</span> runner_type</a>
              <a href="https://docs.pikoci.com/Pipeline#secret_type" target="_blank" rel="noopener"><span class="piko-block-icon st">S</span> secret_type</a>
              <a href="https://docs.pikoci.com/Pipeline#service_type" target="_blank" rel="noopener"><span class="piko-block-icon sv">S</span> service_type</a>
              <a href="https://docs.pikoci.com/Notifications#notification-types" target="_blank" rel="noopener"><span class="piko-block-icon nt">N</span> notification_type</a>
              <a href="https://docs.pikoci.com/Notifications#notifications" target="_blank" rel="noopener"><span class="piko-block-icon no">N</span> notification</a>
              <a href="https://docs.pikoci.com/Pipeline#variable" target="_blank" rel="noopener"><span class="piko-block-icon vr">V</span> variable</a>
            </div>
          </div>
          <div class="piko-toolbar-sep"></div>
          <button type="button" class="piko-tbtn${!blocksCollapsed ? ' active' : ''}" id="blocks-btn" title="Toggle blocks panel" onClick=${toggleBlocks}>
            <i class="bi bi-list-nested"></i> <span class="piko-tbtn-label">Blocks</span>
            <span class="piko-error-badge${hasErrors ? ' visible' : ''}"></span>
          </button>
          <div class="piko-toolbar-sep"></div>
          <button type="button" class="piko-tbtn${graphBottomVisible ? ' active' : ''}" id="graph-btn" title="Toggle graph preview" onClick=${toggleGraphPanel}>
            <i class="bi bi-diagram-3"></i>
          </button>
          <div class="piko-toolbar-sep"></div>
          <button type="button" class="piko-tbtn" id="fullscreen-btn" title="Fullscreen (Esc to exit)" onClick=${toggleFullscreen}>
            <i class="bi ${isFullscreen ? 'bi-fullscreen-exit' : 'bi-arrows-fullscreen'}"></i>
          </button>
        </div>
        <div class="piko-editor-body">
          <div class="piko-blocks-panel${blocksCollapsed ? ' collapsed' : ''}" id="blocks-panel"
            onClick=${clickBlock} dangerouslySetInnerHTML=${{ __html: blocksHtml }}></div>
          <div class="piko-code-area" id="code-area" style=${activeTab !== 'hcl' ? 'display:none' : ''}>
            <div id="pipeline-editor" ref=${editorElRef}></div>
            <textarea id="pipeline" ref=${pipelineRef} style="display:none">${initialRaw}</textarea>
          </div>
          <div class="piko-vars-area${activeTab === 'vars' ? ' visible' : ''}" id="vars-area">
            <textarea type="text" rows="10" class="form-control" id="vars" ref=${varsRef} placeholder='${'{"key": "value"}'}'></textarea>
            <div class="piko-vars-hint">JSON object passed as variables to the pipeline definition.</div>
          </div>
        </div>
        <div class="piko-graph-bottom${graphBottomVisible ? ' visible' : ''}" id="graph-bottom-panel">
          <div class="piko-graph-bottom-header">
            <span><i class="bi bi-diagram-3"></i> Graph Preview</span>
            <button type="button" id="graph-bottom-close" title="Close" onClick=${toggleGraphPanel}><i class="bi bi-x-lg"></i></button>
          </div>
          <div class="piko-graph-bottom-body" id="graph-fullscreen" ref=${graphFsRef}></div>
        </div>
      </div>
      <div class="piko-graph-strip" id="graph-strip">
        <div class="piko-graph-strip-header${graphStripOpen ? ' open' : ''}" id="graph-strip-header" onClick=${toggleGraphStrip}>
          <span><i class="bi bi-diagram-3"></i> Graph Preview</span>
          <i class="bi bi-chevron-right piko-graph-chev"></i>
        </div>
        <div class="piko-graph-strip-body" id="graph" ref=${graphRef}>
        </div>
      </div>
      <button type="submit" class="btn btn-primary mt-2">${isUpdate ? 'Update' : 'Create'}</button>
    </form>
  `;
}

// ================================================================
// PipelineNew wrapper
// ================================================================
export function PipelineNew({ tc }) {
  useRequireAuth({ adminOnly: true, teamCanonical: tc });

  const onSave = useCallback((data) => {
    return createPipeline(tc, data);
  }, [tc]);

  const onSaveSuccess = useCallback((resp) => {
    const canonical = resp && (resp.canonical || (resp.data && resp.data.canonical));
    if (canonical) {
      route('/teams/' + tc + '/pipelines/' + canonical);
    }
  }, [tc]);

  return html`<${Editor} pipeline=${null} teamCanonical=${tc} onSave=${onSave} onSaveSuccess=${onSaveSuccess} />`;
}

// ================================================================
// PipelineEdit wrapper
// ================================================================
export function PipelineEdit({ tc, pn }) {
  useRequireAuth({ adminOnly: true, teamCanonical: tc, redirectTo: '/teams/' + tc + '/pipelines/' + pn });
  const [pipeline, setPipeline] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetchPipeline(tc, pn).then(p => {
      setPipeline(p);
      setLoading(false);
    }).catch(err => {
      apiNotice.value = { ...apiNotice.value, error: (err && err.message) || String(err) };
      setLoading(false);
    });
  }, [tc, pn]);

  const onSave = useCallback((data) => {
    return updatePipeline(tc, pn, data);
  }, [tc, pn]);

  const onSaveSuccess = useCallback(() => {
    route('/teams/' + tc + '/pipelines/' + pn);
  }, [tc, pn]);

  if (loading) return html`<div class="text-center py-5"><div class="spinner-border" role="status"></div></div>`;
  if (!pipeline) return html`<div class="text-muted py-3">Pipeline not found.</div>`;

  return html`<${Editor} pipeline=${pipeline} teamCanonical=${tc} onSave=${onSave} onSaveSuccess=${onSaveSuccess} />`;
}
