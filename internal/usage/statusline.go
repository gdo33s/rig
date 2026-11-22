package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// statusInput is the part of Claude Code's status-line JSON that rig reads.
type statusInput struct {
	SessionID   string `json:"session_id"`
	SessionName string `json:"session_name"`
	Cwd         string `json:"cwd"`
	Model       struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
		ProjectDir string `json:"project_dir"`
	} `json:"workspace"`
	ContextWindow struct {
		TotalInputTokens  int64    `json:"total_input_tokens"`
		ContextWindowSize int64    `json:"context_window_size"`
		UsedPercentage    *float64 `json:"used_percentage"`
	} `json:"context_window"`
	RateLimits map[string]Window `json:"rate_limits"`
}

// report is what rig keeps from one status line. A session's context is
// known once it has sent its first message.
func (in statusInput) report() Report {
	r := Report{Windows: in.RateLimits, SessionID: in.SessionID}
	cw := in.ContextWindow
	if cw.UsedPercentage == nil {
		return r
	}
	dir := in.Workspace.ProjectDir
	if dir == "" {
		dir = in.Workspace.CurrentDir
	}
	if dir == "" {
		dir = in.Cwd
	}
	r.Session = &Session{
		Name: in.SessionName, Dir: dir, Model: in.Model.DisplayName,
		UsedPercentage: *cw.UsedPercentage, WindowSize: cw.ContextWindowSize, InputTokens: cw.TotalInputTokens,
	}
	return r
}

const maxStatusInput = 1 << 20

// StatusLine is Claude Code's status-line command. It records the rate
// limits and context in the JSON on stdin, then prints a status line: the output of
// then, run with the same input, or a short line of rig's own.
func StatusLine(stdin io.Reader, stdout io.Writer, then string) error {
	input, err := io.ReadAll(io.LimitReader(stdin, maxStatusInput))
	if err != nil {
		return err
	}
	var in statusInput
	if err := json.Unmarshal(input, &in); err == nil {
		// Recording is a side effect; the status line must print regardless.
		_ = Record(StatePath(), in.report(), time.Now())
	}
	if then != "" {
		return runThen(then, input, stdout)
	}
	_, err = fmt.Fprintln(stdout, defaultLine(in))
	return err
}

// runThen runs the status-line command rig was installed in front of.
func runThen(command string, input []byte, stdout io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func defaultLine(in statusInput) string {
	var parts []string
	if in.Model.DisplayName != "" {
		parts = append(parts, in.Model.DisplayName)
	}
	if in.Workspace.CurrentDir != "" {
		parts = append(parts, filepath.Base(in.Workspace.CurrentDir))
	}
	if p := in.ContextWindow.UsedPercentage; p != nil {
		parts = append(parts, fmt.Sprintf("ctx %.0f%%", *p))
	}
	if w, ok := in.RateLimits["five_hour"]; ok {
		parts = append(parts, fmt.Sprintf("5h %.0f%%", w.UsedPercentage))
	}
	return strings.Join(parts, " · ")
}
