'use strict';

import { session } from '../collections.js';
import { pikoTimeAgo } from '../namespace.js';

export var WorkersListView = Backbone.View.extend({
  template: _.template($('#workers-list-view').html()),
  initialize: function() {
    this.listenTo(this.collection, "add", this.addWorker);
    this.listenTo(this.collection, "reset", this.render);
    this.collection.fetch();
  },
  render: function() {
    this.$el.html(this.template());
    var that = this;
    this.collection.each(function(m) {
      that.addWorker(m);
    });
    return this;
  },
  addWorker: function(m) {
    var view = new WorkersRowView({model: m, collection: this.collection});
    this.$('#workers-table-body').append(view.render().el);
  },
});

var WorkersRowView = Backbone.View.extend({
  template: _.template($('#workers-row-view').html()),
  tagName: "tr",
  events: {
    'click .delete-worker': 'deleteWorker',
  },
  render: function() {
    var data = this.model.toJSON();
    data.uptime = pikoTimeAgo(data.started_at);
    data.last_seen = pikoTimeAgo(data.last_ping_at);
    this.$el.html(this.template(data));
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
