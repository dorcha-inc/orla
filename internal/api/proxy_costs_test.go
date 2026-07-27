package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/backends"
	"github.com/harvard-cns/orla/internal/costs"
	"github.com/harvard-cns/orla/internal/provider"
	"github.com/harvard-cns/orla/internal/scheduler"
	"github.com/harvard-cns/orla/internal/stages"
)

// TestProxy_LiveCostOverridesStaticColumns proves a polled price wins
// over the backend's static per-mtoken columns when both exist.
func TestProxy_LiveCostOverridesStaticColumns(t *testing.T) {
	mock := provider.NewMockProvider().WithName("gpt4o").
		WithResponse(&openai.ChatCompletion{
			ID:    "chatcmpl-live-cost",
			Model: "openai:gpt-4o",
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{Role: "assistant", Content: "hi"},
			}},
			Usage: openai.CompletionUsage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000},
		})

	sched := scheduler.New(
		func(b *backends.Backend) provider.Backend { return mock },
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	t.Cleanup(func() { _ = sched.Shutdown(context.Background()) })
	staticIn, staticOut := 100.0, 100.0
	sched.Register(&backends.Backend{
		Name: "gpt4o", Endpoint: "x", ModelID: new("openai:gpt-4o"),
		MaxConcurrency:      2,
		InputCostPerMtoken:  &staticIn,
		OutputCostPerMtoken: &staticOut,
	})

	stageReg := stages.NewFakeRegistry()
	_, err := stageReg.Replace(context.Background(), &stages.Stage{ID: "planning", Backend: "gpt4o"})
	require.NoError(t, err)

	liveCosts := costs.NewStore()
	liveCosts.Set("gpt4o", costs.Price{InputPerMtoken: 0.5, OutputPerMtoken: 1.5})

	sink := &recordingSink{}
	srv := NewServer(ServerConfig{
		ListenAddress: "127.0.0.1:0",
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	RegisterProxyRoutes(srv.Router(), ProxyDeps{
		Stages: stageReg, Scheduler: sched, CompletionSink: sink, Costs: liveCosts,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		bytes.NewReader(bodyForChat("hi")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderStage, "planning")
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	require.Len(t, sink.got, 1)
	require.NotNil(t, sink.got[0].CostUSD)
	// 1M prompt at $0.50/Mt plus 1M completion at $1.50/Mt is $2.00.
	// The static columns would have priced the same call at $200.
	assert.InDelta(t, 2.0, *sink.got[0].CostUSD, 1e-9)
}

// TestProxy_NoLiveCostFallsBackToStatic proves the static columns still
// price a backend the store holds no entry for.
func TestProxy_NoLiveCostFallsBackToStatic(t *testing.T) {
	mock := provider.NewMockProvider().WithName("gpt4o").
		WithResponse(&openai.ChatCompletion{
			ID:    "chatcmpl-static-cost",
			Model: "openai:gpt-4o",
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{Role: "assistant", Content: "hi"},
			}},
			Usage: openai.CompletionUsage{PromptTokens: 1_000_000, CompletionTokens: 0},
		})

	sched := scheduler.New(
		func(b *backends.Backend) provider.Backend { return mock },
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	t.Cleanup(func() { _ = sched.Shutdown(context.Background()) })
	staticIn := 3.0
	sched.Register(&backends.Backend{
		Name: "gpt4o", Endpoint: "x", ModelID: new("openai:gpt-4o"),
		MaxConcurrency:     2,
		InputCostPerMtoken: &staticIn,
	})

	stageReg := stages.NewFakeRegistry()
	_, err := stageReg.Replace(context.Background(), &stages.Stage{ID: "planning", Backend: "gpt4o"})
	require.NoError(t, err)

	sink := &recordingSink{}
	srv := NewServer(ServerConfig{
		ListenAddress: "127.0.0.1:0",
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	RegisterProxyRoutes(srv.Router(), ProxyDeps{
		Stages: stageReg, Scheduler: sched, CompletionSink: sink, Costs: costs.NewStore(),
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		bytes.NewReader(bodyForChat("hi")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderStage, "planning")
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	require.Len(t, sink.got, 1)
	require.NotNil(t, sink.got[0].CostUSD)
	assert.InDelta(t, 3.0, *sink.got[0].CostUSD, 1e-9)
}
