// Package auth provides background token refresh for standalone instances.
package auth

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// TokenManager manages token lifecycle for a standalone instance.
// Periodically refreshes Firebase tokens and re-registers with Windsurf
// to keep the service API key valid.
type TokenManager struct {
	tokens       *FirebaseTokens
	serviceAuth  *WindsurfServiceAuth
	apiKeyUpdate func(newKey string) // Callback to update language server

	mu     sync.Mutex
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// Constants for token refresh
const (
	refreshMarginSeconds = 600 // Refresh 10 minutes before expiry (tokens last ~3600s)
	minRefreshInterval   = 300 // Never refresh faster than every 5 min
	retryDelay           = 30  // Seconds between retries on failure
	maxRetries           = 5
)

// NewTokenManager creates a new token manager.
func NewTokenManager(
	tokens *FirebaseTokens,
	serviceAuth *WindsurfServiceAuth,
	apiKeyUpdate func(newKey string),
) *TokenManager {
	return &TokenManager{
		tokens:       tokens,
		serviceAuth:  serviceAuth,
		apiKeyUpdate: apiKeyUpdate,
		stopCh:       make(chan struct{}),
	}
}

// APIKey returns the current service API key.
func (tm *TokenManager) APIKey() string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.serviceAuth.APIKey
}

// refreshInterval calculates the next refresh interval.
func (tm *TokenManager) refreshInterval() time.Duration {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	interval := tm.tokens.ExpiresIn - refreshMarginSeconds
	if interval < minRefreshInterval {
		interval = minRefreshInterval
	}

	return time.Duration(interval) * time.Second
}

// Start begins the background refresh loop.
func (tm *TokenManager) Start() {
	tm.wg.Add(1)
	go tm.refreshLoop()
	log.Printf("[TokenManager] Started (interval ~%d seconds)", tm.refreshInterval()/time.Second)
}

// Stop halts the background refresh loop.
func (tm *TokenManager) Stop() {
	close(tm.stopCh)
	tm.wg.Wait()
	log.Printf("[TokenManager] Stopped")
}

// refreshLoop periodically refreshes tokens.
func (tm *TokenManager) refreshLoop() {
	defer tm.wg.Done()

	for {
		select {
		case <-tm.stopCh:
			return
		case <-time.After(tm.refreshInterval()):
			// Time to refresh
		}

		// Check if stopped
		select {
		case <-tm.stopCh:
			return
		default:
		}

		// Retry loop
		retries := 0
		for retries < maxRetries {
			err := tm.doRefresh()
			if err == nil {
				break
			}

			retries++
			log.Printf("[TokenManager] Refresh failed (attempt %d/%d): %v", retries, maxRetries, err)

			if retries < maxRetries {
				select {
				case <-tm.stopCh:
					return
				case <-time.After(retryDelay * time.Second):
					// Retry
				}
			}
		}

		if retries >= maxRetries {
			log.Printf("[TokenManager] Token refresh exhausted all retries, stopping")
			return
		}
	}
}

// doRefresh performs a single refresh cycle.
// Step 1: Refresh Firebase token
// Step 2: Re-register to get new service API key
// Step 3: Update language server if key changed
func (tm *TokenManager) doRefresh() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Step 1: Refresh Firebase token
	newTokens, err := RefreshFirebaseToken(ctx, tm.tokens.RefreshToken)
	if err != nil {
		return fmt.Errorf("Firebase token refresh failed: %w", err)
	}

	tm.mu.Lock()
	tm.tokens = newTokens
	tm.mu.Unlock()

	log.Printf("[TokenManager] Firebase token refreshed (expires_in=%ds)", newTokens.ExpiresIn)

	// Step 2: Re-register to get new service API key
	newAuth, err := RegisterWindsurfUser(ctx, newTokens.IDToken)
	if err != nil {
		return fmt.Errorf("RegisterWindsurfUser failed: %w", err)
	}

	tm.mu.Lock()
	oldKey := tm.serviceAuth.APIKey
	tm.serviceAuth = newAuth
	tm.mu.Unlock()

	// Step 3: Update language server if key changed
	if newAuth.APIKey != oldKey {
		log.Printf("[TokenManager] Service API key changed, notifying language server...")
		if tm.apiKeyUpdate != nil {
			tm.apiKeyUpdate(newAuth.APIKey)
		}
	} else {
		log.Printf("[TokenManager] Service API key unchanged, no LS restart needed")
	}

	return nil
}