// hive — interactive TUI client for the Hive multi-agent orchestrator.
//
// Usage:
//
//	hive                                 Interactive mode (prompt for task)
//	hive "Plan a REST API for todos"     Direct mode (run task immediately)
//	hive --model qwen2.5-coder:7b       Select model, then interactive prompt
//
// Environment:
//
//	HIVE_URL      Orchestrator base URL   (default: http://localhost:30800)
//	OLLAMA_HOST   Ollama API base URL     (default: http://localhost:11434)
//
// The TUI shows real-time streaming progress via SSE: routing decisions,
// per-agent status, synthesis, and a scrollable result view with save-to-file.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Configuration ──────────────────────────────────────────────────────────────

const (
	defaultURL  = "http://localhost:30800"
	defaultPort = "30800"
	k8sNS       = "hive"
	k8sSvc      = "service/hive-orchestrator"
	k8sSvcPort  = "9000"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ── Auto-tunnel ────────────────────────────────────────────────────────────────
// If the orchestrator isn't already reachable, start kubectl port-forward in
// the background so the user never has to manage tunnels manually.

// portOpen does a quick TCP dial to check if something is listening.
func portOpen(host, port string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ensureTunnel starts kubectl port-forward if nothing is listening on the
// local port. Returns a cleanup func and any error.
func ensureTunnel() (cleanup func(), err error) {
	noop := func() {}

	// Already reachable — nothing to do.
	if portOpen("127.0.0.1", defaultPort, 500*time.Millisecond) {
		return noop, nil
	}

	// Check kubectl is available.
	kubectlPath, err := exec.LookPath("kubectl")
	if err != nil {
		return noop, fmt.Errorf("orchestrator not reachable on port %s and kubectl not found on PATH", defaultPort)
	}

	// Start port-forward in the background.
	cmd := exec.Command(kubectlPath,
		"port-forward", k8sSvc,
		defaultPort+":"+k8sSvcPort,
		"-n", k8sNS,
	)
	// Detach stdin and discard output.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return noop, fmt.Errorf("failed to start port-forward: %w", err)
	}

	// Wait for the port to come up (up to 5 s).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if portOpen("127.0.0.1", defaultPort, 300*time.Millisecond) {
			return func() {
				// Best-effort kill when the TUI exits.
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}, nil
		}
		time.Sleep(250 * time.Millisecond)
	}

	// Timed out — kill the zombie and report.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return noop, fmt.Errorf("port-forward started but orchestrator not reachable after 5 s (is minikube running?)")
}

// ── Ollama model helpers ───────────────────────────────────────────────────────
// Talk directly to the Ollama HTTP API running on the host.

var ollamaHost = envOr("OLLAMA_HOST", "http://localhost:11434")

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

// setOrchestratorModel tells the orchestrator to switch models via its /model endpoint.
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

// formatBytes renders bytes as human-readable.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// Tea messages for model operations
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
			// We can't send intermediate messages from a Cmd, so we just
			// wait for it to finish. The spinner shows activity.
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

// ── Popular models (suggestions for pulling) ──────────────────────────────────

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

// ── Styles ─────────────────────────────────────────────────────────────────────

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Padding(0, 1)
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	subtleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	borderColor  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

func ruler(w int) string {
	if w < 4 {
		w = 80
	}
	return borderColor.Render(strings.Repeat("-", w-2))
}

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
		return successStyle.Render("  [done]    " + a.name)
	case "working":
		return warnStyle.Render("  [working] " + a.name)
	default:
		return subtleStyle.Render("  [pending] " + a.name)
	}
}

// ── Tea messages ───────────────────────────────────────────────────────────────

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
type fileSavedMsg struct{ path string }
type fileSaveErrMsg struct{ err error }

// ── SSE helpers ────────────────────────────────────────────────────────────────

