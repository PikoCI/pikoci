'use strict';

import { session } from '../collections.js';
import { addSessionFunctions, fetchInterval, withLoading } from '../namespace.js';

export var ResourceVersionsView = Backbone.View.extend({
  template: _.template($('#resource-versions-view').html()),
  events: {
    'click #trigger-resource': 'clickTriggerResource',
    'click #toggle-webhook-panel': 'clickToggleWebhookPanel',
    'click #copy-webhook': 'clickCopyWebhook',
    'click #regenerate-webhook': 'clickRegenerateWebhook',
    'click #unpin-banner': 'clickUnpinBanner',
  },
  initialize: function() {
    var that = this;
    this.listenTo(this.collection, "add", function(m) { that.addVersion(m); });
    this.listenTo(this.collection, "reset", this.resetVersions);
    this.listenTo(this.model, "change", this.render);
    this.listenTo(this.collection.resource, "change:pinned_version_id", this.renderPinnedBanner);

    this.collection.fetch({
      reset: true,
      success: function() { that.bindScrollListener(); },
    });

    this.intervalID = window.setInterval(function() {
      that.model.fetch({isInterval: true});
      that.collection.fetch({remove: false});
    }, fetchInterval);
  },
  render: function () {
    var webhookPanelOpen = this.$('#webhook-panel').is(':visible');
    var expandedVersions = {};
    this.$el.find('.piko-version-row-body').each(function(i) {
      if ($(this).css('display') !== 'none') {
        expandedVersions[i] = true;
      }
    });
    this.$el.html(this.template(addSessionFunctions({
      team: this.collection.resource.collection.pipeline.collection.team.toJSON(),
      pipeline: this.collection.resource.collection.pipeline.toJSON(),
      resource: this.collection.resource.toJSON(),
    })));
    if (webhookPanelOpen) {
      var token = this.collection.resource.get('webhook_token');
      var url = window.location.origin + '/webhooks/' + token + '';
      this.$('#webhook-url').text(url);
      this.$('#webhook-panel').show();
    }
    this.resetVersions();
    this.renderPinnedBanner();
    if (Object.keys(expandedVersions).length > 0) {
      this.$el.find('.piko-version-row-body').each(function(i) {
        if (expandedVersions[i]) {
          this.style.display = 'block';
        }
      });
    }
    return this;
  },
  resetVersions: function() {
    this.$('#resource-versions').empty();
    this.collection.each(function(m) {
      this.addVersion(m, true);
    }, this);
  },
  renderPinnedBanner: function() {
    var banner = this.$('#pinned-version-banner');
    var pinnedID = this.collection.resource.get('pinned_version_id');
    if (!pinnedID) {
      banner.empty();
      return;
    }
    var pinnedModel = this.collection.findWhere({id: pinnedID});
    var versionKV = '';
    if (pinnedModel) {
      _.each(pinnedModel.get('version'), function(v, k) {
        versionKV += k + ': ' + v + '  ';
      });
    }
    var isMember = session.isMember(this.collection.resource.collection.pipeline.collection.team.get('canonical'));
    var html = '<div class="piko-pinned-banner">' +
      '<span><i class="bi bi-pin-fill"></i> Pinned to version #' + pinnedID +
      (versionKV ? ' — <span class="piko-version-kv">' + _.escape(versionKV.trim()) + '</span>' : '') +
      '</span>' +
      (isMember ? '<button id="unpin-banner" class="btn btn-sm btn-outline-warning">Unpin</button>' : '') +
      '</div>';
    banner.html(html);
  },
  clickUnpinBanner: function(event) {
    event.preventDefault();
    var rs = this.collection.resource;
    var $btn = $(event.currentTarget);
    $btn.attr('data-loading-text', 'Unpinning...');
    withLoading($btn, function() {
      return rs.unpinVersion({
        headers: { 'Authorization': 'Bearer ' + session.get('jwt') },
        success: function() {
          rs.set('pinned_version_id', null);
          window.app.apiNotice.setSuccess("Resource unpinned");
        },
      });
    });
  },
  addVersion: function(m, isReset) {
    var isFirst = $('#resource-versions').children().length === 0;
    var ver = new ResourceVersionView({model: m, isFirst: isFirst, resource: this.collection.resource});
    if (isReset) {
      $('#resource-versions').append(ver.render().el);
    } else {
      var idx = this.collection.indexOf(m);
      var children = $('#resource-versions').children();
      if (idx === 0 || children.length === 0) {
        // Remove "latest" badge from previous first version
        children.first().find('.piko-badge-succeeded').filter(function() {
          return $(this).text().trim() === 'latest';
        }).remove();
        ver.isFirst = true;
        $('#resource-versions').prepend(ver.render().el);
      } else if (idx < children.length) {
        $(children[idx]).before(ver.render().el);
      } else {
        $('#resource-versions').append(ver.render().el);
      }
    }
  },
  bindScrollListener: function() {
    var that = this;
    this._scrollHandler = function() {
      if ($(window).scrollTop() + $(window).height() >= $(document).height() - 200) {
        that.collection.fetchMore();
      }
    };
    $(window).on('scroll', this._scrollHandler);
  },
  remove: function() {
    clearInterval(this.intervalID);
    $(window).off('scroll', this._scrollHandler);
    Backbone.View.prototype.remove.call(this);
  },
  clickTriggerResource: function(event) {
    event.preventDefault();
    var rs = this.collection.resource;
    withLoading(this.$('#trigger-resource'), function() {
      return rs.fetchTrigger({success: function() { window.app.apiNotice.setSuccess("Resource check triggered"); }});
    });
  },
  clickToggleWebhookPanel: function(event) {
    event.preventDefault();
    var panel = this.$('#webhook-panel');
    if (panel.is(':visible')) {
      panel.hide();
    } else {
      var token = this.collection.resource.get('webhook_token');
      var url = window.location.origin + '/webhooks/' + token + '';
      this.$('#webhook-url').text(url);
      panel.show();
    }
  },
  clickCopyWebhook: function(event) {
    event.preventDefault();
    var url = this.$('#webhook-url').text();
    navigator.clipboard.writeText(url);
  },
  clickRegenerateWebhook: function(event) {
    event.preventDefault();
    var that = this;
    var rs = this.collection.resource;
    var tc = rs.collection.pipeline.collection.team.get('canonical');
    var pn = rs.collection.pipeline.get('canonical');
    var rCan = rs.get('canonical');
    withLoading(this.$('#regenerate-webhook'), function() {
      return $.ajax({
        url: '/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/webhook_token',
        type: 'POST',
        contentType: 'application/json',
        headers: { 'Authorization': 'Bearer ' + session.get('jwt') },
        success: function(resp) {
          if (resp.token) {
            rs.set('webhook_token', resp.token);
            var url = window.location.origin + '/webhooks/' + resp.token + '';
            that.$('#webhook-url').text(url);
            window.app.apiNotice.setSuccess("Webhook token regenerated");
          }
        }
      });
    });
  },
});

