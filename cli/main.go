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
// Slash commands (inside the TUI):
//
//	/model         Select or change Ollama model
//	/config        Show orchestrator configuration
//	/set <k> <v>   Update config (model, temperature, max_tokens)
//	/agents        List available agents
//	/file <path>   Attach a file from current directory
//	/files         List attached files
//	/clear         Clear attached files
//	/help          Show available commands
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

	var presetModel, task string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch {
		case (args[i] == "--model" || args[i] == "-m") && i+1 < len(args):
			presetModel = args[i+1]
			i++
		case args[i] == "--help" || args[i] == "-h":
			fmt.Println("Usage: hive [--model <name>] [\"task description\"]")
			fmt.Println()
			fmt.Println("Options:")
			fmt.Println("  --model, -m    Ollama model to use")
			fmt.Println()
			fmt.Println("Environment:")
			fmt.Println("  HIVE_URL       Orchestrator URL (default: http://localhost:30800)")
			fmt.Println("  OLLAMA_HOST    Ollama API URL   (default: http://localhost:11434)")
			fmt.Println()
			fmt.Println("Slash commands (inside the TUI):")
			fmt.Println("  /model         Select or change Ollama model")
			fmt.Println("  /config        Show orchestrator configuration")
			fmt.Println("  /set <k> <v>   Update config value")
			fmt.Println("  /agents        List available agents")
			fmt.Println("  /file <path>   Attach a file from CWD")
			fmt.Println("  /files         List attached files")
			fmt.Println("  /clear         Clear attached files")
			fmt.Println("  /help          Show all commands")
			os.Exit(0)
		default:
			if task == "" {
				task = args[i]
			} else {
				task += " " + args[i]
			}
		}
	}
	task = strings.TrimSpace(task)

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

	if m, ok := finalModel.(model); ok && m.phase == phaseResult && m.result != "" {
		fmt.Printf("\nTask: %s\n", m.task)
		fmt.Printf("Agents: %s\n\n", strings.Join(m.agentsUsed, ", "))
		fmt.Println(m.result)
	}
}
