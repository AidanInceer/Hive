FROM python:3.12-slim AS deps

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir --upgrade pip \
 && pip install --no-cache-dir -r requirements.txt

# ── Runtime ────────────────────────────────────────────────────────────────────
FROM python:3.12-slim AS runtime

WORKDIR /app

COPY --from=deps /usr/local/lib/python3.12 /usr/local/lib/python3.12
COPY --from=deps /usr/local/bin /usr/local/bin

COPY orchestrator.py .

ENV LLM_BASE_URL="http://host.docker.internal:11434/v1"
ENV LLM_MODEL="qwen3-coder-next:latest"
ENV MAX_TOKENS=4096
ENV TEMPERATURE=0.6
ENV AGENT_TIMEOUT=120

EXPOSE 9000

RUN adduser --disabled-password --no-create-home --gecos "" hiveuser \
 && chown -R hiveuser /app
USER hiveuser

CMD ["uvicorn", "orchestrator:app", "--host", "0.0.0.0", "--port", "9000", "--workers", "1"]
