package xdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

func (c *Client) call(ctx context.Context, op operation, input Params) (JSON, error) {
	req, err := c.buildRequest(ctx, op, input)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(payload)}
	}

	if len(bytes.TrimSpace(payload)) == 0 {
		return JSON{}, nil
	}

	var data any
	if err := json.Unmarshal(payload, &data); err != nil {
		return JSON{"raw": string(payload)}, nil
	}

	return toJSONMap(data), nil
}

func (c *Client) buildRequest(ctx context.Context, op operation, input Params) (*http.Request, error) {
	if input == nil {
		input = Params{}
	}
	if err := validateRequired(op, input); err != nil {
		return nil, err
	}

	endpoint := op.Path
	pathParamSet := map[string]struct{}{}
	for _, p := range op.PathParams {
		v, ok := input[p]
		if !ok || isNil(v) {
			return nil, fmt.Errorf("missing path parameter: %s", p)
		}
		endpoint = strings.ReplaceAll(endpoint, "{"+p+"}", url.PathEscape(fmt.Sprint(v)))
		pathParamSet[p] = struct{}{}
	}

	fullURL := c.BaseURL + endpoint
	u, err := url.Parse(fullURL)
	if err != nil {
		return nil, err
	}

	query := u.Query()
	used := map[string]struct{}{}

	for p, qk := range op.QueryParams {
		if val, ok := input[p]; ok && !isNil(val) {
			addQueryValue(query, qk, val)
			used[p] = struct{}{}
			continue
		}
		if p != qk {
			if val, ok := input[qk]; ok && !isNil(val) {
				addQueryValue(query, qk, val)
				used[qk] = struct{}{}
			}
		}
	}

	if op.PaginationParam != "" {
		if val, ok := input["pagination_token"]; ok && !isNil(val) {
			addQueryValue(query, op.PaginationParam, val)
			used["pagination_token"] = struct{}{}
		}
		if val, ok := input[op.PaginationParam]; ok && !isNil(val) {
			addQueryValue(query, op.PaginationParam, val)
			used[op.PaginationParam] = struct{}{}
		}
	}

	for k, v := range input {
		if isNil(v) {
			continue
		}
		if k == "body" || k == "stream_config" {
			continue
		}
		if _, ok := pathParamSet[k]; ok {
			continue
		}
		if _, ok := used[k]; ok {
			continue
		}
		if slices.Contains(op.AllParams, k) {
			addQueryValue(query, k, v)
		}
	}

	u.RawQuery = query.Encode()

	var body io.Reader
	if rawBody, ok := input["body"]; ok && !isNil(rawBody) {
		b, err := json.Marshal(rawBody)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(op.Method), u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	authHeader, err := c.selectAuthorizationHeader(ctx, op, req.Method, u.String())
	if err != nil {
		return nil, err
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	if op.Streaming {
		req.Header.Set("Accept", "application/json")
	}

	return req, nil
}

func validateRequired(op operation, input Params) error {
	for _, key := range op.RequiredParams {
		val, ok := input[key]
		if !ok || isNil(val) {
			return fmt.Errorf("missing required parameter: %s", key)
		}
	}
	return nil
}

func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Func:
		return rv.IsNil()
	default:
		return false
	}
}

func addQueryValue(values url.Values, key string, value any) {
	if isNil(value) {
		return
	}
	rv := reflect.ValueOf(value)
	if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
		parts := make([]string, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			parts = append(parts, toQueryValue(rv.Index(i).Interface()))
		}
		values.Set(key, strings.Join(parts, ","))
		return
	}
	values.Set(key, toQueryValue(value))
}

func toQueryValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case int:
		return strconv.Itoa(t)
	case int8, int16, int32, int64:
		return fmt.Sprintf("%d", t)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", t)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 64)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprint(t)
	}
}

func (c *Client) selectAuthorizationHeader(ctx context.Context, op operation, method string, fullURL string) (string, error) {
	availableBearer := strings.TrimSpace(c.BearerToken) != ""
	availableOAuth2 := strings.TrimSpace(c.currentAccessToken()) != ""
	availableOAuth1 := c.Auth != nil && c.Auth.AccessToken != nil && c.Auth.AccessToken.AccessToken != ""

	schemes := op.SecuritySchemes
	selected := ""

	if len(schemes) == 0 {
		switch {
		case availableBearer:
			selected = "bearer"
		case availableOAuth2:
			selected = "oauth2"
		case availableOAuth1:
			selected = "oauth1"
		}
	} else if len(schemes) == 1 {
		switch schemes[0] {
		case "BearerToken":
			if availableBearer {
				selected = "bearer"
			}
		case "OAuth2UserToken":
			if availableOAuth2 {
				selected = "oauth2"
			}
		case "UserToken":
			if availableOAuth1 {
				selected = "oauth1"
			}
		}
	} else {
		isWrite := method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete || method == http.MethodPatch
		if isWrite {
			if hasScheme(schemes, "UserToken") && availableOAuth1 {
				selected = "oauth1"
			} else if hasScheme(schemes, "OAuth2UserToken") && availableOAuth2 {
				selected = "oauth2"
			} else if hasScheme(schemes, "BearerToken") && availableBearer {
				selected = "bearer"
			}
		} else {
			if hasScheme(schemes, "BearerToken") && availableBearer {
				selected = "bearer"
			} else if hasScheme(schemes, "OAuth2UserToken") && availableOAuth2 {
				selected = "oauth2"
			} else if hasScheme(schemes, "UserToken") && availableOAuth1 {
				selected = "oauth1"
			}
		}
	}

	if selected == "" {
		if len(schemes) > 0 {
			return "", fmt.Errorf("authentication required; required schemes: %v", schemes)
		}
		return "", nil
	}

	switch selected {
	case "bearer":
		if c.BearerToken != "" {
			return "Bearer " + c.BearerToken, nil
		}
		if token := c.currentAccessToken(); token != "" {
			return "Bearer " + token, nil
		}
	case "oauth2":
		if c.OAuth2Auth != nil && c.OAuth2Auth.Token != nil && c.OAuth2Auth.IsTokenExpired() {
			if _, err := c.RefreshToken(ctx); err != nil {
				return "", err
			}
		}
		if token := c.currentAccessToken(); token != "" {
			return "Bearer " + token, nil
		}
	case "oauth1":
		if c.Auth == nil {
			return "", fmt.Errorf("oauth1 auth not configured")
		}
		h, err := c.Auth.BuildRequestHeader(method, fullURL, "")
		if err != nil {
			return "", err
		}
		return h, nil
	}

	return "", fmt.Errorf("failed to resolve authentication header")
}

func hasScheme(schemes []string, scheme string) bool {
	for _, s := range schemes {
		if s == scheme {
			return true
		}
	}
	return false
}

func (c *Client) currentAccessToken() string {
	if c.AccessToken != "" {
		return c.AccessToken
	}
	if c.OAuth2Auth != nil {
		if token := c.OAuth2Auth.AccessToken(); token != "" {
			c.AccessToken = token
			return token
		}
	}
	return ""
}
