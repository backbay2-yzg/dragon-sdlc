package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"dragon/internal/store"
)

func TestBuildInvocationAcrossAgentsAndReadOnlyModes(t *testing.T) {
	t.Run("claude", func(t *testing.T) {
		a := NewCLIAgentAdapter(store.AgentProfile{ID: "claude", Kind: "claude", PermissionMode: "acceptEdits"})

		argsRO, stdinRO, _, cleanupRO := a.buildInvocation(RunRequest{Prompt: "spec", SystemPrompt: "sys", ReadOnly: true})
		defer cleanupRO()
		if stdinRO != "spec" {
			t.Fatalf("expected raw prompt on stdin when SystemPrompt uses --append-system-prompt, got %q", stdinRO)
		}
		if !containsPair(argsRO, "--permission-mode", "plan") {
			t.Fatalf("expected claude ReadOnly plan mode, got %v", argsRO)
		}
		if !containsPair(argsRO, "--append-system-prompt", "sys") {
			t.Fatalf("expected --append-system-prompt, got %v", argsRO)
		}

		argsWrite, _, _, cleanupW := a.buildInvocation(RunRequest{Prompt: "code", Model: "claude-sonnet-4-6", ReadOnly: false})
		defer cleanupW()
		if !containsPair(argsWrite, "--permission-mode", "acceptEdits") || !containsPair(argsWrite, "--model", "claude-sonnet-4-6") {
			t.Fatalf("unexpected claude write args: %v", argsWrite)
		}

		aBypass := NewCLIAgentAdapter(store.AgentProfile{ID: "claude", Kind: "claude", PermissionMode: "bypass"})
		argsBypass, _, _, cleanupB := aBypass.buildInvocation(RunRequest{Prompt: "code", ReadOnly: false})
		defer cleanupB()
		if !containsPair(argsBypass, "--permission-mode", "bypassPermissions") || !slices.Contains(argsBypass, "--dangerously-skip-permissions") {
			t.Fatalf("expected claude bypass args, got %v", argsBypass)
		}
	})

	t.Run("codex", func(t *testing.T) {
		a := NewCLIAgentAdapter(store.AgentProfile{ID: "codex", Kind: "codex", PermissionMode: "workspace-write"})

		argsRO, stdinRO, lastRO, cleanupRO := a.buildInvocation(RunRequest{WorkDir: `D:\repo`, Prompt: "review", SystemPrompt: "sys", ReadOnly: true})
		defer cleanupRO()
		if lastRO == "" {
			t.Fatalf("expected -o last message temp file for codex")
		}
		if slices.Contains(argsRO, "--full-auto") {
			t.Fatalf("codex must never pass deprecated --full-auto flag: %v", argsRO)
		}
		if !containsPair(argsRO, "-s", "read-only") || !containsPair(argsRO, "-C", `D:\repo`) || !containsPair(argsRO, "-o", lastRO) {
			t.Fatalf("unexpected codex read-only args: %v", argsRO)
		}
		if argsRO[len(argsRO)-1] != "-" || !strings.Contains(stdinRO, "sys\n\nreview") {
			t.Fatalf("unexpected codex stdin/args: args=%v stdin=%q", argsRO, stdinRO)
		}

		argsW, _, _, cleanupW := a.buildInvocation(RunRequest{WorkDir: `D:\repo`, Prompt: "fix", Model: "gpt-5.4", ReadOnly: false})
		defer cleanupW()
		if slices.Contains(argsW, "--full-auto") {
			t.Fatalf("codex must never pass --full-auto: %v", argsW)
		}
		if !containsPair(argsW, "-s", "workspace-write") || !slices.Contains(argsW, "--approve-for-me") || !containsPair(argsW, "-m", "gpt-5.4") {
			t.Fatalf("unexpected codex write args: %v", argsW)
		}

		aBypass := NewCLIAgentAdapter(store.AgentProfile{ID: "codex", Kind: "codex", PermissionMode: "bypass"})
		argsB, _, _, cleanupB := aBypass.buildInvocation(RunRequest{Prompt: "fix", ReadOnly: false})
		defer cleanupB()
		if !slices.Contains(argsB, "--dangerously-bypass-approvals-and-sandbox") {
			t.Fatalf("expected codex bypass flag, got %v", argsB)
		}
	})

	t.Run("qoder", func(t *testing.T) {
		a := NewCLIAgentAdapter(store.AgentProfile{ID: "qoder", Kind: "qoder", PermissionMode: "accept_edits"})

		argsRO, stdinRO, _, cleanupRO := a.buildInvocation(RunRequest{Prompt: "spec", SystemPrompt: "sys", ReadOnly: true})
		defer cleanupRO()
		if stdinRO != "spec" {
			t.Fatalf("expected qoder raw stdin, got %q", stdinRO)
		}
		if !containsPair(argsRO, "--permission-mode", "dont_ask") || !containsPair(argsRO, "--disallowed-tools", "Edit,Write,NotebookEdit,Bash") {
			t.Fatalf("expected qoder read-only args, got %v", argsRO)
		}

		argsW, _, _, cleanupW := a.buildInvocation(RunRequest{Prompt: "impl", Model: "qwen3", ReadOnly: false})
		defer cleanupW()
		if !containsPair(argsW, "--permission-mode", "accept_edits") || !containsPair(argsW, "-m", "qwen3") {
			t.Fatalf("unexpected qoder write args: %v", argsW)
		}

		aBypass := NewCLIAgentAdapter(store.AgentProfile{ID: "qoder", Kind: "qoder", PermissionMode: "bypass"})
		argsB, _, _, cleanupB := aBypass.buildInvocation(RunRequest{Prompt: "impl", ReadOnly: false})
		defer cleanupB()
		if !containsPair(argsB, "--permission-mode", "bypass_permissions") || !slices.Contains(argsB, "--dangerously-skip-permissions") {
			t.Fatalf("expected qoder bypass args, got %v", argsB)
		}
	})

	for _, kind := range []string{"workbuddy", "codebuddy"} {
		t.Run(kind, func(t *testing.T) {
			a := NewCLIAgentAdapter(store.AgentProfile{ID: kind, Kind: kind, PermissionMode: "acceptEdits"})

			argsRO, stdinRO, _, cleanupRO := a.buildInvocation(RunRequest{Prompt: "design", SystemPrompt: "sys", ReadOnly: true})
			defer cleanupRO()
			if stdinRO != "design" || !containsPair(argsRO, "--permission-mode", "plan") || !slices.Contains(argsRO, "--no-session-persistence") {
				t.Fatalf("unexpected %s read-only args=%v stdin=%q", kind, argsRO, stdinRO)
			}

			argsW, _, _, cleanupW := a.buildInvocation(RunRequest{Prompt: "impl", Model: "glm-5.1", ReadOnly: false})
			defer cleanupW()
			if !containsPair(argsW, "--permission-mode", "acceptEdits") || !containsPair(argsW, "--model", "glm-5.1") {
				t.Fatalf("unexpected %s write args: %v", kind, argsW)
			}

			aBypass := NewCLIAgentAdapter(store.AgentProfile{ID: kind, Kind: kind, PermissionMode: "bypass"})
			argsB, _, _, cleanupB := aBypass.buildInvocation(RunRequest{Prompt: "impl", ReadOnly: false})
			defer cleanupB()
			if !containsPair(argsB, "--permission-mode", "bypassPermissions") || !slices.Contains(argsB, "-y") {
				t.Fatalf("expected %s bypass args, got %v", kind, argsB)
			}
		})
	}

	t.Run("agy", func(t *testing.T) {
		a := NewCLIAgentAdapter(store.AgentProfile{ID: "agy", Kind: "agy", PermissionMode: "auto"})

		argsRO, _, _, cleanupRO := a.buildInvocation(RunRequest{Prompt: "spec", ReadOnly: true})
		defer cleanupRO()
		if slices.Contains(argsRO, "--dangerously-skip-permissions") {
			t.Fatalf("agy read-only must not pass --dangerously-skip-permissions: %v", argsRO)
		}
		if !containsPair(argsRO, "--output-format", "stream-json") {
			t.Fatalf("expected agy stream-json flag: %v", argsRO)
		}

		argsW, _, _, cleanupW := a.buildInvocation(RunRequest{Prompt: "impl", Model: "gemini-2.5-pro", ReadOnly: false})
		defer cleanupW()
		if !slices.Contains(argsW, "--dangerously-skip-permissions") || !containsPair(argsW, "--model", "gemini-2.5-pro") {
			t.Fatalf("unexpected agy write args: %v", argsW)
		}
	})
}

