package core

import "strings"

// AnthropicModelAliases maps Anthropic-native model IDs (as used by the
// official Anthropic API / Claude Code / @anthropic-ai/sdk) to the canonical
// internal names used by this proxy. Keys are always lowercase.
//
// Date-suffixed identifiers (e.g. "claude-3-5-sonnet-20241022") live here
// alongside the "-latest" pointers. When Anthropic ships a new snapshot the
// alias table is the only place that needs updating.
var AnthropicModelAliases = map[string]string{
	// Claude 3
	"claude-3-opus-20240229":   "claude-3-opus",
	"claude-3-opus-latest":     "claude-3-opus",
	"claude-3-sonnet-20240229": "claude-3-sonnet",
	"claude-3-haiku-20240307":  "claude-3-haiku",

	// Claude 3.5
	"claude-3-5-sonnet-20240620": "claude-3.5-sonnet",
	"claude-3-5-sonnet-20241022": "claude-3.5-sonnet",
	"claude-3-5-sonnet-latest":   "claude-3.5-sonnet",
	"claude-3-5-haiku-20241022":  "claude-3.5-haiku",
	"claude-3-5-haiku-latest":    "claude-3.5-haiku",

	// Claude 3.7
	"claude-3-7-sonnet-20250219": "claude-3.7-sonnet",
	"claude-3-7-sonnet-latest":   "claude-3.7-sonnet",

	// Claude 4 (opus/sonnet use "claude-<variant>-4-<date>" form on the
	// Anthropic side).
	"claude-opus-4-20250514":   "claude-4-opus",
	"claude-opus-4-0":          "claude-4-opus",
	"claude-opus-4-latest":     "claude-4-opus",
	"claude-sonnet-4-20250514": "claude-4-sonnet",
	"claude-sonnet-4-0":        "claude-4-sonnet",
	"claude-sonnet-4-latest":   "claude-4-sonnet",

	// Claude 4.1
	"claude-opus-4-1-20250805": "claude-4.1-opus",
	"claude-opus-4-1":          "claude-4.1-opus",

	// Claude 4.5
	"claude-sonnet-4-5-20250929": "claude-4.5-sonnet",
	"claude-sonnet-4-5":          "claude-4.5-sonnet",
	"claude-sonnet-4-5-latest":   "claude-4.5-sonnet",
	"claude-opus-4-5-20250929":   "claude-4.5-opus",
	"claude-opus-4-5":            "claude-4.5-opus",

	// Claude 4.6 (match the dash-form UIDs the proxy already stores).
	"claude-sonnet-4-6":        "claude-4.6-sonnet",
	"claude-sonnet-4-6-latest": "claude-4.6-sonnet",
	"claude-opus-4-6":          "claude-4.6-opus",

	// Claude 4.7 (effort-tiered, native 1M context). The bare/-latest names
	// fall back to the project's "default = low" convention used by the
	// dash-form UID table; explicit tiers map straight through.
	"claude-opus-4-7":        "claude-4.7-opus",
	"claude-opus-4-7-latest": "claude-4.7-opus",
	"claude-opus-4-7-low":    "claude-4.7-opus-low",
	"claude-opus-4-7-medium": "claude-4.7-opus-medium",
	"claude-opus-4-7-high":   "claude-4.7-opus-high",
	"claude-opus-4-7-xhigh":  "claude-4.7-opus-xhigh",
	"claude-opus-4-7-max":    "claude-4.7-opus-max",
}

// ResolveAnthropicAlias returns the internal canonical model name if the
// supplied Anthropic-style name has a known mapping. It falls back to the
// original name (trimmed & lowered) when no alias matches, so existing
// internal names like "claude-3.5-sonnet" keep working on /v1/messages too.
//
// Bracketed suffixes such as "[1m]" or "[200k]" — added by Claude Code to
// distinguish context-window variants of the same model — are stripped
// before lookup, since Cascade decides the context size upstream.
func ResolveAnthropicAlias(name string) string {
	normalized := stripContextSuffix(normalizeModelName(name))
	if mapped, ok := AnthropicModelAliases[normalized]; ok {
		return mapped
	}
	return normalized
}

// stripContextSuffix removes a trailing "[...]" segment (e.g. "[1m]",
// "[200k]") from a model name. Returns the input unchanged if no such
// suffix is present.
func stripContextSuffix(name string) string {
	if !strings.HasSuffix(name, "]") {
		return name
	}
	if i := strings.LastIndexByte(name, '['); i > 0 {
		return strings.TrimSpace(name[:i])
	}
	return name
}
