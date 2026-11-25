//go:build !windows

package secrets

// NewDefaultStore falls back to an in-memory credential store on non-Windows platforms.
func NewDefaultStore() Store {
	return NewMemoryStore()
}
