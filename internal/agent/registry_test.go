package agent

import (
	"testing"

	"dragon/internal/secrets"
	"dragon/internal/store"
)

func TestBuildProviderEnvAndTiers(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sec := secrets.NewMemoryStore()
	if err := sec.Set(secrets.ProviderSecretKey("moonshot"), "sk-kimi-secret"); err != nil {
		t.Fatal(err)
	}

	claudeProf, err := db.GetAgentProfile("claude")
	if err != nil {
		t.Fatal(err)
	}
	claudeProf.ProviderID = "moonshot"
	claudeProf.Model = "kimi-k2.5"
	claudeProf.TiersJSON = `{"opus":"moonshot/kimi-k2-thinking","sonnet":"moonshot/kimi-k2.5","haiku":"moonshot/kimi-k2.5"}`

	model, env := BuildProviderEnv(db, sec, claudeProf, "opus")
	if model != "opus" {
		t.Fatalf("expected claude tier model 'opus', got %q", model)
	}
	if env["ANTHROPIC_BASE_URL"] != "https://api.moonshot.cn/anthropic" || env["ANTHROPIC_AUTH_TOKEN"] != "sk-kimi-secret" {
		t.Fatalf("unexpected claude provider env: %+v", env)
	}
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "kimi-k2-thinking" || env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "kimi-k2.5" {
		t.Fatalf("unexpected claude tier env vars: %+v", env)
	}

	// OpenAI-compatible CLI test.
	if err := sec.Set(secrets.ProviderSecretKey("deepseek"), "sk-ds-secret"); err != nil {
		t.Fatal(err)
	}
	codexProf, err := db.GetAgentProfile("codex")
	if err != nil {
		t.Fatal(err)
	}
	model2, env2 := BuildProviderEnv(db, sec, codexProf, "deepseek/deepseek-chat")
	if model2 != "deepseek-chat" {
		t.Fatalf("expected bare model 'deepseek-chat', got %q", model2)
	}
	if env2["OPENAI_BASE_URL"] != "https://api.deepseek.com/v1" || env2["OPENAI_API_KEY"] != "sk-ds-secret" {
		t.Fatalf("unexpected openai-compatible env: %+v", env2)
	}
}
