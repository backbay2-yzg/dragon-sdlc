package appservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"dragon/internal/agent"
	"dragon/internal/config"
	"dragon/internal/gitops"
	"dragon/internal/hosting"
	"dragon/internal/hosting/github"
	"dragon/internal/pipeline"
	"dragon/internal/poller"
	"dragon/internal/release"
	"dragon/internal/router"
	"dragon/internal/secrets"
	"dragon/internal/store"
)

func (c *AppCore) activeProvider() hosting.Provider {
	if c.Provider != nil {
		return c.Provider
	}
	if c.GitHubClient != nil {
		return c.GitHubClient
	}
	return nil
}

// --- DashboardService ---

type DashboardOverview struct {
	Stats            store.DashboardStats  `json:"stats"`
	ActiveRuns       []store.PipelineRun   `json:"activeRuns"`
	PendingApprovals []store.PipelineRun   `json:"pendingApprovals"`
	RecentReleases   []store.ReleaseRecord `json:"recentReleases"`
	Repos            []store.Repository    `json:"repos"`
}

type DashboardService struct{ core *AppCore }

func NewDashboardService(core *AppCore) *DashboardService { return &DashboardService{core: core} }

func (s *DashboardService) GetOverview() (*DashboardOverview, error) {
	stats, err := s.core.DB.GetDashboardStats()
	if err != nil {
		return nil, err
	}
	runs, err := s.core.DB.ListPipelineRuns("all", "all", 40)
	if err != nil {
		return nil, err
	}
	var active, waiting []store.PipelineRun
	for _, r := range runs {
		switch r.Status {
		case "queued", "running":
			active = append(active, r)
		case "waiting_approval":
			waiting = append(waiting, r)
		}
	}
	rels, err := s.core.DB.ListReleaseRecords("all", 10)
	if err != nil {
		return nil, err
	}
	repos, err := s.core.DB.ListRepositories()
	if err != nil {
		return nil, err
	}
	return &DashboardOverview{
		Stats:            stats,
		ActiveRuns:       active,
		PendingApprovals: waiting,
		RecentReleases:   rels,
		Repos:            repos,
	}, nil
}

func (s *DashboardService) SyncAll() ([]poller.SyncSummary, error) {
	return s.core.Poller.SyncAllNow(s.core.BaseCtx)
}

// --- RepoService ---

type AddRepoRequest struct {
	FullName          string `json:"fullName"` // owner/repo or https://github.com/owner/repo
	LocalPath         string `json:"localPath"`
	TriggerLabel      string `json:"triggerLabel"`
	AutonomyMode      string `json:"autonomyMode"`
	DefaultTemplateID string `json:"defaultTemplateId"`
	ReleaseMode       string `json:"releaseMode"`
	BuildCmd          string `json:"buildCmd"`
	TestCmd           string `json:"testCmd"`
	LintCmd           string `json:"lintCmd"`
}

// RemoteRepoOption represents a remote GitHub repository available to connect.
type RemoteRepoOption struct {
	Owner            string `json:"owner"`
	Name             string `json:"name"`
	FullName         string `json:"fullName"`
	Description      string `json:"description"`
	DefaultBranch    string `json:"defaultBranch"`
	CloneURL         string `json:"cloneUrl"`
	Private          bool   `json:"private"`
	Archived         bool   `json:"archived"`
	Fork             bool   `json:"fork"`
	Language         string `json:"language"`
	OpenIssuesCount  int    `json:"openIssuesCount"`
	PushedAt         string `json:"pushedAt"`
	HTMLURL          string `json:"htmlUrl"`
	OwnerType        string `json:"ownerType"`
	OwnerAvatarURL   string `json:"ownerAvatarUrl"`
	CanAdmin         bool   `json:"canAdmin"`
	CanPush          bool   `json:"canPush"`
	CanPull          bool   `json:"canPull"`
	AlreadyConnected bool   `json:"alreadyConnected"`
}

// BatchAddReposRequest adds multiple repositories with shared default automation settings.
type BatchAddReposRequest struct {
	FullNames          []string `json:"fullNames"`
	WorkspaceParentDir string   `json:"workspaceParentDir"`
	TriggerLabel       string   `json:"triggerLabel"`
	AutonomyMode       string   `json:"autonomyMode"`
	DefaultTemplateID  string   `json:"defaultTemplateId"`
	ReleaseMode        string   `json:"releaseMode"`
}

// BatchAddReposResult reports added, skipped, and failed items from BatchAddRepos.
type BatchAddReposResult struct {
	Added   []store.Repository `json:"added"`
	Skipped []string           `json:"skipped"`
	Errors  []string           `json:"errors"`
}

// RepoHealthResult reports remote connectivity, permissions, and branch check details for one repo.
type RepoHealthResult struct {
	RepoID            string `json:"repoId"`
	FullName          string `json:"fullName"`
	OK                bool   `json:"ok"`
	CanPush           bool   `json:"canPush"`
	CanAdmin          bool   `json:"canAdmin"`
	CanPull           bool   `json:"canPull"`
	Private           bool   `json:"private"`
	Archived          bool   `json:"archived"`
	DefaultBranch     string `json:"defaultBranch"`
	BranchUpdated     bool   `json:"branchUpdated"`
	LatencyMs         int64  `json:"latencyMs"`
	CheckedAt         string `json:"checkedAt"`
	LocalPath         string `json:"localPath"`
	WorkspaceStatus   string `json:"workspaceStatus"`
	WorkspaceMessage  string `json:"workspaceMessage"`
	Error             string `json:"error,omitempty"`
	PermissionWarning string `json:"permissionWarning,omitempty"`
}

type RepoService struct{ core *AppCore }

func NewRepoService(core *AppCore) *RepoService { return &RepoService{core: core} }

func (s *RepoService) ListRepos() ([]store.Repository, error) {
	return s.core.DB.ListRepositories()
}

func (s *RepoService) ListRemoteRepos() ([]RemoteRepoOption, error) {
	prov := s.core.activeProvider()
	if prov == nil {
		return nil, errors.New("未初始化托管平台客户端")
	}
	remotes, err := prov.ListUserRepos(s.core.BaseCtx)
	if err != nil {
		return nil, err
	}
	existing, err := s.core.DB.ListRepositories()
	if err != nil {
		return nil, err
	}
	connectedSet := make(map[string]bool, len(existing))
	for _, r := range existing {
		connectedSet[strings.ToLower(strings.TrimSpace(r.FullName))] = true
	}

	out := make([]RemoteRepoOption, 0, len(remotes))
	for _, r := range remotes {
		out = append(out, RemoteRepoOption{
			Owner:            r.Owner,
			Name:             r.Name,
			FullName:         r.FullName,
			Description:      r.Description,
			DefaultBranch:    r.DefaultBranch,
			CloneURL:         r.CloneURL,
			Private:          r.Private,
			Archived:         r.Archived,
			Fork:             r.Fork,
			Language:         r.Language,
			OpenIssuesCount:  r.OpenIssuesCount,
			PushedAt:         r.PushedAt,
			HTMLURL:          r.HTMLURL,
			OwnerType:        r.OwnerType,
			OwnerAvatarURL:   r.OwnerAvatarURL,
			CanAdmin:         r.CanAdmin,
			CanPush:          r.CanPush,
			CanPull:          r.CanPull,
			AlreadyConnected: connectedSet[strings.ToLower(strings.TrimSpace(r.FullName))],
		})
	}
	return out, nil
}

