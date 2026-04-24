package management

import "testing"

func TestShouldShowLogFiltersToolParseDebug(t *testing.T) {
	if ShouldShowLog(`[Tool parse] structured response parsed: action=final tool_calls=0 content_len=10 prefix_len=42`) {
		t.Fatalf("expected tool parse debug logs to be filtered from user log stream")
	}
}

func TestTranslateLogMessageKeepsCascadeStabilizedMeaning(t *testing.T) {
	got := TranslateLogMessage(`[Cascade] content stabilized after 8 polls, ending session`)
	if got != `内容已稳定，结束会话 after 8 polls, ending session` {
		t.Fatalf("TranslateLogMessage() = %q", got)
	}
}
