'use strict';

import './namespace.js';
import { session, apiNotice, userSessionKey } from './collections.js';
import { Router } from './router.js';

var backboneSync = Backbone.sync;
Backbone.sync = function (method, model, options) {
  options.headers = {
    'Content-Type': 'application/json',
  };
  if (!session.isEmpty()) {
    options.headers['Authorization'] = 'Bearer ' + session.get("jwt");
  }
  var optError = options.error;
  options.error = function(response){
    var msg;
    if (response.status === 0 || response.status === 502 || response.status === 503 || response.status === 504) {
      msg = "Connection lost. Retrying...";
    } else {
      msg = (response.responseJSON && response.responseJSON.error) || response.statusText || "Unknown error";
    }
    if (options.isInterval && apiNotice.get("error") !== "") {
      if (optError) {
        optError(response);
      }
      return;
    }
    apiNotice.set({error: msg});
    if (optError) {
      optError(response);
    }
  };
  var optSuccess = options.success;
  options.success = function(response, textStatus, jqXHR){
    if (options.isInterval) {
      apiNotice.set({error: ""});
    } else {
      apiNotice.clear();
    }
    if (jqXHR && jqXHR.getResponseHeader('X-Refresh-Token') === 'true') {
      $.ajax({
        url: '/refresh-token',
        type: 'POST',
        headers: {
          'Authorization': 'Bearer ' + session.get("jwt"),
          'Content-Type': 'application/json',
        },
        success: function(resp) {
          if (resp.data && resp.data.jwt) {
            session.set({jwt: resp.data.jwt, user: resp.data.user});
            window.localStorage.setItem(userSessionKey, JSON.stringify(session.toJSON()));
          }
        },
      });
    }
    if (optSuccess) {
      optSuccess(response);
    }
  };
  return backboneSync(method, model, options);
};

window.app.router = new Router();
Backbone.history.start({pushState: true});
