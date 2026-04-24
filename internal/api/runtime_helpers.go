package api

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"windsurf-proxy-go/internal/accounts"
	"windsurf-proxy-go/internal/balancer"
	"windsurf-proxy-go/internal/reuse"
)

const accountResolveCooldown = 2 * time.Minute

type requestTarget struct {
	Instance    *balancer.Instance
	AccountID   string
	AccountName string
	APIKey      string
	APIServer   string
	ProxyURL    string
}

func (t *requestTarget) routeKey() string {
	if t == nil || t.Instance == nil {
		return ""
	}
	return balancer.RouteKey(t.Instance.Name, t.AccountID)
}

func (t *requestTarget) accountLabel() string {
	if t == nil {
		return ""
	}
	if t.AccountName != "" {
		return t.AccountName
	}
	return t.AccountID
}

func (t *requestTarget) instanceName() string {
	if t == nil || t.Instance == nil {
		return ""
	}
	return t.Instance.Name
}

func (h *Handler) selectRequestTarget(ctx context.Context, messages []map[string]string, triedRoutes map[string]bool, triedAccounts map[string]bool, modelKey string) (*requestTarget, error) {
	fingerprint := reuse.FingerprintBefore(messages)
	if fingerprint != "" {
		entry := reuse.Peek(fingerprint)
		if entry != nil {
			if target, ok := h.tryReuseTarget(ctx, entry, triedRoutes, triedAccounts, modelKey); ok {
				return target, nil
			}
		}
	}

	if target, ok := h.selectAccountBackedTarget(ctx, triedRoutes, triedAccounts, modelKey); ok {
		return target, nil
	}
	target, err := h.selectInstanceBackedTarget(triedRoutes)
	if err != nil && h.accounts != nil && h.accounts.ActiveCount() > 0 && strings.TrimSpace(modelKey) != "" {
		return nil, fmt.Errorf("model %s temporarily unavailable for all accounts; upstream returned internal errors or rate limits", modelKey)
	}
	return target, err
}

func (h *Handler) tryReuseTarget(ctx context.Context, entry *reuse.Entry, triedRoutes map[string]bool, triedAccounts map[string]bool, modelKey string) (*requestTarget, bool) {
	if entry == nil {
		return nil, false
	}
	if entry.AccountID == "" {
		inst, ok := h.balancer.TryAcquireWorkerByName(entry.InstanceName, "", "", "", triedRoutes)
		if !ok {
			return nil, false
		}
		target := h.buildInstanceBackedTarget(inst)
		h.markTargetAcquire(target)
		return target, true
	}
	if triedAccounts != nil && triedAccounts[entry.AccountID] {
		return nil, false
	}
	if h.accounts == nil {
		return nil, false
	}
	if modelKey != "" && !h.accounts.IsSchedulableForModel(entry.AccountID, modelKey) {
		if triedAccounts != nil {
			triedAccounts[entry.AccountID] = true
		}
		return nil, false
	}

	resolved, err := h.accounts.ResolveForRequest(ctx, entry.AccountID)
	if err != nil {
		log.Printf("[API] Reuse target skipped for account '%s': %v", entry.AccountID, err)
		if triedAccounts != nil {
			triedAccounts[entry.AccountID] = true
		}
		return nil, false
	}
	if h.accounts.ShouldRefreshUsage(entry.AccountID, accounts.UsageRefreshTTL) {
		proxyURL := strings.TrimSpace(resolved.Account.Proxy)
		h.ensureStandaloneWorker(resolved.APIServer, proxyURL, resolved.APIKey)
		probe := &requestTarget{
			Instance:    h.balancer.ProbeWorker(entry.AccountID, resolved.APIServer, proxyURL),
			AccountID:   resolved.Account.ID,
			AccountName: resolved.Account.Name,
			APIKey:      resolved.APIKey,
			APIServer:   resolved.APIServer,
			ProxyURL:    proxyURL,
		}
		if h.refreshAccountUsage(ctx, probe) {
			if triedAccounts != nil {
				triedAccounts[entry.AccountID] = true
			}
			return nil, false
		}
	}

	proxyURL := strings.TrimSpace(resolved.Account.Proxy)
	h.ensureStandaloneWorker(resolved.APIServer, proxyURL, resolved.APIKey)
	inst, ok := h.balancer.TryAcquireWorkerByName(entry.InstanceName, entry.AccountID, resolved.APIServer, proxyURL, triedRoutes)
	if !ok {
		return nil, false
	}
	target := &requestTarget{
		Instance:    inst,
		AccountID:   resolved.Account.ID,
		AccountName: resolved.Account.Name,
		APIKey:      resolved.APIKey,
		APIServer:   resolved.APIServer,
		ProxyURL:    proxyURL,
	}
	h.markTargetAcquire(target)
	return target, true
}

