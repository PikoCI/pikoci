'use strict';

import { session } from '../collections.js';
import { Build } from '../models.js';
import { addSessionFunctions, fetchInterval, pikoTimeAgo, withLoading } from '../namespace.js';

export var JobBuildsView = Backbone.View.extend({
  template: _.template($('#job-builds-view').html()),
  initialize: function(opts) {
    this.listenTo(this.collection, "reset", this.addBuilds);
    this.listenTo(this.collection, "add", this.addBuild);

    this.job = opts.job;
    this.pipeline = opts.pipeline;
    this.embedded = opts.embedded || false;
    this.trackedVersionID = opts.trackedVersionID || null;

    var that = this;
    if (this.trackedVersionID) {
      // When tracking a version, fetch the version path to find the correct
      // build IDs for this job, then filter to only show those
      this._fetchTrackedBuilds(function() {
        that.collection.fetch({
          reset: true,
          success: function() {
            that._filterByTrackedBuildIDs();
            that.activateAndNavigate(opts.currentBuildID);
            that.bindScrollListener();
          },
        });
      });
    } else {
      this.collection.fetch({
        reset: true,
        success: function(){
          that.activateAndNavigate(opts.currentBuildID);
          that.bindScrollListener();
        },
      });
    }

    this.intervalID = window.setInterval(function() {
      if (that.trackedVersionID) {
        // Re-fetch tracked build IDs to pick up retries, then refresh builds
        that._fetchTrackedBuilds(function() {
          that.collection.fetchNew({
            success: function() {
              that._filterByTrackedBuildIDs();
              that.fetchActiveBuild();
            }
          });
        });
      } else {
        that.collection.fetchNew({
          success: function() { that.fetchActiveBuild(); }
        });
      }
    }, fetchInterval);
  },
  events: {
    'click #trigger-job': 'clickTriggerJob',
    'click #pause-job': 'clickPauseJob',
    'click #unpause-job': 'clickUnpauseJob',
    'click .piko-build-tab': 'clickOnTab',
    'click #job-version-back': 'clickBackToPipeline',
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
    // Show version tracking banner if tracking (not when embedded in list view)
    if (this.trackedVersionID && !this.embedded) {
      this._showVersionBanner();
    }
    return this;
  },
  _showVersionBanner: function() {
    var banner = this.$('#job-version-banner');
    if (!banner.length) return;
    // Fetch version path to get resource name and ref
    var that = this;
    var tc = this.collection.job.collection.pipeline.collection.team.get('canonical');
    var pc = this.pipeline.get('canonical');
    var resources = this.pipeline.get('resources') || [];
    var idx = 0;
    function tryNext() {
      if (idx >= resources.length) return;
      var rCan = resources[idx].canonical;
      idx++;
      var url = '/teams/' + tc + '/pipelines/' + pc + '/resources/' + rCan + '/versions/' + that.trackedVersionID + '/path';
      $.ajax({
        url: url, type: 'GET', contentType: 'application/json',
        headers: session.isEmpty() ? {} : { 'Authorization': 'Bearer ' + session.get('jwt') },
        success: function(resp) {
          if (resp.data && resp.data.path && resp.data.path.length > 0) {
            var v = resp.data.resource.version || {};
            that.$('#job-version-banner-resource').text(resp.data.resource.canonical);
            that.$('#job-version-banner-ref').text(v.ref || v.digest || v.tag || (function() {
              for (var k in v) { if (v.hasOwnProperty(k)) return k + ': ' + v[k]; }
              return '';
            })());
            banner.show();
          } else {
            tryNext();
          }
        },
        error: function() { tryNext(); },
      });
    }
    tryNext();
  },
  clickBackToPipeline: function(event) {
    event.preventDefault();
    var tc = this.collection.job.collection.pipeline.collection.team.get('canonical');
    var pc = this.pipeline.get('canonical');
    if (this.trackedVersionID) {
      window.app.router._trackedVersionID = this.trackedVersionID;
    }
    window.app.router.navigate('teams/' + tc + '/pipelines/' + pc, { trigger: true });
    if (this.trackedVersionID) {
      window.history.replaceState(null, '', '/teams/' + tc + '/pipelines/' + pc + '?version=' + this.trackedVersionID);
    }
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
    if (!this.embedded) {
      var tc = this.collection.job.collection.pipeline.collection.team.get("canonical");
      var navPath = "teams/"+tc+"/pipelines/"+this.pipeline.get("canonical")+"/jobs/"+this.job.get("name")+"/builds/"+bid;
      window.app.router.navigate(navPath, { trigger: false, replace: true });
      // Preserve ?version= in URL via history API
      if (this.trackedVersionID) {
        window.history.replaceState(null, '', window.location.pathname + '?version=' + this.trackedVersionID);
      }
    }
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
    // Preserve tracked version for the next view (e.g., navigating back to pipeline)
    if (this.trackedVersionID) {
      window.app.router._trackedVersionID = this.trackedVersionID;
    }
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
  fetchActiveBuild: function() {
    var active = this.collection.find(function(m) { return m.get("active"); });
    // Fetch the active build if it's in a non-terminal state
    if (active) {
      var status = active.get("status");
      if (status !== "succeeded" && status !== "failed" && status !== "cancelled") {
        active.fetch();
      }
    }
    // Also fetch any other non-terminal builds so their tabs update
    this.collection.each(function(m) {
      if (m === active) return;
      var s = m.get("status");
      if (s === "started" || s === "pending") {
        m.fetch();
      }
    });
  },
  _fetchTrackedBuilds: function(callback) {
    // Find which resource has the tracked version and get path data
    var that = this;
    var tc = this.collection.job.collection.pipeline.collection.team.get('canonical');
    var pn = this.pipeline.get('canonical');
    var jn = this.job.get('name');
    var resources = this.pipeline.get('resources') || [];
    this._trackedBuildIDs = null;
    // Try the pipeline resources endpoint to find path
    var url = '/teams/' + tc + '/pipelines/' + pn + '/resources';
    $.ajax({
      url: url, type: 'GET', contentType: 'application/json',
      headers: session.isEmpty() ? {} : { 'Authorization': 'Bearer ' + session.get('jwt') },
      success: function(resp) {
        if (!resp || !resp.data) { callback(); return; }
        var resList = resp.data;
        var idx = 0;
        function tryNextResource() {
          if (idx >= resList.length) { callback(); return; }
          var rCan = resList[idx].canonical;
          idx++;
          var pathUrl = '/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/versions/' + that.trackedVersionID + '/path';
          $.ajax({
            url: pathUrl, type: 'GET', contentType: 'application/json',
            headers: session.isEmpty() ? {} : { 'Authorization': 'Bearer ' + session.get('jwt') },
            success: function(pathResp) {
              if (pathResp.data && pathResp.data.path && pathResp.data.path.length > 0) {
                // Found the right resource - extract build IDs for this job
                var ids = {};
                for (var i = 0; i < pathResp.data.path.length; i++) {
                  var entry = pathResp.data.path[i];
                  if (entry.job_name === jn && entry.build) {
                    ids[entry.build.id] = true;
                    if (entry.retries) {
                      for (var j = 0; j < entry.retries.length; j++) {
                        ids[entry.retries[j].id] = true;
                      }
                    }
                  }
                }
                that._trackedBuildIDs = ids;
                callback();
              } else {
                tryNextResource();
              }
            },
            error: function() { tryNextResource(); },
          });
        }
        tryNextResource();
      },
      error: function() { callback(); },
    });
  },
  _filterByTrackedBuildIDs: function() {
    if (!this._trackedBuildIDs) return;
    var ids = this._trackedBuildIDs;
    var toRemove = this.collection.filter(function(m) {
      return !ids[m.get('id')];
    });
    // Only re-render if the set of builds actually changed
    if (toRemove.length === 0 && this._lastFilteredCount === this.collection.length) return;
    this._lastFilteredCount = this.collection.length - toRemove.length;
    this.collection.remove(toRemove, {silent: true});
    this.$('#builds-tabs').empty();
    this.$('#builds-content').empty();
    this.addBuilds();
  },
  clickTriggerJob: function(event) {
    event.preventDefault();
    var that = this;
    withLoading(this.$('#trigger-job'), function() {
      return that.collection.job.fetchTrigger({success: function() { window.app.apiNotice.setSuccess("Job triggered"); }});
    });
  },
  clickPauseJob: function(event) {
    event.preventDefault();
    var that = this;
    var url = this.collection.job.url() + "/pause";
    withLoading(this.$('#pause-job'), function() {
      return $.ajax({ url: url, type: "POST", contentType: "application/json", headers: { "Authorization": "Bearer " + session.get("jwt") },
        success: function() {
          window.app.apiNotice.setSuccess("Job paused");
          that.collection.job.fetch({ success: function() { that.render(); that.addBuilds(); that.collection.setActive(that.currentBuildID); } });
        },
      });
    });
  },
  clickUnpauseJob: function(event) {
    event.preventDefault();
    var that = this;
    var url = this.collection.job.url() + "/unpause";
    withLoading(this.$('#unpause-job'), function() {
      return $.ajax({ url: url, type: "POST", contentType: "application/json", headers: { "Authorization": "Bearer " + session.get("jwt") },
        success: function() {
          window.app.apiNotice.setSuccess("Job unpaused");
          that.collection.job.fetch({ success: function() { that.render(); that.addBuilds(); that.collection.setActive(that.currentBuildID); } });
        },
      });
    });
  },
  clickOnTab: function(event) {
    event.preventDefault();
    var el = event.currentTarget || event.target;
    var bid = el.id.split("-")[1];
    this.collection.setActive(bid);
    this.currentBuildID = bid;
    if (!this.embedded) {
      var tc = this.collection.job.collection.pipeline.collection.team.get("canonical");
      var navPath = "teams/"+tc+"/pipelines/"+this.pipeline.get("canonical")+"/jobs/"+this.job.get("name")+"/builds/"+bid;
      window.app.router.navigate(navPath, { trigger: false, replace: true });
      if (this.trackedVersionID) {
        window.history.replaceState(null, '', window.location.pathname + '?version=' + this.trackedVersionID);
      }
    }
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
    var url = this.model.collection.url() + "/" + bid + "/cancel";
    withLoading($(e.currentTarget), function() {
      return $.ajax({ url: url, type: "POST", contentType: "application/json", headers: { "Authorization": "Bearer " + session.get("jwt") },
        success: function() { window.app.apiNotice.setSuccess("Build cancelled"); that.model.fetch(); },
      });
    });
  },
  retryBuild: function(e) {
    e.preventDefault();
    var bid = this.model.get("build_number");
    var collection = this.model.collection;
    var url = collection.url() + "/" + bid + "/retry";
    withLoading($(e.currentTarget), function() {
      return $.ajax({ url: url, type: "POST", contentType: "application/json", headers: { "Authorization": "Bearer " + session.get("jwt") },
        success: function() { window.app.apiNotice.setSuccess("Build retried"); collection.fetchNew(); },
      });
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