func parseOwnerRepo(input string) (string, string, error) {
	clean := strings.TrimSpace(input)
	clean = strings.TrimPrefix(clean, "https://github.com/")
	clean = strings.TrimPrefix(clean, "http://github.com/")
	clean = strings.TrimSuffix(clean, ".git")
	clean = strings.Trim(clean, "/")
	parts := strings.Split(clean, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("请输入有效的仓库地址，例如 owner/repo")
	}
	return parts[0], parts[1], nil
}

func (s *RepoService) TestRepoConnection(fullName string) (*hosting.RemoteRepo, error) {
	owner, name, err := parseOwnerRepo(fullName)
	if err != nil {
		return nil, err
	}
	prov := s.core.activeProvider()
	if prov == nil {
		return nil, errors.New("未初始化托管平台客户端")
	}
	return prov.GetRepo(s.core.BaseCtx, owner, name)
}

func collectUsedLocalPaths(existing []store.Repository) map[string]bool {
	used := make(map[string]bool, len(existing))
	for _, r := range existing {
		if clean, err := gitops.NormalizeLocalPath(r.LocalPath); err == nil && clean != "" {
			used[strings.ToLower(clean)] = true
		}
	}
	return used
}

func hasDuplicateLocalPath(existing []store.Repository, excludeRepoID, candidate string) (string, bool) {
	clean, err := gitops.NormalizeLocalPath(candidate)
	if err != nil || clean == "" {
		return "", false
	}
	for _, r := range existing {
		if excludeRepoID != "" && r.ID == excludeRepoID {
			continue
		}
		other, err := gitops.NormalizeLocalPath(r.LocalPath)
		if err != nil || other == "" {
			continue
		}
		if strings.EqualFold(other, clean) {
			return r.FullName, true
		}
	}
	return "", false
}

func resolveDefaultLocalPath(parentDir, owner, name string, usedPaths map[string]bool) string {
	parent, err := gitops.NormalizeLocalPath(parentDir)
	if err != nil || parent == "" {
		return ""
	}
	cand := filepath.Join(parent, name)
	if usedPaths[strings.ToLower(cand)] {
		cand = filepath.Join(parent, owner+"__"+name)
	}
	usedPaths[strings.ToLower(cand)] = true
	return cand
}