func startStream(url, task string) tea.Cmd {
	return func() tea.Msg {
		payload, _ := json.Marshal(map[string]string{"message": task})
		req, err := http.NewRequest("POST", url+"/chat/stream", bytes.NewReader(payload))
		if err != nil {
			return streamErrMsg{err}
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")

		client := &http.Client{Timeout: 0}
		resp, err := client.Do(req)
		if err != nil {
			return streamErrMsg{fmt.Errorf("connection failed: %w", err)}
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return streamErrMsg{fmt.Errorf("server %d: %s", resp.StatusCode, string(body))}
		}
		return streamStartedMsg{
			reader: bufio.NewReaderSize(resp.Body, 32*1024),
			body:   resp.Body,
		}
	}
}

func nextSSEEvent(reader *bufio.Reader) tea.Cmd {
	return func() tea.Msg {
		var event, data string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF && event != "" {
					break
				}
				if err == io.EOF {
					return streamDoneMsg{}
				}
				return streamErrMsg{err}
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" && (event != "" || data != "") {
				break
			}
			if strings.HasPrefix(line, "event: ") {
				event = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				data = strings.TrimPrefix(line, "data: ")
			}
		}
		var parsed map[string]interface{}
		if data != "" {
			json.Unmarshal([]byte(data), &parsed)
		}
		return sseEventMsg{event: event, data: parsed}
	}
}

func saveFile(cwd, task, result string, agents []string) tea.Cmd {
	return func() tea.Msg {
		ts := time.Now().Format("2006-01-02-150405")
		name := fmt.Sprintf("hive-output-%s.md", ts)
		path := filepath.Join(cwd, name)

		var b strings.Builder
		fmt.Fprintf(&b, "# Hive Output\n\n")
		fmt.Fprintf(&b, "**Task:** %s\n\n", task)
		fmt.Fprintf(&b, "**Agents:** %s\n\n", strings.Join(agents, ", "))
		fmt.Fprintf(&b, "**Generated:** %s\n\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(&b, "---\n\n%s\n", result)

		if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
			return fileSaveErrMsg{err}
		}
		return fileSavedMsg{path: path}
	}
}

// ── Model ──────────────────────────────────────────────────────────────────────

type model struct {
	hiveURL string
	cwd     string

	phase     appPhase
	task      string
	startTime time.Time

	textInput textinput.Model
	spin      spinner.Model
	vp        viewport.Model

	// Model selection
	localModels   []ollamaModel
	modelCursor   int
	modelFilter   textinput.Model
	activeModel   string
	showPullInput bool
	pullInput     textinput.Model
	pullStatus    string

	agents       []agentInfo
	agentsUsed   []string
	agentResults map[string]string
	result       string

	events []string
	err    error

	width  int
	height int
	ready  bool

	sseReader *bufio.Reader
	sseBody   io.ReadCloser
	saved     string
}

func newModel(task, hiveURL, cwd, presetModel string) model {
	ti := textinput.New()
	ti.Placeholder = "Describe your task..."
	ti.Focus()
	ti.CharLimit = 2000
	ti.Width = 60

	mf := textinput.New()
	mf.Placeholder = "Type to filter models..."
	mf.CharLimit = 100
	mf.Width = 40

	pi := textinput.New()
	pi.Placeholder = "e.g. qwen2.5-coder:7b"
	pi.CharLimit = 100
	pi.Width = 40

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = accentStyle

	m := model{
		hiveURL:      hiveURL,
		cwd:          cwd,
		textInput:    ti,
		modelFilter:  mf,
		pullInput:    pi,
		spin:         sp,
		agentResults: make(map[string]string),
		activeModel:  presetModel,
	}

	if presetModel != "" && task != "" {
		// Both model and task specified on CLI
		m.task = task
		m.phase = phaseConnecting
		m.startTime = time.Now()
	} else if presetModel != "" {
		// Model specified, go to task input
		m.phase = phaseInput
	} else if task != "" {
		// Task specified, skip model selection
		m.task = task
		m.phase = phaseConnecting
		m.startTime = time.Now()
	} else {
		// Interactive — start with model selection
		m.phase = phaseModelSelect
	}
	return m
}

