'use strict';

import { html } from 'htm/preact';
import { useState, useEffect } from 'preact/hooks';
import {
  fetchOAuthProviders, createOAuthProvider, updateOAuthProvider, deleteOAuthProvider,
  fetchAdminAuthSettings, updateAdminAuthSettings,
} from '../api.js';
import { useRequireAuth, useLoading } from '../hooks.js';
import { showToast } from '../toast.js';
import { getProviderIcon } from '../provider-icons.js';

export function OAuthAdmin() {
  useRequireAuth({ adminOnly: true });

  const [providers, setProviders] = useState([]);
  const [settings, setSettings] = useState(null);
  const [showForm, setShowForm] = useState(false);
  const [editProvider, setEditProvider] = useState(null);

  const load = () => {
    fetchOAuthProviders().then(data => setProviders(data || [])).catch(() => {});
    fetchAdminAuthSettings().then(data => setSettings(data)).catch(() => {});
  };

  useEffect(load, []);

  const onToggleLocal = async () => {
    if (!settings) return;
    const newValue = !settings.local_auth_enabled;
    try {
      await updateAdminAuthSettings({ local_auth_enabled: newValue });
      setSettings({ ...settings, local_auth_enabled: newValue });
      showToast('Auth settings updated', 'success');
    } catch {
      // Re-fetch to revert the checkbox to the actual server state
      fetchAdminAuthSettings().then(data => setSettings(data)).catch(() => {});
    }
  };

  const onDelete = async (canonical) => {
    if (!confirm('Are you sure you want to delete this OAuth provider?')) return;
    try {
      await deleteOAuthProvider(canonical);
      setProviders(providers.filter(p => p.canonical !== canonical));
      showToast('Provider deleted', 'success');
    } catch {}
  };

  const onEdit = (p) => {
    setEditProvider(p);
    setShowForm(true);
  };

  const onFormDone = () => {
    setShowForm(false);
    setEditProvider(null);
    load();
  };

  return html`
    <div class="d-flex align-items-center justify-content-between mb-3">
      <h1 class="h4 fw-bold mb-0">Authentication Settings</h1>
    </div>

    ${settings ? html`
      <div class="card mb-4">
        <div class="card-body">
          <div class="form-check form-switch">
            <input class="form-check-input" type="checkbox" id="localAuth"
              checked=${settings.local_auth_enabled}
              onChange=${onToggleLocal} />
            <label class="form-check-label" for="localAuth">
              Enable local username/password login
            </label>
          </div>
        </div>
      </div>
    ` : null}

    <div class="d-flex align-items-center justify-content-between mb-3">
      <h2 class="h5 fw-bold mb-0">OAuth Providers</h2>
      <button class="btn btn-success btn-sm" onClick=${() => { setEditProvider(null); setShowForm(!showForm); }}>
        <i class="bi bi-plus"></i> Add Provider
      </button>
    </div>

    ${showForm ? html`<${ProviderForm} provider=${editProvider} onDone=${onFormDone} onCancel=${() => { setShowForm(false); setEditProvider(null); }} />` : null}

    ${providers.length > 0 ? html`
      <div class="table-responsive">
        <table class="table table-sm">
          <thead>
            <tr>
              <th>Name</th>
              <th>Canonical</th>
              <th>Type</th>
              <th>Enabled</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            ${providers.map(p => html`
              <tr key=${p.canonical}>
                <td class="d-flex align-items-center gap-2">${getProviderIcon(p.canonical)} ${p.name}</td>
                <td><code>${p.canonical}</code></td>
                <td><span class="badge ${p.type === 'oidc' ? 'bg-primary' : 'bg-info'}">${p.type.toUpperCase()}</span></td>
                <td>${p.enabled
                  ? html`<span class="badge bg-success">Yes</span>`
                  : html`<span class="badge bg-secondary">No</span>`
                }</td>
                <td>
                  <button class="btn btn-outline-primary btn-sm me-1" onClick=${() => onEdit(p)}>
                    <i class="bi bi-pencil"></i>
                  </button>
                  <button class="btn btn-outline-danger btn-sm" onClick=${() => onDelete(p.canonical)}>
                    <i class="bi bi-trash"></i>
                  </button>
                </td>
              </tr>
            `)}
          </tbody>
        </table>
      </div>
    ` : html`
      <div class="text-muted text-center py-4">
        No OAuth providers configured. Add one to enable single sign-on.
      </div>
    `}
  `;
}

function slugify(s) {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
}

