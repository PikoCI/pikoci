'use strict';

import { session } from '../collections.js';
import { PipelineImage } from '../models.js';
import { addSessionFunctions, clickLink, fetchInterval, pikoTimeAgo, withLoading } from '../namespace.js';
import { PipelineGraphView, PikoGraphZoom } from './editor.js';

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

export var PipelineShowView = Backbone.View.extend({
  template: _.template($('#pipeline-show-view').html()),
  initialize: function(options) {
    this.image = options.image;
    this.image.fetch();

    var that = this;
    this.intervalID = window.setInterval(function() {
      that.image.fetch({isInterval: true});
    }, fetchInterval);
  },
  events: {
    'click': 'clickPipeline',
    'click #edit-pipeline': 'clickEdit',
    'click #delete-pipeline': 'clickDelete',
    'click #pause-pipeline': 'clickPausePipeline',
    'click #unpause-pipeline': 'clickUnpausePipeline',
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
    return this;
  },
  remove: function() {
    clearInterval(this.intervalID);
    if (this.graphZoom) { this.graphZoom.destroy(); }
    Backbone.View.prototype.remove.call(this);
  },
  clickPipeline: function(event) {
    event.preventDefault();
    if (event.target.parentElement.href !== undefined){
      window.app.router.navigate(event.target.parentElement.href.baseVal, { trigger: true });
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
