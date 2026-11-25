package gitops

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const (
	WorkspaceStatusUnconfigured = "unconfigured"
	WorkspaceStatusReady        = "ready"
	WorkspaceStatusPendingClone = "pending_clone"
	WorkspaceStatusInvalid      = "invalid"
)

// WorkspaceCheckResult describes the validation state of a repository's local folder.
type WorkspaceCheckResult struct {
	Path       string `json:"path"`
	Status     string `json:"status"`
	OK         bool   `json:"ok"`
	IsGitRepo  bool   `json:"isGitRepo"`
	RemoteURL  string `json:"remoteUrl,omitempty"`
	RemoteRepo string `json:"remoteRepo,omitempty"`
	Message    string `json:"message"`
}

// Manager manages bare mirror repositories, isolated git worktrees, diffs, pushes, and verification commands.
type Manager struct {
	reposDir     string
	worktreesDir string
	repoLocksMu  sync.Mutex
	repoLocks    map[string]*sync.Mutex
}

// NewManager creates a gitops Manager rooted at reposDir and worktreesDir.
func NewManager(reposDir, worktreesDir string) *Manager {
	return &Manager{
		reposDir:     reposDir,
		worktreesDir: worktreesDir,
		repoLocks:    make(map[string]*sync.Mutex),
	}
}

func (m *Manager) lockRepo(repoFullName string) *sync.Mutex {
	m.repoLocksMu.Lock()
	defer m.repoLocksMu.Unlock()
	mu, ok := m.repoLocks[repoFullName]
	if !ok {
		mu = &sync.Mutex{}
		m.repoLocks[repoFullName] = mu
	}
	return mu
}

// MirrorPath returns the local bare repository path for owner/name.
func (m *Manager) MirrorPath(repoFullName string) string {
	safe := strings.ReplaceAll(repoFullName, "/", "__")
	safe = strings.ReplaceAll(safe, "\\", "__")
	return filepath.Join(m.reposDir, safe+".git")
}

// RemoveMirror deletes the cached bare mirror directory for owner/name if present.
func (m *Manager) RemoveMirror(repoFullName string) error {
	if strings.TrimSpace(repoFullName) == "" {
		return nil
	}
	mu := m.lockRepo(repoFullName)
	mu.Lock()
	defer mu.Unlock()
	return os.RemoveAll(m.MirrorPath(repoFullName))
}

// WorktreePath returns the legacy isolated worktree path for a given pipeline run ID.
func (m *Manager) WorktreePath(runID string) string {
	return filepath.Join(m.worktreesDir, runID)
}

// NormalizeLocalPath cleans and validates an absolute non-root local workspace path.
func NormalizeLocalPath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if !filepath.IsAbs(trimmed) {
		return "", errors.New("工作区路径必须是绝对路径")
	}
	clean := filepath.Clean(trimmed)
	if filepath.Dir(clean) == clean {
		return "", errors.New("不能直接使用磁盘或系统根目录作为仓库工作区")
	}
	return clean, nil
}

func isHTTPURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func isNetworkRemoteURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "file://") {
		return false
	}
	if strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "ssh://") ||
		strings.HasPrefix(lower, "git://") {
		return true
	}
	return !filepath.IsAbs(trimmed) && strings.Contains(trimmed, "@") && strings.Contains(trimmed, ":")
}

// ParseGitHubOwnerRepo extracts "owner/repo" from GitHub HTTPS or SSH remote URLs.
func ParseGitHubOwnerRepo(remoteURL string) (string, bool) {
	trimmed := strings.TrimSpace(remoteURL)
	if trimmed == "" {
		return "", false
	}
	var pathPart string
	if strings.Contains(trimmed, "://") {
		u, err := url.Parse(trimmed)
		if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
			return "", false
		}
		pathPart = u.Path
	} else if atIdx := strings.Index(trimmed, "@"); atIdx != -1 {
		rest := trimmed[atIdx+1:]
		colonIdx := strings.Index(rest, ":")
		if colonIdx <= 0 {
			return "", false
		}
		host := rest[:colonIdx]
		if !strings.EqualFold(host, "github.com") {
			return "", false
		}
		pathPart = rest[colonIdx+1:]
	} else {
		return "", false
	}

	pathPart = strings.Trim(pathPart, "/")
	pathPart = strings.TrimSuffix(pathPart, ".git")
	parts := strings.Split(pathPart, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
}

