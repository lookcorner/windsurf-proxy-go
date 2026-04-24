package api

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type cachedResponse struct {
	Text       string
	CreatedAt  time.Time
	LastAccess time.Time
}

const ( 
	responseCacheTTL  = 10 * time.Minute
	responseCacheSize = 256
)

var globalResponseCache = struct {
	mu    sync.Mutex
	items map[string]cachedResponse
}{
	items: make(map[string]cachedResponse),
}

func makeResponseCacheKey(protocol string, apiKey string, body []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(protocol))
	_, _ = hash.Write([]byte("\n"))
	_, _ = hash.Write([]byte(apiKey))
	_, _ = hash.Write([]byte("\n"))
	_, _ = hash.Write(body)
	return hex.EncodeToString(hash.Sum(nil))
}

func getCachedResponse(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	globalResponseCache.mu.Lock()
	defer globalResponseCache.mu.Unlock()
	entry, ok := globalResponseCache.items[key]
	if !ok {
		return "", false
	}
	if time.Since(entry.LastAccess) > responseCacheTTL {
		delete(globalResponseCache.items, key)
		return "", false
	}
	entry.LastAccess = time.Now()
	globalResponseCache.items[key] = entry
	return entry.Text, true
}

func putCachedResponse(key string, text string) {
	if key == "" {
		return
	}
	globalResponseCache.mu.Lock()
	defer globalResponseCache.mu.Unlock()
	now := time.Now()
	globalResponseCache.items[key] = cachedResponse{
		Text:       text,
		CreatedAt:  now,
		LastAccess: now,
	}
	if len(globalResponseCache.items) <= responseCacheSize {
		return
	}
	var oldestKey string
	var oldestTime time.Time
	first := true
	for key, entry := range globalResponseCache.items {
		if time.Since(entry.LastAccess) > responseCacheTTL {
			delete(globalResponseCache.items, key)
			continue
		}
		if first || entry.LastAccess.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.LastAccess
			first = false
		}
	}
	if len(globalResponseCache.items) > responseCacheSize && oldestKey != "" {
		delete(globalResponseCache.items, oldestKey)
	}
}

func responseCacheCount() int {
	globalResponseCache.mu.Lock()
	defer globalResponseCache.mu.Unlock()
	for key, entry := range globalResponseCache.items {
		if time.Since(entry.LastAccess) > responseCacheTTL {
			delete(globalResponseCache.items, key)
		}
	}
	return len(globalResponseCache.items)
}