func (h *Handler) selectAccountBackedTarget(ctx context.Context, triedRoutes map[string]bool, triedAccounts map[string]bool, modelKey string) (*requestTarget, bool) {
	if h.accounts == nil {
		return nil, false
	}

	for {
		account := h.accounts.Select(triedAccounts, modelKey)
		if account == nil {
			return nil, false
		}

		resolved, err := h.accounts.ResolveForRequest(ctx, account.ID)
		if err != nil {
			log.Printf("[API] Account '%s' skipped: %v", account.Name, err)
			h.accounts.Block(account.ID, err.Error(), accountResolveCooldown)
			if triedAccounts != nil {
				triedAccounts[account.ID] = true
			}
			continue
		}
		if h.accounts.ShouldRefreshUsage(account.ID, accounts.UsageRefreshTTL) {
			proxyURL := strings.TrimSpace(resolved.Account.Proxy)
			h.ensureStandaloneWorker(resolved.APIServer, proxyURL, resolved.APIKey)
			probe := &requestTarget{
				Instance:    h.balancer.ProbeWorker(account.ID, resolved.APIServer, proxyURL),
				AccountID:   resolved.Account.ID,
				AccountName: resolved.Account.Name,
				APIKey:      resolved.APIKey,
				APIServer:   resolved.APIServer,
				ProxyURL:    proxyURL,
			}
			if h.refreshAccountUsage(ctx, probe) {
				if triedAccounts != nil {
					triedAccounts[account.ID] = true
				}
				continue
			}
		}

		proxyURL := strings.TrimSpace(resolved.Account.Proxy)
		h.ensureStandaloneWorker(resolved.APIServer, proxyURL, resolved.APIKey)
		inst, err := h.balancer.GetWorker(triedRoutes, account.ID, resolved.APIServer, proxyURL)
		if err != nil {
			if triedAccounts != nil {
				triedAccounts[account.ID] = true
			}
			continue
		}

		target := &requestTarget{
			Instance:    inst,
			AccountID:   resolved.Account.ID,
			AccountName: resolved.Account.Name,
			APIKey:      resolved.APIKey,
			APIServer:   resolved.APIServer,
			ProxyURL:    proxyURL,
		}
		h.markTargetAcquire(target)
		return target, true
	}
}

func (h *Handler) selectInstanceBackedTarget(triedRoutes map[string]bool) (*requestTarget, error) {
	inst, err := h.balancer.GetWorker(triedRoutes, "", "", "")
	if err != nil {
		return nil, err
	}
	target := h.buildInstanceBackedTarget(inst)
	h.markTargetAcquire(target)
	return target, nil
}

func (h *Handler) ensureStandaloneWorker(apiServer string, proxyURL string, bootstrapAPIKey string) {
	if h == nil || h.balancer == nil {
		return
	}
	if _, err := h.balancer.EnsureStandaloneForRoute(apiServer, proxyURL, bootstrapAPIKey); err != nil && err != balancer.ErrNoStandaloneTemplate {
		log.Printf("[API] Standalone worker route ensure failed (api_server=%s proxy=%s): %v",
			apiServer, proxyForLog(proxyURL), err)
	}
}

func (h *Handler) buildInstanceBackedTarget(inst *balancer.Instance) *requestTarget {
	target := &requestTarget{
		Instance: inst,
		APIKey:   inst.CurrentAPIKey(),
	}
	if inst == nil || inst.AccountID == "" {
		return target
	}
	target.AccountID = inst.AccountID
	if h.accounts != nil {
		if account := h.accounts.Get(inst.AccountID); account != nil {
			target.AccountName = account.Name
		}
	}
	return target
}