// RepoWorktreesRoot returns the sibling worktrees directory for a bound local repo path:
// <parentDir>\.dragon-worktrees\<repoFolderName>.
func RepoWorktreesRoot(localPath string) string {
	clean, err := NormalizeLocalPath(localPath)
	if err != nil || clean == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(clean), ".dragon-worktrees", filepath.Base(clean))
}

// WorktreePathForRun returns the sibling worktree path for a given issue number and run ID.
func (m *Manager) WorktreePathForRun(localPath string, issueNumber int, runID string) string {
	root := RepoWorktreesRoot(localPath)
	if root == "" {
		return ""
	}
	shortID := runID
	if len(shortID) > 6 {
		shortID = shortID[:6]
	}
	if shortID == "" {
		shortID = "run"
	}
	return filepath.Join(root, fmt.Sprintf("issue-%d-%s", issueNumber, shortID))
}

// ValidateLocalWorkspace checks whether localPath is ready, pending clone, unconfigured, or invalid.
func (m *Manager) ValidateLocalWorkspace(ctx context.Context, localPath, expectedFullName string) WorkspaceCheckResult {
	trimmed := strings.TrimSpace(localPath)
	if trimmed == "" {
		return WorkspaceCheckResult{
			Status:  WorkspaceStatusUnconfigured,
			OK:      false,
			Message: "尚未配置本地工作区目录",
		}
	}
	clean, err := NormalizeLocalPath(trimmed)
	if err != nil {
		return WorkspaceCheckResult{
			Path:    trimmed,
			Status:  WorkspaceStatusInvalid,
			OK:      false,
			Message: err.Error(),
		}
	}
	info, err := os.Stat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return WorkspaceCheckResult{
				Path:    clean,
				Status:  WorkspaceStatusPendingClone,
				OK:      true,
				Message: "目录尚不存在，首次执行时将自动克隆仓库（也可点击立即克隆）",
			}
		}
		return WorkspaceCheckResult{
			Path:    clean,
			Status:  WorkspaceStatusInvalid,
			OK:      false,
			Message: fmt.Sprintf("无法访问目录: %v", err),
		}
	}
	if !info.IsDir() {
		return WorkspaceCheckResult{
			Path:    clean,
			Status:  WorkspaceStatusInvalid,
			OK:      false,
			Message: "配置路径不是文件夹",
		}
	}

	if _, gitErr := os.Stat(filepath.Join(clean, ".git")); gitErr == nil {
		if _, err := runGit(ctx, clean, nil, "rev-parse", "--git-dir"); err != nil {
			return WorkspaceCheckResult{
				Path:    clean,
				Status:  WorkspaceStatusInvalid,
				OK:      false,
				Message: "目录包含无效或损坏的 .git 信息",
			}
		}
		remoteRaw, err := runGit(ctx, clean, nil, "remote", "get-url", "origin")
		if err != nil {
			return WorkspaceCheckResult{
				Path:      clean,
				Status:    WorkspaceStatusInvalid,
				OK:        false,
				IsGitRepo: true,
				Message:   "本地 Git 仓库缺少 origin 远程仓库配置",
			}
		}
		remoteURL := strings.TrimSpace(remoteRaw)
		parsedRepo, isGitHub := ParseGitHubOwnerRepo(remoteURL)
		if expectedFullName != "" {
			if isGitHub {
				if !strings.EqualFold(parsedRepo, expectedFullName) {
					return WorkspaceCheckResult{
						Path:       clean,
						Status:     WorkspaceStatusInvalid,
						OK:         false,
						IsGitRepo:  true,
						RemoteURL:  remoteURL,
						RemoteRepo: parsedRepo,
						Message:    fmt.Sprintf("本地仓库 origin (%s) 与当前 GitHub 仓库 (%s) 不匹配", parsedRepo, expectedFullName),
					}
				}
			} else if isNetworkRemoteURL(remoteURL) {
				return WorkspaceCheckResult{
					Path:      clean,
					Status:    WorkspaceStatusInvalid,
					OK:        false,
					IsGitRepo: true,
					RemoteURL: remoteURL,
					Message:   fmt.Sprintf("本地仓库 origin (%s) 与当前 GitHub 仓库 (%s) 不匹配", remoteURL, expectedFullName),
				}
			}
		}
		return WorkspaceCheckResult{
			Path:       clean,
			Status:     WorkspaceStatusReady,
			OK:         true,
			IsGitRepo:  true,
			RemoteURL:  remoteURL,
			RemoteRepo: parsedRepo,
			Message:    "已就绪：已关联匹配的本地 Git 仓库",
		}
	}

	entries, err := os.ReadDir(clean)
	if err != nil {
		return WorkspaceCheckResult{
			Path:    clean,
			Status:  WorkspaceStatusInvalid,
			OK:      false,
			Message: fmt.Sprintf("读取目录内容失败: %v", err),
		}
	}
	if len(entries) == 0 {
		return WorkspaceCheckResult{
			Path:    clean,
			Status:  WorkspaceStatusPendingClone,
			OK:      true,
			Message: "目录为空，首次执行时将自动克隆仓库（也可点击立即克隆）",
		}
	}
	return WorkspaceCheckResult{
		Path:    clean,
		Status:  WorkspaceStatusInvalid,
		OK:      false,
		Message: "目标目录非空且不是 Git 仓库，请选择空目录或对应的本地仓库目录",
	}
}

