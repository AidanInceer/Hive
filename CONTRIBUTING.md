# Contributing to Hive

## Getting Started

1. Fork the repo and clone locally.
2. Follow the [Quick Start](README.md#quick-start) to set up your environment.
3. Read [AGENTS.md](AGENTS.md) for coding guidelines.

## Development Workflow

1. Create a feature branch from `main`.
2. Make changes following the rules in `AGENTS.md`.
3. Run validation:
   ```bash
   # Python
   ruff check agent/ orchestrator/
   ruff format --check agent/ orchestrator/

   # Go
   cd cli && go vet ./... && go build .

   # K8s
   find k8s -name '*.yaml' -exec kubeval --strict {} +
   ```
4. Open a PR against `main`.

## Commit Messages

Use clear, imperative-mood messages:
- `feat: add model search to TUI`
- `fix: handle empty agent response`
- `docs: update API endpoint table`
- `refactor: modularize orchestrator into package`

## Code Style

- **Python**: Ruff (config in `pyproject.toml`). Type hints required. Async endpoints.
- **Go**: `go vet`, `gofmt`. Lipgloss for styling. Error wrapping with `%w`.
- **YAML**: 2-space indent.

## Adding a New Agent

1. Add role + system prompt to `agent/app/prompts.py`.
2. Create K8s deployment+service in `k8s/agents/<role>.yaml`.
3. Add URL to `orchestrator/app/agents.py` `_DEFAULT_AGENT_URLS`.
4. Update `scripts/deploy.ps1` deployment list.
5. Update `README.md` architecture diagram and agent list.
