'use strict';

import { html, render } from 'htm/preact';
import { useEffect } from 'preact/hooks';
import { Router, route } from 'preact-router';

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
        <${TeamShow} path="/teams/:tc" />
        <${PipelineList} path="/teams/:tc/pipelines" />
        <${PipelineNew} path="/teams/:tc/pipelines/new" />
        <${PipelineShow} path="/teams/:tc/pipelines/:pn" />
        <${PipelineEdit} path="/teams/:tc/pipelines/:pn/edit" />
        <${JobBuilds} path="/teams/:tc/pipelines/:pn/jobs/:jn/builds/:bid?" />
        <${ResourceVersions} path="/teams/:tc/pipelines/:pn/resources/:rCan/versions" />
        <${WorkersList} path="/workers" />
        <${UsersList} path="/users" />
        <${UserNew} path="/users/new" />
        <${UserShow} path="/users/:username" />
        <${Profile} path="/profile" />
        <${NotFound} default />
      </${Router}>
    </${Layout}>
  `;
}

render(html`<${App} />`, document.getElementById('app'));
