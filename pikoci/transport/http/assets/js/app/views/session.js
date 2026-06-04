'use strict';

import { session, userSessionKey } from '../collections.js';
import { withLoading } from '../namespace.js';

export var SessionNewView = Backbone.View.extend({
  template: _.template($('#session-new-view').html()),
  events: {
    'click #login': 'clickLink',
    'submit #login': 'clickLink',
  },
  render: function () {
    this.$el.html(this.template());
    return this;
  },
  clickLink: function(event) {
    event.preventDefault();
    var username = this.$el.find("#username").get(0).value;
    var password = this.$el.find("#password").get(0).value;

    withLoading(this.$('#login'), function() {
      return session.save({username: username, password: password}, {
        success: function(resp) {
          session.unset("password");
          session.unset("username");
          window.localStorage.setItem(userSessionKey, JSON.stringify(session.toJSON()));
          var u = session.get("user");
          if (u && u.must_change_password) {
            window.app.router.navigate('profile', { trigger: true });
          } else {
            window.app.router.navigate('', { trigger: true });
          }
        },
      });
    });
  },
});
