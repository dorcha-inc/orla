package serving

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/openai/openai-go"
)

const retryMaxAttempts = 3

// isRetryable returns true if the error indicates a transient failure worth retrying.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 400, 401, 403, 404:
			return false
		case 429, 500, 502, 503, 504:
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"connection refused", "connection reset", "eof", "timeout", "temporary failure", "no such host"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// chatWithRetry calls provider.Chat with exponential backoff (with jitter) on
// retryable errors. Non-retryable errors fail fast via backoff.Permanent.
func chatWithRetry(ctx context.Context, provider model.Provider, messages []model.Message, tools []*model.Tool, opts model.InferenceOptions) (*model.Response, <-chan model.StreamEvent, error) {
	var resp *model.Response
	var ch <-chan model.StreamEvent

	operation := func() error {
		var err error
		resp, ch, err = provider.Chat(ctx, messages, tools, opts)
		if err == nil {
			return nil
		}
		if !isRetryable(err) {
			return backoff.Permanent(err)
		}
		return err
	}

	policy := backoff.NewExponentialBackOff()
	policy.InitialInterval = 500 * time.Millisecond
	bo := backoff.WithContext(backoff.WithMaxRetries(policy, retryMaxAttempts-1), ctx)
	err := backoff.RetryNotify(operation, bo, func(err error, d time.Duration) {
		slog.Warn("Retrying provider Chat after transient error", "error", err, "delay", d)
	})
	return resp, ch, err
}
