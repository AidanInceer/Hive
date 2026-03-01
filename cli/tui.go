package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// ── Model ──────────────────────────────────────────────────────────────────────

type model struct {
	hiveURL string
	cwd     string

	phase     appPhase
	task      string
	startTime time.Time

	taskInput textarea.Model
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
	ta := textarea.New()
	ta.Placeholder = "Describe your task..."
	ta.CharLimit = 2000
	ta.SetWidth(60)
	ta.SetHeight(5)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetKeys("alt+enter")

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
		taskInput:    ta,
		modelFilter:  mf,
		pullInput:    pi,
		spin:         sp,
		agentResults: make(map[string]string),
		activeModel:  presetModel,
	}

	if presetModel != "" && task != "" {
		m.task = task
		m.phase = phaseConnecting
		m.startTime = time.Now()
	} else if presetModel != "" {
		m.phase = phaseInput
		m.taskInput.Focus()
	} else if task != "" {
		m.task = task
		m.phase = phaseConnecting
		m.startTime = time.Now()
	} else {
		m.phase = phaseModelSelect
		m.modelFilter.Focus()
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
