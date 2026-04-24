// Package auth provides Firebase authentication for Windsurf.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const authRequestTimeout = 15 * time.Second

// commonHeaders returns common headers for Firebase requests.
func commonHeaders() map[string]string {
	return map[string]string{
		"Content-Type":     "application/json",
		"Referer":          "https://windsurf.com/",
		"X-Client-Version": "Chrome/JsCore/11.0.0/FirebaseCore-web",
		"X-Firebase-gmpid": FirebaseAppID,
		"User-Agent":       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	}
}

// FirebaseSignIn signs in with email + password via Firebase Identity Toolkit.
func FirebaseSignIn(ctx context.Context, email string, password string) (*FirebaseTokens, error) {
	return FirebaseSignInWithProxy(ctx, email, password, "")
}

// FirebaseSignInWithProxy signs in with email + password via Firebase Identity Toolkit using
// the provided outbound proxy when set.
func FirebaseSignInWithProxy(ctx context.Context, email string, password string, proxyURL string) (*FirebaseTokens, error) {
	body := map[string]interface{}{
		"returnSecureToken": true,
		"email":             email,
		"password":          password,
		"clientType":        "CLIENT_TYPE_WEB",
	}

	resp, err := doRequest(ctx, FirebaseSignInURL, body, proxyURL)
	if err != nil {
		return nil, err
	}

	var data map[string]interface{}
	if err := json.Unmarshal(resp, &data); err != nil {
		return nil, err
	}

	// Check for error
	if data["error"] != nil {
		errData := data["error"].(map[string]interface{})
		msg := errData["message"].(string)
		errorMap := map[string]string{
			"EMAIL_NOT_FOUND":             "邮箱不存在",
			"INVALID_PASSWORD":            "密码错误",
			"USER_DISABLED":               "账号已被禁用",
			"INVALID_LOGIN_CREDENTIALS":   "邮箱或密码错误",
			"TOO_MANY_ATTEMPTS_TRY_LATER": "尝试次数过多，请稍后再试",
		}
		if mapped, ok := errorMap[msg]; ok {
			return nil, errors.New(mapped)
		}
		return nil, fmt.Errorf("Firebase error: %s", msg)
	}

	// Extract tokens
	idToken, _ := data["idToken"].(string)
	refreshToken, _ := data["refreshToken"].(string)
	expiresIn := 3600
	if exp, ok := data["expiresIn"].(float64); ok {
		expiresIn = int(exp)
	} else if exp, ok := data["expiresIn"].(string); ok {
		var f float64
		json.Unmarshal([]byte(exp), &f)
		expiresIn = int(f)
	}
	emailResp, _ := data["email"].(string)

	return &FirebaseTokens{
		IDToken:      idToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresIn,
		Email:        emailResp,
	}, nil
}

// RegisterWindsurfUser exchanges Firebase idToken for Windsurf service API key.
func RegisterWindsurfUser(ctx context.Context, idToken string) (*WindsurfServiceAuth, error) {
	return RegisterWindsurfUserWithProxy(ctx, idToken, "")
}

// RegisterWindsurfUserWithProxy exchanges Firebase idToken for Windsurf service API key using
// the provided outbound proxy when set.
func RegisterWindsurfUserWithProxy(ctx context.Context, idToken string, proxyURL string) (*WindsurfServiceAuth, error) {
	body := map[string]interface{}{
		"firebase_id_token": idToken,
	}

	resp, err := doRequest(ctx, RegisterUserURL, body, proxyURL)
	if err != nil {
		return nil, err
	}

	var data map[string]interface{}
	if err := json.Unmarshal(resp, &data); err != nil {
		return nil, err
	}

	// Check for error
	if data["api_key"] == nil {
		if data["message"] != nil {
			return nil, fmt.Errorf("RegisterUser error: %s", data["message"])
		}
		return nil, fmt.Errorf("RegisterUser failed: no api_key in response")
	}

	apiKey, _ := data["api_key"].(string)
	name, _ := data["name"].(string)
	apiServerURL, _ := data["api_server_url"].(string)
	if apiServerURL == "" {
		apiServerURL = DefaultAPIServerURL
	}

	return &WindsurfServiceAuth{
		APIKey:       apiKey,
		Name:         name,
		APIServerURL: apiServerURL,
	}, nil
}

