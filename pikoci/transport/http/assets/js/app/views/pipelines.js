'use strict';

import { session, Builds, Jobs } from '../collections.js';
import { PipelineImage, Job, Pipeline } from '../models.js';
import { addSessionFunctions, clickLink, fetchInterval, pikoTimeAgo, withLoading } from '../namespace.js';
import { JobBuildsView } from './jobs.js';
import { PipelineGraphView, PikoGraphZoom } from './editor.js';

// Extract a human-readable ref string from a version metadata map.
function versionRef(v) {
  if (!v) return '';
  if (typeof v === 'string') return v;
  if (v.ref) return v.ref;
  if (v.digest) return v.digest;
  if (v.tag) return v.tag;
  if (typeof v.version === 'string') return v.version;
  for (var key in v) {
    if (v.hasOwnProperty(key)) {
      return key + ': ' + v[key];
    }
  }
  return '';
}

export var PipelinesView = Backbone.View.extend({
  template: _.template($('#pipelines-view').html()),
  initialize: function() {
    this.cardViews = [];
    this.listenTo(this.collection, "add", this.addPipeline);

    this.collection.fetch();
  },
  events: {
    'click #pipelines-new': clickLink,
    'click #live-status-toggle': 'toggleLive',
  },
  addPipeline: function(pp) {
    var liveEnabled = localStorage.getItem("liveStatusEnabled") === "true";
    var view = new PipelinesCardView({model: pp, liveEnabled: liveEnabled});
    this.cardViews.push(view);
    this.$el.find('#pipelines').append(view.render().el);
  },
  render: function () {
    for (var i = 0; i < this.cardViews.length; i++) {
      this.cardViews[i].remove();
    }
    this.cardViews = [];

    this.$el.html(this.template(addSessionFunctions({team: this.collection.team.toJSON()})));

    if (localStorage.getItem("liveStatusEnabled") === "true") {
      this.$el.find('#live-status-toggle').addClass('on');
    }

    var that = this;
    this.collection.each(function(m) {
      that.addPipeline(m);
    });
    return this;
  },
  toggleLive: function(event) {
    event.preventDefault();
    var isOn = localStorage.getItem("liveStatusEnabled") === "true";
    var newState = !isOn;
    localStorage.setItem("liveStatusEnabled", newState ? "true" : "false");
    this.$el.find('#live-status-toggle').toggleClass('on', newState);
    for (var i = 0; i < this.cardViews.length; i++) {
      if (newState) {
        this.cardViews[i].startPolling();
      } else {
        this.cardViews[i].stopPolling();
      }
    }
  },
  remove: function() {
    for (var i = 0; i < this.cardViews.length; i++) {
      this.cardViews[i].remove();
    }
    this.cardViews = [];
    Backbone.View.prototype.remove.call(this);
  },
});

var PipelinesCardView = Backbone.View.extend({
  template: _.template($('#pipelines-card-view').html()),
  attributes: {
    class: "col",
  },
  events: {
    'click': 'clickCard',
  },
  initialize: function(options) {
    this.intervalID = null;
    this.image = new PipelineImage(null, {pipeline: this.model});
    this.image.fetch();
    this.listenTo(this.image, "change", this.updateStatus);
    if (options && options.liveEnabled) {
      this.startPolling();
    }
  },
  startPolling: function() {
    if (this.intervalID) return;
    var that = this;
    this.intervalID = window.setInterval(function() {
      that.image.fetch({isInterval: true});
    }, fetchInterval);
  },
  stopPolling: function() {
    if (this.intervalID) {
      clearInterval(this.intervalID);
      this.intervalID = null;
    }
  },
  remove: function() {
    this.stopPolling();
    Backbone.View.prototype.remove.call(this);
  },
  render: function () {
    this.$el.html(this.template(this.model.toJSON()));
    this.$el.find(".card-img-top").html(new PipelineGraphView({model: this.image, noLinks: true}).render().el);
    return this;
  },
  updateStatus: function() {
    var that = this;
    var attempts = 0;
    function checkSvg() {
      var svg = that.$el.find("svg")[0];
      if (!svg) {
        if (attempts++ < 20) setTimeout(checkSvg, 200);
        return;
      }
      var hasFailed = false, hasRunning = false, hasSucceeded = false;
      svg.querySelectorAll('polygon, rect, ellipse, path').forEach(function(el) {
        var fill = (el.getAttribute("fill")||"").toLowerCase();
        if (fill === "#ff004d") hasFailed = true;
        if (fill === "#ffa300") hasRunning = true;
        if (fill === "#00a83a") hasSucceeded = true;
      });
      var html = '';
      var timeAgo = '';
      var lastBuildAt = that.model.get('last_build_at');
      if (lastBuildAt) {
        timeAgo = ' · ' + pikoTimeAgo(lastBuildAt);
      }
      if (hasFailed) {
        html = '<span class="piko-card-status-dot" style="background:#FF004D;"></span> Last build failed' + timeAgo;
      } else if (hasRunning) {
        html = '<span class="piko-card-status-dot" style="background:#FFA300;"></span> Running' + timeAgo;
      } else if (hasSucceeded) {
        html = '<span class="piko-card-status-dot" style="background:#00A83A;"></span> Last build passed' + timeAgo;
      } else {
        html = '<span style="color:var(--text-muted);">No builds</span>';
      }
      that.$el.find(".piko-card-status").html(html);
    }
    setTimeout(checkSvg, 300);
  },
  clickCard: function(event) {
    event.preventDefault();
    window.app.router.navigate(this.model.url(), { trigger: true });
  },
});

