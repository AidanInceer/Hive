package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// startStream opens an SSE connection to the orchestrator's /chat/stream endpoint.
// files is an optional map of {path: content} to send alongside the task.
func startStream(url, task string, files map[string]string) tea.Cmd {
	return func() tea.Msg {
		body := map[string]interface{}{"message": task}
		if len(files) > 0 {
			body["files"] = files
		}
		payload, _ := json.Marshal(body)
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
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return streamErrMsg{fmt.Errorf("server %d: %s", resp.StatusCode, string(respBody))}
		}
		return streamStartedMsg{
			reader: bufio.NewReaderSize(resp.Body, 32*1024),
			body:   resp.Body,
		}
	}
}

// nextSSEEvent reads the next SSE event from the stream.
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

// saveFile writes the task result to a markdown file.
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
