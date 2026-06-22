'use strict';

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

  _onPanEnd: function(_e) {
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
