package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemVerAndChangelog(t *testing.T) {
	items := []ConventionalChange{
		ParseConventionalTitle(12, "feat(router): add AI issue classifier"),
		ParseConventionalTitle(13, "fix: handle 304 ETag response"),
		ParseConventionalTitle(14, "docs: update README"),
	}
	if items[0].Type != "feat" || items[0].Scope != "router" || items[0].Breaking {
		t.Fatalf("unexpected parse result: %+v", items[0])
	}

	latest := LatestSemVerTag([]string{"v0.9.0", "v1.2.3", "v1.10.0", "non-semver"})
	if latest != "v1.10.0" {
		t.Fatalf("expected v1.10.0, got %q", latest)
	}

	nextTag, ver, bump := NextVersion("v1.10.0", items)
	if nextTag != "v1.11.0" || ver != "1.11.0" || bump != "minor" {
		t.Fatalf("unexpected next version: %s %s %s", nextTag, ver, bump)
	}

	breakingItems := append(items, ParseConventionalTitle(15, "feat!: drop v1 config"))
	nextTag, _, bump = NextVersion("v1.10.0", breakingItems)
	if nextTag != "v2.0.0" || bump != "major" {
		t.Fatalf("expected v2.0.0 major, got %s %s", nextTag, bump)
	}

	cl := GenerateChangelog("v1.11.0", "acme/dragon", items)
	if !strings.Contains(cl, "### Features") || !strings.Contains(cl, "### Bug Fixes") || !strings.Contains(cl, "#12") {
		t.Fatalf("unexpected changelog:\n%s", cl)
	}
}

