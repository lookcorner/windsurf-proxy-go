package reuse

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

type Entry struct {
	InstanceName string
	AccountID    string
	CascadeID    string
	CreatedAt    time.Time
	LastAccess   time.Time
}

const (
	ttl     = 10 * time.Minute
	maxSize = 500
)

var globalPool = struct {
	mu    sync.Mutex
	items map[string]Entry
}{
	items: make(map[string]Entry),
}

func FingerprintBefore(messages []map[string]string) string {
	if len(messages) < 2 {
		return ""
	}
	history := messages[:len(messages)-1]
	hasAssistant := false
	for _, msg := range history {
		role := msg["role"]
		if role == "assistant" || role == "tool" {
			hasAssistant = true
			break
		}
	}
	if !hasAssistant {
		return ""
	}
	return fingerprint(history)
}

func FingerprintAfter(messages []map[string]string, assistantText string) string {
	if assistantText == "" {
		return ""
	}
	full := make([]map[string]string, 0, len(messages)+1)
	for _, msg := range messages {
		full = append(full, canonical(msg))
	}
	full = append(full, map[string]string{
		"role":    "assistant",
		"content": assistantText,
	})
	return fingerprint(full)
}

func Peek(fingerprint string) *Entry {
	if fingerprint == "" {
		return nil
	}
	globalPool.mu.Lock()
	defer globalPool.mu.Unlock()
	entry, ok := globalPool.items[fingerprint]
	if !ok {
		return nil
	}
	if expired(entry) {
		delete(globalPool.items, fingerprint)
		return nil
	}
	entry.LastAccess = time.Now()
	globalPool.items[fingerprint] = entry
	copy := entry
	return &copy
}

func Checkout(fingerprint string, instanceName string, accountID string) *Entry {
	if fingerprint == "" || instanceName == "" {
		return nil
	}
	globalPool.mu.Lock()
	defer globalPool.mu.Unlock()
	entry, ok := globalPool.items[fingerprint]
	if !ok {
		return nil
	}
	if expired(entry) || entry.InstanceName != instanceName || entry.AccountID != accountID {
		if expired(entry) {
			delete(globalPool.items, fingerprint)
		}
		return nil
	}
	delete(globalPool.items, fingerprint)
	entry.LastAccess = time.Now()
	copy := entry
	return &copy
}

func Checkin(fingerprint string, entry Entry) {
	if fingerprint == "" || entry.InstanceName == "" || entry.CascadeID == "" {
		return
	}
	globalPool.mu.Lock()
	defer globalPool.mu.Unlock()
	now := time.Now()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.LastAccess = now
	globalPool.items[fingerprint] = entry
	pruneLocked()
}

func Count() int {
	globalPool.mu.Lock()
	defer globalPool.mu.Unlock()
	for key, entry := range globalPool.items {
		if expired(entry) {
			delete(globalPool.items, key)
		}
	}
	return len(globalPool.items)
}

func canonical(msg map[string]string) map[string]string {
	return map[string]string{
		"role":    msg["role"],
		"content": msg["content"],
	}
}

func fingerprint(messages []map[string]string) string {
	canon := make([]map[string]string, 0, len(messages))
	for _, msg := range messages {
		canon = append(canon, canonical(msg))
	}
	data, _ := json.Marshal(canon)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func expired(entry Entry) bool {
	return time.Since(entry.LastAccess) > ttl
}

func pruneLocked() {
	if len(globalPool.items) <= maxSize {
		return
	}
	type kv struct {
		key   string
		entry Entry
	}
	items := make([]kv, 0, len(globalPool.items))
	for key, entry := range globalPool.items {
		if expired(entry) {
			delete(globalPool.items, key)
			continue
		}
		items = append(items, kv{key: key, entry: entry})
	}
	if len(globalPool.items) <= maxSize {
		return
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].entry.LastAccess.Before(items[j].entry.LastAccess)
	})
	for len(globalPool.items) > maxSize && len(items) > 0 {
		delete(globalPool.items, items[0].key)
		items = items[1:]
	}
}
