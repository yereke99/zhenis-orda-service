package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"zhenis-orda-service/config"
	"zhenis-orda-service/internal/handler"
	"zhenis-orda-service/internal/repository"
	"zhenis-orda-service/traits/database"
)

func newTestHTTPServer(t *testing.T, env string) *handler.Server {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Token:                   "test-token",
		Port:                    "8080",
		Env:                     env,
		BaseURL:                 "http://localhost:8080",
		MiniAppURL:              "http://localhost:8080",
		DBPath:                  ":memory:",
		UploadDir:               t.TempDir(),
		AllowedOrigins:          []string{"http://localhost:8080"},
		PaymentPendingTTL:       time.Hour,
		SubscriptionDefaultDays: 30,
		MaxReceiptBytes:         1024 * 1024,
		BrowserSessionTTL:       time.Hour,
		TelegramInitDataMaxAge:  time.Hour,
	}
	return handler.NewServer(cfg, repository.New(db), handler.NewMemoryKV(), zap.NewNop())
}

func TestMiniAppDevAuth(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["user"] == nil {
		t.Fatal("expected user payload")
	}
}

func TestMiniAppProductionRequiresInitData(t *testing.T) {
	srv := newTestHTTPServer(t, "production")
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestBrowserAdminAuthAndStats(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	loginBody := bytes.NewBufferString(`{"password":"admin"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/browser-auth/login", loginBody)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}

	statsReq := httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	statsReq.AddCookie(cookies[0])
	statsRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(statsRec, statsReq)
	if statsRec.Code != http.StatusOK {
		t.Fatalf("stats expected 200, got %d: %s", statsRec.Code, statsRec.Body.String())
	}
}

func TestAdminStatsRequiresAuth(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
