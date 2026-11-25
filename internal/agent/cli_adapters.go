package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"dragon/internal/gitops"
	"dragon/internal/store"
)

// CLIAgentAdapter runs a configured local Coding CLI (Claude Code, Codex, Qoder, WorkBuddy, CodeBuddy, Antigravity, Gemini, OpenCode, Copilot, or custom).
type CLIAgentAdapter struct {
	profile store.AgentProfile
}

// NewCLIAgentAdapter wraps an AgentProfile as an Adapter.
func NewCLIAgentAdapter(profile store.AgentProfile) *CLIAgentAdapter {
	return &CLIAgentAdapter{profile: profile}
}

func (a *CLIAgentAdapter) ID() string   { return a.profile.ID }
func (a *CLIAgentAdapter) Kind() string { return a.profile.Kind }

func (a *CLIAgentAdapter) Detect(ctx context.Context) (bool, string, string) {
	resolved, ok := ResolveExecutable(a.profile)
	if !ok {
		return false, "", ""
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	args := append(append([]string(nil), resolved.PrefixArgs...), "--version")
	cmd := exec.CommandContext(cctx, resolved.Executable, args...)
	cmd.Env = mergeProcessEnv(resolved.ExtraEnv, a.profile.ExtraEnvJSON, nil)
	gitops.ConfigureHiddenCommand(cmd)
	out, err := cmd.CombinedOutput()
	ver := strings.TrimSpace(string(out))
	if idx := strings.IndexAny(ver, "\r\n"); idx != -1 {
		ver = strings.TrimSpace(ver[:idx])
	}
	if err != nil && ver == "" {
		ver = "detected"
	}
	return true, resolved.DisplayPath, ver
}

func (a *CLIAgentAdapter) Run(ctx context.Context, req RunRequest, onEvent func(StreamEvent)) (*RunResult, error) {
	resolved, ok := ResolveExecutable(a.profile)
	if !ok {
		return nil, fmt.Errorf("未找到 %s CLI（command=%q）。安装指引：%s", a.profile.Name, a.profile.Command, InstallHint(a.profile.Kind))
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args, stdinPrompt, lastMsgFile, cleanup := a.buildInvocation(req)
	defer cleanup()

	fullArgs := append(append([]string(nil), resolved.PrefixArgs...), args...)
	cmd := exec.CommandContext(runCtx, resolved.Executable, fullArgs...)
	if req.WorkDir != "" {
		cmd.Dir = req.WorkDir
	}
	gitops.ConfigureHiddenCommand(cmd)
	cmd.Env = mergeProcessEnv(resolved.ExtraEnv, a.profile.ExtraEnvJSON, req.Env)
	if stdinPrompt != "" {
		cmd.Stdin = strings.NewReader(stdinPrompt)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 %s 失败: %w", a.profile.Name, err)
	}

	var mu sync.Mutex
	var rawText strings.Builder
	var resultText strings.Builder
	var errLines []string
	var streamErrMsg string

	emit := func(stream, evType, text string) {
		if text == "" {
			return
		}
		if onEvent != nil {
			onEvent(StreamEvent{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Stream:    stream,
				EventType: evType,
				Text:      text,
			})
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		scanLines(stdout, func(line string) {
			evType, display, finalChunk, errMsg := a.parseStdoutLine(line)
			mu.Lock()
			if errMsg != "" && streamErrMsg == "" {
				streamErrMsg = errMsg
			}
			if finalChunk != "" {
				resultText.Reset()
				resultText.WriteString(finalChunk)
			} else if display != "" {
				rawText.WriteString(display)
				rawText.WriteByte('\n')
			}
			mu.Unlock()
			emit("stdout", evType, display)
		})
	}()

	go func() {
		defer wg.Done()
		scanLines(stderr, func(line string) {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				return
			}
			mu.Lock()
			if len(errLines) < 20 {
				errLines = append(errLines, trimmed)
			}
			mu.Unlock()
			emit("stderr", "error", line)
		})
	}()

	wg.Wait()
	waitErr := cmd.Wait()
	dur := time.Since(start).Milliseconds()

	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	output := strings.TrimSpace(resultText.String())
	if output == "" && lastMsgFile != "" {
		if data, readErr := os.ReadFile(lastMsgFile); readErr == nil {
			if msg := trimBOMAndNulls(data); msg != "" {
				output = msg
			}
		}
	}
	if output == "" {
		output = strings.TrimSpace(rawText.String())
	}
	if output == "" && streamErrMsg != "" {
		output = streamErrMsg
	}

	res := &RunResult{
		AgentID:    a.profile.ID,
		Output:     output,
		ExitCode:   exitCode,
		DurationMs: dur,
	}

	if waitErr == nil && streamErrMsg != "" {
		res.ExitCode = 1
		return res, fmt.Errorf("%s 执行失败: %s", a.profile.Name, streamErrMsg)
	}
	if waitErr != nil {
		detail := streamErrMsg
		if detail == "" && len(errLines) > 0 {
			detail = errLines[len(errLines)-1]
		}
		if detail != "" {
			return res, fmt.Errorf("%s 执行失败 (exit=%d): %s (%w)", a.profile.Name, exitCode, detail, waitErr)
		}
		return res, fmt.Errorf("%s 执行失败 (exit=%d): %w", a.profile.Name, exitCode, waitErr)
	}
	return res, nil
}

func mergeProcessEnv(resolvedEnv map[string]string, profileExtraEnvJSON string, reqEnv map[string]string) []string {
	env := os.Environ()
	for k, v := range resolvedEnv {
		env = append(env, k+"="+v)
	}
	if strings.TrimSpace(profileExtraEnvJSON) != "" && profileExtraEnvJSON != "{}" {
		var profileEnv map[string]string
		if err := json.Unmarshal([]byte(profileExtraEnvJSON), &profileEnv); err == nil {
			for k, v := range profileEnv {
				env = append(env, k+"="+v)
			}
		}
	}
	for k, v := range reqEnv {
		env = append(env, k+"="+v)
	}
	return env
}

func isBypassPermission(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "bypass", "bypasspermissions", "bypass_permissions", "danger-full-access", "dangerously-skip-permissions", "dangerously-bypass-approvals-and-sandbox", "full-bypass":
		return true
	default:
		return false
	}
}

func supportsNativeAppendSystemPrompt(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "claude", "qoder", "workbuddy", "codebuddy":
		return true
	default:
		return false
	}
}

