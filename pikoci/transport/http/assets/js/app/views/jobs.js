'use strict';

import { session } from '../collections.js';
import { Build } from '../models.js';
import { addSessionFunctions, fetchInterval, pikoTimeAgo } from '../namespace.js';

export var JobBuildsView = Backbone.View.extend({
  template: _.template($('#job-builds-view').html()),
  initialize: function(opts) {
    this.listenTo(this.collection, "reset", this.addBuilds);
    this.listenTo(this.collection, "add", this.addBuild);

    this.job = opts.job;
    this.pipeline = opts.pipeline;

    var that = this;
    this.collection.fetch({
      reset: true,
      success: function(){
        that.activateAndNavigate(opts.currentBuildID);
        that.bindScrollListener();
      },
    });

    this.intervalID = window.setInterval(function() {
      that.collection.fetchNew();
    }, fetchInterval);
  },
  events: {
    'click #trigger-job': 'clickTriggerJob',
    'click .piko-build-tab': 'clickOnTab',
  },
  addBuilds: function() {
    var that = this;
    this.collection.each(function(m) {
      that.addBuild(m);
    });
  },
  render: function () {
    this.$el.html(this.template(addSessionFunctions({
      team: this.collection.job.collection.pipeline.collection.team.toJSON(),
      pipeline: this.collection.job.collection.pipeline.toJSON(),
      job: this.collection.job.toJSON(),
    })));
    return this;
  },
  activateAndNavigate: function(requestedBID) {
    var that = this;
    var found = requestedBID && this.collection.get(requestedBID);
    if (requestedBID && !found) {
      var singleBuild = new Build({build_number: requestedBID});
      singleBuild.url = this.collection.url() + "/" + requestedBID;
      singleBuild.fetch({
        success: function(model) {
          that.collection.add(model, {merge: true});
          that.doActivate(requestedBID);
        },
        error: function() {
          that.doActivate(null);
        },
      });
    } else {
      this.doActivate(requestedBID);
    }
  },
  doActivate: function(bid) {
    bid = this.collection.setActive(bid);
    this.currentBuildID = bid;
    var tc = this.collection.job.collection.pipeline.collection.team.get("canonical");
    var url = new URL(location.origin+"/teams/"+tc+"/pipelines/"+this.pipeline.get("canonical")+"/jobs/"+this.job.get("name")+"/builds/"+bid);
    window.app.router.navigate(url.pathname, { trigger: false, replace: true });
  },
  bindScrollListener: function() {
    var that = this;
    this._scrollHandler = function() {
      var el = that.$('#builds-tabs')[0];
      if (el && el.scrollLeft + el.clientWidth >= el.scrollWidth - 50) {
        that.collection.fetchMore();
      }
    };
    this.$('#builds-tabs').on('scroll', this._scrollHandler);
  },
  remove: function() {
    clearInterval(this.intervalID);
    this.$('#builds-tabs').off('scroll', this._scrollHandler);
    Backbone.View.prototype.remove.call(this);
  },
  addBuild: function(m) {
    var tab = new JobBuildsTabView({model: m});
    var cont = new JobBuildsContentView({model: m});
    var idx = this.collection.indexOf(m);
    var tabsEl = this.$('#builds-tabs');
    var contEl = this.$('#builds-content');
    if (idx === 0) {
      tabsEl.prepend(tab.render().el);
      contEl.prepend(cont.render().el);
    } else {
      var tabChildren = tabsEl.children();
      if (idx < tabChildren.length) {
        $(tabChildren[idx]).before(tab.render().el);
        $(contEl.children()[idx]).before(cont.render().el);
      } else {
        tabsEl.append(tab.render().el);
        contEl.append(cont.render().el);
      }
    }
  },
  clickTriggerJob: function(event) {
    event.preventDefault();
    this.collection.job.fetchTrigger();
  },
  clickOnTab: function(event) {
    event.preventDefault();
    var el = event.currentTarget || event.target;
    var bid = el.id.split("-")[1];
    this.collection.setActive(bid);
    this.currentBuildID = bid;
    var tc = this.collection.job.collection.pipeline.collection.team.get("canonical");
    var url = new URL(location.origin+"/teams/"+tc+"/pipelines/"+this.pipeline.get("canonical")+"/jobs/"+this.job.get("name")+"/builds/"+bid);
    window.app.router.navigate(url.pathname, { trigger: false, replace: true });
  }
});

var JobBuildsTabView = Backbone.View.extend({
  tagName: "div",
  attributes: function() {
    return {
      class: "piko-build-tab",
      id: "t-"+this.model.get("build_number"),
    };
  },
  initialize: function(opts) {
    this.listenTo(this.model, "change", this.render);
  },
  template: _.template($('#job-builds-tab-view').html()),
  render: function () {
    if (this.model.get("active")) {
      this.$el.addClass("active");
    } else {
      this.$el.removeClass("active");
    }
    var data = this.model.toJSON();
    this.$el.html(this.template(data));
    var stripe = this.$el.find(".piko-tab-status");
    stripe.removeClass("status-succeeded status-failed status-started status-cancelled status-pending");
    stripe.addClass("status-"+this.model.get("status"));
    return this;
  },
});

