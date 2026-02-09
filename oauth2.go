package xdk

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth2Config configures OAuth2 PKCE behavior.
type OAuth2Config struct {
	BaseURL              string
	AuthorizationBaseURL string
	ClientID             string
	ClientSecret         string
	RedirectURI          string
	Token                map[string]any
	Scope                []string
	HTTPClient           *http.Client
}

// OAuth2PKCEAuth provides OAuth2 PKCE helper methods.
type OAuth2PKCEAuth struct {
	BaseURL              string
	AuthorizationBaseURL string
	ClientID             string
	ClientSecret         string
	RedirectURI          string
	Scope                string
	Token                map[string]any

	codeVerifier  string
	codeChallenge string
	httpClient    *http.Client
}

func NewOAuth2PKCEAuth(cfg OAuth2Config) *OAuth2PKCEAuth {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	authBase := strings.TrimRight(cfg.AuthorizationBaseURL, "/")
	if authBase == "" {
		authBase = defaultAuthorizationURL
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	return &OAuth2PKCEAuth{
		BaseURL:              baseURL,
		AuthorizationBaseURL: authBase,
		ClientID:             cfg.ClientID,
		ClientSecret:         cfg.ClientSecret,
		RedirectURI:          cfg.RedirectURI,
		Scope:                strings.Join(cfg.Scope, " "),
		Token:                cfg.Token,
		httpClient:           hc,
	}
}

func (o *OAuth2PKCEAuth) SetPKCEParameters(codeVerifier, codeChallenge string) {
	o.codeVerifier = codeVerifier
	if codeChallenge != "" {
		o.codeChallenge = codeChallenge
		return
	}
	o.codeChallenge = generateCodeChallenge(codeVerifier)
}

func (o *OAuth2PKCEAuth) GetAuthorizationURL(state string) (string, error) {
	if o.ClientID == "" {
		return "", errors.New("client_id is required")
	}
	if o.codeVerifier == "" || o.codeChallenge == "" {
		o.codeVerifier = generateCodeVerifier(96)
		o.codeChallenge = generateCodeChallenge(o.codeVerifier)
	}

	u, err := url.Parse(o.AuthorizationBaseURL + "/oauth2/authorize")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", o.ClientID)
	if o.RedirectURI != "" {
		q.Set("redirect_uri", o.RedirectURI)
	}
	if o.Scope != "" {
		q.Set("scope", o.Scope)
	}
	if state != "" {
		q.Set("state", state)
	}
	q.Set("code_challenge", o.codeChallenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (o *OAuth2PKCEAuth) ExchangeCode(ctx context.Context, code, codeVerifier string) (map[string]any, error) {
	if codeVerifier == "" {
		codeVerifier = o.codeVerifier
	}
	if codeVerifier == "" {
		return nil, errors.New("code_verifier is required")
	}
	if o.ClientID == "" {
		return nil, errors.New("client_id is required")
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	if o.RedirectURI != "" {
		form.Set("redirect_uri", o.RedirectURI)
	}
	form.Set("code_verifier", codeVerifier)
	if o.ClientSecret == "" {
		form.Set("client_id", o.ClientID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/2/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if o.ClientSecret != "" {
		req.SetBasicAuth(o.ClientID, o.ClientSecret)
	}

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token exchange failed: status=%d body=%s", resp.StatusCode, string(b))
	}

	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}

	o.Token = map[string]any{
		"access_token":  raw["access_token"],
		"token_type":    raw["token_type"],
		"expires_in":    raw["expires_in"],
		"refresh_token": raw["refresh_token"],
		"scope":         raw["scope"],
	}
	if expiresIn, ok := toInt64(raw["expires_in"]); ok && expiresIn > 0 {
		o.Token["expires_at"] = time.Now().Unix() + expiresIn
	}
	return o.Token, nil
}

func (o *OAuth2PKCEAuth) FetchToken(ctx context.Context, authorizationResponse string) (map[string]any, error) {
	u, err := url.Parse(authorizationResponse)
	if err != nil {
		return nil, err
	}
	code := u.Query().Get("code")
	if code == "" {
		return nil, errors.New("no authorization code in callback URL")
	}
	return o.ExchangeCode(ctx, code, "")
}

func (o *OAuth2PKCEAuth) RefreshToken(ctx context.Context) (map[string]any, error) {
	if o.Token == nil {
		return nil, errors.New("no token to refresh")
	}
	refreshToken, _ := o.Token["refresh_token"].(string)
	if refreshToken == "" {
		return nil, errors.New("refresh_token is missing")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	if o.ClientSecret == "" {
		form.Set("client_id", o.ClientID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/2/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if o.ClientSecret != "" {
		req.SetBasicAuth(o.ClientID, o.ClientSecret)
	}

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token refresh failed: status=%d body=%s", resp.StatusCode, string(b))
	}

	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	o.Token = raw
	if expiresIn, ok := toInt64(raw["expires_in"]); ok && expiresIn > 0 {
		o.Token["expires_at"] = time.Now().Unix() + expiresIn
	}
	return o.Token, nil
}

func (o *OAuth2PKCEAuth) AccessToken() string {
	if o.Token == nil {
		return ""
	}
	t, _ := o.Token["access_token"].(string)
	return t
}

func (o *OAuth2PKCEAuth) IsTokenExpired() bool {
	if o.Token == nil {
		return true
	}
	expiresAt, ok := toInt64(o.Token["expires_at"])
	if !ok || expiresAt == 0 {
		return true
	}
	return time.Now().Unix() > (expiresAt - 10)
}

func (o *OAuth2PKCEAuth) CodeVerifier() string {
	return o.codeVerifier
}

func (o *OAuth2PKCEAuth) CodeChallenge() string {
	return o.codeChallenge
}

func generateCodeVerifier(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	s := base64.RawURLEncoding.EncodeToString(b)
	if len(s) > 128 {
		s = s[:128]
	}
	return s
}

func generateCodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func toInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int:
		return int64(t), true
	case int64:
		return t, true
	case float64:
		return int64(t), true
	case json.Number:
		iv, err := t.Int64()
		if err == nil {
			return iv, true
		}
	}
	return 0, false
}
