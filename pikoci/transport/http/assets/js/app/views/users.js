'use strict';

import { session, userSessionKey } from '../collections.js';

export var UsersListView = Backbone.View.extend({
  template: _.template($('#users-list-view').html()),
  initialize: function() {
    this.listenTo(this.collection, "add", this.addUser);
    this.listenTo(this.collection, "destroy", this.render);
    this.collection.fetch();
  },
  events: {
    'click a': 'clickLink',
  },
  render: function() {
    this.$el.html(this.template());
    var that = this;
    this.collection.each(function(m) {
      that.addUser(m);
    });
    return this;
  },
  addUser: function(m) {
    var view = new UsersRowView({model: m});
    this.$('#users-table-body').append(view.render().el);
  },
  clickLink: function(event) {
    event.preventDefault();
    var target = event.target.closest('a');
    var url = new URL(target.href);
    window.app.router.navigate(url.pathname, { trigger: true });
  },
});

var UsersRowView = Backbone.View.extend({
  template: _.template($('#users-row-view').html()),
  tagName: "tr",
  events: {
    'click a': 'clickLink',
  },
  render: function() {
    this.$el.html(this.template(this.model.toJSON()));
    return this;
  },
  clickLink: function(event) {
    event.preventDefault();
    var url = new URL(event.target.href);
    window.app.router.navigate(url.pathname, { trigger: true });
  },
});

export var UserShowView = Backbone.View.extend({
  template: _.template($('#user-show-view').html()),
  events: {
    'submit #user-form': 'submitForm',
    'submit #reset-password-form': 'resetPassword',
    'click #delete-user': 'deleteUser',
  },
  render: function() {
    this.$el.html(this.template(this.model.toJSON()));
    return this;
  },
  submitForm: function(event) {
    event.preventDefault();
    var fullName = this.$('#full_name').val();
    var username = this.$('#username').val();
    var admin = this.$('#admin').is(':checked');
    var originalUsername = this.model.get("username");
    var that = this;

    $.ajax({
      url: '/users/' + originalUsername + '',
      type: 'PUT',
      headers: {
        'Authorization': 'Bearer ' + session.get("jwt"),
        'Content-Type': 'application/json',
      },
      data: JSON.stringify({full_name: fullName, username: username, admin: admin}),
      success: function(resp) {
        if (resp.error) {
          window.app.apiNotice.set({error: resp.error});
          return;
        }
        if (username !== originalUsername) {
          window.app.router.navigate('users/' + username, { trigger: true });
        } else {
          that.model.set(resp.data);
          that.render();
        }
      },
    });
  },
  resetPassword: function(event) {
    event.preventDefault();
    var newPassword = this.$('#new_password').val();
    if (!newPassword) return;
    var username = this.model.get("username");

    $.ajax({
      url: '/users/' + username + '',
      type: 'PUT',
      headers: {
        'Authorization': 'Bearer ' + session.get("jwt"),
        'Content-Type': 'application/json',
      },
      data: JSON.stringify({password: newPassword, admin: this.model.get("admin")}),
      success: function(resp) {
        if (resp.error) {
          window.app.apiNotice.set({error: resp.error});
          return;
        }
        window.app.apiNotice.setSuccess("Password reset successfully");
      },
    });
  },
  deleteUser: function(event) {
    event.preventDefault();
    var username = this.model.get("username");
    if (!confirm("Are you sure you want to delete user '" + username + "'?")) return;

    $.ajax({
      url: '/users/' + username + '',
      type: 'DELETE',
      headers: {
        'Authorization': 'Bearer ' + session.get("jwt"),
        'Content-Type': 'application/json',
      },
      success: function(resp) {
        if (resp.error) {
          window.app.apiNotice.set({error: resp.error});
          return;
        }
        window.app.router.navigate('users', { trigger: true });
      },
    });
  },
});

