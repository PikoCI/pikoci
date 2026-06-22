'use strict';

import { html, render } from 'htm/preact';
import { useState, useEffect } from 'preact/hooks';
import { fetchLocalConfig, saveLocalConfig } from './api.js';
import { setNoticeSuccess } from './state.js';
import { Notice } from './components/Layout.js';
import { Editor } from './components/Editor.js';

function LocalApp() {
  const [config, setConfig] = useState(null);

  useEffect(() => {
    fetchLocalConfig().then(c => setConfig(c));
  }, []);

  if (!config) return null;

  const onSave = async (payload) => {
    await saveLocalConfig({ config: btoa(String.fromCharCode.apply(null, payload.config)) });
  };

  const onSaveSuccess = () => {
    setNoticeSuccess('File saved successfully');
  };

  return html`
    <main class="container">
      <${Notice} />
      <${Editor}
        pipeline=${{ raw: config.raw, name: config.name, canonical: null }}
        teamCanonical="local"
        isLocal=${true}
        onSave=${onSave}
        onSaveSuccess=${onSaveSuccess}
      />
    </main>
  `;
}

render(html`<${LocalApp} />`, document.getElementById('app'));