func proxyForLog(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		if at := strings.LastIndex(raw, "@"); at > 0 {
			return "***@" + raw[at+1:]
		}
		return raw
	}
	if parsed.User != nil {
		if username := parsed.User.Username(); username != "" {
			parsed.User = url.UserPassword(username, "***")
		} else {
			parsed.User = url.User("***")
		}
	}
	return parsed.String()
}

func conversationDiagnostics(messages []map[string]string) (turns int, chars int) {
	for _, message := range messages {
		role := message["role"]
		if role != "" && role != "system" {
			turns++
		}
		chars += len(role)
		chars += len(message["content"])
		chars += len(message["name"])
	}
	return turns, chars
}

func streamEventError(eventType string, data interface{}) error {
	if eventType != "error" {
		return nil
	}
	if err, ok := data.(error); ok && err != nil {
		return err
	}
	if text, ok := data.(string); ok && strings.TrimSpace(text) != "" {
		return fmt.Errorf("%s", text)
	}
	return fmt.Errorf("upstream stream error")
}

func shouldRefreshLocalCredentials(target *requestTarget, err error) bool {
	if target == nil || target.Instance == nil || target.Instance.Type != "local" || err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "csrf token") && strings.Contains(msg, "unauthenticated")
}

func isQuotaExhaustedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "quota") ||
		strings.Contains(msg, "credit") ||
		strings.Contains(msg, "overage") ||
		strings.Contains(msg, "payment required")
}

func isRateLimitedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "rate_limit") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "trial users")
}

func isGlobalRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "global rate limit") || strings.Contains(msg, "trial users")
}

func isModelUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "model not available") ||
		strings.Contains(msg, "model_not_available") ||
		strings.Contains(msg, "not entitled") ||
		strings.Contains(msg, "model not entitled") ||
		strings.Contains(msg, "unsupported model") ||
		strings.Contains(msg, "model is not supported")
}

func isUpstreamInternalError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "internal error occurred") ||
		strings.Contains(msg, "an internal error")
}

func rateLimitCooldown(err error) time.Duration {
	if err == nil {
		return 5 * time.Minute
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "about an hour") ||
		strings.Contains(msg, "in an hour") ||
		(strings.Contains(msg, "try again in") && strings.Contains(msg, "hour")) {
		return time.Hour
	}
	return 5 * time.Minute
}

func modelUnavailableCooldown(err error) time.Duration {
	if err == nil {
		return 6 * time.Hour
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "not entitled") {
		return 24 * time.Hour
	}
	return 6 * time.Hour
}

func upstreamInternalCooldown(err error) time.Duration {
	if err == nil {
		return 2 * time.Minute
	}
	return 2 * time.Minute
}

func rememberFailedAccount(triedAccounts map[string]bool, target *requestTarget) {
	if triedAccounts == nil || target == nil || target.AccountID == "" {
		return
	}
	triedAccounts[target.AccountID] = true
}

func (h *Handler) refreshAccountUsage(ctx context.Context, target *requestTarget) bool {
	if h.accounts == nil || target == nil || target.Instance == nil || target.AccountID == "" || target.APIKey == "" {
		return false
	}
	if target.Instance.Client == nil || !h.accounts.ShouldRefreshUsage(target.AccountID, accounts.UsageRefreshTTL) {
		return false
	}

	status, err := target.Instance.Client.GetUserStatus(ctx, target.APIKey, target.Instance.Version)
	if err != nil {
		h.accounts.MarkUsageUnavailable(target.AccountID, err.Error())
		log.Printf("[API] Account '%s' usage refresh failed: %v", target.accountLabel(), err)
		return false
	}

	h.accounts.SyncAllowedModels(target.AccountID, status.AllowedModels)
	snapshot := accounts.UsageFromUserStatus(status)
	h.accounts.UpdateUsage(target.AccountID, snapshot, accounts.QuotaRetryCooldown)
	if snapshot.QuotaExhausted {
		log.Printf("[API] Account '%s' skipped: upstream quota exhausted", target.accountLabel())
		return true
	}
	return false
}

