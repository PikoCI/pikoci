'use strict';

import { Session, ApiNotice, User, Team, Pipeline, PipelineImage, Job, Build, Resource, ResourceVersion, TeamMembers } from './models.js';

export var Users = Backbone.Collection.extend({
  model: User,
  url: "/users",
  parse: function(response) {
    return response.data;
  }
});

export var Teams = Backbone.Collection.extend({
  model: Team,
  url: "/teams",
  parse: function(response) {
    return response.data;
  }
});

export var Pipelines = Backbone.Collection.extend({
  model: Pipeline,
  url: function() {
    return this.team.url()+"/pipelines";
  },
  initialize: function(attr, opts) {
    this.team = opts.team;
  },
  parse: function(response) {
    return response.data;
  }
});

export var Jobs = Backbone.Collection.extend({
  model: Job,
  url: function() {
    return this.pipeline.url() + "/jobs";
  },
  initialize: function(attr, opts) {
    this.pipeline = opts.pipeline;
  },
  parse: function(response) {
    return response.data;
  }
});

export var Resources = Backbone.Collection.extend({
  model: Resource,
  url: function() {
    return this.pipeline.url() + "/resources";
  },
  initialize: function(attr, opts) {
    this.pipeline = opts.pipeline;
  },
  parse: function(response) {
    return response.data;
  }
});

export var Builds = Backbone.Collection.extend({
  model: Build,
  url: function() {
    return this.job.url() + "/builds";
  },
  comparator: function(a, b) {
    var pa = a.get("build_number").split(".");
    var pb = b.get("build_number").split(".");
    var mainA = parseInt(pa[0], 10);
    var mainB = parseInt(pb[0], 10);
    if (mainA !== mainB) return mainB - mainA;
    var subA = pa.length > 1 ? parseInt(pa[1], 10) : -1;
    var subB = pb.length > 1 ? parseInt(pb[1], 10) : -1;
    return subB - subA;
  },
  initialize: function(attr, opts) {
    this.job = opts.job;
    this.newestID = 0;
    this.oldestID = 0;
    this.hasMore = false;
    this.loadingMore = false;
  },
  parse: function(response) {
    if (response.meta) {
      this.hasMore = response.meta.has_more;
      if (!this.newestID || response.meta.newest_id > this.newestID) {
        this.newestID = response.meta.newest_id;
      }
      if (!this.oldestID || response.meta.oldest_id < this.oldestID) {
        this.oldestID = response.meta.oldest_id;
      }
    }
    return response.data;
  },
  fetchMore: function() {
    if (this.loadingMore || !this.hasMore) return;
    this.loadingMore = true;
    var that = this;
    this.fetch({
      remove: false,
      data: { before: this.oldestID, limit: 50 },
      success: function() { that.loadingMore = false; },
      error: function() { that.loadingMore = false; },
    });
  },
  fetchNew: function(opts) {
    opts = opts || {};
    if (!this.newestID) {
      this.fetch(_.extend({ remove: false }, opts));
      return;
    }
    this.fetch(_.extend({
      remove: false,
      data: { after: this.newestID },
    }, opts));
  },
  setActive: function(id) {
    if (!id && this.first()) {
      id = this.first().get("build_number");
    }
    this.each(function(m){
      m.set("active", m.get("build_number") === id);
    });
    return id;
  },
});

export var ResourceVersions = Backbone.Collection.extend({
  model: ResourceVersion,
  url: function() {
    return this.resource.url() + "/versions";
  },
  comparator: function(a, b) {
    return (b.get("id") || 0) - (a.get("id") || 0);
  },
  initialize: function(attr, opts) {
    this.resource = opts.resource;
    this.newestID = 0;
    this.oldestID = 0;
    this.hasMore = false;
    this.loadingMore = false;
  },
  parse: function(response) {
    if (response.meta) {
      this.hasMore = response.meta.has_more;
      if (!this.newestID || response.meta.newest_id > this.newestID) {
        this.newestID = response.meta.newest_id;
      }
      if (!this.oldestID || response.meta.oldest_id < this.oldestID) {
        this.oldestID = response.meta.oldest_id;
      }
    }
    return response.data;
  },
  fetchMore: function() {
    if (this.loadingMore || !this.hasMore) return;
    this.loadingMore = true;
    var that = this;
    this.fetch({
      remove: false,
      data: { before: this.oldestID, limit: 50 },
      success: function() { that.loadingMore = false; },
      error: function() { that.loadingMore = false; },
    });
  },
  fetchNew: function() {
    if (!this.newestID) {
      this.fetch({ remove: false });
      return;
    }
    this.fetch({
      remove: false,
      data: { after: this.newestID },
    });
  },
});

// Singletons
export var userSessionKey = "piko-user-jwt";
export var session = new Session(JSON.parse(window.localStorage.getItem(userSessionKey) || "{}"));
export var teams = new Teams();
export var apiNotice = new ApiNotice();

// Expose on window.app for HTML templates
window.app.session = session;
window.app.teams = teams;
window.app.apiNotice = apiNotice;
