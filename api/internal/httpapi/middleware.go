package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aribpos/license-api/internal/auth"
	"golang.org/x/time/rate"
)

type ctxKey int

const claimsKey ctxKey = iota

// requireAuth validates the bearer access token and stores claims in context.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := s.auth.TokenManager().Parse(token)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdmin rejects non-admin callers (must run after requireAuth).
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r.Context())
		if c == nil || !c.Admin {
			writeErr(w, http.StatusForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func claimsFrom(ctx context.Context) *auth.Claims {
	c, _ := ctx.Value(claimsKey).(*auth.Claims)
	return c
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// requestLogger logs method, path, status and latency.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			log.Info("http",
				"method", r.Method, "path", r.URL.Path,
				"status", sw.status, "dur_ms", time.Since(start).Milliseconds())
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher by delegating to the wrapped ResponseWriter.
// Embedding http.ResponseWriter only promotes its own three methods, not
// Flush — without this, every request through requestLogger (i.e. every
// request) fails the SSE handler's w.(http.Flusher) check and the event
// stream 500s on every connection attempt.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// --- OTP rate limiting (per client IP) ---

func (s *Server) rateLimitOTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.otpLimiter.Allow(clientIP(r)) {
			writeErr(w, http.StatusTooManyRequests, "too many requests, slow down")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if i := strings.LastIndex(r.RemoteAddr, ":"); i >= 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

type keyedLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rate.Limiter
	r       rate.Limit
	burst   int
}

func newKeyedLimiter(r rate.Limit, burst int) *keyedLimiter {
	return &keyedLimiter{buckets: map[string]*rate.Limiter{}, r: r, burst: burst}
}

func (k *keyedLimiter) Allow(key string) bool {
	k.mu.Lock()
	lim, ok := k.buckets[key]
	if !ok {
		lim = rate.NewLimiter(k.r, k.burst)
		k.buckets[key] = lim
	}
	k.mu.Unlock()
	return lim.Allow()
}

func rateEvery(d time.Duration) rate.Limit { return rate.Every(d) }
