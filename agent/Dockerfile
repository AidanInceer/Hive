# ── Stage 1: dependency layer ──────────────────────────────────────────────────
FROM python:3.12-slim AS deps

WORKDIR /app

# System deps needed to compile some Python packages
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential \
        curl \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir --upgrade pip \
 && pip install --no-cache-dir -r requirements.txt

# ── Stage 2: runtime image ─────────────────────────────────────────────────────
FROM python:3.12-slim AS runtime

WORKDIR /app

# Copy installed packages from the deps stage
COPY --from=deps /usr/local/lib/python3.12 /usr/local/lib/python3.12
COPY --from=deps /usr/local/bin /usr/local/bin

# Copy application source
COPY agent.py .

# ── Environment variables (overridable at `docker run` time) ───────────────────
# AGENT_ROLE controls the agent's personality / system prompt.
# Supported values: code_reviewer | planner | researcher | debugger | architect | default
ENV AGENT_ROLE=default

# LLM_API_KEY: set to your remote API key when not using Ollama.
#   Ollama does not require a real key — the default "ollama" is fine.
#   For remote APIs, inject at runtime: docker run -e LLM_API_KEY=sk-...
#   Never hardcode secret values here.
ENV LLM_BASE_URL="http://host.docker.internal:11434/v1"
ENV LLM_MODEL="qwen3-coder-next:latest"

# Tuning knobs
ENV MAX_TOKENS=4096
ENV TEMPERATURE=0.6

# ── Networking ─────────────────────────────────────────────────────────────────
EXPOSE 8000

# ── Non-root user for security ─────────────────────────────────────────────────
RUN adduser --disabled-password --no-create-home --gecos "" hiveuser \
 && chown -R hiveuser /app
USER hiveuser

# ── Launch ─────────────────────────────────────────────────────────────────────
# If TASK_JSON is set the agent runs in batch mode (K8s Job) and exits.
# Otherwise it starts the HTTP server for persistent deployment use.
CMD ["python", "agent.py"]