var PipelineResourcesPanelView = Backbone.View.extend({
  template: _.template($('#pipeline-resources-panel-view').html()),
  initialize: function(options) {
    this.collection = options.collection;
    this.pipeline = options.pipeline;
    this._expandedResources = {};
    this.listenTo(this.collection, 'sync', this._onSync);
    this._polling = false;
  },
  startPolling: function() {
    if (this._polling) return;
    this._polling = true;
    this.collection.fetch();
    var that = this;
    this.intervalID = window.setInterval(function() {
      that.collection.fetch();
    }, fetchInterval);
  },
  stopPolling: function() {
    if (!this._polling) return;
    this._polling = false;
    clearInterval(this.intervalID);
  },
  events: {
    'click #close-resources-panel': 'closePanel',
    'click .check-resource-now': 'checkNow',
    'click .piko-resource-card-name': clickLink,
    'click .piko-resource-expand-toggle': 'toggleVersionsList',
    'click .piko-panel-track-btn': 'trackVersionFromPanel',
    'click .piko-panel-trigger-btn': 'triggerVersionFromPanel',
    'click .piko-panel-pin-btn': 'pinVersionFromPanel',
  },
  _onSync: function() {
    // If any version lists are expanded, do targeted updates instead of full re-render
    if (Object.keys(this._expandedResources).length > 0) {
      this._updateResourceCards();
      return;
    }
    this.render();
  },
  _updateResourceCards: function() {
    var that = this;
    this.collection.each(function(r) {
      var rData = r.toJSON();
      var card = that.$('.piko-resource-card[data-canonical="' + rData.canonical + '"]');
      if (card.length === 0) return;
      // Update status dot
      var dot = card.find('.piko-version-status-dot');
      if (rData.latest_version && rData.latest_version.status) {
        if (dot.length) {
          dot.css('background', 'var(--status-' + rData.latest_version.status + ')');
        }
      }
      // Update meta
      var meta = card.find('.piko-resource-card-meta');
      var metaHtml = '';
      if (rData.check_interval) {
        metaHtml += '<span>' + _.escape(rData.check_interval) + '</span>';
      }
      if (rData.last_check && !rData.last_check.startsWith('0001-01-01')) {
        metaHtml += '<span>Checked: ' + pikoTimeAgo(rData.last_check) + '</span>';
      }
      meta.html(metaHtml);
    });
  },
  render: function() {
    var teamCanonical = this.pipeline.collection ? this.pipeline.collection.team.get('canonical') : '';
    var pipelineCanonical = this.pipeline.get('canonical');
    var basePath = '/teams/' + teamCanonical + '/pipelines/' + pipelineCanonical;
    var sf = addSessionFunctions({});
    this.$el.html(this.template({
      resources: this.collection.toJSON(),
      isMember: sf.isMember(teamCanonical),
      pikoTimeAgo: pikoTimeAgo,
      resourceUrl: function(canonical) {
        return basePath + '/resources/' + canonical + '/versions';
      },
    }));
    return this;
  },
  closePanel: function(event) {
    event.preventDefault();
    event.stopPropagation();
    this.$el.removeClass('open');
    this.stopPolling();
  },
  checkNow: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var canonical = $(event.currentTarget).data('canonical');
    var teamCanonical = this.pipeline.collection ? this.pipeline.collection.team.get('canonical') : '';
    var pipelineCanonical = this.pipeline.get('canonical');
    var url = '/teams/' + teamCanonical + '/pipelines/' + pipelineCanonical + '/resources/' + canonical + '/trigger';
    var that = this;
    $.ajax({
      url: url, type: 'POST', contentType: 'application/json',
      headers: { 'Authorization': 'Bearer ' + session.get('jwt') },
      success: function() {
        window.app.apiNotice.setSuccess('Resource check triggered');
        that.collection.fetch();
      },
      error: function() {
        window.app.apiNotice.set({error: 'Failed to trigger resource check'});
      },
    });
  },
  toggleVersionsList: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var canonical = $(event.currentTarget).data('canonical');
    var container = this.$('.piko-resource-panel-versions[data-canonical="' + canonical + '"]');
    if (container.is(':visible')) {
      container.hide();
      delete this._expandedResources[canonical];
      $(event.currentTarget).find('i').removeClass('bi-chevron-up').addClass('bi-chevron-down');
      return;
    }
    $(event.currentTarget).find('i').removeClass('bi-chevron-down').addClass('bi-chevron-up');
    container.show();
    this._expandedResources[canonical] = true;
    this._fetchPanelVersions(canonical, container);
  },
  _fetchPanelVersions: function(canonical, container) {
    var teamCanonical = this.pipeline.collection ? this.pipeline.collection.team.get('canonical') : '';
    var pipelineCanonical = this.pipeline.get('canonical');
    var url = '/teams/' + teamCanonical + '/pipelines/' + pipelineCanonical + '/resources/' + canonical + '/versions?limit=5';
    var resModel = this.collection.findWhere({canonical: canonical});
    var pinnedVersionID = resModel ? resModel.get('pinned_version_id') : null;
    var trackedVersionID = (this.parentShowView && this.parentShowView.trackedVersion) ? this.parentShowView.trackedVersion.versionID : null;
    $.ajax({
      url: url, type: 'GET', contentType: 'application/json',
      headers: session.isEmpty() ? {} : { 'Authorization': 'Bearer ' + session.get('jwt') },
      success: function(resp) {
        if (!resp || !resp.data) return;
        var html = '';
        for (var i = 0; i < resp.data.length; i++) {
          var v = resp.data[i];
          var ref = v.version ? versionRef(v.version) : '';
          var status = v.status || '';
          var isPinned = pinnedVersionID === v.id;
          var isTracked = trackedVersionID === v.id;
          html += '<div class="piko-resource-panel-version-item' + (isTracked ? ' tracked' : '') + '">';
          html += '<span class="piko-vsel-dot piko-status-dot-' + status + '"></span>';
          html += '<span class="piko-vsel-ref">' + _.escape(ref) + '</span>';
          if (isTracked) {
            html += '<span class="piko-panel-tracking-badge"><i class="bi bi-signpost-2"></i></span>';
          } else {
            html += '<button class="piko-panel-track-btn" data-canonical="' + _.escape(canonical) + '" data-version-id="' + v.id + '" data-version-ref="' + _.escape(ref) + '" title="Track"><i class="bi bi-signpost-2"></i></button>';
          }
          html += '<button class="piko-panel-trigger-btn" data-canonical="' + _.escape(canonical) + '" data-version-id="' + v.id + '" title="Trigger"><i class="bi bi-play-fill"></i></button>';
          html += '<button class="piko-panel-pin-btn ' + (isPinned ? 'pinned' : '') + '" data-canonical="' + _.escape(canonical) + '" data-version-id="' + v.id + '" title="' + (isPinned ? 'Unpin' : 'Pin') + '"><i class="bi ' + (isPinned ? 'bi-pin-fill' : 'bi-pin') + '"></i></button>';
          html += '</div>';
        }
        container.html(html);
      },
    });
  },
  trackVersionFromPanel: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var btn = $(event.currentTarget);
    var canonical = btn.data('canonical');
    var versionID = parseInt(btn.data('version-id'), 10);
    var ref = btn.data('version-ref') || '';
    if (typeof ref === 'object') ref = versionRef(ref);
    if (this.parentShowView && this.parentShowView.trackVersion) {
      this.parentShowView.trackVersion(canonical, versionID, ref);
    }
  },
  triggerVersionFromPanel: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var btn = $(event.currentTarget);
    var canonical = btn.data('canonical');
    var versionID = parseInt(btn.data('version-id'), 10);
    var tc = this.pipeline.collection ? this.pipeline.collection.team.get('canonical') : '';
    var pc = this.pipeline.get('canonical');
    var url = '/teams/' + tc + '/pipelines/' + pc + '/resources/' + canonical + '/versions/' + versionID + '/trigger';
    withLoading(btn, function() {
      return $.ajax({
        url: url, type: 'POST', contentType: 'application/json',
        headers: { 'Authorization': 'Bearer ' + session.get('jwt') },
        success: function() {
          window.app.apiNotice.setSuccess('Triggered downstream jobs with version #' + versionID);
        },
      });
    });
  },
  pinVersionFromPanel: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var btn = $(event.currentTarget);
    var canonical = btn.data('canonical');
    var versionID = parseInt(btn.data('version-id'), 10);
    var tc = this.pipeline.collection ? this.pipeline.collection.team.get('canonical') : '';
    var pc = this.pipeline.get('canonical');
    var isPinned = btn.hasClass('pinned');
    var that = this;
    var url;
    if (isPinned) {
      url = '/teams/' + tc + '/pipelines/' + pc + '/resources/' + canonical + '/unpin';
      withLoading(btn, function() {
        return $.ajax({
          url: url, type: 'POST', contentType: 'application/json',
          headers: { 'Authorization': 'Bearer ' + session.get('jwt') },
          success: function() {
            window.app.apiNotice.setSuccess('Resource unpinned');
            that.collection.fetch();
          },
        });
      });
    } else {
      url = '/teams/' + tc + '/pipelines/' + pc + '/resources/' + canonical + '/pin';
      withLoading(btn, function() {
        return $.ajax({
          url: url, type: 'POST', contentType: 'application/json',
          data: JSON.stringify({version_id: versionID}),
          headers: { 'Authorization': 'Bearer ' + session.get('jwt') },
          success: function() {
            window.app.apiNotice.setSuccess('Resource pinned to version #' + versionID);
            that.collection.fetch();
          },
        });
      });
    }
  },
  refreshExpandedVersions: function() {
    var that = this;
    _.each(this._expandedResources, function(val, canonical) {
      var container = that.$('.piko-resource-panel-versions[data-canonical="' + canonical + '"]');
      if (container.length && container.is(':visible')) {
        that._fetchPanelVersions(canonical, container);
      }
    });
  },
  remove: function() {
    this.stopPolling();
    Backbone.View.prototype.remove.call(this);
  },
});

