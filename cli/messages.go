package main

import (
	"bufio"
	"encoding/json"
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Phase enum ─────────────────────────────────────────────────────────────────

type appPhase int

const (
	phaseModelSelect appPhase = iota
	phaseModelPull
	phaseInput
	phaseConnecting
	phaseRouting
	phaseFanOut
	phaseSynthesis
	phaseResult
	phaseError
)

func (p appPhase) String() string {
	switch p {
	case phaseModelSelect:
		return "Select model"
	case phaseModelPull:
		return "Pulling model..."
	case phaseInput:
		return "Input"
	case phaseConnecting:
		return "Connecting..."
	case phaseRouting:
		return "Routing task to agents..."
	case phaseFanOut:
		return "Agents working..."
	case phaseSynthesis:
		return "Synthesising results..."
	case phaseResult:
		return "Complete"
	case phaseError:
		return "Error"
	}
	return ""
}

// ── Agent tracking ─────────────────────────────────────────────────────────────

type agentInfo struct {
	name   string
	status string // "pending" | "working" | "done"
}

func (a agentInfo) render() string {
	switch a.status {
	case "done":
		return successStyle.Render("  ✔ ") + a.name
	case "working":
		return warnStyle.Render("  ◌ ") + accentStyle.Render(a.name)
	default:
		return subtleStyle.Render("  ─ " + a.name)
	}
}

// ── Tea messages ───────────────────────────────────────────────────────────────

// Model selection
type modelsLoadedMsg struct{ models []ollamaModel }
type modelsErrMsg struct{ err error }
type pullProgressMsg struct {
	status    string
	total     int64
	completed int64
	done      bool
}
type pullErrMsg struct{ err error }
type modelSetMsg struct{ name string }
type modelSetErrMsg struct{ err error }

// SSE streaming
type streamStartedMsg struct {
	reader *bufio.Reader
	body   io.ReadCloser
}
type sseEventMsg struct {
	event string
	data  map[string]interface{}
}
type streamDoneMsg struct{}
type streamErrMsg struct{ err error }

// File output
type fileSavedMsg struct{ path string }
type fileSaveErrMsg struct{ err error }

// Config / agents
type configDataMsg struct{ data map[string]string }
type configErrMsg struct{ err error }
type configSetOKMsg struct{ key, value string }
type configSetErrMsg struct{ err error }
type agentsListMsg struct{ agents []string }
type agentsErrMsg struct{ err error }

// File changes
type filesAppliedMsg struct{ paths []string }
type filesApplyErrMsg struct{ err error }

// ── Tea commands ───────────────────────────────────────────────────────────────

func loadModelsCmd() tea.Cmd {
	return func() tea.Msg {
		models, err := ollamaListModels()
		if err != nil {
			return modelsErrMsg{err}
		}
		return modelsLoadedMsg{models}
	}
}

func pullModelCmd(name string) tea.Cmd {
	return func() tea.Msg {
		body, err := ollamaPullStream(name)
		if err != nil {
			return pullErrMsg{err}
		}
		defer body.Close()
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 256*1024), 256*1024)
		for scanner.Scan() {
			var progress struct {
				Status    string `json:"status"`
				Total     int64  `json:"total"`
				Completed int64  `json:"completed"`
			}
			json.Unmarshal(scanner.Bytes(), &progress)
			if progress.Status == "success" {
				return pullProgressMsg{status: "success", done: true}
			}
		}
		if err := scanner.Err(); err != nil {
			return pullErrMsg{err}
		}
		return pullProgressMsg{status: "success", done: true}
	}
}

func setModelCmd(hiveURL, name string) tea.Cmd {
	return func() tea.Msg {
		if err := setOrchestratorModel(hiveURL, name); err != nil {
			return modelSetErrMsg{err}
		}
		return modelSetMsg{name}
	}
}
