# PikoCI Documentation

PikoCI is a portable, self-hosted CI/CD system. One binary, any database, runs anywhere.

## Pages

- [Getting Started](Getting-Started.md) - Install and run your first pipeline
- [Pipeline Reference](Pipeline.md) - HCL pipeline configuration
- [Resource Types](Resource-Types.md) - Built-in and custom resource types
- [Runners](Runners.md) - Built-in and custom runners
- [Secret Types](Secret-Types.md) - Built-in and custom secret types
- [Services](Services.md) - Ephemeral per-job services
- [Notifications](Notifications.md) - Fire-and-forget notifications (GitHub checks, Slack, Discord)
- [Server Configuration](Server.md) - Server flags and options
- [Database Backends](Database.md) - Supported database systems
- [Functions](Functions.md) - HCL functions available in pipelines (Terraform-compatible)
- [for_each and matrix](Pipeline.md#for_each) - Generate multiple job instances from a single definition
- [Variables](Variables.md) - Pipeline variables
- [Roles & Permissions](Roles.md) - Team roles and what each role can do
- [CLI Reference](CLI.md) - Client commands and flags
- [Pause / Unpause](Pause.md) - Temporarily stop pipelines or jobs
- [Resource Pinning](Resource-Pinning.md) - Pin resources to a version and trigger with specific versions
- [Public Pipelines](Public-Pipelines.md) - Sharing pipeline status publicly
- [Scaling](Scaling.md) - From single binary to distributed production
- [Running Workers Separately](Workers.md) - Distributed worker setup
- [Deployment](Deployment.md) - Production deployment guide
- [Portability and Bundling](Portability.md) - Bundle and deploy anywhere
- [Coming from Concourse](Concourse.md) - Migration guide for Concourse users
