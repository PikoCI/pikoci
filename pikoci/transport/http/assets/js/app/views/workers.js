'use strict';

import { session } from '../collections.js';
import { pikoTimeAgo } from '../namespace.js';

export var WorkersListView = Backbone.View.extend({
  template: _.template($('#workers-list-view').html()),
  initialize: function() {
    this.serverVersion = '';
    this.serverCommit = '';
    var self = this;
    $.ajax({
      url: '/version',
      type: 'GET',
      headers: {'Content-Type': 'application/json'},
      dataType: 'json',
    }).done(function(data) {
      self.serverVersion = data.version;
      self.serverCommit = data.commit;
      self.render();
    });
    this.listenTo(this.collection, "add", this.addWorker);
    this.listenTo(this.collection, "reset", this.render);
    this.collection.fetch();
  },
  render: function() {
    this.$el.html(this.template());
    var that = this;
    var hasOutdated = false;
    this.collection.each(function(m) {
      var data = m.toJSON();
      if (that.serverVersion && (data.version !== that.serverVersion || data.commit !== that.serverCommit)) {
        hasOutdated = true;
      }
      that.addWorker(m);
    });
    if (hasOutdated) {
      this.$('#worker-health-banner-container').html(
        '<div class="piko-worker-banner mb-3">' +
        '\u26A0 Some workers are running an outdated version. Restart them to pick up the latest release.' +
        '</div>'
      );
    }
    return this;
  },
  addWorker: function(m) {
    var view = new WorkersRowView({model: m, collection: this.collection, serverVersion: this.serverVersion, serverCommit: this.serverCommit});
    this.$('#workers-table-body').append(view.render().el);
  },
});

var WorkersRowView = Backbone.View.extend({
  template: _.template($('#workers-row-view').html()),
  tagName: "tr",
  events: {
    'click .delete-worker': 'deleteWorker',
  },
  initialize: function(options) {
    this.serverVersion = options.serverVersion || '';
    this.serverCommit = options.serverCommit || '';
  },
  render: function() {
    var data = this.model.toJSON();
    data.uptime = pikoTimeAgo(data.started_at);
    data.last_seen = pikoTimeAgo(data.last_ping_at);
    data.server_version = this.serverVersion;
    data.server_commit = this.serverCommit;
    data.is_outdated = this.serverVersion && (data.version !== this.serverVersion || data.commit !== this.serverCommit);
    this.$el.html(this.template(data));
    this.$('[data-bs-toggle="tooltip"]').each(function() {
      new bootstrap.Tooltip(this);
    });
    return this;
  },
  deleteWorker: function(e) {
    e.preventDefault();
    var name = $(e.currentTarget).data('name');
    var that = this;
    $.ajax({
      url: '/workers/' + encodeURIComponent(name),
      type: 'DELETE',
      headers: {
        'Authorization': 'Bearer ' + session.get("jwt"),
        'Content-Type': 'application/json',
      },
      success: function() {
        that.model.collection.remove(that.model);
        that.remove();
      },
    });
  },
});
