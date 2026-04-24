package auth

import (
	"context"
	"net/http"
	"testing"
)

func TestClientForProxyUsesExplicitProxy(t *testing.T) {
	client, err := clientForProxy("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("clientForProxy() error = %v", err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport type = %T, want *http.Transport", client.Transport)
	}

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	proxy, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("transport.Proxy() error = %v", err)
	}
	if proxy == nil || proxy.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy = %v, want http://127.0.0.1:7890", proxy)
	}
}

func TestTokenManagerDoRefreshUsesProxy(t *testing.T) {
	prevRefresh := refreshFirebaseTokenFunc
	prevRegister := registerWindsurfUserFunc
	defer func() {
		refreshFirebaseTokenFunc = prevRefresh
		registerWindsurfUserFunc = prevRegister
	}()

	refreshCalls := 0
	registerCalls := 0

	refreshFirebaseTokenFunc = func(_ context.Context, refreshToken string, proxyURL string) (*FirebaseTokens, error) {
		refreshCalls++
		if refreshToken != "refresh-token" {
			t.Fatalf("refresh token = %q, want refresh-token", refreshToken)
		}
		if proxyURL != "http://127.0.0.1:7890" {
			t.Fatalf("proxyURL = %q, want explicit account proxy", proxyURL)
		}
		return &FirebaseTokens{
			IDToken:      "new-id-token",
			RefreshToken: refreshToken,
			ExpiresIn:    3600,
		}, nil
	}
	registerWindsurfUserFunc = func(_ context.Context, idToken string, proxyURL string) (*WindsurfServiceAuth, error) {
		registerCalls++
		if idToken != "new-id-token" {
			t.Fatalf("idToken = %q, want new-id-token", idToken)
		}
		if proxyURL != "http://127.0.0.1:7890" {
			t.Fatalf("proxyURL = %q, want explicit account proxy", proxyURL)
		}
		return &WindsurfServiceAuth{
			APIKey:       "sk-new",
			APIServerURL: "https://server.example",
		}, nil
	}

	tm := NewTokenManager(
		&FirebaseTokens{RefreshToken: "refresh-token", ExpiresIn: 3600},
		&WindsurfServiceAuth{APIKey: "sk-old", APIServerURL: "https://server.example"},
		"http://127.0.0.1:7890",
		nil,
		nil,
	)

	if err := tm.doRefresh(); err != nil {
		t.Fatalf("doRefresh() error = %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls)
	}
	if registerCalls != 1 {
		t.Fatalf("register calls = %d, want 1", registerCalls)
	}
}
