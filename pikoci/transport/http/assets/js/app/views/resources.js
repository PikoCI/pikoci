'use strict';

import { session } from '../collections.js';
import { addSessionFunctions, fetchInterval } from '../namespace.js';

export var ResourceVersionsView = Backbone.View.extend({
  template: _.template($('#resource-versions-view').html()),
  events: {
    'click #trigger-resource': 'clickTriggerResource',
    'click #toggle-webhook-panel': 'clickToggleWebhookPanel',
    'click #copy-webhook': 'clickCopyWebhook',
    'click #regenerate-webhook': 'clickRegenerateWebhook',
  },
  initialize: function() {
    this.listenTo(this.collection, "add", this.addVersion);
    this.listenTo(this.collection, "reset", this.resetVersions);
    this.listenTo(this.model, "change", this.render);

    var that = this;
    this.collection.fetch({
      reset: true,
      success: function() { that.bindScrollListener(); },
    });

    this.intervalID = window.setInterval(function() {
      that.model.fetch({isInterval: true});
      that.collection.fetchNew();
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
      var url = window.location.origin + '/webhooks/' + token + '.json';
      this.$('#webhook-url').text(url);
      this.$('#webhook-panel').show();
    }
    this.resetVersions();
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
  addVersion: function(m, isReset) {
    var isFirst = $('#resource-versions').children().length === 0;
    var ver = new ResourceVersionView({model: m, isFirst: isFirst});
    if (isReset) {
      $('#resource-versions').append(ver.render().el);
    } else {
      var idx = this.collection.indexOf(m);
      var children = $('#resource-versions').children();
      if (idx === 0 || children.length === 0) {
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
    this.collection.resource.fetchTrigger();
  },
  clickToggleWebhookPanel: function(event) {
    event.preventDefault();
    var panel = this.$('#webhook-panel');
    if (panel.is(':visible')) {
      panel.hide();
    } else {
      var token = this.collection.resource.get('webhook_token');
      var url = window.location.origin + '/webhooks/' + token + '.json';
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
    $.ajax({
      url: '/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/webhook_token.json',
      type: 'POST',
      contentType: 'application/json',
      headers: { 'Authorization': 'Bearer ' + session.get('jwt') },
      success: function(resp) {
        if (resp.token) {
          rs.set('webhook_token', resp.token);
          var url = window.location.origin + '/webhooks/' + resp.token + '.json';
          that.$('#webhook-url').text(url);
        }
      }
    });
  },
});

var ResourceVersionView = Backbone.View.extend({
  template: _.template($('#resource-version-view').html()),
  initialize: function(opts) {
    this.isFirst = opts.isFirst || false;
  },
  render: function () {
    var data = this.model.toJSON();
    data.isFirst = this.isFirst;
    this.$el.html(this.template(data));
    return this;
  },
});