var JobBuildsContentView = Backbone.View.extend({
  className: "piko-build-content",
  attributes: function() {
    return { id: "c-"+this.model.get("build_number") };
  },
  template: _.template($('#job-builds-content-view').html()),
  events: {
    'click .piko-cancel-build': 'cancelBuild',
    'click .piko-retry-build': 'retryBuild',
    'click .piko-follow-toggle': 'toggleFollow',
  },
  initialize: function() {
    this.autoFollow = true;
    this._isAutoScrolling = false;
    this._elapsedInterval = null;
    this.listenTo(this.model, "change", this.render);
  },
  cancelBuild: function(e) {
    e.preventDefault();
    var that = this;
    var bid = this.model.get("build_number");
    var url = this.model.collection.url() + "/" + bid + "/cancel.json";
    $.ajax({ url: url, type: "POST", headers: { "Authorization": "Bearer " + session.get("jwt") },
      success: function() { that.model.fetch(); },
    });
  },
  retryBuild: function(e) {
    e.preventDefault();
    var bid = this.model.get("build_number");
    var collection = this.model.collection;
    var url = collection.url() + "/" + bid + "/retry.json";
    $.ajax({ url: url, type: "POST", headers: { "Authorization": "Bearer " + session.get("jwt") },
      success: function() { collection.fetchNew(); },
    });
  },
  toggleFollow: function(e) {
    e.preventDefault();
    this.autoFollow = !this.autoFollow;
    this._updateFollowButton();
    if (this.autoFollow) {
      var runningPre = this.$el.find('.piko-step-row[data-status="started"] .piko-step-row-body pre');
      if (!runningPre.length) {
        runningPre = this.$el.find('.piko-step-row-body:visible pre').last();
      }
      if (runningPre.length) {
        var that = this;
        var el = runningPre[0];
        this._isAutoScrolling = true;
        requestAnimationFrame(function() {
          el.scrollTop = el.scrollHeight;
          requestAnimationFrame(function() { that._isAutoScrolling = false; });
        });
      }
    }
  },
  _updateFollowButton: function() {
    var btn = this.$el.find('.piko-follow-toggle');
    if (this.autoFollow) {
      btn.removeClass('btn-outline-info').addClass('btn-info');
      btn.html('<i class="bi bi-arrow-down-circle-fill"></i> Following');
    } else {
      btn.removeClass('btn-info').addClass('btn-outline-info');
      btn.html('<i class="bi bi-arrow-down-circle"></i> Follow');
    }
  },
  _startElapsedTimers: function() {
    var that = this;
    if (this._elapsedInterval) clearInterval(this._elapsedInterval);
    var update = function() {
      that.$el.find('.piko-elapsed').each(function() {
        var started = $(this).data('started');
        if (!started) return;
        var elapsed = Math.floor((Date.now() - new Date(started).getTime()) / 1000);
        var h = Math.floor(elapsed / 3600);
        var m = Math.floor((elapsed % 3600) / 60);
        var s = elapsed % 60;
        var text = '';
        if (h > 0) text += h + 'h ';
        if (m > 0 || h > 0) text += m + 'm ';
        text += s + 's';
        $(this).text('(' + text + ')');
      });
      that.$el.find('.piko-time-ago').each(function() {
        var t = $(this).data('time');
        if (!t) return;
        $(this).text(pikoTimeAgo(t));
      });
    };
    update();
    this._elapsedInterval = setInterval(update, 1000);
  },
  _setupScrollListeners: function() {
    var that = this;
    this.$el.find('.piko-step-row-body pre').each(function() {
      var pre = this;
      $(pre).off('scroll.autofollow').on('scroll.autofollow', function() {
        if (that._isAutoScrolling) return;
        var atBottom = (pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 20);
        if (atBottom) {
          that.autoFollow = true;
        } else {
          that.autoFollow = false;
        }
        that._updateFollowButton();
        var gotoBtn = $(pre).closest('.piko-step-row-body').find('.piko-goto-bottom-btn');
        if (pre.scrollHeight > pre.clientHeight && !atBottom) {
          gotoBtn.addClass('visible');
        } else {
          gotoBtn.removeClass('visible');
        }
      });
    });
  },
  render: function () {
    var data = this.model.toJSON();
    data.active = this.model.get("active");
    if (data.active) {
      this.$el.removeClass("d-none");
    } else {
      this.$el.addClass("d-none");
    }
    var expandedSteps = {};
    this.$el.find('.piko-step-row-body').each(function(i) {
      if ($(this).css('display') !== 'none') {
        expandedSteps[i] = true;
      }
    });
    this.$el.html(this.template(data));
    if (Object.keys(expandedSteps).length > 0) {
      this.$el.find('.piko-step-row-body').each(function(i) {
        if (expandedSteps[i]) {
          this.style.display = 'block';
        }
      });
    }
    this._setupScrollListeners();
    if (this.autoFollow) {
      var runningPre = this.$el.find('.piko-step-row[data-status="started"] .piko-step-row-body pre');
      if (!runningPre.length) {
        runningPre = this.$el.find('.piko-step-row-body:visible pre').last();
      }
      if (runningPre.length) {
        var that = this;
        var el = runningPre[0];
        this._isAutoScrolling = true;
        requestAnimationFrame(function() {
          el.scrollTop = el.scrollHeight;
          requestAnimationFrame(function() { that._isAutoScrolling = false; });
        });
      }
    }
    this._updateFollowButton();
    this._startElapsedTimers();
    return this;
  },
  remove: function() {
    if (this._elapsedInterval) clearInterval(this._elapsedInterval);
    Backbone.View.prototype.remove.call(this);
  },
});