const PROVIDER_PRESETS = {
  github: {
    label: 'GitHub',
    type: 'oauth2',
    auth_url: 'https://github.com/login/oauth/authorize',
    token_url: 'https://github.com/login/oauth/access_token',
    userinfo_url: 'https://api.github.com/user',
    scopes: 'user:email',
    username_claim: 'login',
  },
  google: {
    label: 'Google',
    type: 'oidc',
    issuer_url: 'https://accounts.google.com',
    scopes: 'openid email profile',
    username_claim: 'email',
  },
  gitlab: {
    label: 'GitLab',
    type: 'oidc',
    issuer_url: 'https://gitlab.com',
    scopes: 'openid email profile',
    username_claim: 'preferred_username',
  },
  microsoft: {
    label: 'Microsoft',
    type: 'oidc',
    scopes: 'openid email profile',
    username_claim: 'email',
  },
  bitbucket: {
    label: 'Bitbucket',
    type: 'oauth2',
    auth_url: 'https://bitbucket.org/site/oauth2/authorize',
    token_url: 'https://bitbucket.org/site/oauth2/access_token',
    userinfo_url: 'https://api.bitbucket.org/2.0/user',
    scopes: 'account email',
    username_claim: 'username',
  },
  keycloak: {
    label: 'Keycloak',
    type: 'oidc',
    scopes: 'openid email profile',
    username_claim: 'preferred_username',
  },
};