// RefreshFirebaseToken refreshes a Firebase idToken using a refresh token.
func RefreshFirebaseToken(ctx context.Context, refreshToken string) (*FirebaseTokens, error) {
	return RefreshFirebaseTokenWithProxy(ctx, refreshToken, "")
}

// RefreshFirebaseTokenWithProxy refreshes a Firebase idToken using a refresh token and the
// provided outbound proxy when set.
func RefreshFirebaseTokenWithProxy(ctx context.Context, refreshToken string, proxyURL string) (*FirebaseTokens, error) {
	body := map[string]interface{}{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
	}

	resp, err := doRequest(ctx, FirebaseRefreshURL, body, proxyURL)
	if err != nil {
		return nil, err
	}

	var data map[string]interface{}
	if err := json.Unmarshal(resp, &data); err != nil {
		return nil, err
	}

	idToken, _ := data["id_token"].(string)
	if idToken == "" {
		idToken, _ = data["idToken"].(string)
	}
	newRefresh, _ := data["refresh_token"].(string)
	if newRefresh == "" {
		newRefresh, _ = data["refreshToken"].(string)
	}
	if newRefresh == "" {
		newRefresh = refreshToken
	}

	expiresIn := 3600
	if exp, ok := data["expires_in"].(float64); ok {
		expiresIn = int(exp)
	} else if exp, ok := data["expiresIn"].(float64); ok {
		expiresIn = int(exp)
	}
	email, _ := data["email"].(string)

	if idToken == "" {
		return nil, fmt.Errorf("Token refresh failed: no idToken")
	}

	return &FirebaseTokens{
		IDToken:      idToken,
		RefreshToken: newRefresh,
		ExpiresIn:    expiresIn,
		Email:        email,
	}, nil
}

// FullLogin performs the complete login flow: Firebase sign-in → RegisterUser.
func FullLogin(ctx context.Context, email string, password string) (*WindsurfServiceAuth, *FirebaseTokens, error) {
	return FullLoginWithProxy(ctx, email, password, "")
}

// FullLoginWithProxy performs the complete login flow using the provided outbound proxy when set.
func FullLoginWithProxy(ctx context.Context, email string, password string, proxyURL string) (*WindsurfServiceAuth, *FirebaseTokens, error) {
	tokens, err := FirebaseSignInWithProxy(ctx, email, password, proxyURL)
	if err != nil {
		return nil, nil, err
	}

	service, err := RegisterWindsurfUserWithProxy(ctx, tokens.IDToken, proxyURL)
	if err != nil {
		return nil, nil, err
	}

	return service, tokens, nil
}

// doRequest makes an HTTP request with JSON body.
func doRequest(ctx context.Context, url string, body interface{}, proxyURL string) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}

	for k, v := range commonHeaders() {
		req.Header.Set(k, v)
	}

	httpClient, err := clientForProxy(proxyURL)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}

	return data, nil
}

func clientForProxy(proxyURL string) (*http.Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("unexpected default HTTP transport type %T", http.DefaultTransport)
	}

	cloned := transport.Clone()
	proxyURL = strings.TrimSpace(proxyURL)
	switch {
	case proxyURL == "":
		cloned.Proxy = http.ProxyFromEnvironment
	default:
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL %q: %w", proxyURL, err)
		}
		cloned.Proxy = http.ProxyURL(parsed)
	}

	return &http.Client{
		Timeout:   authRequestTimeout,
		Transport: cloned,
	}, nil
}
