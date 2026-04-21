// Package core provides model name to Windsurf protobuf enum/UID mappings.
package core

import (
	"strings"
)

// ModelEnum contains numeric enum values for Windsurf models used in protobuf encoding.
// Values extracted from Windsurf extension.js via reverse engineering.
type ModelEnum int

const (
	ModelUnspecified ModelEnum = 0

	// Claude Models
	Claude3Opus            ModelEnum = 63
	Claude3Sonnet          ModelEnum = 64
	Claude3Haiku           ModelEnum = 172
	Claude35Sonnet         ModelEnum = 80
	Claude35SonnetNew      ModelEnum = 166
	Claude35Haiku          ModelEnum = 171
	Claude37Sonnet         ModelEnum = 226
	Claude37SonnetThinking ModelEnum = 227
	Claude4Opus            ModelEnum = 290
	Claude4OpusThinking    ModelEnum = 291
	Claude4Sonnet          ModelEnum = 281
	Claude4SonnetThinking  ModelEnum = 282
	Claude41Opus           ModelEnum = 328
	Claude41OpusThinking   ModelEnum = 329
	Claude45Sonnet         ModelEnum = 353
	Claude45SonnetThinking ModelEnum = 354
	Claude45Sonnet1M       ModelEnum = 370
	Claude45Opus           ModelEnum = 391
	Claude45OpusThinking   ModelEnum = 392
	ClaudeCode             ModelEnum = 344

	// Claude 4.6 Models
	Claude46Opus             ModelEnum = 450
	Claude46OpusThinking     ModelEnum = 451
	Claude46Sonnet           ModelEnum = 452
	Claude46SonnetThinking   ModelEnum = 453
	Claude46Sonnet1M         ModelEnum = 454
	Claude46SonnetThinking1M ModelEnum = 455

	// GPT Models
	GPT4        ModelEnum = 30
	GPT4Turbo   ModelEnum = 37
	GPT4o       ModelEnum = 71
	GPT4oNew    ModelEnum = 109
	GPT4oMini   ModelEnum = 113
	GPT45       ModelEnum = 228
	GPT41       ModelEnum = 259
	GPT41Mini   ModelEnum = 260
	GPT41Nano   ModelEnum = 261
	GPT5Nano    ModelEnum = 337
	GPT5Minimal ModelEnum = 338
	GPT5Low     ModelEnum = 339
	GPT5        ModelEnum = 340
	GPT5High    ModelEnum = 341
	GPT5Codex   ModelEnum = 346

	// GPT 5.1 Codex Models
	GPT51CodexMiniLow    ModelEnum = 385
	GPT51CodexMiniMedium ModelEnum = 386
	GPT51CodexMiniHigh   ModelEnum = 387
	GPT51CodexLow        ModelEnum = 388
	GPT51CodexMedium     ModelEnum = 389
	GPT51CodexHigh       ModelEnum = 390
	GPT51CodexMaxLow     ModelEnum = 395
	GPT51CodexMaxMedium  ModelEnum = 396
	GPT51CodexMaxHigh    ModelEnum = 397

	// GPT 5.2 Models
	GPT52None           ModelEnum = 399
	GPT52Low            ModelEnum = 400
	GPT52Medium         ModelEnum = 401
	GPT52High           ModelEnum = 402
	GPT52XHigh          ModelEnum = 403
	GPT52NonePriority   ModelEnum = 404
	GPT52LowPriority    ModelEnum = 405
	GPT52MediumPriority ModelEnum = 406
	GPT52HighPriority   ModelEnum = 407
	GPT52XHighPriority  ModelEnum = 408

	// GPT 5.3 Models (Codex)
	GPT53Codex              ModelEnum = 420
	GPT53CodexLow           ModelEnum = 421
	GPT53CodexHigh          ModelEnum = 422
	GPT53CodexXHigh         ModelEnum = 423
	GPT53CodexSpark         ModelEnum = 424
	GPT53CodexLowPriority   ModelEnum = 425
	GPT53CodexPriority      ModelEnum = 426
	GPT53CodexHighPriority  ModelEnum = 427
	GPT53CodexXHighPriority ModelEnum = 428

	// GPT 5.4 Models
	GPT54None           ModelEnum = 430
	GPT54Low            ModelEnum = 431
	GPT54Medium         ModelEnum = 432
	GPT54High           ModelEnum = 433
	GPT54XHigh          ModelEnum = 434
	GPT54NonePriority   ModelEnum = 435
	GPT54LowPriority    ModelEnum = 436
	GPT54MediumPriority ModelEnum = 437
	GPT54HighPriority   ModelEnum = 438
	GPT54XHighPriority  ModelEnum = 439
	GPT54MiniLow        ModelEnum = 440
	GPT54MiniMedium     ModelEnum = 441
	GPT54MiniHigh       ModelEnum = 442
	GPT54MiniXHigh      ModelEnum = 443

	// O-Series (OpenAI Reasoning)
	O1Preview  ModelEnum = 117
	O1Mini     ModelEnum = 118
	O1         ModelEnum = 170
	O3Mini     ModelEnum = 207
	O3MiniLow  ModelEnum = 213
	O3MiniHigh ModelEnum = 214
	O3         ModelEnum = 218
	O3Low      ModelEnum = 262
	O3High     ModelEnum = 263
	O3Pro      ModelEnum = 294
	O3ProLow   ModelEnum = 295
	O3ProHigh  ModelEnum = 296
	O4Mini     ModelEnum = 264
	O4MiniLow  ModelEnum = 265
	O4MiniHigh ModelEnum = 266

	// Google Gemini
	Gemini10Pro           ModelEnum = 61
	Gemini15Pro           ModelEnum = 62
	Gemini20Flash         ModelEnum = 184
	Gemini25Pro           ModelEnum = 246
	Gemini25Flash         ModelEnum = 312
	Gemini25FlashThinking ModelEnum = 313
	Gemini25FlashLite     ModelEnum = 343
	Gemini30ProLow        ModelEnum = 378
	Gemini30ProHigh       ModelEnum = 379
	Gemini30ProMinimal    ModelEnum = 411
	Gemini30ProMedium     ModelEnum = 412
	Gemini30FlashMinimal  ModelEnum = 413
	Gemini30FlashLow      ModelEnum = 414
	Gemini30FlashMedium   ModelEnum = 415
	Gemini30FlashHigh     ModelEnum = 416
	Gemini31ProLow        ModelEnum = 460
	Gemini31ProHigh       ModelEnum = 461

	// DeepSeek
	DeepSeekV3     ModelEnum = 205
	DeepSeekR1     ModelEnum = 206
	DeepSeekR1Slow ModelEnum = 215
	DeepSeekR1Fast ModelEnum = 216
	DeepSeekV32    ModelEnum = 409

	// Llama
	Llama318B    ModelEnum = 106
	Llama3170B   ModelEnum = 107
	Llama31405B  ModelEnum = 105
	Llama3370B   ModelEnum = 208
	Llama3370BR1 ModelEnum = 209

	// Qwen
	Qwen257B           ModelEnum = 178
	Qwen2532B          ModelEnum = 179
	Qwen2572B          ModelEnum = 180
	Qwen3235B          ModelEnum = 324
	Qwen3Coder480B     ModelEnum = 325
	Qwen3Coder480BFast ModelEnum = 327
	Qwen2532BR1        ModelEnum = 224

	// XAI Grok
	Grok2        ModelEnum = 212
	Grok3        ModelEnum = 217
	Grok3Mini    ModelEnum = 234
	GrokCodeFast ModelEnum = 345

	// Other Models
	Mistral7B      ModelEnum = 77
	KimiK2         ModelEnum = 323
	KimiK2Thinking ModelEnum = 394
	GLM45          ModelEnum = 342
	GLM45Fast      ModelEnum = 352
	GLM46          ModelEnum = 356
	GLM46Fast      ModelEnum = 357
	GLM47          ModelEnum = 417
	GLM47Fast      ModelEnum = 418
	MiniMaxM2      ModelEnum = 368
	MiniMaxM21     ModelEnum = 419
	SWE15          ModelEnum = 359
	SWE15Thinking  ModelEnum = 369
	SWE15Slow      ModelEnum = 377
	SWE16          ModelEnum = 380
	SWE16Fast      ModelEnum = 381
)