func TestWorktreeLifecycleAndDiff(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	// Create a local upstream repo with one initial commit on main.
	upstream := filepath.Join(tmp, "upstream")
	if err := os.MkdirAll(upstream, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, upstream, nil, "init", "-b", "main"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(upstream, "README.md"), []byte("# Dragon\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, upstream, nil, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, upstream, nil, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init"); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(filepath.Join(tmp, "repos"), filepath.Join(tmp, "worktrees"))
	if _, err := mgr.EnsureMirror(ctx, "acme/dragon", upstream, ""); err != nil {
		t.Fatalf("EnsureMirror failed: %v", err)
	}

	wt := mgr.WorktreePath("run-1")
	if err := mgr.CreateWorktree(ctx, "acme/dragon", "main", "auto/issue-1", wt); err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Modify tracked file and add an untracked file inside the isolated worktree.
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("# Dragon\nUpdated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "hello.txt"), []byte("new file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := mgr.HasChanges(ctx, wt, "main")
	if err != nil || !changed {
		t.Fatalf("expected HasChanges=true, got %v err=%v", changed, err)
	}

	diff, err := mgr.GetDiff(ctx, wt, "main")
	if err != nil {
		t.Fatalf("GetDiff failed: %v", err)
	}
	if !strings.Contains(diff, "+Updated") || !strings.Contains(diff, "+new file") {
		t.Fatalf("expected both modified and untracked diffs, got:\n%s", diff)
	}

	if err := mgr.RemoveWorktree(ctx, "acme/dragon", wt); err != nil {
		t.Fatalf("RemoveWorktree failed: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("expected worktree dir removed, got err=%v", err)
	}
}

func TestLocalWorkspaceValidationAndSiblingWorktrees(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	mgr := NewManager(filepath.Join(tmp, "repos"), filepath.Join(tmp, "worktrees"))

	// ParseGitHubOwnerRepo coverage.
	cases := []struct {
		url  string
		want string
		ok   bool
	}{
		{"https://github.com/Acme/Dragon.git", "Acme/Dragon", true},
		{"https://github.com/acme/dragon/", "acme/dragon", true},
		{"git@github.com:acme/dragon.git", "acme/dragon", true},
		{"ssh://git@github.com/acme/dragon", "acme/dragon", true},
		{"https://gitlab.com/acme/dragon.git", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseGitHubOwnerRepo(tc.url)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("ParseGitHubOwnerRepo(%q) = (%q, %v), want (%q, %v)", tc.url, got, ok, tc.want, tc.ok)
		}
	}

	// NormalizeLocalPath rejects relative paths and root dirs.
	if _, err := NormalizeLocalPath("relative/path"); err == nil {
		t.Fatalf("expected relative path error")
	}
	rootPath := filepath.VolumeName(tmp) + string(filepath.Separator)
	if _, err := NormalizeLocalPath(rootPath); err == nil {
		t.Fatalf("expected root path %q error", rootPath)
	}

	// 1. unconfigured
	res := mgr.ValidateLocalWorkspace(ctx, "", "acme/dragon")
	if res.Status != WorkspaceStatusUnconfigured || res.OK {
		t.Fatalf("expected unconfigured, got %+v", res)
	}

	// 2. pending_clone (non-existent and empty dir)
	nonExistent := filepath.Join(tmp, "workspaces", "dragon")
	res = mgr.ValidateLocalWorkspace(ctx, nonExistent, "acme/dragon")
	if res.Status != WorkspaceStatusPendingClone || !res.OK {
		t.Fatalf("expected pending_clone for non-existent dir, got %+v", res)
	}
	emptyDir := filepath.Join(tmp, "empty-ws")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	res = mgr.ValidateLocalWorkspace(ctx, emptyDir, "acme/dragon")
	if res.Status != WorkspaceStatusPendingClone || !res.OK {
		t.Fatalf("expected pending_clone for empty dir, got %+v", res)
	}

	// 3. invalid: non-empty non-git dir
	nonGitDir := filepath.Join(tmp, "non-git")
	if err := os.MkdirAll(nonGitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonGitDir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = mgr.ValidateLocalWorkspace(ctx, nonGitDir, "acme/dragon")
	if res.Status != WorkspaceStatusInvalid || res.OK {
		t.Fatalf("expected invalid for non-empty non-git dir, got %+v", res)
	}

	// 4. invalid: mismatched GitHub remote
	mismatchDir := filepath.Join(tmp, "mismatch")
	if err := os.MkdirAll(mismatchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, mismatchDir, nil, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, mismatchDir, nil, "remote", "add", "origin", "git@github.com:other/repo.git"); err != nil {
		t.Fatal(err)
	}
	res = mgr.ValidateLocalWorkspace(ctx, mismatchDir, "acme/dragon")
	if res.Status != WorkspaceStatusInvalid || res.OK || res.RemoteRepo != "other/repo" {
		t.Fatalf("expected invalid remote mismatch, got %+v", res)
	}

	// 5. ready: matching GitHub remote (case-insensitive)
	if _, err := runGit(ctx, mismatchDir, nil, "remote", "set-url", "origin", "https://github.com/ACME/Dragon.git"); err != nil {
		t.Fatal(err)
	}
	res = mgr.ValidateLocalWorkspace(ctx, mismatchDir, "acme/dragon")
	if res.Status != WorkspaceStatusReady || !res.OK {
		t.Fatalf("expected ready for matching GitHub remote, got %+v", res)
	}

	// 6. End-to-end local upstream -> EnsureLocalRepo clone & fetch -> sibling worktree lifecycle.
	upstreamBare := filepath.Join(tmp, "upstream.git")
	seed := filepath.Join(tmp, "seed")
	if _, err := runGit(ctx, tmp, nil, "init", "--bare", upstreamBare); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, seed, nil, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, seed, nil, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, seed, nil, "-c", "user.name=T", "-c", "user.email=t@e.com", "commit", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, seed, nil, "remote", "add", "origin", upstreamBare); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, seed, nil, "push", "-u", "origin", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, upstreamBare, nil, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		t.Fatal(err)
	}

	localWS := filepath.Join(tmp, "workspaces", "dragon")
	if err := mgr.EnsureLocalRepo(ctx, localWS, "acme/dragon", upstreamBare, ""); err != nil {
		t.Fatalf("EnsureLocalRepo(clone) failed: %v", err)
	}
	// User switches local workspace to a feature branch and leaves an uncommitted file.
	if _, err := runGit(ctx, localWS, nil, "checkout", "-b", "user-wip"); err != nil {
		t.Fatal(err)
	}
	uncommitted := filepath.Join(localWS, "wip.txt")
	if err := os.WriteFile(uncommitted, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// EnsureLocalRepo(fetch) must not disturb user-wip branch or uncommitted file.
	if err := mgr.EnsureLocalRepo(ctx, localWS, "acme/dragon", upstreamBare, ""); err != nil {
		t.Fatalf("EnsureLocalRepo(fetch) failed: %v", err)
	}
	wtPath := mgr.WorktreePathForRun(localWS, 12, "abcdef123456")
	expectedSuffix := filepath.Join(".dragon-worktrees", "dragon", "issue-12-abcdef")
	if !strings.HasSuffix(wtPath, expectedSuffix) {
		t.Fatalf("unexpected sibling worktree path %q (want suffix %q)", wtPath, expectedSuffix)
	}
	if err := mgr.CreateWorktreeFromLocal(ctx, localWS, "main", "auto/issue-12-abcdef", wtPath); err != nil {
		t.Fatalf("CreateWorktreeFromLocal failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "README.md")); err != nil {
		t.Fatalf("expected README.md in sibling worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "wip.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected uncommitted wip.txt NOT leaked into isolated worktree")
	}

	if err := mgr.RemoveWorktreeFromLocal(ctx, localWS, wtPath); err != nil {
		t.Fatalf("RemoveWorktreeFromLocal failed: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree removed, got err=%v", err)
	}

	// Create another worktree and test RemoveAllRepoWorktrees preserves localWS.
	wtPath2 := mgr.WorktreePathForRun(localWS, 13, "11223344")
	if err := mgr.CreateWorktreeFromLocal(ctx, localWS, "main", "auto/issue-13-112233", wtPath2); err != nil {
		t.Fatalf("CreateWorktreeFromLocal(2) failed: %v", err)
	}
	if err := mgr.RemoveAllRepoWorktrees(ctx, localWS); err != nil {
		t.Fatalf("RemoveAllRepoWorktrees failed: %v", err)
	}
	if _, err := os.Stat(wtPath2); !os.IsNotExist(err) {
		t.Fatalf("expected wtPath2 removed, got err=%v", err)
	}
	if _, err := os.Stat(uncommitted); err != nil {
		t.Fatalf("expected user local workspace and wip.txt preserved, got err=%v", err)
	}
}
