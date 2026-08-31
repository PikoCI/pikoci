'use strict';

import { html, render } from 'htm/preact';
import { useEffect } from 'preact/hooks';
import { Router, route } from 'preact-router';
import { showToast } from './toast.js';

// Components
import { Layout } from './components/Layout.js';
import { Login } from './components/Login.js';
import { TeamsView, TeamShow, TeamNew } from './components/Teams.js';
import { PipelineList, PipelineNew } from './components/PipelineList.js';
import { PipelineShow, PipelineEdit } from './components/PipelineShow.js';
import { JobBuilds } from './components/Jobs.js';
import { ResourceVersions } from './components/Resources.js';
import { WorkersList } from './components/Workers.js';
import { UsersList, UserNew, UserShow, Profile } from './components/Users.js';
import { OAuthCallback } from './components/OAuthCallback.js';
import { OAuthCompleteProfile } from './components/OAuthCompleteProfile.js';
import { OAuthAdmin } from './components/OAuthAdmin.js';
import { PipelineSecrets } from './components/Secrets.js';

// Handle OAuth server redirects. The server uses query params on the root path
// (e.g., /?oauth_action=complete-profile&token=...) because HTTP redirects
// don't preserve URL fragments. We detect them here and re-route the SPA.
const sp = new URLSearchParams(window.location.search);
const oauthAction = sp.get('oauth_action');
const oauthError = sp.get('oauth_error');

if (oauthAction === 'callback' || oauthAction === 'complete-profile') {
  // Store params and navigate to the SPA route (preserving query string
  // so the target component can read them from window.location.search).
  const target = oauthAction === 'callback' ? '/auth/callback' : '/auth/complete-profile';
  if (window.location.pathname !== target) {
    window.location.replace(target + window.location.search);
  }
} else if (oauthAction === 'linked') {
  const provider = sp.get('provider') || '';
  setTimeout(() => showToast('Account linked: ' + provider, 'success'), 200);
  window.history.replaceState(null, '', '/profile?tab=linked');
} else if (oauthError) {
  setTimeout(() => showToast(oauthError, 'error'), 200);
  window.history.replaceState(null, '', '/login');
}


function NotFound() {
  useEffect(() => {
    route('/', true);
  }, []);
  return null;
}

function App() {
  return html`
    <${Layout}>
      <${Router}>
        <${Login} path="/login" />
        <${TeamsView} path="/" />
        <${TeamsView} path="/teams" />
        <${TeamNew} path="/teams/new" />
        <${TeamShow} path="/teams/:tc/:tab?" />
        <${PipelineList} path="/teams/:tc/pipelines" />
        <${PipelineNew} path="/teams/:tc/pipelines/new" />
        <${PipelineShow} path="/teams/:tc/pipelines/:pn" />
        <${PipelineEdit} path="/teams/:tc/pipelines/:pn/edit" />
        <${PipelineSecrets} path="/teams/:tc/pipelines/:pn/secrets" />
        <${JobBuilds} path="/teams/:tc/pipelines/:pn/jobs/:jn/builds/:bid?" />
        <${ResourceVersions} path="/teams/:tc/pipelines/:pn/resources/:rCan/versions" />
        <${WorkersList} path="/workers" />
        <${UsersList} path="/users" />
        <${UserNew} path="/users/new" />
        <${UserShow} path="/users/:username" />
        <${Profile} path="/profile" />
        <${OAuthAdmin} path="/admin/auth" />
        <${OAuthCallback} path="/auth/callback" />
        <${OAuthCompleteProfile} path="/auth/complete-profile" />
        <${NotFound} default />
      </${Router}>
    </${Layout}>
  `;
}

render(html`<${App} />`, document.getElementById('app'));
