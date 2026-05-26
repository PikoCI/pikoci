# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Separate queues for jobs and resource checks: long-running jobs no longer block resource version detection. New `--queues` flag lets operators run dedicated check-only or job-only workers ([#368](https://github.com/PikoCI/pikoci/issues/368))
- User management: Profile page, admin Users page, CLI commands, force password change on default `admin/admin123`, and `--users` flag preserves UI/CLI password changes ([#237](https://github.com/PikoCI/pikoci/issues/237))
- Pipeline graph zoom, pan, and fullscreen controls: scroll-wheel zoom toward cursor, click-drag pan (including on nodes), zoom buttons, reset, fullscreen overlay with navbar visible, pinch-zoom on mobile, and auto-sizing that prevents small pipelines from appearing oversized ([#338](https://github.com/PikoCI/pikoci/issues/338))
- Database export: admin-only `GET /admin/export` endpoint, CLI `pikoci client export -o file.db`, and web UI dropdown button to download the full database as a portable SQLite file ([#275](https://github.com/PikoCI/pikoci/issues/275))

### Fixed

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
