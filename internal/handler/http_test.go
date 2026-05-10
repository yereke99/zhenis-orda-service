package handler_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
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
		PaymentDir:              t.TempDir(),
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
	req := httptest.NewRequest(http.MethodGet, "/api/me?miniapp_dev=1", nil)
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

func TestMiniAppRequiresInitDataWithoutExplicitDev(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMiniAppAcceptsSignedTelegramInitData(t *testing.T) {
	srv := newTestHTTPServer(t, "production")
	initData := signedTelegramInitData(t, "test-token", time.Now(), map[string]any{
		"id":            99112233,
		"first_name":    "Yasmina",
		"last_name":     "Inside",
		"username":      "yasmina_inside",
		"language_code": "ru",
		"photo_url":     "https://t.me/i/userpic/320/yasmina.jpg",
	}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-Telegram-Init-Data", initData)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		User repository.User `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.User.TelegramID != 99112233 {
		t.Fatalf("expected telegram id 99112233, got %d", body.User.TelegramID)
	}
	if body.User.Language != "ru" {
		t.Fatalf("expected ru language, got %q", body.User.Language)
	}
	if body.User.PhotoURL != "https://t.me/i/userpic/320/yasmina.jpg" {
		t.Fatalf("expected photo url to be returned, got %q", body.User.PhotoURL)
	}
}

func TestMiniAppPreservesTelegramPhotoWhenInitDataOmitsIt(t *testing.T) {
	srv := newTestHTTPServer(t, "production")
	firstInitData := signedTelegramInitData(t, "test-token", time.Now(), map[string]any{
		"id":         99112235,
		"first_name": "Aigerim",
		"photo_url":  "https://t.me/i/userpic/320/aigerim.jpg",
	}, nil)
	firstReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	firstReq.Header.Set("X-Telegram-Init-Data", firstInitData)
	firstRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first /api/me expected 200, got %d: %s", firstRec.Code, firstRec.Body.String())
	}

	secondInitData := signedTelegramInitData(t, "test-token", time.Now(), map[string]any{
		"id":         99112235,
		"first_name": "Aigerim",
	}, nil)
	secondReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	secondReq.Header.Set("X-Telegram-Init-Data", secondInitData)
	secondRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second /api/me expected 200, got %d: %s", secondRec.Code, secondRec.Body.String())
	}
	var body struct {
		User repository.User `json:"user"`
	}
	if err := json.NewDecoder(secondRec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.User.PhotoURL != "https://t.me/i/userpic/320/aigerim.jpg" {
		t.Fatalf("expected existing photo url to be preserved, got %q", body.User.PhotoURL)
	}
}

func TestMiniAppRejectsTamperedTelegramInitData(t *testing.T) {
	srv := newTestHTTPServer(t, "production")
	initData := signedTelegramInitData(t, "test-token", time.Now(), map[string]any{
		"id":         99112234,
		"first_name": "Aruzhan",
	}, nil)
	initData = strings.Replace(initData, "Aruzhan", "Other", 1)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-Telegram-Init-Data", initData)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
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

func TestUUIDRouteRejectsBadPaymentID(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	req := httptest.NewRequest(http.MethodGet, "/api/payments/not-a-uuid?miniapp_dev=1", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMiniAppReceiptUpload(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	createReq := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1", bytes.NewBufferString(`{"tariff_code":"BASIC","provider":"kaspi_qr"}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, createReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create payment expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Payment repository.Payment `json:"payment"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("receipt", "receipt.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("Kaspi чек transaction XYZ999 amount 4 990 ₸ 10.05.2026")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/payments/"+created.Payment.ID+"/receipt?miniapp_dev=1", &buf)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload expected 200, got %d: %s", uploadRec.Code, uploadRec.Body.String())
	}
}

func signedTelegramInitData(t *testing.T, token string, authDate time.Time, user map[string]any, extra map[string]string) string {
	t.Helper()
	rawUser, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		"auth_date": strconv.FormatInt(authDate.Unix(), 10),
		"user":      string(rawUser),
	}
	for key, value := range extra {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(token))
	checkMAC := hmac.New(sha256.New, secretMAC.Sum(nil))
	_, _ = checkMAC.Write([]byte(strings.Join(parts, "\n")))

	query := url.Values{}
	for key, value := range values {
		query.Set(key, value)
	}
	query.Set("hash", hex.EncodeToString(checkMAC.Sum(nil)))
	return query.Encode()
}