func (h *Handler) markTargetAcquire(target *requestTarget) {
	if target == nil || h.accounts == nil || target.AccountID == "" {
		return
	}
	h.accounts.MarkAcquire(target.AccountID)
}

func (h *Handler) releaseTarget(target *requestTarget) {
	if target == nil || target.Instance == nil {
		return
	}
	h.balancer.ReleaseInstance(target.Instance)
	if h.accounts != nil && target.AccountID != "" {
		h.accounts.MarkRelease(target.AccountID)
	}
}

func (h *Handler) markTargetSuccess(target *requestTarget) {
	if target == nil || target.Instance == nil {
		return
	}
	h.balancer.MarkSuccess(target.Instance)
	if h.accounts != nil && target.AccountID != "" {
		h.accounts.MarkSuccess(target.AccountID)
	}
}

func (h *Handler) markTargetError(target *requestTarget, reqErr error, modelKey string) {
	if target == nil || target.Instance == nil || reqErr == nil {
		return
	}
	accountQuotaErr := target.AccountID != "" && isQuotaExhaustedError(reqErr)
	accountRateLimitErr := target.AccountID != "" && isRateLimitedError(reqErr)
	accountModelErr := target.AccountID != "" && isModelUnavailableError(reqErr)
	accountUpstreamInternalErr := target.AccountID != "" && modelKey != "" && isUpstreamInternalError(reqErr)
	if !accountQuotaErr && !accountRateLimitErr && !accountModelErr && !accountUpstreamInternalErr {
		h.balancer.MarkError(target.Instance, reqErr.Error())
	}
	if h.accounts != nil && target.AccountID != "" {
		switch {
		case accountQuotaErr:
			h.accounts.MarkQuotaExhausted(target.AccountID, reqErr.Error(), accounts.QuotaRetryCooldown)
		case accountRateLimitErr:
			cooldown := rateLimitCooldown(reqErr)
			if modelKey != "" && !isGlobalRateLimitError(reqErr) {
				h.accounts.MarkModelRateLimited(target.AccountID, modelKey, reqErr.Error(), cooldown)
				log.Printf("[API] Account '%s' rate limited on %s for %s: %v", target.accountLabel(), modelKey, cooldown.Round(time.Second), reqErr)
			} else {
				h.accounts.MarkRateLimited(target.AccountID, reqErr.Error(), cooldown)
				log.Printf("[API] Account '%s' rate limited for %s: %v", target.accountLabel(), cooldown.Round(time.Second), reqErr)
			}
		case accountModelErr:
			cooldown := modelUnavailableCooldown(reqErr)
			h.accounts.MarkModelUnavailable(target.AccountID, modelKey, reqErr.Error(), cooldown)
			log.Printf("[API] Account '%s' model %s blocked for %s: %v", target.accountLabel(), modelKey, cooldown.Round(time.Minute), reqErr)
		case accountUpstreamInternalErr:
			cooldown := upstreamInternalCooldown(reqErr)
			h.accounts.MarkModelRateLimited(target.AccountID, modelKey, reqErr.Error(), cooldown)
			h.balancer.MarkUnhealthy(target.Instance, reqErr.Error())
			log.Printf("[API] Account '%s' upstream internal error on %s; cooling model for %s: %v", target.accountLabel(), modelKey, cooldown.Round(time.Second), reqErr)
		default:
			h.accounts.MarkError(target.AccountID, reqErr.Error(), 3)
		}
	}
}

func (h *Handler) recoverLocalInstanceAuth(target *requestTarget, reqErr error, triedRoutes map[string]bool) (retry bool, handled bool) {
	if !shouldRefreshLocalCredentials(target, reqErr) {
		return false, false
	}

	h.releaseTarget(target)
	if err := h.balancer.RefreshLocalInstance(target.Instance); err != nil {
		h.markTargetError(target, fmt.Errorf("%s; local credential refresh failed: %v", reqErr.Error(), err), "")
		log.Printf("[API] Local instance '%s' auth refresh failed after CSRF error: %v", target.Instance.Name, err)
		return false, true
	}

	if triedRoutes != nil {
		delete(triedRoutes, target.routeKey())
	}
	log.Printf("[API] Local instance '%s' auth refreshed after CSRF failure", target.Instance.Name)
	return true, true
}
