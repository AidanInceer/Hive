package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ── View ───────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if !m.ready {
		return "\n  Initialising...\n"
	}

	var b strings.Builder
	w := m.width

	// ── Header ──────────────────────────────────────────────────────────────
	banner := titleStyle.Render(" 🐝 HIVE ") + " " + subtleStyle.Render("Multi-Agent AI System")
	b.WriteString("\n")
	b.WriteString(center(banner, w))
	b.WriteString("\n")
	b.WriteString(center(borderColor.Render(strings.Repeat("─", min(w-4, 60))), w))
	b.WriteString("\n")

	switch m.phase {
	case phaseModelSelect:
		m.renderModelSelect(&b, w)
	case phaseModelPull:
		m.renderModelPull(&b, w)
	case phaseInput:
		m.renderInput(&b, w)
	case phaseResult:
		m.renderResult(&b, w)
	case phaseError:
		m.renderError(&b, w)
	default:
		m.renderStreaming(&b, w)
	}

	return b.String()
}

// ── Phase renderers ────────────────────────────────────────────────────────────

func (m model) renderModelSelect(b *strings.Builder, w int) {
	var content strings.Builder

	if m.activeModel != "" {
		content.WriteString(fmt.Sprintf("Active: %s\n\n", accentStyle.Render(m.activeModel)))
	}
	content.WriteString(accentStyle.Render("Local Models") + "  " + m.modelFilter.View() + "\n\n")

	filtered := m.filteredModels()
	if len(filtered) == 0 && len(m.localModels) == 0 {
		content.WriteString(subtleStyle.Render("Loading models...") + "\n")
	} else if len(filtered) == 0 {
		content.WriteString(subtleStyle.Render("No models match filter") + "\n")
	} else {
		maxShow := 8
		if len(filtered) < maxShow {
			maxShow = len(filtered)
		}
		for i := 0; i < maxShow; i++ {
			cursor := "  "
			mod := filtered[i]
			label := fmt.Sprintf("%s  %s", mod.Name, subtleStyle.Render(formatBytes(mod.Size)))
			if i == m.modelCursor {
				cursor = accentStyle.Render("▸ ")
				label = selectedStyle.Render(" "+mod.Name+" ") + "  " + subtleStyle.Render(formatBytes(mod.Size))
			}
			content.WriteString(fmt.Sprintf("%s%s\n", cursor, label))
		}
		if len(filtered) > maxShow {
			content.WriteString(subtleStyle.Render(fmt.Sprintf("  ... and %d more", len(filtered)-maxShow)) + "\n")
		}
	}

	if m.showPullInput {
		content.WriteString("\n" + warnStyle.Render("Pull new model: ") + m.pullInput.View() + "\n")
	} else {
		content.WriteString("\n" + borderColor.Render(strings.Repeat("─", 40)) + "\n")
		content.WriteString(accentStyle.Render("Pull a model (number key):") + "\n\n")
		for i, name := range popularModels {
			if i >= 9 {
				break
			}
			content.WriteString(fmt.Sprintf("  %s %s\n", subtleStyle.Render(fmt.Sprintf("[%d]", i+1)), name))
		}
	}

	panelW := min(w-4, 62)
	panel := boxStyle.Width(panelW).Render(content.String())
	b.WriteString("\n")
	b.WriteString(center(panel, w))
	b.WriteString("\n\n")
	b.WriteString(center(helpStyle.Render("↑/↓ navigate │ Enter select │ [p]ull custom │ Tab skip │ Ctrl+C quit"), w))
	b.WriteString("\n")
}

func (m model) renderModelPull(b *strings.Builder, w int) {
	var content strings.Builder
	content.WriteString(fmt.Sprintf("%s %s\n", m.spin.View(), accentStyle.Render(m.pullStatus)))
	panelW := min(w-4, 62)
	panel := boxStyle.Width(panelW).Render(content.String())
	b.WriteString("\n")
	b.WriteString(center(panel, w))
	b.WriteString("\n\n")
	b.WriteString(m.eventLogView())
	b.WriteString(center(helpStyle.Render("Ctrl+C cancel"), w))
	b.WriteString("\n")
}

