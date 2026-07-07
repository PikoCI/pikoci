'use strict';

import { useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { login } from '../state.js';
import { postRefreshToken } from '../api.js';

export function OAuthCallback() {
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const jwt = params.get('jwt');
    if (jwt) {
      window.history.replaceState(null, '', '/auth/callback');
      login(jwt, {});
      postRefreshToken().then(resp => {
        if (resp.data && resp.data.jwt) {
          login(resp.data.jwt, resp.data.user);
        }
        route('/', true);
      }).catch(() => {
        route('/', true);
      });
    } else {
      route('/login', true);
    }
  }, []);
  return null;
}