func resolveExistingDir(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	cur := filepath.Clean(trimmed)
	for {
		if info, err := os.Stat(cur); err == nil && info.IsDir() {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

func (s *RepoService) AddRepo(req AddRepoRequest) (*store.Repository, error) {
	owner, name, err := parseOwnerRepo(req.FullName)
	if err != nil {
		return nil, err
	}
	defaultBranch := "main"
	isPrivate := false
	description := ""
	if prov := s.core.activeProvider(); prov != nil {
		if remote, err := prov.GetRepo(s.core.BaseCtx, owner, name); err == nil {
			if remote.Owner != "" {
				owner = remote.Owner
			}
			if remote.Name != "" {
				name = remote.Name
			}
			if remote.DefaultBranch != "" {
				defaultBranch = remote.DefaultBranch
			}
			isPrivate = remote.Private
			description = remote.Description
		}
	}
	canonicalFullName := owner + "/" + name
	if _, err := s.core.DB.GetRepositoryByFullName(canonicalFullName); err == nil {
		return nil, errors.New("仓库 " + canonicalFullName + " 已接入")
	}

	settings := s.core.GetSettingsSnapshot()
	existingRepos, _ := s.core.DB.ListRepositories()
	localPath := strings.TrimSpace(req.LocalPath)
	if localPath == "" && strings.TrimSpace(settings.DefaultWorkspaceRoot) != "" {
		usedPaths := collectUsedLocalPaths(existingRepos)
		localPath = resolveDefaultLocalPath(settings.DefaultWorkspaceRoot, owner, name, usedPaths)
	}
	if localPath != "" {
		clean, err := gitops.NormalizeLocalPath(localPath)
		if err != nil {
			return nil, err
		}
		if dupRepo, dup := hasDuplicateLocalPath(existingRepos, "", clean); dup {
			return nil, fmt.Errorf("工作区目录 %s 已被仓库 %s 绑定", clean, dupRepo)
		}
		if s.core.GitMgr != nil {
			ws := s.core.GitMgr.ValidateLocalWorkspace(s.core.BaseCtx, clean, canonicalFullName)
			if !ws.OK {
				return nil, errors.New(ws.Message)
			}
		}
		localPath = clean
	}

	autonomy := req.AutonomyMode
	if autonomy == "" {
		autonomy = settings.DefaultAutonomyMode
	}
	trigger := strings.TrimSpace(req.TriggerLabel)
	if trigger == "" {
		trigger = "dragon-auto"
	}
	tplID := req.DefaultTemplateID
	if tplID == "" {
		tplID = "auto"
	}
	relMode := req.ReleaseMode
	if relMode == "" {
		relMode = "batch"
	}

	repo := store.Repository{
		ID:                uuid.NewString(),
		Provider:          "github",
		Owner:             owner,
		Name:              name,
		FullName:          canonicalFullName,
		Description:       description,
		Private:           isPrivate,
		DefaultBranch:     defaultBranch,
		LocalPath:         localPath,
		PollIntervalSec:   120,
		TriggerLabel:      trigger,
		AutonomyMode:      autonomy,
		DefaultTemplateID: tplID,
		ReleaseMode:       relMode,
		MergeStrategy:     "squash",
		BuildCmd:          req.BuildCmd,
		TestCmd:           req.TestCmd,
		LintCmd:           req.LintCmd,
		MaxTestRetries:    2,
		MaxCIRetries:      2,
		Enabled:           true,
		CreatedAt:         store.NowRFC3339(),
	}
	if err := s.core.DB.UpsertRepository(repo); err != nil {
		return nil, err
	}
	// Best-effort initial sync in background so adding a repo returns immediately.
	go func(id string) {
		_, _ = s.core.Poller.SyncRepoNow(s.core.BaseCtx, id)
	}(repo.ID)
	return &repo, nil
}

func (s *RepoService) BatchAddRepos(req BatchAddReposRequest) (*BatchAddReposResult, error) {
	result := &BatchAddReposResult{
		Added:   []store.Repository{},
		Skipped: []string{},
		Errors:  []string{},
	}

	parentDir := strings.TrimSpace(req.WorkspaceParentDir)
	if parentDir == "" {
		parentDir = strings.TrimSpace(s.core.GetSettingsSnapshot().DefaultWorkspaceRoot)
	}
	if parentDir != "" {
		normParent, err := gitops.NormalizeLocalPath(parentDir)
		if err != nil {
			return nil, fmt.Errorf("工作区父目录无效: %w", err)
		}
		if info, statErr := os.Stat(normParent); statErr == nil && !info.IsDir() {
			return nil, errors.New("工作区父路径不是文件夹")
		}
		parentDir = normParent
	}

	existingRepos, _ := s.core.DB.ListRepositories()
	usedPaths := collectUsedLocalPaths(existingRepos)

	seen := make(map[string]bool, len(req.FullNames))
	for _, raw := range req.FullNames {
		owner, name, err := parseOwnerRepo(raw)
		if err != nil {
			if strings.TrimSpace(raw) != "" {
				result.Errors = append(result.Errors, raw+": "+err.Error())
			}
			continue
		}
		key := strings.ToLower(owner + "/" + name)
		if seen[key] {
			continue
		}
		seen[key] = true

		if _, err := s.core.DB.GetRepositoryByFullName(owner + "/" + name); err == nil {
			result.Skipped = append(result.Skipped, owner+"/"+name)
			continue
		}

		localPath := ""
		if parentDir != "" {
			localPath = resolveDefaultLocalPath(parentDir, owner, name, usedPaths)
		}
		added, err := s.AddRepo(AddRepoRequest{
			FullName:          owner + "/" + name,
			LocalPath:         localPath,
			TriggerLabel:      req.TriggerLabel,
			AutonomyMode:      req.AutonomyMode,
			DefaultTemplateID: req.DefaultTemplateID,
			ReleaseMode:       req.ReleaseMode,
		})
		if err != nil {
			result.Errors = append(result.Errors, owner+"/"+name+": "+err.Error())
			continue
		}
		if added != nil {
			result.Added = append(result.Added, *added)
		}
	}
	return result, nil
}

func (s *RepoService) UpdateRepo(repo store.Repository) (*store.Repository, error) {
	if repo.ID == "" {
		return nil, errors.New("缺少仓库 ID")
	}
	if repo.FullName == "" && repo.Owner != "" && repo.Name != "" {
		repo.FullName = repo.Owner + "/" + repo.Name
	}
	if strings.TrimSpace(repo.LocalPath) != "" {
		clean, err := gitops.NormalizeLocalPath(repo.LocalPath)
		if err != nil {
			return nil, err
		}
		existingRepos, _ := s.core.DB.ListRepositories()
		if dupRepo, dup := hasDuplicateLocalPath(existingRepos, repo.ID, clean); dup {
			return nil, fmt.Errorf("工作区目录 %s 已被仓库 %s 绑定", clean, dupRepo)
		}
		if s.core.GitMgr != nil {
			ws := s.core.GitMgr.ValidateLocalWorkspace(s.core.BaseCtx, clean, repo.FullName)
			if !ws.OK {
				return nil, errors.New(ws.Message)
			}
		}
		repo.LocalPath = clean
	} else {
		repo.LocalPath = ""
	}
	if err := s.core.DB.UpsertRepository(repo); err != nil {
		return nil, err
	}
	updated, err := s.core.DB.GetRepository(repo.ID)
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *RepoService) ValidateRepoWorkspace(repoID, fullName, localPath string) gitops.WorkspaceCheckResult {
	if s.core.GitMgr == nil {
		return gitops.WorkspaceCheckResult{
			Path:    localPath,
			Status:  gitops.WorkspaceStatusInvalid,
			OK:      false,
			Message: "Git 工作区管理器未初始化",
		}
	}
	res := s.core.GitMgr.ValidateLocalWorkspace(s.core.BaseCtx, localPath, fullName)
	if res.OK && res.Path != "" {
		existingRepos, _ := s.core.DB.ListRepositories()
		if dupRepo, dup := hasDuplicateLocalPath(existingRepos, repoID, res.Path); dup {
			res.Status = gitops.WorkspaceStatusInvalid
			res.OK = false
			res.Message = fmt.Sprintf("该目录已被仓库 %s 绑定，请为每个仓库指定独立目录", dupRepo)
		}
	}
	return res
}

func (s *RepoService) CloneRepoWorkspace(repoID, localPath string) (*gitops.WorkspaceCheckResult, error) {
	if s.core.GitMgr == nil {
		return nil, errors.New("Git 工作区管理器未初始化")
	}
	repo, err := s.core.DB.GetRepository(repoID)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSpace(localPath)
	if target == "" {
		target = repo.LocalPath
	}
	clean, err := gitops.NormalizeLocalPath(target)
	if err != nil {
		return nil, err
	}
	if clean == "" {
		return nil, errors.New("请先填写目标本地工作区目录")
	}

	existingRepos, _ := s.core.DB.ListRepositories()
	if dupRepo, dup := hasDuplicateLocalPath(existingRepos, repo.ID, clean); dup {
		return nil, fmt.Errorf("工作区目录 %s 已被仓库 %s 绑定", clean, dupRepo)
	}

	ws := s.core.GitMgr.ValidateLocalWorkspace(s.core.BaseCtx, clean, repo.FullName)
	if !ws.OK {
		return nil, errors.New(ws.Message)
	}

	cloneURL := fmt.Sprintf("https://github.com/%s.git", repo.FullName)
	if prov := s.core.activeProvider(); prov != nil {
		if remote, err := prov.GetRepo(s.core.BaseCtx, repo.Owner, repo.Name); err == nil && remote.CloneURL != "" {
			cloneURL = remote.CloneURL
		}
	}
	token := ""
	if s.core.AuthMgr != nil {
		if tok, _, err := s.core.AuthMgr.ResolveToken(s.core.BaseCtx, s.core.GetSettingsSnapshot().AuthMethod); err == nil {
			token = tok
		}
	}

	if err := s.core.GitMgr.EnsureLocalRepo(s.core.BaseCtx, clean, repo.FullName, cloneURL, token); err != nil {
		return nil, err
	}
	if repo.LocalPath != clean {
		repo.LocalPath = clean
		if err := s.core.DB.UpsertRepository(repo); err != nil {
			return nil, err
		}
	}

	updated := s.core.GitMgr.ValidateLocalWorkspace(s.core.BaseCtx, clean, repo.FullName)
	return &updated, nil
}

func (s *RepoService) SetRepoEnabled(id string, enabled bool) (*store.Repository, error) {
	if err := s.core.DB.SetRepositoryEnabled(id, enabled); err != nil {
		return nil, err
	}
	updated, err := s.core.DB.GetRepository(id)
	if err != nil {
		return nil, err
	}
	if enabled && s.core.Poller != nil {
		go func(repoID string) {
			_, _ = s.core.Poller.SyncRepoNow(s.core.BaseCtx, repoID)
		}(updated.ID)
	}
	return &updated, nil
}

func (s *RepoService) CheckRepoHealth(id string) (*RepoHealthResult, error) {
	repo, err := s.core.DB.GetRepository(id)
	if err != nil {
		return nil, err
	}
	ws := s.ValidateRepoWorkspace(repo.ID, repo.FullName, repo.LocalPath)
	now := store.NowRFC3339()
	prov := s.core.activeProvider()
	if prov == nil {
		return &RepoHealthResult{
			RepoID:           repo.ID,
			FullName:         repo.FullName,
			OK:               false,
			CheckedAt:        now,
			LocalPath:        repo.LocalPath,
			WorkspaceStatus:  ws.Status,
			WorkspaceMessage: ws.Message,
			Error:            "未初始化托管平台客户端",
		}, nil
	}

	start := time.Now()
	remote, err := prov.GetRepo(s.core.BaseCtx, repo.Owner, repo.Name)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return &RepoHealthResult{
			RepoID:           repo.ID,
			FullName:         repo.FullName,
			OK:               false,
			LatencyMs:        latency,
			CheckedAt:        now,
			LocalPath:        repo.LocalPath,
			WorkspaceStatus:  ws.Status,
			WorkspaceMessage: ws.Message,
			Error:            err.Error(),
		}, nil
	}

	branchUpdated := false
	changed := false
	if remote.DefaultBranch != "" && remote.DefaultBranch != repo.DefaultBranch {
		repo.DefaultBranch = remote.DefaultBranch
		branchUpdated = true
		changed = true
	}
	if remote.Private != repo.Private {
		repo.Private = remote.Private
		changed = true
	}
	if remote.Description != repo.Description {
		repo.Description = remote.Description
		changed = true
	}
	if changed {
		_ = s.core.DB.UpsertRepository(repo)
	}

	var warn string
	if remote.Archived {
		warn = "仓库已归档（只读）"
	} else if !remote.CanPush {
		warn = "当前 Token 仅有只读权限，无法推送分支或创建 PR"
	}

	return &RepoHealthResult{
		RepoID:            repo.ID,
		FullName:          repo.FullName,
		OK:                true,
		CanPush:           remote.CanPush,
		CanAdmin:          remote.CanAdmin,
		CanPull:           remote.CanPull,
		Private:           remote.Private,
		Archived:          remote.Archived,
		DefaultBranch:     repo.DefaultBranch,
		BranchUpdated:     branchUpdated,
		LatencyMs:         latency,
		CheckedAt:         now,
		LocalPath:         repo.LocalPath,
		WorkspaceStatus:   ws.Status,
		WorkspaceMessage:  ws.Message,
		PermissionWarning: warn,
	}, nil
}

func (s *RepoService) DeleteRepo(id string) error {
	return s.DeleteRepoWithOptions(id, false)
}

func (s *RepoService) DeleteRepoWithOptions(id string, cleanLocalCache bool) error {
	repo, err := s.core.DB.GetRepository(id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if err := s.core.DB.DeleteRepository(id); err != nil {
		return err
	}
	if cleanLocalCache && s.core.GitMgr != nil {
		if repo.LocalPath != "" {
			_ = s.core.GitMgr.RemoveAllRepoWorktrees(s.core.BaseCtx, repo.LocalPath)
		}
		if repo.FullName != "" {
			_ = s.core.GitMgr.RemoveMirror(repo.FullName)
		}
	}
	return nil
}

func (s *RepoService) SyncRepo(id string) (*poller.SyncSummary, error) {
	return s.core.Poller.SyncRepoNow(s.core.BaseCtx, id)
}

// --- IssueService ---

type IssueService struct{ core *AppCore }

func NewIssueService(core *AppCore) *IssueService { return &IssueService{core: core} }

func (s *IssueService) ListIssues(repoID, status, search string) ([]store.Issue, error) {
	return s.core.DB.ListIssues(repoID, status, search)
}

func (s *IssueService) GetIssue(id string) (*store.Issue, error) {
	it, err := s.core.DB.GetIssue(id)
	if err != nil {
		return nil, err
	}
	return &it, nil
}

func (c *AppCore) listProvidersWithKeyState() ([]store.ModelProvider, error) {
	list, err := c.DB.ListModelProviders()
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].HasKey = c.Secrets.Has(secrets.ProviderSecretKey(list[i].ID))
	}
	return list, nil
}

func (s *IssueService) PreviewRoute(repoID, issueID string) (*router.RoutingDecision, error) {
	repo, err := s.core.DB.GetRepository(repoID)
	if err != nil {
		return nil, err
	}
	issue, err := s.core.DB.GetIssue(issueID)
	if err != nil {
		return nil, err
	}
	rules, _ := s.core.DB.ListRoutingRules()
	profiles, _ := s.core.DB.ListAgentProfiles()
	templates, _ := s.core.DB.ListTemplates()
	providers, _ := s.core.listProvidersWithKeyState()
	routingCfg, _ := s.core.DB.GetRoutingConfig()
	return s.core.Router.Route(s.core.BaseCtx, router.RouteRequest{
		Repo:          repo,
		Issue:         issue,
		Rules:         rules,
		Profiles:      profiles,
		Templates:     templates,
		Providers:     providers,
		RoutingConfig: routingCfg,
		Settings:      s.core.GetSettingsSnapshot(),
	})
}

func (s *IssueService) StartIssueRun(req pipeline.StartRunRequest) (*store.PipelineRun, error) {
	return s.core.Orchestrator.StartIssueRun(s.core.BaseCtx, req)
}

func (s *IssueService) IgnoreIssue(id string) error {
	return s.core.DB.UpdateIssueStatus(id, "ignored")
}

func (s *IssueService) ReopenIssue(id string) error {
	return s.core.DB.UpdateIssueStatus(id, "backlog")
}

// --- PipelineService ---

type ArtifactEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type RunDetail struct {
	Run            store.PipelineRun      `json:"run"`
	Stages         []store.StageExecution `json:"stages"`
	Logs           []store.RunLog         `json:"logs"`
	Artifacts      []ArtifactEntry        `json:"artifacts"`
	WorktreeExists bool                   `json:"worktreeExists"`
}

type PipelineService struct{ core *AppCore }

func NewPipelineService(core *AppCore) *PipelineService { return &PipelineService{core: core} }

func (s *PipelineService) ListRuns(repoID, status string) ([]store.PipelineRun, error) {
	return s.core.DB.ListPipelineRuns(repoID, status, 100)
}

func (s *PipelineService) GetRunDetail(runID string) (*RunDetail, error) {
	run, err := s.core.DB.GetPipelineRun(runID)
	if err != nil {
		return nil, err
	}
	stages, err := s.core.DB.ListStagesByRun(runID)
	if err != nil {
		return nil, err
	}
	logs, err := s.core.DB.ListRunLogs(runID, 600)
	if err != nil {
		return nil, err
	}
	var artifacts []ArtifactEntry
	artDir := filepath.Join(s.core.Paths.ArtifactsDir, runID)
	if entries, err := os.ReadDir(artDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			var sz int64
			if err == nil {
				sz = info.Size()
			}
			artifacts = append(artifacts, ArtifactEntry{Name: e.Name(), Size: sz})
		}
		sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })
	}
	worktreeExists := false
	if run.WorktreePath != "" {
		if info, statErr := os.Stat(run.WorktreePath); statErr == nil && info.IsDir() {
			worktreeExists = true
		}
	}
	return &RunDetail{
		Run:            run,
		Stages:         stages,
		Logs:           logs,
		Artifacts:      artifacts,
		WorktreeExists: worktreeExists,
	}, nil
}

