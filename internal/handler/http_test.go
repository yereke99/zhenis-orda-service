package handler_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	return newTestHTTPServerWithConfig(t, env, nil)
}

func newTestHTTPServerWithConfig(t *testing.T, env string, configure func(*config.Config)) *handler.Server {
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
		BookUploadDir:           t.TempDir(),
		FreeLessonUploadDir:     t.TempDir(),
		PaymentDir:              t.TempDir(),
		AllowedOrigins:          []string{"http://localhost:8080"},
		WhatsAppSalesPhone:      "77476823396",
		PaymentPendingTTL:       time.Hour,
		SubscriptionDefaultDays: 30,
		MaxReceiptBytes:         1024 * 1024,
		MaxBookImageBytes:       1024 * 1024,
		MaxFreeLessonImageBytes: 1024 * 1024,
		BrowserSessionTTL:       time.Hour,
		TelegramInitDataMaxAge:  time.Hour,
	}
	if configure != nil {
		configure(&cfg)
	}
	return handler.NewServer(cfg, repository.New(db), handler.NewMemoryKV(), zap.NewNop())
}

type sentBotMessage struct {
	chatID int64
	text   string
}

type testInviteBot struct {
	messages []sentBotMessage
	links    []string
	calls    int
	chatID   string
	name     string
}

func (b *testInviteBot) CreateInviteLink(ctx context.Context, chatID, name string, expiresAt time.Time) (string, error) {
	b.calls++
	b.chatID = chatID
	b.name = name
	if len(b.links) > 0 {
		return b.links[0], nil
	}
	return "https://t.me/+test", nil
}

func (b *testInviteBot) SendMessage(ctx context.Context, chatID int64, text string) error {
	b.messages = append(b.messages, sentBotMessage{chatID: chatID, text: text})
	return nil
}

type failingInviteBot struct{}

func (b failingInviteBot) CreateInviteLink(ctx context.Context, chatID, name string, expiresAt time.Time) (string, error) {
	return "", errors.New("telegram says bot is not admin")
}

func (b failingInviteBot) SendMessage(ctx context.Context, chatID int64, text string) error {
	return nil
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
	if body.User.Language != "kk" {
		t.Fatalf("expected kk language, got %q", body.User.Language)
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

func loginAdminCookie(t *testing.T, srv *handler.Server) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/browser-auth/login", bytes.NewBufferString(`{"password":"admin"}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected admin session cookie")
	}
	return cookies[0]
}

func multipartBody(t *testing.T, fieldName, fileName string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, writer.FormDataContentType()
}

func tinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}
}

