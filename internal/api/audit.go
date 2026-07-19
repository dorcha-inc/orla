package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

// ControlPlaneAuditMetrics records mutating requests to the /api/v1
// control plane. It is defined in this package, the consumer, so
// metrics.Metrics can satisfy it without this package importing the
// metrics package.
type ControlPlaneAuditMetrics interface {
	IncControlPlaneMutation(resource, method, outcome string)
}

// controlPlaneResource returns the resource name from a control-plane
// path, the segment right after /api/v1/, and reports whether path
// has that shape.
func controlPlaneResource(path string) (string, bool) {
	const prefix = "/api/v1/"
	rest, ok := strings.CutPrefix(path, prefix)
	if !ok || rest == "" {
		return "", false
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return rest, true
}

// AuditControlPlaneMutations logs and counts mutating requests,
// anything but GET and HEAD, to the /api/v1 control plane. It is a
// visibility aid, not an access gate. Orla has no per-caller identity
// to check, so remote_addr and X-Forwarded-For are recorded best
// effort, not verified. A handler panic is recorded with an error
// outcome before the panic is re-raised, so a crashed mutation still
// leaves a trace for the outer recoverer to turn into a 500.
func AuditControlPlaneMutations(logger *slog.Logger, metrics ControlPlaneAuditMetrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				next.ServeHTTP(w, r)
				return
			}

			resource, ok := controlPlaneResource(r.URL.Path)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			// loggingMiddleware, mounted globally, already wraps w by
			// the time any control-plane route sees a request. Reuse
			// that wrap instead of layering a second one.
			ww, wrapped := w.(middleware.WrapResponseWriter)
			if !wrapped {
				ww = middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			}

			defer func() {
				if rec := recover(); rec != nil {
					recordMutation(logger, metrics, r, resource, ww.Status(), "error")
					panic(rec)
				}
			}()

			next.ServeHTTP(ww, r)

			outcome := "success"
			if status := ww.Status(); status < 200 || status >= 300 {
				outcome = "error"
			}
			recordMutation(logger, metrics, r, resource, ww.Status(), outcome)
		})
	}
}

// recordMutation increments the counter and writes the audit log line.
// outcome is "success" for a 2xx response, "error" otherwise, so a
// rejected write is never counted the same as one that actually
// changed state.
func recordMutation(logger *slog.Logger, metrics ControlPlaneAuditMetrics, r *http.Request, resource string, status int, outcome string) {
	metrics.IncControlPlaneMutation(resource, r.Method, outcome)
	logger.LogAttrs(r.Context(), slog.LevelWarn, "control plane mutation",
		slog.String("resource", resource),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.String("outcome", outcome),
		slog.String("request_id", middleware.GetReqID(r.Context())),
		slog.String("remote_addr", r.RemoteAddr),
		slog.String("forwarded_for", r.Header.Get("X-Forwarded-For")),
	)
}
