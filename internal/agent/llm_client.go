package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"dragon/internal/config"
	"dragon/internal/secrets"
	"dragon/internal/store"
)

// ErrLLMNotConfigured indicates the optional lightweight LLM API is disabled or missing credentials.
var ErrLLMNotConfigured = errors.New("轻量辅助 LLM API 未启用或未配置 API Key")

// LLMClient calls an optional lightweight OpenAI-compatible or Anthropic endpoint for routing & summarization.
type LLMClient struct {
	httpClient *http.Client
	secrets    secrets.Store
}

// NewLLMClient initializes an LLMClient backed by secrets.Store.
func NewLLMClient(sec secrets.Store) *LLMClient {
	return &LLMClient{
		httpClient: &http.Client{Timeout: 25 * time.Second},
		secrets:    sec,
	}
}

// CompleteWithModelRef routes a completion call using a unified "providerId/modelId" ref if configured,
// falling back to legacy AppSettings LLM configuration otherwise.
func (c *LLMClient) CompleteWithModelRef(ctx context.Context, db *store.DB, settings config.AppSettings, modelRef, systemPrompt, userPrompt string) (string, error) {
	provID, modelID := SplitModelRef(modelRef)
	if provID != "" && db != nil && c.secrets != nil {
		if prov, err := db.GetModelProvider(provID); err == nil && prov.Enabled {
			if apiKey, err := c.secrets.Get(secrets.ProviderSecretKey(prov.ID)); err == nil && strings.TrimSpace(apiKey) != "" {
				s := settings
				s.LLMBaseURL = prov.BaseURL
				s.LLMModel = modelID
				if prov.Protocol == "anthropic" {
					return c.completeAnthropic(ctx, s, apiKey, systemPrompt, userPrompt)
				}
				return c.completeOpenAI(ctx, s, apiKey, systemPrompt, userPrompt)
			}
		}
	}
	return c.Complete(ctx, settings, systemPrompt, userPrompt)
}

// Complete sends a system + user prompt to the configured lightweight LLM provider.
func (c *LLMClient) Complete(ctx context.Context, settings config.AppSettings, systemPrompt, userPrompt string) (string, error) {
	if settings.LLMProvider == "" || settings.LLMProvider == "none" {
		return "", ErrLLMNotConfigured
	}
	apiKey, err := c.secrets.Get(secrets.KeyLLMAPIKey)
	if err != nil || strings.TrimSpace(apiKey) == "" {
		return "", ErrLLMNotConfigured
	}

	switch settings.LLMProvider {
	case "openai_compatible":
		return c.completeOpenAI(ctx, settings, apiKey, systemPrompt, userPrompt)
	case "anthropic":
		return c.completeAnthropic(ctx, settings, apiKey, systemPrompt, userPrompt)
	default:
		return "", fmt.Errorf("不支持的 LLM Provider: %s", settings.LLMProvider)
	}
}

func (c *LLMClient) completeOpenAI(ctx context.Context, settings config.AppSettings, apiKey, systemPrompt, userPrompt string) (string, error) {
	base := strings.TrimRight(settings.LLMBaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := settings.LLMModel
	if model == "" {
		model = "gpt-4o-mini"
	}

	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.1,
	}
	b, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("LLM API 请求失败 (%d): %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", errors.New("LLM API 返回空结果")
	}
	return out.Choices[0].Message.Content, nil
}

func (c *LLMClient) completeAnthropic(ctx context.Context, settings config.AppSettings, apiKey, systemPrompt, userPrompt string) (string, error) {
	base := strings.TrimRight(settings.LLMBaseURL, "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	model := settings.LLMModel
	if model == "" {
		model = "claude-3-5-haiku-latest"
	}

	payload := map[string]any{
		"model":      model,
		"max_tokens": 1024,
		"system":     systemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": userPrompt},
		},
	}
	b, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("Anthropic API 请求失败 (%d): %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	for _, block := range out.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}
	return "", errors.New("Anthropic API 返回空内容")
}
