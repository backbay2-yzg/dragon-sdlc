package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"dragon/internal/gitops"
	"dragon/internal/secrets"
)

const DefaultOAuthURL = "https://github.com"

// DeviceFlowStart holds the response from GitHub's OAuth device code endpoint.
type DeviceFlowStart struct {
	DeviceCode      string `json:"deviceCode"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	ExpiresIn       int    `json:"expiresIn"`
	Interval        int    `json:"interval"`
}

// DeviceFlowPollResult describes the result of polling GitHub's OAuth token endpoint.
type DeviceFlowPollResult struct {
	Status string `json:"status"` // pending | slow_down | completed | expired | error
	Token  string `json:"-"`
	Error  string `json:"error,omitempty"`
}

// AuthManager resolves and stores GitHub credentials using gh CLI, PAT, or OAuth Device Flow.
type AuthManager struct {
	secrets    secrets.Store
	httpClient *http.Client
	oauthBase  string
}

// NewAuthManager constructs a GitHub AuthManager backed by secrets.Store.
func NewAuthManager(sec secrets.Store) *AuthManager {
	return &AuthManager{
		secrets:    sec,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		oauthBase:  DefaultOAuthURL,
	}
}

// DetectGHCLIToken attempts to read the active token from the local `gh` CLI without popping up a console window.
func DetectGHCLIToken(ctx context.Context) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("未检测到 gh CLI: %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cctx, "gh", "auth", "token")
	gitops.ConfigureHiddenCommand(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh auth token 执行失败（请确认已执行 gh auth login）: %w", err)
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", errors.New("gh CLI 未返回有效 Token")
	}
	return tok, nil
}

// ResolveToken resolves a token according to the configured auth method ("gh_cli" | "pat" | "oauth_device" | "none").
func (a *AuthManager) ResolveToken(ctx context.Context, method string) (string, string, error) {
	switch method {
	case "none":
		return "", "none", errors.New("未连接 GitHub")
	case "pat", "oauth_device":
		tok, err := a.secrets.Get(secrets.KeyGitHubToken)
		if err != nil {
			return "", method, fmt.Errorf("未在系统凭据库中找到 GitHub Token (%s)", method)
		}
		return tok, method, nil
	case "gh_cli", "":
		if tok, err := DetectGHCLIToken(ctx); err == nil {
			return tok, "gh_cli", nil
		}
		// Fallback to stored credential if gh CLI isn't logged in.
		if tok, err := a.secrets.Get(secrets.KeyGitHubToken); err == nil && tok != "" {
			return tok, "pat", nil
		}
		return "", "gh_cli", errors.New("未检测到已登录的 gh CLI，也未配置 GitHub PAT")
	default:
		return "", method, fmt.Errorf("不支持的认证方式: %s", method)
	}
}

// SaveToken stores a GitHub token securely in Windows Credential Manager (or fallback store).
func (a *AuthManager) SaveToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("Token 不能为空")
	}
	return a.secrets.Set(secrets.KeyGitHubToken, token)
}

// ClearToken deletes any saved GitHub token.
func (a *AuthManager) ClearToken() error {
	return a.secrets.Delete(secrets.KeyGitHubToken)
}

// StartDeviceFlow initiates GitHub OAuth Device Flow for the supplied OAuth App Client ID.
func (a *AuthManager) StartDeviceFlow(ctx context.Context, clientID string) (*DeviceFlowStart, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, errors.New("请提供 GitHub OAuth App Client ID")
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", "repo read:user workflow")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.oauthBase, "/")+"/login/device/code", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub Device Flow 启动失败 (%d): %s", resp.StatusCode, string(body))
	}

	var payload struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
		ErrorDesc       string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.Error != "" {
		return nil, fmt.Errorf("%s: %s", payload.Error, payload.ErrorDesc)
	}
	if payload.Interval <= 0 {
		payload.Interval = 5
	}
	return &DeviceFlowStart{
		DeviceCode:      payload.DeviceCode,
		UserCode:        payload.UserCode,
		VerificationURI: payload.VerificationURI,
		ExpiresIn:       payload.ExpiresIn,
		Interval:        payload.Interval,
	}, nil
}

// PollDeviceFlow polls once for the OAuth Device Flow access token and saves it on success.
func (a *AuthManager) PollDeviceFlow(ctx context.Context, clientID, deviceCode string) (*DeviceFlowPollResult, error) {
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(clientID))
	form.Set("device_code", strings.TrimSpace(deviceCode))
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.oauthBase, "/")+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.AccessToken != "" {
		if err := a.SaveToken(payload.AccessToken); err != nil {
			return nil, err
		}
		return &DeviceFlowPollResult{Status: "completed", Token: payload.AccessToken}, nil
	}
	switch payload.Error {
	case "authorization_pending":
		return &DeviceFlowPollResult{Status: "pending"}, nil
	case "slow_down":
		return &DeviceFlowPollResult{Status: "slow_down"}, nil
	case "expired_token":
		return &DeviceFlowPollResult{Status: "expired", Error: "授权码已过期，请重新发起"}, nil
	default:
		return &DeviceFlowPollResult{Status: "error", Error: fmt.Sprintf("%s: %s", payload.Error, payload.ErrorDesc)}, nil
	}
}
