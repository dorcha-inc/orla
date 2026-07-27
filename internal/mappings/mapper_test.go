package mappings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decideServer(t *testing.T, handler http.HandlerFunc) *HTTPMapper {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewHTTPMapper(srv.URL, time.Second)
}

func TestHTTPMapper_DecideReturnsChosenBackend(t *testing.T) {
	var got DecideRequest
	mapper := decideServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, _ = w.Write([]byte(`{"backend": "cheap-region"}`))
	})

	quality := 0.9
	backend, err := mapper.Decide(context.Background(), DecideRequest{
		Stage:   "hop",
		Tags:    map[string]string{"tenant": "acme"},
		Current: "expensive-region",
		Candidates: []Candidate{
			{Name: "expensive-region", Quality: &quality, Circuit: "closed"},
			{Name: "cheap-region", Circuit: "closed"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "cheap-region", backend)

	assert.Equal(t, "hop", got.Stage)
	assert.Equal(t, "expensive-region", got.Current)
	assert.Equal(t, map[string]string{"tenant": "acme"}, got.Tags)
	require.Len(t, got.Candidates, 2)
	require.NotNil(t, got.Candidates[0].Quality)
	assert.InDelta(t, 0.9, *got.Candidates[0].Quality, 1e-9)
}

func TestHTTPMapper_DecideEmptyBackendMeansDecline(t *testing.T) {
	mapper := decideServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"backend": ""}`))
	})
	backend, err := mapper.Decide(context.Background(), DecideRequest{Stage: "hop"})
	require.NoError(t, err)
	assert.Empty(t, backend)
}

func TestHTTPMapper_DecideErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "non-200 status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
		{
			name: "malformed body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`not json`))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper := decideServer(t, tt.handler)
			_, err := mapper.Decide(context.Background(), DecideRequest{Stage: "hop"})
			assert.Error(t, err)
		})
	}
}

func TestHTTPMapper_DecideTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"backend": "late"}`))
	}))
	t.Cleanup(srv.Close)

	mapper := NewHTTPMapper(srv.URL, 20*time.Millisecond)
	_, err := mapper.Decide(context.Background(), DecideRequest{Stage: "hop"})
	assert.Error(t, err)
}

func TestMapperHolder_SwapsAtRuntime(t *testing.T) {
	var holder MapperHolder
	assert.Nil(t, holder.Get(), "the zero value holds no policy")

	mapper := NewHTTPMapper("http://localhost:1/decide", time.Second)
	holder.Set(mapper)
	assert.Same(t, mapper, holder.Get())

	holder.Set(nil)
	assert.Nil(t, holder.Get(), "setting nil disables dynamic routing")
}