export var UsersNewView = Backbone.View.extend({
  template: _.template($('#users-new-view').html()),
  events: {
    'submit #user-create-form': 'submitForm',
  },
  render: function() {
    this.$el.html(this.template());
    return this;
  },
  submitForm: function(event) {
    event.preventDefault();
    var username = this.$('#username').val();
    var fullName = this.$('#full_name').val();
    var password = this.$('#password').val();
    var admin = this.$('#admin').is(':checked');

    $.ajax({
      url: '/users',
      type: 'POST',
      headers: {
        'Authorization': 'Bearer ' + session.get("jwt"),
        'Content-Type': 'application/json',
      },
      data: JSON.stringify({username: username, password: password, full_name: fullName, admin: admin}),
      success: function(resp) {
        if (resp.error) {
          window.app.apiNotice.set({error: resp.error});
          return;
        }
        window.app.router.navigate('users/' + username, { trigger: true });
      },
    });
  },
});

export var ProfileView = Backbone.View.extend({
  template: _.template($('#profile-view').html()),
  initialize: function(opts) {
    this.mustChangePassword = opts.mustChangePassword || false;
  },
  events: {
    'submit #profile-form': 'submitProfile',
    'submit #change-password-form': 'changePassword',
  },
  render: function() {
    var u = session.get("user");
    this.$el.html(this.template({
      fullName: u.full_name || '',
      currentUsername: u.username || '',
      mustChangePassword: this.mustChangePassword,
    }));
    return this;
  },
  submitProfile: function(event) {
    event.preventDefault();
    var fullName = this.$('#full_name').val();
    var username = this.$('#username').val();

    $.ajax({
      url: '/profile',
      type: 'PUT',
      headers: {
        'Authorization': 'Bearer ' + session.get("jwt"),
        'Content-Type': 'application/json',
      },
      data: JSON.stringify({full_name: fullName, username: username}),
      success: function(resp) {
        if (resp.error) {
          window.app.apiNotice.set({error: resp.error});
          return;
        }
        $.ajax({
          url: '/refresh-token',
          type: 'POST',
          headers: {
            'Authorization': 'Bearer ' + session.get("jwt"),
            'Content-Type': 'application/json',
          },
          success: function(refreshResp) {
            if (refreshResp.data && refreshResp.data.jwt) {
              session.set({jwt: refreshResp.data.jwt, user: refreshResp.data.user});
              window.localStorage.setItem(userSessionKey, JSON.stringify(session.toJSON()));
            }
          },
        });
        window.app.apiNotice.setSuccess("Profile updated successfully");
      },
    });
  },
  changePassword: function(event) {
    event.preventDefault();
    var currentPassword = this.$('#current_password').val();
    var newPassword = this.$('#new_password').val();
    var confirmPassword = this.$('#confirm_password').val();

    if (newPassword !== confirmPassword) {
      window.app.apiNotice.set({error: "New passwords do not match"});
      return;
    }

    var that = this;
    $.ajax({
      url: '/users/change-password',
      type: 'POST',
      headers: {
        'Authorization': 'Bearer ' + session.get("jwt"),
        'Content-Type': 'application/json',
      },
      data: JSON.stringify({old_password: currentPassword, new_password: newPassword}),
      success: function(resp) {
        if (resp.error) {
          window.app.apiNotice.set({error: resp.error});
          return;
        }
        var wasForcedChange = that.mustChangePassword;
        that.mustChangePassword = false;
        that.$('#must-change-password-banner').remove();
        var u = session.get("user");
        if (u) {
          u.must_change_password = false;
          session.set({user: u});
          window.localStorage.setItem(userSessionKey, JSON.stringify(session.toJSON()));
        }
        window.app.apiNotice.setSuccess("Password changed successfully");
        if (wasForcedChange) {
          window.app.router.navigate('', { trigger: true });
        }
      },
      error: function(response) {
        var msg = (response.responseJSON && response.responseJSON.error) || response.statusText || "Unknown error";
        window.app.apiNotice.set({error: msg});
      },
    });
  },
});
