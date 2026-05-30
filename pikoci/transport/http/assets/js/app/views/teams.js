'use strict';

import { session, teams, Users } from '../collections.js';
import { Team } from '../models.js';
import { addSessionFunctions } from '../namespace.js';

export var TeamsView = Backbone.View.extend({
  template: _.template($('#teams-view').html()),
  initialize: function() {
    this.listenTo(this.collection, "add", this.addTeam);
    this.listenTo(this.collection, "destroy", this.render);

    teams.fetch();
  },
  events: {
    'click #team-new': 'clickLink',
  },
  addTeam: function(t) {
    var view = new TeamRowView({model: t});
    this.$el.find('#team-list').append(view.render().el);
  },
  render: function () {
    this.$el.html(this.template(addSessionFunctions({})));

    var that = this;
    this.collection.each(function(m) {
      that.addTeam(m);
    });
    return this;
  },
  clickLink: function(event) {
    event.preventDefault();
    var url = new URL(event.target.href);
    window.app.router.navigate(url.pathname, { trigger: true });
  },
});

var TeamRowView = Backbone.View.extend({
  template: _.template($('#team-row-view').html()),
  tagName: "div",
  className: "piko-team-row",
  events: {
    'click': 'clickLink',
    'click #pipelines':   'clickLink',
    'click #manage':      'clickLink',
    'click #delete':      'clickDelete',
  },
  render: function () {
    this.$el.html(this.template(addSessionFunctions(this.model.toJSON())));
    return this;
  },
  clickLink: function(event) {
    event.preventDefault();
    var url = new URL(event.target.href);
    window.app.router.navigate(url.pathname, { trigger: true });
  },
  clickDelete: function(event) {
    event.preventDefault();
    event.stopPropagation();

    this.model.destroy({success: function() { window.app.apiNotice.setSuccess("Team deleted"); }});
  },
});

export var TeamsNewView = Backbone.View.extend({
  template: _.template($('#teams-new-view').html()),
  events: {
    'click #create': 'clickCreate',
    'submit form':   'clickCreate',
  },
  render: function () {
    var data = this.model.toJSON();
    if (data.raw) {
      data.raw = atob(data.raw);
    }
    this.$el.html(this.template(data));
    return this;
  },
  clickCreate: function(event) {
    event.preventDefault();
    var name = this.$el.find("#name").get(0).value;

    teams.create({name: name}, {
      wait: true,
      success: function(m) {
        window.app.router.navigate('teams/'+m.get("canonical"), { trigger: true });
      },
    });
  },
});

export var TeamShowView = Backbone.View.extend({
  template: _.template($('#team-show-view').html()),
  initialize: function() {
    this.listenTo(this.model, "change", this.render);
    this.listenTo(this.model.get("members"), "destroy", this.renderMembers);
    this.listenTo(this.model.get("members"), "add", this.renderMembers);
  },
  events: {
    'submit form': 'clickUpdate',
    'click #new-member': 'clickNewMember',
  },
  render: function () {
    this.$el.html(this.template(addSessionFunctions(this.model.toJSON())));
    this.renderMembers();

    return this;
  },
  renderMembers: function() {
    this.$el.find('tbody').empty();
    var that = this;
    this.model.get("members").each(function(m) {
      that.renderMember(m);
    });

    return this;
  },
  renderMember: function(m) {
    var view = new TeamShowMemberRowView({team: this.model, model: m});
    this.$el.find('tbody').append(view.render().el);
  },
  clickUpdate: function(event){
    event.preventDefault();
    var name = this.$el.find("#name").get(0).value;
    this.model.save({name: name}, {
      success: function(m) {
        window.app.router.navigate('teams/'+m.get("canonical"), { trigger: true });
      },
    });
  },
  clickNewMember: function(event){
    event.preventDefault();
    var view = new TeamNewMemberRowView({members: this.model.get("members")});
    if (this.$el.find('tbody #create-member').length === 0) {
      this.$el.find('tbody').prepend(view.render().el);
    }
  },
});

var TeamNewMemberRowView = Backbone.View.extend({
  template: _.template($('#team-new-member-row-view').html()),
  tagName: "tr",
  attributes: {
    id: "create-member",
  },
  events: {
    'click #create': 'clickCreateMember',
    'submit': 'clickCreateMember',
  },
  initialize: function(opts) {
    opts = opts||{};
    this.members = opts.members;
  },
  render: function() {
    var users = new Users();
    var that = this;
    users.fetch({
      success: function() {
        var membersJSON = _.invoke(users.filter(function(u) {
          return !that.members.get(u.get("username"));
        }), "toJSON");
        that.$el.html(that.template({members: membersJSON}));
      },
    });
    return this;
  },
  clickCreateMember: function(event) {
    event.preventDefault();
    var username = this.$el.find("#username").get(0).value;
    var admin = this.$el.find("#admin").get(0).checked;
    var that = this;
    this.members.create({admin: admin, user: {
      username: username,
    }}, {url: this.members.url(), method: "POST",
      wait: true,
      success: function(){
        window.app.apiNotice.setSuccess("Member added");
        Backbone.View.prototype.remove.call(that);
      },
    });
  },
});

var TeamShowMemberRowView = Backbone.View.extend({
  template: _.template($('#team-show-member-row-view').html()),
  tagName: "tr",
  initialize: function(opts) {
    this.listenTo(this.model, "change", this.render);
    this.team = opts.team;
  },
  events: {
    'click #admin': 'updateAdmin',
    'click #delete': 'deleteMember',
  },
  render: function () {
    this.$el.html(this.template(addSessionFunctions({member: this.model.toJSON(), team: this.team.toJSON()})));
    return this;
  },
  updateAdmin: function(event) {
    event.preventDefault();
    this.model.save({admin: !this.model.get("admin")},{
      wait: true,
      success: function() { window.app.apiNotice.setSuccess("Member role updated"); },
    });
  },
  deleteMember: function(event) {
    event.preventDefault();
    this.model.destroy({success: function() { window.app.apiNotice.setSuccess("Member removed"); }});
  },
});