// ModelNameToEnum maps OpenAI-style model names to Windsurf enum values.
var ModelNameToEnum = map[string]ModelEnum{
	// Claude
	"claude-3-opus":              Claude3Opus,
	"claude-3-sonnet":            Claude3Sonnet,
	"claude-3-haiku":             Claude3Haiku,
	"claude-3.5-sonnet":          Claude35SonnetNew,
	"claude-3.5-haiku":           Claude35Haiku,
	"claude-3.7-sonnet":          Claude37Sonnet,
	"claude-3.7-sonnet-thinking": Claude37SonnetThinking,
	"claude-4-opus":              Claude4Opus,
	"claude-4-opus-thinking":     Claude4OpusThinking,
	"claude-4-sonnet":            Claude4Sonnet,
	"claude-4-sonnet-thinking":   Claude4SonnetThinking,
	"claude-4.1-opus":            Claude41Opus,
	"claude-4.1-opus-thinking":   Claude41OpusThinking,
	"claude-4.5-sonnet":          Claude45Sonnet,
	"claude-4.5-sonnet-thinking": Claude45SonnetThinking,
	"claude-4.5-opus":            Claude45Opus,
	"claude-4.5-opus-thinking":   Claude45OpusThinking,
	"claude-code":                ClaudeCode,

	// Claude 4.6
	"claude-4.6-opus":               Claude46Opus,
	"claude-4.6-opus-thinking":      Claude46OpusThinking,
	"claude-4.6-sonnet":             Claude46Sonnet,
	"claude-4.6-sonnet-thinking":    Claude46SonnetThinking,
	"claude-4.6-sonnet-1m":          Claude46Sonnet1M,
	"claude-4.6-sonnet-thinking-1m": Claude46SonnetThinking1M,

	// GPT
	"gpt-4":        GPT4,
	"gpt-4-turbo":  GPT4Turbo,
	"gpt-4o":       GPT4oNew,
	"gpt-4o-mini":  GPT4oMini,
	"gpt-4.1":      GPT41,
	"gpt-4.1-mini": GPT41Mini,
	"gpt-4.1-nano": GPT41Nano,
	"gpt-5":        GPT5,
	"gpt-5-nano":   GPT5Nano,
	"gpt-5-low":    GPT5Low,
	"gpt-5-high":   GPT5High,
	"gpt-5-codex":  GPT5Codex,

	// GPT 5.1 Codex
	"gpt-5.1-codex-mini":      GPT51CodexMiniMedium,
	"gpt-5.1-codex-mini-low":  GPT51CodexMiniLow,
	"gpt-5.1-codex-mini-high": GPT51CodexMiniHigh,
	"gpt-5.1-codex":           GPT51CodexMedium,
	"gpt-5.1-codex-low":       GPT51CodexLow,
	"gpt-5.1-codex-high":      GPT51CodexHigh,
	"gpt-5.1-codex-max":       GPT51CodexMaxMedium,
	"gpt-5.1-codex-max-low":   GPT51CodexMaxLow,
	"gpt-5.1-codex-max-high":  GPT51CodexMaxHigh,

	// GPT 5.2
	"gpt-5.2":                GPT52Medium,
	"gpt-5.2-low":            GPT52Low,
	"gpt-5.2-high":           GPT52High,
	"gpt-5.2-xhigh":          GPT52XHigh,
	"gpt-5.2-none":           GPT52None,
	"gpt-5.2-priority":       GPT52MediumPriority,
	"gpt-5.2-low-priority":   GPT52LowPriority,
	"gpt-5.2-high-priority":  GPT52HighPriority,
	"gpt-5.2-xhigh-priority": GPT52XHighPriority,

	// GPT 5.3
	"gpt-5.3-codex":                GPT53Codex,
	"gpt-5.3-codex-low":            GPT53CodexLow,
	"gpt-5.3-codex-high":           GPT53CodexHigh,
	"gpt-5.3-codex-xhigh":          GPT53CodexXHigh,
	"gpt-5.3-codex-spark":          GPT53CodexSpark,
	"gpt-5.3-codex-priority":       GPT53CodexPriority,
	"gpt-5.3-codex-low-priority":   GPT53CodexLowPriority,
	"gpt-5.3-codex-high-priority":  GPT53CodexHighPriority,
	"gpt-5.3-codex-xhigh-priority": GPT53CodexXHighPriority,

	// GPT 5.4
	"gpt-5.4":                 GPT54Medium,
	"gpt-5.4-none":            GPT54None,
	"gpt-5.4-low":             GPT54Low,
	"gpt-5.4-medium":          GPT54Medium,
	"gpt-5.4-high":            GPT54High,
	"gpt-5.4-xhigh":           GPT54XHigh,
	"gpt-5.4-none-priority":   GPT54NonePriority,
	"gpt-5.4-low-priority":    GPT54LowPriority,
	"gpt-5.4-medium-priority": GPT54MediumPriority,
	"gpt-5.4-high-priority":   GPT54HighPriority,
	"gpt-5.4-xhigh-priority":  GPT54XHighPriority,
	"gpt-5.4-mini":            GPT54MiniLow,
	"gpt-5.4-mini-low":        GPT54MiniLow,
	"gpt-5.4-mini-medium":     GPT54MiniMedium,
	"gpt-5.4-mini-high":       GPT54MiniHigh,
	"gpt-5.4-mini-xhigh":      GPT54MiniXHigh,

	// O-Series
	"o3":           O3,
	"o3-mini":      O3Mini,
	"o3-low":       O3Low,
	"o3-high":      O3High,
	"o3-pro":       O3Pro,
	"o3-pro-low":   O3ProLow,
	"o3-pro-high":  O3ProHigh,
	"o4-mini":      O4Mini,
	"o4-mini-low":  O4MiniLow,
	"o4-mini-high": O4MiniHigh,

	// Gemini
	"gemini-2.0-flash":          Gemini20Flash,
	"gemini-2.5-pro":            Gemini25Pro,
	"gemini-2.5-flash":          Gemini25Flash,
	"gemini-2.5-flash-thinking": Gemini25FlashThinking,
	"gemini-2.5-flash-lite":     Gemini25FlashLite,
	"gemini-3.0-pro":            Gemini30ProMedium,
	"gemini-3.0-pro-low":        Gemini30ProLow,
	"gemini-3.0-pro-high":       Gemini30ProHigh,
	"gemini-3.0-pro-minimal":    Gemini30ProMinimal,
	"gemini-3.0-pro-medium":     Gemini30ProMedium,
	"gemini-3.0-flash":          Gemini30FlashMedium,
	"gemini-3.0-flash-minimal":  Gemini30FlashMinimal,
	"gemini-3.0-flash-low":      Gemini30FlashLow,
	"gemini-3.0-flash-medium":   Gemini30FlashMedium,
	"gemini-3.0-flash-high":     Gemini30FlashHigh,
	"gemini-3.1-pro":            Gemini31ProLow,
	"gemini-3.1-pro-low":        Gemini31ProLow,
	"gemini-3.1-pro-high":       Gemini31ProHigh,

	// DeepSeek
	"deepseek-v3":      DeepSeekV3,
	"deepseek-r1":      DeepSeekR1,
	"deepseek-r1-fast": DeepSeekR1Fast,
	"deepseek-r1-slow": DeepSeekR1Slow,
	"deepseek-v3-2":    DeepSeekV32,

	// Llama
	"llama-3.1-8b":     Llama318B,
	"llama-3.1-70b":    Llama3170B,
	"llama-3.1-405b":   Llama31405B,
	"llama-3.3-70b":    Llama3370B,
	"llama-3.3-70b-r1": Llama3370BR1,

	// Qwen
	"qwen-2.5-7b":            Qwen257B,
	"qwen-2.5-32b":           Qwen2532B,
	"qwen-2.5-72b":           Qwen2572B,
	"qwen-3-235b":            Qwen3235B,
	"qwen-3-coder-480b":      Qwen3Coder480B,
	"qwen-3-coder-480b-fast": Qwen3Coder480BFast,
	"qwen-2.5-32b-r1":        Qwen2532BR1,

	// Grok
	"grok-2":         Grok2,
	"grok-3":         Grok3,
	"grok-3-mini":    Grok3Mini,
	"grok-code-fast": GrokCodeFast,

	// Other
	"mistral-7b":       Mistral7B,
	"kimi-k2":          KimiK2,
	"kimi-k2-thinking": KimiK2Thinking,
	"glm-4.5":          GLM45,
	"glm-4.5-fast":     GLM45Fast,
	"glm-4.6":          GLM46,
	"glm-4.6-fast":     GLM46Fast,
	"glm-4.7":          GLM47,
	"glm-4.7-fast":     GLM47Fast,
	"minimax-m2":       MiniMaxM2,
	"minimax-m2.1":     MiniMaxM21,
	"swe-1.5":          SWE15,
	"swe-1.5-thinking": SWE15Thinking,
	"swe-1.5-slow":     SWE15Slow,
	"swe-1.6":          SWE16,
	"swe-1.6-fast":     SWE16Fast,
}

