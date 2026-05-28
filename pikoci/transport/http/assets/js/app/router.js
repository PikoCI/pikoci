'use strict';

import { session, teams, userSessionKey, Users, Pipelines, Jobs, Builds, Resources, ResourceVersions } from './collections.js';
import { Pipeline, PipelineImage, Team, Job, User, Resource } from './models.js';
import { MainView, HeaderView, BreadcrumbView } from './views/layout.js';
import { SessionNewView } from './views/session.js';
import { TeamsView, TeamShowView, TeamsNewView } from './views/teams.js';
import { PipelinesView, PipelineShowView } from './views/pipelines.js';
import { PipelinesNewView } from './views/editor.js';
import { JobBuildsView } from './views/jobs.js';
import { ResourceVersionsView } from './views/resources.js';
import { UsersListView, UserShowView, UsersNewView, ProfileView } from './views/users.js';

var isLogin = true;

export var Router = Backbone.Router.extend({
  routes: {
    'login':  'sessionNew',
    'logout': 'sessionDelete',

    '':                'teamsIndex',
    'teams':           'teamsIndex',
    'teams/new':       'teamsNew',
    'teams/:tn':       'teamsShow',

    'teams/:tc/pipelines' :           'pipelinesIndex',
    'teams/:tc/pipelines/new' :       'pipelinesNew',
    'teams/:tc/pipelines/:pn':        'pipelinesShow',
    'teams/:tc/pipelines/:pn/edit' :  'pipelinesEdit',

    'teams/:tc/pipelines/:pn/jobs/:jn/builds':        'jobBuilds',
    'teams/:tc/pipelines/:pn/jobs/:jn/builds/:bid':   'jobBuilds',

    'teams/:tn/pipelines/:pn/resources/:rCan/versions': 'resourceVersions',

    'users':          'usersIndex',
    'users/new':      'usersNew',
    'users/:username': 'usersShow',
    'profile':        'profile',

    '*notFound': 'notFound',
  },
  sessionNew: function() {
    if (!this.setup(isLogin)) {
      return;
    }
    this.contentView = new SessionNewView();
    $('#main').html(this.contentView.render().el);
    $('#breadcrumb').empty();
  },
  sessionDelete: function() {
    window.localStorage.removeItem(userSessionKey);
    session.clear();
    window.app.router.navigate("login", { trigger: true });
  },
  teamsIndex: function() {
    if (!this.setup(!isLogin)) {
      return;
    }
    this.contentView = new TeamsView({collection: teams});
    $('#main').html(this.contentView.render().el);
    $('#breadcrumb').html(new BreadcrumbView().render().el);
  },
  teamsNew: function() {
    if (!this.setup(!isLogin)) {
      return;
    }
    if (!session.isAdmin()) {
      window.app.router.navigate('', { trigger: true });
      return;
    }
    this.contentView = new TeamsNewView({collection: teams, model: new Team()});
    $('#main').html(this.contentView.render().el);
    $('#breadcrumb').html(new BreadcrumbView().render().el);
  },
  teamsShow: function(tc) {
    if (!this.setup(!isLogin)) {
      return;
    }
    var promises = [];
    var t = teams.get(tc);
    if (t === undefined) {
      t = new Team({canonical: tc}, {collection: teams});
      promises.push(t);
    }
    var complete = _.invoke(promises, 'fetch');
    var that = this;
    $.when.apply($, complete).done(function() {
      that.contentView = new TeamShowView({model: t});
      $('#main').html(that.contentView.render().el);
      $('#breadcrumb').html(new BreadcrumbView({
        team: t.toJSON(),
      }).render().el);
    });
  },
  pipelinesIndex: function(tc) {
    if (!this.setup(!isLogin)) {
      return;
    }
    var promises = [];
    var t = teams.get(tc);
    if (t === undefined) {
      t = new Team({canonical: tc}, {collection: teams});
      promises.push(t);
    }
    var pps = new Pipelines(null, {team: t});
    var complete = _.invoke(promises, 'fetch');
    var that = this;
    $.when.apply($, complete).done(function() {
      that.contentView = new PipelinesView({collection: pps});
      $('#main').html(that.contentView.render().el);
      $('#breadcrumb').html(new BreadcrumbView({
        team: t.toJSON(),
        showPipelines: true,
      }).render().el);
    });
  },
  pipelinesNew: function(tc) {
    if (!this.setup(!isLogin)) {
      return;
    }
    if (!session.isAdmin(tc)) {
      window.app.router.navigate('teams/'+tc+'/pipelines', { trigger: true });
      return;
    }
    var promises = [];
    var t = teams.get(tc);
    if (t === undefined) {
      t = new Team({canonical: tc}, {collection: teams});
      promises.push(t);
    }
    var pps = new Pipelines(null, {team: t});
    var complete = _.invoke(promises, 'fetch');
    var that = this;
    $.when.apply($, complete).done(function() {
      that.contentView = new PipelinesNewView({collection: pps, model: new Pipeline(null, {team: t})});
      $('#main').html(that.contentView.render().el);
      $('#breadcrumb').html(new BreadcrumbView({
        team: t.toJSON(),
      }).render().el);
    });
  },
  pipelinesEdit: function(tc, pn) {
    if (!this.setup(!isLogin)) {
      return;
    }
    if (!session.isAdmin(tc)) {
      window.app.router.navigate('teams/'+tc+'/pipelines/'+pn, { trigger: true });
      return;
    }
    var promises = [];
    var t = teams.get(tc);
    if (t === undefined) {
      t = new Team({canonical: tc}, {collection: teams});
      promises.push(t);
    }
    var pps = new Pipelines(null, {team: t});
    var pp = new Pipeline({canonical: pn},{collection: pps});
    promises.push(pp);
    var complete = _.invoke(promises, 'fetch');
    var that = this;
    $.when.apply($, complete).done(function() {
      that.contentView = new PipelinesNewView({collection: pps, model: pp});
      $('#main').html(that.contentView.render().el);
      $('#breadcrumb').html(new BreadcrumbView({
        team: t.toJSON(),
        pipeline: pp.toJSON(),
      }).render().el);
    });
  },
  pipelinesShow: function(tc, pn) {
    if (!this.setup(!isLogin, true)) {
      return;
    }
    var promises = [];
    var t = teams.get(tc);
    if (t === undefined) {
      t = new Team({canonical: tc}, {collection: teams});
      if (!session.isEmpty()) {
        promises.push(t);
      }
    }
    var pps = new Pipelines(null, {team: t});
    var pp = new Pipeline({canonical: pn},{collection: pps});
    promises.push(pp);
    var complete = _.invoke(promises, 'fetch');
    var that = this;
    $.when.apply($, complete).done(function() {
      that.contentView = new PipelineShowView({model: pp, image: new PipelineImage(null, {pipeline: pp}) });
      $('#main').html(that.contentView.render().el);
      $('#breadcrumb').html(new BreadcrumbView({
        team: t.toJSON(),
        pipeline: pp.toJSON(),
      }).render().el);
    }).fail(function() {
      if (session.isEmpty()) {
        window.app.router.navigate("login", { trigger: true });
      }
    });
  },
  jobBuilds: function(tc, pn, jn, bid) {
    if (!this.setup(!isLogin, true)) {
      return;
    }
    var promises = [];
    var t = teams.get(tc);
    if (t === undefined) {
      t = new Team({canonical: tc}, {collection: teams});
      if (!session.isEmpty()) {
        promises.push(t);
      }
    }
    var pps = new Pipelines(null, {team: t});
    var pp = new Pipeline({canonical: pn},{collection: pps});
    promises.push(pp);
    var jbs = new Jobs(null, {pipeline: pp});
    var jb = new Job({name: jn}, {collection: jbs});
    promises.push(jb);
    var builds = new Builds(null, {job: jb});
    var complete = _.invoke(promises, 'fetch');
    var that = this;
    $.when.apply($, complete).done(function() {
      that.contentView = new JobBuildsView({
        collection: builds,
        currentBuildID: bid,
        job: jb,
        pipeline: pp,
      });
      $('#main').html(that.contentView.render().el);
      $('#breadcrumb').html(new BreadcrumbView({
        team: t.toJSON(),
        pipeline: pp.toJSON(),
        job: jb.toJSON(),
      }).render().el);
    }).fail(function() {
      if (session.isEmpty()) {
        window.app.router.navigate("login", { trigger: true });
      }
    });
  },
  resourceVersions: function(tc, pn, rCan) {
    if (!this.setup(!isLogin, true)) {
      return;
    }
    var promises = [];
    var t = teams.get(tc);
    if (t === undefined) {
      t = new Team({canonical: tc}, {collection: teams});
      if (!session.isEmpty()) {
        promises.push(t);
      }
    }
    var pps = new Pipelines(null, {team: t});
    var pp = new Pipeline({canonical: pn},{collection: pps});
    promises.push(pp);
    var rss = new Resources(null, {pipeline: pp});
    var rs = new Resource({canonical: rCan}, {collection: rss});
    promises.push(rs);
    var versions = new ResourceVersions(null, {resource: rs});

    var complete = _.invoke(promises, 'fetch');
    var that = this;
    $.when.apply($, complete).done(function() {
      that.contentView = new ResourceVersionsView({
        model: rs,
        collection: versions,
      });
      $('#main').html(that.contentView.render().el);
      $('#breadcrumb').html(new BreadcrumbView({
        team: t.toJSON(),
        pipeline: pp.toJSON(),
        resource: rs.toJSON(),
      }).render().el);
    }).fail(function() {
      if (session.isEmpty()) {
        window.app.router.navigate("login", { trigger: true });
      }
    });
  },
  usersIndex: function() {
    if (!this.setup(!isLogin)) {
      return;
    }
    if (!session.isAdmin()) {
      window.app.router.navigate('', { trigger: true });
      return;
    }
    var users = new Users();
    this.contentView = new UsersListView({collection: users});
    $('#main').html(this.contentView.render().el);
    $('#breadcrumb').html(new BreadcrumbView().render().el);
  },
  usersNew: function() {
    if (!this.setup(!isLogin)) {
      return;
    }
    if (!session.isAdmin()) {
      window.app.router.navigate('', { trigger: true });
      return;
    }
    this.contentView = new UsersNewView();
    $('#main').html(this.contentView.render().el);
    $('#breadcrumb').html(new BreadcrumbView().render().el);
  },
  usersShow: function(username) {
    if (!this.setup(!isLogin)) {
      return;
    }
    if (!session.isAdmin()) {
      window.app.router.navigate('', { trigger: true });
      return;
    }
    var u = new User({username: username});
    var that = this;
    u.fetch({
      success: function() {
        that.contentView = new UserShowView({model: u});
        $('#main').html(that.contentView.render().el);
        $('#breadcrumb').html(new BreadcrumbView().render().el);
      },
    });
  },
  profile: function() {
    if (!this.setup(!isLogin, false, true)) {
      return;
    }
    var u = session.get("user");
    var mustChange = u && u.must_change_password;
    this.contentView = new ProfileView({mustChangePassword: mustChange});
    $('#main').html(this.contentView.render().el);
    $('#breadcrumb').html(new BreadcrumbView().render().el);
  },
  notFound: function() {
    window.app.router.navigate('', { trigger: true });
  },
  setup: function(isLogin, allowPublic, isProfile) {
    if (!this.mainView) {
      this.mainView = new MainView({ el: '#app' });
    }
    if (session.isEmpty() && !isLogin && !allowPublic) {
      window.app.router.navigate("login", { trigger: true });
      return false;
    } else if (!session.isEmpty() && isLogin) {
      window.app.router.navigate("", { trigger: true });
      return true;
    }
    if (!isProfile && !session.isEmpty()) {
      var u = session.get("user");
      if (u && u.must_change_password) {
        window.app.router.navigate("profile", { trigger: true });
        return false;
      }
    }
    if (this.contentView) {
      this.contentView.undelegateEvents();
      this.contentView.$el.removeData().unbind();
      this.contentView.remove();
      Backbone.View.prototype.remove.call(this.contentView);
    }
    return true;
  }
});