func TestParseStdoutLineStreamFormats(t *testing.T) {
	qoder := NewCLIAgentAdapter(store.AgentProfile{ID: "qoder", Kind: "qoder"})
	evType, display, finalChunk, errMsg := qoder.parseStdoutLine(`{"type":"result","is_error":true,"result":"Not logged in · Please run /login"}`)
	if evType != "error" || errMsg != "Not logged in · Please run /login" || finalChunk != "" || display == "" {
		t.Fatalf("unexpected qoder error result parse: ev=%q display=%q final=%q err=%q", evType, display, finalChunk, errMsg)
	}

	wb := NewCLIAgentAdapter(store.AgentProfile{ID: "workbuddy", Kind: "workbuddy"})
	evType, display, _, errMsg = wb.parseStdoutLine(`{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["Authentication required. Please use /login command to sign in to your account"]}`)
	if evType != "error" || !strings.Contains(errMsg, "Authentication required") || display == "" {
		t.Fatalf("unexpected workbuddy errors[] parse: ev=%q display=%q err=%q", evType, display, errMsg)
	}

	evType, display, _, errMsg = wb.parseStdoutLine(`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}`)
	if evType != "text" || display != "Hello world" || errMsg != "" {
		t.Fatalf("unexpected assistant parse: ev=%q display=%q err=%q", evType, display, errMsg)
	}

	evType, display, finalChunk, errMsg = wb.parseStdoutLine(`{"type":"result","is_error":false,"result":"Final OK"}`)
	if evType != "result" || finalChunk != "Final OK" || errMsg != "" || display == "" {
		t.Fatalf("unexpected success result parse: ev=%q display=%q final=%q err=%q", evType, display, finalChunk, errMsg)
	}

	codex := NewCLIAgentAdapter(store.AgentProfile{ID: "codex", Kind: "codex"})
	evType, display, finalChunk, errMsg = codex.parseStdoutLine(`{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"Codex reply"}}`)
	if evType != "text" || display != "Codex reply" || finalChunk != "Codex reply" || errMsg != "" {
		t.Fatalf("unexpected codex agent_message parse: ev=%q display=%q final=%q err=%q", evType, display, finalChunk, errMsg)
	}

	evType, display, _, errMsg = codex.parseStdoutLine(`{"type":"item.completed","item":{"type":"command_execution","command":"go test ./..."}}`)
	if evType != "tool" || display != "[cmd] go test ./..." || errMsg != "" {
		t.Fatalf("unexpected codex command_execution parse: ev=%q display=%q err=%q", evType, display, errMsg)
	}

	evType, _, _, errMsg = codex.parseStdoutLine(`{"type":"turn.failed","error":{"message":"rate limit exceeded"}}`)
	if evType != "error" || errMsg != "rate limit exceeded" {
		t.Fatalf("unexpected codex turn.failed parse: ev=%q err=%q", evType, errMsg)
	}
}