var ResourceVersionView = Backbone.View.extend({
  template: _.template($('#resource-version-view').html()),
  events: {
    'click .track-version': 'clickTrackVersion',
    'click .trigger-version': 'clickTriggerVersion',
    'click .pin-version': 'clickPinVersion',
  },
  initialize: function(opts) {
    this.isFirst = opts.isFirst || false;
    this.resource = opts.resource;
    this.listenTo(this.resource, 'change:pinned_version_id', this.render);
    this.listenTo(this.model, 'change:status', this.updateStatusDot);
  },
  updateStatusDot: function() {
    var status = this.model.get('status');
    var dot = this.$('.piko-version-status-dot');
    if (dot.length && status) {
      dot.css('background', 'var(--status-' + status + ')');
      dot.attr('title', status);
    }
  },
  render: function () {
    var data = this.model.toJSON();
    data.isFirst = this.isFirst;
    data.pinned_version_id = this.resource.get('pinned_version_id');
    // Read tracked version from URL query param (not the router property, which is transient)
    var urlParams = new URLSearchParams(window.location.search);
    data.tracked_version_id = urlParams.get('version') ? parseInt(urlParams.get('version'), 10) : null;
    data.isMember = session.isMember(this.resource.collection.pipeline.collection.team.get('canonical'));
    this.$el.html(this.template(data));
    return this;
  },
  clickTrackVersion: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var versionID = this.model.get('id');
    var rs = this.resource;
    var tc = rs.collection.pipeline.collection.team.get('canonical');
    var pn = rs.collection.pipeline.get('canonical');
    window.app.router.navigate('teams/' + tc + '/pipelines/' + pn + '?version=' + versionID, { trigger: true });
  },
  clickTriggerVersion: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var versionID = this.model.get('id');
    var rs = this.resource;
    var tc = rs.collection.pipeline.collection.team.get('canonical');
    var pn = rs.collection.pipeline.get('canonical');
    var rCan = rs.get('canonical');
    withLoading($(event.currentTarget), function() {
      return $.ajax({
        url: '/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/versions/' + versionID + '/trigger',
        type: 'POST',
        contentType: 'application/json',
        headers: { 'Authorization': 'Bearer ' + session.get('jwt') },
        success: function() {
          window.app.apiNotice.setSuccess("Triggered downstream jobs with version #" + versionID);
        },
      });
    });
  },
  clickPinVersion: function(event) {
    event.preventDefault();
    event.stopPropagation();
    var versionID = this.model.get('id');
    var rs = this.resource;
    var isPinned = rs.get('pinned_version_id') === versionID;
    var $btn = $(event.currentTarget);
    $btn.attr('data-loading-text', isPinned ? 'Unpinning...' : 'Pinning...');
    withLoading($btn, function() {
      if (isPinned) {
        return rs.unpinVersion({
          headers: { 'Authorization': 'Bearer ' + session.get('jwt') },
          success: function() {
            rs.set('pinned_version_id', null);
            window.app.apiNotice.setSuccess("Resource unpinned");
          },
        });
      } else {
        return rs.pinVersion(versionID, {
          headers: { 'Authorization': 'Bearer ' + session.get('jwt') },
          success: function() {
            rs.set('pinned_version_id', versionID);
            window.app.apiNotice.setSuccess("Resource pinned to version #" + versionID);
          },
        });
      }
    });
  },
});
