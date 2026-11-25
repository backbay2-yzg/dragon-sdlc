package hosting

import "context"

// AuthIdentity describes the currently authenticated platform user.
type AuthIdentity struct {
	Authenticated bool   `json:"authenticated"`
	Method        string `json:"method"` // gh_cli | pat | oauth_device | none
	Username      string `json:"username"`
	AvatarURL     string `json:"avatarUrl"`
	Scopes        string `json:"scopes"`
	Error         string `json:"error,omitempty"`
}

// RemoteRepo holds basic repository metadata retrieved from the hosting platform.
type RemoteRepo struct {
	Owner           string `json:"owner"`
	Name            string `json:"name"`
	FullName        string `json:"fullName"`
	Description     string `json:"description"`
	DefaultBranch   string `json:"defaultBranch"`
	CloneURL        string `json:"cloneUrl"`
	Private         bool   `json:"private"`
	Archived        bool   `json:"archived"`
	Fork            bool   `json:"fork"`
	Language        string `json:"language"`
	OpenIssuesCount int    `json:"openIssuesCount"`
	PushedAt        string `json:"pushedAt"`
	HTMLURL         string `json:"htmlUrl"`
	OwnerType       string `json:"ownerType"` // User | Organization
	OwnerAvatarURL  string `json:"ownerAvatarUrl"`
	CanAdmin        bool   `json:"canAdmin"`
	CanPush         bool   `json:"canPush"`
	CanPull         bool   `json:"canPull"`
}

// RemoteIssue represents an issue returned from the hosting platform API.
type RemoteIssue struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	State     string   `json:"state"`
	Author    string   `json:"author"`
	Labels    []string `json:"labels"`
	HTMLURL   string   `json:"htmlUrl"`
	UpdatedAt string   `json:"updatedAt"`
}

// IssuePage represents a conditional fetch result for repository issues.
type IssuePage struct {
	NotModified        bool          `json:"notModified"`
	ETag               string        `json:"etag"`
	RateLimitRemaining int           `json:"rateLimitRemaining"`
	Issues             []RemoteIssue `json:"issues"`
}

// CreatePRRequest specifies parameters to open a Pull Request.
type CreatePRRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft"`
}

// RemotePR describes an opened Pull Request.
type RemotePR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"htmlUrl"`
	State   string `json:"state"`
	HeadSHA string `json:"headSha"`
}

// CheckRunDetail describes an individual CI check or workflow job.
type CheckRunDetail struct {
	Name       string `json:"name"`
	Status     string `json:"status"`     // queued | in_progress | completed
	Conclusion string `json:"conclusion"` // success | failure | neutral | cancelled | skipped | timed_out
	HTMLURL    string `json:"htmlUrl"`
	RunID      int64  `json:"runId"`
}

// CheckSummary aggregates all CI checks for a commit ref.
type CheckSummary struct {
	OverallState string           `json:"overallState"` // none | pending | success | failure
	Checks       []CheckRunDetail `json:"checks"`
	FailedRunIDs []int64          `json:"failedRunIds"`
}

// MergeResult describes the outcome of merging a Pull Request.
type MergeResult struct {
	Merged  bool   `json:"merged"`
	SHA     string `json:"sha"`
	Message string `json:"message"`
}

// CreateReleaseRequest specifies parameters to publish a release & tag.
type CreateReleaseRequest struct {
	TagName         string `json:"tagName"`
	TargetCommitish string `json:"targetCommitish"`
	Name            string `json:"name"`
	Body            string `json:"body"`
	Draft           bool   `json:"draft"`
	Prerelease      bool   `json:"prerelease"`
}

// RemoteRelease describes a created release on the hosting platform.
type RemoteRelease struct {
	ID          int64  `json:"id"`
	TagName     string `json:"tagName"`
	HTMLURL     string `json:"htmlUrl"`
	PublishedAt string `json:"publishedAt"`
}

// WorkflowRunStatus represents a GitHub Actions workflow run tied to a release tag.
type WorkflowRunStatus struct {
	Status     string `json:"status"` // none | queued | in_progress | completed_success | completed_failure
	HTMLURL    string `json:"htmlUrl"`
	WorkflowID int64  `json:"workflowId"`
}

// Provider abstracts code hosting platform APIs (GitHub first, extensible to GitLab/Gitee).
type Provider interface {
	Name() string
	VerifyAuth(ctx context.Context) (*AuthIdentity, error)
	ListUserRepos(ctx context.Context) ([]RemoteRepo, error)
	GetRepo(ctx context.Context, owner, name string) (*RemoteRepo, error)
	ListIssues(ctx context.Context, owner, name, etag string) (*IssuePage, error)
	AddIssueLabels(ctx context.Context, owner, name string, number int, labels []string) error
	CreateIssueComment(ctx context.Context, owner, name string, number int, body string) error
	CreatePullRequest(ctx context.Context, owner, name string, req CreatePRRequest) (*RemotePR, error)
	GetPRChecks(ctx context.Context, owner, name, ref string) (*CheckSummary, error)
	GetFailedActionLogs(ctx context.Context, owner, name string, runID int64) (string, error)
	MergePullRequest(ctx context.Context, owner, name string, number int, strategy, title, message string) (*MergeResult, error)
	ListTags(ctx context.Context, owner, name string) ([]string, error)
	CreateRelease(ctx context.Context, owner, name string, req CreateReleaseRequest) (*RemoteRelease, error)
	GetReleaseWorkflowStatus(ctx context.Context, owner, name, tag string) (*WorkflowRunStatus, error)
}
