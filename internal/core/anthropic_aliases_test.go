package core

import "testing"

func TestResolveAnthropicAliasStripsContextSuffix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Claude Code's "Sonnet 4.6 (1M context)" sends this form; the
		// "[1m]" hint must be stripped before alias lookup, otherwise
		// the proxy returns 400 ("model not supported") and Claude Code
		// shows "model may not exist or you may not have access".
		{"claude-sonnet-4-6[1m]", "claude-4.6-sonnet"},
		{"claude-sonnet-4-6[200k]", "claude-4.6-sonnet"},
		{"  Claude-Sonnet-4-6[1m] ", "claude-4.6-sonnet"},

		// Internal canonical names with a context hint should still
		// resolve to the internal name (no alias entry, but the suffix
		// stripping still applies).
		{"claude-4.6-sonnet[1m]", "claude-4.6-sonnet"},

		// Plain alias path keeps working.
		{"claude-sonnet-4-6", "claude-4.6-sonnet"},
		{"claude-3-5-sonnet-latest", "claude-3.5-sonnet"},

		// Unknown model — pass through (lower/trim only).
		{"some-future-model", "some-future-model"},
	}

	for _, tc := range cases {
		got := ResolveAnthropicAlias(tc.in)
		if got != tc.want {
			t.Errorf("ResolveAnthropicAlias(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStripContextSuffix(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"foo[1m]", "foo"},
		{"foo", "foo"},
		{"foo[bar][baz]", "foo[bar]"}, // only trims the trailing bracket pair
		{"[only]", "[only]"},          // index 0 is not > 0, leave alone
		{"", ""},
	}
	for _, tc := range cases {
		got := stripContextSuffix(tc.in)
		if got != tc.want {
			t.Errorf("stripContextSuffix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