func (s *PipelineService) CleanupRunWorktree(runID string) error {
	return s.core.Orchestrator.CleanupRunWorktree(runID)
}

func (s *PipelineService) GetStageArtifact(runID, filename string) (string, error) {
	clean := filepath.Base(filename)
	if clean == "." || clean == ".." {
		return "", errors.New("非法文件名")
	}
	p := filepath.Join(s.core.Paths.ArtifactsDir, runID, clean)
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *PipelineService) GetRunDiff(runID string) (string, error) {
	run, err := s.core.DB.GetPipelineRun(runID)
	if err != nil {
		return "", err
	}
	repo, err := s.core.DB.GetRepository(run.RepoID)
	if err == nil && run.WorktreePath != "" {
		if _, err := os.Stat(run.WorktreePath); err == nil {
			if diff, err := s.core.GitMgr.GetDiff(s.core.BaseCtx, run.WorktreePath, repo.DefaultBranch); err == nil {
				return diff, nil
			}
		}
	}
	// Fallback to saved artifact diff after worktree cleanup.
	if saved, err := s.GetStageArtifact(runID, "changes.diff"); err == nil {
		return saved, nil
	}
	return "", nil
}

func (s *PipelineService) ApproveRun(runID string) error {
	return s.core.Orchestrator.ApproveRun(runID)
}

