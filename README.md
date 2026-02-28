# Hive

**Multi-agent AI system** powered by local LLMs via [Ollama](https://ollama.com).

Hive routes your tasks to specialist AI agents (code reviewer, planner, researcher, debugger, architect) running in Kubernetes, then synthesises their responses into a single coherent answer — all from an interactive terminal UI.

```
$ hive
┌──────────────────────────────────┐
│  HIVE  Multi-Agent AI System     │
│──────────────────────────────────│
│  Select model > qwen2.5-coder:7b│
│  Enter your task: ...            │
│  [working] code_reviewer         │
│  [done]    planner               │
│  [pending] architect             │
└──────────────────────────────────┘
```

## Features

- **Interactive TUI** — Bubbletea-powered terminal interface with real-time streaming
- **Model selection** — Pick from local Ollama models or pull new ones directly from the CLI
- **Multi-agent routing** — LLM-powered task routing to specialist agents
- **Live progress** — See which agents are working, completed, or pending
- **SSE streaming** — Server-Sent Events for real-time updates from orchestrator to CLI
- **Save to file** — Export results as Markdown to the current working directory
- **Auto-tunnel** — Automatically starts `kubectl port-forward` if needed
- **Global install** — Run `hive` from anywhere on your machine
- **Plug & play models** — Switch between any Ollama model at runtime

## Architecture

```
┌─────────┐    SSE     ┌──────────────┐    HTTP    ┌──────────────┐
│  hive   │ ────────── │ Orchestrator │ ────────── │  Agent Pods  │
│  (CLI)  │            │   (FastAPI)  │            │  (FastAPI)   │
└─────────┘            └──────────────┘            └──────────────┘
     │                        │                     code_reviewer
     │                        │                     planner
     │   kubectl              │                     researcher
     │   port-forward         │                     debugger
     │                        │                     architect
     └── Minikube (K8s) ──────┘                          │
                                                         │
                                               ┌─────────┘
                                               ▼
                                         ┌──────────┐
                                         │  Ollama  │
                                         │  (host)  │
                                         └──────────┘
```

**Pipeline:**
1. **Route** — LLM decides which agents are relevant to your task
2. **Fan-out** — Selected agents process your task in parallel
3. **Synthesise** — LLM merges all agent responses into one answer

## Requirements

| Tool | Version | Purpose |
|------|---------|---------|
| [Docker Desktop](https://www.docker.com/products/docker-desktop/) | 20+ | Container runtime |
| [Minikube](https://minikube.sigs.k8s.io/) | 1.30+ | Local Kubernetes cluster |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | 1.28+ | Kubernetes CLI |
| [Ollama](https://ollama.com) | 0.1+ | Local LLM runtime |
| [Go](https://go.dev/dl/) | 1.22+ | Build the CLI (optional if using release binary) |

## Quick Start

### 1. Install prerequisites

```powershell
# Windows (winget)
winget install Docker.DockerDesktop
winget install Kubernetes.minikube
winget install Kubernetes.kubectl
winget install Ollama.Ollama
winget install GoLang.Go
```

```bash
# macOS (brew)
brew install --cask docker
brew install minikube kubectl ollama go
```

### 2. Start services

```powershell
# Start Docker Desktop (GUI or service)
# Then:
minikube start --driver=docker
ollama serve   # or use the system tray app
```

### 3. Pull a model

```powershell
ollama pull qwen2.5-coder:7b   # ~4.7 GB, runs on 8 GB RAM
```

### 4. Build & deploy

```powershell
git clone https://github.com/your-user/Hive.git
cd Hive

# Build Docker images + CLI binary
.\scripts\build.ps1

# Deploy to Minikube
.\scripts\deploy.ps1
```

### 5. Run

```powershell
hive
```

That's it. The CLI auto-starts `kubectl port-forward` and presents an interactive model selector + task prompt.

## Usage

### Interactive mode (recommended)

```
hive
```

Opens the TUI with:
1. **Model selection** — browse local models, pull new ones, or skip
2. **Task input** — type your task
3. **Live streaming** — watch agents work in real-time
4. **Results** — scrollable output with save option

### Direct mode

```
hive "Review this Python function for security issues"
```

### With a specific model

```
hive --model llama3.1:8b "Design a REST API for a todo app"
hive -m qwen2.5-coder:14b
```

### Keyboard shortcuts

| Key | Phase | Action |
|-----|-------|--------|
| `↑/↓` | Model select | Navigate model list |
| `Enter` | Model select | Choose highlighted model |
| `1`-`9` | Model select | Pull popular model by number |
| `p` | Model select | Pull a custom model by name |
| `Tab` | Model select | Skip, use current model |
| `Enter` | Task input | Submit task |
| `Alt+Enter` | Task input | Insert new line |
| `s` | Results | Save output to Markdown file |
| `n` | Results / Error | Start new task |
| `q` | Results / Error | Quit |
| `Ctrl+C` | Any | Cancel / quit |
| `Up/Down` | Results | Scroll |

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `HIVE_URL` | `http://localhost:30800` | Orchestrator URL |
| `OLLAMA_HOST` | `http://localhost:11434` | Ollama API URL |

## Project Structure

```
Hive/
├── .github/workflows/ci.yml   # CI/CD pipeline
├── .editorconfig               # Cross-editor formatting rules
├── .env.example                # Environment variable reference
├── .gitignore
├── .dockerignore
├── AGENTS.md                   # AI agent coding guidelines & learnings
├── CONTRIBUTING.md             # Contributor guide
├── SECURITY.md                 # Security policy
├── pyproject.toml              # Ruff linter/formatter config
├── agent/
│   ├── app/                    # FastAPI agent package
│   │   ├── __init__.py         # App factory & lifecycle hooks
│   │   ├── config.py           # Settings (env vars, defaults)
│   │   ├── prompts.py          # Role-based system prompts
│   │   ├── routes.py           # /task and health endpoints
│   │   └── schemas.py          # Pydantic request/response models
│   ├── Dockerfile              # Multi-stage Python 3.12 image
│   └── requirements.txt
├── orchestrator/
│   ├── app/                    # FastAPI orchestrator package
│   │   ├── __init__.py         # App factory, CORS, lifecycle hooks
│   │   ├── agents.py           # Agent registry, call_agent(), fan_out()
│   │   ├── config.py           # Settings (env vars, defaults)
│   │   ├── llm.py              # LLM client, route(), synthesise()
│   │   ├── routes.py           # /chat, /model, health endpoints
│   │   ├── schemas.py          # Pydantic request/response models
│   │   └── sse.py              # SSE event formatting helper
│   ├── Dockerfile
│   └── requirements.txt
├── cli/
│   ├── main.go                 # Entry point & arg parsing
│   ├── config.go               # Constants & env helpers
│   ├── styles.go               # Lipgloss styles & formatting
│   ├── tunnel.go               # kubectl port-forward management
│   ├── ollama.go               # Ollama API (list/pull models)
│   ├── messages.go             # Tea messages & commands
│   ├── sse.go                  # SSE streaming client
│   ├── tui.go                  # Model struct, Init(), helpers
│   ├── update.go               # Update() — input handling
│   ├── view.go                 # View() — UI rendering
│   ├── go.mod
│   └── go.sum
├── k8s/
│   ├── namespace.yaml
│   ├── configmap.yaml
│   ├── secret.yaml
│   ├── agents/                 # 5 agent deployments + services
│   │   ├── architect.yaml
│   │   ├── code-reviewer.yaml
│   │   ├── debugger.yaml
│   │   ├── planner.yaml
│   │   └── researcher.yaml
│   └── orchestrator/
│       ├── deployment.yaml
│       └── service.yaml
└── scripts/
    ├── build.ps1               # Build images + compile CLI
    ├── deploy.ps1              # Apply K8s manifests
    └── tunnel.ps1              # Manual port-forward
```

## API Endpoints

### Orchestrator (port 9000)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Health check + config info |
| `GET` | `/model` | Current active model |
| `POST` | `/model` | Switch model at runtime |
| `POST` | `/chat` | Synchronous chat (JSON response) |
| `POST` | `/chat/stream` | Streaming chat (SSE events) |

### Agent (port 8000)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Health check |
| `POST` | `/task` | Process a task with role-specific prompt |

## Supported Models

Any model available through Ollama works. Recommended options by RAM:

| RAM | Model | Command |
|-----|-------|---------|
| 8 GB | `qwen2.5-coder:1.5b` | `ollama pull qwen2.5-coder:1.5b` |
| 8 GB | `phi3:mini` | `ollama pull phi3:mini` |
| 16 GB | `qwen2.5-coder:7b` | `ollama pull qwen2.5-coder:7b` |
| 16 GB | `llama3.1:8b` | `ollama pull llama3.1:8b` |
| 32 GB | `qwen2.5-coder:14b` | `ollama pull qwen2.5-coder:14b` |
| 32 GB | `deepseek-coder-v2:16b` | `ollama pull deepseek-coder-v2:16b` |

Or use any OpenAI-compatible API by setting `LLM_BASE_URL` and `LLM_API_KEY` in the ConfigMap/Secret.

## Development

### Rebuild after code changes

```powershell
.\scripts\build.ps1    # Rebuilds images + CLI
.\scripts\deploy.ps1   # Redeploys to K8s
```

### Rebuild just the orchestrator

```powershell
minikube docker-env | Invoke-Expression
docker build -t orchestrator-image:latest ./orchestrator
kubectl rollout restart deployment/hive-orchestrator -n hive
```

### Rebuild just the CLI

```powershell
cd cli
go build -o ..\hive.exe .
Copy-Item ..\hive.exe "$env:USERPROFILE\.local\bin\hive.exe"
```

### View logs

```powershell
kubectl logs -f deployment/hive-orchestrator -n hive
kubectl logs -f deployment/agent-planner -n hive
```

### Check pod status

```powershell
kubectl get pods -n hive
```

## CI/CD

GitHub Actions runs on every push/PR to `main`:

- **Python lint** — Ruff linter + formatter check for agent and orchestrator
- **Go build** — Vet and compile the CLI
- **Docker build** — Validates both Dockerfiles build successfully
- **K8s lint** — Validates all Kubernetes manifests with kubeval
- **Release** — On version tags (`v*`), builds cross-platform binaries (Windows/Linux/macOS) and attaches to GitHub Release

## Troubleshooting

### "hive: orchestrator not reachable on port 30800"
- Is Docker Desktop running?
- Is Minikube started? → `minikube status`
- Are pods healthy? → `kubectl get pods -n hive`

### "model pull failed"
- Is Ollama running? → `ollama list`
- Check available models at https://ollama.com/library

### Port 30800 already in use
```powershell
Get-NetTCPConnection -LocalPort 30800 | Select-Object OwningProcess
Stop-Process -Id <PID> -Force
```

### Pods stuck in CrashLoopBackOff
```powershell
kubectl describe pod <pod-name> -n hive
kubectl logs <pod-name> -n hive --previous
```

## License

MIT
