package serving

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harvard-cns/orla/internal/model"
	"github.com/openai/openai-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newOpenAIError builds an *openai.Error suitable for tests. The Error() method
// dereferences Request and Response, so we stub them.
func newOpenAIError(status int, message string) *openai.Error {
	req, err := http.NewRequest("POST", "http://example", nil)
	if err != nil {
		panic(err)
	}
	resp := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}
	return &openai.Error{StatusCode: status, Message: message, Request: req, Response: resp}
}

// mockRetryProvider returns a configurable sequence of (response, error) per call.
type mockRetryProvider struct {
	results []struct {
		resp *model.Response
		err  error
	}
	callCount atomic.Int32
}

func (p *mockRetryProvider) Name() string { return "mock" }

func (p *mockRetryProvider) Chat(_ context.Context, _ []model.Message, _ []*model.Tool, _ model.InferenceOptions) (*model.Response, <-chan model.StreamEvent, error) {
	idx := int(p.callCount.Add(1) - 1)
	if idx >= len(p.results) {
		return nil, nil, errors.New("unexpected call: no more results")
	}
	r := p.results[idx]
	return r.resp, nil, r.err
}

func (p *mockRetryProvider) EnsureReady(_ context.Context) error { return nil }

func (p *mockRetryProvider) callCountValue() int { return int(p.callCount.Load()) }

// --- isRetryable tests ---

func TestIsRetryable_NilReturnsFalse(t *testing.T) {
	assert.False(t, isRetryable(nil))
}

func TestIsRetryable_ContextCanceledReturnsFalse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.False(t, isRetryable(ctx.Err()))
}

func TestIsRetryable_ContextDeadlineExceededReturnsFalse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	assert.False(t, isRetryable(ctx.Err()))
}

func TestIsRetryable_APIError400ReturnsFalse(t *testing.T) {
	err := newOpenAIError(400, "bad request")
	assert.False(t, isRetryable(err))
}

func TestIsRetryable_APIError404ReturnsFalse(t *testing.T) {
	err := newOpenAIError(404, "not found")
	assert.False(t, isRetryable(err))
}

func TestIsRetryable_APIError429ReturnsTrue(t *testing.T) {
	err := newOpenAIError(429, "rate limit")
	assert.True(t, isRetryable(err))
}

func TestIsRetryable_APIError500ReturnsTrue(t *testing.T) {
	err := newOpenAIError(500, "internal error")
	assert.True(t, isRetryable(err))
}

func TestIsRetryable_APIError503ReturnsTrue(t *testing.T) {
	err := newOpenAIError(503, "unavailable")
	assert.True(t, isRetryable(err))
}

func TestIsRetryable_ConnectionRefusedReturnsTrue(t *testing.T) {
	assert.True(t, isRetryable(errors.New("connection refused")))
}

func TestIsRetryable_TimeoutInMessageReturnsTrue(t *testing.T) {
	assert.True(t, isRetryable(errors.New("request timeout")))
}

func TestIsRetryable_NetErrorReturnsTrue(t *testing.T) {
	err := &net.DNSError{Err: "no such host", Name: "example.com"}
	assert.True(t, isRetryable(err))
}

func TestIsRetryable_GenericErrorReturnsFalse(t *testing.T) {
	assert.False(t, isRetryable(errors.New("some other error")))
}

// --- chatWithRetry tests ---

func TestChatWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	p := &mockRetryProvider{
		results: []struct {
			resp *model.Response
			err  error
		}{
			{&model.Response{Content: "ok"}, nil},
		},
	}
	resp, _, err := chatWithRetry(context.Background(), p, nil, nil, model.InferenceOptions{})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Content)
	assert.Equal(t, 1, p.callCountValue())
}

func TestChatWithRetry_SuccessOnSecondAttemptAfter500(t *testing.T) {
	p := &mockRetryProvider{
		results: []struct {
			resp *model.Response
			err  error
		}{
			{nil, newOpenAIError(500, "server error")},
			{&model.Response{Content: "ok"}, nil},
		},
	}
	resp, _, err := chatWithRetry(context.Background(), p, nil, nil, model.InferenceOptions{})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Content)
	assert.Equal(t, 2, p.callCountValue())
}

func TestChatWithRetry_NonRetryable400ReturnsImmediately(t *testing.T) {
	p := &mockRetryProvider{
		results: []struct {
			resp *model.Response
			err  error
		}{
			{nil, newOpenAIError(400, "bad request")},
		},
	}
	_, _, err := chatWithRetry(context.Background(), p, nil, nil, model.InferenceOptions{})
	require.Error(t, err)
	assert.Equal(t, 1, p.callCountValue(), "should not retry on 400")
}

func TestChatWithRetry_ExhaustsRetriesThenReturnsError(t *testing.T) {
	p := &mockRetryProvider{
		results: []struct {
			resp *model.Response
			err  error
		}{
			{nil, newOpenAIError(503, "unavailable")},
			{nil, newOpenAIError(503, "unavailable")},
			{nil, newOpenAIError(503, "unavailable")},
		},
	}
	_, _, err := chatWithRetry(context.Background(), p, nil, nil, model.InferenceOptions{})
	require.Error(t, err)
	assert.Equal(t, 3, p.callCountValue())
}

func TestChatWithRetry_ContextCanceledDuringBackoffReturnsContextError(t *testing.T) {
	p := &mockRetryProvider{
		results: []struct {
			resp *model.Response
			err  error
		}{
			{nil, newOpenAIError(503, "unavailable")},
			{&model.Response{Content: "ok"}, nil}, // would succeed on retry
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel shortly after first failure (during backoff)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, _, err := chatWithRetry(ctx, p, nil, nil, model.InferenceOptions{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Equal(t, 1, p.callCountValue(), "should not have retried after context cancel")
}
