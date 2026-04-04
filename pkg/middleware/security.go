package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dp229/openpool/pkg/auth"
	"github.com/dp229/openpool/pkg/ratelimit"
	"github.com/dp229/openpool/pkg/security"
)

type SecurityMiddleware struct {
	authManager *auth.Manager
	rateLimiter *ratelimit.RateLimiter
	sanitizer   *security.Sanitizer
	adminSecret string
	requireAuth bool
}

func NewSecurityMiddleware(authManager *auth.Manager, rateLimitRequests int, adminSecret string, requireAuth bool) *SecurityMiddleware {
	return &SecurityMiddleware{
		authManager: authManager,
		rateLimiter: ratelimit.NewRateLimiter(rateLimitRequests, rateLimitRequests/5, time.Minute),
		sanitizer:   security.NewSanitizer(),
		adminSecret: adminSecret,
		requireAuth: requireAuth,
	}
}

func (sm *SecurityMiddleware) Authenticate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sm.requireAuth {
			next(w, r)
			return
		}

		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			sm.respondError(w, http.StatusUnauthorized, "API key required")
			return
		}

		if err := security.ValidateAPIKey(apiKey); err != nil {
			sm.respondError(w, http.StatusBadRequest, "Invalid API key format")
			return
		}

		authKey, err := sm.authManager.Validate(apiKey)
		if err != nil {
			if err == auth.ErrExpiredAPIKey {
				sm.respondError(w, http.StatusUnauthorized, "API key expired")
			} else {
				sm.respondError(w, http.StatusUnauthorized, "Invalid API key")
			}
			return
		}

		ctx := r.Context()
		ctx = contextWithAPIKey(ctx, authKey)
		next(w, r.WithContext(ctx))
	}
}

func (sm *SecurityMiddleware) RateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientID := sm.getClientID(r)

		if !sm.rateLimiter.Allow(clientID) {
			sm.respondError(w, http.StatusTooManyRequests, "Rate limit exceeded")
			return
		}

		next(w, r)
	}
}

func (sm *SecurityMiddleware) RequireScope(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey, ok := APIKeyFromContext(r.Context())
		if !ok {
			sm.respondError(w, http.StatusUnauthorized, "Authentication required")
			return
		}

		hasScope := false
		for _, s := range apiKey.Scopes {
			if s == scope {
				hasScope = true
				break
			}
		}

		if !hasScope {
			sm.respondError(w, http.StatusForbidden, fmt.Sprintf("Scope '%s' required", scope))
			return
		}

		next(w, r)
	}
}

func (sm *SecurityMiddleware) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminSecret := r.Header.Get("X-Admin-Secret")
		if adminSecret == "" {
			sm.respondError(w, http.StatusUnauthorized, "Admin secret required")
			return
		}

		if adminSecret != sm.adminSecret {
			sm.respondError(w, http.StatusForbidden, "Invalid admin secret")
			return
		}

		next(w, r)
	}
}

func (sm *SecurityMiddleware) ValidateTaskInput(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var task map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
			sm.respondError(w, http.StatusBadRequest, "Invalid JSON")
			return
		}

		if wasmPath, ok := task["wasm_path"].(string); ok && wasmPath != "" {
			sanitized, err := sm.sanitizer.ValidateWASMPath("/opt/openpool/wasm", wasmPath)
			if err != nil {
				sm.respondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid wasm_path: %v", err))
				return
			}
			task["wasm_path"] = sanitized
		}

		if id, ok := task["id"].(string); ok {
			if err := security.ValidateTaskID(id); err != nil {
				sm.respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			task["id"] = sm.sanitizer.SanitizeString(id, 256)
		}

		if credits, ok := task["credits"].(float64); ok {
			if err := security.ValidateCredits(int(credits)); err != nil {
				sm.respondError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		sanitizedJSON, err := json.Marshal(task)
		if err != nil {
			sm.respondError(w, http.StatusInternalServerError, "Failed to process task")
			return
		}

		r.Body = io.NopCloser(bytes.NewReader(sanitizedJSON))
		next(w, r)
	}
}

func (sm *SecurityMiddleware) LogRequest(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next(sw, r)

		duration := time.Since(start)
		clientID := sm.getClientID(r)

		fmt.Printf("[%s] %s %s %d %v %s\n",
			time.Now().Format("2006-01-02 15:04:05"),
			r.Method,
			r.URL.Path,
			sw.statusCode,
			duration,
			clientID,
		)
	}
}

func (sm *SecurityMiddleware) getClientID(r *http.Request) string {
	apiKey := r.Header.Get("X-API-Key")
	if apiKey != "" {
		return apiKey
	}

	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}

	return strings.Split(r.RemoteAddr, ":")[0]
}

func (sm *SecurityMiddleware) respondError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
		"code":  http.StatusText(code),
	})
}

type contextKey string

const apiKeyKey contextKey = "api_key"

func contextWithAPIKey(ctx context.Context, key *auth.APIKey) context.Context {
	return context.WithValue(ctx, apiKeyKey, key)
}

func APIKeyFromContext(ctx context.Context) (*auth.APIKey, bool) {
	key, ok := ctx.Value(apiKeyKey).(*auth.APIKey)
	return key, ok
}

type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.statusCode = code
	sw.ResponseWriter.WriteHeader(code)
}
