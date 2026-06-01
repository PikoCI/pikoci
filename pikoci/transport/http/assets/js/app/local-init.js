'use strict';

import './namespace.js';
import { ApiNotice, Pipeline } from './models.js';
import { PipelinesNewView } from './views/editor.js';

// Set up minimal app globals
var apiNotice = new ApiNotice();
window.app.apiNotice = apiNotice;
window.app.session = new (Backbone.Model.extend({
  isEmpty: function() { return true; },
  isAdmin: function() { return true; },
  isMember: function() { return true; },
  data: function() {
    return {
      isAdmin: function() { return true; },
      isMember: function() { return true; },
    };
  },
}))();

// Override Backbone.sync to skip auth headers
var backboneSync = Backbone.sync;
Backbone.sync = function(method, model, options) {
  options.headers = {
    'Content-Type': 'application/json',
  };
  var optError = options.error;
  options.error = function(response) {
    var msg = (response.responseJSON && response.responseJSON.error) || response.statusText || 'Unknown error';
    apiNotice.set({error: msg});
    if (optError) optError(response);
  };
  var optSuccess = options.success;
  options.success = function(response) {
    apiNotice.clear();
    if (optSuccess) optSuccess(response);
  };
  return backboneSync(method, model, options);
};

// Notice view
var NoticeView = Backbone.View.extend({
  template: _.template($('#notice-view').html()),
  initialize: function() {
    this.listenTo(this.model, 'change', this.render);
  },
  render: function() {
    this.$el.html(this.template(this.model.toJSON()));
    var success = this.model.get('success');
    if (success) {
      this.showToast(success, 'success');
      this.model.set({success: ''}, {silent: true});
    }
    var error = this.model.get('error');
    if (error) {
      this.showToast(error, 'error');
      this.model.set({error: ''}, {silent: true});
    }
    return this;
  },
  showToast: function(msg, type) {
    $('.piko-toast').remove();
    var toast = $('<div class="piko-toast"></div>').addClass('piko-toast-' + type);
    var closeBtn = $('<button class="piko-toast-close" aria-label="Dismiss"></button>');
    toast.append(document.createTextNode(msg)).append(closeBtn);
    $('body').append(toast);
    requestAnimationFrame(function() { toast.addClass('show'); });
    var dismissTimer = setTimeout(function() {
      toast.removeClass('show');
      setTimeout(function() { toast.remove(); }, 300);
    }, type === 'error' ? 8000 : 4000);
    closeBtn.on('click', function() {
      clearTimeout(dismissTimer);
      toast.removeClass('show');
      setTimeout(function() { toast.remove(); }, 300);
    });
  },
});

// Fetch initial config and boot the editor
$.getJSON('/local/config', function(config) {
  var model = new Pipeline({
    raw: config.raw,
    name: config.name,
    canonical: null,
    id: null,
  });

  var collection = {
    url: function() { return '/teams/local/pipelines'; },
    create: function(attrs, options) {
      options = options || {};
      $.ajax({
        url: '/local/save',
        type: 'POST',
        contentType: 'application/json',
        data: JSON.stringify({config: btoa(String.fromCharCode.apply(null, attrs.config))}),
        success: function() {
          apiNotice.setSuccess('File saved successfully');
          // Do not call options.success — it tries to navigate via router which doesn't exist locally
        },
        error: function(xhr) {
          var msg = 'Failed to save file';
          try {
            var resp = JSON.parse(xhr.responseText);
            if (resp && resp.error) msg = resp.error;
          } catch(e) {}
          apiNotice.set({error: msg});
          if (options.error) options.error(xhr);
        },
      });
    },
  };

  // Render main layout
  $('#app').html($('#main-view').html());
  var noticeView = new NoticeView({model: apiNotice});
  $('#notice').html(noticeView.render().el);

  var view = new PipelinesNewView({model: model, collection: collection});
  $('#main').html(view.render().el);
});