func authConfigArgs(token string) []string {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return []string{"-c", "http.extraheader=AUTHORIZATION: basic " + encoded}
}

func runGit(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	ConfigureHiddenCommand(cmd)
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	env = append(env, extraEnv...)
	cmd.Env = env

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	if err != nil {
		return out, fmt.Errorf("git %s failed: %w\n%s", sanitizeGitArgs(args), err, strings.TrimSpace(out))
	}
	return out, nil
}

func sanitizeGitArgs(args []string) string {
	cleaned := make([]string, len(args))
	for i, a := range args {
		if strings.Contains(strings.ToLower(a), "http.extraheader=") {
			cleaned[i] = "http.extraheader=***"
		} else {
			cleaned[i] = a
		}
	}
	return strings.Join(cleaned, " ")
}

// EnsureMirror clones or fetches the bare mirror repository without writing credentials into git config.
func (m *Manager) EnsureMirror(ctx context.Context, repoFullName, cloneURL, token string) (string, error) {
	mu := m.lockRepo(repoFullName)
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(m.reposDir, 0o755); err != nil {
		return "", err
	}
	mirrorPath := m.MirrorPath(repoFullName)
	if _, err := os.Stat(filepath.Join(mirrorPath, "HEAD")); os.IsNotExist(err) {
		args := append(authConfigArgs(token), "clone", "--bare", cloneURL, mirrorPath)
		if _, err := runGit(ctx, "", nil, args...); err != nil {
			return "", err
		}
		if _, err := runGit(ctx, mirrorPath, nil, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
			return "", err
		}
	} else {
		_, _ = runGit(ctx, mirrorPath, nil, "remote", "set-url", "origin", cloneURL)
	}

	fetchArgs := append(authConfigArgs(token), "fetch", "--prune", "origin", "+refs/heads/*:refs/remotes/origin/*")
	if _, err := runGit(ctx, mirrorPath, nil, fetchArgs...); err != nil {
		return "", err
	}
	return mirrorPath, nil
}

// CreateWorktree creates an isolated branch worktree based on origin/<defaultBranch>.
func (m *Manager) CreateWorktree(ctx context.Context, repoFullName, defaultBranch, branchName, worktreePath string) error {
	mu := m.lockRepo(repoFullName)
	mu.Lock()
	defer mu.Unlock()

	mirrorPath := m.MirrorPath(repoFullName)
	if _, err := os.Stat(filepath.Join(mirrorPath, "HEAD")); err != nil {
		return fmt.Errorf("本地镜像仓库不存在 (%s): %w", mirrorPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return err
	}
	// Clean stale directory or previous registration if retrying.
	_, _ = runGit(ctx, mirrorPath, nil, "worktree", "remove", "--force", worktreePath)
	_, _ = runGit(ctx, mirrorPath, nil, "worktree", "prune")
	_ = os.RemoveAll(worktreePath)

	baseRef := "origin/" + defaultBranch
	if _, err := runGit(ctx, mirrorPath, nil, "rev-parse", "--verify", baseRef); err != nil {
		// Fallback to HEAD if remote tracking ref has a non-standard name.
		baseRef = "HEAD"
	}

	if _, err := runGit(ctx, mirrorPath, nil, "worktree", "add", "-B", branchName, worktreePath, baseRef); err != nil {
		return err
	}
	return nil
}

// RemoveWorktree removes an isolated worktree cleanly.
func (m *Manager) RemoveWorktree(ctx context.Context, repoFullName, worktreePath string) error {
	if worktreePath == "" {
		return nil
	}
	mu := m.lockRepo(repoFullName)
	mu.Lock()
	defer mu.Unlock()

	mirrorPath := m.MirrorPath(repoFullName)
	if _, err := os.Stat(mirrorPath); err == nil {
		_, _ = runGit(ctx, mirrorPath, nil, "worktree", "remove", "--force", worktreePath)
		_, _ = runGit(ctx, mirrorPath, nil, "worktree", "prune")
	}
	return os.RemoveAll(worktreePath)
}

// EnsureLocalRepo clones cloneURL into localPath if pending_clone, or fetches origin if already cloned.
// It never mutates the user's existing origin remote URL or checked-out branch.
func (m *Manager) EnsureLocalRepo(ctx context.Context, localPath, expectedFullName, cloneURL, token string) error {
	clean, err := NormalizeLocalPath(localPath)
	if err != nil {
		return err
	}
	if clean == "" {
		return errors.New("未配置本地工作区目录")
	}

	mu := m.lockRepo("local:" + strings.ToLower(clean))
	mu.Lock()
	defer mu.Unlock()

	check := m.ValidateLocalWorkspace(ctx, clean, expectedFullName)
	if !check.OK {
		return errors.New(check.Message)
	}

	if check.Status == WorkspaceStatusPendingClone {
		if strings.TrimSpace(cloneURL) == "" {
			return errors.New("缺少远程仓库克隆地址")
		}
		if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
			return err
		}
		var args []string
		if isHTTPURL(cloneURL) {
			args = append(args, authConfigArgs(token)...)
		}
		args = append(args, "clone", cloneURL, clean)
		if _, err := runGit(ctx, "", nil, args...); err != nil {
			return err
		}
		return nil
	}

	var fetchArgs []string
	if isHTTPURL(check.RemoteURL) {
		fetchArgs = append(fetchArgs, authConfigArgs(token)...)
	}
	fetchArgs = append(fetchArgs, "fetch", "--prune", "origin")
	if _, err := runGit(ctx, clean, nil, fetchArgs...); err != nil {
		return err
	}
	return nil
}

