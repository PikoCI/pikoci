'use strict';

import { html } from 'htm/preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { postLogin, fetchAuthMethods } from '../api.js';
import { login } from '../state.js';
import { useLoading } from '../hooks.js';
import { getProviderIcon } from '../provider-icons.js';

export function Login() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [loading, withLoading] = useLoading();
  const [authMethods, setAuthMethods] = useState(null);

  useEffect(() => {
    fetchAuthMethods().then(setAuthMethods);
  }, []);

  const onSubmit = (e) => {
    e.preventDefault();
    withLoading(async () => {
      const resp = await postLogin({ username, password });
      const { jwt, user } = resp.data;
      login(jwt, user);
      setPassword('');
      if (user.must_change_password) {
        route('/profile');
      } else {
        route('/');
      }
    });
  };

  const onOAuthLogin = (canonical) => {
    window.location.href = '/auth/oauth/' + canonical;
  };

  const localEnabled = !authMethods || authMethods.local_auth_enabled;
  const providers = (authMethods && authMethods.providers) || [];

  return html`
    <div class="piko-login-wrapper">
      <div class="piko-login-box">
        <div class="text-center mb-4">
          <img src="/images/logo.svg" alt="PikoCI" width="60" height="60" class="mb-2" />
          <h1 class="h4 fw-bold">Log In</h1>
        </div>
        ${localEnabled ? html`
          <form onSubmit=${onSubmit}>
            <div class="mb-3">
              <label for="username" class="form-label">Username</label>
              <input
                type="text"
                class="form-control"
                id="username"
                placeholder="Enter username"
                value=${username}
                onInput=${(e) => setUsername(e.target.value)}
              />
            </div>
            <div class="mb-3">
              <label for="password" class="form-label">Password</label>
              <input
                type="password"
                class="form-control"
                id="password"
                placeholder="Enter password"
                value=${password}
                onInput=${(e) => setPassword(e.target.value)}
              />
            </div>
            <button
              type="submit"
              id="login"
              class="btn btn-primary w-100"
              disabled=${loading}
            >
              ${loading ? 'Logging in...' : 'Log In'}
            </button>
          </form>
        ` : null}
        ${providers.length > 0 ? html`
          ${localEnabled ? html`
            <div class="d-flex align-items-center my-3">
              <hr class="flex-grow-1" style="border-color: var(--border);" />
              <span class="px-2 text-muted small">or</span>
              <hr class="flex-grow-1" style="border-color: var(--border);" />
            </div>
          ` : null}
          <div>
            ${providers.map(p => html`
              <button
                key=${p.canonical}
                type="button"
                class="btn btn-outline-secondary w-100 mb-2 d-flex align-items-center justify-content-center gap-2"
                onClick=${() => onOAuthLogin(p.canonical)}
              >
                ${getProviderIcon(p.canonical)}
                Log in with ${p.name}
              </button>
            `)}
          </div>
        ` : null}
        ${!localEnabled && providers.length === 0 ? html`
          <div class="text-muted text-center">No authentication methods available.</div>
        ` : null}
      </div>
    </div>
  `;
}
