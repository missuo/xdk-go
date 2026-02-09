# xdk-go

A Go SDK for the X API, generated from the `xdk-python` API surface.

## Installation

```bash
go get github.com/missuo/xdk-go
```

## Features

- Full generated coverage based on current `xdk-python` clients
- 18 service clients
- 142 operations
- Bearer Token, OAuth2 PKCE, and OAuth1.0a support
- Built-in pagination helper (`Pager`)
- Built-in resilient streaming helper (`StreamConfig` with retry/backoff)

## Quick Start

```go
package main

import (
	"context"
	"fmt"

	"github.com/missuo/xdk-go"
)

func main() {
	ctx := context.Background()

	client := xdk.NewClient(xdk.Config{
		BearerToken: "YOUR_BEARER_TOKEN",
	})

	resp, err := client.Posts.GetById(ctx, xdk.Params{
		"id":           "1234567890",
		"tweet_fields": []string{"created_at", "author_id"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(resp)
}
```

## API Layout

`NewClient` exposes these generated service clients:

- `AccountActivity`
- `Activity`
- `Communities`
- `CommunityNotes`
- `Compliance`
- `Connections`
- `DirectMessages`
- `General`
- `Lists`
- `Media`
- `News`
- `Posts`
- `Spaces`
- `Stream`
- `Trends`
- `Usage`
- `Users`
- `Webhooks`

## Request and Response Types

- Request input type: `xdk.Params` (`map[string]any`)
- Response type: `xdk.JSON` (`map[string]any`)

Parameter naming follows generated Python method parameters, for example:

- `tweet_fields`
- `user_fields`
- `max_results`
- `body`

The SDK maps these to the actual API query keys automatically when needed (for example, `tweet_fields` to `tweet.fields`).

## Authentication

### Bearer Token

```go
client := xdk.NewClient(xdk.Config{
	BearerToken: "YOUR_BEARER_TOKEN",
})
```

### OAuth2 PKCE

```go
ctx := context.Background()

client := xdk.NewClient(xdk.Config{
	ClientID:    "YOUR_CLIENT_ID",
	RedirectURI: "http://localhost:8080/callback",
	Scope:       []string{"tweet.read", "users.read", "offline.access"},
})

authURL, err := client.GetAuthorizationURL("optional-state")
if err != nil {
	panic(err)
}
fmt.Println("Open:", authURL)

// After callback, exchange code for tokens.
token, err := client.ExchangeCode(ctx, "AUTH_CODE", "")
if err != nil {
	panic(err)
}
fmt.Println(token)
```

### OAuth1.0a

```go
ctx := context.Background()

oauth1 := xdk.NewOAuth1(
	"API_KEY",
	"API_SECRET",
	"http://localhost:8080/callback",
	"", // access token (optional)
	"", // access token secret (optional)
)

url, err := oauth1.StartOAuthFlow(ctx, false)
if err != nil {
	panic(err)
}
fmt.Println("Authorize:", url)

// After verifier is returned:
_, err = oauth1.GetAccessToken(ctx, "OAUTH_VERIFIER")
if err != nil {
	panic(err)
}

client := xdk.NewClient(xdk.Config{Auth: oauth1})
_ = client
```

## Pagination

Generated paginated methods return `*Pager`.

```go
ctx := context.Background()
pager := client.Posts.SearchRecent(xdk.Params{
	"query":       "golang",
	"max_results": 10,
})

for {
	page, ok, err := pager.Next(ctx)
	if err != nil {
		panic(err)
	}
	if !ok {
		break
	}
	fmt.Println(page)
}
```

## Streaming

Streaming methods return `(<-chan xdk.JSON, <-chan error)`.

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

streamCfg := &xdk.StreamConfig{
	MaxRetries: -1, // infinite retry
}

dataCh, errCh := client.Stream.PostsSample(ctx, xdk.Params{}, streamCfg)

for {
	select {
	case item, ok := <-dataCh:
		if !ok {
			return
		}
		fmt.Println(item)
	case err := <-errCh:
		if err != nil {
			panic(err)
		}
	}
}
```

## Error Handling

Non-2xx API responses return `*xdk.APIError`.

```go
resp, err := client.Users.GetByUsername(ctx, xdk.Params{"username": "not-exists"})
if err != nil {
	if apiErr, ok := err.(*xdk.APIError); ok {
		fmt.Println(apiErr.StatusCode, apiErr.Body)
		return
	}
	panic(err)
}
fmt.Println(resp)
```

## Regeneration

Generated files come from:

- source: `xdk-python/xdk/*/client.py`
- generator: `scripts/generate_go_sdk.py`

Regenerate with:

```bash
python3 scripts/generate_go_sdk.py
gofmt -w *.go
```

## Documentation

XDK Python docs reference:

- https://docs.x.com/xdks/python/overview

## License

MIT