func (a *CLIAgentAdapter) buildInvocation(req RunRequest) (args []string, stdinPrompt string, lastMsgFile string, cleanup func()) {
	cleanupFns := make([]func(), 0, 2)
	cleanup = func() {
		for _, fn := range cleanupFns {
			fn()
		}
	}

	model := req.Model
	if model == "" {
		model = a.profile.Model
	}
	permMode := req.PermissionMode
	if permMode == "" {
		permMode = a.profile.PermissionMode
	}

	kind := strings.ToLower(strings.TrimSpace(a.profile.Kind))
	prompt := req.Prompt
	if req.SystemPrompt != "" && !supportsNativeAppendSystemPrompt(kind) {
		prompt = req.SystemPrompt + "\n\n" + prompt
	}

	switch kind {
	case "claude":
		args = []string{"-p", "--output-format", "stream-json", "--verbose"}
		if req.ReadOnly {
			args = append(args, "--permission-mode", "plan")
		} else if isBypassPermission(permMode) {
			args = append(args, "--permission-mode", "bypassPermissions", "--dangerously-skip-permissions")
		} else if permMode != "" {
			args = append(args, "--permission-mode", permMode)
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		if req.SystemPrompt != "" {
			args = append(args, "--append-system-prompt", req.SystemPrompt)
		}
		args = append(args, req.ExtraArgs...)
		return args, prompt, "", cleanup

	case "codex":
		if tmpFile, err := os.CreateTemp("", "dragon-codex-last-*.md"); err == nil {
			lastMsgFile = tmpFile.Name()
			_ = tmpFile.Close()
			cleanupFns = append(cleanupFns, func() { _ = os.Remove(lastMsgFile) })
		}
		args = []string{"exec", "--json", "--skip-git-repo-check"}
		if lastMsgFile != "" {
			args = append(args, "-o", lastMsgFile)
		}
		if req.WorkDir != "" {
			args = append(args, "-C", req.WorkDir)
		}
		if model != "" {
			args = append(args, "-m", model)
		}
		if req.ReadOnly {
			args = append(args, "-s", "read-only")
		} else if isBypassPermission(permMode) {
			args = append(args, "--dangerously-bypass-approvals-and-sandbox")
		} else {
			args = append(args, "-s", "workspace-write", "--approve-for-me")
		}
		args = append(args, req.ExtraArgs...)
		args = append(args, "-")
		return args, prompt, lastMsgFile, cleanup

	case "qoder":
		args = []string{"-p", "-o", "stream-json"}
		if req.ReadOnly {
			args = append(args, "--permission-mode", "dont_ask", "--disallowed-tools", "Edit,Write,NotebookEdit,Bash")
		} else if isBypassPermission(permMode) {
			args = append(args, "--permission-mode", "bypass_permissions", "--dangerously-skip-permissions")
		} else {
			args = append(args, "--permission-mode", "accept_edits")
		}
		if model != "" {
			args = append(args, "-m", model)
		}
		if req.SystemPrompt != "" {
			args = append(args, "--append-system-prompt", req.SystemPrompt)
		}
		args = append(args, req.ExtraArgs...)
		return args, prompt, "", cleanup

	case "workbuddy", "codebuddy":
		args = []string{"-p", "--output-format", "stream-json", "--no-session-persistence"}
		if req.ReadOnly {
			args = append(args, "--permission-mode", "plan")
		} else if isBypassPermission(permMode) {
			args = append(args, "--permission-mode", "bypassPermissions", "-y")
		} else {
			args = append(args, "--permission-mode", "acceptEdits")
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		if req.SystemPrompt != "" {
			args = append(args, "--append-system-prompt", req.SystemPrompt)
		}
		args = append(args, req.ExtraArgs...)
		return args, prompt, "", cleanup

	case "agy":
		args = []string{"-p", "--output-format", "stream-json"}
		if !req.ReadOnly {
			pm := strings.ToLower(strings.TrimSpace(permMode))
			if isBypassPermission(pm) || pm == "" || pm == "auto" || pm == "acceptedits" || pm == "full-auto" {
				args = append(args, "--dangerously-skip-permissions")
			}
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, req.ExtraArgs...)
		return args, prompt, "", cleanup

	case "gemini", "kimi", "qwen_code":
		args = splitArgs(a.profile.ArgsTemplate)
		if len(args) == 0 {
			args = []string{"-p"}
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, req.ExtraArgs...)
		return args, prompt, "", cleanup

	case "aider":
		args = splitArgs(a.profile.ArgsTemplate)
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, "--message", prompt)
		args = append(args, req.ExtraArgs...)
		return args, "", "", cleanup

	case "opencode", "goose":
		args = splitArgs(a.profile.ArgsTemplate)
		if len(args) == 0 {
			args = []string{"run"}
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, req.ExtraArgs...)
		return args, prompt, "", cleanup

	case "copilot", "cline", "amp":
		args = splitArgs(a.profile.ArgsTemplate)
		if len(args) == 0 {
			args = []string{"-p"}
		}
		args = append(args, req.ExtraArgs...)
		return args, prompt, "", cleanup

	default:
		// Custom CLI adapter supports {{work_dir}}, {{model}}, and {{prompt_file}} placeholders.
		tpl := a.profile.ArgsTemplate
		if strings.Contains(tpl, "{{prompt_file}}") {
			tmpFile, err := os.CreateTemp("", "dragon-prompt-*.md")
			if err == nil {
				_, _ = tmpFile.WriteString(prompt)
				_ = tmpFile.Close()
				name := tmpFile.Name()
				tpl = strings.ReplaceAll(tpl, "{{prompt_file}}", name)
				cleanupFns = append(cleanupFns, func() { _ = os.Remove(name) })
				prompt = ""
			}
		}
		tpl = strings.ReplaceAll(tpl, "{{work_dir}}", req.WorkDir)
		tpl = strings.ReplaceAll(tpl, "{{model}}", model)
		args = append(splitArgs(tpl), req.ExtraArgs...)
		return args, prompt, "", cleanup
	}
}

func (a *CLIAgentAdapter) parseStdoutLine(line string) (evType string, display string, finalChunk string, errMsg string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", "", "", ""
	}
	if !strings.HasPrefix(trimmed, "{") {
		return "text", trimmed, "", ""
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return "text", trimmed, "", ""
	}

	typ, _ := obj["type"].(string)
	kind := strings.ToLower(strings.TrimSpace(a.profile.Kind))
	switch kind {
	case "claude", "qoder", "workbuddy", "codebuddy", "agy":
		switch typ {
		case "assistant":
			if msg, ok := obj["message"].(map[string]any); ok {
				if content, ok := msg["content"].([]any); ok {
					var parts []string
					for _, item := range content {
						m, ok := item.(map[string]any)
						if !ok {
							continue
						}
						switch t, _ := m["type"].(string); t {
						case "text":
							if txt, _ := m["text"].(string); txt != "" {
								parts = append(parts, txt)
							}
						case "tool_use":
							name, _ := m["name"].(string)
							return "tool", fmt.Sprintf("[tool] %s", name), "", ""
						}
					}
					if len(parts) > 0 {
						return "text", strings.Join(parts, "\n"), "", ""
					}
				}
			}
		case "result":
			isErr, _ := obj["is_error"].(bool)
			res, _ := obj["result"].(string)
			errJoined := extractStringArrayErrors(obj["errors"])
			if isErr {
				errTxt := strings.TrimSpace(res)
				if errTxt == "" {
					errTxt = errJoined
				}
				if errTxt == "" {
					errTxt = "execution error"
				}
				return "error", errTxt, "", errTxt
			}
			if res != "" {
				return "result", "[completed] output captured", res, ""
			}
		case "error":
			msg := extractErrorMessage(obj)
			if msg != "" {
				return "error", msg, "", msg
			}
		case "system":
			sub, _ := obj["subtype"].(string)
			if sub != "" && sub != "status" {
				return "text", fmt.Sprintf("[%s] %s", kind, sub), "", ""
			}
		}
		return "", "", "", ""

	case "codex":
		switch typ {
		case "item.completed":
			if item, ok := obj["item"].(map[string]any); ok {
				itemType, _ := item["type"].(string)
				switch itemType {
				case "agent_message":
					txt := extractCodexItemText(item)
					if txt != "" {
						return "text", txt, txt, ""
					}
				case "command_execution":
					cmdStr, _ := item["command"].(string)
					if cmdStr == "" {
						cmdStr = "shell"
					}
					return "tool", "[cmd] " + cmdStr, "", ""
				case "file_change":
					return "tool", "[file] modified", "", ""
				case "reasoning":
					txt := extractCodexItemText(item)
					if txt != "" {
						return "text", "[thinking] " + txt, "", ""
					}
				}
			}
			return "", "", "", ""
		case "item.started":
			if item, ok := obj["item"].(map[string]any); ok {
				if itemType, _ := item["type"].(string); itemType == "command_execution" {
					if cmdStr, _ := item["command"].(string); cmdStr != "" {
						return "tool", "[cmd] " + cmdStr, "", ""
					}
				}
			}
			return "", "", "", ""
		case "turn.failed":
			msg := extractErrorMessage(obj)
			if msg == "" {
				msg = "codex turn failed"
			}
			return "error", msg, "", msg
		case "error":
			msg := extractErrorMessage(obj)
			if msg == "" {
				msg = "codex error"
			}
			return "error", msg, "", msg
		case "thread.started", "turn.started", "turn.completed", "item.updated":
			return "", "", "", ""
		}
		if msg, _ := obj["message"].(string); msg != "" {
			return "text", msg, "", ""
		}
		if res, _ := obj["result"].(string); res != "" {
			return "result", res, res, ""
		}
		return "text", trimmed, "", ""
	}

	return "text", trimmed, "", ""
}

