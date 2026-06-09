# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Worker health monitoring: workers send periodic heartbeats to the server, which tracks their status as healthy or stale (no heartbeat for over 90 seconds). Admin users see all workers in a new dashboard at `#workers` with name, status, queues, platform, version, uptime, and last seen time. A warning banner appears when no healthy workers are detected ([#482](https://github.com/PikoCI/pikoci/issues/482))
- Delete stale workers: admins can remove stale workers from the dashboard via a delete button or the `DELETE /workers/{name}` API endpoint. This only removes the registration from the database, it does not signal the worker process
- Prometheus `pikoci_workers` gauge metric with `status` label (healthy/stale) for monitoring and alerting on worker availability

## [0.4.0] - 2026-06-08

### Added

- Runtime variable interpolation in notification messages: `message` fields on `notify` steps and `notification` blocks now expand `$VAR` references at build time (`$BUILD_*`, `$GET_*`, `$TASK_*`). Literal `\n` sequences are converted to real newlines, enabling multiline content like changelogs from `$PIKOCI_OUTPUT` ([#477](https://github.com/PikoCI/pikoci/issues/477))
- `for_each` and `matrix` on jobs: generate multiple job instances from a single definition using `for_each = toset(...)`, `for_each = { key = "value" }`, or `matrix { axis = [...] }`. Each instance gets independent builds, status, and logs. Downstream `passed` constraints automatically fan-in across all instances. Also expands HCL functions to match Terraform (`startswith`, `endswith`, `base64encode`/`decode`, `urlencode`, `timestamp`, `formatdate`, `alltrue`, `anytrue`, `one`, `sum`, `transpose`, `toset`, `setproduct`, `setintersection`, `setunion`, type conversions, and more) ([#193](https://github.com/PikoCI/pikoci/issues/193), [#472](https://github.com/PikoCI/pikoci/issues/472))
- Include resource name in webhook URLs for readability: tokens now use `{canonical}_{uuid}` format instead of bare UUIDs. Old tokens continue to work unchanged ([#470](https://github.com/PikoCI/pikoci/issues/470))
- Add `serial_groups` on jobs for cross-job mutual exclusion ([#186](https://github.com/PikoCI/pikoci/issues/186))
- Step metadata export: get steps auto-forward version metadata to subsequent steps as `GET_<STEPNAME>_<KEY>` env vars, and task steps can write `KEY=VALUE` lines to `$PIKOCI_OUTPUT` to export custom values as `TASK_<STEPNAME>_<KEY>` env vars ([#459](https://github.com/PikoCI/pikoci/issues/459))
- Loading indicators on action buttons: all mutating buttons (trigger, cancel, retry, delete, pause, unpause, pin, etc.) now show a spinner and loading text while the request is in flight, preventing double-clicks and providing visual feedback ([#453](https://github.com/PikoCI/pikoci/issues/453))
- Built-in `shell` runner for simplified shell command execution. Supports inline `cmd` mode (`run "shell" { cmd = "..." }`) and script `file` mode (`run "shell" { file = "script.sh" }`), with optional `shell` param to override the default `/bin/sh` ([#458](https://github.com/PikoCI/pikoci/issues/458))

## [0.3.0] - 2026-06-03

### Added

- Job-level timeout: set `timeout` on a job (e.g. `timeout = "30m"`) to limit total wall-clock time for plan steps. When exceeded the build fails with a "job timed out" error and `on_cancel`/`ensure` hooks still run ([#60](https://github.com/PikoCI/pikoci/issues/60))
- Resource pinning and trigger with specific version: pin a resource to lock the pipeline to a known-good version — the scheduler, worker, and manual job triggers all respect the pin. Trigger downstream jobs with any specific version via the play button. Pinned resources show an amber border in the pipeline graph and a sticky banner on the versions page. Pinning cancels mismatched pending builds. Cron resource GET steps now print the trigger date ([#151](https://github.com/PikoCI/pikoci/issues/151))
- Pause/unpause pipelines and jobs: temporarily stop build triggering without deleting configuration or history. Pausing a pipeline pauses all its jobs; unpausing individual jobs is supported. Resource checks continue running while paused. Paused jobs appear blue in the pipeline graph ([#148](https://github.com/PikoCI/pikoci/issues/148))
- Persistent resource caching: `cache = true` on `resource_type` enables a persistent `$CACHE_DIR` for check and pull scripts, with per-resource override support. The built-in `git` resource type uses caching by default — maintaining a bare clone for faster `git fetch` checks and `--reference-if-able` pulls ([#216](https://github.com/PikoCI/pikoci/issues/216))
- Type-level runner overrides: `resource_type`, `notification_type`, `secret_type`, and `service_type` definitions can specify a `runner` block to override where their exec commands execute, e.g. run inside Docker without modifying the type's command definitions ([#221](https://github.com/PikoCI/pikoci/issues/221))
- Floating toast notifications for success and error feedback on all mutating actions (trigger, delete, retry, cancel, etc.) with auto-dismiss and dark mode support ([#351](https://github.com/PikoCI/pikoci/issues/351))
- File secret type: auto-detect format from file extension (`json`, `yaml`, `env`, `raw`), add `json` and `yaml` as explicit format options, and fix multi-line JSON parsing ([#435](https://github.com/PikoCI/pikoci/issues/435))
- Built-in `artifact` resource type for passing build outputs between jobs using local filesystem storage with content-addressable deduplication, implicit get-after-put for `passed` constraint support, and `$CACHE_DIR` for push steps ([#150](https://github.com/PikoCI/pikoci/issues/150))
- First-class notification entities: `notification_type`, `notification`, and `notify` steps for fire-and-forget notifications with automatic dispatch, job scoping, and `github-check`/`slack`/`discord` notification types ([#154](https://github.com/PikoCI/pikoci/issues/154))
- SVG and PNG pipeline graph formats for embedding in READMEs and dashboards ([#332](https://github.com/PikoCI/pikoci/issues/332))

### Changed

- Removed `.json` URL suffix trick from API endpoints ([#427](https://github.com/PikoCI/pikoci/issues/427))

### Fixed

- After first-login forced password change, user stays on profile page instead of being redirected to the teams page ([#442](https://github.com/PikoCI/pikoci/issues/442))
- Build status transitions not visible in real-time on non-active build tabs in the job builds page ([#444](https://github.com/PikoCI/pikoci/issues/444))
- Toast notifications overlap action buttons on wide screens and cannot be dismissed by clicking ([#443](https://github.com/PikoCI/pikoci/issues/443))
- Job builds view defaults to newest build instead of navigating to the running/pending build ([#441](https://github.com/PikoCI/pikoci/issues/441))
- Pipelines link on team edit page causes full page reload instead of client-side navigation ([#445](https://github.com/PikoCI/pikoci/issues/445))
- Git resource tag mode returns all tags on every check, triggering builds for every historical tag after pipeline recreation ([#419](https://github.com/PikoCI/pikoci/issues/419))
- Pipeline graph shows original build color instead of successful retry when a new build is running ([#421](https://github.com/PikoCI/pikoci/issues/421))
- Pipeline graph shows gray (pending) for jobs with pending + running + succeeded builds instead of the succeeded color ([#429](https://github.com/PikoCI/pikoci/issues/429))

## [0.2.2] - 2026-05-29

### Added

- Local pipeline editor: `pikoci pipeline edit ./file.hcl` opens the browser-based editor for local HCL files with live graph preview and save-to-disk ([#353](https://github.com/PikoCI/pikoci/issues/353))
- `base_url` param for `github-check` resource type to auto-construct a Details link on GitHub check runs from build metadata ([#257](https://github.com/PikoCI/pikoci/issues/257))
- Display current deployed version and commit hash in the web UI header dropdown and via `/version.json` API endpoint ([#392](https://github.com/PikoCI/pikoci/issues/392))
- `--version` CLI flag to print the application version and commit hash ([#392](https://github.com/PikoCI/pikoci/issues/392))
- Docker pull and run instructions in README Quick Start ([#387](https://github.com/PikoCI/pikoci/issues/387))

### Changed

- Refactored frontend JavaScript from a single inline `<script>` block into ES modules under `js/app/` ([#317](https://github.com/PikoCI/pikoci/issues/317))

### Fixed

- Webhook tokens lost on every pipeline update, causing GitHub webhooks to return 400 ([#408](https://github.com/PikoCI/pikoci/issues/408))
- Active build view not refreshing steps and logs during execution ([#411](https://github.com/PikoCI/pikoci/issues/411))
- Scheduler keeps creating pending builds while a build is already running ([#413](https://github.com/PikoCI/pikoci/issues/413))
- Re-enqueue pending builds on server startup so they are not stranded forever ([#399](https://github.com/PikoCI/pikoci/issues/399))
- Error banner persists after backend reconnects: polling successes now clear errors, connection failures show "Connection lost. Retrying...", and polling errors are debounced ([#400](https://github.com/PikoCI/pikoci/issues/400))

## [0.2.1] - 2026-05-27

### Added

- Comprehensive godoc documentation across all packages: package-level comments, exported types, interfaces, functions, methods, and constants ([#395](https://github.com/PikoCI/pikoci/issues/395))
- Go Reference badge on README

### Changed

- Renamed Go module path from `github.com/xescugc/pikoci` to `github.com/pikoci/pikoci` ([#397](https://github.com/PikoCI/pikoci/issues/397))

### Added

- Lazy loading (infinite scroll) for job builds and resource versions: only the newest 50 items load initially, with older items fetched on scroll and new items appearing via polling. CLI backward compatibility preserved with `limit=0`. Cursor-based pagination uses `before`/`after`/`limit` query params with `has_more` metadata ([#345](https://github.com/PikoCI/pikoci/issues/345))

## [0.2.0] - 2026-05-27

### Added

- Documentation site at docs.pikoci.com using MkDocs Material with search, PICO-8 themed light/dark mode, and auto-deploy on master push ([#377](https://github.com/PikoCI/pikoci/issues/377))
- Separate queues for jobs and resource checks: long-running jobs no longer block resource version detection. New `--queues` flag lets operators run dedicated check-only or job-only workers ([#368](https://github.com/PikoCI/pikoci/issues/368))
- User management: Profile page, admin Users page, CLI commands, force password change on default `admin/admin123`, and `--users` flag preserves UI/CLI password changes ([#237](https://github.com/PikoCI/pikoci/issues/237))
- Pipeline graph zoom, pan, and fullscreen controls: scroll-wheel zoom toward cursor, click-drag pan (including on nodes), zoom buttons, reset, fullscreen overlay with navbar visible, pinch-zoom on mobile, and auto-sizing that prevents small pipelines from appearing oversized ([#338](https://github.com/PikoCI/pikoci/issues/338))
- Database export: admin-only `GET /admin/export` endpoint, CLI `pikoci client export -o file.db`, and web UI dropdown button to download the full database as a portable SQLite file ([#275](https://github.com/PikoCI/pikoci/issues/275))

### Fixed

- Drain hanging on SIGQUIT: `Drain()` now cancels the receive context immediately so blocked `Receive()` calls unblock without waiting for the drain timeout ([#390](https://github.com/PikoCI/pikoci/issues/390))
- Documentation: update stale GitHub/Docker URLs to PikoCI org and GHCR, fix outdated claims, add missing docs for `run`, `export`, `pipelines rename`, `on_cancel` hook, and `/metrics` endpoint ([#388](https://github.com/PikoCI/pikoci/issues/388))
- Trigger Resource new version appears at bottom of versions list instead of at top ([#380](https://github.com/PikoCI/pikoci/issues/380))
- SIGQUIT graceful shutdown hanging forever: cancel the main context after workers drain so blocked Receive() calls unblock and the process exits cleanly ([#384](https://github.com/PikoCI/pikoci/issues/384))
- Worker stuck in loop after build cancellation: use parent server context for DB operations so writes survive job cancellation but respect server shutdown ([#382](https://github.com/PikoCI/pikoci/issues/382))
- Follow button: fix race conditions between auto-scroll and scroll listeners, target running step in multi-step builds, and improve visual feedback with "Following"/"Follow" text and solid/outline styles ([#343](https://github.com/PikoCI/pikoci/issues/343))
- Pending builds showing gray on pipeline graph instead of previous build's color with dashed border
- Concurrency re-queuing infinite loop: builds are now created as Pending at trigger time and transitioned to Started atomically by workers, eliminating duplicate build creation from re-queued messages ([#358](https://github.com/PikoCI/pikoci/issues/358), [#246](https://github.com/PikoCI/pikoci/issues/246))
- Resource "Last checked" showing raw duration (e.g., "739760d ago") instead of "never" for resources that have never been checked ([#367](https://github.com/PikoCI/pikoci/issues/367))

### Changed

- CLI outputs JSON instead of Go struct dumps for all commands, making output parseable and pipeable to tools like `jq` ([#360](https://github.com/PikoCI/pikoci/issues/360))
- Make web UI mobile-friendly: viewport meta tag, hamburger menu, responsive CSS for graphs, logs, tables, and touch targets; login page centered without scroll ([#350](https://github.com/PikoCI/pikoci/issues/350))
- Build detail and resource views now show relative timestamps ("5m ago") with absolute time on hover ([#344](https://github.com/PikoCI/pikoci/issues/344))
- Pipeline editor: clicking a block or graph node now scrolls it to the top of the viewport, and resource blocks in the sidebar show `type.label` (e.g., `git.pikoci_pr`) instead of just the type ([#336](https://github.com/PikoCI/pikoci/issues/336))

## [0.1.0] - 2026-05-23

### Added

- Local pipeline execution: `pikoci run -p pipeline.hcl -j test` runs any job locally without a server. Supports `--resource type.name=path` overrides and `--var key=value` ([#161](https://github.com/PikoCI/pikoci/issues/161))
- Pipeline rename via UI and CLI through the existing `UpdatePipeline` flow ([#337](https://github.com/PikoCI/pikoci/issues/337))
- Pipeline names with spaces and special characters, auto-slugified for URLs ([#333](https://github.com/PikoCI/pikoci/issues/333))
- Full CLI for all API operations: pipelines, jobs, builds, resources, teams, users, triggers ([#327](https://github.com/PikoCI/pikoci/issues/327))
- CodeMirror HCL editor for pipeline create/edit with syntax highlighting, block panel, and graph preview ([#320](https://github.com/PikoCI/pikoci/issues/320))
- Job concurrency limits ([#319](https://github.com/PikoCI/pikoci/issues/319))
- Job build retry: re-run builds with `PARENT.N` numbering (e.g. "3.1", "3.2") via UI or API ([#312](https://github.com/PikoCI/pikoci/issues/312))
- Job cancellation with `on_cancel` hook via DB polling ([#321](https://github.com/PikoCI/pikoci/issues/321))
- Built-in trigger resource type for manual triggers ([#326](https://github.com/PikoCI/pikoci/issues/326))
- Worker authentication via `--worker-token` for distributed deployments ([#309](https://github.com/PikoCI/pikoci/issues/309))
- Sequential build numbers per job instead of global DB IDs ([#310](https://github.com/PikoCI/pikoci/issues/310))
- `inputs` and `outputs` on task steps for declarative filesystem checks ([#177](https://github.com/PikoCI/pikoci/issues/177))
- Live status toggle on pipelines list for auto-refreshing graph images ([#282](https://github.com/PikoCI/pikoci/issues/282))
- Graceful shutdown with `SIGQUIT`: drains in-flight jobs before exiting ([#281](https://github.com/PikoCI/pikoci/issues/281))
- Live build log streaming with 2s DB flushes and auto-scroll ([#13](https://github.com/PikoCI/pikoci/issues/13))
- Sticky step header and auto-scroll for long build logs ([#318](https://github.com/PikoCI/pikoci/issues/318))
- Secret-backed variables: `secret` block on variables for lazy runtime resolution from secret types
- GitHub Checks support via `github-check` resource type and build metadata env vars ([#179](https://github.com/PikoCI/pikoci/issues/179))
- `service_type` blocks for ephemeral per-job processes (databases, caches) with ready_check polling ([#227](https://github.com/PikoCI/pikoci/issues/227))
- `secret_type` and `secret` blocks with built-in `pikoci://vault` and `pikoci://file` types ([#12](https://github.com/PikoCI/pikoci/issues/12))
- `raw` and `env` format support for the built-in `file` secret type
- `attempts` field on steps for automatic retry on failure ([#174](https://github.com/PikoCI/pikoci/issues/174))
- `timeout` field on steps for execution time limits ([#173](https://github.com/PikoCI/pikoci/issues/173))
- `source` field on `resource_type`, `runner_type`, and `service_type` for URL-based definitions ([#11](https://github.com/PikoCI/pikoci/issues/11))
- Built-in `git` resource type with API-aware check for GitHub/GitLab
- Built-in `docker` runner ([#206](https://github.com/PikoCI/pikoci/issues/206))
- HCL standard functions (string, collection, numeric, encoding, regex) ([#104](https://github.com/PikoCI/pikoci/issues/104))
- Public pipelines: read-only views of graph, jobs, builds, and resources without authentication ([#100](https://github.com/PikoCI/pikoci/issues/100))
- Webhook triggers for resources with token regeneration ([#144](https://github.com/PikoCI/pikoci/issues/144))
- Ordered plan execution and `put` step support for CD workflows ([#169](https://github.com/PikoCI/pikoci/issues/169))
- Users and teams with role-based access control ([#124](https://github.com/PikoCI/pikoci/pull/124))
- PostgreSQL, RabbitMQ, and Kafka backend support ([#128](https://github.com/PikoCI/pikoci/pull/128))
- `/metrics` endpoint for Prometheus scraping ([#234](https://github.com/PikoCI/pikoci/issues/234))
- Database-polling scheduler for horizontal scaling with PostgreSQL/MySQL ([#213](https://github.com/PikoCI/pikoci/issues/213))
- Documentation wiki with GitHub Actions sync ([#211](https://github.com/PikoCI/pikoci/issues/211))
- Deployment infrastructure: systemd unit, Docker Compose, Caddy reverse proxy, deploy script
- Bootstrap Icons across the UI with copy-to-clipboard on build logs ([#254](https://github.com/PikoCI/pikoci/issues/254))
- pikoci.com landing page with automated deploys

### Changed

- Unified `--pipeline-vars` to `--vars`/`-v` across server, client, and run commands
- Replaced `urfave/cli` + `koanf` with `cobra` + `viper` for CLI and config ([#238](https://github.com/PikoCI/pikoci/issues/238))
- Replaced `mattn/go-sqlite3` (CGO) with `modernc.org/sqlite` (pure Go) for cross-compilation
- Replaced shellquote splitting with native HCL list args for runner commands ([#201](https://github.com/PikoCI/pikoci/issues/201))
- Passed constraints now require a common resource version across all upstream jobs ([#253](https://github.com/PikoCI/pikoci/issues/253))
- `--users` flag is idempotent: existing users get password updated ([#232](https://github.com/PikoCI/pikoci/issues/232))
- Redesigned UI with PICO-8 color palette, dark mode, and modernized layout ([#133](https://github.com/PikoCI/pikoci/issues/133))
- Replaced pixel-art logo with hexagonal SVG logo ([#202](https://github.com/PikoCI/pikoci/issues/202))
- Migrated Docker images from Docker Hub to GHCR (`ghcr.io/pikoci/pikoci`)
- Renamed QID to PikoCI ([#136](https://github.com/PikoCI/pikoci/issues/136))

### Fixed

- Pipeline graph shows retry status instead of original build status ([#334](https://github.com/PikoCI/pikoci/issues/334))
- Job status display on pipeline view ([#325](https://github.com/PikoCI/pikoci/issues/325))
- Duplicate resource version rows on poll refresh ([#315](https://github.com/PikoCI/pikoci/issues/315))
- Hide retry and cancel buttons on public pipeline views ([#331](https://github.com/PikoCI/pikoci/issues/331))
- Hide unlinked resources from pipeline graph ([#256](https://github.com/PikoCI/pikoci/issues/256))
- Browser back navigation from build page ([#263](https://github.com/PikoCI/pikoci/issues/263))
- Accordion closing on live updates ([#289](https://github.com/PikoCI/pikoci/issues/289))
- Secret fetch commands running from wrong directory
- SPA catch-all intercepting API requests ([#140](https://github.com/PikoCI/pikoci/issues/140))
- Entity still shown in collection when backend creation fails ([#123](https://github.com/PikoCI/pikoci/issues/123))
- User permission changes not reflected until re-login ([#126](https://github.com/PikoCI/pikoci/pull/126))
- CLI `pipelines update` using POST instead of PUT ([#339](https://github.com/PikoCI/pikoci/issues/339))
- Update handler preserving `team_canonical` from URL after JSON decode

[unreleased]: https://github.com/PikoCI/pikoci/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/PikoCI/pikoci/releases/tag/v0.1.0
