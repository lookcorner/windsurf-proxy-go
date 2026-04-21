// Package tokenizer provides token counting utilities.
package tokenizer

import (
	"strings"
	"unicode"
)

// Model encoding mapping (similar to tiktoken encodings)
// Different models use different tokenization strategies
var modelEncodings = map[string]string{
	"gpt-":     "o200k_base", // GPT-4o uses o200k
	"o3":       "o200k_base",
	"o4":       "o200k_base",
	"claude-":  "cl100k_base", // Claude uses cl100k
	"gemini-":  "cl100k_base",
	"deepseek": "cl100k_base",
	"qwen-":    "cl100k_base",
	"grok-":    "cl100k_base",
	"kimi-":    "cl100k_base",
	"glm-":     "cl100k_base",
	"llama-":   "cl100k_base",
	"minimax-": "cl100k_base",
}

const defaultEncoding = "cl100k_base"

// Message overhead tokens (role + delimiters)
const msgOverhead = 4
const replyOverhead = 3

// GetEncoding returns the encoding name for a model.
func GetEncoding(model string) string {
	modelLower := strings.ToLower(model)
	for prefix, enc := range modelEncodings {
		if strings.HasPrefix(modelLower, prefix) {
			return enc
		}
	}
	return defaultEncoding
}

// charEstimate estimates tokens based on character count.
// Rough estimate: ~4 characters per token for English, ~2 for CJK.
func charEstimate(text string) int {
	if text == "" {
		return 0
	}

	cjkCount := 0
	for _, c := range text {
		if c >= '\u4e00' && c <= '\u9fff' {
			cjkCount++
		}
	}

	nonCJK := len(text) - cjkCount
	result := (nonCJK / 4) + (cjkCount / 2)
	if result < 1 {
		return 1
	}
	return result
}

// CountTextTokens counts tokens in a text string.
// Uses character estimation since we don't have tiktoken in Go.
// For more accurate counting, use an external tokenizer library.
func CountTextTokens(text string, model string) int {
	if text == "" {
		return 0
	}

	// Character-based estimation
	// cl100k_base and o200k_base have similar ratios
	return charEstimate(text)
}

// CountMessagesTokens counts tokens in a list of chat messages.
// Each message has overhead for role + delimiters (~4 tokens).
// Plus ~3 tokens for reply priming.
func CountMessagesTokens(messages []map[string]string, model string) int {
	total := 0

	for _, msg := range messages {
		total += msgOverhead

		content := msg["content"]
		role := msg["role"]

		total += CountTextTokens(content, model)
		total += CountTextTokens(role, model)
	}

	total += replyOverhead
	return total
}

// EstimatePromptTokens estimates prompt tokens from request.
func EstimatePromptTokens(messages []map[string]interface{}) int {
	total := 0

	for _, msg := range messages {
		total += msgOverhead

		content := extractContent(msg["content"])
		role, _ := msg["role"].(string)

		total += charEstimate(content)
		total += charEstimate(role)
	}

	total += replyOverhead
	return total
}

// EstimateCompletionTokens estimates completion tokens from response.
func EstimateCompletionTokens(text string) int {
	return charEstimate(text)
}

// extractContent extracts text content from OpenAI message content.
func extractContent(content interface{}) string {
	if content == nil {
		return ""
	}

	if str, ok := content.(string); ok {
		return str
	}

	if arr, ok := content.([]interface{}); ok {
		parts := []string{}
		for _, p := range arr {
			if pMap, ok := p.(map[string]interface{}); ok {
				if pMap["type"] == "text" {
					if text, ok := pMap["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}

	return ""
}

// IsCJK checks if a character is Chinese/Japanese/Korean.
func IsCJK(c rune) bool {
	return c >= '\u4e00' && c <= '\u9fff' ||
		c >= '\u3040' && c <= '\u30ff' || // Japanese Hiragana/Katakana
		c >= '\uac00' && c <= '\ud7a3' // Korean Hangul
}

// WordCount estimates word count for comparison.
func WordCount(text string) int {
	if text == "" {
		return 0
	}

	words := 0
	inWord := false

	for _, c := range text {
		if unicode.IsSpace(c) {
			inWord = false
		} else if !inWord {
			words++
			inWord = true
		}
	}

	return words
}