function ProviderForm({ provider, onDone, onCancel }) {
  const isEdit = !!provider;
  const [name, setName] = useState(provider?.name || '');
  const [canonical, setCanonical] = useState(provider?.canonical || '');
  const [canonicalTouched, setCanonicalTouched] = useState(isEdit);
  const [type, setType] = useState(provider?.type || 'oidc');
  const [issuerUrl, setIssuerUrl] = useState(provider?.issuer_url || '');
  const [authUrl, setAuthUrl] = useState(provider?.auth_url || '');
  const [tokenUrl, setTokenUrl] = useState(provider?.token_url || '');
  const [userinfoUrl, setUserinfoUrl] = useState(provider?.userinfo_url || '');
  const [scopes, setScopes] = useState(provider?.scopes || '');
  const [clientId, setClientId] = useState(provider?.client_id || '');
  const [clientSecret, setClientSecret] = useState('');
  const [usernameClaim, setUsernameClaim] = useState(provider?.username_claim || '');
  const [enabled, setEnabled] = useState(provider?.enabled ?? true);
  const [loading, withLoading] = useLoading();
  const [selectedPreset, setSelectedPreset] = useState('');

  const onPresetChange = (key) => {
    const preset = key ? PROVIDER_PRESETS[key] : null;
    setName(preset?.label || '');
    setCanonical(preset ? slugify(preset.label) : '');
    setCanonicalTouched(false);
    setType(preset?.type || 'oidc');
    setIssuerUrl(preset?.issuer_url || '');
    setAuthUrl(preset?.auth_url || '');
    setTokenUrl(preset?.token_url || '');
    setUserinfoUrl(preset?.userinfo_url || '');
    setScopes(preset?.scopes || '');
    setUsernameClaim(preset?.username_claim || '');
  };

  const onSubmit = (e) => {
    e.preventDefault();
    withLoading(async () => {
      const data = {
        name, canonical, type,
        issuer_url: issuerUrl, auth_url: authUrl, token_url: tokenUrl,
        userinfo_url: userinfoUrl, scopes, client_id: clientId,
        client_secret: clientSecret, username_claim: usernameClaim, enabled,
      };
      try {
        if (isEdit) {
          await updateOAuthProvider(provider.canonical, data);
          showToast('Provider updated', 'success');
        } else {
          await createOAuthProvider(data);
          showToast('Provider created', 'success');
        }
        onDone();
      } catch {}
    });
  };

  return html`
    <div class="card mb-3">
      <div class="card-body">
        <h6 class="card-title">${isEdit ? 'Edit' : 'New'} OAuth Provider</h6>
        <form onSubmit=${onSubmit}>
          ${!isEdit ? html`
            <div class="mb-3">
              <label class="form-label">Provider Template</label>
              <div class="d-flex flex-wrap gap-2">
                <button type="button" class="btn btn-outline-secondary btn-sm d-inline-flex align-items-center gap-1 ${!selectedPreset ? 'active' : ''}"
                  onClick=${() => { setSelectedPreset(''); onPresetChange(''); }}>
                  <i class="bi bi-shield-lock"></i> Other
                </button>
                ${Object.entries(PROVIDER_PRESETS).map(([key, p]) => html`
                  <button key=${key} type="button"
                    class="btn btn-outline-secondary btn-sm d-inline-flex align-items-center gap-1 ${selectedPreset === key ? 'active' : ''}"
                    onClick=${() => { setSelectedPreset(key); onPresetChange(key); }}>
                    ${getProviderIcon(key)} ${p.label}
                  </button>
                `)}
              </div>
              <div class="form-text mt-1">Select a provider to pre-fill URLs, scopes, and type. You can edit all fields after.</div>
            </div>
          ` : null}
          <div class="row mb-3">
            <div class="col">
              <label class="form-label">Display Name</label>
              <input type="text" class="form-control" value=${name} required
                placeholder="e.g. GitHub" onInput=${(e) => {
                  setName(e.target.value);
                  if (!canonicalTouched) setCanonical(slugify(e.target.value));
                }} />
            </div>
            <div class="col">
              <label class="form-label">Canonical</label>
              <input type="text" class="form-control" value=${canonical}
                placeholder="e.g. github (auto-generated)" onInput=${(e) => {
                  setCanonical(e.target.value);
                  setCanonicalTouched(true);
                }} />
              <div class="form-text">URL-safe slug. Auto-filled from name.</div>
            </div>
          </div>
          ${canonical ? html`
            <div class="mb-3">
              <label class="form-label">Callback URL</label>
              <div class="input-group">
                <input type="text" class="form-control font-monospace" readonly disabled
                  value=${window.location.origin + '/auth/oauth/' + canonical + '/callback'} />
                <button class="btn btn-outline-secondary" type="button" onClick=${() => {
                  navigator.clipboard.writeText(window.location.origin + '/auth/oauth/' + canonical + '/callback')
                    .then(() => showToast('Copied to clipboard', 'success'))
                    .catch(() => {});
                }}>
                  <i class="bi bi-clipboard"></i>
                </button>
              </div>
              <div class="form-text">Set this as the redirect/callback URI in your provider's settings.</div>
            </div>
          ` : null}
          <div class="mb-3">
            <label class="form-label d-block">Type</label>
            <div class="form-check form-check-inline mt-1">
              <input class="form-check-input" type="radio" id="type-oidc" name="providerType"
                checked=${type === 'oidc'} onChange=${() => setType('oidc')} />
              <label class="form-check-label" for="type-oidc">OIDC</label>
            </div>
            <div class="form-check form-check-inline">
              <input class="form-check-input" type="radio" id="type-oauth2" name="providerType"
                checked=${type === 'oauth2'} onChange=${() => setType('oauth2')} />
              <label class="form-check-label" for="type-oauth2">OAuth2</label>
            </div>
            <div class="form-text">
              ${type === 'oidc'
                ? 'OIDC: Only issuer URL needed. Endpoints are auto-discovered.'
                : 'OAuth2: Provide auth, token, and userinfo URLs manually (e.g. GitHub).'}
            </div>
          </div>
          ${type === 'oidc' ? html`
            <div class="mb-3">
              <label class="form-label">Issuer URL</label>
              <input type="url" class="form-control" value=${issuerUrl} required
                placeholder="https://accounts.google.com" onInput=${(e) => setIssuerUrl(e.target.value)} />
            </div>
          ` : html`
            <div class="mb-3">
              <label class="form-label">Authorization URL</label>
              <input type="url" class="form-control" value=${authUrl} required
                placeholder="https://github.com/login/oauth/authorize" onInput=${(e) => setAuthUrl(e.target.value)} />
            </div>
            <div class="mb-3">
              <label class="form-label">Token URL</label>
              <input type="url" class="form-control" value=${tokenUrl} required
                placeholder="https://github.com/login/oauth/access_token" onInput=${(e) => setTokenUrl(e.target.value)} />
            </div>
            <div class="mb-3">
              <label class="form-label">Userinfo URL</label>
              <input type="url" class="form-control" value=${userinfoUrl}
                placeholder="https://api.github.com/user" onInput=${(e) => setUserinfoUrl(e.target.value)} />
            </div>
          `}
          <div class="row mb-3">
            <div class="col">
              <label class="form-label">Client ID</label>
              <input type="text" class="form-control" value=${clientId} required
                onInput=${(e) => setClientId(e.target.value)} />
            </div>
            <div class="col">
              <label class="form-label">Client Secret</label>
              <input type="password" class="form-control" value=${clientSecret}
                placeholder=${isEdit ? '(unchanged if empty)' : ''}
                onInput=${(e) => setClientSecret(e.target.value)} />
            </div>
          </div>
          <div class="row mb-3">
            <div class="col">
              <label class="form-label">Scopes</label>
              <input type="text" class="form-control" value=${scopes}
                placeholder=${type === 'oidc' ? 'openid email profile' : 'user:email'}
                onInput=${(e) => setScopes(e.target.value)} />
            </div>
            <div class="col">
              <label class="form-label">Username Claim</label>
              <input type="text" class="form-control" value=${usernameClaim}
                placeholder="email" onInput=${(e) => setUsernameClaim(e.target.value)} />
              <div class="form-text">Claim used to suggest username (email, preferred_username, login).</div>
            </div>
          </div>
          <div class="mb-3 form-check">
            <input type="checkbox" class="form-check-input" id="providerEnabled"
              checked=${enabled} onChange=${(e) => setEnabled(e.target.checked)} />
            <label class="form-check-label" for="providerEnabled">Enabled</label>
          </div>
          <button type="submit" class="btn btn-primary" disabled=${loading}>
            ${loading ? 'Saving...' : (isEdit ? 'Update Provider' : 'Create Provider')}
          </button>
          <button type="button" class="btn btn-secondary ms-2" onClick=${onCancel}>Cancel</button>
        </form>
      </div>
    </div>
  `;
}