func TestResolveExecutableExplicitMJSAndNativeFirst(t *testing.T) {
	if !IsNativeFirstKind("codex") || !IsNativeFirstKind("qoder") || !IsNativeFirstKind("workbuddy") || !IsNativeFirstKind("codebuddy") || !IsNativeFirstKind("agy") {
		t.Fatalf("expected all 5 target kinds to be native-first")
	}
	if IsNativeFirstKind("claude") || IsNativeFirstKind("aider") {
		t.Fatalf("unexpected native-first flag for claude/aider")
	}

	nodePath := findNodeExecutable()
	if nodePath == "" {
		t.Skip("node executable not available on host")
	}

	tmpDir := t.TempDir()
	mjsFile := filepath.Join(tmpDir, "fake-agent.mjs")
	if err := os.WriteFile(mjsFile, []byte("console.log('1.0.0');\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved, ok := ResolveExecutable(store.AgentProfile{
		ID:      "custom-wb",
		Kind:    "workbuddy",
		Command: mjsFile,
	})
	if !ok {
		t.Fatalf("expected ResolveExecutable to resolve .mjs file")
	}
	if resolved.Executable != nodePath || !reflect.DeepEqual(resolved.PrefixArgs, []string{mjsFile}) {
		t.Fatalf("unexpected resolved mjs wrapper: %+v", resolved)
	}
}

func TestLocalDiscoverySmokeCheck(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	reg := NewRegistry(db)
	profiles, err := reg.DetectAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var detected []string
	for _, p := range profiles {
		if p.Detected {
			detected = append(detected, p.ID+"="+p.DetectedPath)
		}
	}
	t.Logf("Detected agents on this host (%d): %v", len(detected), detected)
}

func containsPair(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}
