package agent

import (
	"context"
	"time"
)

// StreamEvent represents a single real-time log/progress event emitted by an Agent run.
type StreamEvent struct {
	Timestamp string `json:"timestamp"`
	Stream    string `json:"stream"`    // stdout | stderr | system
	EventType string `json:"eventType"` // text | tool | result | error
	Text      string `json:"text"`
}

// RunRequest configures one execution of a Coding Agent inside a worktree.
type RunRequest struct {
	WorkDir        string
	Prompt         string
	SystemPrompt   string
	Model          string
	PermissionMode string
	ExtraArgs      []string
	Env            map[string]string
	Timeout        time.Duration
	ReadOnly       bool
}

// RunResult summarizes the outcome of an Agent execution.
type RunResult struct {
	AgentID    string `json:"agentId"`
	Output     string `json:"output"`
	ExitCode   int    `json:"exitCode"`
	DurationMs int64  `json:"durationMs"`
}

// Adapter defines the uniform interface implemented by local Coding CLI and mock adapters.
type Adapter interface {
	ID() string
	Kind() string
	Detect(ctx context.Context) (detected bool, binPath string, version string)
	Run(ctx context.Context, req RunRequest, onEvent func(StreamEvent)) (*RunResult, error)
}
