// Package keys provides API key management and request authentication.
package keys

import (
	"strings"
	"sync"
	"time"

	"windsurf-proxy-go/internal/config"
)

// RateBucket implements sliding-window token bucket for rate limiting.
type RateBucket struct {
	timestamps []time.Time
	mu         sync.Mutex
}

// Allow checks if a request is within the rate limit.
func (b *RateBucket) Allow(limit int, window time.Duration) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	// Remove old timestamps
	valid := make([]time.Time, 0)
	for _, t := range b.timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	b.timestamps = valid

	if len(b.timestamps) >= limit {
		return false
	}

	b.timestamps = append(b.timestamps, now)
	return true
}

// Manager validates API keys and enforces rate limits + model allowlists.
type Manager struct {
	keys    map[string]*config.APIKeyEntry
	buckets map[string]*RateBucket
	enabled bool
	mu      sync.RWMutex
}

// NewManager creates a new API key manager.
func NewManager(entries []config.APIKeyEntry) *Manager {
	m := &Manager{
		keys:    make(map[string]*config.APIKeyEntry),
		buckets: make(map[string]*RateBucket),
		enabled: len(entries) > 0,
	}

	for _, entry := range entries {
		m.keys[entry.Key] = &entry
		m.buckets[entry.Key] = &RateBucket{}
	}

	return m
}

// Validate validates a Bearer token.
// Returns the matching APIKeyEntry or nil.
// If no keys are configured, all requests are allowed.
func (m *Manager) Validate(token string) *config.APIKeyEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.enabled {
		return &config.APIKeyEntry{
			Key:           "__open__",
			Name:          "open",
			AllowedModels: []string{"*"},
		}
	}

	if token == "" {
		return nil
	}

	// Strip 'Bearer ' prefix if present
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = token[7:]
	}

	return m.keys[token]
}

// CheckRateLimit checks if the request is within the rate limit for this key.
func (m *Manager) CheckRateLimit(key string) bool {
	m.mu.RLock()
	entry, ok := m.keys[key]
	bucket := m.buckets[key]
	m.mu.RUnlock()

	if !ok || entry == nil {
		return true
	}

	if bucket == nil {
		return true
	}

	return bucket.Allow(entry.RateLimit, time.Minute)
}

// IsModelAllowed checks if the given model is allowed for this key.
func (m *Manager) IsModelAllowed(key string, model string) bool {
	m.mu.RLock()
	entry, ok := m.keys[key]
	m.mu.RUnlock()

	if !ok || entry == nil {
		return true
	}

	// Wildcard allows all models
	for _, allowed := range entry.AllowedModels {
		if allowed == "*" {
			return true
		}
		if strings.EqualFold(allowed, model) {
			return true
		}
	}

	return false
}

// Add adds a new API key.
func (m *Manager) Add(entry *config.APIKeyEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.keys[entry.Key] = entry
	m.buckets[entry.Key] = &RateBucket{}
	m.enabled = true
}

// AddKey adds a new API key (alias for Add).
func (m *Manager) AddKey(entry config.APIKeyEntry) {
	m.Add(&entry)
}

// Remove removes an API key.
func (m *Manager) Remove(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.keys, key)
	delete(m.buckets, key)
	m.enabled = len(m.keys) > 0
}

// RemoveKey removes an API key (alias for Remove).
func (m *Manager) RemoveKey(key string) {
	m.Remove(key)
}

// GetEntry returns an API key entry.
func (m *Manager) GetEntry(key string) *config.APIKeyEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.keys[key]
}

// List returns all API keys (masked).
func (m *Manager) List() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]map[string]interface{}, 0)
	for key, entry := range m.keys {
		masked := maskKey(key)
		result = append(result, map[string]interface{}{
			"id":            key,
			"name":          entry.Name,
			"key_masked":    masked,
			"rate_limit":    entry.RateLimit,
			"allowed_models": entry.AllowedModels,
		})
	}
	return result
}

// Enabled returns whether API key auth is enabled.
func (m *Manager) Enabled() bool {
	return m.enabled
}

func maskKey(key string) string {
	if len(key) > 16 {
		return key[:8] + "..." + key[len(key)-4:]
	}
	return "****"
}