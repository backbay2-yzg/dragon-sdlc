package secrets

import (
	"errors"
	"sync"
)

const (
	KeyGitHubToken    = "github_token"
	KeyLLMAPIKey      = "llm_api_key"
	KeyAgentEnvPrefix = "agent_env/"
	KeyProviderPrefix = "provider_key/"
)

// ProviderSecretKey returns the canonical credential key for a model provider.
func ProviderSecretKey(providerID string) string {
	return KeyProviderPrefix + providerID
}

// ErrNotFound is returned when a secret key is absent from the store.
var ErrNotFound = errors.New("secret not found")

// Store defines secure credential storage operations.
type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
	Has(key string) bool
}

// MemoryStore is a thread-safe in-memory credential store for tests and non-Windows fallback.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewMemoryStore initializes an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]string)}
}

func (m *MemoryStore) Get(key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok || v == "" {
		return "", ErrNotFound
	}
	return v, nil
}

func (m *MemoryStore) Set(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value == "" {
		delete(m.data, key)
		return nil
	}
	m.data[key] = value
	return nil
}

func (m *MemoryStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *MemoryStore) Has(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.data[key] != ""
}
