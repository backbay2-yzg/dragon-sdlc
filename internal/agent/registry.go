package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"dragon/internal/secrets"
	"dragon/internal/store"
)

// Registry manages configured AgentProfiles, detects local CLI availability, and resolves Adapter instances.
type Registry struct {
	db        *store.DB
	secrets   secrets.Store
	mu        sync.RWMutex
	overrides map[string]Adapter
}

// NewRegistry creates an Agent Registry backed by store.DB and optional secrets.Store.
func NewRegistry(db *store.DB, sec ...secrets.Store) *Registry {
	var s secrets.Store
	if len(sec) > 0 {
		s = sec[0]
	}
	return &Registry{
		db:        db,
		secrets:   s,
		overrides: make(map[string]Adapter),
	}
}

// RegisterOverride registers a custom/mock Adapter by ID (primarily for tests).
func (r *Registry) RegisterOverride(adapter Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overrides[adapter.ID()] = adapter
}

// DetectAll scans every stored AgentProfile against local PATH concurrently and updates DB detection metadata.
func (r *Registry) DetectAll(ctx context.Context) ([]store.AgentProfile, error) {
	profiles, err := r.db.ListAgentProfiles()
	if err != nil {
		return nil, err
	}

	type detectResult struct {
		detected bool
		binPath  string
		ver      string
	}
	results := make([]detectResult, len(profiles))

	var wg sync.WaitGroup
	for i, p := range profiles {
		wg.Add(1)
		go func(idx int, prof store.AgentProfile) {
			defer wg.Done()
			adapter := NewCLIAgentAdapter(prof)
			detected, binPath, ver := adapter.Detect(ctx)
			results[idx] = detectResult{
				detected: detected,
				binPath:  binPath,
				ver:      ver,
			}
		}(i, p)
	}
	wg.Wait()

	for i, res := range results {
		if err := r.db.UpdateAgentDetection(profiles[i].ID, res.detected, res.binPath, res.ver); err == nil {
			profiles[i].Detected = res.detected
			profiles[i].DetectedPath = res.binPath
			profiles[i].DetectedVersion = res.ver
		}
	}
	return profiles, nil
}

// DetectOne probes a single stored AgentProfile against local PATH and updates its DB detection metadata.
func (r *Registry) DetectOne(ctx context.Context, agentID string) (store.AgentProfile, error) {
	p, err := r.db.GetAgentProfile(agentID)
	if err != nil {
		return store.AgentProfile{}, err
	}
	adapter := NewCLIAgentAdapter(p)
	detected, binPath, ver := adapter.Detect(ctx)
	if err := r.db.UpdateAgentDetection(p.ID, detected, binPath, ver); err == nil {
		p.Detected = detected
		p.DetectedPath = binPath
		p.DetectedVersion = ver
	}
	return p, nil
}

// Resolve returns an executable Adapter for agentID.
func (r *Registry) Resolve(agentID string) (Adapter, store.AgentProfile, error) {
	r.mu.RLock()
	ov, ok := r.overrides[agentID]
	r.mu.RUnlock()
	if ok {
		p, err := r.db.GetAgentProfile(agentID)
		if err != nil {
			p = store.AgentProfile{ID: agentID, Name: agentID, Kind: ov.Kind(), Enabled: true, Detected: true}
		}
		return ov, p, nil
	}

	p, err := r.db.GetAgentProfile(agentID)
	if err != nil {
		return nil, store.AgentProfile{}, fmt.Errorf("未找到 Agent %q: %w", agentID, err)
	}
	return NewCLIAgentAdapter(p), p, nil
}

// ResolveInvocation resolves the Adapter, Profile, effective model ID, and isolated provider environment variables.
func (r *Registry) ResolveInvocation(agentID, modelRefOverride string) (Adapter, store.AgentProfile, string, map[string]string, error) {
	adapter, profile, err := r.Resolve(agentID)
	if err != nil {
		return nil, store.AgentProfile{}, "", nil, err
	}
	model, env := BuildProviderEnv(r.db, r.secrets, profile, modelRefOverride)
	return adapter, profile, model, env, nil
}

// IsNativeFirstKind reports whether an agent kind defaults to its own native login & CLI default model
// unless the user explicitly binds a Provider/Model override.
func IsNativeFirstKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "codex", "qoder", "workbuddy", "codebuddy", "agy", "copilot":
		return true
	default:
		return false
	}
}

// SplitModelRef splits "providerId/modelId" into provider ID and bare model ID.
func SplitModelRef(ref string) (providerID string, modelID string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", ""
	}
	if idx := strings.IndexByte(ref, '/'); idx > 0 {
		return ref[:idx], ref[idx+1:]
	}
	return "", ref
}

// BuildProviderEnv computes the CLI model flag and isolated provider environment variables for an Agent invocation.
func BuildProviderEnv(db *store.DB, sec secrets.Store, profile store.AgentProfile, modelRefOverride string) (string, map[string]string) {
	env := map[string]string{}
	providerID := profile.ProviderID
	modelID := profile.Model

	override := strings.TrimSpace(modelRefOverride)
	isTierKeyword := override == "opus" || override == "sonnet" || override == "haiku"

	if override != "" {
		if isTierKeyword && profile.SupportsTiers {
			tiers := profile.Tiers()
			if mapped := tiers[override]; mapped != "" {
				if p, m := SplitModelRef(mapped); p != "" {
					providerID = p
					modelID = m
				} else {
					modelID = mapped
				}
			} else {
				modelID = override
			}
		} else if p, m := SplitModelRef(override); p != "" {
			providerID = p
			modelID = m
		} else {
			modelID = override
		}
	}

	if db != nil && sec != nil && providerID != "" {
		if prov, err := db.GetModelProvider(providerID); err == nil && prov.Enabled {
			if apiKey, err := sec.Get(secrets.ProviderSecretKey(prov.ID)); err == nil && strings.TrimSpace(apiKey) != "" {
				switch {
				case profile.Kind == "claude":
					if prov.BaseURL != "" {
						env["ANTHROPIC_BASE_URL"] = strings.TrimRight(prov.BaseURL, "/")
					}
					env["ANTHROPIC_AUTH_TOKEN"] = apiKey
					env["ANTHROPIC_API_KEY"] = apiKey
				case prov.Protocol == "google" || profile.Kind == "gemini" || profile.Kind == "agy":
					env["GEMINI_API_KEY"] = apiKey
					if prov.BaseURL != "" && profile.Kind != "gemini" {
						env["OPENAI_BASE_URL"] = strings.TrimRight(prov.BaseURL, "/")
					}
				default:
					env["OPENAI_API_KEY"] = apiKey
					if prov.BaseURL != "" {
						env["OPENAI_BASE_URL"] = strings.TrimRight(prov.BaseURL, "/")
					}
				}
			}
		}
	}

	// Inject Claude Code tier mappings when configured so /model & tier selection align.
	if profile.Kind == "claude" && profile.SupportsTiers {
		tiers := profile.Tiers()
		if _, m := SplitModelRef(tiers["opus"]); m != "" {
			env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = m
		}
		if _, m := SplitModelRef(tiers["sonnet"]); m != "" {
			env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = m
		}
		if _, m := SplitModelRef(tiers["haiku"]); m != "" {
			env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = m
		}
		if isTierKeyword {
			return override, env
		}
	}

	return modelID, env
}
