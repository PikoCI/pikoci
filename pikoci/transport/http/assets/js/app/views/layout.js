'use strict';

import { session, apiNotice } from '../collections.js';
import { syncThemeSwitch } from '../namespace.js';

export var MainView = Backbone.View.extend({
  template: _.template($('#main-view').html()),
  initialize: function () {
    this.render();
    this.header = new HeaderView({ el: 'header' });
    this.notice = new NoticeView({ el: '#notice', model: apiNotice });
  },
  render: function () {
    this.$el.html(this.template());
    return this;
  }
});

export var HeaderView = Backbone.View.extend({
  template: _.template($('#header-view').html()),
  initialize: function() {
    this.listenTo(session, "change", this.render);
    this.versionText = '';
    var self = this;
    $.getJSON('/version.json').done(function(data) {
      self.versionText = data.version + ' (' + data.commit + ')';
      self.render();
    });
    this.render();
  },
  events: {
    'click a#logo': 'clickLogo',
    'click a#logout': 'clickLogout',
    'click a#nav-profile': 'clickNavLink',
    'click a#nav-users': 'clickNavLink',
  },
  clickLogo: function(event) {
    event.preventDefault();
    if (event.target.tagName === "IMG") {
      event.target = event.target.parentElement;
    }
    var url = new URL(event.target.href);
    window.app.router.navigate(url.pathname, { trigger: true });
  },
  clickLogout: function(event) {
    event.preventDefault();
    var url = new URL(event.target.href);
    window.app.router.navigate(url.pathname, { trigger: true });
  },
  clickNavLink: function(event) {
    event.preventDefault();
    var target = event.target.closest('a');
    var url = new URL(target.href);
    window.app.router.navigate(url.pathname, { trigger: true });
  },
  render: function () {
    var sjson;
    if (!session.isEmpty()) {
      sjson = session.toJSON();
    }
    this.$el.html(this.template({session: sjson}));
    if (this.versionText) {
      this.$('#app-version').text(this.versionText);
    }
    syncThemeSwitch();
    return this;
  }
});

export var NoticeView = Backbone.View.extend({
  template: _.template($('#notice-view').html()),
  initialize: function() {
    this.listenTo(this.model, "change", this.render);
  },
  render: function() {
    this.$el.html(this.template(this.model.toJSON()));
    return this;
  },
});

export var BreadcrumbView = Backbone.View.extend({
  tagName: "nav",
  template: _.template($('#breadcrumb-view').html()),
  events: {
    'click a': 'clickLink',
  },
  initialize: function(opts) {
    opts = opts||{};
    this.team = opts.team;
    this.pipeline = opts.pipeline;
    this.job = opts.job;
    this.resource = opts.resource;
    this.showPipelines = opts.showPipelines;
  },
  render: function() {
    this.$el.html(this.template({
      team: this.team,
      pipeline: this.pipeline,
      job: this.job,
      resource: this.resource,
      showPipelines: this.showPipelines,
    }));
    return this;
  },
  clickLink: function(event) {
    event.preventDefault();
    var url = new URL(event.target.href);
    window.app.router.navigate(url.pathname, { trigger: true });
  },
});
