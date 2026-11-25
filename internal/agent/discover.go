package agent

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"dragon/internal/store"
)

// ResolvedExec describes a dynamically resolved local executable (and optional interpreter wrapper).
type ResolvedExec struct {
	Executable  string
	PrefixArgs  []string
	ExtraEnv    map[string]string
	DisplayPath string
	Source      string // "custom" | "path" | "windowsapps" | "desktop-bundle" | "user-dir"
}

// ResolveExecutable resolves an AgentProfile's executable dynamically across PATH, desktop bundles,
// WindowsApps packages, and common per-user install directories without mutating profile.Command.
func ResolveExecutable(profile store.AgentProfile) (ResolvedExec, bool) {
	cmd := strings.TrimSpace(profile.Command)
	kind := strings.ToLower(strings.TrimSpace(profile.Kind))
	if cmd == "" {
		cmd = defaultCommandForKind(kind)
	}
	if cmd == "" {
		return ResolvedExec{}, false
	}

	// Explicit path or JS/MJS entrypoint configured by the user.
	if hasPathSep(cmd) || hasJSExtension(cmd) {
		return resolveExplicitCommand(cmd, kind)
	}

	// Fast-path: direct PATH hit.
	if path, err := exec.LookPath(cmd); err == nil {
		return ResolvedExec{
			Executable:  path,
			DisplayPath: path,
			Source:      "path",
		}, true
	}

	// Kind-specific dynamic discovery for bundled desktop CLIs and non-PATH locations.
	switch kind {
	case "codex":
		if res, ok := discoverCodexDesktop(); ok {
			return res, true
		}
		if res, ok := lookUpCandidatePaths("user-dir", candidateUserBins("codex")...); ok {
			return res, true
		}

	case "qoder":
		if path, err := exec.LookPath("qodercli"); err == nil {
			return ResolvedExec{Executable: path, DisplayPath: path, Source: "path"}, true
		}
		if res, ok := lookUpCandidatePaths("user-dir", candidateQoderBins()...); ok {
			return res, true
		}

	case "workbuddy":
		if res, ok := discoverWorkBuddyCLI(); ok {
			return res, true
		}

	case "codebuddy":
		for _, alt := range []string{"codebuddy", "cbc"} {
			if path, err := exec.LookPath(alt); err == nil {
				return ResolvedExec{Executable: path, DisplayPath: path, Source: "path"}, true
			}
		}
		var cands []string
		cands = append(cands, candidateUserBins("codebuddy")...)
		cands = append(cands, candidateUserBins("cbc")...)
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			cands = append(cands,
				filepath.Join(home, ".codebuddy", "bin", exeName("codebuddy")),
				filepath.Join(home, ".codebuddy", "bin", exeName("cbc")),
			)
		}
		if res, ok := lookUpCandidatePaths("user-dir", cands...); ok {
			return res, true
		}

	case "agy":
		if path, err := exec.LookPath("agy"); err == nil {
			return ResolvedExec{Executable: path, DisplayPath: path, Source: "path"}, true
		}
		if res, ok := lookUpCandidatePaths("user-dir", candidateAgyBins()...); ok {
			return res, true
		}
	}

	return ResolvedExec{}, false
}

// InstallHint returns a concise installation command/instruction for the given agent kind.
func InstallHint(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "codex":
		return "运行 npm install -g @openai/codex（或从 Microsoft Store 安装 Codex 桌面端）"
	case "qoder":
		return "运行 npm install -g @qoder-ai/qodercli"
	case "workbuddy":
		return "安装 WorkBuddy 桌面客户端（并确保系统已安装 Node.js >= 18）"
	case "codebuddy":
		return "运行 npm install -g @tencent-ai/codebuddy-code"
	case "agy":
		return "PowerShell 执行: irm https://antigravity.google/cli/install.ps1 | iex"
	case "claude":
		return "运行 npm install -g @anthropic-ai/claude-code"
	case "gemini":
		return "运行 npm install -g @google/gemini-cli"
	case "opencode":
		return "运行 npm install -g opencode-ai"
	case "kimi":
		return "运行 uv tool install kimi-cli（或 pip install kimi-cli）"
	case "aider":
		return "运行 pip install aider-chat"
	default:
		return "请确认对应 Coding CLI 已安装并加入系统 PATH，或在详情中填写绝对路径"
	}
}

// LoginHint returns a concise authentication instruction for the given agent kind.
func LoginHint(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "codex":
		return "在终端执行 codex login，或在 Codex 桌面端登录 ChatGPT 账号"
	case "qoder":
		return "在终端执行 qodercli login 完成账号登录"
	case "workbuddy":
		return "请先登录 WorkBuddy 或在终端通过内置 CLI 执行 /login 完成账号认证（或改选已配置 API Key 的 Provider 模型）"
	case "codebuddy":
		return "在终端执行 codebuddy login 完成腾讯云账号登录"
	case "agy":
		return "在终端执行 agy 完成 Google 账号登录"
	case "claude":
		return "在终端执行 claude 完成登录，或在 Providers 中配置 Anthropic Key"
	default:
		return "请在对应 CLI 终端完成登录，或在 Providers 中配置 API Key"
	}
}

