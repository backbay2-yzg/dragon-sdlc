package github

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"dragon/internal/hosting"
)

const DefaultAPIBaseURL = "https://api.github.com"

// TokenSource supplies the active GitHub token and authentication method name.
type TokenSource func(ctx context.Context) (token string, method string, err error)

// Client implements hosting.Provider against the GitHub REST API v3.
type Client struct {
	baseURL     string
	httpClient  *http.Client
	tokenSource TokenSource
}

// NewClient constructs a GitHub API client.
func NewClient(baseURL string, tokenSource TokenSource) *Client {
	if baseURL == "" {
		baseURL = DefaultAPIBaseURL
	}
	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		tokenSource: tokenSource,
	}
}

func (c *Client) Name() string { return "github" }

func (c *Client) doRequest(ctx context.Context, method, path string, body any, headers map[string]string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Dragon-SDLC/0.1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.tokenSource != nil {
		tok, _, err := c.tokenSource(ctx)
		if err == nil && tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	return c.httpClient.Do(req)
}

func readErrorBody(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return strings.TrimSpace(string(b))
}

func (c *Client) VerifyAuth(ctx context.Context) (*hosting.AuthIdentity, error) {
	var authMethod string
	if c.tokenSource != nil {
		tok, m, err := c.tokenSource(ctx)
		authMethod = m
		if err != nil || tok == "" {
			msg := "未登录 GitHub"
			if err != nil {
				msg = err.Error()
			}
			return &hosting.AuthIdentity{
				Authenticated: false,
				Method:        m,
				Error:         msg,
			}, nil
		}
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/user", nil, nil)
	if err != nil {
		return &hosting.AuthIdentity{
			Authenticated: false,
			Method:        authMethod,
			Error:         err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return &hosting.AuthIdentity{
			Authenticated: false,
			Method:        authMethod,
			Error:         fmt.Sprintf("HTTP %d: %s", resp.StatusCode, readErrorBody(resp)),
		}, nil
	}

	var u struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	return &hosting.AuthIdentity{
		Authenticated: true,
		Method:        authMethod,
		Username:      u.Login,
		AvatarURL:     u.AvatarURL,
		Scopes:        resp.Header.Get("X-OAuth-Scopes"),
	}, nil
}

type ghRepoPayload struct {
	Name            string `json:"name"`
	FullName        string `json:"full_name"`
	Description     string `json:"description"`
	DefaultBranch   string `json:"default_branch"`
	CloneURL        string `json:"clone_url"`
	Private         bool   `json:"private"`
	Archived        bool   `json:"archived"`
	Fork            bool   `json:"fork"`
	Language        string `json:"language"`
	OpenIssuesCount int    `json:"open_issues_count"`
	PushedAt        string `json:"pushed_at"`
	HTMLURL         string `json:"html_url"`
	Owner           struct {
		Login     string `json:"login"`
		Type      string `json:"type"`
		AvatarURL string `json:"avatar_url"`
	} `json:"owner"`
	Permissions struct {
		Admin bool `json:"admin"`
		Push  bool `json:"push"`
		Pull  bool `json:"pull"`
	} `json:"permissions"`
}

func (r ghRepoPayload) toRemoteRepo() hosting.RemoteRepo {
	return hosting.RemoteRepo{
		Owner:           r.Owner.Login,
		Name:            r.Name,
		FullName:        r.FullName,
		Description:     r.Description,
		DefaultBranch:   r.DefaultBranch,
		CloneURL:        r.CloneURL,
		Private:         r.Private,
		Archived:        r.Archived,
		Fork:            r.Fork,
		Language:        r.Language,
		OpenIssuesCount: r.OpenIssuesCount,
		PushedAt:        r.PushedAt,
		HTMLURL:         r.HTMLURL,
		OwnerType:       r.Owner.Type,
		OwnerAvatarURL:  r.Owner.AvatarURL,
		CanAdmin:        r.Permissions.Admin,
		CanPush:         r.Permissions.Push,
		CanPull:         r.Permissions.Pull,
	}
}

func (c *Client) ListUserRepos(ctx context.Context) ([]hosting.RemoteRepo, error) {
	if c.tokenSource != nil {
		tok, _, err := c.tokenSource(ctx)
		if err != nil || tok == "" {
			return nil, errors.New("请先完成 GitHub 授权")
		}
	}

	out := make([]hosting.RemoteRepo, 0)
	for page := 1; page <= 3; page++ {
		path := fmt.Sprintf("/user/repos?affiliation=owner,collaborator,organization_member&sort=pushed&direction=desc&per_page=100&page=%d", page)
		resp, err := c.doRequest(ctx, http.MethodGet, path, nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 300 {
			errMsg := readErrorBody(resp)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("获取 GitHub 仓库列表失败 (%d): %s", resp.StatusCode, errMsg)
		}

		var batch []ghRepoPayload
		err = json.NewDecoder(resp.Body).Decode(&batch)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, item := range batch {
			out = append(out, item.toRemoteRepo())
		}
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

func (c *Client) GetRepo(ctx context.Context, owner, name string) (*hosting.RemoteRepo, error) {
	path := fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(name))
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("获取仓库失败 (%d): %s", resp.StatusCode, readErrorBody(resp))
	}

	var r ghRepoPayload
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	remote := r.toRemoteRepo()
	return &remote, nil
}

func (c *Client) ListIssues(ctx context.Context, owner, name, etag string) (*hosting.IssuePage, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues?state=open&sort=updated&direction=desc&per_page=50",
		url.PathEscape(owner), url.PathEscape(name))
	headers := map[string]string{}
	if etag != "" {
		headers["If-None-Match"] = etag
	}
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rem, _ := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining"))
	newETag := resp.Header.Get("ETag")
	if newETag == "" {
		newETag = etag
	}

	if resp.StatusCode == http.StatusNotModified {
		return &hosting.IssuePage{
			NotModified:        true,
			ETag:               newETag,
			RateLimitRemaining: rem,
		}, nil
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("拉取 Issue 失败 (%d): %s", resp.StatusCode, readErrorBody(resp))
	}

	var raw []struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		State       string `json:"state"`
		HTMLURL     string `json:"html_url"`
		UpdatedAt   string `json:"updated_at"`
		PullRequest any    `json:"pull_request"`
		User        struct {
			Login string `json:"login"`
		} `json:"user"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	var issues []hosting.RemoteIssue
	for _, it := range raw {
		if it.PullRequest != nil {
			continue // GitHub's /issues endpoint includes PRs; skip them.
		}
		var labels []string
		for _, lb := range it.Labels {
			labels = append(labels, lb.Name)
		}
		issues = append(issues, hosting.RemoteIssue{
			Number:    it.Number,
			Title:     it.Title,
			Body:      it.Body,
			State:     it.State,
			Author:    it.User.Login,
			Labels:    labels,
			HTMLURL:   it.HTMLURL,
			UpdatedAt: it.UpdatedAt,
		})
	}

	return &hosting.IssuePage{
		NotModified:        false,
		ETag:               newETag,
		RateLimitRemaining: rem,
		Issues:             issues,
	}, nil
}

func (c *Client) AddIssueLabels(ctx context.Context, owner, name string, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", url.PathEscape(owner), url.PathEscape(name), number)
	resp, err := c.doRequest(ctx, http.MethodPost, path, map[string]any{"labels": labels}, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("添加 Label 失败 (%d): %s", resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

func (c *Client) CreateIssueComment(ctx context.Context, owner, name string, number int, body string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", url.PathEscape(owner), url.PathEscape(name), number)
	resp, err := c.doRequest(ctx, http.MethodPost, path, map[string]any{"body": body}, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("发表 Issue 评论失败 (%d): %s", resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

func (c *Client) CreatePullRequest(ctx context.Context, owner, name string, req hosting.CreatePRRequest) (*hosting.RemotePR, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls", url.PathEscape(owner), url.PathEscape(name))
	payload := map[string]any{
		"title": req.Title,
		"body":  req.Body,
		"head":  req.Head,
		"base":  req.Base,
		"draft": req.Draft,
	}
	resp, err := c.doRequest(ctx, http.MethodPost, path, payload, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("创建 PR 失败 (%d): %s", resp.StatusCode, readErrorBody(resp))
	}
	var pr struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		State   string `json:"state"`
		Head    struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	return &hosting.RemotePR{
		Number:  pr.Number,
		Title:   pr.Title,
		HTMLURL: pr.HTMLURL,
		State:   pr.State,
		HeadSHA: pr.Head.SHA,
	}, nil
}

func (c *Client) GetPRChecks(ctx context.Context, owner, name, ref string) (*hosting.CheckSummary, error) {
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs",
		url.PathEscape(owner), url.PathEscape(name), url.PathEscape(ref))
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("获取 Check Runs 失败 (%d): %s", resp.StatusCode, readErrorBody(resp))
	}

	var payload struct {
		TotalCount int `json:"total_count"`
		CheckRuns  []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
			CheckSuite struct {
				ID int64 `json:"id"`
			} `json:"check_suite"`
			DetailsURL string `json:"details_url"`
		} `json:"check_runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	if payload.TotalCount == 0 {
		return &hosting.CheckSummary{OverallState: "none"}, nil
	}

	summary := &hosting.CheckSummary{OverallState: "success"}
	seenRunIDs := map[int64]bool{}
	anyPending := false
	anyFailed := false

	for _, cr := range payload.CheckRuns {
		runID := extractActionsRunID(cr.DetailsURL, cr.HTMLURL)
		detail := hosting.CheckRunDetail{
			Name:       cr.Name,
			Status:     cr.Status,
			Conclusion: cr.Conclusion,
			HTMLURL:    cr.HTMLURL,
			RunID:      runID,
		}
		summary.Checks = append(summary.Checks, detail)

		if cr.Status != "completed" {
			anyPending = true
			continue
		}
		switch cr.Conclusion {
		case "success", "neutral", "skipped":
		default:
			anyFailed = true
			if runID > 0 && !seenRunIDs[runID] {
				seenRunIDs[runID] = true
				summary.FailedRunIDs = append(summary.FailedRunIDs, runID)
			}
		}
	}

	if anyFailed {
		summary.OverallState = "failure"
	} else if anyPending {
		summary.OverallState = "pending"
	}
	return summary, nil
}

func extractActionsRunID(detailsURL, htmlURL string) int64 {
	for _, u := range []string{detailsURL, htmlURL} {
		idx := strings.Index(u, "/actions/runs/")
		if idx == -1 {
			continue
		}
		rest := u[idx+len("/actions/runs/"):]
		parts := strings.SplitN(rest, "/", 2)
		if id, err := strconv.ParseInt(parts[0], 10, 64); err == nil && id > 0 {
			return id
		}
	}
	return 0
}

func (c *Client) GetFailedActionLogs(ctx context.Context, owner, name string, runID int64) (string, error) {
	if runID <= 0 {
		return "", errors.New("无效的 Actions Run ID")
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/logs", url.PathEscape(owner), url.PathEscape(name), runID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("下载 Actions 日志失败 (%d): %s", resp.StatusCode, readErrorBody(resp))
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // cap 8MB
	if err != nil {
		return "", err
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		// Some endpoints or tests may return plain text logs directly.
		return truncateTail(string(raw), 12000), nil
	}

	var buf strings.Builder
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(rc, 256*1024))
		_ = rc.Close()
		txt := string(data)
		if strings.Contains(txt, "##[error]") || strings.Contains(strings.ToLower(txt), "fail") || len(zr.File) <= 4 {
			buf.WriteString("=== " + f.Name + " ===\n")
			buf.WriteString(truncateTail(txt, 4000))
			buf.WriteString("\n")
		}
	}
	if buf.Len() == 0 {
		return "未能在日志压缩包中提取到明确错误信息。", nil
	}
	return truncateTail(buf.String(), 12000), nil
}

func truncateTail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "...(truncated)\n" + s[len(s)-max:]
}