// ModelNameToUID maps model names to Windsurf model UID for Cascade session.
var ModelNameToUID = map[string]string{
	// Claude (MODEL_* format)
	"claude-3-opus":              "MODEL_CLAUDE_3_OPUS_20240229",
	"claude-3-sonnet":            "MODEL_CLAUDE_3_SONNET_20240229",
	"claude-3-haiku":             "MODEL_CLAUDE_3_HAIKU_20240307",
	"claude-3.5-sonnet":          "MODEL_CLAUDE_3_5_SONNET_20241022",
	"claude-3.5-haiku":           "MODEL_CLAUDE_3_5_HAIKU_20241022",
	"claude-3.7-sonnet":          "MODEL_CLAUDE_3_7_SONNET_20250219",
	"claude-3.7-sonnet-thinking": "MODEL_CLAUDE_3_7_SONNET_20250219_THINKING",
	"claude-4-opus":              "MODEL_CLAUDE_4_OPUS",
	"claude-4-opus-thinking":     "MODEL_CLAUDE_4_OPUS_THINKING",
	"claude-4-sonnet":            "MODEL_CLAUDE_4_SONNET",
	"claude-4-sonnet-thinking":   "MODEL_CLAUDE_4_SONNET_THINKING",
	"claude-4.1-opus":            "MODEL_CLAUDE_4_1_OPUS",
	"claude-4.1-opus-thinking":   "MODEL_CLAUDE_4_1_OPUS_THINKING",
	"claude-4.5-sonnet":          "MODEL_CLAUDE_4_5_SONNET",
	"claude-4.5-sonnet-thinking": "MODEL_CLAUDE_4_5_SONNET_THINKING",
	"claude-4.5-opus":            "MODEL_CLAUDE_4_5_OPUS",
	"claude-4.5-opus-thinking":   "MODEL_CLAUDE_4_5_OPUS_THINKING",
	"claude-code":                "MODEL_CLAUDE_CODE",

	// Claude 4.6 (dash-format UIDs)
	"claude-4.6-opus":               "claude-opus-4-6",
	"claude-4.6-opus-thinking":      "claude-opus-4-6-thinking",
	"claude-4.6-sonnet":             "claude-sonnet-4-6",
	"claude-4.6-sonnet-thinking":    "claude-sonnet-4-6-thinking",
	"claude-4.6-sonnet-1m":          "claude-sonnet-4-6-1m",
	"claude-4.6-sonnet-thinking-1m": "claude-sonnet-4-6-thinking-1m",

	// Claude 4.7 (dash-format UIDs, reasoning effort variants; native 1M context)
	"claude-4.7-opus":        "claude-opus-4-7-low",
	"claude-4.7-opus-low":    "claude-opus-4-7-low",
	"claude-4.7-opus-medium": "claude-opus-4-7-medium",
	"claude-4.7-opus-high":   "claude-opus-4-7-high",
	"claude-4.7-opus-xhigh":  "claude-opus-4-7-xhigh",
	"claude-4.7-opus-max":    "claude-opus-4-7-max",

	// GPT
	"gpt-4":        "MODEL_CHAT_GPT_4",
	"gpt-4-turbo":  "MODEL_CHAT_GPT_4_1106_PREVIEW",
	"gpt-4o":       "MODEL_CHAT_GPT_4O_2024_08_06",
	"gpt-4o-mini":  "MODEL_CHAT_GPT_4O_MINI_2024_07_18",
	"gpt-4.1":      "MODEL_CHAT_GPT_4_1_2025_04_14",
	"gpt-4.1-mini": "MODEL_CHAT_GPT_4_1_MINI_2025_04_14",
	"gpt-4.1-nano": "MODEL_CHAT_GPT_4_1_NANO_2025_04_14",
	"gpt-5":        "MODEL_CHAT_GPT_5",
	"gpt-5-nano":   "MODEL_GPT_5_NANO",
	"gpt-5-low":    "MODEL_CHAT_GPT_5_LOW",
	"gpt-5-high":   "MODEL_CHAT_GPT_5_HIGH",
	"gpt-5-codex":  "MODEL_CHAT_GPT_5_CODEX",

	// GPT 5.1 Codex
	"gpt-5.1":                 "gpt-5.1",
	"gpt-5.1-codex-mini":      "MODEL_GPT_5_1_CODEX_MINI_MEDIUM",
	"gpt-5.1-codex-mini-low":  "MODEL_GPT_5_1_CODEX_MINI_LOW",
	"gpt-5.1-codex-mini-high": "MODEL_GPT_5_1_CODEX_MINI_HIGH",
	"gpt-5.1-codex":           "MODEL_GPT_5_1_CODEX_MEDIUM",
	"gpt-5.1-codex-low":       "MODEL_GPT_5_1_CODEX_LOW",
	"gpt-5.1-codex-high":      "MODEL_GPT_5_1_CODEX_HIGH",
	"gpt-5.1-codex-max":       "MODEL_GPT_5_1_CODEX_MAX_MEDIUM",
	"gpt-5.1-codex-max-low":   "MODEL_GPT_5_1_CODEX_MAX_LOW",
	"gpt-5.1-codex-max-high":  "MODEL_GPT_5_1_CODEX_MAX_HIGH",

	// GPT 5.2 (dash-format UIDs)
	"gpt-5.2":                 "MODEL_GPT_5_2_MEDIUM",
	"gpt-5.2-low":             "MODEL_GPT_5_2_LOW",
	"gpt-5.2-medium":          "MODEL_GPT_5_2_MEDIUM",
	"gpt-5.2-high":            "MODEL_GPT_5_2_HIGH",
	"gpt-5.2-xhigh":           "MODEL_GPT_5_2_XHIGH",
	"gpt-5.2-none":            "MODEL_GPT_5_2_NONE",
	"gpt-5.2-none-priority":   "MODEL_GPT_5_2_NONE_PRIORITY",
	"gpt-5.2-low-priority":    "MODEL_GPT_5_2_LOW_PRIORITY",
	"gpt-5.2-medium-priority": "MODEL_GPT_5_2_MEDIUM_PRIORITY",
	"gpt-5.2-high-priority":   "MODEL_GPT_5_2_HIGH_PRIORITY",
	"gpt-5.2-xhigh-priority":  "MODEL_GPT_5_2_XHIGH_PRIORITY",
	"gpt-5.2-priority":        "MODEL_GPT_5_2_MEDIUM_PRIORITY",
	"gpt-5.2-codex":           "gpt-5.2-codex",

	// GPT 5.3 (dash-format UIDs)
	"gpt-5.3-codex":                "gpt-5-3-codex-medium",
	"gpt-5.3-codex-low":            "gpt-5-3-codex-low",
	"gpt-5.3-codex-high":           "gpt-5-3-codex-high",
	"gpt-5.3-codex-xhigh":          "gpt-5-3-codex-xhigh",
	"gpt-5.3-codex-spark":          "gpt-5-3-codex-spark-medium",
	"gpt-5.3-codex-priority":       "gpt-5-3-codex-medium-priority",
	"gpt-5.3-codex-low-priority":   "gpt-5-3-codex-low-priority",
	"gpt-5.3-codex-high-priority":  "gpt-5-3-codex-high-priority",
	"gpt-5.3-codex-xhigh-priority": "gpt-5-3-codex-xhigh-priority",

	// GPT 5.4 (dash-format UIDs)
	"gpt-5.4":                 "gpt-5-4-low",
	"gpt-5.4-none":            "gpt-5-4-none",
	"gpt-5.4-low":             "gpt-5-4-low",
	"gpt-5.4-medium":          "gpt-5-4-medium",
	"gpt-5.4-high":            "gpt-5-4-high",
	"gpt-5.4-xhigh":           "gpt-5-4-xhigh",
	"gpt-5.4-none-priority":   "gpt-5-4-none-priority",
	"gpt-5.4-low-priority":    "gpt-5-4-low-priority",
	"gpt-5.4-medium-priority": "gpt-5-4-medium-priority",
	"gpt-5.4-high-priority":   "gpt-5-4-high-priority",
	"gpt-5.4-xhigh-priority":  "gpt-5-4-xhigh-priority",
	"gpt-5.4-mini":            "gpt-5-4-mini-low",
	"gpt-5.4-mini-low":        "gpt-5-4-mini-low",
	"gpt-5.4-mini-medium":     "gpt-5-4-mini-medium",
	"gpt-5.4-mini-high":       "gpt-5-4-mini-high",
	"gpt-5.4-mini-xhigh":      "gpt-5-4-mini-xhigh",

	// O-Series
	"o3":           "MODEL_CHAT_O3",
	"o3-mini":      "MODEL_O3_MINI",
	"o3-low":       "MODEL_O3_LOW",
	"o3-high":      "MODEL_O3_HIGH",
	"o3-pro":       "MODEL_O3_PRO_2025_06_10",
	"o3-pro-low":   "MODEL_O3_PRO_2025_06_10_LOW",
	"o3-pro-high":  "MODEL_O3_PRO_2025_06_10_HIGH",
	"o4-mini":      "MODEL_CHAT_O4_MINI",
	"o4-mini-low":  "MODEL_CHAT_O4_MINI_LOW",
	"o4-mini-high": "MODEL_CHAT_O4_MINI_HIGH",

	// Gemini
	"gemini-2.0-flash":          "MODEL_GOOGLE_GEMINI_2_0_FLASH",
	"gemini-2.5-pro":            "MODEL_GOOGLE_GEMINI_2_5_PRO",
	"gemini-2.5-flash":          "MODEL_GOOGLE_GEMINI_2_5_FLASH",
	"gemini-2.5-flash-thinking": "MODEL_GOOGLE_GEMINI_2_5_FLASH_THINKING",
	"gemini-2.5-flash-lite":     "MODEL_GOOGLE_GEMINI_2_5_FLASH_LITE",

	// Gemini 3.0 (MODEL_* format)
	"gemini-3.0-pro":           "MODEL_GOOGLE_GEMINI_3_0_PRO_MEDIUM",
	"gemini-3.0-pro-low":       "MODEL_GOOGLE_GEMINI_3_0_PRO_LOW",
	"gemini-3.0-pro-high":      "MODEL_GOOGLE_GEMINI_3_0_PRO_HIGH",
	"gemini-3.0-pro-minimal":   "MODEL_GOOGLE_GEMINI_3_0_PRO_MINIMAL",
	"gemini-3.0-pro-medium":    "MODEL_GOOGLE_GEMINI_3_0_PRO_MEDIUM",
	"gemini-3.0-flash":         "MODEL_GOOGLE_GEMINI_3_0_FLASH_MEDIUM",
	"gemini-3.0-flash-minimal": "MODEL_GOOGLE_GEMINI_3_0_FLASH_MINIMAL",
	"gemini-3.0-flash-low":     "MODEL_GOOGLE_GEMINI_3_0_FLASH_LOW",
	"gemini-3.0-flash-medium":  "MODEL_GOOGLE_GEMINI_3_0_FLASH_MEDIUM",
	"gemini-3.0-flash-high":    "MODEL_GOOGLE_GEMINI_3_0_FLASH_HIGH",

	// Gemini 3.1 (dash-format UIDs)
	"gemini-3.1-pro":      "gemini-3-1-pro-low",
	"gemini-3.1-pro-low":  "gemini-3-1-pro-low",
	"gemini-3.1-pro-high": "gemini-3-1-pro-high",

	// DeepSeek
	"deepseek-v3":      "MODEL_DEEPSEEK_V3",
	"deepseek-r1":      "MODEL_DEEPSEEK_R1",
	"deepseek-r1-fast": "MODEL_DEEPSEEK_R1_FAST",
	"deepseek-r1-slow": "MODEL_DEEPSEEK_R1_SLOW",
	"deepseek-v3-2":    "MODEL_DEEPSEEK_V3_2",

	// Llama
	"llama-3.1-8b":     "MODEL_LLAMA_3_1_8B_INSTRUCT",
	"llama-3.1-70b":    "MODEL_LLAMA_3_1_70B_INSTRUCT",
	"llama-3.1-405b":   "MODEL_LLAMA_3_1_405B_INSTRUCT",
	"llama-3.3-70b":    "MODEL_LLAMA_3_3_70B_INSTRUCT",
	"llama-3.3-70b-r1": "MODEL_LLAMA_3_3_70B_INSTRUCT_R1",

	// Qwen
	"qwen-2.5-7b":            "MODEL_QWEN_2_5_7B_INSTRUCT",
	"qwen-2.5-32b":           "MODEL_QWEN_2_5_32B_INSTRUCT",
	"qwen-2.5-72b":           "MODEL_QWEN_2_5_72B_INSTRUCT",
	"qwen-3-235b":            "MODEL_QWEN_3_235B_INSTRUCT",
	"qwen-3-coder-480b":      "MODEL_QWEN_3_CODER_480B_INSTRUCT",
	"qwen-3-coder-480b-fast": "MODEL_QWEN_3_CODER_480B_INSTRUCT_FAST",
	"qwen-2.5-32b-r1":        "MODEL_QWEN_2_5_32B_INSTRUCT_R1",

	// Grok
	"grok-2":         "MODEL_XAI_GROK_2",
	"grok-3":         "MODEL_XAI_GROK_3",
	"grok-3-mini":    "MODEL_XAI_GROK_3_MINI_REASONING",
	"grok-code-fast": "MODEL_XAI_GROK_CODE_FAST",

	// Other
	"kimi-k2":          "MODEL_KIMI_K2",
	"kimi-k2-thinking": "MODEL_KIMI_K2_THINKING",
	"kimi-k2.5":        "kimi-k2-5",
	"glm-4.5-fast":     "MODEL_GLM_4_5_FAST",
	"glm-4.6":          "glm-4-6",
	"glm-4.6-fast":     "glm-4-6-fast",
	"glm-4.7":          "glm-4-7",
	"glm-4.7-fast":     "glm-4-7-fast",
	"glm-5.1":          "glm-5-1",
	"swe-1.5":          "MODEL_SWE_1_5",
	"swe-1.5-thinking": "MODEL_SWE_1_5_THINKING",
	"swe-1.5-slow":     "MODEL_SWE_1_5_SLOW",
	"swe-1.6":          "swe-1-6",
	"swe-1.6-fast":     "swe-1-6-fast",
}

