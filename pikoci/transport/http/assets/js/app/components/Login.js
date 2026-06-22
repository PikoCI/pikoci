'use strict';

import { html } from 'htm/preact';
import { useState } from 'preact/hooks';
import { route } from 'preact-router';
import { postLogin } from '../api.js';
import { login } from '../state.js';
import { useLoading } from '../hooks.js';

export function Login() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [loading, withLoading] = useLoading();

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

  return html`
    <div class="piko-login-wrapper">
      <div class="piko-login-box">
        <div class="text-center mb-4">
          <img src="/images/logo.svg" alt="PikoCI" width="60" height="60" class="mb-2" />
          <h1 class="h4 fw-bold">Log In</h1>
        </div>
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
      </div>
    </div>
  `;
}