func defaultCommandForKind(kind string) string {
	switch kind {
	case "qoder":
		return "qodercli"
	case "workbuddy":
		return "workbuddy"
	case "codebuddy":
		return "codebuddy"
	case "codex":
		return "codex"
	case "agy":
		return "agy"
	case "claude":
		return "claude"
	default:
		return ""
	}
}

func resolveExplicitCommand(cmd, kind string) (ResolvedExec, bool) {
	if isRegularFile(cmd) {
		abs, err := filepath.Abs(cmd)
		if err != nil {
			abs = cmd
		}
		if hasJSExtension(abs) || isNodeShebangScript(abs) {
			nodePath := findNodeExecutable()
			if nodePath == "" {
				return ResolvedExec{}, false
			}
			extraEnv := map[string]string(nil)
			if kind == "workbuddy" && strings.EqualFold(filepath.Base(abs), "codebuddy") {
				extraEnv = map[string]string{"CODEBUDDY_FORCE_LITE_WB_BUNDLE": "1"}
			}
			return ResolvedExec{
				Executable:  nodePath,
				PrefixArgs:  []string{abs},
				ExtraEnv:    extraEnv,
				DisplayPath: abs,
				Source:      "custom",
			}, true
		}
		return ResolvedExec{
			Executable:  abs,
			DisplayPath: abs,
			Source:      "custom",
		}, true
	}

	if path, err := exec.LookPath(cmd); err == nil {
		return ResolvedExec{
			Executable:  path,
			DisplayPath: path,
			Source:      "custom",
		}, true
	}
	return ResolvedExec{}, false
}

func discoverWorkBuddyCLI() (ResolvedExec, bool) {
	nodePath := findNodeExecutable()
	if nodePath == "" {
		return ResolvedExec{}, false
	}
	roots := discoverDesktopInstallDirs("WorkBuddy")
	for _, root := range roots {
		binEntry := filepath.Join(root, "resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
		if isRegularFile(binEntry) {
			return ResolvedExec{
				Executable:  nodePath,
				PrefixArgs:  []string{binEntry},
				ExtraEnv:    map[string]string{"CODEBUDDY_FORCE_LITE_WB_BUNDLE": "1"},
				DisplayPath: binEntry,
				Source:      "desktop-bundle",
			}, true
		}
		mjsEntry := filepath.Join(root, "resources", "app.asar.unpacked", "cli", "dist", "codebuddy-lite-wb.mjs")
		if isRegularFile(mjsEntry) {
			return ResolvedExec{
				Executable:  nodePath,
				PrefixArgs:  []string{mjsEntry},
				DisplayPath: mjsEntry,
				Source:      "desktop-bundle",
			}, true
		}
	}
	return ResolvedExec{}, false
}

func lookUpCandidatePaths(source string, candidates ...string) (ResolvedExec, bool) {
	for _, cand := range candidates {
		if cand == "" {
			continue
		}
		if isRegularFile(cand) {
			return ResolvedExec{
				Executable:  cand,
				DisplayPath: cand,
				Source:      source,
			}, true
		}
	}
	return ResolvedExec{}, false
}

func candidateQoderBins() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".qoder", "bin", "qodercli", exeName("qodercli")),
		filepath.Join(home, ".local", "bin", exeName("qodercli")),
	}
}

func candidateAgyBins() []string {
	var cands []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cands = append(cands,
			filepath.Join(home, ".local", "bin", exeName("agy")),
			filepath.Join(home, ".antigravity", "bin", exeName("agy")),
			filepath.Join(home, ".gemini", "antigravity-cli", "bin", exeName("agy")),
		)
	}
	if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
		cands = append(cands,
			filepath.Join(localApp, "Antigravity", "agy.exe"),
			filepath.Join(localApp, "Programs", "agy", "agy.exe"),
		)
	}
	cands = append(cands, candidateUserBins("agy")...)
	return cands
}

func candidateUserBins(name string) []string {
	var cands []string
	if appData := os.Getenv("APPDATA"); appData != "" {
		cands = append(cands,
			filepath.Join(appData, "npm", name+".cmd"),
			filepath.Join(appData, "npm", exeName(name)),
		)
	}
	if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
		cands = append(cands,
			filepath.Join(localApp, "pnpm", name+".cmd"),
			filepath.Join(localApp, "pnpm", exeName(name)),
		)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cands = append(cands,
			filepath.Join(home, ".local", "bin", exeName(name)),
			filepath.Join(home, ".npm-global", "bin", name),
		)
	}
	return cands
}

func hasPathSep(s string) bool {
	return strings.ContainsAny(s, `/\`)
}

func hasJSExtension(s string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(s)))
	return ext == ".mjs" || ext == ".js" || ext == ".cjs"
}

func isNodeShebangScript(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 128)
	n, _ := f.Read(buf)
	if n <= 0 {
		return false
	}
	firstLine := string(buf[:n])
	if idx := strings.IndexByte(firstLine, '\n'); idx != -1 {
		firstLine = firstLine[:idx]
	}
	firstLine = strings.ToLower(strings.TrimSpace(firstLine))
	return strings.HasPrefix(firstLine, "#!") && strings.Contains(firstLine, "node")
}

func isRegularFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func exeName(base string) string {
	if os.PathSeparator == '\\' && !strings.HasSuffix(strings.ToLower(base), ".exe") {
		return base + ".exe"
	}
	return base
}

func trimBOMAndNulls(b []byte) string {
	return strings.TrimSpace(string(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})))
}