func (s *PipelineService) RequestChanges(runID, feedback string) error {
	return s.core.Orchestrator.RequestChanges(runID, feedback)
}

func (s *PipelineService) AbortRun(runID string) error {
	return s.core.Orchestrator.AbortRun(runID)
}

func (s *PipelineService) RetryRun(runID string) (*store.PipelineRun, error) {
	return s.core.Orchestrator.RetryRun(s.core.BaseCtx, runID)
}

func (s *PipelineService) ListStageDescriptors() []store.StageDescriptor {
	return store.AllStageDescriptors()
}

// --- AgentService ---

type AgentService struct{ core *AppCore }

func NewAgentService(core *AppCore) *AgentService { return &AgentService{core: core} }

func (s *AgentService) ListAgents() ([]store.AgentProfile, error) {
	return s.core.DB.ListAgentProfiles()
}

func (s *AgentService) DetectAgents() ([]store.AgentProfile, error) {
	return s.core.Registry.DetectAll(s.core.BaseCtx)
}

func (s *AgentService) SaveAgent(profile store.AgentProfile) (*store.AgentProfile, error) {
	if strings.TrimSpace(profile.ID) == "" {
		profile.ID = "custom-" + uuid.NewString()[:8]
	}
	if strings.TrimSpace(profile.Kind) == "" {
		profile.Kind = "custom"
	}
	if err := s.core.DB.UpsertAgentProfile(profile); err != nil {
		return nil, err
	}
	// Re-detect only the saved profile so saving a custom CLI doesn't wait on all other CLIs.
	if updated, err := s.core.Registry.DetectOne(s.core.BaseCtx, profile.ID); err == nil {
		return &updated, nil
	}
	got, err := s.core.DB.GetAgentProfile(profile.ID)
	if err != nil {
		return nil, err
	}
	return &got, nil
}

func (s *AgentService) DeleteAgent(id string) error {
	return s.core.DB.DeleteAgentProfile(id)
}

func (s *AgentService) ListRoutingRules() ([]store.RoutingRule, error) {
	return s.core.DB.ListRoutingRules()
}

