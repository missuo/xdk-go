package xdk

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// OAuth1RequestToken contains OAuth1 request token credentials.
type OAuth1RequestToken struct {
	OAuthToken       string
	OAuthTokenSecret string
}

// OAuth1AccessToken contains OAuth1 access token credentials.
type OAuth1AccessToken struct {
	AccessToken       string
	AccessTokenSecret string
}

// OAuth1 supports the OAuth 1.0a flow and request signing.
type OAuth1 struct {
	APIKey    string
	APISecret string
	Callback  string

	RequestToken *OAuth1RequestToken
	AccessToken  *OAuth1AccessToken

	HTTPClient *http.Client
}

func NewOAuth1(apiKey, apiSecret, callback string, accessToken, accessTokenSecret string) *OAuth1 {
	o := &OAuth1{
		APIKey:     apiKey,
		APISecret:  apiSecret,
		Callback:   callback,
		HTTPClient: http.DefaultClient,
	}
	if accessToken != "" && accessTokenSecret != "" {
		o.AccessToken = &OAuth1AccessToken{AccessToken: accessToken, AccessTokenSecret: accessTokenSecret}
	}
	return o
}

func (o *OAuth1) GetAuthorizationURL(loginWithX bool) (string, error) {
	if o.RequestToken == nil {
		return "", errors.New("request token not obtained; call GetRequestToken first")
	}
	base := "https://x.com/oauth/authorize"
	if loginWithX {
		base = "https://x.com/i/oauth/authenticate"
	}
	q := url.Values{}
	q.Set("oauth_token", o.RequestToken.OAuthToken)
	return base + "?" + q.Encode(), nil
}

func (o *OAuth1) GetRequestToken(ctx context.Context) (*OAuth1RequestToken, error) {
	u := "https://api.x.com/oauth/request_token"
	q := url.Values{}
	q.Set("oauth_callback", o.Callback)
	oauthHeader := o.buildOAuthHeader(http.MethodPost, u, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", oauthHeader)

	client := o.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get request token: %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	vals, err := url.ParseQuery(string(b))
	if err != nil {
		return nil, err
	}
	tok := vals.Get("oauth_token")
	secret := vals.Get("oauth_token_secret")
	if tok == "" || secret == "" {
		return nil, errors.New("invalid request token response")
	}
	o.RequestToken = &OAuth1RequestToken{OAuthToken: tok, OAuthTokenSecret: secret}
	return o.RequestToken, nil
}

func (o *OAuth1) GetAccessToken(ctx context.Context, verifier string) (*OAuth1AccessToken, error) {
	if o.RequestToken == nil {
		return nil, errors.New("request token not obtained; call GetRequestToken first")
	}
	u := "https://api.x.com/oauth/access_token"
	q := url.Values{}
	q.Set("oauth_token", o.RequestToken.OAuthToken)
	q.Set("oauth_verifier", verifier)
	oauthHeader := o.buildOAuthHeader(http.MethodPost, u, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", oauthHeader)

	client := o.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get access token: %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	vals, err := url.ParseQuery(string(b))
	if err != nil {
		return nil, err
	}
	tok := vals.Get("oauth_token")
	secret := vals.Get("oauth_token_secret")
	if tok == "" || secret == "" {
		return nil, errors.New("invalid access token response")
	}
	o.AccessToken = &OAuth1AccessToken{AccessToken: tok, AccessTokenSecret: secret}
	// Once access credentials are available, request token credentials are no longer used.
	o.RequestToken = nil
	return o.AccessToken, nil
}

func (o *OAuth1) StartOAuthFlow(ctx context.Context, loginWithX bool) (string, error) {
	if _, err := o.GetRequestToken(ctx); err != nil {
		return "", err
	}
	return o.GetAuthorizationURL(loginWithX)
}

func (o *OAuth1) BuildRequestHeader(method, rawURL, body string) (string, error) {
	if o.AccessToken == nil {
		return "", errors.New("access token not available")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	baseURL := parsed.Scheme + "://" + parsed.Host + parsed.Path
	all := parsed.RawQuery
	if body != "" {
		if all != "" {
			all += "&" + body
		} else {
			all = body
		}
	}
	return o.buildOAuthHeader(method, baseURL, all), nil
}

func (o *OAuth1) buildOAuthHeader(method, rawURL, encodedParams string) string {
	oauthParams := map[string]string{
		"oauth_consumer_key":     o.APIKey,
		"oauth_nonce":            oauthNonce(),
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        strconv.FormatInt(time.Now().Unix(), 10),
		"oauth_version":          "1.0",
	}
	if o.RequestToken != nil {
		oauthParams["oauth_token"] = o.RequestToken.OAuthToken
	}
	if o.AccessToken != nil {
		oauthParams["oauth_token"] = o.AccessToken.AccessToken
	}

	paramString := buildOAuthParamString(oauthParams, encodedParams)
	baseString := strings.ToUpper(method) + "&" + oauthEncode(rawURL) + "&" + oauthEncode(paramString)

	tokenSecret := ""
	if o.AccessToken != nil {
		tokenSecret = o.AccessToken.AccessTokenSecret
	} else if o.RequestToken != nil {
		tokenSecret = o.RequestToken.OAuthTokenSecret
	}
	signingKey := oauthEncode(o.APISecret) + "&" + oauthEncode(tokenSecret)
	h := hmac.New(sha1.New, []byte(signingKey))
	_, _ = h.Write([]byte(baseString))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
	oauthParams["oauth_signature"] = signature

	keys := make([]string, 0, len(oauthParams))
	for k := range oauthParams {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, oauthEncode(oauthParams[k])))
	}
	return "OAuth " + strings.Join(parts, ", ")
}

func buildOAuthParamString(oauthParams map[string]string, encodedParams string) string {
	all := map[string]string{}
	for k, v := range oauthParams {
		all[k] = v
	}
	if encodedParams != "" {
		parsed, err := url.ParseQuery(encodedParams)
		if err == nil {
			for k, vals := range parsed {
				if len(vals) > 0 {
					all[k] = vals[0]
				}
			}
		}
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, oauthEncode(k)+"="+oauthEncode(all[k]))
	}
	return strings.Join(parts, "&")
}

func oauthEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func oauthNonce() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
