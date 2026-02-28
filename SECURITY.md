# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it privately via
[GitHub Security Advisories](https://github.com/AidanInceer/Hive/security/advisories/new).

**Do not open a public issue.**

## Scope

- Orchestrator and agent APIs (FastAPI)
- CLI binary (Go)
- Kubernetes manifests and secrets
- Docker images

## Design Principles

- **No hardcoded secrets** — all credentials via env vars / K8s Secrets.
- **Non-root containers** — all images run as `hiveuser`.
- **Minimal base images** — `python:3.12-slim` to reduce attack surface.
- **CORS configured** — tighten `allow_origins` for production deployments.