// ResolvedModel represents a resolved model with enum and UID.
type ResolvedModel struct {
	EnumValue ModelEnum
	ModelID   string
	ModelName string
	ModelUID  string
}

// ResolveModel resolves an OpenAI-style model name to Windsurf enum + UID.
func ResolveModel(modelName string) ResolvedModel {
	normalized := normalizeModelName(modelName)

	enum, hasEnum := ModelNameToEnum[normalized]
	uid, hasUID := ModelNameToUID[normalized]

	if !hasEnum && !hasUID {
		// Fallback to claude-3.5-sonnet
		return ResolvedModel{
			EnumValue: Claude35SonnetNew,
			ModelID:   "claude-3.5-sonnet",
			ModelName: "claude-3.5-sonnet",
			ModelUID:  "MODEL_CLAUDE_3_5_SONNET_20241022",
		}
	}

	return ResolvedModel{
		EnumValue: enum,
		ModelID:   normalized,
		ModelName: normalized,
		ModelUID:  uid,
	}
}

// GetModelUID returns the Windsurf model UID for Cascade session.
func GetModelUID(modelName string) string {
	return ModelNameToUID[normalizeModelName(modelName)]
}

// IsModelSupported checks if a model name is supported.
func IsModelSupported(modelName string) bool {
	normalized := normalizeModelName(modelName)
	_, hasEnum := ModelNameToEnum[normalized]
	_, hasUID := ModelNameToUID[normalized]
	return hasEnum || hasUID
}

// GetSupportedModels returns sorted list of all supported model names.
func GetSupportedModels() []string {
	models := make(map[string]bool)
	for name := range ModelNameToEnum {
		models[name] = true
	}
	for name := range ModelNameToUID {
		models[name] = true
	}

	result := make([]string, 0, len(models))
	for name := range models {
		result = append(result, name)
	}
	return result
}

func normalizeModelName(name string) string {
	// Simple normalization: lowercase, trim spaces
	result := strings.ToLower(strings.TrimSpace(name))
	return result
}
