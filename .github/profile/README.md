<div align="center">

  **The portable CI/CD. One binary, any database, any queue, runs anywhere.**

  [![Website](https://img.shields.io/badge/website-pikoci.com-29ADFF)](https://pikoci.com)
  [![GitHub](https://img.shields.io/github/stars/PikoCI/pikoci?style=social)](https://github.com/PikoCI/pikoci)
  [![License](https://img.shields.io/badge/license-Apache%202.0-yellow)](https://github.com/PikoCI/pikoci/blob/master/LICENSE)
  [![Live Pipeline](https://img.shields.io/badge/live-pipeline-00A83A)](https://ci.pikoci.com/teams/main/pipelines/pikoci)
</div>

---

PikoCI is a self-hosted CI/CD system inspired by [Concourse CI](https://concourse-ci.org), built around a resource/resource-type pipeline model. It runs as a single binary with pluggable database and queue backends — start in memory for development, add SQLite for persistence, scale to PostgreSQL with distributed workers.

### Highlights

- **Single binary** — download and run, no Docker Compose or Kubernetes required
- **HCL pipelines** — Terraform-style syntax, more expressive than YAML
- **Run locally** — `pikoci run -p pipeline.hcl -j test` executes jobs on your laptop
- **Pluggable everything** — databases (SQLite, MySQL, PostgreSQL), queues (NATS, Kafka, RabbitMQ), runners, secret backends
- **Built-in services** — ephemeral databases and caches alongside your jobs
- **Public pipelines** — share build status without requiring authentication

### Quick links

- **Website** — [pikoci.com](https://pikoci.com)
- **Documentation** — [docs.pikoci.com](https://docs.pikoci.com)
- **Live pipeline** — [ci.pikoci.com](https://ci.pikoci.com/teams/main/pipelines/pikoci)
- **Get started** — [Quick Start](https://github.com/PikoCI/pikoci#quick-start)
