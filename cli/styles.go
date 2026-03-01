package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── Hive Theme ─────────────────────────────────────────────────────────────────
//
// Palette: warm amber/honey tones with neutral grays.
//   Honey:   #F0A500  (256-color 214)
//   Amber:   #D4880F  (256-color 172)
//   Gold:    #FFD866  (256-color 222)
//   Dim:     #6B6B6B  (256-color 242)
//   Dark:    #3A3A3A  (256-color 237)
//   Cream:   #FFF8E7  (256-color 230)

var (
	// Primary accent — warm honey yellow for highlights.
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	// Title badge — bold cream on amber background.
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("172")).
			Padding(0, 1)

	// Selected item in lists — inverted honey.
	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("214"))

	// Muted text — timestamps, secondary info.
	subtleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

	// Success — soft green.
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))

	// Warning — gold.
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("222"))

	// Error — muted red.
	errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("167"))

	// Borders and dividers.
	borderColor = lipgloss.NewStyle().Foreground(lipgloss.Color("237"))

	// Help / keybind hints.
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

	// Box style — rounded border in amber, used for panels.
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("172")).
			Padding(1, 2)
)

// ── Helpers ────────────────────────────────────────────────────────────────────

// ruler renders a thin horizontal line spanning width w.
func ruler(w int) string {
	if w < 4 {
		w = 80
	}
	return borderColor.Render(strings.Repeat("─", w-2))
}

// center pads every line of s so the block appears horizontally centred within
// width w.  Works correctly for multi-line strings (e.g. boxed panels).
func center(s string, w int) string {
	return lipgloss.PlaceHorizontal(w, lipgloss.Center, s)
}

// formatBytes renders bytes as human-readable (KB, MB, GB, …).
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
