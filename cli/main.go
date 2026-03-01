// Package main — hive CLI: interactive TUI client for the Hive multi-agent orchestrator.
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
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	hiveURL := strings.TrimRight(envOr("HIVE_URL", defaultURL), "/")
	cwd, _ := os.Getwd()

	// Parse args: support --model <name> and positional task.
	var presetModel, task string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		if (args[i] == "--model" || args[i] == "-m") && i+1 < len(args) {
			presetModel = args[i+1]
			i++ // skip next
		} else if args[i] == "--help" || args[i] == "-h" {
			fmt.Println("Usage: hive [--model <name>] [\"task description\"]")
			fmt.Println()
			fmt.Println("Options:")
			fmt.Println("  --model, -m    Ollama model to use (e.g. qwen2.5-coder:7b)")
			fmt.Println()
			fmt.Println("Environment:")
			fmt.Println("  HIVE_URL       Orchestrator URL (default: http://localhost:30800)")
			fmt.Println("  OLLAMA_HOST    Ollama API URL   (default: http://localhost:11434)")
			os.Exit(0)
		} else {
			if task == "" {
				task = args[i]
			} else {
				task += " " + args[i]
			}
		}
	}
	task = strings.TrimSpace(task)

	// Auto-start kubectl port-forward if orchestrator isn't reachable.
	cleanup, tunnelErr := ensureTunnel()
	if tunnelErr != nil {
		fmt.Fprintf(os.Stderr, "hive: %v\n", tunnelErr)
		os.Exit(1)
	}
	defer cleanup()

	p := tea.NewProgram(
		newModel(task, hiveURL, cwd, presetModel),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hive: %v\n", err)
		os.Exit(1)
	}

	// Print result to stdout after TUI exits so it's in scrollback.
	if m, ok := finalModel.(model); ok && m.phase == phaseResult && m.result != "" {
		fmt.Printf("\nTask: %s\n", m.task)
		fmt.Printf("Agents: %s\n\n", strings.Join(m.agentsUsed, ", "))
		fmt.Println(m.result)
	}
}