func (m *model) addEvent(msg string) {
	ts := time.Now().Format("15:04:05")
	m.events = append(m.events, fmt.Sprintf("[%s] %s", ts, msg))
}

func (m *model) filteredModels() []ollamaModel {
	filter := strings.ToLower(strings.TrimSpace(m.modelFilter.Value()))
	if filter == "" {
		return m.localModels
	}
	var out []ollamaModel
	for _, mod := range m.localModels {
		if strings.Contains(strings.ToLower(mod.Name), filter) {
			out = append(out, mod)
		}
	}
	return out
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick}
	switch m.phase {
	case phaseModelSelect:
		cmds = append(cmds, loadModelsCmd(), textinput.Blink)
	case phaseInput:
		cmds = append(cmds, textinput.Blink)
	default:
		cmds = append(cmds, startStream(m.hiveURL, m.task))
	}
	return tea.Batch(cmds...)
}

// ── Update ─────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── Window resize ──────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpH := msg.Height - 16
		if vpH < 4 {
			vpH = 4
		}
		m.vp = viewport.New(msg.Width-4, vpH)
		if m.result != "" {
			m.vp.SetContent(m.result)
		}
		m.textInput.Width = min(msg.Width-10, 80)
		m.ready = true
		return m, nil

	// ── Keyboard ───────────────────────────────────────────────────────────
	case tea.KeyMsg:
		switch m.phase {
		case phaseInput:
			switch msg.String() {
			case "enter":
				task := strings.TrimSpace(m.textInput.Value())
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
			}

		case phaseResult:
			switch msg.String() {
			case "s":
				if m.saved == "" {
					return m, saveFile(m.cwd, m.task, m.result, m.agentsUsed)
				}
			case "n":
				m2 := newModel("", m.hiveURL, m.cwd)
				m2.width = m.width
				m2.height = m.height
				m2.ready = m.ready
				return m2, m2.Init()
			case "q", "ctrl+c":
				return m, tea.Quit
			default:
				var cmd tea.Cmd
				m.vp, cmd = m.vp.Update(msg)
				cmds = append(cmds, cmd)
			}

		case phaseError:
			switch msg.String() {
			case "n":
				m2 := newModel("", m.hiveURL, m.cwd)
				m2.width = m.width
				m2.height = m.height
				m2.ready = m.ready
				return m2, m2.Init()
			case "q", "ctrl+c":
				return m, tea.Quit
			}

		default: // streaming phases
			if msg.String() == "ctrl+c" {
				if m.sseBody != nil {
					m.sseBody.Close()
				}
				return m, tea.Quit
			}
		}

	// ── SSE stream started ─────────────────────────────────────────────────
	case streamStartedMsg:
		m.sseReader = msg.reader
		m.sseBody = msg.body
		m.addEvent("Connected to orchestrator")
		return m, nextSSEEvent(m.sseReader)

	// ── SSE event received ─────────────────────────────────────────────────
	case sseEventMsg:
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
	if m.phase == phaseInput {
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.phase == phaseResult {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// ── View ───────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if !m.ready {
		return "\n  Initialising...\n"
	}

	var b strings.Builder
	w := m.width

	// Header
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(" HIVE "))
	b.WriteString(subtleStyle.Render(" Multi-Agent AI System"))
	b.WriteString("\n")
	b.WriteString(ruler(w))
	b.WriteString("\n")

	switch m.phase {
	case phaseInput:
		b.WriteString("\n  Enter your task:\n\n")
		b.WriteString("  " + m.textInput.View() + "\n\n")
		b.WriteString(helpStyle.Render("  Enter submit | Ctrl+C quit") + "\n")

	case phaseResult:
		elapsed := time.Since(m.startTime).Round(time.Millisecond)
		b.WriteString(fmt.Sprintf("\n  Task:   %s\n", m.task))
		b.WriteString(fmt.Sprintf("  Agents: %s\n", accentStyle.Render(strings.Join(m.agentsUsed, ", "))))
		b.WriteString(fmt.Sprintf("  Time:   %s\n", successStyle.Render(elapsed.String())))
		if m.saved != "" {
			b.WriteString(fmt.Sprintf("  Saved:  %s\n", successStyle.Render(m.saved)))
		}
		b.WriteString("\n" + ruler(w) + "\n")
		b.WriteString(m.vp.View())
		b.WriteString("\n" + ruler(w) + "\n")
		saveHint := "[s]ave"
		if m.saved != "" {
			saveHint = subtleStyle.Render("[saved]")
		}
		b.WriteString(helpStyle.Render(fmt.Sprintf("  %s  [n]ew task  [q]uit  up/down scroll", saveHint)))
		b.WriteString("\n")

	case phaseError:
		b.WriteString("\n")
		b.WriteString(errStyle.Render(fmt.Sprintf("  Error: %v", m.err)))
		b.WriteString("\n\n")
		b.WriteString(m.eventLogView())
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("  [n]ew task  [q]uit"))
		b.WriteString("\n")

	default: // streaming phases
		b.WriteString(fmt.Sprintf("\n  Task: %s\n", m.task))
		elapsed := time.Since(m.startTime).Round(time.Millisecond)
		b.WriteString(fmt.Sprintf("  %s %s  %s\n",
			m.spin.View(),
			accentStyle.Render(m.phase.String()),
			subtleStyle.Render(elapsed.String()),
		))

		// Agent list
		if len(m.agents) > 0 {
			b.WriteString("\n")
			done := 0
			for _, a := range m.agents {
				b.WriteString(a.render() + "\n")
				if a.status == "done" {
					done++
				}
			}
			b.WriteString(subtleStyle.Render(fmt.Sprintf("\n  %d/%d agents complete", done, len(m.agents))) + "\n")
		}

		// Event log
		b.WriteString("\n" + ruler(w) + "\n")
		b.WriteString(m.eventLogView())
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("  Ctrl+C cancel"))
		b.WriteString("\n")
	}

	return b.String()
}

