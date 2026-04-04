package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/dp229/openpool/pkg/auth"
)

func TestSecurityMiddleware_Authenticate(t *testing.T) {
	dbPath := "test_middleware.db"
	defer os.Remove(dbPath)

	mgr, _ := auth.NewManager(dbPath)
	defer mgr.Close()

	apiKey, _ := mgr.GenerateAPIKey("Test User", "test@example.com", 100, []string{"submit"}, 365*24*time.Hour)

	mw := NewSecurityMiddleware(mgr, 100, "admin-secret", true)

	handler := mw.Authenticate(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	t.Run("Valid API Key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", apiKey.Key)
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("Missing API Key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Invalid API Key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", "invalid_key")
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Auth Disabled", func(t *testing.T) {
		mwNoAuth := NewSecurityMiddleware(mgr, 100, "admin-secret", false)
		handlerNoAuth := mwNoAuth.Authenticate(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handlerNoAuth(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})
}

func TestSecurityMiddleware_RateLimit(t *testing.T) {
	dbPath := "test_middleware_ratelimit.db"
	defer os.Remove(dbPath)

	mgr, _ := auth.NewManager(dbPath)
	defer mgr.Close()

	mw := NewSecurityMiddleware(mgr, 5, "admin-secret", false)

	handler := mw.RateLimit(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d should succeed", i+1)
		}
	}

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status %d, got %d", http.StatusTooManyRequests, w.Code)
	}
}

func TestSecurityMiddleware_ValidateTaskInput(t *testing.T) {
	dbPath := "test_middleware_validate.db"
	defer os.Remove(dbPath)

	mgr, _ := auth.NewManager(dbPath)
	defer mgr.Close()

	mw := NewSecurityMiddleware(mgr, 100, "admin-secret", false)

	handler := mw.ValidateTaskInput(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	t.Run("Valid Input", func(t *testing.T) {
		body := `{"id": "test-123", "credits": 10}`
		req := httptest.NewRequest("POST", "/submit", bytes.NewReader([]byte(body)))
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		body := `{invalid json}`
		req := httptest.NewRequest("POST", "/submit", bytes.NewReader([]byte(body)))
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("Invalid Credits", func(t *testing.T) {
		body := `{"id": "test-123", "credits": -10}`
		req := httptest.NewRequest("POST", "/submit", bytes.NewReader([]byte(body)))
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

func TestSecurityMiddleware_RequireAdmin(t *testing.T) {
	dbPath := "test_middleware_admin.db"
	defer os.Remove(dbPath)

	mgr, _ := auth.NewManager(dbPath)
	defer mgr.Close()

	mw := NewSecurityMiddleware(mgr, 100, "my-secret-key", false)

	handler := mw.RequireAdmin(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("Valid Admin Secret", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/admin", nil)
		req.Header.Set("X-Admin-Secret", "my-secret-key")
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("Missing Admin Secret", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/admin", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Invalid Admin Secret", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/admin", nil)
		req.Header.Set("X-Admin-Secret", "wrong-key")
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status %d, got %d", http.StatusForbidden, w.Code)
		}
	})
}

func TestSecurityMiddleware_RequireScope(t *testing.T) {
	dbPath := "test_middleware_scope.db"
	defer os.Remove(dbPath)

	mgr, _ := auth.NewManager(dbPath)
	defer mgr.Close()

	apiKey, _ := mgr.GenerateAPIKey("Test User", "test@example.com", 100, []string{"query"}, 365*24*time.Hour)

	mw := NewSecurityMiddleware(mgr, 100, "admin-secret", true)

	handler := mw.RequireScope("submit", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("Has Scope", func(t *testing.T) {
		keyWithScope, _ := mgr.GenerateAPIKey("User2", "user2@example.com", 100, []string{"submit"}, 365*24*time.Hour)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", keyWithScope.Key)
		ctx := contextWithAPIKey(context.Background(), keyWithScope)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("Missing Scope", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", apiKey.Key)
		ctx := contextWithAPIKey(context.Background(), apiKey)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status %d, got %d", http.StatusForbidden, w.Code)
		}
	})
}
