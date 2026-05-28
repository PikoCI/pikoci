'use strict';

export var Session = Backbone.Model.extend({
  url: "login",
  parse: function(response) {
    if (response.data){
      return response.data;
    }
    return response;
  },
  isEmpty: function() {
    return !this.get("jwt");
  },
  isAdmin: function(tc) {
    var u = this.get("user");
    if (!u) return false;
    if (u.admin){
      return true;
    }
    var admin = false;
    if (tc !== "") {
      _.each(u.memberships, function(m) {
        if (m.admin && m.team_canonical === tc) {
          admin = true;
        }
      });
    }
    return admin;
  },
  isMember: function(tc) {
    var u = this.get("user");
    if (!u) return false;
    if (u.admin) {
      return true;
    }
    if (tc === "") {
      return true;
    }
    var member = false;
    _.each(u.memberships, function(m) {
      if (m.team_canonical === tc) {
        member = true;
      }
    });
    return member;
  },
  data: function() {
    return {
      isAdmin: this.isAdmin.bind(this),
      isMember: this.isMember.bind(this),
    };
  },
});

export var User = Backbone.Model.extend({
  defaults: {
    id: null,
    full_name: null,
    username: null,
    admin: false,
  },
  idAttribute: "username",
  url: function() {
    return "/users/" + this.get("username");
  },
  parse: function(response) {
    if (response.data) {
      return response.data;
    }
    return response;
  },
});

export var Team = Backbone.Model.extend({
  idAttribute: "canonical",
  defaults: {
    id: null,
    name: null,
    canonical: null,
  },
  initialize: function(attr) {
    attr = attr || {};
    if (attr.members && Array.isArray(attr.members)) {
      this.set("members", new TeamMembers(attr.members, {team: this}));
    }
  },
  parse: function(response) {
    if (response.data){
      response.data.members = new TeamMembers(response.data.members, {team: this});
      return response.data;
    }
    response.members = new TeamMembers(response.members, {team: this});
    return response;
  }
});

export var TeamMember = Backbone.Model.extend({
  initialize: function(attr, opts){
    opts = opts || {};
    this.set("id", attr.user.username);
    this.team = opts.team;
  },
  parse: function(response) {
    if (response.data){
      return response.data;
    }
    return response;
  }
});

export var Pipeline = Backbone.Model.extend({
  idAttribute: "canonical",
  defaults: {
    id: null,
    raw: null,
    name: null,
    canonical: null,
    public: false,
  },
  parse: function(response) {
    if (response.data){
      return response.data;
    }
    return response;
  }
});

export var PipelineImage = Backbone.Model.extend({
  url: function() {
    return this.pipeline.url() + "/image.dot";
  },
  initialize: function(attr, opts) {
    opts = opts || {};
    this.pipeline = opts.pipeline;
  },
});

export var Job = Backbone.Model.extend({
  idAttribute: "name",
  parse: function(response) {
    if (response.data){
      return response.data;
    }
    return response;
  },
  fetchTrigger: function() {
    this.fetch({url: this.url()+"/trigger", type: "POST"});
  },
});

export var Build = Backbone.Model.extend({
  idAttribute: "build_number",
  parse: function(response) {
    if (response.data){
      return response.data;
    }
    return response;
  }
});

export var Resource = Backbone.Model.extend({
  idAttribute: "canonical",
  parse: function(response) {
    return response.data;
  },
  fetchTrigger: function() {
    this.fetch({url: this.url()+"/trigger", type: "POST"});
  },
});

export var ResourceVersion = Backbone.Model.extend({
  idAttribute: "id",
  parse: function(response) {
    if (response.data){
      return response.data;
    }
    return response;
  }
});

export var ApiNotice = Backbone.Model.extend({
  defaults: {
    error: "",
    success: "",
  },
  clear: function(){
    Backbone.Model.prototype.set.call(this, {error: "", success: ""});
  },
  set: function(attrs, options) {
    if (attrs.error && attrs.error !== "") {
      attrs.success = "";
    }
    return Backbone.Model.prototype.set.call(this, attrs, options);
  },
  setSuccess: function(msg){
    this.set({error: "", success: msg});
  }
});

// TeamMembers is exported for use by Team model
export var TeamMembers = Backbone.Collection.extend({
  model: TeamMember,
  url: function() {
    return this.team.url()+"/members";
  },
  initialize: function(attr, opts) {
    this.team = opts.team;
  },
  parse: function(response) {
    return response.data;
  }
});
