'use strict';

import { PipelineImage } from '../models.js';
import { parseHCLErrors, blockTypes } from '../namespace.js';

// ================================================================
// PikoGraphZoom — zoom, pan, fullscreen for pipeline graph SVGs
// ================================================================
export var PikoGraphZoom = function(containerEl, opts) {
  opts = opts || {};
  this.container = containerEl;
  this.svg = null;
  this.baseViewBox = null;
  this.currentViewBox = null;
  this.zoomLevel = 1;
  this.isPanning = false;
  this.panStart = null;
  this.onFullscreenChange = opts.onFullscreenChange || null;
  this._fsOverlay = null;
  this._bound = {};
  this._injectControls();
  this._bindContainerEvents();
};

PikoGraphZoom.prototype = {
  MIN_ZOOM: 0.25,
  MAX_ZOOM: 4,
  ZOOM_FACTOR: 1.15,
  DRAG_THRESHOLD: 5,

  _injectControls: function() {
    var div = document.createElement('div');
    div.className = 'piko-graph-controls';
    div.innerHTML =
      '<button type="button" title="Zoom in" data-action="zoomIn">+</button>' +
      '<button type="button" title="Zoom out" data-action="zoomOut">&minus;</button>' +
      '<button type="button" title="Reset" data-action="reset">&#x21BA;</button>' +
      '<button type="button" title="Fullscreen" data-action="fullscreen">&#x26F6;</button>';
    this.container.appendChild(div);
    this._controlsEl = div;
    var that = this;
    div.addEventListener('click', function(e) {
      var btn = e.target.closest('button');
      if (!btn) return;
      e.stopPropagation();
      var action = btn.getAttribute('data-action');
      if (action === 'zoomIn') that.zoomIn();
      else if (action === 'zoomOut') that.zoomOut();
      else if (action === 'reset') that.reset();
      else if (action === 'fullscreen') that.toggleFullscreen();
    });
  },

  _bindContainerEvents: function() {
    var that = this;
    this._bound.wheel = function(e) { that._onWheel(e); };
    this._bound.mousedown = function(e) { that._onPanStart(e); };
    this._bound.mousemove = function(e) { that._onPanMove(e); };
    this._bound.mouseup = function(e) { that._onPanEnd(e); };
    this._bound.touchstart = function(e) { that._onTouchStart(e); };
    this._bound.touchmove = function(e) { that._onTouchMove(e); };
    this._bound.touchend = function(e) { that._onPanEnd(e); };
    this._bound.click = function(e) {
      if (that._suppressClick) {
        that._suppressClick = false;
        e.preventDefault();
        e.stopPropagation();
      }
    };
    this.container.addEventListener('wheel', this._bound.wheel, {passive: false});
    this.container.addEventListener('mousedown', this._bound.mousedown);
    window.addEventListener('mousemove', this._bound.mousemove);
    window.addEventListener('mouseup', this._bound.mouseup);
    this.container.addEventListener('click', this._bound.click, true);
    this.container.addEventListener('touchstart', this._bound.touchstart, {passive: false});
    this.container.addEventListener('touchmove', this._bound.touchmove, {passive: false});
    this.container.addEventListener('touchend', this._bound.touchend);
  },

  attachSVG: function(svg) {
    this.svg = svg;
    var w = parseFloat(svg.getAttribute('width'));
    var h = parseFloat(svg.getAttribute('height'));
    if (!w || !h) {
      var vb = svg.getAttribute('viewBox');
      if (vb) {
        var parts = vb.split(/[\s,]+/);
        w = parseFloat(parts[2]);
        h = parseFloat(parts[3]);
      }
    }
    var natW = w || 800;
    var natH = h || 400;
    var cRect = this.container.getBoundingClientRect();
    var cW = cRect.width || natW;
    var cH = cRect.height || natH;
    var scaleX = cW / natW;
    var scaleY = cH / natH;
    var scale = Math.min(scaleX, scaleY);
    var vbW, vbH;
    var MAX_SCALE = 1.5;
    if (scale > MAX_SCALE) {
      vbW = cW / MAX_SCALE;
      vbH = cH / MAX_SCALE;
    } else if (scale > 1) {
      vbW = natW;
      vbH = natH;
    } else {
      vbW = natW;
      vbH = natH;
    }
    var offsetX = -(vbW - natW) / 2;
    var offsetY = -(vbH - natH) / 2;
    this.baseViewBox = {x: offsetX, y: offsetY, w: vbW, h: vbH};
    if (!this.currentViewBox) {
      this.currentViewBox = {x: offsetX, y: offsetY, w: vbW, h: vbH};
      this.zoomLevel = 1;
    }
    svg.setAttribute('viewBox', this.currentViewBox.x + ' ' + this.currentViewBox.y + ' ' + this.currentViewBox.w + ' ' + this.currentViewBox.h);
    svg.setAttribute('width', '100%');
    svg.setAttribute('height', '100%');
    svg.style.maxWidth = 'none';
    svg.style.maxHeight = 'none';
  },

  _applyViewBox: function() {
    if (!this.svg || !this.currentViewBox) return;
    var vb = this.currentViewBox;
    this.svg.setAttribute('viewBox', vb.x + ' ' + vb.y + ' ' + vb.w + ' ' + vb.h);
  },

  zoomIn: function() {
    this._zoomAtCenter(this.ZOOM_FACTOR);
  },

  zoomOut: function() {
    this._zoomAtCenter(1 / this.ZOOM_FACTOR);
  },

  _zoomAtCenter: function(factor) {
    if (!this.currentViewBox || !this.baseViewBox) return;
    var newZoom = this.zoomLevel * factor;
    newZoom = Math.max(this.MIN_ZOOM, Math.min(this.MAX_ZOOM, newZoom));
    var vb = this.currentViewBox;
    var newW = this.baseViewBox.w / newZoom;
    var newH = this.baseViewBox.h / newZoom;
    var cx = vb.x + vb.w / 2;
    var cy = vb.y + vb.h / 2;
    this.currentViewBox = {x: cx - newW / 2, y: cy - newH / 2, w: newW, h: newH};
    this.zoomLevel = newZoom;
    this._applyViewBox();
  },

  zoomAtPoint: function(clientX, clientY, factor) {
    if (!this.svg || !this.currentViewBox || !this.baseViewBox) return;
    var rect = this.svg.getBoundingClientRect();
    var px = (clientX - rect.left) / rect.width;
    var py = (clientY - rect.top) / rect.height;
    var vb = this.currentViewBox;
    var pointX = vb.x + px * vb.w;
    var pointY = vb.y + py * vb.h;
    var newZoom = this.zoomLevel * factor;
    newZoom = Math.max(this.MIN_ZOOM, Math.min(this.MAX_ZOOM, newZoom));
    var newW = this.baseViewBox.w / newZoom;
    var newH = this.baseViewBox.h / newZoom;
    this.currentViewBox = {
      x: pointX - px * newW,
      y: pointY - py * newH,
      w: newW,
      h: newH
    };
    this.zoomLevel = newZoom;
    this._applyViewBox();
  },

  reset: function() {
    if (!this.baseViewBox) return;
    this.currentViewBox = {x: this.baseViewBox.x, y: this.baseViewBox.y, w: this.baseViewBox.w, h: this.baseViewBox.h};
    this.zoomLevel = 1;
    this._applyViewBox();
  },

  _onWheel: function(e) {
    if (!this.svg) return;
    e.preventDefault();
    var factor = e.deltaY < 0 ? this.ZOOM_FACTOR : 1 / this.ZOOM_FACTOR;
    this.zoomAtPoint(e.clientX, e.clientY, factor);
  },

  _getPanTarget: function() {
    return this._fsOverlay ? this._fsOverlay.querySelector('.piko-graph-fullscreen-body') : this.container;
  },

  _onPanStart: function(e) {
    if (e.button !== 0) return;
    if (e.target.closest && e.target.closest('.piko-graph-controls')) return;
    this.isPanning = true;
    this.panStart = {x: e.clientX, y: e.clientY, dragged: false};
    this._getPanTarget().classList.add('panning');
  },

  _onPanMove: function(e) {
    if (!this.isPanning || !this.panStart || !this.svg || !this.currentViewBox) return;
    var dx = e.clientX - this.panStart.x;
    var dy = e.clientY - this.panStart.y;
    if (Math.abs(dx) > this.DRAG_THRESHOLD || Math.abs(dy) > this.DRAG_THRESHOLD) {
      this.panStart.dragged = true;
    }
    if (!this.panStart.dragged) return;
    var rect = this.svg.getBoundingClientRect();
    var vb = this.currentViewBox;
    vb.x -= dx / rect.width * vb.w;
    vb.y -= dy / rect.height * vb.h;
    this.panStart.x = e.clientX;
    this.panStart.y = e.clientY;
    this._applyViewBox();
  },

  _onPanEnd: function(e) {
    var wasDragged = this.isPanning && this.panStart && this.panStart.dragged;
    if (this.isPanning) {
      this._getPanTarget().classList.remove('panning');
    }
    if (wasDragged) {
      this._suppressClick = true;
    }
    this.isPanning = false;
    this.panStart = null;
    this._pinchStart = null;
  },

  _onTouchStart: function(e) {
    if (e.touches.length === 1) {
      this.isPanning = true;
      this.panStart = {x: e.touches[0].clientX, y: e.touches[0].clientY, dragged: false};
      this._getPanTarget().classList.add('panning');
    } else if (e.touches.length === 2) {
      e.preventDefault();
      this._pinchStart = this._getPinchDist(e);
    }
  },

  _onTouchMove: function(e) {
    if (e.touches.length === 1 && this.isPanning && this.panStart && this.svg && this.currentViewBox) {
      var dx = e.touches[0].clientX - this.panStart.x;
      var dy = e.touches[0].clientY - this.panStart.y;
      if (Math.abs(dx) > this.DRAG_THRESHOLD || Math.abs(dy) > this.DRAG_THRESHOLD) {
        this.panStart.dragged = true;
      }
      if (!this.panStart.dragged) return;
      e.preventDefault();
      var rect = this.svg.getBoundingClientRect();
      var vb = this.currentViewBox;
      vb.x -= dx / rect.width * vb.w;
      vb.y -= dy / rect.height * vb.h;
      this.panStart.x = e.touches[0].clientX;
      this.panStart.y = e.touches[0].clientY;
      this._applyViewBox();
    } else if (e.touches.length === 2 && this._pinchStart) {
      e.preventDefault();
      var dist = this._getPinchDist(e);
      var factor = dist / this._pinchStart;
      var cx = (e.touches[0].clientX + e.touches[1].clientX) / 2;
      var cy = (e.touches[0].clientY + e.touches[1].clientY) / 2;
      this.zoomAtPoint(cx, cy, factor);
      this._pinchStart = dist;
    }
  },

  _getPinchDist: function(e) {
    var dx = e.touches[0].clientX - e.touches[1].clientX;
    var dy = e.touches[0].clientY - e.touches[1].clientY;
    return Math.sqrt(dx * dx + dy * dy);
  },

  toggleFullscreen: function() {
    if (this._fsOverlay) {
      this._exitFullscreen();
    } else {
      this._enterFullscreen();
    }
  },

  _enterFullscreen: function() {
    if (!this.svg) return;
    var overlay = document.createElement('div');
    overlay.className = 'piko-graph-fullscreen';
    var hint = document.createElement('span');
    hint.className = 'piko-graph-fullscreen-hint';
    hint.textContent = 'Press Esc to exit';
    overlay.appendChild(hint);

    var closeDiv = document.createElement('div');
    closeDiv.className = 'piko-graph-fullscreen-close piko-graph-controls';
    closeDiv.style.position = 'absolute';
    closeDiv.innerHTML =
      '<button type="button" title="Zoom in" data-action="zoomIn">+</button>' +
      '<button type="button" title="Zoom out" data-action="zoomOut">&minus;</button>' +
      '<button type="button" title="Reset" data-action="reset">&#x21BA;</button>' +
      '<button type="button" title="Close" data-action="close">&times;</button>';
    overlay.appendChild(closeDiv);

    var body = document.createElement('div');
    body.className = 'piko-graph-fullscreen-body';
    overlay.appendChild(body);

    this._svgParent = this.svg.parentNode;
    this._svgNextSibling = this.svg.nextSibling;
    body.appendChild(this.svg);
    this.svg.style.maxWidth = 'none';
    this.svg.style.maxHeight = '100%';
    this.svg.style.width = '100%';
    this.svg.style.height = '100%';

    var legend = this.container.querySelector('.piko-graph-legend');
    if (!legend) {
      var next = this.container.nextElementSibling;
      if (next && next.classList.contains('piko-graph-legend')) {
        legend = next;
      }
    }
    if (legend) {
      this._legendParent = legend.parentNode;
      this._legendNextSibling = legend.nextSibling;
      overlay.appendChild(legend);
    }

    document.body.appendChild(overlay);
    this._fsOverlay = overlay;

    var that = this;
    overlay.addEventListener('wheel', this._bound.wheel, {passive: false});
    body.addEventListener('mousedown', this._bound.mousedown);
    body.addEventListener('click', this._bound.click, true);
    body.addEventListener('touchstart', this._bound.touchstart, {passive: false});
    body.addEventListener('touchmove', this._bound.touchmove, {passive: false});
    body.addEventListener('touchend', this._bound.touchend);

    closeDiv.addEventListener('click', function(e) {
      var btn = e.target.closest('button');
      if (!btn) return;
      e.stopPropagation();
      var action = btn.getAttribute('data-action');
      if (action === 'zoomIn') that.zoomIn();
      else if (action === 'zoomOut') that.zoomOut();
      else if (action === 'reset') that.reset();
      else if (action === 'close') that._exitFullscreen();
    });

    this._bound.escKey = function(e) {
      if (e.key === 'Escape') that._exitFullscreen();
    };
    document.addEventListener('keydown', this._bound.escKey);

    this._controlsEl.style.display = 'none';
    if (this.onFullscreenChange) this.onFullscreenChange(true);
  },

  _exitFullscreen: function() {
    if (!this._fsOverlay) return;

    if (this._svgParent) {
      if (this._svgNextSibling) {
        this._svgParent.insertBefore(this.svg, this._svgNextSibling);
      } else {
        this._svgParent.appendChild(this.svg);
      }
    }
    if (this.baseViewBox) {
      this.svg.style.maxWidth = 'none';
      this.svg.style.maxHeight = 'none';
      this.svg.style.width = '100%';
      this.svg.style.height = '100%';
    }

    if (this._legendParent) {
      var legend = this._fsOverlay.querySelector('.piko-graph-legend');
      if (legend) {
        if (this._legendNextSibling) {
          this._legendParent.insertBefore(legend, this._legendNextSibling);
        } else {
          this._legendParent.appendChild(legend);
        }
      }
      this._legendParent = null;
      this._legendNextSibling = null;
    }

    document.removeEventListener('keydown', this._bound.escKey);
    this._fsOverlay.remove();
    this._fsOverlay = null;
    this._controlsEl.style.display = 'flex';
    if (this.onFullscreenChange) this.onFullscreenChange(false);
  },

  destroy: function() {
    this._exitFullscreen();
    this.container.removeEventListener('wheel', this._bound.wheel);
    this.container.removeEventListener('mousedown', this._bound.mousedown);
    window.removeEventListener('mousemove', this._bound.mousemove);
    window.removeEventListener('mouseup', this._bound.mouseup);
    this.container.removeEventListener('click', this._bound.click, true);
    this.container.removeEventListener('touchstart', this._bound.touchstart);
    this.container.removeEventListener('touchmove', this._bound.touchmove);
    this.container.removeEventListener('touchend', this._bound.touchend);
    if (this._controlsEl && this._controlsEl.parentNode) {
      this._controlsEl.remove();
    }
  }
};