func setLevelTelegramChat(t *testing.T, srv *handler.Server, cookie *http.Cookie, levelNumber int, chatID string) repository.Level {
	t.Helper()
	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/levels", nil)
	listReq.AddCookie(cookie)
	listRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("levels expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	var listBody struct {
		Levels []repository.Level `json:"levels"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&listBody); err != nil {
		t.Fatal(err)
	}
	var level repository.Level
	for _, item := range listBody.Levels {
		if item.Number == levelNumber {
			level = item
			break
		}
	}
	if level.ID == "" {
		t.Fatalf("level %d not found", levelNumber)
	}
	payload := map[string]any{
		"number":           level.Number,
		"title_kk":         level.TitleKK,
		"title_ru":         level.TitleRU,
		"description_kk":   level.DescriptionKK,
		"description_ru":   level.DescriptionRU,
		"telegram_chat_id": chatID,
		"sort_order":       level.SortOrder,
		"is_active":        level.IsActive,
	}
	raw, _ := json.Marshal(payload)
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/admin/levels/"+level.ID, bytes.NewReader(raw))
	patchReq.AddCookie(cookie)
	patchRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch level expected 200, got %d: %s", patchRec.Code, patchRec.Body.String())
	}
	var patchBody struct {
		Level repository.Level `json:"level"`
	}
	if err := json.NewDecoder(patchRec.Body).Decode(&patchBody); err != nil {
		t.Fatal(err)
	}
	return patchBody.Level
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

func TestAdminBooksCRUDAndPublicVisibility(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/books", bytes.NewBufferString(`{
		"title": "Ақша формуласы",
		"description": "Авторлық кітап сипаттамасы",
		"price_kzt": 12000,
		"image_url": "https://example.com/book.webp",
		"is_active": true
	}`))
	createReq.AddCookie(cookie)
	createRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create book expected 200, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Book repository.Book `json:"book"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Book.ID == "" || !created.Book.IsActive {
		t.Fatalf("unexpected created book: %#v", created.Book)
	}

	publicReq := httptest.NewRequest(http.MethodGet, "/api/books?miniapp_dev=1", nil)
	publicRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(publicRec, publicReq)
	if publicRec.Code != http.StatusOK {
		t.Fatalf("public books expected 200, got %d: %s", publicRec.Code, publicRec.Body.String())
	}
	var publicBody struct {
		Books              []repository.Book `json:"books"`
		WhatsAppSalesPhone string            `json:"whatsapp_sales_phone"`
	}
	if err := json.NewDecoder(publicRec.Body).Decode(&publicBody); err != nil {
		t.Fatal(err)
	}
	if len(publicBody.Books) != 1 || publicBody.Books[0].ID != created.Book.ID {
		t.Fatalf("expected one active public book, got %#v", publicBody.Books)
	}
	if publicBody.WhatsAppSalesPhone != "77476823396" {
		t.Fatalf("whatsapp phone = %q", publicBody.WhatsAppSalesPhone)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/books/"+created.Book.ID, nil)
	deleteReq.AddCookie(cookie)
	deleteRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete book expected 200, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}

	publicAfterReq := httptest.NewRequest(http.MethodGet, "/api/books?miniapp_dev=1", nil)
	publicAfterRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(publicAfterRec, publicAfterReq)
	if publicAfterRec.Code != http.StatusOK {
		t.Fatalf("public books after delete expected 200, got %d: %s", publicAfterRec.Code, publicAfterRec.Body.String())
	}
	publicBody.Books = nil
	if err := json.NewDecoder(publicAfterRec.Body).Decode(&publicBody); err != nil {
		t.Fatal(err)
	}
	if len(publicBody.Books) != 0 {
		t.Fatalf("expected inactive book hidden from public API, got %#v", publicBody.Books)
	}
}

func TestAdminBookImageUploadValidationAndServing(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)

	unauthReq := httptest.NewRequest(http.MethodPost, "/api/admin/books/upload-image", nil)
	unauthRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected upload auth 401, got %d", unauthRec.Code)
	}

	badBuf, badType := multipartBody(t, "image", "bad.txt", []byte("not an image"))
	badReq := httptest.NewRequest(http.MethodPost, "/api/admin/books/upload-image", badBuf)
	badReq.Header.Set("Content-Type", badType)
	badReq.AddCookie(cookie)
	badRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad image expected 400, got %d: %s", badRec.Code, badRec.Body.String())
	}

	imageBuf, imageType := multipartBody(t, "image", "cover.png", tinyPNG())
	imageReq := httptest.NewRequest(http.MethodPost, "/api/admin/books/upload-image", imageBuf)
	imageReq.Header.Set("Content-Type", imageType)
	imageReq.AddCookie(cookie)
	imageRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(imageRec, imageReq)
	if imageRec.Code != http.StatusOK {
		t.Fatalf("image upload expected 200, got %d: %s", imageRec.Code, imageRec.Body.String())
	}
	var body struct {
		ImageFilePath string `json:"image_file_path"`
		ImageSource   string `json:"image_source"`
	}
	if err := json.NewDecoder(imageRec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body.ImageFilePath, "/uploads/books/") || body.ImageSource != "uploaded" {
		t.Fatalf("unexpected upload payload: %#v", body)
	}
	staticReq := httptest.NewRequest(http.MethodGet, body.ImageFilePath, nil)
	staticRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(staticRec, staticReq)
	if staticRec.Code != http.StatusOK {
		t.Fatalf("uploaded image static serve expected 200, got %d", staticRec.Code)
	}
}

