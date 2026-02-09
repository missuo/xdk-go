package xdk

import (
	"context"
	"net/http"
	"strings"
)

const (
	defaultBaseURL          = "https://api.x.com"
	defaultAuthorizationURL = "https://x.com/i"
	defaultUserAgent        = "xdk-go/0.1.0"
)

// Config configures the main XDK client.
type Config struct {
	BaseURL              string
	BearerToken          string
	AccessToken          string
	ClientID             string
	ClientSecret         string
	RedirectURI          string
	Token                map[string]any
	Scope                []string
	AuthorizationBaseURL string
	Auth                 *OAuth1
	HTTPClient           *http.Client
}

// Client is the primary entry point for the X API.
type Client struct {
	BaseURL     string
	HTTPClient  *http.Client
	BearerToken string
	AccessToken string
	UserAgent   string

	Auth       *OAuth1
	OAuth2Auth *OAuth2PKCEAuth

	AccountActivity *AccountActivityClient
	Activity        *ActivityClient
	Communities     *CommunitiesClient
	CommunityNotes  *CommunityNotesClient
	Compliance      *ComplianceClient
	Connections     *ConnectionsClient
	DirectMessages  *DirectMessagesClient
	General         *GeneralClient
	Lists           *ListsClient
	Media           *MediaClient
	News            *NewsClient
	Posts           *PostsClient
	Spaces          *SpacesClient
	Stream          *StreamClient
	Trends          *TrendsClient
	Usage           *UsageClient
	Users           *UsersClient
	Webhooks        *WebhooksClient
}

// NewClient creates a client instance with all generated sub-clients.
func NewClient(cfg Config) *Client {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	authzBase := strings.TrimRight(cfg.AuthorizationBaseURL, "/")
	if authzBase == "" {
		authzBase = defaultAuthorizationURL
	}

	c := &Client{
		BaseURL:     baseURL,
		HTTPClient:  httpClient,
		BearerToken: cfg.BearerToken,
		AccessToken: cfg.AccessToken,
		UserAgent:   defaultUserAgent,
		Auth:        cfg.Auth,
	}

	if tokenAccess, ok := cfg.Token["access_token"].(string); ok && tokenAccess != "" {
		c.AccessToken = tokenAccess
	}

	if cfg.ClientID != "" || cfg.Token != nil {
		c.OAuth2Auth = NewOAuth2PKCEAuth(OAuth2Config{
			BaseURL:              baseURL,
			AuthorizationBaseURL: authzBase,
			ClientID:             cfg.ClientID,
			ClientSecret:         cfg.ClientSecret,
			RedirectURI:          cfg.RedirectURI,
			Token:                cfg.Token,
			Scope:                cfg.Scope,
			HTTPClient:           httpClient,
		})
		if token := c.OAuth2Auth.AccessToken(); token != "" {
			c.AccessToken = token
		}
	}

	c.AccountActivity = &AccountActivityClient{client: c}
	c.Activity = &ActivityClient{client: c}
	c.Communities = &CommunitiesClient{client: c}
	c.CommunityNotes = &CommunityNotesClient{client: c}
	c.Compliance = &ComplianceClient{client: c}
	c.Connections = &ConnectionsClient{client: c}
	c.DirectMessages = &DirectMessagesClient{client: c}
	c.General = &GeneralClient{client: c}
	c.Lists = &ListsClient{client: c}
	c.Media = &MediaClient{client: c}
	c.News = &NewsClient{client: c}
	c.Posts = &PostsClient{client: c}
	c.Spaces = &SpacesClient{client: c}
	c.Stream = &StreamClient{client: c}
	c.Trends = &TrendsClient{client: c}
	c.Usage = &UsageClient{client: c}
	c.Users = &UsersClient{client: c}
	c.Webhooks = &WebhooksClient{client: c}

	return c
}

func (c *Client) OAuth2Token() map[string]any {
	if c.OAuth2Auth == nil {
		return nil
	}
	return c.OAuth2Auth.Token
}

func (c *Client) GetAuthorizationURL(state string) (string, error) {
	if c.OAuth2Auth == nil {
		return "", ErrOAuth2NotConfigured
	}
	return c.OAuth2Auth.GetAuthorizationURL(state)
}

func (c *Client) ExchangeCode(ctx context.Context, code string, codeVerifier string) (map[string]any, error) {
	if c.OAuth2Auth == nil {
		return nil, ErrOAuth2NotConfigured
	}
	token, err := c.OAuth2Auth.ExchangeCode(ctx, code, codeVerifier)
	if err != nil {
		return nil, err
	}
	if access, ok := token["access_token"].(string); ok {
		c.AccessToken = access
	}
	return token, nil
}

func (c *Client) FetchToken(ctx context.Context, authorizationResponse string) (map[string]any, error) {
	if c.OAuth2Auth == nil {
		return nil, ErrOAuth2NotConfigured
	}
	token, err := c.OAuth2Auth.FetchToken(ctx, authorizationResponse)
	if err != nil {
		return nil, err
	}
	if access, ok := token["access_token"].(string); ok {
		c.AccessToken = access
	}
	return token, nil
}

func (c *Client) RefreshToken(ctx context.Context) (map[string]any, error) {
	if c.OAuth2Auth == nil {
		return nil, ErrOAuth2NotConfigured
	}
	token, err := c.OAuth2Auth.RefreshToken(ctx)
	if err != nil {
		return nil, err
	}
	if access, ok := token["access_token"].(string); ok {
		c.AccessToken = access
	}
	return token, nil
}

func (c *Client) IsTokenExpired() bool {
	if c.OAuth2Auth == nil {
		return true
	}
	return c.OAuth2Auth.IsTokenExpired()
}