export var PipelineGraphView = Backbone.View.extend({
  template: _.template($('#pipeline-graph-view').html()),
  initialize: function(options) {
    this.noLinks = options.noLinks||false;
    this.onSVGReady = options.onSVGReady||null;
    this.listenTo(this.model, "change", this.render);
  },
  render: function() {
    if (this.model.get("image")) {
      var that = this;
      window.Viz.instance().then(viz =>{
        let svg = viz.renderSVGElement(that.model.get("image"));
        var naturalWidth = parseFloat(svg.getAttribute("width"));
        var naturalHeight = parseFloat(svg.getAttribute("height"));
        if (naturalWidth && naturalHeight) {
          svg.setAttribute("viewBox", "0 0 " + naturalWidth + " " + naturalHeight);
        }
        svg.setAttribute("width", "100%");
        svg.removeAttribute("height");
        svg.style.maxWidth = (naturalWidth || 800) + "px";
        svg.style.maxHeight = "400px";
        svg.style.background = "transparent";
        svg.querySelectorAll('polygon, rect').forEach(function(el, i) {
          var f = (el.getAttribute('fill')||'').toLowerCase();
          if (i === 0 || f === 'white' || f === '#ffffff') {
            el.setAttribute('fill', 'transparent');
            el.setAttribute('stroke', 'transparent');
            return;
          }
          if (el.tagName === 'polygon' && f && f !== 'none' && f !== 'transparent') {
            var points = el.getAttribute('points');
            if (!points) return;
            var pts = points.trim().split(/\s+/).map(function(p) {
              var xy = p.split(','); return {x: parseFloat(xy[0]), y: parseFloat(xy[1])};
            });
            if (pts.length === 4 || pts.length === 5 && pts[0].x === pts[4].x && pts[0].y === pts[4].y) {
              var xs = pts.map(function(p){return p.x;}), ys = pts.map(function(p){return p.y;});
              var minX = Math.min.apply(null,xs), maxX = Math.max.apply(null,xs);
              var minY = Math.min.apply(null,ys), maxY = Math.max.apply(null,ys);
              var rect = document.createElementNS('http://www.w3.org/2000/svg','rect');
              rect.setAttribute('x', minX);
              rect.setAttribute('y', minY);
              rect.setAttribute('width', maxX - minX);
              rect.setAttribute('height', maxY - minY);
              rect.setAttribute('rx', '4');
              rect.setAttribute('ry', '4');
              rect.setAttribute('fill', el.getAttribute('fill'));
              rect.setAttribute('stroke', el.getAttribute('stroke')||'none');
              el.parentNode.replaceChild(rect, el);
            }
          }
        });
        svg.querySelectorAll('text').forEach(function(t) {
          t.style.fontFamily = "'Plus Jakarta Sans', system-ui, sans-serif";
        });
        svg.querySelectorAll('g.node').forEach(function(g) {
          g.style.cursor = 'pointer';
        });
        that.$el.html(that.template());
        that.$el.find("#pipeline-graph").html(svg);
        if (that.noLinks) {
          that.$el.find("a").each(function() {
            $(this).attr("xlink:href",null);
          });
        }
        if (that.onSVGReady) that.onSVGReady(svg);
      });
    }
    return this;
  },
});