func (m model) renderInput(b *strings.Builder, w int) {
	var content strings.Builder

	if m.activeModel != "" {
		content.WriteString(subtleStyle.Render("Model: ") + accentStyle.Render(m.activeModel) + "\n\n")
	}
	content.WriteString(accentStyle.Render("What can I help you with?") + "\n\n")
	content.WriteString(m.taskInput.View())

	panelW := min(w-4, 72)
	panel := boxStyle.Width(panelW).Render(content.String())

	// Vertically center the input panel in available space.
	usedH := lipgloss.Height(panel) + 6 // header + hints + padding
	padTop := (m.height - usedH) / 3
	if padTop < 1 {
		padTop = 1
	}
	b.WriteString(strings.Repeat("\n", padTop))
	b.WriteString(center(panel, w))
	b.WriteString("\n\n")
	b.WriteString(center(helpStyle.Render("Enter submit │ Alt+Enter new line │ Ctrl+C quit"), w))
	b.WriteString("\n")
}

func (m model) renderResult(b *strings.Builder, w int) {
	elapsed := time.Since(m.startTime).Round(time.Millisecond)

	meta := fmt.Sprintf(
		"%s %s   %s %s   %s %s",
		subtleStyle.Render("Task:"), m.task,
		subtleStyle.Render("Agents:"), accentStyle.Render(strings.Join(m.agentsUsed, ", ")),
		subtleStyle.Render("Time:"), successStyle.Render(elapsed.String()),
	)
	if m.saved != "" {
		meta += fmt.Sprintf("   %s %s", subtleStyle.Render("Saved:"), successStyle.Render(m.saved))
	}
	b.WriteString("\n")
	b.WriteString(center(meta, w))
	b.WriteString("\n")
	b.WriteString(center(borderColor.Render(strings.Repeat("─", min(w-4, 80))), w))
	b.WriteString("\n")

	vpBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("237")).
		Width(min(w-4, 100)).
		Render(m.vp.View())
	b.WriteString(center(vpBox, w))
	b.WriteString("\n")
	b.WriteString(center(borderColor.Render(strings.Repeat("─", min(w-4, 80))), w))
	b.WriteString("\n")

	saveHint := "[s]ave"
	if m.saved != "" {
		saveHint = subtleStyle.Render("[saved]")
	}
	b.WriteString(center(helpStyle.Render(fmt.Sprintf("%s │ [n]ew task │ [q]uit │ ↑/↓ scroll", saveHint)), w))
	b.WriteString("\n")
}

func (m model) renderError(b *strings.Builder, w int) {
	var content strings.Builder
	content.WriteString(errStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	content.WriteString("\n")

	panelW := min(w-4, 62)
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("167")).
		Padding(1, 2).
		Width(panelW).
		Render(content.String())

	b.WriteString("\n")
	b.WriteString(center(panel, w))
	b.WriteString("\n\n")
	b.WriteString(m.eventLogView())
	b.WriteString(center(helpStyle.Render("[n]ew task │ [q]uit"), w))
	b.WriteString("\n")
}

func (m model) renderStreaming(b *strings.Builder, w int) {
	var content strings.Builder

	content.WriteString(subtleStyle.Render("Task: ") + m.task + "\n\n")

	elapsed := time.Since(m.startTime).Round(time.Millisecond)
	content.WriteString(fmt.Sprintf("%s %s  %s\n",
		m.spin.View(),
		accentStyle.Render(m.phase.String()),
		subtleStyle.Render(elapsed.String()),
	))

	if len(m.agents) > 0 {
		content.WriteString("\n")
		done := 0
		for _, a := range m.agents {
			content.WriteString(a.render() + "\n")
			if a.status == "done" {
				done++
			}
		}
		content.WriteString("\n" + subtleStyle.Render(fmt.Sprintf("%d/%d agents complete", done, len(m.agents))))
	}

	panelW := min(w-4, 62)
	panel := boxStyle.Width(panelW).Render(content.String())

	b.WriteString("\n")
	b.WriteString(center(panel, w))
	b.WriteString("\n\n")
	b.WriteString(m.eventLogView())
	b.WriteString(center(helpStyle.Render("Ctrl+C cancel"), w))
	b.WriteString("\n")
}

// ── Event log ──────────────────────────────────────────────────────────────────

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
		b.WriteString(center(subtleStyle.Render(e), m.width) + "\n")
	}
	return b.String()
}