export var PipelineShowView = Backbone.View.extend({
  template: _.template($('#pipeline-show-view').html()),
  initialize: function(options) {
    this.image = options.image;
    this.image.hideIntermediates = localStorage.getItem("piko-hide-intermediates") === "1";
    this.image.groupParallel = localStorage.getItem("piko-group-parallel") === "1";

    this.listView = null;
    this.trackedVersion = null;
    this.versionPathData = null;
    var savedView = localStorage.getItem("piko-pipeline-view") || "graph";
    this.currentView = savedView === "pipeline" ? "graph" : savedView;

    // Check for tracked version: first from router property (in-app navigation),
    // then from URL query param (page reload / direct link)
    var urlVersionID = window.app.router._trackedVersionID || null;
    delete window.app.router._trackedVersionID;
    if (!urlVersionID) {
      var urlParams = new URLSearchParams(window.location.search);
      urlVersionID = urlParams.get('version') ? parseInt(urlParams.get('version'), 10) : null;
    }
    if (urlVersionID) {
      this._pendingVersionID = urlVersionID;
      this.image.versionID = this._pendingVersionID;
    }

    this.image.fetch({
      error: function(model, xhr) {
        try {
          var resp = JSON.parse(xhr.responseText);
          if (resp && resp.error) {
            window.app.apiNotice.set({error: "Pipeline graph: " + resp.error});
          }
        } catch(e) {}
      }
    });

    var pipelineModel = this.model;
    var PanelResources = Backbone.Collection.extend({
      url: function() { return pipelineModel.url() + "/resources"; },
      parse: function(response) { return response.data; },
    });
    this.resourcesCollection = new PanelResources();

    var that = this;
    this.intervalID = window.setInterval(function() {
      that.image.fetch({isInterval: true});
      if (that.trackedVersion) {
        that._pollVersionPath();
      }
    }, fetchInterval);
  },
  events: {
    'click': 'clickPipeline',
    'click #edit-pipeline': 'clickEdit',
    'click #delete-pipeline': 'clickDelete',
    'click #pause-pipeline': 'clickPausePipeline',
    'click #unpause-pipeline': 'clickUnpausePipeline',
    'click #toggle-resources-panel': 'toggleResourcesPanel',
    'click #toggle-gear-panel': 'toggleGearPanel',
    'click #toggle-share-panel': 'toggleSharePanel',
    'click .piko-share-copy': 'copyShareUrl',
    'change #gear-hide-intermediates': 'toggleHideIntermediates',
    'change #gear-group-parallel': 'toggleGroupParallel',
    'click .piko-view-btn': 'switchView',
    'click #clear-version-scope': 'clearVersionScope',
  },
  render: function () {
    this.$el.html(this.template(addSessionFunctions({ pipeline: this.model.toJSON(), team: this.model.collection.team.toJSON() })));
    var that = this;
    this.graphView = new PipelineGraphView({model: this.image, onSVGReady: function(svg) {
      var container = that.$el.find("#graphviz")[0];
      if (!container) return;
      if (!that.graphZoom) {
        that.graphZoom = new PikoGraphZoom(container);
      }
      that.graphZoom.attachSVG(svg);
    }});
    this.$el.find("#graphviz").html(this.graphView.render().el);
    this.image.trigger("change", this.image);

    this.panelView = new PipelineResourcesPanelView({
      collection: this.resourcesCollection,
      pipeline: this.model,
      el: this.$el.find('#pipeline-resources-panel'),
    });

    this.$('#gear-hide-intermediates').prop('checked', this.image.hideIntermediates);
    this.$('#gear-group-parallel').prop('checked', this.image.groupParallel);
    this._applyView(this.currentView);

    // If loaded with ?version= param, find the resource and initiate tracking.
    // The versionID was already set on image before the first fetch in initialize().
    if (this._pendingVersionID) {
      var vid = this._pendingVersionID;
      delete this._pendingVersionID;
      var that = this;
      this.resourcesCollection.fetch({
        success: function() {
          that._resolveVersionResource(vid);
        },
      });
    }

    this.panelView.parentShowView = this;

    return this;
  },
  _applyView: function(mode) {
    this.$('.piko-view-btn').removeClass('active');
    this.$('.piko-view-btn[data-view="' + mode + '"]').addClass('active');
    if (mode === 'list') {
      this.$('.piko-view-graph').hide();
      this.$('.piko-view-list').show();
      this.$('.piko-gear-wrap').hide();
      this.$('.piko-share-wrap').hide();
      this.$('#gear-panel').removeClass('open');
      this.$('#share-panel').removeClass('open');
      if (!this.listView) {
        this.listView = new PipelineListView({
          el: this.$('.piko-view-list'),
          pipeline: this.model,
          resourcesCollection: this.resourcesCollection,
          parentShowView: this,
        });
      } else {
        this.listView.resumePolling();
      }
    } else {
      this.$('.piko-view-graph').show();
      this.$('.piko-view-list').hide();
      this.$('.piko-gear-wrap').show();
      this.$('.piko-share-wrap').show();
      if (this.listView) { this.listView.pausePolling(); }
    }
  },
  switchView: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var btn = $(event.currentTarget);
    var mode = btn.data('view');
    if (!mode) return;
    if (btn.hasClass('piko-view-btn-disabled')) return;
    if (btn.attr('id') === 'toggle-resources-panel') return;
    this.currentView = mode;
    localStorage.setItem("piko-pipeline-view", mode);
    this._applyView(mode);
    // Update URL to match the current view, preserving ?version= if tracking
    var tc = this.model.collection.team.get('canonical');
    var pc = this.model.get('canonical');
    var vqs = (this.trackedVersion && this.trackedVersion.versionID) ? '?version=' + this.trackedVersion.versionID : '';
    if (mode === 'list' && this.listView && this.listView.selectedJob) {
      window.app.router.navigate('teams/' + tc + '/pipelines/' + pc + '/jobs/' + this.listView.selectedJob + '/builds', { trigger: false, replace: true });
    } else {
      window.app.router.navigate('teams/' + tc + '/pipelines/' + pc, { trigger: false, replace: true });
    }
    if (vqs) {
      window.history.replaceState(null, '', window.location.pathname + vqs);
    }
  },
  remove: function() {
    clearInterval(this.intervalID);
    if (this.panelView) { this.panelView.remove(); }
    if (this.graphZoom) { this.graphZoom.destroy(); }
    if (this.listView) { this.listView.remove(); }
    Backbone.View.prototype.remove.call(this);
  },
  toggleResourcesPanel: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var panel = this.$el.find('#pipeline-resources-panel');
    panel.toggleClass('open');
    if (panel.hasClass('open')) {
      this.panelView.startPolling();
    } else {
      this.panelView.stopPolling();
    }
  },
  toggleHideIntermediates: function(event) {
    var checked = $(event.target).is(':checked');
    localStorage.setItem("piko-hide-intermediates", checked ? "1" : "0");
    this.image.hideIntermediates = checked;
    this.image.fetch();
  },
  toggleGroupParallel: function(event) {
    var checked = $(event.target).is(':checked');
    localStorage.setItem("piko-group-parallel", checked ? "1" : "0");
    this.image.groupParallel = checked;
    this.image.fetch();
  },
  toggleGearPanel: function(event) {
    event.preventDefault();
    event.stopPropagation();
    this.$('#share-panel').removeClass('open');
    var panel = this.$('#gear-panel');
    panel.toggleClass('open');
    if (panel.hasClass('open')) {
      var that = this;
      var closeHandler = function(e) {
        if (!$(e.target).closest('#gear-panel').length) {
          that.$('#gear-panel').removeClass('open');
        } else {
          $(document).one('click', closeHandler);
        }
      };
      $(document).one('click', closeHandler);
    }
  },
  toggleSharePanel: function(event) {
    event.preventDefault();
    event.stopPropagation();
    this.$('#gear-panel').removeClass('open');
    var panel = this.$('#share-panel');
    panel.toggleClass('open');
    if (panel.hasClass('open')) {
      var tc = this.model.collection.team.get('canonical');
      var pc = this.model.get('canonical');
      var base = window.location.origin + '/teams/' + tc + '/pipelines/' + pc;
      var params = [];
      if (this.image.hideIntermediates) params.push('hide_intermediates=1');
      if (this.image.groupParallel) params.push('group_parallel=1');
      var qs = params.length ? '?' + params.join('&') : '';
      var svgUrl = base + '/image.svg' + qs;
      var pngUrl = base + '/image.png' + qs;
      var pipelineName = this.model.get('name');
      this.$('#share-svg-url').val(svgUrl);
      this.$('#share-png-url').val(pngUrl);
      this.$('#share-md-url').val('![' + pipelineName + '](' + svgUrl + ')');
      var that = this;
      var closeHandler = function(e) {
        if (!$(e.target).closest('#share-panel').length) {
          that.$('#share-panel').removeClass('open');
        } else {
          $(document).one('click', closeHandler);
        }
      };
      $(document).one('click', closeHandler);
    }
  },
  copyShareUrl: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var btn = $(event.currentTarget);
    var targetId = btn.data('target');
    var value = this.$('#' + targetId).val();
    navigator.clipboard.writeText(value).then(function() {
      var original = btn.text();
      btn.text('Copied!');
      setTimeout(function() { btn.text(original); }, 1500);
    });
  },
  trackVersion: function(resourceCanonical, versionID, versionRef) {
    this.trackedVersion = { resource: resourceCanonical, versionID: versionID, ref: versionRef };
    this.image.versionID = versionID;
    this.image.fetch();
    this._pollVersionPath();
    this._updateVersionURL();
    // Refresh expanded panel version lists to show tracked indicator
    if (this.panelView) { this.panelView.refreshExpandedVersions(); }
  },
  clearVersionScope: function(event) {
    if (event) { event.preventDefault(); event.stopPropagation(); }
    this.trackedVersion = null;
    this.versionPathData = null;
    this.$('#version-scope-banner').hide();
    this.image.versionID = null;
    this.image.fetch();
    if (this.listView) { this.listView.clearVersionScope(); }
    // Remove ?version from URL using history API
    var tc = this.model.collection.team.get('canonical');
    var pc = this.model.get('canonical');
    window.history.replaceState(null, '', '/teams/' + tc + '/pipelines/' + pc);
    if (this.panelView) { this.panelView.refreshExpandedVersions(); }
  },
  showVersionBanner: function(resource, ref, completed, total) {
    this.$('#version-banner-resource').text(resource);
    this.$('#version-banner-ref').text(ref);
    this.$('#version-banner-progress').text(completed + '/' + total + ' completed');
    this.$('#version-scope-banner').show();
  },
  _pollVersionPath: function() {
    if (!this.trackedVersion || !this.trackedVersion.resource) return;
    var that = this;
    var tv = this.trackedVersion;
    var url = this.model.url() + '/resources/' + tv.resource + '/versions/' + tv.versionID + '/path';
    $.ajax({
      url: url,
      type: 'GET',
      contentType: 'application/json',
      headers: { 'Authorization': 'Bearer ' + session.get('jwt') },
      success: function(resp) {
        if (!resp.data) return;
        that.versionPathData = resp.data;
        // Determine the ref to display
        var ref = tv.ref || '';
        if (!ref && resp.data.resource && resp.data.resource.version) {
          var v = resp.data.resource.version;
          ref = versionRef(v);
        }
        that.showVersionBanner(resp.data.resource.canonical, ref, resp.data.completed, resp.data.total);
        if (that.listView && that.listView.applyVersionScope) {
          that.listView.applyVersionScope(resp.data);
        }
      },
    });
  },
  _updateVersionURL: function() {
    if (!this.trackedVersion) return;
    var tc = this.model.collection.team.get('canonical');
    var pc = this.model.get('canonical');
    var path = '/teams/' + tc + '/pipelines/' + pc;
    // Use history.replaceState to set ?version= without triggering Backbone routing
    window.history.replaceState(null, '', path + '?version=' + this.trackedVersion.versionID);
  },
  _resolveVersionResource: function(versionID) {
    // Try each resource sequentially until we find the one that owns this version
    var that = this;
    var resources = this.resourcesCollection.toJSON();
    var idx = 0;
    function tryNext() {
      if (idx >= resources.length) return;
      var rCan = resources[idx].canonical;
      idx++;
      var url = that.model.url() + '/resources/' + rCan + '/versions/' + versionID + '/path';
      $.ajax({
        url: url, type: 'GET', contentType: 'application/json',
        headers: session.isEmpty() ? {} : { 'Authorization': 'Bearer ' + session.get('jwt') },
        success: function(resp) {
          if (resp.data && resp.data.path && resp.data.path.length > 0) {
            that.trackedVersion = { resource: rCan, versionID: versionID, ref: '' };
            that.versionPathData = resp.data;
            var v = resp.data.resource.version || {};
            var ref = versionRef(v);
            that.trackedVersion.ref = ref;
            that.showVersionBanner(rCan, ref, resp.data.completed, resp.data.total);
            if (that.listView && that.listView.applyVersionScope) {
              that.listView.applyVersionScope(resp.data);
            }
          } else {
            tryNext();
          }
        },
        error: function() {
          tryNext();
        },
      });
    }
    tryNext();
  },
  clickPipeline: function(event) {
    // Only handle clicks on SVG links inside the graph
    if (event.target.parentElement && event.target.parentElement.href && event.target.parentElement.href.baseVal) {
      event.preventDefault();
      var href = event.target.parentElement.href.baseVal;
      // Pass tracked version via a property on the router so the handler
      // can read it synchronously (Backbone can't handle query params).
      if (this.trackedVersion && this.trackedVersion.versionID) {
        window.app.router._trackedVersionID = this.trackedVersion.versionID;
      }
      window.app.router.navigate(href, { trigger: true });
      // After navigate, set ?version= in URL for reload persistence
      if (this.trackedVersion && this.trackedVersion.versionID) {
        window.history.replaceState(null, '', window.location.pathname + '?version=' + this.trackedVersion.versionID);
      }
    }
  },
  clickEdit: function(event){
    event.preventDefault();
    window.app.router.navigate(this.model.url()+"/edit", { trigger: true });
  },
  clickDelete: function(event){
    event.preventDefault();
    if (confirm("Are you sure you want to delete Pipeline '"+this.model.get("name")+"'")) {
      var pps = this.model.collection;
      var that = this;
      withLoading(this.$('#delete-pipeline'), function() {
        return that.model.destroy({
          success: function() {
            window.app.router.navigate(pps.url(), { trigger: true });
          },
        });
      });
    }
  },
  clickPausePipeline: function(event) {
    event.preventDefault();
    var that = this;
    var url = this.model.url() + "/pause";
    withLoading(this.$('#pause-pipeline'), function() {
      return $.ajax({ url: url, type: "POST", contentType: "application/json", headers: { "Authorization": "Bearer " + session.get("jwt") },
        success: function() {
          window.app.apiNotice.setSuccess("Pipeline paused");
          that.model.fetch({ success: function() { that.render(); } });
        },
      });
    });
  },
  clickUnpausePipeline: function(event) {
    event.preventDefault();
    var that = this;
    var url = this.model.url() + "/unpause";
    withLoading(this.$('#unpause-pipeline'), function() {
      return $.ajax({ url: url, type: "POST", contentType: "application/json", headers: { "Authorization": "Bearer " + session.get("jwt") },
        success: function() {
          window.app.apiNotice.setSuccess("Pipeline unpaused");
          that.model.fetch({ success: function() { that.render(); } });
        },
      });
    });
  },
});

