'use strict';

import { html } from 'htm/preact';
import { useState } from 'preact/hooks';
import { route } from 'preact-router';
import { postOAuthCompleteProfile } from '../api.js';
import { login } from '../state.js';
import { useLoading } from '../hooks.js';
import { showToast } from '../toast.js';

export function OAuthCompleteProfile() {
  // Read params once on mount via useState initializer — survives re-renders
  const [initParams] = useState(() => {
    const p = new URLSearchParams(window.location.search);
    // Strip sensitive params from URL
    if (window.location.search) {
      window.history.replaceState(null, '', '/auth/complete-profile');
    }
    return {
      token: p.get('token') || '',
      username: p.get('username') || '',
      fullName: p.get('full_name') || '',
      email: p.get('email') || '',
    };
  });
  const [username, setUsername] = useState(initParams.username);
  const [fullName, setFullName] = useState(initParams.fullName);
  const token = initParams.token;
  const email = initParams.email;
  const [loading, withLoading] = useLoading();

  if (!token) {
    route('/login', true);
    return null;
  }

  const onSubmit = (e) => {
    e.preventDefault();
    withLoading(async () => {
      const resp = await postOAuthCompleteProfile({ token, username, full_name: fullName });
      if (resp.error) {
        showToast(resp.error, 'error');
        return;
      }
      const { jwt, user } = resp.data;
      login(jwt, user);
      route('/');
    });
  };

  return html`
    <div class="piko-login-wrapper">
      <div class="piko-login-box">
        <div class="text-center mb-4">
          <img src="/images/logo.svg" alt="PikoCI" width="60" height="60" class="mb-2" />
          <h1 class="h4 fw-bold">Complete Your Profile</h1>
          ${email ? html`<p class="text-muted small">Signing in as ${email}</p>` : null}
        </div>
        <form onSubmit=${onSubmit}>
          <div class="mb-3">
            <label for="username" class="form-label">Username</label>
            <input
              type="text"
              class="form-control"
              id="username"
              placeholder="Choose a username"
              value=${username}
              onInput=${(e) => setUsername(e.target.value)}
              required
            />
            <div class="form-text">Lowercase letters, numbers, and hyphens only.</div>
          </div>
          <div class="mb-3">
            <label for="full_name" class="form-label">Full Name</label>
            <input
              type="text"
              class="form-control"
              id="full_name"
              placeholder="Your full name"
              value=${fullName}
              onInput=${(e) => setFullName(e.target.value)}
            />
          </div>
          <button
            type="submit"
            class="btn btn-primary w-100"
            disabled=${loading}
          >
            ${loading ? 'Creating account...' : 'Create Account'}
          </button>
        </form>
      </div>
    </div>
  `;
}
