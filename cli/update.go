package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ── Update ─────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── Window resize ──────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		return m.handleResize(msg)

	// ── Keyboard ───────────────────────────────────────────────────────────
	case tea.KeyMsg:
		return m.handleKey(msg)

	// ── Models loaded ──────────────────────────────────────────────────────
	case modelsLoadedMsg:
		m.localModels = msg.models
		m.addEvent(fmt.Sprintf("Found %d local model(s)", len(msg.models)))
		return m, nil

	case modelsErrMsg:
		m.addEvent(fmt.Sprintf("Ollama: %v", msg.err))
		return m, nil

	// ── Pull progress ──────────────────────────────────────────────────────
	case pullProgressMsg:
		if msg.done {
			m.pullStatus = "Pull complete!"
			m.addEvent(fmt.Sprintf("Model %s ready", m.activeModel))
			return m, setModelCmd(m.hiveURL, m.activeModel)
		}
		m.pullStatus = msg.status
		return m, nil

	case pullErrMsg:
		m.phase = phaseError
		m.err = fmt.Errorf("model pull failed: %w", msg.err)
		m.addEvent(fmt.Sprintf("Pull error: %v", msg.err))
		return m, nil

	// ── Model set on orchestrator ──────────────────────────────────────────
	case modelSetMsg:
		m.activeModel = msg.name
		m.addEvent(fmt.Sprintf("Orchestrator using model: %s", msg.name))
		m.phase = phaseInput
		cmd := m.taskInput.Focus()
		return m, cmd

	case modelSetErrMsg:
		m.addEvent(fmt.Sprintf("Warning: could not set model on orchestrator: %v", msg.err))
		m.phase = phaseInput
		cmd := m.taskInput.Focus()
		return m, cmd

	// ── SSE stream started ─────────────────────────────────────────────────
	case streamStartedMsg:
		m.sseReader = msg.reader
		m.sseBody = msg.body
		m.addEvent("Connected to orchestrator")
		return m, nextSSEEvent(m.sseReader)

	// ── SSE event received ─────────────────────────────────────────────────
	case sseEventMsg:
		return m.handleSSE(msg)

	// ── Stream ended / error ───────────────────────────────────────────────
	case streamDoneMsg:
		if m.phase != phaseResult {
			m.phase = phaseError
			m.err = fmt.Errorf("stream ended unexpectedly")
			m.addEvent("Stream ended unexpectedly")
		}
		if m.sseBody != nil {
			m.sseBody.Close()
			m.sseBody = nil
		}
		return m, nil

	case streamErrMsg:
		m.phase = phaseError
		m.err = msg.err
		m.addEvent(fmt.Sprintf("Error: %v", msg.err))
		if m.sseBody != nil {
			m.sseBody.Close()
			m.sseBody = nil
		}
		return m, nil

	// ── File saved ─────────────────────────────────────────────────────────
	case fileSavedMsg:
		m.saved = msg.path
		m.addEvent(fmt.Sprintf("Saved to %s", msg.path))
		return m, nil

	case fileSaveErrMsg:
		m.addEvent(fmt.Sprintf("Save failed: %v", msg.err))
		return m, nil

	// ── Spinner tick ───────────────────────────────────────────────────────
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Update sub-components
	if m.phase == phaseModelSelect && !m.showPullInput {
		var cmd tea.Cmd
		m.modelFilter, cmd = m.modelFilter.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.phase == phaseInput {
		var cmd tea.Cmd
		m.taskInput, cmd = m.taskInput.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.phase == phaseResult {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// ── Resize handler ─────────────────────────────────────────────────────────────

func (m model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	vpH := msg.Height - 16
	if vpH < 4 {
		vpH = 4
	}
	m.vp.Width = msg.Width - 4
	m.vp.Height = vpH
	if m.result != "" {
		m.vp.SetContent(m.result)
	}
	m.taskInput.SetWidth(min(msg.Width-10, 66))
	taH := min(8, (msg.Height-20)/3)
	if taH < 3 {
		taH = 3
	}
	m.taskInput.SetHeight(taH)
	m.ready = true
	return m, nil
}

// ── Key handler ────────────────────────────────────────────────────────────────

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.phase {
	case phaseModelSelect:
		return m.handleKeyModelSelect(msg)
	case phaseModelPull:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case phaseInput:
		return m.handleKeyInput(msg)
	case phaseResult:
		return m.handleKeyResult(msg)
	case phaseError:
		return m.handleKeyError(msg)
	default:
		if msg.String() == "ctrl+c" {
			if m.sseBody != nil {
				m.sseBody.Close()
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) handleKeyModelSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.showPullInput {
		switch msg.String() {
		case "enter":
			name := strings.TrimSpace(m.pullInput.Value())
			if name == "" {
				return m, nil
			}
			m.showPullInput = false
			m.phase = phaseModelPull
			m.pullStatus = fmt.Sprintf("Pulling %s...", name)
			m.activeModel = name
			m.addEvent(fmt.Sprintf("Pulling model %s from Ollama...", name))
			return m, pullModelCmd(name)
		case "esc":
			m.showPullInput = false
			m.modelFilter.Focus()
			return m, nil
		default:
			var cmd tea.Cmd
			m.pullInput, cmd = m.pullInput.Update(msg)
			return m, cmd
		}
	}

	filtered := m.filteredModels()
	switch msg.String() {
	case "up", "k":
		if m.modelCursor > 0 {
			m.modelCursor--
		}
	case "down", "j":
		if m.modelCursor < len(filtered)-1 {
			m.modelCursor++
		}
	case "enter":
		if len(filtered) > 0 && m.modelCursor < len(filtered) {
			chosen := filtered[m.modelCursor].Name
			m.activeModel = chosen
			m.addEvent(fmt.Sprintf("Selected model: %s", chosen))
			m.phase = phaseModelPull
			m.pullStatus = fmt.Sprintf("Setting model to %s...", chosen)
			return m, setModelCmd(m.hiveURL, chosen)
		}
	case "p":
		m.showPullInput = true
		m.pullInput.SetValue("")
		m.pullInput.Focus()
		m.modelFilter.Blur()
		return m, textinput.Blink
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(msg.String()[0]-'0') - 1
		if idx < len(popularModels) {
			name := popularModels[idx]
			m.phase = phaseModelPull
			m.pullStatus = fmt.Sprintf("Pulling %s...", name)
			m.activeModel = name
			m.addEvent(fmt.Sprintf("Pulling model %s from Ollama...", name))
			return m, pullModelCmd(name)
		}
	case "tab":
		m.phase = phaseInput
		cmd := m.taskInput.Focus()
		return m, cmd
	case "ctrl+c":
		return m, tea.Quit
	default:
		var cmd tea.Cmd
		m.modelFilter, cmd = m.modelFilter.Update(msg)
		m.modelCursor = 0
		return m, cmd
	}
	return m, nil
}

func (m model) handleKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		task := strings.TrimSpace(m.taskInput.Value())
		if task == "" {
			return m, nil
		}
		m.task = task
		m.phase = phaseConnecting
		m.startTime = time.Now()
		m.addEvent("Task submitted")
		return m, tea.Batch(startStream(m.hiveURL, m.task), m.spin.Tick)
	case "ctrl+c":
		return m, tea.Quit
	default:
		var cmd tea.Cmd
		m.taskInput, cmd = m.taskInput.Update(msg)
		return m, cmd
	}
}

func (m model) handleKeyResult(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "s":
		if m.saved == "" {
			return m, saveFile(m.cwd, m.task, m.result, m.agentsUsed)
		}
	case "n":
		m2 := newModel("", m.hiveURL, m.cwd, m.activeModel)
		m2.width = m.width
		m2.height = m.height
		m2.ready = m.ready
		return m2, m2.Init()
	case "q", "ctrl+c":
		return m, tea.Quit
	default:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) handleKeyError(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "n":
		m2 := newModel("", m.hiveURL, m.cwd, m.activeModel)
		m2.width = m.width
		m2.height = m.height
		m2.ready = m.ready
		return m2, m2.Init()
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

// ── SSE event handler ──────────────────────────────────────────────────────────

func (m model) handleSSE(msg sseEventMsg) (tea.Model, tea.Cmd) {
	switch msg.event {
	case "routing_start":
		m.phase = phaseRouting
		m.addEvent("Routing task to agents...")

	case "routing_complete":
		if arr, ok := msg.data["agents"].([]interface{}); ok {
			m.agents = make([]agentInfo, len(arr))
			names := make([]string, len(arr))
			for i, v := range arr {
				name := fmt.Sprint(v)
				m.agents[i] = agentInfo{name: name, status: "pending"}
				names[i] = name
			}
			m.addEvent(fmt.Sprintf("Routed to %d agent(s): %s", len(arr), strings.Join(names, ", ")))
		}
		m.phase = phaseFanOut

	case "agent_start":
		if name, ok := msg.data["agent"].(string); ok {
			for i := range m.agents {
				if m.agents[i].name == name {
					m.agents[i].status = "working"
				}
			}
			m.addEvent(fmt.Sprintf("Agent %s started", name))
		}

	case "agent_complete":
		if name, ok := msg.data["agent"].(string); ok {
			for i := range m.agents {
				if m.agents[i].name == name {
					m.agents[i].status = "done"
				}
			}
			preview := ""
			if p, ok := msg.data["preview"].(string); ok && len(p) > 80 {
				preview = " -- " + p[:80] + "..."
			} else if p, ok := msg.data["preview"].(string); ok && p != "" {
				preview = " -- " + p
			}
			m.addEvent(fmt.Sprintf("Agent %s complete%s", name, preview))
		}

	case "synthesis_start":
		m.phase = phaseSynthesis
		m.addEvent("Synthesising agent responses...")

	case "synthesis_complete":
		m.addEvent("Synthesis complete")

	case "done":
		if r, ok := msg.data["result"].(string); ok {
			m.result = r
		}
		if arr, ok := msg.data["agents_used"].([]interface{}); ok {
			m.agentsUsed = make([]string, len(arr))
			for i, v := range arr {
				m.agentsUsed[i] = fmt.Sprint(v)
			}
		}
		if ar, ok := msg.data["agent_results"].(map[string]interface{}); ok {
			for k, v := range ar {
				m.agentResults[k] = fmt.Sprint(v)
			}
		}
		m.phase = phaseResult
		m.vp.SetContent(m.result)
		m.vp.GotoTop()
		elapsed := time.Since(m.startTime).Round(time.Millisecond)
		m.addEvent(fmt.Sprintf("Done in %s", elapsed))
		if m.sseBody != nil {
			m.sseBody.Close()
			m.sseBody = nil
		}
		return m, nil
	}

	if m.phase != phaseResult {
		return m, nextSSEEvent(m.sseReader)
	}
	return m, nil
}
