'use strict';

import { html } from 'htm/preact';
import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { session, isLoggedIn, isAdmin, logout } from '../state.js';
import { fetchVersion, fetchWorkersHealth } from '../api.js';
import { toggleTheme, syncThemeSwitch, exportDatabase } from '../utils.js';
import { registerToastSetter, dismissToast } from '../toast.js';

// --- Notice (toast renderer) ---

export const Notice = () => {
  const [toasts, setToasts] = useState([]);

  useEffect(() => {
    registerToastSetter(setToasts);
    return () => registerToastSetter(null);
  }, []);

  return html`
    <div id="notice">
      ${toasts.map(t => html`
        <div class="piko-toast ${t.type ? 'piko-toast-' + t.type : ''} ${t.show ? 'show' : ''}" key=${t.id}>
          ${t.msg}
          <button class="piko-toast-close" aria-label="Dismiss" onClick=${() => dismissToast(t.id)}></button>
        </div>
      `)}
    </div>
  `;
};

// --- Header ---

const Header = () => {
  const [versionText, setVersionText] = useState('');
  const [unhealthy, setUnhealthy] = useState(false);
  const s = session.value;
  const loggedIn = isLoggedIn.value;
  const admin = isAdmin.value;

  useEffect(() => {
    fetchVersion()
      .then(data => {
        setVersionText('Version: ' + data.version + ' (' + data.commit + ')');
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (admin) {
      fetchWorkersHealth()
        .then(res => {
          if (!res.error && !res.healthy) {
            setUnhealthy(true);
          } else {
            setUnhealthy(false);
          }
        })
        .catch(() => {});
    }
  }, [admin]);

  useEffect(() => {
    syncThemeSwitch();
  }, []);

  const navLink = (e, href) => {
    e.preventDefault();
    route(href);
  };

  const handleToggleTheme = (e) => {
    e.preventDefault();
    e.stopPropagation();
    toggleTheme();
  };

  const handleExportDb = (e) => {
    e.preventDefault();
    exportDatabase(session.value.jwt);
  };

  const handleLogout = (e) => {
    e.preventDefault();
    e.stopPropagation();
    logout();
    route('/login', true);
  };

  return html`
    <nav class="navbar navbar-expand-md">
      <div class="container-fluid">
        <a class="navbar-brand" id="logo" href="/" data-native onClick=${e => navLink(e, '/')}>
          <img src="/images/logo.svg" alt="PikoCI" width="25" height="25" class="d-inline-block align-text-top" />
          PikoCI
        </a>
        ${loggedIn && html`
          <button class="navbar-toggler" type="button" data-bs-toggle="collapse" data-bs-target="#navbarContent" aria-controls="navbarContent" aria-expanded="false" aria-label="Toggle navigation">
            <span class="navbar-toggler-icon"></span>
          </button>
          <div class="collapse navbar-collapse justify-content-end" id="navbarContent">
            <div class="dropdown">
              <a class="nav-link dropdown-toggle" href="#" role="button" data-bs-toggle="dropdown" data-bs-auto-close="outside" aria-expanded="false">
                ${s.user && s.user.full_name}
              </a>
              <ul class="dropdown-menu dropdown-menu-end">
                <li>
                  <a class="dropdown-item d-flex align-items-center justify-content-between" href="#" onClick=${handleToggleTheme}>
                    <span>Dark Mode</span>
                    <span class="piko-toggle" id="theme-toggle">
                      <span class="piko-toggle-thumb"></span>
                    </span>
                  </a>
                </li>
                <li><hr class="dropdown-divider" /></li>
                <li><a class="dropdown-item" id="nav-profile" href="/profile" data-native onClick=${e => navLink(e, '/profile')}><i class="bi bi-person"></i> Profile</a></li>
                ${admin && html`
                  <li><a class="dropdown-item" id="nav-users" href="/users" data-native onClick=${e => navLink(e, '/users')}><i class="bi bi-people"></i> Users <span class="badge bg-secondary ms-1">Admin</span></a></li>
                  <li><a class="dropdown-item" id="nav-workers" href="/workers" data-native onClick=${e => navLink(e, '/workers')}><i class="bi bi-cpu"></i> Workers <span class="badge bg-secondary ms-1">Admin</span></a></li>
                  <li><a class="dropdown-item" id="nav-export-db" href="#" onClick=${handleExportDb}><i class="bi bi-download"></i> Export Database <span class="badge bg-secondary ms-1">Admin</span></a></li>
                `}
                <li><hr class="dropdown-divider" /></li>
                <li><span class="dropdown-item" id="app-version">${versionText}</span></li>
                <li><a class="dropdown-item" id="logout" href="#" onClick=${handleLogout}><i class="bi bi-box-arrow-right"></i> Logout</a></li>
              </ul>
            </div>
          </div>
        `}
      </div>
    </nav>
    ${unhealthy && html`
      <div id="worker-health-banner" class="piko-worker-banner">
        <i class="bi bi-exclamation-triangle"></i>${' '}
        No healthy workers detected. Builds will queue until a worker comes online.${' '}
        <a href="/workers" data-native class="worker-banner-link" onClick=${e => navLink(e, '/workers')}>View workers</a>
      </div>
    `}
  `;
};

// --- Breadcrumb ---

export const Breadcrumb = ({ team, pipeline, job, resource, showPipelines }) => {
  const link = (href, text) => html`
    <a href=${href} data-native onClick=${e => { e.preventDefault(); route(href); }}>${text}</a>
  `;

  return html`
    <nav id="breadcrumb" aria-label="breadcrumb">
      <ol class="breadcrumb">
        <li class="breadcrumb-item">${link('/', 'Teams')}</li>
        ${team && !pipeline && !showPipelines && html`
          <li class="breadcrumb-item active">${team.name}</li>
        `}
        ${team && (pipeline || showPipelines) && html`
          <li class="breadcrumb-item">${link('/teams/' + team.canonical, team.name)}</li>
          ${showPipelines && !pipeline && html`
            <li class="breadcrumb-item active">Pipelines</li>
          `}
          ${(pipeline || !showPipelines) && html`
            <li class="breadcrumb-item">${link('/teams/' + team.canonical + '/pipelines', 'Pipelines')}</li>
          `}
        `}
        ${pipeline && !job && !resource && html`
          <li class="breadcrumb-item active">${pipeline.name}</li>
        `}
        ${team && pipeline && (job || resource) && html`
          <li class="breadcrumb-item">${link('/teams/' + team.canonical + '/pipelines/' + pipeline.canonical, pipeline.name)}</li>
        `}
        ${job && html`
          <li class="breadcrumb-item active">Jobs</li>
          <li class="breadcrumb-item active">${job.name}</li>
          <li class="breadcrumb-item active">Builds</li>
        `}
        ${resource && html`
          <li class="breadcrumb-item active">Resources</li>
          <li class="breadcrumb-item active">${resource.canonical}</li>
          <li class="breadcrumb-item active">Versions</li>
        `}
      </ol>
    </nav>
  `;
};

// --- Layout (root wrapper) ---

export const Layout = ({ children }) => {
  return html`
    <header>
      <${Header} />
    </header>
    <main class="container">
      <${Notice} />
      <div id="breadcrumb-container"></div>
      ${children}
    </main>
    <footer></footer>
  `;
};

export default Layout;
