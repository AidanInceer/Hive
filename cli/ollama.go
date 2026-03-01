package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
)

// ── Ollama model helpers ───────────────────────────────────────────────────────
// Talk directly to the Ollama HTTP API running on the host.

var ollamaHost = envOr("OLLAMA_HOST", "http://localhost:11434")

// ollamaModel represents a locally available model from Ollama.
type ollamaModel struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// ollamaListModels returns locally available model names.
func ollamaListModels() ([]ollamaModel, error) {
	resp, err := http.Get(ollamaHost + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("cannot reach Ollama at %s: %w", ollamaHost, err)
	}
	defer resp.Body.Close()
	var result struct {
		Models []ollamaModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	sort.Slice(result.Models, func(i, j int) bool {
		return result.Models[i].Name < result.Models[j].Name
	})
	return result.Models, nil
}

// ollamaPullStream starts pulling a model, returns a reader for NDJSON progress.
func ollamaPullStream(modelName string) (io.ReadCloser, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"name":   modelName,
		"stream": true,
	})
	resp, err := http.Post(ollamaHost+"/api/pull", "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("pull request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("pull error %d: %s", resp.StatusCode, string(body))
	}
	return resp.Body, nil
}

// setOrchestratorModel tells the orchestrator to switch models via POST /model.
func setOrchestratorModel(hiveURL, modelName string) error {
	payload, _ := json.Marshal(map[string]string{"model": modelName})
	resp, err := http.Post(hiveURL+"/model", "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

// popularModels lists well-known models users can pull via number keys.
var popularModels = []string{
	"qwen2.5-coder:7b",
	"qwen2.5-coder:14b",
	"qwen2.5-coder:1.5b",
	"deepseek-coder-v2:16b",
	"codellama:7b",
	"llama3.1:8b",
	"mistral:7b",
	"gemma2:9b",
	"phi3:mini",
}
