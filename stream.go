package xdk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"time"
)

type StreamErrorType string

const (
	StreamConnectionError StreamErrorType = "connection_error"
	StreamTimeout         StreamErrorType = "timeout"
	StreamServerError     StreamErrorType = "server_error"
	StreamRateLimited     StreamErrorType = "rate_limited"
	StreamInterrupted     StreamErrorType = "stream_interrupted"
	StreamAuthentication  StreamErrorType = "authentication_error"
	StreamClientError     StreamErrorType = "client_error"
	StreamFatal           StreamErrorType = "fatal_error"
)

// StreamError classifies streaming failures and whether retries are allowed.
type StreamError struct {
	Type       StreamErrorType
	Message    string
	Err        error
	StatusCode int
	Body       string
}

func (e *StreamError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s (status=%d): %s", e.Type, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *StreamError) Unwrap() error {
	return e.Err
}

func (e *StreamError) Retryable() bool {
	switch e.Type {
	case StreamConnectionError, StreamTimeout, StreamServerError, StreamRateLimited, StreamInterrupted:
		return true
	default:
		return false
	}
}

// StreamConfig controls retry behavior for streaming endpoints.
type StreamConfig struct {
	MaxRetries        int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
	Jitter            bool
	Timeout           time.Duration
	ChunkSize         int

	OnConnect    func()
	OnDisconnect func(error)
	OnReconnect  func(attempt int, delay time.Duration)
	OnError      func(*StreamError)
}

func defaultStreamConfig() StreamConfig {
	return StreamConfig{
		MaxRetries:        10,
		InitialBackoff:    time.Second,
		MaxBackoff:        64 * time.Second,
		BackoffMultiplier: 2,
		Jitter:            true,
		Timeout:           0,
		ChunkSize:         1024,
	}
}

func (c *Client) stream(ctx context.Context, op operation, input Params, config *StreamConfig) (<-chan JSON, <-chan error) {
	dataCh := make(chan JSON)
	errCh := make(chan error, 1)

	cfg := defaultStreamConfig()
	if config != nil {
		cfg = *config
		if cfg.InitialBackoff <= 0 {
			cfg.InitialBackoff = time.Second
		}
		if cfg.MaxBackoff <= 0 {
			cfg.MaxBackoff = 64 * time.Second
		}
		if cfg.BackoffMultiplier <= 0 {
			cfg.BackoffMultiplier = 2
		}
		if cfg.ChunkSize <= 0 {
			cfg.ChunkSize = 1024
		}
	}

	go func() {
		defer close(dataCh)
		defer close(errCh)

		attempt := 0
		for {
			if ctx.Err() != nil {
				return
			}

			requestCtx := ctx
			var cancel context.CancelFunc
			if cfg.Timeout > 0 {
				requestCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
			}

			req, err := c.buildRequest(requestCtx, op, cloneParams(input))
			if err != nil {
				if cancel != nil {
					cancel()
				}
				errCh <- err
				return
			}

			resp, err := c.HTTPClient.Do(req)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					if ctx.Err() != nil {
						return
					}
				}
				if cancel != nil {
					cancel()
				}
				streamErr := classifyStreamError(err, nil)
				if cfg.OnError != nil {
					cfg.OnError(streamErr)
				}
				if cfg.OnDisconnect != nil {
					cfg.OnDisconnect(streamErr)
				}
				if !shouldRetry(streamErr, cfg, attempt) {
					errCh <- streamErr
					return
				}
				delay := backoffDelay(cfg, attempt)
				attempt++
				if cfg.OnReconnect != nil {
					cfg.OnReconnect(attempt, delay)
				}
				if !sleepWithContext(ctx, delay) {
					return
				}
				continue
			}

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				bodyBytes, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if cancel != nil {
					cancel()
				}
				streamErr := classifyStreamError(&APIError{StatusCode: resp.StatusCode, Body: string(bodyBytes)}, resp)
				if cfg.OnError != nil {
					cfg.OnError(streamErr)
				}
				if cfg.OnDisconnect != nil {
					cfg.OnDisconnect(streamErr)
				}
				if !shouldRetry(streamErr, cfg, attempt) {
					errCh <- streamErr
					return
				}
				delay := backoffDelay(cfg, attempt)
				attempt++
				if cfg.OnReconnect != nil {
					cfg.OnReconnect(attempt, delay)
				}
				if !sleepWithContext(ctx, delay) {
					return
				}
				continue
			}

			if cfg.OnConnect != nil {
				cfg.OnConnect()
			}
			attempt = 0

			procErr := processStreamResponse(ctx, resp.Body, cfg.ChunkSize, dataCh)
			_ = resp.Body.Close()
			if cancel != nil {
				cancel()
			}
			if errors.Is(procErr, context.Canceled) || (ctx.Err() != nil && procErr != nil) {
				return
			}

			if cfg.OnDisconnect != nil {
				cfg.OnDisconnect(procErr)
			}

			streamErr := classifyStreamError(procErr, resp)
			if !streamErr.Retryable() {
				if cfg.OnError != nil {
					cfg.OnError(streamErr)
				}
				errCh <- streamErr
				return
			}
			if !shouldRetry(streamErr, cfg, attempt) {
				if cfg.OnError != nil {
					cfg.OnError(streamErr)
				}
				errCh <- streamErr
				return
			}

			delay := backoffDelay(cfg, attempt)
			attempt++
			if cfg.OnReconnect != nil {
				cfg.OnReconnect(attempt, delay)
			}
			if !sleepWithContext(ctx, delay) {
				return
			}
		}
	}()

	return dataCh, errCh
}

