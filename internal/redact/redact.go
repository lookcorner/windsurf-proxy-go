package redact

import (
	"regexp"
	"strings"
)

var (
	workspacePathRE   = regexp.MustCompile("/tmp/windsurf-workspace(/[^\\s\"'`<>)}\\],*;]*)?")
	optWindsurfPathRE = regexp.MustCompile("/opt/windsurf(/[^\\s\"'`<>)}\\],*;]*)?")
	rootAPIPathRE     = regexp.MustCompile("/root/WindsurfAPI(/[^\\s\"'`<>)}\\],*;]*)?")
	sensitiveLiterals = []string{"/tmp/windsurf-workspace", "/opt/windsurf", "/root/WindsurfAPI"}
)

func SanitizeText(s string) string {
	if s == "" {
		return s
	}
	out := workspacePathRE.ReplaceAllString(s, ".$1")
	out = optWindsurfPathRE.ReplaceAllString(out, "[internal]")
	out = rootAPIPathRE.ReplaceAllString(out, "[internal]")
	return out
}

type PathSanitizer struct {
	buffer string
}

func NewPathSanitizer() *PathSanitizer {
	return &PathSanitizer{}
}

func (p *PathSanitizer) Feed(delta string) string {
	if delta == "" {
		return ""
	}
	p.buffer += delta
	cut := p.safeCutPoint()
	if cut == 0 {
		return ""
	}
	safe := p.buffer[:cut]
	p.buffer = p.buffer[cut:]
	return SanitizeText(safe)
}

func (p *PathSanitizer) Flush() string {
	if p.buffer == "" {
		return ""
	}
	out := SanitizeText(p.buffer)
	p.buffer = ""
	return out
}

func (p *PathSanitizer) safeCutPoint() int {
	buf := p.buffer
	cut := len(buf)

	for _, lit := range sensitiveLiterals {
		searchFrom := 0
		for searchFrom < len(buf) {
			idx := strings.Index(buf[searchFrom:], lit)
			if idx == -1 {
				break
			}
			idx += searchFrom
			end := idx + len(lit)
			for end < len(buf) && isPathBodyChar(buf[end]) {
				end++
			}
			if end == len(buf) {
				if idx < cut {
					cut = idx
				}
				break
			}
			searchFrom = end + 1
		}
	}

	for _, lit := range sensitiveLiterals {
		maxLen := len(lit) - 1
		if maxLen > len(buf) {
			maxLen = len(buf)
		}
		for plen := maxLen; plen > 0; plen-- {
			if strings.HasSuffix(buf, lit[:plen]) {
				start := len(buf) - plen
				if start < cut {
					cut = start
				}
				break
			}
		}
	}

	return cut
}

func isPathBodyChar(b byte) bool {
	switch b {
	case ' ', '\n', '\r', '\t', '"', '\'', '`', '<', '>', ')', '}', ']', ',', '*', ';':
		return false
	default:
		return true
	}
}
