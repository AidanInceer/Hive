package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	tea "github.com/charmbracelet/bubbletea"
)

// ── HTTP helpers for /commands ─────────────────────────────────────────────────

func fetchConfig(hiveURL string) (map[string]string, error) {
	resp, err := http.Get(hiveURL + "/config")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server %d: %s", resp.StatusCode, string(body))
	}
	var data map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

func fetchAgents(hiveURL string) ([]string, error) {
	resp, err := http.Get(hiveURL + "/agents")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server %d: %s", resp.StatusCode, string(body))
	}
	var result struct {
		Agents []string `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Agents, nil
}

func postConfig(hiveURL, key, value string) error {
	payload, _ := json.Marshal(map[string]string{key: value})
	resp, err := http.Post(hiveURL+"/config", "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

// ── Tea commands ───────────────────────────────────────────────────────────────

func fetchConfigCmd(hiveURL string) tea.Cmd {
	return func() tea.Msg {
		data, err := fetchConfig(hiveURL)
		if err != nil {
			return configErrMsg{err}
		}
		return configDataMsg{data}
	}
}

func fetchAgentsCmd(hiveURL string) tea.Cmd {
	return func() tea.Msg {
		agents, err := fetchAgents(hiveURL)
		if err != nil {
			return agentsErrMsg{err}
		}
		return agentsListMsg{agents}
	}
}

func setConfigCmd(hiveURL, key, value string) tea.Cmd {
	return func() tea.Msg {
		if err := postConfig(hiveURL, key, value); err != nil {
			return configSetErrMsg{err}
		}
		return configSetOKMsg{key, value}
	}
}