// HCL stream language for CodeMirror
export var hclLanguage = function() {
  var CM = window.PikoCM;
  var keywords = 'job resource resource_type runner_type secret_type service_type notification_type notification variable get put notify task service plan params start stop ready_check secret on_success on_failure on_cancel ensure concurrency';
  var keywordSet = {};
  keywords.split(' ').forEach(function(k){ keywordSet[k] = true; });
  var atoms = {true:true, false:true, null:true};
  return CM.StreamLanguage.define({
    startState: function() { return {inString: false, inComment: false}; },
    token: function(stream, state) {
      if (state.inComment) {
        var end = stream.match(/.*?\*\//);
        if (end) { state.inComment = false; }
        else { stream.skipToEnd(); }
        return 'comment';
      }
      if (state.inString) {
        while (!stream.eol()) {
          var ch = stream.next();
          if (ch === '\\') { stream.next(); }
          else if (ch === '"') { state.inString = false; break; }
        }
        return 'string';
      }
      if (stream.match(/\/\*/)) { state.inComment = true; return 'comment'; }
      if (stream.match(/\/\//) || stream.match(/#/)) { stream.skipToEnd(); return 'comment'; }
      if (stream.match(/"/)) { state.inString = true; return 'string'; }
      if (stream.match(/\d+(\.\d+)?/)) { return 'number'; }
      if (stream.match(/[{}\[\]()]/)) { return 'bracket'; }
      if (stream.match(/[=:]/)) { return 'operator'; }
      if (stream.match(/[a-zA-Z_][a-zA-Z0-9_]*/)) {
        var w = stream.current();
        if (keywordSet[w]) return 'keyword';
        if (atoms[w]) return 'atom';
        return 'variableName';
      }
      stream.next();
      return null;
    }
  });
};

// CodeMirror themes using PikoCI CSS variables
export var cmLightTheme = function() {
  var CM = window.PikoCM;
  return CM.EditorView.theme({
    '&': { backgroundColor: 'var(--bg-surface)', color: 'var(--text-primary)', fontFamily: 'var(--font-mono)', fontSize: '0.85rem' },
    '.cm-content': { caretColor: 'var(--text-primary)' },
    '.cm-gutters': { backgroundColor: 'var(--bg-muted)', color: 'var(--text-muted)', borderRight: '1px solid var(--border)' },
    '.cm-activeLineGutter': { backgroundColor: 'var(--primary-light)' },
    '.cm-activeLine': { backgroundColor: 'var(--primary-light)' },
    '.cm-selectionBackground': { backgroundColor: 'rgba(41,173,255,0.2) !important' },
    '.cm-cursor': { borderLeftColor: 'var(--text-primary)' },
    '.cm-matchingBracket': { backgroundColor: 'rgba(41,173,255,0.3)', outline: 'none' },
  }, {dark: false});
};

export var cmDarkTheme = function() {
  var CM = window.PikoCM;
  return CM.EditorView.theme({
    '&': { backgroundColor: 'var(--bg-surface)', color: 'var(--text-primary)', fontFamily: 'var(--font-mono)', fontSize: '0.85rem' },
    '.cm-content': { caretColor: 'var(--text-primary)' },
    '.cm-gutters': { backgroundColor: 'var(--bg-muted)', color: 'var(--text-muted)', borderRight: '1px solid var(--border)' },
    '.cm-activeLineGutter': { backgroundColor: 'rgba(41,173,255,0.15)' },
    '.cm-activeLine': { backgroundColor: 'rgba(41,173,255,0.1)' },
    '.cm-selectionBackground': { backgroundColor: 'rgba(41,173,255,0.3) !important' },
    '.cm-cursor': { borderLeftColor: 'var(--text-primary)' },
    '.cm-matchingBracket': { backgroundColor: 'rgba(41,173,255,0.4)', outline: 'none' },
  }, {dark: true});
};

export var cmHighlightLight = function() {
  var t = window.PikoCM.tags;
  return window.PikoCM.HighlightStyle.define([
    {tag: t.keyword, color: '#7B2FBE'},
    {tag: t.atom, color: '#AB5236'},
    {tag: t.string, color: '#00A83A'},
    {tag: t.comment, color: '#83769C', fontStyle: 'italic'},
    {tag: t.number, color: '#FF004D'},
    {tag: t.bracket, color: '#5F574F'},
    {tag: t.operator, color: '#5F574F'},
    {tag: t.variableName, color: '#1D2B53'},
  ]);
};

export var cmHighlightDark = function() {
  var t = window.PikoCM.tags;
  return window.PikoCM.HighlightStyle.define([
    {tag: t.keyword, color: '#FF77A8'},
    {tag: t.atom, color: '#FFA300'},
    {tag: t.string, color: '#00E756'},
    {tag: t.comment, color: '#83769C', fontStyle: 'italic'},
    {tag: t.number, color: '#FF004D'},
    {tag: t.bracket, color: '#C2C3C7'},
    {tag: t.operator, color: '#C2C3C7'},
    {tag: t.variableName, color: '#FFF1E8'},
  ]);
};

export var PipelinesNewView = Backbone.View.extend({
  template: _.template($('#pipelines-new-view').html()),
  events: {
    'click #create': 'clickCreate',
    'submit form':   'clickCreate',
    'change textarea#vars': 'changePipeline',
    'click #graph g.node': 'clickGraphNode',
    'click #graph-fullscreen g.node': 'clickGraphNode',
    'click #tab-hcl': 'showHclTab',
    'click #tab-vars': 'showVarsTab',
    'click #docs-btn': 'toggleDocs',
    'click #blocks-btn': 'toggleBlocks',
    'click #graph-btn': 'toggleGraphPanel',
    'click #graph-bottom-close': 'toggleGraphPanel',
    'click #fullscreen-btn': 'toggleFullscreen',
    'click #graph-strip-header': 'toggleGraphStrip',
    'click .piko-blocks-item': 'clickBlock',
  },
  render: function () {
    var data = this.model.toJSON();
    if (data.raw) {
      data.raw = atob(data.raw);
    }
    this.$el.html(this.template(data));
    this.image = new PipelineImage();
    var that = this;
    this.graphView = new PipelineGraphView({model: this.image, noLinks: true, onSVGReady: function(svg) {
      var container = that.$el.find("#graph")[0];
      if (!container) return;
      if (!that.graphZoom) {
        that.graphZoom = new PikoGraphZoom(container);
      }
      that.graphZoom.attachSVG(svg);
    }});
    this.$el.find("#graph").html(this.graphView.render().el);
    this.graphViewFs = new PipelineGraphView({model: this.image, noLinks: true, onSVGReady: function(svg) {
      var container = that.$el.find("#graph-fullscreen")[0];
      if (!container) return;
      if (!that.graphZoomFs) {
        that.graphZoomFs = new PikoGraphZoom(container);
      }
      that.graphZoomFs.attachSVG(svg);
    }});
    this.$el.find("#graph-fullscreen").html(this.graphViewFs.render().el);
    this.initEditor(data.raw || '');
    this.$el.find('#graph-strip-header').addClass('open');
    if (data.raw) {
      this.changePipeline();
    }
    this.updateBlocksPanel();
    return this;
  },
  initEditor: function(initialValue) {
    var CM = window.PikoCM;
    var that = this;
    this._themeCompartment = new CM.Compartment();
    this._highlightCompartment = new CM.Compartment();
    var isDark = document.documentElement.getAttribute('data-theme') === 'dark';
    var theme = isDark ? cmDarkTheme() : cmLightTheme();
    var highlight = isDark ? cmHighlightDark() : cmHighlightLight();
    this.editor = new CM.EditorView({
      state: CM.EditorState.create({
        doc: initialValue,
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
          this._themeCompartment.of(theme),
          this._highlightCompartment.of(CM.syntaxHighlighting(highlight)),
          hclLanguage(),
          CM.keymap.of([
            ...CM.closeBracketsKeymap,
            ...CM.defaultKeymap,
            ...CM.searchKeymap,
            ...CM.historyKeymap,
            CM.indentWithTab,
          ]),
          CM.EditorView.updateListener.of(function(update) {
            if (update.docChanged) {
              that.$el.find('#pipeline').val(that.editor.state.doc.toString());
              clearTimeout(that._previewTimer);
              that._previewTimer = setTimeout(function() {
                that.changePipeline();
                that.updateBlocksPanel();
              }, 500);
            }
          }),
        ],
      }),
      parent: this.$el.find('#pipeline-editor')[0],
    });
    window._pikoEditor = this.editor;
    this._themeObserver = new MutationObserver(function() {
      var dark = document.documentElement.getAttribute('data-theme') === 'dark';
      that.editor.dispatch({
        effects: [
          that._themeCompartment.reconfigure(dark ? cmDarkTheme() : cmLightTheme()),
          that._highlightCompartment.reconfigure(CM.syntaxHighlighting(dark ? cmHighlightDark() : cmHighlightLight())),
        ]
      });
    });
    this._themeObserver.observe(document.documentElement, {attributes: true, attributeFilter: ['data-theme']});
    this._escHandler = function(e) {
      if (e.key === 'Escape' && that.$el.find('#editor-card').hasClass('fullscreen')) {
        that.toggleFullscreen(e);
      }
    };
    $(document).on('keydown', this._escHandler);
    this._docClickHandler = function(e) {
      if (!$(e.target).closest('.piko-docs-dropdown').length) {
        that.$el.find('#docs-menu').removeClass('open');
      }
    };
    $(document).on('click', this._docClickHandler);
  },
  showHclTab: function() {
    this.$el.find('#tab-hcl').addClass('active');
    this.$el.find('#tab-vars').removeClass('active');
    this.$el.find('#code-area').show();
    this.$el.find('#vars-area').removeClass('visible');
    this.$el.find('#blocks-panel').show();
  },
  showVarsTab: function() {
    this.$el.find('#tab-vars').addClass('active');
    this.$el.find('#tab-hcl').removeClass('active');
    this.$el.find('#code-area').hide();
    this.$el.find('#vars-area').addClass('visible');
    this.$el.find('#blocks-panel').hide();
  },
  toggleDocs: function(e) {
    e.stopPropagation();
    this.$el.find('#docs-menu').toggleClass('open');
  },
  toggleBlocks: function() {
    this.$el.find('#blocks-panel').toggleClass('collapsed');
    this.$el.find('#blocks-btn').toggleClass('active');
  },
  updateBlocksPanel: function() {
    if (!this.editor) return;
    var doc = this.editor.state.doc.toString();
    var panel = this.$el.find('#blocks-panel');
    var html = '';
    var errorLines = this._errorLines || {};
    blockTypes.forEach(function(bt) {
      var twoLabel = bt.type === 'resource' || bt.type === 'notification';
      var re = twoLabel
        ? new RegExp(bt.type + '\\s+"([^"]+)"\\s+"([^"]+)"', 'g')
        : new RegExp(bt.type + '\\s+"([^"]+)"', 'g');
      var matches = [];
      var m;
      while ((m = re.exec(doc)) !== null) {
        var displayName = twoLabel ? m[1] + '.' + m[2] : m[1];
        matches.push({name: displayName, pos: m.index});
      }
      if (matches.length === 0) return;
      html += '<div class="piko-blocks-section">';
      html += '<div class="piko-blocks-section-title">' + bt.label + '</div>';
      matches.forEach(function(match) {
        var hasErr = errorLines[bt.type + ':"' + match.name + '"'];
        html += '<div class="piko-blocks-item' + (hasErr ? ' has-error' : '') + '" data-pos="' + match.pos + '">';
        html += '<span class="piko-block-icon ' + bt.icon + '">' + bt.letter + '</span> ';
        html += _.escape(match.name);
        if (hasErr) html += '<span class="piko-error-dot"></span>';
        html += '</div>';
      });
      html += '</div>';
    });
    panel.html(html || '<div style="padding:12px;color:var(--text-muted);font-size:0.78rem">No blocks found</div>');
  },
  clickBlock: function(e) {
    var pos = parseInt($(e.currentTarget).attr('data-pos'), 10);
    if (isNaN(pos) || !this.editor) return;
    this.$el.find('.piko-blocks-item').removeClass('active');
    $(e.currentTarget).addClass('active');
    var CM = window.PikoCM;
    this.editor.dispatch({
      selection: {anchor: pos},
      effects: CM.EditorView.scrollIntoView(pos, {y: 'start'}),
    });
    this.editor.focus();
  },
  toggleGraphPanel: function() {
    this.$el.find('#graph-bottom-panel').toggleClass('visible');
    this.$el.find('#graph-btn').toggleClass('active');
  },
  toggleFullscreen: function(e) {
    if (e && e.preventDefault) e.preventDefault();
    var card = this.$el.find('#editor-card');
    card.toggleClass('fullscreen');
    var isFs = card.hasClass('fullscreen');
    var icon = this.$el.find('#fullscreen-btn i');
    icon.attr('class', isFs ? 'bi bi-fullscreen-exit' : 'bi bi-arrows-fullscreen');
    $('body').toggleClass('piko-fullscreen', isFs);
  },
  toggleGraphStrip: function() {
    this.$el.find('#graph-strip-header').toggleClass('open');
  },
  clickGraphNode: function(event) {
    var g = $(event.currentTarget);
    var textEl = g.find('text').last();
    if (!textEl.length || !this.editor) return;
    var name = textEl.text().trim();
    if (!name) return;
    var doc = this.editor.state.doc.toString();
    var dotIdx = name.indexOf('.');
    var re;
    if (dotIdx !== -1) {
      var rType = name.substring(0, dotIdx).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      var rLabel = name.substring(dotIdx + 1).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      re = new RegExp('(?:resource|notification)\\s+"' + rType + '"\\s+"' + rLabel + '"');
    } else {
      re = new RegExp('(?:job|resource|resource_type|runner_type|secret_type|service_type|notification_type|notification)\\s+"' + name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '"');
    }
    var match = re.exec(doc);
    if (match) {
      var CM = window.PikoCM;
      this.editor.dispatch({
        selection: {anchor: match.index, head: match.index + match[0].length},
        effects: CM.EditorView.scrollIntoView(match.index, {y: 'start'}),
      });
      this.editor.focus();
    }
  },
  showEditorErrors: function(errorStr) {
    if (!this.editor) return;
    var CM = window.PikoCM;
    var diags = parseHCLErrors(errorStr);
    var doc = this.editor.state.doc;
    var docText = doc.toString();
    var diagnostics = [];
    var errorLines = {};
    for (var i = 0; i < diags.length; i++) {
      var d = diags[i];
      if (d.blockType) {
        errorLines[d.blockType + ':"' + d.blockName + '"'] = true;
        var pos = this._findBlockAttribute(docText, d.blockType, d.blockName, d.attribute);
        if (pos) {
          diagnostics.push({from: pos.from, to: pos.to, severity: 'error', message: d.message});
        } else {
          var esc = d.blockName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
          var headerRe = new RegExp(d.blockType + '\\s+"' + esc + '"');
          var hm = headerRe.exec(docText);
          if (hm) {
            diagnostics.push({from: hm.index, to: hm.index + hm[0].length, severity: 'error', message: d.message});
          }
        }
      } else {
        if (d.line < 1 || d.line > doc.lines) continue;
        var lineOffset = doc.line(d.line).from;
        var blockRe = /(?:job|resource|resource_type|runner_type|secret_type|service_type|notification_type|notification|variable)\s+"([^"]+)"/g;
        var bm, lastBlock = null;
        while ((bm = blockRe.exec(docText)) !== null) {
          if (bm.index <= lineOffset) lastBlock = bm;
          else break;
        }
        if (lastBlock) {
          var btMatch = lastBlock[0].match(/^(\w+)/);
          if (btMatch) errorLines[btMatch[1] + ':"' + lastBlock[1] + '"'] = true;
        }
        var line = doc.line(d.line);
        var from = line.from + Math.max(0, d.colStart - 1);
        var to = line.from + Math.min(line.length, d.colEnd - 1);
        if (from > doc.length) from = doc.length;
        if (to > doc.length) to = doc.length;
        if (from >= to) to = from + 1;
        if (to > doc.length) to = doc.length;
        diagnostics.push({from: from, to: to, severity: 'error', message: d.message});
      }
    }
    this._errorLines = errorLines;
    this.editor.dispatch(CM.setDiagnostics(this.editor.state, diagnostics));
    this.updateBlocksPanel();
    var hasErrors = Object.keys(errorLines).length > 0;
    this.$el.find('#blocks-btn .piko-error-badge').toggleClass('visible', hasErrors);
  },
  _findBlockAttribute: function(docText, blockType, blockName, attribute) {
    var esc = blockName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    var blockRe = new RegExp(blockType + '\\s+"' + esc + '"\\s*\\{');
    var bm = blockRe.exec(docText);
    if (!bm) return null;
    var start = bm.index + bm[0].length;
    var depth = 1;
    var blockEnd = docText.length;
    for (var j = start; j < docText.length; j++) {
      if (docText[j] === '{') depth++;
      else if (docText[j] === '}') { depth--; if (depth === 0) { blockEnd = j; break; } }
    }
    var blockContent = docText.substring(start, blockEnd);
    var attrRe = new RegExp('(^|\\n)(\\s*)' + attribute + '\\s*=\\s*("(?:[^"\\\\]|\\\\.)*"|[^\\n]+)');
    var am = attrRe.exec(blockContent);
    if (!am) return null;
    var attrStart = start + am.index + am[1].length + am[2].length;
    var attrEnd = attrStart + am[0].length - am[1].length - am[2].length;
    return {from: attrStart, to: attrEnd};
  },
  clearEditorErrors: function() {
    if (!this.editor) return;
    var CM = window.PikoCM;
    this._errorLines = {};
    this.editor.dispatch(CM.setDiagnostics(this.editor.state, []));
    this.updateBlocksPanel();
    this.$el.find('#blocks-btn .piko-error-badge').removeClass('visible');
  },
  clickCreate: function(event) {
    event.preventDefault();
    var name = this.$el.find("#name").get(0).value;
    var pp = this.editor ? this.editor.state.doc.toString() : this.$el.find("#pipeline").get(0).value;
    var rvars = this.$el.find("#vars").get(0).value||"{}";
    var isPublic = this.$el.find("#public").is(":checked");
    try{
      var vars = JSON.parse(rvars);
    } catch (error){
      window.app.apiNotice.set({error: "Error parsing Vars: "+error});
      return;
    }
    var data = [];
    for (var i = 0; i < pp.length; i++){
      data.push(pp.charCodeAt(i));
    }
    if (this.model.get("id")){
      this.collection.create({name: name, canonical: this.model.get("canonical"), config: data, vars: vars, public: isPublic}, {
        wait: true,
        url: this.model.url(),
        success: function(m) {
          window.app.router.navigate(m.url(), { trigger: true });
        },
      });
    } else {
      this.collection.create({name: name, config: data, vars: vars}, {
        wait: true,
        type: "POST",
        url: this.collection.url(),
        success: function(m) {
          window.app.router.navigate(m.url(), { trigger: true });
        },
      });
    }
  },
  changePipeline: function() {
    var pp = this.editor ? this.editor.state.doc.toString() : this.$el.find("#pipeline").get(0).value;
    var rvars = this.$el.find("#vars").get(0).value||"{}";
    var that = this;
    try{
      var vars = JSON.parse(rvars);
    } catch (error){
      window.app.apiNotice.set({error: "Error parsing Vars: "+error});
      return;
    }
    var data = [];
    for (var i = 0; i < pp.length; i++){
      data.push(pp.charCodeAt(i));
    }
    this.image.save({config: data, vars: vars}, {
      url: this.collection.url()+"/image.dot",
      success: function(model, response) {
        that.clearEditorErrors();
      },
      error: function(model, xhr) {
        try {
          var resp = JSON.parse(xhr.responseText);
          if (resp && resp.error) {
            that.showEditorErrors(resp.error);
          }
        } catch(e) {
          that.showEditorErrors(xhr.responseText || 'Unknown error');
        }
      },
    });
  },
  remove: function() {
    clearTimeout(this._previewTimer);
    if (this._themeObserver) { this._themeObserver.disconnect(); }
    if (this.editor) { this.editor.destroy(); }
    if (this._escHandler) { $(document).off('keydown', this._escHandler); }
    if (this._docClickHandler) { $(document).off('click', this._docClickHandler); }
    if (this.graphZoom) { this.graphZoom.destroy(); }
    if (this.graphZoomFs) { this.graphZoomFs.destroy(); }
    $('body').removeClass('piko-fullscreen');
    Backbone.View.prototype.remove.call(this);
  },
});
