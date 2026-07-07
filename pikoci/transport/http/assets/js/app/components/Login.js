'use strict';

import { html } from 'htm/preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { postLogin, fetchAuthMethods } from '../api.js';
import { login } from '../state.js';
import { useLoading } from '../hooks.js';

const providerIcons = {
  github: html`<svg width="20" height="20" viewBox="0 0 16 16" fill="currentColor"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></svg>`,
  google: html`<svg width="20" height="20" viewBox="0 0 48 48"><path fill="#FFC107" d="M43.611 20.083H42V20H24v8h11.303c-1.649 4.657-6.08 8-11.303 8-6.627 0-12-5.373-12-12s5.373-12 12-12c3.059 0 5.842 1.154 7.961 3.039l5.657-5.657C34.046 6.053 29.268 4 24 4 12.955 4 4 12.955 4 24s8.955 20 20 20 20-8.955 20-20c0-1.341-.138-2.65-.389-3.917z"/><path fill="#FF3D00" d="m6.306 14.691 6.571 4.819C14.655 15.108 18.961 12 24 12c3.059 0 5.842 1.154 7.961 3.039l5.657-5.657C34.046 6.053 29.268 4 24 4 16.318 4 9.656 8.337 6.306 14.691z"/><path fill="#4CAF50" d="M24 44c5.166 0 9.86-1.977 13.409-5.192l-6.19-5.238A11.91 11.91 0 0 1 24 36c-5.202 0-9.619-3.317-11.283-7.946l-6.522 5.025C9.505 39.556 16.227 44 24 44z"/><path fill="#1976D2" d="M43.611 20.083H42V20H24v8h11.303a12.04 12.04 0 0 1-4.087 5.571l.003-.002 6.19 5.238C36.971 39.205 44 34 44 24c0-1.341-.138-2.65-.389-3.917z"/></svg>`,
  gitlab: html`<svg width="20" height="20" viewBox="0 0 16 16" fill="currentColor"><path d="m15.734 6.1-.022-.058L13.534.358a.57.57 0 0 0-.563-.356.583.583 0 0 0-.328.122.582.582 0 0 0-.193.294l-1.47 4.499H5.025L3.555.418a.57.57 0 0 0-.193-.295.583.583 0 0 0-.89.236L.293 6.04l-.022.058a4.044 4.044 0 0 0 1.34 4.669l.008.006.021.015 3.318 2.484 1.641 1.242 1 .755a.672.672 0 0 0 .814 0l1-.755 1.64-1.242 3.338-2.5.009-.006a4.046 4.046 0 0 0 1.334-4.666z"/></svg>`,
  bitbucket: html`<svg width="20" height="20" viewBox="0 0 16 16" fill="currentColor"><path d="M.778 1.213a.768.768 0 0 0-.768.892l2.17 13.095a.768.768 0 0 0 .768.644h9.608a.563.563 0 0 0 .558-.476l2.172-13.263a.768.768 0 0 0-.768-.892H.778zm8.024 9.386H6.96L6.313 6.677h3.138l-.649 3.922z"/></svg>`,
  microsoft: html`<svg width="20" height="20" viewBox="0 0 21 21"><rect x="1" y="1" width="9" height="9" fill="#f25022"/><rect x="1" y="11" width="9" height="9" fill="#00a4ef"/><rect x="11" y="1" width="9" height="9" fill="#7fba00"/><rect x="11" y="11" width="9" height="9" fill="#ffb900"/></svg>`,
};

function getProviderIcon(canonical) {
  for (const [key, icon] of Object.entries(providerIcons)) {
    if (canonical.startsWith(key)) return icon;
  }
  return html`<i class="bi bi-shield-lock"></i>`;
}

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
        ${providers.length > 0 ? html`
          <div class="mb-3">
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
          ${localEnabled ? html`
            <div class="d-flex align-items-center mb-3">
              <hr class="flex-grow-1" style="border-color: var(--border);" />
              <span class="px-2 text-muted small">or</span>
              <hr class="flex-grow-1" style="border-color: var(--border);" />
            </div>
          ` : null}
        ` : null}
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
        ${!localEnabled && providers.length === 0 ? html`
          <div class="text-muted text-center">No authentication methods available.</div>
        ` : null}
      </div>
    </div>
  `;
}