func (s *AgentService) SaveRoutingRule(rule store.RoutingRule) (*store.RoutingRule, error) {
	if strings.TrimSpace(rule.ID) == "" {
		rule.ID = uuid.NewString()
	}
	if rule.Priority <= 0 {
		rule.Priority = 100
	}
	if err := s.core.DB.UpsertRoutingRule(rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

func (s *AgentService) DeleteRoutingRule(id string) error {
	return s.core.DB.DeleteRoutingRule(id)
}

func (s *AgentService) ListTemplates() ([]store.PipelineTemplate, error) {
	return s.core.DB.ListTemplates()
}

func (s *AgentService) SaveTemplate(tpl store.PipelineTemplate) (*store.PipelineTemplate, error) {
	if strings.TrimSpace(tpl.ID) == "" {
		tpl.ID = "tpl-" + uuid.NewString()[:8]
	}
	if err := s.core.DB.UpsertTemplate(tpl); err != nil {
		return nil, err
	}
	got, err := s.core.DB.GetTemplate(tpl.ID)
	if err != nil {
		return nil, err
	}
	return &got, nil
}

func (s *AgentService) DeleteTemplate(id string) error {
	return s.core.DB.DeleteTemplate(id)
}

// --- Magpie-Style Agent Inline Controls, Providers, Profiles & Routing ---

type ModelCatalogOption struct {
	Ref          string `json:"ref"` // "providerId/modelId"
	ProviderID   string `json:"providerId"`
	ProviderName string `json:"providerName"`
	Protocol     string `json:"protocol"`
	ModelID      string `json:"modelId"`
	Name         string `json:"name"`
	CostTier     string `json:"costTier"`
	ContextK     int    `json:"contextK"`
	HasKey       bool   `json:"hasKey"`
}

type AgentWorkspaceSnapshot struct {
	Agents        []store.AgentProfile     `json:"agents"`
	Providers     []store.ModelProvider    `json:"providers"`
	Models        []ModelCatalogOption     `json:"models"`
	Profiles      []store.WorkspaceProfile `json:"profiles"`
	Rules         []store.RoutingRule      `json:"rules"`
	Templates     []store.PipelineTemplate `json:"templates"`
	Stages        []store.StageDescriptor  `json:"stages"`
	RoutingConfig store.RoutingConfig      `json:"routingConfig"`
}

type ApplyProfileResult struct {
	Profile       store.WorkspaceProfile `json:"profile"`
	Agents        []store.AgentProfile   `json:"agents"`
	RoutingConfig store.RoutingConfig    `json:"routingConfig"`
}

type SimulateRouteRequest struct {
	RepoID string   `json:"repoId"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
}

type TestAgentResult struct {
	AgentID    string `json:"agentId"`
	OK         bool   `json:"ok"`
	Detected   bool   `json:"detected"`
	BinPath    string `json:"binPath"`
	Version    string `json:"version"`
	DurationMs int64  `json:"durationMs"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	Hint       string `json:"hint,omitempty"`
}

func (s *AgentService) SetAgentModel(agentID, providerID, model string) (*store.AgentProfile, error) {
	p, err := s.core.DB.GetAgentProfile(agentID)
	if err != nil {
		return nil, err
	}
	p.ProviderID = strings.TrimSpace(providerID)
	p.Model = strings.TrimSpace(model)
	if err := s.core.DB.UpsertAgentProfile(p); err != nil {
		return nil, err
	}
	got, err := s.core.DB.GetAgentProfile(agentID)
	if err != nil {
		return nil, err
	}
	return &got, nil
}

func (s *AgentService) SetAgentEnabled(agentID string, enabled bool) (*store.AgentProfile, error) {
	p, err := s.core.DB.GetAgentProfile(agentID)
	if err != nil {
		return nil, err
	}
	p.Enabled = enabled
	if err := s.core.DB.UpsertAgentProfile(p); err != nil {
		return nil, err
	}
	got, err := s.core.DB.GetAgentProfile(agentID)
	if err != nil {
		return nil, err
	}
	return &got, nil
}

func (s *AgentService) SaveAgentTiers(agentID, tiersJSON string) (*store.AgentProfile, error) {
	p, err := s.core.DB.GetAgentProfile(agentID)
	if err != nil {
		return nil, err
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(tiersJSON), &parsed); err != nil {
		return nil, errors.New("Tiers JSON 格式无效")
	}
	b, _ := json.Marshal(parsed)
	p.TiersJSON = string(b)
	if err := s.core.DB.UpsertAgentProfile(p); err != nil {
		return nil, err
	}
	got, err := s.core.DB.GetAgentProfile(agentID)
	if err != nil {
		return nil, err
	}
	return &got, nil
}

func (s *AgentService) ListProviders() ([]store.ModelProvider, error) {
	return s.core.listProvidersWithKeyState()
}

func (s *AgentService) SaveProvider(provider store.ModelProvider) (*store.ModelProvider, error) {
	if strings.TrimSpace(provider.ID) == "" {
		provider.ID = "prov-" + uuid.NewString()[:8]
	}
	if strings.TrimSpace(provider.Name) == "" {
		provider.Name = provider.ID
	}
	if err := s.core.DB.UpsertModelProvider(provider); err != nil {
		return nil, err
	}
	got, err := s.core.DB.GetModelProvider(provider.ID)
	if err != nil {
		return nil, err
	}
	got.HasKey = s.core.Secrets.Has(secrets.ProviderSecretKey(got.ID))
	return &got, nil
}

func (s *AgentService) DeleteProvider(id string) error {
	_ = s.core.Secrets.Delete(secrets.ProviderSecretKey(id))
	return s.core.DB.DeleteModelProvider(id)
}

func (s *AgentService) SaveProviderAPIKey(providerID, apiKey string) error {
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return errors.New("API Key 不能为空")
	}
	return s.core.Secrets.Set(secrets.ProviderSecretKey(providerID), trimmed)
}

func (s *AgentService) ClearProviderAPIKey(providerID string) error {
	return s.core.Secrets.Delete(secrets.ProviderSecretKey(providerID))
}

func buildModelCatalog(providers []store.ModelProvider) []ModelCatalogOption {
	var opts []ModelCatalogOption
	for _, p := range providers {
		if !p.Enabled {
			continue
		}
		for _, m := range p.Models() {
			opts = append(opts, ModelCatalogOption{
				Ref:          p.ID + "/" + m.ID,
				ProviderID:   p.ID,
				ProviderName: p.Name,
				Protocol:     p.Protocol,
				ModelID:      m.ID,
				Name:         m.Name,
				CostTier:     m.CostTier,
				ContextK:     m.ContextK,
				HasKey:       p.HasKey,
			})
		}
	}
	return opts
}

func (s *AgentService) ListAllModels() ([]ModelCatalogOption, error) {
	providers, err := s.core.listProvidersWithKeyState()
	if err != nil {
		return nil, err
	}
	return buildModelCatalog(providers), nil
}

// GetWorkspaceSnapshot returns all datasets needed by the Agents & Routing page in one RPC call.
func (s *AgentService) GetWorkspaceSnapshot() (*AgentWorkspaceSnapshot, error) {
	agents, err := s.core.DB.ListAgentProfiles()
	if err != nil {
		return nil, err
	}
	providers, err := s.core.listProvidersWithKeyState()
	if err != nil {
		return nil, err
	}
	profiles, err := s.core.DB.ListWorkspaceProfiles()
	if err != nil {
		return nil, err
	}
	rules, err := s.core.DB.ListRoutingRules()
	if err != nil {
		return nil, err
	}
	templates, err := s.core.DB.ListTemplates()
	if err != nil {
		return nil, err
	}
	rcfg, err := s.core.DB.GetRoutingConfig()
	if err != nil {
		rcfg = store.DefaultRoutingConfig()
	}
	return &AgentWorkspaceSnapshot{
		Agents:        agents,
		Providers:     providers,
		Models:        buildModelCatalog(providers),
		Profiles:      profiles,
		Rules:         rules,
		Templates:     templates,
		Stages:        store.AllStageDescriptors(),
		RoutingConfig: rcfg,
	}, nil
}

func (s *AgentService) ListWorkspaceProfiles() ([]store.WorkspaceProfile, error) {
	return s.core.DB.ListWorkspaceProfiles()
}

func (s *AgentService) ApplyWorkspaceProfile(profileID string) (*ApplyProfileResult, error) {
	wp, err := s.core.DB.GetWorkspaceProfile(profileID)
	if err != nil {
		return nil, err
	}

	var agentSnap map[string]store.AgentBindingSnapshot
	if err := json.Unmarshal([]byte(wp.AgentBindingsJSON), &agentSnap); err == nil {
		for agID, snap := range agentSnap {
			ag, err := s.core.DB.GetAgentProfile(agID)
			if err != nil {
				continue
			}
			ag.Enabled = snap.Enabled
			if wp.IsBuiltin && agent.IsNativeFirstKind(ag.Kind) {
				hasSnapKey := snap.ProviderID != "" && s.core.Secrets.Has(secrets.ProviderSecretKey(snap.ProviderID))
				hasCurKey := ag.ProviderID != "" && s.core.Secrets.Has(secrets.ProviderSecretKey(ag.ProviderID))
				if hasSnapKey {
					ag.ProviderID = snap.ProviderID
					ag.Model = snap.Model
				} else if !hasCurKey {
					ag.ProviderID = ""
					ag.Model = ""
				}
			} else if snap.ProviderID != "" || snap.Model != "" {
				ag.ProviderID = snap.ProviderID
				ag.Model = snap.Model
			}
			if snap.TiersJSON != "" {
				ag.TiersJSON = snap.TiersJSON
			}
			_ = s.core.DB.UpsertAgentProfile(ag)
		}
	}

	rcfg, err := s.core.DB.GetRoutingConfig()
	if err != nil {
		rcfg = store.DefaultRoutingConfig()
	}
	if wp.RoutingStrategy != "" {
		rcfg.Strategy = wp.RoutingStrategy
	}
	var stageSnap map[string]store.StageRouteBinding
	if err := json.Unmarshal([]byte(wp.StageRoutingJSON), &stageSnap); err == nil && len(stageSnap) > 0 {
		nextBindings := make(map[string]store.StageRouteBinding)
		for _, st := range store.AllStageDescriptors() {
			if b, ok := stageSnap[st.Key]; ok {
				nextBindings[st.Key] = b
			} else {
				nextBindings[st.Key] = store.StageRouteBinding{}
			}
		}
		rcfg.StageBindings = nextBindings
	}
	savedRCfg, err := s.core.DB.SaveRoutingConfig(rcfg)
	if err != nil {
		return nil, err
	}
	if err := s.core.DB.SetActiveWorkspaceProfile(wp.ID); err != nil {
		return nil, err
	}
	wp.IsActive = true
	agents, _ := s.core.DB.ListAgentProfiles()
	return &ApplyProfileResult{
		Profile:       wp,
		Agents:        agents,
		RoutingConfig: savedRCfg,
	}, nil
}

func (s *AgentService) SaveCurrentAsProfile(name, description, accentColor string) (*store.WorkspaceProfile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("Profile 名称不能为空")
	}
	if accentColor == "" {
		accentColor = "#0F6CBD"
	}
	agents, err := s.core.DB.ListAgentProfiles()
	if err != nil {
		return nil, err
	}
	snap := make(map[string]store.AgentBindingSnapshot, len(agents))
	for _, a := range agents {
		snap[a.ID] = store.AgentBindingSnapshot{
			Enabled:    a.Enabled,
			ProviderID: a.ProviderID,
			Model:      a.Model,
			TiersJSON:  a.TiersJSON,
		}
	}
	rcfg, err := s.core.DB.GetRoutingConfig()
	if err != nil {
		rcfg = store.DefaultRoutingConfig()
	}

	agentBytes, _ := json.Marshal(snap)
	stageBytes, _ := json.Marshal(rcfg.StageBindings)
	wp := store.WorkspaceProfile{
		ID:                "prof-" + uuid.NewString()[:8],
		Name:              name,
		Description:       strings.TrimSpace(description),
		AccentColor:       accentColor,
		RoutingStrategy:   rcfg.Strategy,
		AgentBindingsJSON: string(agentBytes),
		StageRoutingJSON:  string(stageBytes),
		IsActive:          true,
		IsBuiltin:         false,
		CreatedAt:         store.NowRFC3339(),
	}
	if err := s.core.DB.UpsertWorkspaceProfile(wp); err != nil {
		return nil, err
	}
	if err := s.core.DB.SetActiveWorkspaceProfile(wp.ID); err != nil {
		return nil, err
	}
	return &wp, nil
}

