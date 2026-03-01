package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ── File reading ───────────────────────────────────────────────────────────────

// readFileFromCWD reads a file relative to the working directory.
func readFileFromCWD(cwd, path string) (string, error) {
	abs := path
	if !filepath.IsAbs(path) {
		abs = filepath.Join(cwd, path)
	}
	// Ensure path is within CWD
	rel, err := filepath.Rel(cwd, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path is outside working directory")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ── Auto-detection ─────────────────────────────────────────────────────────────

// fileRefPattern matches tokens that look like file paths (containing a dot extension).
var fileRefPattern = regexp.MustCompile(`(?:\b|^)((?:[\w.-]+[/\\])*[\w.-]+\.[\w]+)(?:\b|$)`)

// detectFiles scans task text for potential file paths and reads existing ones from CWD.
func detectFiles(cwd, text string) map[string]string {
	files := make(map[string]string)
	matches := fileRefPattern.FindAllString(text, -1)
	for _, match := range matches {
		abs := filepath.Join(cwd, match)
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		// Skip very large files (> 100 KB)
		if info.Size() > 100*1024 {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		files[match] = string(data)
	}
	return files
}

// ── File change parsing ────────────────────────────────────────────────────────

// fileBlockPattern matches ~~~file:path/to/file\ncontent\n~~~ blocks in agent results.
var fileBlockPattern = regexp.MustCompile("(?s)~~~file:([^\n]+)\n(.*?)~~~")

// parseFileChanges extracts file change blocks from the synthesised result.
func parseFileChanges(result string) map[string]string {
	changes := make(map[string]string)
	matches := fileBlockPattern.FindAllStringSubmatch(result, -1)
	for _, m := range matches {
		if len(m) == 3 {
			path := strings.TrimSpace(m[1])
			content := strings.TrimSuffix(m[2], "\n")
			changes[path] = content
		}
	}
	return changes
}

// ── File writing ───────────────────────────────────────────────────────────────

// applyFiles writes file changes to the working directory.
func applyFiles(cwd string, changes map[string]string) ([]string, error) {
	var applied []string
	for path, content := range changes {
		abs := filepath.Join(cwd, path)
		// Ensure within CWD
		rel, err := filepath.Rel(cwd, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		// Create directories if needed
		dir := filepath.Dir(abs)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return applied, fmt.Errorf("cannot create directory %s: %w", dir, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
			return applied, fmt.Errorf("cannot write %s: %w", path, err)
		}
		applied = append(applied, path)
	}
	return applied, nil
}

// applyFilesCmd is a Tea command that applies file changes to CWD.
func applyFilesCmd(cwd string, changes map[string]string) tea.Cmd {
	return func() tea.Msg {
		paths, err := applyFiles(cwd, changes)
		if err != nil {
			return filesApplyErrMsg{err}
		}
		return filesAppliedMsg{paths}
	}
}