func TestAdminFreeLessonsCRUDAndPublicVisibility(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)

	badReq := httptest.NewRequest(http.MethodPost, "/api/admin/free-lessons", bytes.NewBufferString(`{
		"title": "Тегін сабақ",
		"description": "Ашық сабақ сипаттамасы",
		"image_url": "https://example.com/free.webp",
		"youtube_url": "https://example.com/watch?v=dQw4w9WgXcQ",
		"is_active": true
	}`))
	badReq.AddCookie(cookie)
	badRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid youtube expected 400, got %d: %s", badRec.Code, badRec.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/free-lessons", bytes.NewBufferString(`{
		"title": "Тегін сабақ",
		"short_description": "Қысқаша",
		"description": "Ашық сабақ сипаттамасы",
		"image_url": "https://example.com/free.webp",
		"youtube_url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"is_active": true
	}`))
	createReq.AddCookie(cookie)
	createRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create free lesson expected 200, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		FreeLesson repository.FreeLesson `json:"free_lesson"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.FreeLesson.ID == "" || created.FreeLesson.YouTubeVideoID != "dQw4w9WgXcQ" || created.FreeLesson.YouTubeEmbedURL != "https://www.youtube.com/embed/dQw4w9WgXcQ" {
		t.Fatalf("unexpected created free lesson: %#v", created.FreeLesson)
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/admin/free-lessons/"+created.FreeLesson.ID, bytes.NewBufferString(`{
		"title": "Тегін сабақ 2",
		"short_description": "Қысқаша",
		"description": "Ашық сабақ сипаттамасы",
		"image_url": "https://example.com/free.webp",
		"youtube_url": "https://youtu.be/dQw4w9WgXcQ?si=test",
		"is_active": true
	}`))
	patchReq.AddCookie(cookie)
	patchRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch youtu.be expected 200, got %d: %s", patchRec.Code, patchRec.Body.String())
	}

	publicReq := httptest.NewRequest(http.MethodGet, "/api/free-lessons?miniapp_dev=1", nil)
	publicRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(publicRec, publicReq)
	if publicRec.Code != http.StatusOK {
		t.Fatalf("public free lessons expected 200, got %d: %s", publicRec.Code, publicRec.Body.String())
	}
	var publicBody struct {
		FreeLessons []repository.FreeLesson `json:"free_lessons"`
	}
	if err := json.NewDecoder(publicRec.Body).Decode(&publicBody); err != nil {
		t.Fatal(err)
	}
	if len(publicBody.FreeLessons) != 1 || publicBody.FreeLessons[0].ID != created.FreeLesson.ID {
		t.Fatalf("expected one active public free lesson, got %#v", publicBody.FreeLessons)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/free-lessons/"+created.FreeLesson.ID+"?miniapp_dev=1", nil)
	detailRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("public free lesson detail expected 200, got %d: %s", detailRec.Code, detailRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/free-lessons/"+created.FreeLesson.ID, nil)
	deleteReq.AddCookie(cookie)
	deleteRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("archive free lesson expected 200, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}

	publicAfterReq := httptest.NewRequest(http.MethodGet, "/api/free-lessons?miniapp_dev=1", nil)
	publicAfterRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(publicAfterRec, publicAfterReq)
	if publicAfterRec.Code != http.StatusOK {
		t.Fatalf("public free lessons after delete expected 200, got %d: %s", publicAfterRec.Code, publicAfterRec.Body.String())
	}
	publicBody.FreeLessons = nil
	if err := json.NewDecoder(publicAfterRec.Body).Decode(&publicBody); err != nil {
		t.Fatal(err)
	}
	if len(publicBody.FreeLessons) != 0 {
		t.Fatalf("expected inactive free lesson hidden from public API, got %#v", publicBody.FreeLessons)
	}
}

func TestAdminFreeLessonImageUploadValidationAndServing(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)

	unauthReq := httptest.NewRequest(http.MethodPost, "/api/admin/free-lessons/upload-image", nil)
	unauthRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected upload auth 401, got %d", unauthRec.Code)
	}

	badBuf, badType := multipartBody(t, "image", "bad.txt", []byte("not an image"))
	badReq := httptest.NewRequest(http.MethodPost, "/api/admin/free-lessons/upload-image", badBuf)
	badReq.Header.Set("Content-Type", badType)
	badReq.AddCookie(cookie)
	badRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad free lesson image expected 400, got %d: %s", badRec.Code, badRec.Body.String())
	}

	imageBuf, imageType := multipartBody(t, "image", "cover.png", tinyPNG())
	imageReq := httptest.NewRequest(http.MethodPost, "/api/admin/free-lessons/upload-image", imageBuf)
	imageReq.Header.Set("Content-Type", imageType)
	imageReq.AddCookie(cookie)
	imageRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(imageRec, imageReq)
	if imageRec.Code != http.StatusOK {
		t.Fatalf("free lesson image upload expected 200, got %d: %s", imageRec.Code, imageRec.Body.String())
	}
	var body struct {
		ImageFilePath string `json:"image_file_path"`
		ImageSource   string `json:"image_source"`
	}
	if err := json.NewDecoder(imageRec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body.ImageFilePath, "/uploads/free-lessons/") || body.ImageSource != "uploaded" {
		t.Fatalf("unexpected free lesson upload payload: %#v", body)
	}
	staticReq := httptest.NewRequest(http.MethodGet, body.ImageFilePath, nil)
	staticRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(staticRec, staticReq)
	if staticRec.Code != http.StatusOK {
		t.Fatalf("uploaded free lesson image static serve expected 200, got %d", staticRec.Code)
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

func TestMiniAppSupportNotifiesAdmins(t *testing.T) {
	srv := newTestHTTPServerWithConfig(t, "development", func(cfg *config.Config) {
		cfg.AdminIDs = []int64{111222}
	})
	bot := &testInviteBot{}
	srv.SetBot(bot)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/support?miniapp_dev=1&telegram_id=555777&username=aliya&first_name="+url.QueryEscape("Әлия"),
		bytes.NewBufferString(`{"body":"Сәлем, көмек керек"}`),
	)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("support expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	want := "Хабарламаңыз әкімшіге жіберілді. Жауапты осы чаттан күтіңіз."
	if body.Message != want {
		t.Fatalf("support message = %q, want %q", body.Message, want)
	}
	if len(bot.messages) != 1 {
		t.Fatalf("expected 1 admin notification, got %d", len(bot.messages))
	}
	if bot.messages[0].chatID != 111222 {
		t.Fatalf("admin chat id = %d", bot.messages[0].chatID)
	}
	adminText := bot.messages[0].text
	for _, fragment := range []string{
		"Source: ZHENIS ORDA Mini App support",
		"User ID: 555777",
		"Username: @aliya",
		"Аты: Әлия",
		"Хабарлама:",
		"Сәлем, көмек керек",
	} {
		if !strings.Contains(adminText, fragment) {
			t.Fatalf("admin notification missing %q in %s", fragment, adminText)
		}
	}
}

func TestLevelTelegramInviteRequiresUnlockedLevel(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)
	setLevelTelegramChat(t, srv, cookie, 1, "2351826422")

	req := httptest.NewRequest(http.MethodPost, "/api/levels/1/telegram-invite?miniapp_dev=1&telegram_id=9001", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for locked level, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLevelTelegramInviteCreatesAndReusesLink(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)
	level := setLevelTelegramChat(t, srv, cookie, 1, "2351826422")
	if level.TelegramChatID != "-1002351826422" {
		t.Fatalf("expected normalized chat id, got %q", level.TelegramChatID)
	}
	bot := &testInviteBot{links: []string{"https://t.me/+level-one"}}
	srv.SetBot(bot)

	createReq := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1&telegram_id=9002", bytes.NewBufferString(`{"tariff_code":"BASIC","provider":"kaspi_qr"}`))
	createRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create payment expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Payment repository.Payment `json:"payment"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/admin/payments/"+created.Payment.ID+"/approve", bytes.NewBufferString(`{"days":30}`))
	approveReq.AddCookie(cookie)
	approveRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve expected 200, got %d: %s", approveRec.Code, approveRec.Body.String())
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/levels/1/telegram-invite?miniapp_dev=1&telegram_id=9002", bytes.NewBufferString(`{}`))
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("invite %d expected 200, got %d: %s", i+1, rec.Code, rec.Body.String())
		}
		var body struct {
			InviteLink string `json:"invite_link"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.InviteLink != "https://t.me/+level-one" {
			t.Fatalf("invite link = %q", body.InviteLink)
		}
	}
	if bot.calls != 1 {
		t.Fatalf("expected one Telegram API call, got %d", bot.calls)
	}
	if bot.chatID != "-1002351826422" {
		t.Fatalf("bot chat id = %q", bot.chatID)
	}
	if !strings.Contains(bot.name, "user:9002") || !strings.Contains(bot.name, "level:1") {
		t.Fatalf("unexpected invite name %q", bot.name)
	}
}

func TestLevelTelegramInviteFailureIsSafe(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	cookie := loginAdminCookie(t, srv)
	setLevelTelegramChat(t, srv, cookie, 1, "-1002351826422")
	srv.SetBot(failingInviteBot{})

	createReq := httptest.NewRequest(http.MethodPost, "/api/payments?miniapp_dev=1&telegram_id=9003", bytes.NewBufferString(`{"tariff_code":"BASIC","provider":"kaspi_qr"}`))
	createRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create payment expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Payment repository.Payment `json:"payment"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/admin/payments/"+created.Payment.ID+"/approve", bytes.NewBufferString(`{"days":30}`))
	approveReq.AddCookie(cookie)
	approveRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve expected 200, got %d: %s", approveRec.Code, approveRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/levels/1/telegram-invite?miniapp_dev=1&telegram_id=9003", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "bot is not admin") {
		t.Fatalf("unsafe Telegram error leaked to client: %s", rec.Body.String())
	}
}

func TestFinancialIQResultSavedToMe(t *testing.T) {
	srv := newTestHTTPServer(t, "development")
	req := httptest.NewRequest(http.MethodPost, "/api/financial-iq?miniapp_dev=1&telegram_id=9010", bytes.NewBufferString(`{
		"score": 88,
		"result_title": "81-140 балл аралығы",
		"result_level": "Қаржылық IQ деңгейі — жоғары",
		"result_text": "Жақсы нәтиже",
		"answers": {"q1":"2"}
	}`))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("financial iq save expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	meReq := httptest.NewRequest(http.MethodGet, "/api/me?miniapp_dev=1&telegram_id=9010", nil)
	meRec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me expected 200, got %d: %s", meRec.Code, meRec.Body.String())
	}
	var body struct {
		FinancialIQ *repository.FinancialIQResult `json:"financial_iq"`
	}
	if err := json.NewDecoder(meRec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.FinancialIQ == nil || body.FinancialIQ.Score != 88 {
		t.Fatalf("unexpected financial iq result: %#v", body.FinancialIQ)
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