func (s *AgentService) DeleteWorkspaceProfile(profileID string) error {
	return s.core.DB.DeleteWorkspaceProfile(profileID)
}

func (s *AgentService) GetRoutingConfig() (store.RoutingConfig, error) {
	return s.core.DB.GetRoutingConfig()
}

func (s *AgentService) SaveRoutingConfig(cfg store.RoutingConfig) (store.RoutingConfig, error) {
	return s.core.DB.SaveRoutingConfig(cfg)
}

func (s *AgentService) SimulateRoute(req SimulateRouteRequest) (*router.RoutingDecision, error) {
	var repo store.Repository
	if req.RepoID != "" && req.RepoID != "all" {
		if r, err := s.core.DB.GetRepository(req.RepoID); err == nil {
			repo = r
		}
	}
	if repo.ID == "" {
		repos, _ := s.core.DB.ListRepositories()
		if len(repos) > 0 {
			repo = repos[0]
		} else {
			repo = store.Repository{ID: "preview", FullName: "demo/open-source", DefaultTemplateID: "auto"}
		}
	}
	labelsBytes, _ := json.Marshal(req.Labels)
	issue := store.Issue{
		ID:         "sim-issue",
		RepoID:     repo.ID,
		Number:     999,
		Title:      req.Title,
		Body:       req.Body,
		LabelsJSON: string(labelsBytes),
	}
	rules, _ := s.core.DB.ListRoutingRules()
	profiles, _ := s.core.DB.ListAgentProfiles()
	templates, _ := s.core.DB.ListTemplates()
	providers, _ := s.core.listProvidersWithKeyState()
	routingCfg, _ := s.core.DB.GetRoutingConfig()

	return s.core.Router.Route(s.core.BaseCtx, router.RouteRequest{
		Repo:          repo,
		Issue:         issue,
		Rules:         rules,
		Profiles:      profiles,
		Templates:     templates,
		Providers:     providers,
		RoutingConfig: routingCfg,
		Settings:      s.core.GetSettingsSnapshot(),
	})
}

// TestAgent performs a lightweight read-only smoke test against a single Agent CLI to verify
// local discovery, authentication state, and headless execution readiness.
func (s *AgentService) TestAgent(agentID string) (*TestAgentResult, error) {
	prof, err := s.core.Registry.DetectOne(s.core.BaseCtx, agentID)
	if err != nil {
		return nil, err
	}
	if !prof.Detected {
		return &TestAgentResult{
			AgentID:  agentID,
			OK:       false,
			Detected: false,
			Error:    "本机未检测到可用 CLI",
			Hint:     agent.InstallHint(prof.Kind),
		}, nil
	}

	adapter, profile, resolvedModel, providerEnv, err := s.core.Registry.ResolveInvocation(agentID, "")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(s.core.BaseCtx, 45*time.Second)
	defer cancel()

	start := time.Now()
	res, runErr := adapter.Run(ctx, agent.RunRequest{
		WorkDir:        os.TempDir(),
		Prompt:         "Reply with exactly: OK",
		Model:          resolvedModel,
		PermissionMode: profile.PermissionMode,
		Env:            providerEnv,
		Timeout:        45 * time.Second,
		ReadOnly:       true,
	}, nil)

	dur := time.Since(start).Milliseconds()
	var output string
	if res != nil {
		if res.DurationMs > 0 {
			dur = res.DurationMs
		}
		output = trimSnippet(res.Output, 400)
	}

	if runErr != nil {
		combinedLower := strings.ToLower(runErr.Error() + "\n" + output)
		hint := ""
		for _, kw := range []string{"not logged in", "authentication required", "/login", "unauthorized", "401", "api key", "api_key", "sign in", "authenticate"} {
			if strings.Contains(combinedLower, kw) {
				hint = agent.LoginHint(profile.Kind)
				break
			}
		}
		return &TestAgentResult{
			AgentID:    agentID,
			OK:         false,
			Detected:   true,
			BinPath:    prof.DetectedPath,
			Version:    prof.DetectedVersion,
			DurationMs: dur,
			Output:     output,
			Error:      runErr.Error(),
			Hint:       hint,
		}, nil
	}

	return &TestAgentResult{
		AgentID:    agentID,
		OK:         true,
		Detected:   true,
		BinPath:    prof.DetectedPath,
		Version:    prof.DetectedVersion,
		DurationMs: dur,
		Output:     output,
	}, nil
}

func trimSnippet(s string, maxChars int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return strings.TrimSpace(string(runes[:maxChars])) + "..."
}

// --- ReleaseService ---

type ReleaseOverview struct {
	Preview      *release.ReleasePreview `json:"preview"`
	Records      []store.ReleaseRecord   `json:"records"`
	PendingItems []store.ReleaseItem     `json:"pendingItems"`
}

type ReleaseService struct{ core *AppCore }

func NewReleaseService(core *AppCore) *ReleaseService { return &ReleaseService{core: core} }

func (s *ReleaseService) GetReleaseOverview(repoID string) (*ReleaseOverview, error) {
	var preview *release.ReleasePreview
	if repoID != "" && repoID != "all" {
		if p, err := s.core.ReleaseMgr.PreviewRelease(s.core.BaseCtx, repoID); err == nil {
			preview = p
		}
	}
	records, err := s.core.DB.ListReleaseRecords(repoID, 40)
	if err != nil {
		return nil, err
	}
	items, err := s.core.DB.ListPendingReleaseItems(repoID)
	if err != nil {
		return nil, err
	}
	return &ReleaseOverview{
		Preview:      preview,
		Records:      records,
		PendingItems: items,
	}, nil
}