// --- PipelineListView ---

var statusLabels = {
  succeeded: 'passed',
  failed: 'failed',
  started: 'running',
  pending: 'pending',
  cancelled: 'cancelled',
};

var PipelineListView = Backbone.View.extend({
  jobRowTemplate: _.template($('#pipeline-list-job-row').html()),
  initialize: function(options) {
    this.pipeline = options.pipeline;
    this.resourcesCollection = options.resourcesCollection;
    this.parentShowView = options.parentShowView || null;
    this.jobsData = [];
    this.selectedJob = null;
    this.selectedResource = null;
    this.chainJobs = [];
    this.jobBuildsView = null;
    this._storagePrefix = 'piko-list-' + this.pipeline.get('canonical') + '-';
    this.collapsedGroups = JSON.parse(localStorage.getItem(this._storagePrefix + 'collapsed') || '{}');

    // Find trigger resources (resources consumed by a get step with trigger=true and no passed)
    this.triggerResources = this._findTriggerResources();

    // Restore from localStorage or default to first trigger resource
    var savedResource = localStorage.getItem(this._storagePrefix + 'resource');
    if (savedResource && this.triggerResources.indexOf(savedResource) >= 0) {
      this.selectedResource = savedResource;
    } else if (this.triggerResources.length > 0) {
      this.selectedResource = this.triggerResources[0];
    }

    this.listenTo(this.resourcesCollection, 'sync', this._renderResourceSelector);
    if (this.resourcesCollection.length === 0) {
      this.resourcesCollection.fetch();
    }

    this._renderResourceSelector();
    this._fetchJobs();
    var that = this;
    this._jobsIntervalID = window.setInterval(function() {
      that._fetchJobs();
    }, fetchInterval);
    this._resourcesIntervalID = window.setInterval(function() {
      that.resourcesCollection.fetch();
    }, fetchInterval);
  },
  events: {
    'click .piko-job-row': '_onClickJob',
    'click .piko-parallel-header': '_onToggleParallel',
    'click .piko-rsel-trigger': '_onToggleResourceMenu',
    'click .piko-rsel-option': '_onSelectResource',
    'click .piko-resource-check-btn': '_onCheckResource',
    'click .piko-vsel-btn': '_onToggleVersionMenu',
    'click .piko-vsel-item': '_onSelectVersion',
  },

  // --- Resource & chain resolution ---

  _findTriggerResources: function() {
    var jobs = this.pipeline.get('jobs') || [];
    var seen = {};
    var result = [];
    for (var i = 0; i < jobs.length; i++) {
      var plan = jobs[i].plan || [];
      for (var j = 0; j < plan.length; j++) {
        var s = plan[j];
        if (s.type === 'get' && s.get && s.get.trigger && (!s.get.passed || s.get.passed.length === 0)) {
          var canonical = s.get.type + '.' + s.get.name;
          if (!seen[canonical]) {
            seen[canonical] = true;
            result.push(canonical);
          }
        }
      }
    }
    return result;
  },

  _resolveChain: function(resourceCanonical) {
    var allJobs = this.pipeline.get('jobs') || [];
    var jobByName = {};
    for (var i = 0; i < allJobs.length; i++) {
      jobByName[allJobs[i].name] = allJobs[i];
    }

    // Step 1: find entry-point jobs that get this resource directly (no passed)
    var visited = {};
    var queue = [];
    var chain = [];
    for (var i = 0; i < allJobs.length; i++) {
      var plan = allJobs[i].plan || [];
      for (var j = 0; j < plan.length; j++) {
        var s = plan[j];
        if (s.type === 'get' && s.get) {
          var can = s.get.type + '.' + s.get.name;
          if (can === resourceCanonical && (!s.get.passed || s.get.passed.length === 0)) {
            if (!visited[allJobs[i].name]) {
              visited[allJobs[i].name] = true;
              queue.push(allJobs[i].name);
            }
          }
        }
      }
    }

    // Step 2: BFS — for each job in the chain, find downstream jobs via passed constraints
    while (queue.length > 0) {
      var jobName = queue.shift();
      chain.push(jobName);
      // Find all jobs that have a get step with passed containing jobName
      for (var i = 0; i < allJobs.length; i++) {
        if (visited[allJobs[i].name]) continue;
        var plan = allJobs[i].plan || [];
        for (var j = 0; j < plan.length; j++) {
          var s = plan[j];
          if (s.type === 'get' && s.get && s.get.passed) {
            for (var k = 0; k < s.get.passed.length; k++) {
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
  },

  _renderResourceSelector: function() {
    var container = this.$('.piko-rsel-container');
    var menuOpen = this.$('.piko-rsel-menu').hasClass('open');
    var versionMenuOpen = this.$('.piko-vsel-menu').hasClass('open');

    if (menuOpen || versionMenuOpen) {
      // Targeted in-place update: refresh status dots and info without closing the dropdown
      var resMap = {};
      this.resourcesCollection.each(function(r) {
        resMap[r.get('canonical')] = r.toJSON();
      });
      var selRes = resMap[this.selectedResource] || {};
      var selLv = selRes.latest_version;
      var selStatus = (selLv && selLv.status) ? selLv.status : '';

      // Update trigger button dot
      var triggerDot = this.$('.piko-rsel-trigger .piko-rsel-dot');
      triggerDot.attr('class', 'piko-rsel-dot piko-status-dot-' + selStatus);

      // Update each option dot
      this.$('.piko-rsel-option').each(function() {
        var canonical = $(this).data('canonical');
        var r = resMap[canonical] || {};
        var rlv = r.latest_version;
        var rStatus = (rlv && rlv.status) ? rlv.status : '';
        $(this).find('.piko-rsel-dot').attr('class', 'piko-rsel-dot piko-status-dot-' + rStatus);
      });

      // Update info text
      var infoHtml = '';
      if (selLv && selLv.version) {
        for (var key in selLv.version) {
          if (selLv.version.hasOwnProperty(key)) {
            infoHtml += '<span class="piko-resource-bar-ver">' + _.escape(key + ': ' + selLv.version[key]) + '</span>';
            break;
          }
        }
      }
      if (selRes.check_interval) {
        infoHtml += '<span class="piko-resource-bar-meta">' + _.escape(selRes.check_interval) + '</span>';
      }
      if (selRes.last_check) {
        infoHtml += '<span class="piko-resource-bar-meta">checked ' + pikoTimeAgo(selRes.last_check) + '</span>';
      }
      this.$('.piko-resource-bar-info').html(infoHtml);
      return;
    }
    if (this.triggerResources.length === 0) {
      container.html('<span style="color:var(--text-muted)">No trigger resources</span>');
      return;
    }
    var resMap = {};
    this.resourcesCollection.each(function(r) {
      resMap[r.get('canonical')] = r.toJSON();
    });

    var res = resMap[this.selectedResource] || {};
    var lv = res.latest_version;
    var statusClass = (lv && lv.status) ? lv.status : '';

    // Custom dropdown trigger
    var html = '<div class="piko-rsel">';
    html += '<button class="piko-rsel-trigger" type="button">';
    if (statusClass) {
      html += '<span class="piko-rsel-dot piko-status-dot-' + statusClass + '"></span>';
    }
    html += '<span class="piko-rsel-label">' + _.escape(this.selectedResource) + '</span>';
    html += '<i class="bi bi-chevron-down piko-rsel-arrow"></i>';
    html += '</button>';

    // Dropdown menu
    html += '<div class="piko-rsel-menu">';
    for (var i = 0; i < this.triggerResources.length; i++) {
      var canonical = this.triggerResources[i];
      var r = resMap[canonical] || {};
      var rlv = r.latest_version;
      var rStatus = (rlv && rlv.status) ? rlv.status : '';
      var active = canonical === this.selectedResource ? ' active' : '';
      html += '<div class="piko-rsel-option' + active + '" data-canonical="' + _.escape(canonical) + '">';
      html += '<span class="piko-rsel-dot piko-status-dot-' + rStatus + '"></span>';
      html += '<span>' + _.escape(canonical) + '</span>';
      html += '</div>';
    }
    html += '</div></div>';

    // Version selector placeholder (rendered by _renderVersionSelector)
    html += '<div class="piko-vsel-wrap"></div>';

    // Version + check info
    html += '<span class="piko-resource-bar-info">';
    if (lv && lv.version) {
      for (var key in lv.version) {
        if (lv.version.hasOwnProperty(key)) {
          html += '<span class="piko-resource-bar-ver">' + _.escape(key + ': ' + lv.version[key]) + '</span>';
          break;
        }
      }
    }
    if (res.check_interval) {
      html += '<span class="piko-resource-bar-meta">' + _.escape(res.check_interval) + '</span>';
    }
    if (res.last_check) {
      html += '<span class="piko-resource-bar-meta">checked ' + pikoTimeAgo(res.last_check) + '</span>';
    }
    html += '</span>';

    if (!session.isEmpty()) {
      html += '<button class="btn btn-sm btn-outline-warning piko-resource-check-btn"><i class="bi bi-arrow-clockwise"></i> Check Now</button>';
    }

    container.html(html);
    this._renderVersionSelector();
  },

  _onToggleResourceMenu: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var menu = this.$('.piko-rsel-menu');
    var isOpen = menu.hasClass('open');
    menu.toggleClass('open');
    if (!isOpen) {
      // Close on outside click
      var that = this;
      $(document).one('click', function() { that.$('.piko-rsel-menu').removeClass('open'); });
    }
  },

  _onSelectResource: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var canonical = $(event.currentTarget).data('canonical');
    this.$('.piko-rsel-menu').removeClass('open');
    if (!canonical || canonical === this.selectedResource) return;
    this.selectedResource = canonical;
    this._recentVersions = null;
    this.clearVersionScope();
    localStorage.setItem(this._storagePrefix + 'resource', canonical);
    this.selectedJob = null;
    if (this.jobBuildsView) {
      this.jobBuildsView.remove();
      this.jobBuildsView = null;
    }
    this.$('.piko-job-detail').empty();
    this._renderResourceSelector();
    this._renderJobList();
    if (this.chainJobs.length > 0) {
      this._selectJob(this.chainJobs[0]);
    }
  },

  _onCheckResource: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var canonical = this.selectedResource;
    if (!canonical) return;
    var tc = this.pipeline.collection ? this.pipeline.collection.team.get('canonical') : '';
    var pc = this.pipeline.get('canonical');
    var url = '/teams/' + tc + '/pipelines/' + pc + '/resources/' + canonical + '/trigger';
    var that = this;
    $.ajax({
      url: url, type: 'POST', contentType: 'application/json',
      headers: { 'Authorization': 'Bearer ' + session.get('jwt') },
      success: function() {
        window.app.apiNotice.setSuccess('Resource check triggered');
        that.resourcesCollection.fetch();
      },
      error: function() {
        window.app.apiNotice.set({error: 'Failed to trigger resource check'});
      },
    });
  },

  // --- Version selector & scoping ---

  _renderVersionSelector: function() {
    // Skip re-render when the version menu is open to avoid destroying it
    if (this.$('.piko-vsel-menu').hasClass('open')) {
      return;
    }
    var wrap = this.$('.piko-vsel-wrap');
    if (!this.selectedResource) {
      wrap.empty();
      return;
    }
    var label = this._scopedVersionRef || 'All';
    var html = '<button class="piko-vsel-btn" type="button">';
    html += '<i class="bi bi-signpost-2"></i>';
    html += '<span>' + _.escape(label) + '</span>';
    html += '<i class="bi bi-chevron-down"></i>';
    html += '</button>';
    html += '<div class="piko-vsel-menu">';
    html += '<div class="piko-vsel-item piko-vsel-item-all" data-version-id="">All versions</div>';
    if (this._recentVersions) {
      for (var i = 0; i < this._recentVersions.length; i++) {
        var v = this._recentVersions[i];
        var ref = '';
        if (v.version) {
          ref = versionRef(v.version);
        }
        var status = v.status || '';
        var active = (this._scopedVersionID && this._scopedVersionID === v.id) ? ' active' : '';
        html += '<div class="piko-vsel-item' + active + '" data-version-id="' + v.id + '" data-version-ref="' + _.escape(ref) + '">';
        html += '<span class="piko-vsel-dot piko-status-dot-' + status + '"></span>';
        html += '<span class="piko-vsel-ref">' + _.escape(ref) + '</span>';
        html += '</div>';
      }
    }
    html += '</div>';
    wrap.html(html);
    // Fetch recent versions if not loaded
    if (!this._recentVersions) {
      this._fetchRecentVersions();
    }
  },

  _fetchRecentVersions: function() {
    if (!this.selectedResource) return;
    var that = this;
    var url = this.pipeline.url() + '/resources/' + this.selectedResource + '/versions?limit=10';
    $.ajax({
      url: url,
      type: 'GET',
      contentType: 'application/json',
      headers: session.isEmpty() ? {} : { 'Authorization': 'Bearer ' + session.get('jwt') },
      success: function(resp) {
        if (resp && resp.data) {
          that._recentVersions = resp.data;
          // If menu is open, update items in-place instead of full re-render
          var menu = that.$('.piko-vsel-menu');
          if (menu.hasClass('open')) {
            that._updateVersionMenuItems(menu);
          } else {
            that._renderVersionSelector();
          }
        }
      },
    });
  },

  _updateVersionMenuItems: function(menu) {
    var html = '<div class="piko-vsel-item piko-vsel-item-all" data-version-id="">All versions</div>';
    if (this._recentVersions) {
      for (var i = 0; i < this._recentVersions.length; i++) {
        var v = this._recentVersions[i];
        var ref = '';
        if (v.version) {
          ref = versionRef(v.version);
        }
        var status = v.status || '';
        var active = (this._scopedVersionID && this._scopedVersionID === v.id) ? ' active' : '';
        html += '<div class="piko-vsel-item' + active + '" data-version-id="' + v.id + '" data-version-ref="' + _.escape(ref) + '">';
        html += '<span class="piko-vsel-dot piko-status-dot-' + status + '"></span>';
        html += '<span class="piko-vsel-ref">' + _.escape(ref) + '</span>';
        html += '</div>';
      }
    }
    menu.html(html);
  },

  _onToggleVersionMenu: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var menu = this.$('.piko-vsel-menu');
    var isOpen = menu.hasClass('open');
    menu.toggleClass('open');
    if (!isOpen) {
      this._fetchRecentVersions();
      var that = this;
      setTimeout(function() {
        $(document).one('click', function() { that.$('.piko-vsel-menu').removeClass('open'); });
      }, 0);
    }
  },

  _onSelectVersion: function(event) {
    event.preventDefault();
    event.stopPropagation();
    this.$('.piko-vsel-menu').removeClass('open');
    var el = $(event.currentTarget);
    var versionID = el.data('version-id');
    if (versionID === '' || versionID === undefined) {
      // "All" selected - clear scope
      this.clearVersionScope();
      if (this.parentShowView) {
        this.parentShowView.clearVersionScope();
      }
      return;
    }
    var ref = el.data('version-ref') || '';
    this._scopedVersionID = parseInt(versionID, 10);
    this._scopedVersionRef = ref;
    this._renderVersionSelector();
    // Trigger tracking on parent PipelineShowView (updates banner, graph, and polls)
    if (this.parentShowView && this.parentShowView.trackVersion) {
      this.parentShowView.trackVersion(this.selectedResource, this._scopedVersionID, ref);
    } else {
      // Fallback: fetch path directly for list-only scoping
      var url = this.pipeline.url() + '/resources/' + this.selectedResource + '/versions/' + this._scopedVersionID + '/path';
      var that = this;
      $.ajax({
        url: url, type: 'GET', contentType: 'application/json',
        headers: session.isEmpty() ? {} : { 'Authorization': 'Bearer ' + session.get('jwt') },
        success: function(resp) {
          if (resp && resp.data) {
            that.applyVersionScope(resp.data);
          }
        },
      });
    }
  },

  applyVersionScope: function(pathData) {
    if (!pathData || !pathData.path) return;
    this._versionPathData = pathData;
    if (pathData.resource && pathData.resource.version) {
      var v = pathData.resource.version;
      this._scopedVersionRef = versionRef(v);
    }
    // Try to extract version ID from path builds if not already set
    if (!this._scopedVersionID) {
      for (var i = 0; i < pathData.path.length; i++) {
        if (pathData.path[i].build && pathData.path[i].build.version_id) {
          this._scopedVersionID = pathData.path[i].build.version_id;
          break;
        }
      }
    }
    // Override chain with path job names
    var pathJobs = [];
    var pathBuildMap = {};
    for (var i = 0; i < pathData.path.length; i++) {
      pathJobs.push(pathData.path[i].job_name);
      if (pathData.path[i].build) {
        pathBuildMap[pathData.path[i].job_name] = pathData.path[i].build;
      }
    }
    var wasScoped = !!this._versionChainJobs;
    this._versionChainJobs = pathJobs;
    this._versionBuildMap = pathBuildMap;
    this.chainJobs = pathJobs;
    this._renderJobList();
    this._renderVersionSelector();
    // Only re-select the job when the scope is first applied (not on every poll)
    if (!wasScoped) {
      if (this.selectedJob && pathJobs.indexOf(this.selectedJob) >= 0) {
        this._selectJob(this.selectedJob);
      } else if (pathJobs.length > 0) {
        this._selectJob(pathJobs[0]);
      }
    }
  },

  clearVersionScope: function() {
    this._scopedVersionID = null;
    this._scopedVersionRef = null;
    this._versionPathData = null;
    this._versionChainJobs = null;
    this._versionBuildMap = null;
    // Re-resolve normal chain
    if (this.selectedResource) {
      this.chainJobs = this._resolveChain(this.selectedResource);
    }
    this._renderJobList();
    this._renderVersionSelector();
    // Re-select current job to show all builds (no version filter)
    if (this.selectedJob) {
      this._selectJob(this.selectedJob);
    }
  },

  // --- Data fetching ---

  _fetchJobs: function() {
    var that = this;
    var url = this.pipeline.url() + "/jobs";
    $.ajax({
      url: url,
      type: 'GET',
      contentType: 'application/json',
      headers: session.isEmpty() ? {} : { 'Authorization': 'Bearer ' + session.get('jwt') },
      success: function(resp) {
        if (resp && resp.data) {
          that.jobsData = resp.data;
          that._renderJobList();
          // Auto-select saved job or first non-succeeded job
          if (!that.selectedJob && that.chainJobs.length > 0) {
            var pick = null;
            var savedJob = localStorage.getItem(that._storagePrefix + 'job');
            if (savedJob && that.chainJobs.indexOf(savedJob) >= 0) {
              pick = savedJob;
            } else {
              pick = that.chainJobs[0];
              var statusMap = that._buildStatusMap();
              for (var i = 0; i < that.chainJobs.length; i++) {
                var d = statusMap[that.chainJobs[i]];
                if (d && d.latest_status && d.latest_status !== 'succeeded') {
                  pick = that.chainJobs[i];
                  break;
                }
              }
            }
            that._selectJob(pick);
          }
        }
      },
    });
  },

  _buildStatusMap: function() {
    var statusMap = {};
    for (var i = 0; i < this.jobsData.length; i++) {
      statusMap[this.jobsData[i].name] = this.jobsData[i];
    }
    return statusMap;
  },

  // --- Rendering ---

  _buildTree: function(chainJobs) {
    // Build a parent→children map and find roots (entry-point jobs).
    var allJobs = this.pipeline.get('jobs') || [];
    var jobByName = {};
    for (var i = 0; i < allJobs.length; i++) jobByName[allJobs[i].name] = allJobs[i];
    var chainSet = {};
    for (var i = 0; i < chainJobs.length; i++) chainSet[chainJobs[i]] = true;

    // For each chain job, find its upstream parents (passed constraints within the chain)
    var parents = {}; // jobName → [parentJobName, ...]
    var children = {}; // jobName → [childJobName, ...]
    for (var i = 0; i < chainJobs.length; i++) {
      parents[chainJobs[i]] = [];
      children[chainJobs[i]] = [];
    }
    for (var i = 0; i < chainJobs.length; i++) {
      var name = chainJobs[i];
      var pj = jobByName[name];
      if (!pj) continue;
      var plan = pj.plan || [];
      for (var j = 0; j < plan.length; j++) {
        var s = plan[j];
        if (s.type === 'get' && s.get && s.get.passed) {
          for (var k = 0; k < s.get.passed.length; k++) {
            if (chainSet[s.get.passed[k]]) {
              parents[name].push(s.get.passed[k]);
              children[s.get.passed[k]].push(name);
            }
          }
        }
      }
    }

    // Roots are jobs with no parents in the chain
    var roots = [];
    for (var i = 0; i < chainJobs.length; i++) {
      if (parents[chainJobs[i]].length === 0) {
        roots.push(chainJobs[i]);
      }
    }

    return { roots: roots, children: children, parents: parents };
  },

  _renderJobList: function() {
    if (!this.selectedResource) return;

    // Use version-scoped chain if tracking, otherwise resolve from pipeline
    if (!this._versionChainJobs) {
      this.chainJobs = this._resolveChain(this.selectedResource);
    }
    var statusMap = this._buildStatusMap();
    // When version-scoped, override status with the tracked version's build status
    if (this._versionBuildMap) {
      for (var jn in this._versionBuildMap) {
        if (this._versionBuildMap.hasOwnProperty(jn)) {
          var b = this._versionBuildMap[jn];
          statusMap[jn] = statusMap[jn] || {};
          statusMap[jn].latest_status = b.status;
          statusMap[jn].has_running = (b.status === 'started' || b.status === 'pending');
        }
      }
    }
    var tree = this._buildTree(this.chainJobs);

    // Render tree recursively. Siblings (jobs sharing the same set of parents)
    // are grouped as parallel when there are 2+.
    var that = this;
    var rendered = {};

    var renderChildren = function(parentName) {
      var kids = tree.children[parentName] || [];
      if (kids.length === 0) return '';

      // Group siblings by their parent set (jobs with identical parents are parallel)
      var groupKey = function(name) {
        return (tree.parents[name] || []).slice().sort().join(',');
      };
      var keyToKids = {};
      var kidOrder = [];
      for (var i = 0; i < kids.length; i++) {
        if (rendered[kids[i]]) continue;
        var gk = groupKey(kids[i]);
        if (!keyToKids[gk]) {
          keyToKids[gk] = [];
          kidOrder.push(gk);
        }
        keyToKids[gk].push(kids[i]);
      }

      var html = '';
      for (var i = 0; i < kidOrder.length; i++) {
        var group = keyToKids[kidOrder[i]];
        // Filter out already rendered
        group = group.filter(function(n) { return !rendered[n]; });
        if (group.length === 0) continue;

        if (group.length >= 2) {
          html += that._renderParallelGroup(group, statusMap, tree, rendered, function(names) {
            var sub = '';
            for (var j = 0; j < names.length; j++) {
              rendered[names[j]] = true;
            }
            for (var j = 0; j < names.length; j++) {
              sub += renderChildren(names[j]);
            }
            return sub;
          });
        } else {
          var name = group[0];
          rendered[name] = true;
          var data = statusMap[name] || { name: name, latest_status: '' };
          html += that._renderJobRow(data);
          html += renderChildren(name);
        }
      }
      return html;
    };

    // Render roots
    var roots = tree.roots;
    var html = '';
    if (roots.length >= 2) {
      html += this._renderParallelGroup(roots, statusMap, tree, rendered, function(names) {
        var sub = '';
        for (var j = 0; j < names.length; j++) {
          rendered[names[j]] = true;
        }
        for (var j = 0; j < names.length; j++) {
          sub += renderChildren(names[j]);
        }
        return sub;
      });
    } else if (roots.length === 1) {
      rendered[roots[0]] = true;
      var data = statusMap[roots[0]] || { name: roots[0], latest_status: '' };
      html += this._renderJobRow(data);
      html += renderChildren(roots[0]);
    }

    this.$('.piko-job-list').html(html);
  },

  _renderJobRow: function(data, extraClass) {
    var status = data.latest_status || '';
    if (data.has_running) status = 'started';
    if (data.paused) status = 'paused';
    var isActive = this.selectedJob === data.name;
    return '<div class="piko-job-row' + (isActive ? ' active' : '') + (extraClass || '') + '" data-job="' + _.escape(data.name) + '">' +
      this.jobRowTemplate({
        name: data.name,
        status: status,
        statusLabel: statusLabels[status] || '',
      }) +
      '</div>';
  },

  // renderChildrenFn(jobNames) → html string for downstream jobs of the group members
  _renderParallelGroup: function(jobNames, statusMap, tree, rendered, renderChildrenFn) {
    var groupKey = jobNames.slice().sort().join(',');
    var isCollapsed = this.collapsedGroups[groupKey];
    var arrow = isCollapsed ? '&#9654;' : '&#9660;';
    var counts = {};
    for (var i = 0; i < jobNames.length; i++) {
      var d = statusMap[jobNames[i]] || {};
      var s = d.paused ? 'paused' : (d.has_running ? 'started' : (d.latest_status || ''));
      counts[s] = (counts[s] || 0) + 1;
    }
    var html = '<div class="piko-parallel-header" data-group="' + _.escape(groupKey) + '"><span>' + arrow + ' parallel</span>';
    html += '<span class="piko-parallel-counts">';
    for (var st in counts) {
      if (st) html += '<span class="piko-status-dot-' + st + '" style="width:8px;height:8px;border-radius:50%;display:inline-block"></span> ' + counts[st] + ' ';
    }
    html += '</span></div>';
    html += '<div class="piko-parallel-nested"' + (isCollapsed ? ' style="display:none"' : '') + '>';

    // Identify fan-in children: jobs whose parents are ALL members of this parallel group.
    var groupSet = {};
    for (var i = 0; i < jobNames.length; i++) groupSet[jobNames[i]] = true;
    var fanInChildren = [];
    var fanInParentSet = {};
    for (var i = 0; i < jobNames.length; i++) {
      var kids = tree.children[jobNames[i]] || [];
      for (var k = 0; k < kids.length; k++) {
        if (rendered[kids[k]]) continue;
        var kidParents = tree.parents[kids[k]] || [];
        if (kidParents.length >= 2) {
          var allInGroup = true;
          for (var p = 0; p < kidParents.length; p++) {
            if (!groupSet[kidParents[p]]) { allInGroup = false; break; }
          }
          if (allInGroup) {
            rendered[kids[k]] = true;
            fanInChildren.push(kids[k]);
            for (var p = 0; p < kidParents.length; p++) {
              fanInParentSet[kidParents[p]] = true;
            }
          }
        }
      }
    }

    var hasFanIn = fanInChildren.length > 0;

    // Build the fan-in section HTML (parents in bordered block + children after)
    var fanInHtml = '';
    if (hasFanIn) {
      fanInHtml += '<div class="piko-fan-in-section">';
      for (var j = 0; j < jobNames.length; j++) {
        if (!fanInParentSet[jobNames[j]]) continue;
        var data = statusMap[jobNames[j]] || { name: jobNames[j], latest_status: '' };
        fanInHtml += this._renderJobRow(data);
        if (renderChildrenFn) {
          fanInHtml += renderChildrenFn([jobNames[j]]);
        }
      }
      fanInHtml += '</div>';
      fanInHtml += '<div class="piko-fan-in-cont">';
      for (var i = 0; i < fanInChildren.length; i++) {
        var data = statusMap[fanInChildren[i]] || { name: fanInChildren[i], latest_status: '' };
        fanInHtml += this._renderJobRow(data);
        if (renderChildrenFn) {
          fanInHtml += renderChildrenFn([fanInChildren[i]]);
        }
      }
      fanInHtml += '</div>';
    }

    // Render members in order; non-fan-in-parent jobs get extra padding when
    // fan-in exists so all dots align. Insert the fan-in section at the
    // position of the first fan-in parent.
    var fanInInserted = false;
    var alignClass = hasFanIn ? ' piko-fan-in-aligned' : '';
    for (var j = 0; j < jobNames.length; j++) {
      if (fanInParentSet[jobNames[j]]) {
        if (!fanInInserted) {
          html += fanInHtml;
          fanInInserted = true;
        }
        continue;
      }
      var data = statusMap[jobNames[j]] || { name: jobNames[j], latest_status: '' };
      html += this._renderJobRow(data, alignClass);
      if (renderChildrenFn) {
        html += renderChildrenFn([jobNames[j]]);
      }
    }
    if (fanInHtml && !fanInInserted) {
      html += fanInHtml;
    }

    html += '</div>';

    return html;
  },

  // --- Job selection & detail ---

  _onClickJob: function(event) {
    event.preventDefault();
    var jobName = $(event.currentTarget).data('job');
    if (jobName) {
      this._selectJob(jobName);
    }
  },

  _selectJob: function(jobName) {
    this.selectedJob = jobName;
    localStorage.setItem(this._storagePrefix + 'job', jobName);
    // Update URL to match the job builds path, preserving ?version= if tracking
    var tc = this.pipeline.collection ? this.pipeline.collection.team.get('canonical') : '';
    var pc = this.pipeline.get('canonical');
    window.app.router.navigate('teams/' + tc + '/pipelines/' + pc + '/jobs/' + jobName + '/builds', { trigger: false, replace: true });
    var trackedVID = (this.parentShowView && this.parentShowView.trackedVersion) ? this.parentShowView.trackedVersion.versionID : null;
    if (trackedVID) {
      window.history.replaceState(null, '', window.location.pathname + '?version=' + trackedVID);
    }
    this.$('.piko-job-row').removeClass('active');
    this.$('.piko-job-row[data-job="' + jobName + '"]').addClass('active');

    if (this.jobBuildsView) {
      this.jobBuildsView.remove();
      this.jobBuildsView = null;
    }

    var jbs = new Jobs(null, { pipeline: this.pipeline });
    var jb = new Job({ name: jobName }, { collection: jbs });
    var builds = new Builds(null, { job: jb });

    var that = this;
    jb.fetch({
      success: function() {
        var detailEl = $('<div></div>');
        that.$('.piko-job-detail').html(detailEl);
        that.jobBuildsView = new JobBuildsView({
          el: detailEl,
          collection: builds,
          job: jb,
          pipeline: that.pipeline,
          embedded: true,
          trackedVersionID: trackedVID,
        });
        that.jobBuildsView.render();
      },
      error: function() {
        that.$('.piko-job-detail').html('<div style="padding:14px;color:var(--text-muted)">Failed to load job.</div>');
      },
    });
  },

  _onToggleParallel: function(event) {
    var header = $(event.currentTarget);
    var nested = header.next('.piko-parallel-nested');
    nested.toggle();
    var groupKey = header.data('group');
    var isVisible = nested.is(':visible');
    if (groupKey) {
      if (isVisible) {
        delete this.collapsedGroups[groupKey];
      } else {
        this.collapsedGroups[groupKey] = true;
      }
    }
    localStorage.setItem(this._storagePrefix + 'collapsed', JSON.stringify(this.collapsedGroups));
    var arrow = isVisible ? '&#9660;' : '&#9654;';
    header.find('span:first').html(arrow + ' parallel');
  },

  pausePolling: function() {
    if (this._jobsIntervalID) {
      clearInterval(this._jobsIntervalID);
      this._jobsIntervalID = null;
    }
    if (this._resourcesIntervalID) {
      clearInterval(this._resourcesIntervalID);
      this._resourcesIntervalID = null;
    }
  },
  resumePolling: function() {
    if (this._jobsIntervalID) return;
    this._fetchJobs();
    this.resourcesCollection.fetch();
    var that = this;
    this._jobsIntervalID = window.setInterval(function() {
      that._fetchJobs();
    }, fetchInterval);
    this._resourcesIntervalID = window.setInterval(function() {
      that.resourcesCollection.fetch();
    }, fetchInterval);
  },
  remove: function() {
    this.pausePolling();
    if (this.jobBuildsView) { this.jobBuildsView.remove(); }
    Backbone.View.prototype.remove.call(this);
  },
});
