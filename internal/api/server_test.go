package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// freePort returns an OS-assigned free TCP port as "127.0.0.1:N".
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

func TestServer_StartShutdown(t *testing.T) {
	addr := freePort(t)
	srv := NewServer(ServerConfig{
		ListenAddress: addr,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		ReadTimeout:   1 * time.Second,
		WriteTimeout:  1 * time.Second,
	})

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	// Poll until the listener accepts.
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	var err error
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NoError(t, err, "server never became reachable")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(shutdownCtx))

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}
}

func TestServer_RequestIDPresent(t *testing.T) {
	addr := freePort(t)
	srv := NewServer(ServerConfig{
		ListenAddress: addr,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-errCh
	})

	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	var err error
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.NotEmpty(t, resp.Header.Get("X-Request-Id"),
		"chi middleware should set X-Request-Id")
}

// TestServer_AccessLogRecordsCaller covers the client address on the
// access log, which is the forensic half of the control-plane audit.
// Orla authenticates nobody, so both fields are whatever the caller
// claimed.
func TestServer_AccessLogRecordsCaller(t *testing.T) {
	var buf bytes.Buffer
	srv := NewServer(ServerConfig{
		ListenAddress: "127.0.0.1:0",
		Logger:        slog.New(slog.NewTextHandler(&buf, nil)),
	})
	srv.Router().Put("/api/v1/stages/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/stages/planning", nil)
	req.Header.Set("X-Forwarded-For", "10.1.2.3")
	srv.Router().ServeHTTP(httptest.NewRecorder(), req)

	assert.Contains(t, buf.String(), "remote_addr="+req.RemoteAddr)
	assert.Contains(t, buf.String(), "forwarded_for=10.1.2.3")
}