func (s *ReleaseService) PreviewRelease(repoID string) (*release.ReleasePreview, error) {
	return s.core.ReleaseMgr.PreviewRelease(s.core.BaseCtx, repoID)
}

func (s *ReleaseService) PublishRelease(req release.PublishRequest) (*store.ReleaseRecord, error) {
	return s.core.ReleaseMgr.PublishRelease(s.core.BaseCtx, req)
}

func (s *ReleaseService) RefreshReleaseActions(releaseID string) (*store.ReleaseRecord, error) {
	return s.core.ReleaseMgr.RefreshReleaseActions(s.core.BaseCtx, releaseID)
}

// --- SettingsService ---

type AuthStatusDetail struct {
	Identity  hosting.AuthIdentity `json:"identity"`
	HasPAT    bool                 `json:"hasPat"`
	HasLLMKey bool                 `json:"hasLlmKey"`
	Paths     config.Paths         `json:"paths"`
}

type SettingsService struct{ core *AppCore }

func NewSettingsService(core *AppCore) *SettingsService { return &SettingsService{core: core} }

func (s *SettingsService) GetSettings() (config.AppSettings, error) {
	return s.core.DB.GetAppSettings()
}

func (s *SettingsService) SaveSettings(settings config.AppSettings) (config.AppSettings, error) {
	saved, err := s.core.DB.SaveAppSettings(settings)
	if err != nil {
		return saved, err
	}
	s.core.SetSettingsSnapshot(saved)
	return saved, nil
}

func (s *SettingsService) GetAuthStatus() (*AuthStatusDetail, error) {
	var id *hosting.AuthIdentity
	if prov := s.core.activeProvider(); prov != nil {
		verified, err := prov.VerifyAuth(s.core.BaseCtx)
		if err == nil && verified != nil {
			id = verified
		}
	}
	if id == nil {
		id = &hosting.AuthIdentity{
			Authenticated: false,
			Method:        s.core.GetSettingsSnapshot().AuthMethod,
			Error:         "未连接到 GitHub",
		}
	}
	return &AuthStatusDetail{
		Identity:  *id,
		HasPAT:    s.core.Secrets.Has(secrets.KeyGitHubToken),
		HasLLMKey: s.core.Secrets.Has(secrets.KeyLLMAPIKey),
		Paths:     s.core.Paths,
	}, nil
}

func (s *SettingsService) SaveGitHubPAT(token string) (*hosting.AuthIdentity, error) {
	if err := s.core.AuthMgr.SaveToken(token); err != nil {
		return nil, err
	}
	cur, _ := s.core.DB.GetAppSettings()
	cur.AuthMethod = "pat"
	saved, err := s.core.DB.SaveAppSettings(cur)
	if err == nil {
		s.core.SetSettingsSnapshot(saved)
	}
	if prov := s.core.activeProvider(); prov != nil {
		return prov.VerifyAuth(s.core.BaseCtx)
	}
	return &hosting.AuthIdentity{Authenticated: true, Method: "pat"}, nil
}

func (s *SettingsService) UseGHCLIAuth() (*hosting.AuthIdentity, error) {
	cur, _ := s.core.DB.GetAppSettings()
	cur.AuthMethod = "gh_cli"
	saved, err := s.core.DB.SaveAppSettings(cur)
	if err == nil {
		s.core.SetSettingsSnapshot(saved)
	}
	if prov := s.core.activeProvider(); prov != nil {
		return prov.VerifyAuth(s.core.BaseCtx)
	}
	return &hosting.AuthIdentity{Authenticated: false, Method: "gh_cli"}, nil
}

func (s *SettingsService) StartGitHubDeviceFlow(clientID string) (*github.DeviceFlowStart, error) {
	flow, err := s.core.AuthMgr.StartDeviceFlow(s.core.BaseCtx, clientID)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(clientID)
	if trimmed != "" {
		cur, _ := s.core.DB.GetAppSettings()
		if cur.OAuthClientID != trimmed {
			cur.OAuthClientID = trimmed
			if saved, saveErr := s.core.DB.SaveAppSettings(cur); saveErr == nil {
				s.core.SetSettingsSnapshot(saved)
			}
		}
	}
	return flow, nil
}

func (s *SettingsService) DisconnectGitHub() (*AuthStatusDetail, error) {
	_ = s.core.AuthMgr.ClearToken()
	cur, _ := s.core.DB.GetAppSettings()
	cur.AuthMethod = "none"
	if saved, err := s.core.DB.SaveAppSettings(cur); err == nil {
		s.core.SetSettingsSnapshot(saved)
	}
	return s.GetAuthStatus()
}

func (s *SettingsService) PollGitHubDeviceFlow(clientID, deviceCode string) (*github.DeviceFlowPollResult, error) {
	res, err := s.core.AuthMgr.PollDeviceFlow(s.core.BaseCtx, clientID, deviceCode)
	if err != nil {
		return nil, err
	}
	if res.Status == "completed" {
		cur, _ := s.core.DB.GetAppSettings()
		cur.AuthMethod = "oauth_device"
		if saved, err := s.core.DB.SaveAppSettings(cur); err == nil {
			s.core.SetSettingsSnapshot(saved)
		}
	}
	return res, nil
}

func (s *SettingsService) SaveLLMAPIKey(apiKey string) error {
	return s.core.Secrets.Set(secrets.KeyLLMAPIKey, strings.TrimSpace(apiKey))
}

func (s *SettingsService) ClearLLMAPIKey() error {
	return s.core.Secrets.Delete(secrets.KeyLLMAPIKey)
}

func (s *SettingsService) PickDirectory(title, initialDir string) (string, error) {
	if s.core.App == nil {
		return "", errors.New("当前运行环境不支持原生文件夹选择框")
	}
	if strings.TrimSpace(title) == "" {
		title = "选择工作区目录"
	}
	dlg := s.core.App.Dialog.OpenFile().
		SetTitle(title).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(true)
	if s.core.MainWindow != nil {
		dlg.AttachToWindow(s.core.MainWindow)
	}
	if startDir := resolveExistingDir(initialDir); startDir != "" {
		dlg.SetDirectory(startDir)
	}
	selected, err := dlg.PromptForSingleSelection()
	if err != nil {
		lower := strings.ToLower(err.Error())
		if selected == "" || strings.Contains(lower, "cancel") {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(selected), nil
}

func (s *SettingsService) OpenDirectory(dirPath string) error {
	clean, err := gitops.NormalizeLocalPath(dirPath)
	if err != nil {
		return err
	}
	if clean == "" {
		return errors.New("目录路径为空")
	}
	info, err := os.Stat(clean)
	if err != nil {
		return fmt.Errorf("目录不存在或无法访问: %w", err)
	}
	if !info.IsDir() {
		return errors.New("目标路径不是文件夹")
	}
	if s.core.App == nil {
		return errors.New("当前运行环境不支持打开系统目录")
	}
	return s.core.App.Browser.OpenFile(clean)
}