func extractStringArrayErrors(raw any) string {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return ""
	}
	var msgs []string
	for _, it := range arr {
		if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
			msgs = append(msgs, strings.TrimSpace(s))
		}
	}
	return strings.Join(msgs, "; ")
}

func extractErrorMessage(obj map[string]any) string {
	if msg, _ := obj["message"].(string); strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg)
	}
	switch errVal := obj["error"].(type) {
	case string:
		return strings.TrimSpace(errVal)
	case map[string]any:
		if m, _ := errVal["message"].(string); strings.TrimSpace(m) != "" {
			return strings.TrimSpace(m)
		}
	}
	return ""
}

func extractCodexItemText(item map[string]any) string {
	if txt, _ := item["text"].(string); strings.TrimSpace(txt) != "" {
		return strings.TrimSpace(txt)
	}
	if content, ok := item["content"].([]any); ok {
		var parts []string
		for _, c := range content {
			m, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
				parts = append(parts, strings.TrimSpace(txt))
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func scanLines(r io.Reader, fn func(string)) {
	sc := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	sc.Buffer(buf, 4*1024*1024)
	for sc.Scan() {
		fn(sc.Text())
	}
}

func splitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inQuote := rune(0)
	for _, r := range s {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			inQuote = r
		case r == ' ' || r == '\t':
			if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args
}