func (c *Client) MergePullRequest(ctx context.Context, owner, name string, number int, strategy, title, message string) (*hosting.MergeResult, error) {
	if strategy == "" {
		strategy = "squash"
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", url.PathEscape(owner), url.PathEscape(name), number)
	payload := map[string]any{
		"merge_method": strategy,
	}
	if title != "" {
		payload["commit_title"] = title
	}
	if message != "" {
		payload["commit_message"] = message
	}
	resp, err := c.doRequest(ctx, http.MethodPut, path, payload, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("合并 PR 失败 (%d): %s", resp.StatusCode, readErrorBody(resp))
	}
	var res struct {
		Merged  bool   `json:"merged"`
		SHA     string `json:"sha"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &hosting.MergeResult{
		Merged:  res.Merged,
		SHA:     res.SHA,
		Message: res.Message,
	}, nil
}

func (c *Client) ListTags(ctx context.Context, owner, name string) ([]string, error) {
	path := fmt.Sprintf("/repos/%s/%s/tags?per_page=30", url.PathEscape(owner), url.PathEscape(name))
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("获取 Tags 失败 (%d): %s", resp.StatusCode, readErrorBody(resp))
	}
	var raw []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(raw))
	for _, t := range raw {
		tags = append(tags, t.Name)
	}
	return tags, nil
}

func (c *Client) CreateRelease(ctx context.Context, owner, name string, req hosting.CreateReleaseRequest) (*hosting.RemoteRelease, error) {
	path := fmt.Sprintf("/repos/%s/%s/releases", url.PathEscape(owner), url.PathEscape(name))
	payload := map[string]any{
		"tag_name":               req.TagName,
		"target_commitish":       req.TargetCommitish,
		"name":                   req.Name,
		"body":                   req.Body,
		"draft":                  req.Draft,
		"prerelease":             req.Prerelease,
		"generate_release_notes": false,
	}
	resp, err := c.doRequest(ctx, http.MethodPost, path, payload, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("创建 GitHub Release 失败 (%d): %s", resp.StatusCode, readErrorBody(resp))
	}
	var out struct {
		ID          int64  `json:"id"`
		TagName     string `json:"tag_name"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &hosting.RemoteRelease{
		ID:          out.ID,
		TagName:     out.TagName,
		HTMLURL:     out.HTMLURL,
		PublishedAt: out.PublishedAt,
	}, nil
}

func (c *Client) GetReleaseWorkflowStatus(ctx context.Context, owner, name, tag string) (*hosting.WorkflowRunStatus, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs?per_page=15", url.PathEscape(owner), url.PathEscape(name))
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("查询 Release Actions 状态失败 (%d): %s", resp.StatusCode, readErrorBody(resp))
	}
	var payload struct {
		WorkflowRuns []struct {
			ID         int64  `json:"id"`
			HeadBranch string `json:"head_branch"`
			Event      string `json:"event"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
		} `json:"workflow_runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	for _, wr := range payload.WorkflowRuns {
		if wr.HeadBranch == tag || (wr.Event == "release" && tag != "") {
			st := "in_progress"
			switch wr.Status {
			case "queued":
				st = "queued"
			case "completed":
				if wr.Conclusion == "success" {
					st = "completed_success"
				} else {
					st = "completed_failure"
				}
			}
			return &hosting.WorkflowRunStatus{
				Status:     st,
				HTMLURL:    wr.HTMLURL,
				WorkflowID: wr.ID,
			}, nil
		}
	}
	return &hosting.WorkflowRunStatus{Status: "none"}, nil
}