func processStreamResponse(ctx context.Context, body io.Reader, chunkSize int, out chan<- JSON) error {
	reader := bufio.NewReaderSize(body, chunkSize)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			s := strings.TrimSpace(string(line))
			if s != "" && s != "{}" {
				var payload any
				if decodeErr := json.Unmarshal([]byte(s), &payload); decodeErr == nil {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case out <- toJSONMap(payload):
					}
				}
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return io.EOF
			}
			return err
		}
	}
}

func classifyStreamError(err error, resp *http.Response) *StreamError {
	if err == nil {
		return &StreamError{Type: StreamInterrupted, Message: "stream ended"}
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		sc := apiErr.StatusCode
		switch {
		case sc == 429:
			return &StreamError{Type: StreamRateLimited, Message: "rate limited", Err: err, StatusCode: sc, Body: apiErr.Body}
		case sc == 401 || sc == 403:
			return &StreamError{Type: StreamAuthentication, Message: "authentication error", Err: err, StatusCode: sc, Body: apiErr.Body}
		case sc >= 400 && sc < 500:
			return &StreamError{Type: StreamClientError, Message: "client error", Err: err, StatusCode: sc, Body: apiErr.Body}
		case sc >= 500:
			return &StreamError{Type: StreamServerError, Message: "server error", Err: err, StatusCode: sc, Body: apiErr.Body}
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return &StreamError{Type: StreamTimeout, Message: err.Error(), Err: err}
		}
		return &StreamError{Type: StreamConnectionError, Message: err.Error(), Err: err}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return &StreamError{Type: StreamTimeout, Message: err.Error(), Err: err}
	}
	if errors.Is(err, io.EOF) {
		return &StreamError{Type: StreamInterrupted, Message: "stream interrupted", Err: err}
	}
	if resp != nil && resp.StatusCode >= 500 {
		return &StreamError{Type: StreamServerError, Message: err.Error(), Err: err, StatusCode: resp.StatusCode}
	}

	return &StreamError{Type: StreamFatal, Message: err.Error(), Err: err}
}

func shouldRetry(err *StreamError, cfg StreamConfig, attempt int) bool {
	if err == nil || !err.Retryable() {
		return false
	}
	if cfg.MaxRetries < 0 {
		return true
	}
	return attempt < cfg.MaxRetries
}

func backoffDelay(cfg StreamConfig, attempt int) time.Duration {
	base := float64(cfg.InitialBackoff)
	delay := base * math.Pow(cfg.BackoffMultiplier, float64(attempt))
	if delay > float64(cfg.MaxBackoff) {
		delay = float64(cfg.MaxBackoff)
	}
	if cfg.Jitter {
		delay += delay * (rand.Float64() * 0.25)
	}
	return time.Duration(delay)
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
