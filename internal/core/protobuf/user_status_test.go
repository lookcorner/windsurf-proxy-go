package protobuf

import (
	"slices"
	"testing"

	"windsurf-proxy-go/internal/core"
)

func TestParseGetUserStatusResponse(t *testing.T) {
	planInfo := make([]byte, 0)
	planInfo = append(planInfo, EncodeVarintField(1, 2)...)
	planInfo = append(planInfo, EncodeStringField(2, "Windsurf Pro")...)
	planInfo = append(planInfo, EncodeVarintField(12, 500)...)
	planInfo = append(planInfo, EncodeVarintField(13, 200)...)
	planInfo = append(planInfo, EncodeVarintField(6, 300)...)
	planInfo = append(planInfo, EncodeMessageField(21, EncodeMessageField(1, EncodeVarintField(1, uint64(core.GPT4oMini))))...)
	planInfo = append(planInfo, EncodeMessageField(21, EncodeMessageField(1, EncodeVarintField(1, uint64(core.Gemini25Flash))))...)

	planStatus := make([]byte, 0)
	planStatus = append(planStatus, EncodeMessageField(1, planInfo)...)
	planStatus = append(planStatus, EncodeVarintField(8, 120)...)
	planStatus = append(planStatus, EncodeVarintField(9, 40)...)
	planStatus = append(planStatus, EncodeVarintField(6, 380)...)
	planStatus = append(planStatus, EncodeVarintField(5, 160)...)
	planStatus = append(planStatus, EncodeVarintField(14, 42)...)
	planStatus = append(planStatus, EncodeVarintField(15, 55)...)
	planStatus = append(planStatus, EncodeVarintField(17, 1713800000)...)
	planStatus = append(planStatus, EncodeVarintField(18, 1714400000)...)

	userStatus := make([]byte, 0)
	userStatus = append(userStatus, EncodeMessageField(13, planStatus)...)
	userStatus = append(userStatus, EncodeVarintField(28, 381)...)
	userStatus = append(userStatus, EncodeVarintField(29, 161)...)
	userStatus = append(userStatus, EncodeVarintField(35, 300)...)

	resp := make([]byte, 0)
	resp = append(resp, EncodeMessageField(1, userStatus)...)
	resp = append(resp, EncodeMessageField(2, planInfo)...)

	got := ParseGetUserStatusResponse(resp)
	if got.PlanName != "Windsurf Pro" {
		t.Fatalf("PlanName = %q, want Windsurf Pro", got.PlanName)
	}
	if got.UsedPromptCredits != 381 {
		t.Fatalf("UsedPromptCredits = %d, want 381", got.UsedPromptCredits)
	}
	if got.UsedFlowCredits != 161 {
		t.Fatalf("UsedFlowCredits = %d, want 161", got.UsedFlowCredits)
	}
	if got.AvailablePromptCredits != 120 {
		t.Fatalf("AvailablePromptCredits = %d, want 120", got.AvailablePromptCredits)
	}
	if got.AvailableFlowCredits != 40 {
		t.Fatalf("AvailableFlowCredits = %d, want 40", got.AvailableFlowCredits)
	}
	if got.DailyQuotaRemainingPercent != 42 {
		t.Fatalf("DailyQuotaRemainingPercent = %d, want 42", got.DailyQuotaRemainingPercent)
	}
	if got.WeeklyQuotaRemainingPercent != 55 {
		t.Fatalf("WeeklyQuotaRemainingPercent = %d, want 55", got.WeeklyQuotaRemainingPercent)
	}
	if got.MaxPremiumChatMessages != 300 {
		t.Fatalf("MaxPremiumChatMessages = %d, want 300", got.MaxPremiumChatMessages)
	}
	if got.QuotaExhausted {
		t.Fatalf("QuotaExhausted = true, want false")
	}
	if !slices.Contains(got.AllowedModels, "gpt-4o-mini") {
		t.Fatalf("AllowedModels missing gpt-4o-mini: %#v", got.AllowedModels)
	}
	if !slices.Contains(got.AllowedModels, "gemini-2.5-flash") {
		t.Fatalf("AllowedModels missing gemini-2.5-flash: %#v", got.AllowedModels)
	}
}

func TestParseGetUserStatusResponseDetectsExhaustedQuota(t *testing.T) {
	planInfo := make([]byte, 0)
	planInfo = append(planInfo, EncodeStringField(2, "Starter")...)
	planInfo = append(planInfo, EncodeVarintField(12, 300)...)

	planStatus := make([]byte, 0)
	planStatus = append(planStatus, EncodeMessageField(1, planInfo)...)
	planStatus = append(planStatus, EncodeVarintField(8, 0)...)
	planStatus = append(planStatus, EncodeVarintField(6, 300)...)

	userStatus := make([]byte, 0)
	userStatus = append(userStatus, EncodeMessageField(13, planStatus)...)
	userStatus = append(userStatus, EncodeVarintField(28, 300)...)

	resp := EncodeMessageField(1, userStatus)

	got := ParseGetUserStatusResponse(resp)
	if !got.QuotaExhausted {
		t.Fatalf("QuotaExhausted = false, want true")
	}
}