// CreateWorktreeFromLocal creates an isolated branch worktree from localPath based on origin/<defaultBranch>.
func (m *Manager) CreateWorktreeFromLocal(ctx context.Context, localPath, defaultBranch, branchName, worktreePath string) error {
	clean, err := NormalizeLocalPath(localPath)
	if err != nil {
		return err
	}
	if clean == "" {
		return errors.New("未配置本地工作区目录")
	}

	mu := m.lockRepo("local:" + strings.ToLower(clean))
	mu.Lock()
	defer mu.Unlock()

	if _, err := os.Stat(filepath.Join(clean, ".git")); err != nil {
		return fmt.Errorf("本地仓库目录未初始化 (%s): %w", clean, err)
	}
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return err
	}

	_, _ = runGit(ctx, clean, nil, "worktree", "remove", "--force", worktreePath)
	_, _ = runGit(ctx, clean, nil, "worktree", "prune")
	_ = os.RemoveAll(worktreePath)

	baseRef := "origin/" + defaultBranch
	if _, err := runGit(ctx, clean, nil, "rev-parse", "--verify", baseRef); err != nil {
		baseRef = "HEAD"
	}
	if _, err := runGit(ctx, clean, nil, "worktree", "add", "-B", branchName, worktreePath, baseRef); err != nil {
		return err
	}
	return nil
}

// RemoveWorktreeFromLocal cleanly removes a sibling worktree created from localPath.
func (m *Manager) RemoveWorktreeFromLocal(ctx context.Context, localPath, worktreePath string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return nil
	}
	clean, err := NormalizeLocalPath(localPath)
	if err == nil && clean != "" {
		mu := m.lockRepo("local:" + strings.ToLower(clean))
		mu.Lock()
		defer mu.Unlock()

		if _, err := os.Stat(filepath.Join(clean, ".git")); err == nil {
			_, _ = runGit(ctx, clean, nil, "worktree", "remove", "--force", worktreePath)
			_, _ = runGit(ctx, clean, nil, "worktree", "prune")
		}
	}
	if err := os.RemoveAll(worktreePath); err != nil {
		return err
	}
	if clean != "" {
		repoWorktreesDir := filepath.Dir(worktreePath)
		if strings.EqualFold(filepath.Clean(repoWorktreesDir), filepath.Clean(RepoWorktreesRoot(clean))) {
			_ = os.Remove(repoWorktreesDir)
			_ = os.Remove(filepath.Dir(repoWorktreesDir))
		}
	}
	return nil
}

