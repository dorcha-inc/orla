package serving

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/harvard-cns/orla/internal/model"
	"github.com/openai/openai-go"
	"log/slog"
)

const (
	retryMaxAttempts = 3
	retryBaseDelay   = 500 * time.Millisecond
)

// isRetryable returns true if the error indicates a transient failure worth retrying.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	// Context cancellation - don't retry
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Check for OpenAI API error with HTTP status
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 400, 401, 403, 404:
			return false // client errors
		case 429, 500, 502, 503, 504:
			return true // rate limit, server errors
		}
	}
	// Network/connection errors - retry
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"connection refused", "connection reset", "eof", "timeout", "temporary failure", "no such host"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	// Check for net.Error (e.g. timeout)
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true // network errors are typically transient
	}
	return false
}

// chatWithRetry calls provider.Chat with exponential backoff on retryable errors.
func chatWithRetry(ctx context.Context, provider model.Provider, messages []model.Message, tools []*model.Tool, opts model.InferenceOptions) (*model.Response, <-chan model.StreamEvent, error) {
	var lastErr error
	for attempt := range retryMaxAttempts {
		resp, ch, err := provider.Chat(ctx, messages, tools, opts)
		if err == nil {
			return resp, ch, nil
		}
		lastErr = err
		if !isRetryable(err) || attempt == retryMaxAttempts-1 {
			return nil, nil, err
		}
		delay := retryBaseDelay * (1 << attempt)
		slog.Warn("Retrying provider Chat after transient error",
			"error", err,
			"attempt", attempt+1,
			"max_attempts", retryMaxAttempts,
			"delay", delay)
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(delay):
			// retry
		}
	}
	return nil, nil, lastErr
}