func (m model) eventLogView() string {
	if len(m.events) == 0 {
		return ""
	}
	var b strings.Builder
	start := 0
	maxShow := 10
	if len(m.events) > maxShow {
		start = len(m.events) - maxShow
	}
	for _, e := range m.events[start:] {
		b.WriteString("  " + subtleStyle.Render(e) + "\n")
	}
	return b.String()
}

// ── Main ───────────────────────────────────────────────────────────────────────

func main() {
	hiveURL := strings.TrimRight(envOr("HIVE_URL", defaultURL), "/")
	cwd, _ := os.Getwd()

	task := ""
	if len(os.Args) > 1 {
		task = strings.TrimSpace(strings.Join(os.Args[1:], " "))
	}

	// Auto-start kubectl port-forward if orchestrator isn't reachable.
	cleanup, tunnelErr := ensureTunnel()
	if tunnelErr != nil {
		fmt.Fprintf(os.Stderr, "hive: %v\n", tunnelErr)
		os.Exit(1)
	}
	defer cleanup()

	p := tea.NewProgram(
		newModel(task, hiveURL, cwd),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hive: %v\n", err)
		os.Exit(1)
	}

	// Print result to stdout after TUI exits so it's in scrollback
	if m, ok := finalModel.(model); ok && m.phase == phaseResult && m.result != "" {
		fmt.Printf("\nTask: %s\n", m.task)
		fmt.Printf("Agents: %s\n\n", strings.Join(m.agentsUsed, ", "))
		fmt.Println(m.result)
	}
}