// RemoveAllRepoWorktrees deletes only temporary sibling worktrees for localPath; it never deletes localPath itself.
func (m *Manager) RemoveAllRepoWorktrees(ctx context.Context, localPath string) error {
	clean, err := NormalizeLocalPath(localPath)
	if err != nil || clean == "" {
		return nil
	}
	mu := m.lockRepo("local:" + strings.ToLower(clean))
	mu.Lock()
	defer mu.Unlock()

	root := RepoWorktreesRoot(clean)
	if root == "" {
		return nil
	}
	hasGit := false
	if _, err := os.Stat(filepath.Join(clean, ".git")); err == nil {
		hasGit = true
	}
	if entries, err := os.ReadDir(root); err == nil && hasGit {
		for _, e := range entries {
			wt := filepath.Join(root, e.Name())
			_, _ = runGit(ctx, clean, nil, "worktree", "remove", "--force", wt)
		}
		_, _ = runGit(ctx, clean, nil, "worktree", "prune")
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(root))
	return nil
}

// GetDiff returns the unified diff of changes (including committed branch changes and working tree edits/untracked files)
// relative to baseRef (e.g. "origin/main").
func (m *Manager) GetDiff(ctx context.Context, worktreePath, defaultBranch string) (string, error) {
	if worktreePath == "" {
		return "", errors.New("worktree 路径为空")
	}
	if _, err := os.Stat(worktreePath); err != nil {
		return "", err
	}

	baseRef := "origin/" + defaultBranch
	if _, err := runGit(ctx, worktreePath, nil, "rev-parse", "--verify", baseRef); err != nil {
		baseRef = "HEAD"
	}

	trackedDiff, err := runGit(ctx, worktreePath, nil, "diff", "--no-color", baseRef)
	if err != nil {
		return "", err
	}

	untrackedRaw, err := runGit(ctx, worktreePath, nil, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return trackedDiff, nil
	}

	var out strings.Builder
	out.WriteString(trackedDiff)

	for _, line := range strings.Split(strings.TrimSpace(untrackedRaw), "\n") {
		rel := strings.TrimSpace(line)
		if rel == "" {
			continue
		}
		abs := filepath.Join(worktreePath, rel)
		data, err := os.ReadFile(abs)
		if err != nil || len(data) > 256*1024 || bytes.IndexByte(data, 0) >= 0 {
			out.WriteString(fmt.Sprintf("\ndiff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1 @@\n+[binary or large file omitted]\n", rel, rel, rel))
			continue
		}
		lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
		out.WriteString(fmt.Sprintf("\ndiff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", rel, rel, rel, len(lines)))
		for _, l := range lines {
			out.WriteString("+" + strings.TrimRight(l, "\r") + "\n")
		}
	}
	return strings.TrimSpace(out.String()), nil
}

// HasChanges reports whether the worktree has uncommitted or committed changes compared to baseRef.
func (m *Manager) HasChanges(ctx context.Context, worktreePath, defaultBranch string) (bool, error) {
	status, err := runGit(ctx, worktreePath, nil, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(status) != "" {
		return true, nil
	}
	baseRef := "origin/" + defaultBranch
	if _, err := runGit(ctx, worktreePath, nil, "rev-parse", "--verify", baseRef); err != nil {
		return false, nil
	}
	ahead, err := runGit(ctx, worktreePath, nil, "rev-list", "--count", baseRef+"..HEAD")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(ahead) != "0", nil
}

// CommitAndPush stages all changes in worktreePath (if any), commits, and pushes branchName to origin.
func (m *Manager) CommitAndPush(ctx context.Context, worktreePath, branchName, commitMessage, token string) (string, error) {
	if _, err := runGit(ctx, worktreePath, nil, "add", "-A"); err != nil {
		return "", err
	}

	status, err := runGit(ctx, worktreePath, nil, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status) != "" {
		commitArgs := []string{
			"-c", "user.name=Dragon Bot",
			"-c", "user.email=dragon-bot@users.noreply.github.com",
			"commit", "-m", commitMessage,
		}
		if _, err := runGit(ctx, worktreePath, nil, commitArgs...); err != nil {
			return "", err
		}
	}

	sha, err := runGit(ctx, worktreePath, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}

	var pushArgs []string
	if remoteURL, err := runGit(ctx, worktreePath, nil, "remote", "get-url", "origin"); err == nil && isHTTPURL(remoteURL) {
		pushArgs = append(pushArgs, authConfigArgs(token)...)
	}
	pushArgs = append(pushArgs, "push", "--force-with-lease", "-u", "origin", branchName)
	if _, err := runGit(ctx, worktreePath, nil, pushArgs...); err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

// RunShellCommand executes a build, lint, or test command inside workDir without flashing a console window.
func RunShellCommand(ctx context.Context, workDir, command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", nil
	}
	cmd := shellCommand(ctx, command)
	cmd.Dir = workDir

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}
