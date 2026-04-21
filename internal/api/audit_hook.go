package api

import (
	"bytes"
	"io"
	"net"
	"net/http"

	"windsurf-proxy-go/internal/audit"
)

// startAudit reads and buffers the incoming request body, attaches a
// fresh audit.Entry to the request context, and returns:
//
//   - the cached body bytes (also reinstated on r.Body so subsequent
//     handlers can decode from it normally);
//   - the modified *http.Request carrying the audit entry on its
//     context;
//   - the *audit.Entry itself, on which the caller will populate model,
//     stream flag, response body, and ultimately Finish.
//
// If the body is unreadable the entry still gets created with an empty
// request_body — auditing should never break the actual request.
func startAudit(protocol string, w http.ResponseWriter, r *http.Request) ([]byte, *http.Request, *audit.Entry) {
	entry := audit.New(protocol, r.URL.Path, clientIP(r))

	body, err := io.ReadAll(r.Body)
	if err == nil {
		entry.SetRequestBody(body)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	r = r.WithContext(audit.WithEntry(r.Context(), entry))
	return body, r, entry
}

// clientIP returns the best-effort client IP from the request. We do
// not consult X-Forwarded-For because the proxy is intended to be
// reached only over loopback or LAN.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
