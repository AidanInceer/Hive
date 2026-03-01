# AGENTS.md — Hive Coding Guidelines

> **Purpose:** Concise rules for AI coding agents (and humans) working on this codebase.
> Read this before every session. Update the **Learnings** section when mistakes are made.

---

## Project Overview

Hive is a multi-agent AI system: Go CLI (Bubbletea TUI) → FastAPI orchestrator → 5 FastAPI agent pods → Ollama. Runs on Minikube (Docker driver). See `README.md` for full architecture.

## Stack & Versions

| Component | Tech | Version |
|-----------|------|---------|
| Agents / Orchestrator | Python + FastAPI | 3.12, FastAPI 0.115.x |
| CLI | Go + Bubbletea | Go 1.22+, Bubbletea v1 |
| Containers | Docker multi-stage | python:3.12-slim base |
| Orchestration | Kubernetes (Minikube) | 1.28+ |
| LLM runtime | Ollama | 0.1+ |
| CI | GitHub Actions | See `.github/workflows/ci.yml` |

## Rules

### 1. Keep Docs in Sync

- **README.md** — Update whenever you add/remove endpoints, CLI flags, env vars, K8s resources, or change the project structure.
- **AGENTS.md** — Update the Learnings section after any mistake. Update rules if a recurring issue is identified.
- **.env.example** — Must mirror every env var used in code. Add comments for new vars.
- **Docstrings** — Every Python endpoint and Go exported function needs a docstring/comment.
- If you change a default value anywhere, grep and update it **everywhere**: `orchestrator/app/config.py`, `agent/app/config.py`, both `Dockerfile`s, `configmap.yaml`, `.env.example`, `README.md`.

### 2. Python (agent/ & orchestrator/)

- Both services use the `app/` package pattern: `app/__init__.py` creates the FastAPI app, sub-modules handle config, routes, schemas, etc.
- Formatter & linter: **Ruff** (configured in `pyproject.toml`, enforced in CI).
- No unused imports — Ruff will catch them.
- Use `async def` for all FastAPI endpoints.
- Config via `os.getenv()` with sensible defaults — never hardcode secrets. All settings live in `app/config.py`.
- Logging: use the module-level `log` logger, never `print()`.
- Type hints on all function signatures.
- Pydantic models for all request/response schemas (in `app/schemas.py`).
- Keep `requirements.txt` pinned to exact versions.

### 3. Go (cli/)

- CLI is split into multiple files by responsibility: `config.go`, `styles.go`, `tunnel.go`, `ollama.go`, `messages.go`, `sse.go`, `tui.go`, `update.go`, `view.go`.
- Run `go vet ./...` before committing — enforced in CI.
- Run `go mod tidy` after changing dependencies.
- All Bubbletea phases follow the pattern: `Update()` handles messages → returns model + cmd, `View()` renders.
- Keep `newModel()` signature stable — changes break all call sites.
- Use `lipgloss` for all styling, no raw ANSI.
- Errors: wrap with `fmt.Errorf("context: %w", err)`.
- Constants in `config.go`, styles in `styles.go`.

### 4. Docker

- Multi-stage builds: `deps` stage installs packages, `runtime` stage copies only what's needed.
- Always use non-root user (`hiveuser`).
- `imagePullPolicy: Never` — images are built into Minikube's Docker daemon.
- After code changes, rebuild images **into Minikube's daemon**: `minikube docker-env | Invoke-Expression` then `docker build`.

### 5. Kubernetes

- All resources in namespace `hive`.
- Agent deployments named `agent-{role}`, services named `agent-{role}-svc`.
- Orchestrator: `hive-orchestrator` deployment, `hive-orchestrator` service (NodePort 30800→9000).
- Config via `hive-config` ConfigMap + `hive-secret` Secret.
- Always include readiness and liveness probes.
- Resource limits on every container.

### 6. Env Var Consistency

When changing a default model or env var, update **all** of these:
```
orchestrator/app/config.py     →  LLM_MODEL default
agent/app/config.py            →  LLM_MODEL default
orchestrator/Dockerfile        →  ENV LLM_MODEL
agent/Dockerfile               →  ENV LLM_MODEL
k8s/configmap.yaml             →  LLM_MODEL value
.env.example                   →  LLM_MODEL value
README.md                      →  any references to the model name
```

### 7. Testing & Validation

- After any orchestrator change: rebuild image → `kubectl rollout restart` → test endpoints.
- After any CLI change: `go build` → copy to `~/.local/bin/` → test `hive --help`.
- Quick smoke test: `GET /` should return `{"status":"ok","version":"...","model":"...","agents":[...]}`.
- Check pod health: `kubectl get pods -n hive` — all should be `1/1 Running`.

### 8. Git & CI

- Branch from `main`, PR back to `main`.
- CI runs: Ruff lint, Go vet+build, Docker build, kubeval K8s validation.
- Release tags: `v*` triggers cross-platform binary builds.
- Don't commit `.env`, `hive.exe`, or `__pycache__/`.

---

## Learnings

> Add entries here when mistakes are made during coding sessions. Format: date, what happened, the fix, and the rule to prevent recurrence.

| Date | Mistake | Fix | Prevention |
|------|---------|-----|------------|
| 2026-02-28 | Default model (`qwen3-coder-next`) was set in some files but not others, causing mismatches | Grep'd all files and updated to `qwen2.5-coder:7b` everywhere | Follow Rule 6 — update all locations listed when changing a default |
| 2026-02-28 | Agent deployment names assumed `hive-` prefix but actual names use `agent-` prefix | Used `kubectl get deployments -n hive` to find correct names | Always verify K8s resource names with `kubectl get` before scripting rollouts |
| 2026-02-28 | Model too large (50 GB) for available RAM (18.8 GB) | Switched to `qwen2.5-coder:7b` (~4.7 GB, Q4_K_M) | Check model size against available RAM before pulling. See README RAM table |
| 2026-02-28 | PowerShell treats Docker stderr as errors (exit code 1) even on successful builds | Ignore exit code when Docker stderr contains build success markers | Append `2>&1` and check output content, not just exit code |
| 2026-02-28 | After modularizing orchestrator into `app/` package, `__init__.py` referenced bare `LLM_MODEL` instead of `config.LLM_MODEL` — caused `NameError` at startup | Changed to `config.LLM_MODEL` (module was already imported as `from . import config`) | When splitting a monolith into modules, verify every name reference uses the correct import path — don't leave bare names from the old single-file layout |

---

## Session Checklist

Use this before starting and ending a coding session:

### Start of session
- [ ] `minikube status` — cluster running?
- [ ] `kubectl get pods -n hive` — pods healthy?
- [ ] `ollama list` — model available?
- [ ] Read this file's Learnings section

### End of session
- [ ] All changes tested (endpoints, CLI, pods)?
- [ ] `README.md` updated if structure/API/flags changed?
- [ ] `AGENTS.md` Learnings updated if mistakes were made?
- [ ] `.env.example` updated if new env vars added?
- [ ] Images rebuilt if Python code changed?
- [ ] `go mod tidy` run if Go deps changed?
